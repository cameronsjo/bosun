package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cameronsjo/bosun/internal/fileutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderTemplate_RejectsSymlinkedSource(t *testing.T) {
	templateDir := t.TempDir()
	outsideDir := t.TempDir()
	target := filepath.Join(outsideDir, "source.tmpl")
	templatePath := filepath.Join(templateDir, "source.tmpl")
	require.NoError(t, os.WriteFile(target, []byte("outside"), 0o644))
	if err := os.Symlink(target, templatePath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	err := renderTemplate(context.Background(), templatePath, map[string]any{}, "", templateDir)

	require.Error(t, err)
	assert.ErrorIs(t, err, fileutil.ErrSymlinkSkipped)
}

func TestRenderTemplate_RejectsSymlinkedSourceAncestor(t *testing.T) {
	rootDir := t.TempDir()
	realDir := filepath.Join(rootDir, "real")
	aliasDir := filepath.Join(rootDir, "alias")
	templatePath := filepath.Join(aliasDir, "source.tmpl")
	require.NoError(t, os.Mkdir(realDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(realDir, "source.tmpl"), []byte("outside"), 0o644))
	if err := os.Symlink(realDir, aliasDir); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	err := renderTemplate(context.Background(), templatePath, map[string]any{}, "", rootDir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "pin template parent")
}
