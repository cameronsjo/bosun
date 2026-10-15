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

func TestTemplateOps_ConfinementErrors(t *testing.T) {
	t.Run("missing template parent fails before output mutation", func(t *testing.T) {
		root := t.TempDir()
		templatePath := filepath.Join(root, "missing", "config.tmpl")
		outputPath := filepath.Join(root, "rendered", "config")

		err := NewTemplateOps(nil).ExecuteTemplate(context.Background(), templatePath, outputPath)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read template")
		assert.NoFileExists(t, outputPath)
		assert.NoDirExists(t, filepath.Dir(outputPath))
	})

	t.Run("pinned source root rejects a template outside its namespace", func(t *testing.T) {
		sourceDir := t.TempDir()
		outsideDir := t.TempDir()
		templatePath := filepath.Join(outsideDir, "outside.tmpl")
		outputPath := filepath.Join(t.TempDir(), "rendered")
		require.NoError(t, os.WriteFile(templatePath, []byte("secret"), 0o600))

		root, err := fileutil.OpenPinnedRoot(sourceDir, false)
		require.NoError(t, err)
		defer func() { _ = root.Close() }()

		tmpl := &TemplateOps{
			Data:           map[string]any{},
			sourceRoot:     root,
			sourceRootPath: sourceDir,
		}
		err = tmpl.ExecuteTemplate(context.Background(), templatePath, outputPath)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "escapes source root")
		assert.NoFileExists(t, outputPath)
		assert.Equal(t, "secret", string(readConfinementFile(t, templatePath)))
	})

	t.Run("closed source root fails closed without writing outside", func(t *testing.T) {
		sourceDir := t.TempDir()
		outsideDir := t.TempDir()
		templatePath := filepath.Join(sourceDir, "config.tmpl")
		outputPath := filepath.Join(outsideDir, "rendered")
		require.NoError(t, os.WriteFile(templatePath, []byte("safe"), 0o644))

		root, err := fileutil.OpenPinnedRoot(sourceDir, false)
		require.NoError(t, err)
		require.NoError(t, root.Close())

		tmpl := &TemplateOps{Data: map[string]any{}, sourceRoot: root, sourceRootPath: sourceDir}
		err = tmpl.ExecuteTemplate(context.Background(), templatePath, outputPath)

		require.Error(t, err)
		assert.NoFileExists(t, outputPath)
		assert.NoFileExists(t, filepath.Join(outsideDir, "config"))
	})

	t.Run("configured include root failure is returned before staging", func(t *testing.T) {
		root := t.TempDir()
		sourceDir := filepath.Join(root, "source")
		stagingDir := filepath.Join(root, "staging")
		includePath := filepath.Join(root, "include-file")
		require.NoError(t, os.MkdirAll(sourceDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "static.yml"), []byte("safe"), 0o644))
		require.NoError(t, os.WriteFile(includePath, []byte("not a directory"), 0o644))

		tmpl := NewTemplateOps(nil)
		tmpl.IncludeDir = includePath
		err := tmpl.RenderDirectory(context.Background(), sourceDir, stagingDir, ".")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "open template include root")
		assert.NoDirExists(t, stagingDir)
		assert.Equal(t, "not a directory", string(readConfinementFile(t, includePath)))
	})

	t.Run("include function rejects relative escape with a pinned root", func(t *testing.T) {
		includeDir := t.TempDir()
		outsideDir := t.TempDir()
		outsidePath := filepath.Join(outsideDir, "secret.txt")
		require.NoError(t, os.WriteFile(outsidePath, []byte("secret"), 0o600))

		root, err := fileutil.OpenPinnedRoot(includeDir, false)
		require.NoError(t, err)
		defer func() { _ = root.Close() }()

		ops := &TemplateOps{includeRoot: root, includeRootPath: includeDir}
		include := ops.templateFuncs()["include"].(func(string) (string, error))
		_, err = include(outsidePath)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "escapes the allowed include directory")
		assert.Equal(t, "secret", string(readConfinementFile(t, outsidePath)))
	})

	t.Run("missing include file fails without reading outside the allowlist", func(t *testing.T) {
		includeDir := t.TempDir()
		outsidePath := filepath.Join(t.TempDir(), "secret.txt")
		require.NoError(t, os.WriteFile(outsidePath, []byte("secret"), 0o600))

		include := TemplateFuncs(includeDir)["include"].(func(string) (string, error))
		_, err := include(filepath.Join(includeDir, "missing.txt"))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "include")
		assert.NoFileExists(t, filepath.Join(includeDir, "missing.txt"))
		assert.Equal(t, "secret", string(readConfinementFile(t, outsidePath)))
	})
}

func TestTemplateOps_RenderDirectoryConfinementErrors(t *testing.T) {
	t.Run("already cancelled leaves staging absent", func(t *testing.T) {
		root := t.TempDir()
		sourceDir := filepath.Join(root, "source")
		stagingDir := filepath.Join(root, "staging")
		require.NoError(t, os.MkdirAll(sourceDir, 0o755))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := NewTemplateOps(nil).RenderDirectory(ctx, sourceDir, stagingDir, ".")

		require.ErrorIs(t, err, context.Canceled)
		assert.NoDirExists(t, stagingDir)
	})

	t.Run("closed pinned source root reports its stat failure", func(t *testing.T) {
		sourceDir := t.TempDir()
		root, err := fileutil.OpenPinnedRoot(sourceDir, false)
		require.NoError(t, err)
		require.NoError(t, root.Close())

		err = copyNonTemplateFilesFromRoot(context.Background(), sourceDir, filepath.Join(t.TempDir(), "staging"), root)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to stat pinned source directory")
	})

	t.Run("cancelled pinned source walk stops before copying", func(t *testing.T) {
		root := t.TempDir()
		sourceDir := filepath.Join(root, "source")
		dstDir := filepath.Join(root, "staging")
		require.NoError(t, os.MkdirAll(sourceDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "static.yml"), []byte("safe"), 0o644))
		pinned, err := fileutil.OpenPinnedRoot(sourceDir, false)
		require.NoError(t, err)
		defer func() { _ = pinned.Close() }()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err = copyNonTemplateFilesFromRoot(ctx, sourceDir, dstDir, pinned)

		require.ErrorIs(t, err, context.Canceled)
		assert.NoDirExists(t, dstDir)
		assert.Equal(t, "safe", string(readConfinementFile(t, filepath.Join(sourceDir, "static.yml"))))
	})

	t.Run("copy destination failure does not create an outside file", func(t *testing.T) {
		root := t.TempDir()
		sourceDir := filepath.Join(root, "source")
		dstPath := filepath.Join(root, "staging")
		require.NoError(t, os.MkdirAll(sourceDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "static.yml"), []byte("safe"), 0o644))
		require.NoError(t, os.WriteFile(dstPath, []byte("destination sentinel"), 0o644))
		pinned, err := fileutil.OpenPinnedRoot(sourceDir, false)
		require.NoError(t, err)
		defer func() { _ = pinned.Close() }()

		err = copyNonTemplateFilesFromRoot(context.Background(), sourceDir, dstPath, pinned)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "create parent directories")
		assert.Equal(t, "destination sentinel", string(readConfinementFile(t, dstPath)))
	})

	t.Run("template replaced by symlink during walk is refused", func(t *testing.T) {
		root := t.TempDir()
		sourceDir := filepath.Join(root, "source")
		stagingDir := filepath.Join(root, "staging")
		templatePath := filepath.Join(sourceDir, "config.tmpl")
		outsidePath := filepath.Join(root, "outside-secret")
		require.NoError(t, os.MkdirAll(sourceDir, 0o755))
		require.NoError(t, os.WriteFile(templatePath, []byte("safe"), 0o644))
		require.NoError(t, os.WriteFile(outsidePath, []byte("outside secret"), 0o600))

		tmpl := NewTemplateOps(nil)
		tmpl.walkDirFn = func(_ string, walkFn fs.WalkDirFunc) error {
			info, err := os.Lstat(templatePath)
			require.NoError(t, err)
			require.NoError(t, os.Remove(templatePath))
			if err := os.Symlink(outsidePath, templatePath); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return walkFn(templatePath, fs.FileInfoToDirEntry(info), nil)
		}

		err := tmpl.RenderDirectory(context.Background(), sourceDir, stagingDir, ".")

		require.ErrorIs(t, err, fileutil.ErrSymlinkSkipped)
		assert.Contains(t, err.Error(), templatePath)
		assert.Contains(t, err.Error(), "replace the symlink with a regular file")
		assert.NoFileExists(t, filepath.Join(stagingDir, "config"))
		assert.Equal(t, "outside secret", string(readConfinementFile(t, outsidePath)))
	})
}

func readConfinementFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return content
}
