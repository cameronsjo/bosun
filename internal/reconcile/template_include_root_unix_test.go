//go:build darwin || linux

package reconcile

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"text/template"
	"time"

	"github.com/cameronsjo/bosun/internal/fileutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// includeRootFixture lays out <tmp>/templates/data.json (the allowed file) and
// <tmp>/outside/data.json (a secret at the same relative name), then pins a
// root on templates the way RenderDirectory does before walking the checkout.
func includeRootFixture(t *testing.T) (tmpDir, includeDir string, root *os.Root) {
	t.Helper()
	tmpDir = evalSymlinks(t, t.TempDir())
	includeDir = filepath.Join(tmpDir, "templates")
	outsideDir := filepath.Join(tmpDir, "outside")
	require.NoError(t, os.MkdirAll(includeDir, 0o755))
	require.NoError(t, os.MkdirAll(outsideDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(includeDir, "data.json"), []byte(`{"v":"safe"}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outsideDir, "data.json"), []byte(`{"v":"secret"}`), 0o600))

	root, err := fileutil.OpenPinnedRoot(includeDir, false)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	return tmpDir, includeDir, root
}

// swapIncludeDirForEscapingSymlink replaces the include directory with a
// symlink to <tmp>/outside after the root was pinned. validateIncludePath
// resolves both the include path and the include root through the same link,
// so its containment check passes; only a read bound to the pinned root stays
// inside the original directory.
func swapIncludeDirForEscapingSymlink(t *testing.T, tmpDir, includeDir string) {
	t.Helper()
	require.NoError(t, os.Rename(includeDir, filepath.Join(tmpDir, "templates.pinned")))
	require.NoError(t, os.Symlink(filepath.Join(tmpDir, "outside"), includeDir))
	require.NoError(t, validateIncludePath(filepath.Join(includeDir, "data.json"), includeDir),
		"fixture must reproduce the window the path validator cannot see")
}

func execIncludeFunc(t *testing.T, funcs template.FuncMap, text string) (string, error) {
	t.Helper()
	tmpl, err := template.New("t").Funcs(funcs).Parse(text)
	require.NoError(t, err)
	var out bytes.Buffer
	err = tmpl.Execute(&out, nil)
	return out.String(), err
}

// TestTemplateFuncs_IncludeReadsStayOnPinnedRootAfterSwap pins #696: include
// and fromJsonFile must read through the include root opened before rendering,
// never re-open the validated path. A path-based read follows the swapped
// symlink and returns the outside secret.
func TestTemplateFuncs_IncludeReadsStayOnPinnedRootAfterSwap(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
	}{
		{name: "include", text: `{{ include %q }}`},
		{name: "fromJsonFile", text: `{{ (fromJsonFile %q).v }}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir, includeDir, root := includeRootFixture(t)
			funcs := templateFuncsWithRoot(includeDir, root, includeDir)
			swapIncludeDirForEscapingSymlink(t, tmpDir, includeDir)

			out, err := execIncludeFunc(t, funcs, fmt.Sprintf(tc.text, filepath.Join(includeDir, "data.json")))
			require.NoError(t, err)
			assert.NotContains(t, out, "secret", "read followed the swapped include directory")
			assert.Contains(t, out, "safe")
		})
	}
}

// TestRenderDirectory_IncludeIgnoresIncludeDirSwappedMidRender drives the same
// swap through RenderDirectory: the swap lands after the include root is
// pinned and before the template that includes it executes.
func TestRenderDirectory_IncludeIgnoresIncludeDirSwappedMidRender(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())
	sourceDir := filepath.Join(tmpDir, "source")
	includeDir := filepath.Join(sourceDir, DefaultIncludeSubdir)
	outsideDir := filepath.Join(tmpDir, "outside")
	require.NoError(t, os.MkdirAll(includeDir, 0o755))
	require.NoError(t, os.MkdirAll(outsideDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(includeDir, "data.txt"), []byte("safe"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outsideDir, "data.txt"), []byte("secret"), 0o600))
	includePath := filepath.Join(includeDir, "data.txt")
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "app.conf.tmpl"),
		[]byte(fmt.Sprintf(`{{ include %q }}`, includePath)), 0o644))

	tmpl := NewTemplateOps(map[string]any{})
	tmpl.walkDirFn = func(root string, fn fs.WalkDirFunc) error {
		// The include root is already pinned when the walk starts.
		require.NoError(t, os.Rename(includeDir, filepath.Join(sourceDir, "templates.pinned")))
		require.NoError(t, os.Symlink(outsideDir, includeDir))
		return filepath.WalkDir(root, fn)
	}

	stagingDir := filepath.Join(tmpDir, "staging")
	require.NoError(t, tmpl.RenderDirectory(context.Background(), sourceDir, stagingDir, "."))
	rendered, err := os.ReadFile(filepath.Join(stagingDir, "app.conf"))
	require.NoError(t, err)
	assert.Equal(t, "safe", string(rendered))
}

// TestTemplateFuncs_IncludeRefusesFIFOWithoutBlocking pins #696: a FIFO under
// the include directory is refused with an error instead of blocking the
// render on an open that waits for a writer.
func TestTemplateFuncs_IncludeRefusesFIFOWithoutBlocking(t *testing.T) {
	for _, tc := range []struct {
		name   string
		text   string
		pinned bool
	}{
		{name: "include with pinned root", text: `{{ include %q }}`, pinned: true},
		{name: "fromJsonFile with pinned root", text: `{{ fromJsonFile %q }}`, pinned: true},
		{name: "include with standalone root", text: `{{ include %q }}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, includeDir, root := includeRootFixture(t)
			fifo := filepath.Join(includeDir, "pipe")
			require.NoError(t, syscall.Mkfifo(fifo, 0o600))
			funcs := TemplateFuncs(includeDir)
			if tc.pinned {
				funcs = templateFuncsWithRoot(includeDir, root, includeDir)
			}

			parsed, err := template.New("t").Funcs(funcs).Parse(fmt.Sprintf(tc.text, fifo))
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() { done <- parsed.Execute(io.Discard, nil) }()

			select {
			case err := <-done:
				require.ErrorIs(t, err, fileutil.ErrUnsupportedFileType)
			case <-time.After(2 * time.Second):
				if writer, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
					_ = writer.Close()
				}
				t.Fatal("include blocked opening a FIFO")
			}
		})
	}
}
