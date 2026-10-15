//go:build darwin || linux

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadRenderTemplateSource_MissingParent(t *testing.T) {
	root := t.TempDir()
	templatePath := filepath.Join(root, "missing", "config.tmpl")

	_, err := readRenderTemplateSource(templatePath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "pin template parent")
}

func TestReadRenderTemplateSource_ReadFailure(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the Linux proc filesystem supplies a regular file whose read fails")
	}

	templatePath := fmt.Sprintf("/proc/%d/mem", os.Getpid())
	if _, err := os.Stat(templatePath); err != nil {
		t.Skipf("proc self memory is unavailable: %v", err)
	}

	_, err := readRenderTemplateSource(templatePath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "read template source")
}
