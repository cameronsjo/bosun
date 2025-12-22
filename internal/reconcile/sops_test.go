package reconcile

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSOPSOps(t *testing.T) {
	sops := NewSOPSOps()
	assert.NotNil(t, sops)
}

func TestSOPSOps_Decrypt(t *testing.T) {
	if _, err := exec.LookPath("sops"); err != nil {
		t.Skip("sops not installed")
	}

	t.Run("decrypt non-existent file", func(t *testing.T) {
		sops := NewSOPSOps()
		ctx := context.Background()

		_, err := sops.Decrypt(ctx, "/non/existent/file.yaml")
		assert.Error(t, err)
	})

	t.Run("decrypt non-sops file", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.yaml")

		// Create a plain YAML file (not SOPS encrypted)
		content := `key: value
nested:
  foo: bar
`
		require.NoError(t, os.WriteFile(testFile, []byte(content), 0644))

		sops := NewSOPSOps()
		ctx := context.Background()

		// SOPS will fail because this is not encrypted
		_, err := sops.Decrypt(ctx, testFile)
		assert.Error(t, err)
	})
}

func TestSOPSOps_DecryptToMap(t *testing.T) {
	if _, err := exec.LookPath("sops"); err != nil {
		t.Skip("sops not installed")
	}

	t.Run("non-existent file", func(t *testing.T) {
		sops := NewSOPSOps()
		ctx := context.Background()

		_, err := sops.DecryptToMap(ctx, "/non/existent/file.yaml")
		assert.Error(t, err)
	})
}

func TestSOPSOps_DecryptFiles(t *testing.T) {
	if _, err := exec.LookPath("sops"); err != nil {
		t.Skip("sops not installed")
	}

	t.Run("empty file list", func(t *testing.T) {
		sops := NewSOPSOps()
		ctx := context.Background()

		result, err := sops.DecryptFiles(ctx, []string{})
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("non-existent files", func(t *testing.T) {
		sops := NewSOPSOps()
		ctx := context.Background()

		_, err := sops.DecryptFiles(ctx, []string{"/non/existent/file1.yaml"})
		assert.Error(t, err)
	})
}

func TestSOPSOps_DecryptToJSON(t *testing.T) {
	if _, err := exec.LookPath("sops"); err != nil {
		t.Skip("sops not installed")
	}

	t.Run("empty file list returns empty object", func(t *testing.T) {
		sops := NewSOPSOps()
		ctx := context.Background()

		result, err := sops.DecryptToJSON(ctx, []string{})
		require.NoError(t, err)
		assert.Equal(t, "{}", string(result))
	})
}

func TestMergeMap(t *testing.T) {
	t.Run("simple merge", func(t *testing.T) {
		dst := map[string]any{
			"key1": "value1",
		}
		src := map[string]any{
			"key2": "value2",
		}

		mergeMap(dst, src)

		assert.Equal(t, "value1", dst["key1"])
		assert.Equal(t, "value2", dst["key2"])
	})

	t.Run("override value", func(t *testing.T) {
		dst := map[string]any{
			"key": "original",
		}
		src := map[string]any{
			"key": "updated",
		}

		mergeMap(dst, src)

		assert.Equal(t, "updated", dst["key"])
	})

	t.Run("nested merge", func(t *testing.T) {
		dst := map[string]any{
			"nested": map[string]any{
				"key1": "value1",
			},
		}
		src := map[string]any{
			"nested": map[string]any{
				"key2": "value2",
			},
		}

		mergeMap(dst, src)

		nested := dst["nested"].(map[string]any)
		assert.Equal(t, "value1", nested["key1"])
		assert.Equal(t, "value2", nested["key2"])
	})

	t.Run("nested override", func(t *testing.T) {
		dst := map[string]any{
			"nested": map[string]any{
				"key": "original",
			},
		}
		src := map[string]any{
			"nested": map[string]any{
				"key": "updated",
			},
		}

		mergeMap(dst, src)

		nested := dst["nested"].(map[string]any)
		assert.Equal(t, "updated", nested["key"])
	})

	t.Run("type mismatch replaces value", func(t *testing.T) {
		dst := map[string]any{
			"key": map[string]any{"nested": "value"},
		}
		src := map[string]any{
			"key": "string value",
		}

		mergeMap(dst, src)

		assert.Equal(t, "string value", dst["key"])
	})
}

func TestSOPSOps_CheckAgeKey(t *testing.T) {
	t.Run("key found via SOPS_AGE_KEY env var", func(t *testing.T) {
		// Save and restore env vars
		origKey := os.Getenv("SOPS_AGE_KEY")
		origKeyFile := os.Getenv("SOPS_AGE_KEY_FILE")
		defer func() {
			os.Setenv("SOPS_AGE_KEY", origKey)
			os.Setenv("SOPS_AGE_KEY_FILE", origKeyFile)
		}()

		os.Setenv("SOPS_AGE_KEY", "AGE-SECRET-KEY-TEST")
		os.Unsetenv("SOPS_AGE_KEY_FILE")

		sops := NewSOPSOps()
		err := sops.CheckAgeKey()
		require.NoError(t, err)
	})

	t.Run("key found via SOPS_AGE_KEY_FILE env var", func(t *testing.T) {
		// Save and restore env vars
		origKey := os.Getenv("SOPS_AGE_KEY")
		origKeyFile := os.Getenv("SOPS_AGE_KEY_FILE")
		defer func() {
			os.Setenv("SOPS_AGE_KEY", origKey)
			os.Setenv("SOPS_AGE_KEY_FILE", origKeyFile)
		}()

		// Create a temp key file
		tmpDir := t.TempDir()
		keyFile := filepath.Join(tmpDir, "key.txt")
		require.NoError(t, os.WriteFile(keyFile, []byte("AGE-SECRET-KEY-TEST"), 0600))

		os.Unsetenv("SOPS_AGE_KEY")
		os.Setenv("SOPS_AGE_KEY_FILE", keyFile)

		sops := NewSOPSOps()
		err := sops.CheckAgeKey()
		require.NoError(t, err)
	})

	t.Run("SOPS_AGE_KEY_FILE set but file does not exist", func(t *testing.T) {
		// Save and restore env vars
		origKey := os.Getenv("SOPS_AGE_KEY")
		origKeyFile := os.Getenv("SOPS_AGE_KEY_FILE")
		defer func() {
			os.Setenv("SOPS_AGE_KEY", origKey)
			os.Setenv("SOPS_AGE_KEY_FILE", origKeyFile)
		}()

		os.Unsetenv("SOPS_AGE_KEY")
		os.Setenv("SOPS_AGE_KEY_FILE", "/nonexistent/path/key.txt")

		sops := NewSOPSOps()
		err := sops.CheckAgeKey()
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrAgeKeyNotFound)
		assert.Contains(t, err.Error(), "file does not exist")
	})

	t.Run("key found in default location", func(t *testing.T) {
		// This test only runs if the default key file exists
		homeDir, err := os.UserHomeDir()
		require.NoError(t, err)

		defaultKeyPath := filepath.Join(homeDir, ".config", "sops", "age", "keys.txt")
		if _, err := os.Stat(defaultKeyPath); os.IsNotExist(err) {
			t.Skip("default age key file does not exist")
		}

		// Save and restore env vars
		origKey := os.Getenv("SOPS_AGE_KEY")
		origKeyFile := os.Getenv("SOPS_AGE_KEY_FILE")
		defer func() {
			os.Setenv("SOPS_AGE_KEY", origKey)
			os.Setenv("SOPS_AGE_KEY_FILE", origKeyFile)
		}()

		os.Unsetenv("SOPS_AGE_KEY")
		os.Unsetenv("SOPS_AGE_KEY_FILE")

		sops := NewSOPSOps()
		err = sops.CheckAgeKey()
		require.NoError(t, err)
	})

	t.Run("error when no key found", func(t *testing.T) {
		// Save and restore env vars
		origKey := os.Getenv("SOPS_AGE_KEY")
		origKeyFile := os.Getenv("SOPS_AGE_KEY_FILE")
		defer func() {
			os.Setenv("SOPS_AGE_KEY", origKey)
			os.Setenv("SOPS_AGE_KEY_FILE", origKeyFile)
		}()

		os.Unsetenv("SOPS_AGE_KEY")
		os.Unsetenv("SOPS_AGE_KEY_FILE")

		// Check if default key file exists - if so, skip this test
		homeDir, err := os.UserHomeDir()
		require.NoError(t, err)
		defaultKeyPath := filepath.Join(homeDir, ".config", "sops", "age", "keys.txt")
		if _, err := os.Stat(defaultKeyPath); err == nil {
			t.Skip("default age key file exists, cannot test 'no key found' scenario")
		}

		sops := NewSOPSOps()
		err = sops.CheckAgeKey()
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrAgeKeyNotFound)
		assert.Contains(t, err.Error(), "To fix:")
		assert.Contains(t, err.Error(), "age-keygen")
	})
}
