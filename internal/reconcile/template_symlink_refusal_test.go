package reconcile

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/cameronsjo/bosun/internal/fileutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderDirectoryRefusesSymlinkedTemplates(t *testing.T) {
	for _, target := range []string{"in-root", "missing"} {
		t.Run(target, func(t *testing.T) {
			base := evalSymlinks(t, t.TempDir())
			source := filepath.Join(base, "source")
			require.NoError(t, os.Mkdir(source, 0o700))
			if target == "in-root" {
				require.NoError(t, os.WriteFile(filepath.Join(source, target), []byte("shared template"), 0o600))
			}
			templatePath := filepath.Join(source, "config.tmpl")
			if err := os.Symlink(target, templatePath); err != nil {
				t.Skipf("symlink creation unavailable: %v", err)
			}

			err := NewTemplateOps(nil).RenderDirectory(context.Background(), source, filepath.Join(base, "stage"), "infra")

			require.ErrorContains(t, err, "refusing symlinked template")
			assert.Contains(t, err.Error(), templatePath)
			assert.Contains(t, err.Error(), "replace the symlink with a regular file")
			assert.NoFileExists(t, filepath.Join(base, "stage", "infra", "config"))
		})
	}
}

func TestRenderDirectoryRefusesTemplateSymlinkAfterDiscovery(t *testing.T) {
	base := evalSymlinks(t, t.TempDir())
	source := filepath.Join(base, "source")
	stage := filepath.Join(base, "stage")
	require.NoError(t, os.Mkdir(source, 0o700))
	templatePath := filepath.Join(source, "config.tmpl")
	require.NoError(t, os.WriteFile(templatePath, []byte("regular template"), 0o600))
	outside := filepath.Join(base, "outside")
	require.NoError(t, os.WriteFile(outside, []byte("outside payload"), 0o600))
	output := filepath.Join(stage, "infra", "config")
	require.NoError(t, os.MkdirAll(filepath.Dir(output), 0o700))
	require.NoError(t, os.WriteFile(output, []byte("previous output"), 0o600))

	tmpl := NewTemplateOps(nil)
	tmpl.walkDirFn = func(_ string, visit fs.WalkDirFunc) error {
		info, err := os.Lstat(templatePath)
		require.NoError(t, err)
		require.NoError(t, os.Remove(templatePath))
		if err := os.Symlink(outside, templatePath); err != nil {
			t.Skipf("symlink creation unavailable: %v", err)
		}
		// The captured regular DirEntry models discovery before replacement.
		return visit(templatePath, fs.FileInfoToDirEntry(info), nil)
	}

	err := tmpl.RenderDirectory(context.Background(), source, stage, "infra")

	require.ErrorIs(t, err, fileutil.ErrSymlinkSkipped)
	assert.Contains(t, err.Error(), templatePath)
	assert.Contains(t, err.Error(), "replace the symlink with a regular file")
	content, err := os.ReadFile(output)
	require.NoError(t, err)
	assert.Equal(t, "previous output", string(content))
}
