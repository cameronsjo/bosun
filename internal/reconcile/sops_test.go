package reconcile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSOPSOps(t *testing.T) {
	sops := NewSOPSOps()
	assert.NotNil(t, sops)
}

func TestValidateSOPSFile(t *testing.T) {
	validMetadata := `secret: ENC[AES256_GCM,data:c2VjcmV0,iv:a,tag:b,type:str]
sops:
  age:
    - recipient: age1example
      enc: encrypted-data-key
  lastmodified: "2026-08-22T16:00:00Z"
  mac: ENC[AES256_GCM,data:bWFj,iv:a,tag:b,type:str]
  version: 3.11.0
`

	tests := []struct {
		name        string
		content     string
		wantErr     bool
		wantContain string
	}{
		{name: "valid age metadata", content: validMetadata},
		{name: "valid unquoted timestamp", content: strings.Replace(validMetadata, `"2026-08-22T16:00:00Z"`, "2026-08-22T16:00:00Z", 1)},
		{
			name: "valid key groups metadata",
			content: `secret: ENC[AES256_GCM,data:c2VjcmV0,iv:a,tag:b,type:str]
sops:
  key_groups:
    - pgp:
        - fp: ABC123
          enc: encrypted-data-key
  lastmodified: "2026-08-22T16:00:00Z"
  mac: ENC[AES256_GCM,data:bWFj,iv:a,tag:b,type:str]
`,
		},
		{
			name:        "missing metadata",
			content:     "secret: plaintext\n",
			wantErr:     true,
			wantContain: "does not contain 'sops' metadata key",
		},
		{
			name:        "metadata is not a mapping",
			content:     "sops: see docs/sops-howto.md\n",
			wantErr:     true,
			wantContain: "expected a mapping",
		},
		{
			name:        "missing mac",
			content:     strings.Replace(validMetadata, "  mac: ENC[AES256_GCM,data:bWFj,iv:a,tag:b,type:str]\n", "", 1),
			wantErr:     true,
			wantContain: "missing non-empty 'mac'",
		},
		{
			name:        "missing lastmodified",
			content:     strings.Replace(validMetadata, "  lastmodified: \"2026-08-22T16:00:00Z\"\n", "", 1),
			wantErr:     true,
			wantContain: "missing non-empty 'lastmodified'",
		},
		{
			name:        "invalid lastmodified",
			content:     strings.Replace(validMetadata, "2026-08-22T16:00:00Z", "yesterday", 1),
			wantErr:     true,
			wantContain: "expected an RFC3339 timestamp",
		},
		{
			name:        "missing recipients",
			content:     strings.Replace(validMetadata, "  age:\n    - recipient: age1example\n      enc: encrypted-data-key\n", "", 1),
			wantErr:     true,
			wantContain: "no key recipient",
		},
		{
			name:        "empty recipient entry",
			content:     strings.Replace(validMetadata, "    - recipient: age1example\n      enc: encrypted-data-key", "    - {}", 1),
			wantErr:     true,
			wantContain: "no key recipient",
		},
		{
			name:        "recipient missing encrypted data key",
			content:     strings.Replace(validMetadata, "      enc: encrypted-data-key\n", "", 1),
			wantErr:     true,
			wantContain: "no key recipient",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "secrets.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.content), 0o600))

			err := ValidateSOPSFile(path)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrNotSOPSFile)
			assert.Contains(t, err.Error(), tt.wantContain)
		})
	}

	t.Run("missing file", func(t *testing.T) {
		err := ValidateSOPSFile(filepath.Join(t.TempDir(), "missing.yaml"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "SOPS file not found")
	})

	t.Run("invalid yaml", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "secrets.yaml")
		require.NoError(t, os.WriteFile(path, []byte("sops: [unterminated"), 0o600))
		err := ValidateSOPSFile(path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid YAML syntax")
	})
}

func TestHasSOPSRecipients(t *testing.T) {
	for _, key := range []string{"age", "pgp", "kms", "gcp_kms", "azure_kv", "hc_vault"} {
		t.Run(key, func(t *testing.T) {
			metadata := map[string]any{
				key: []any{map[string]any{"enc": "encrypted-data-key"}},
			}
			assert.True(t, hasSOPSRecipients(metadata))
		})
	}

	assert.True(t, hasSOPSRecipients(map[string]any{
		"key_groups": []any{
			map[string]any{"age": []any{map[string]any{"enc": "encrypted-data-key"}}},
		},
	}))
	assert.False(t, hasSOPSRecipients(map[string]any{"age": []any{map[string]any{}}}))
	assert.False(t, hasSOPSRecipients(map[string]any{"age": []any{map[string]any{"recipient": "age1example"}}}))
	assert.False(t, hasSOPSRecipients(map[string]any{"key_groups": "not-a-list"}))
}

func TestSOPSOps_Decrypt(t *testing.T) {
	// Note: sops binary is no longer required - we use go-sops library for in-process decryption

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
	// Note: sops binary is no longer required - we use go-sops library for in-process decryption

	t.Run("non-existent file", func(t *testing.T) {
		sops := NewSOPSOps()
		ctx := context.Background()

		_, err := sops.DecryptToMap(ctx, "/non/existent/file.yaml")
		assert.Error(t, err)
	})
}

func TestSOPSOps_DecryptFiles(t *testing.T) {
	// Note: sops binary is no longer required - we use go-sops library for in-process decryption

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
	// Note: sops binary is no longer required - we use go-sops library for in-process decryption

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
		t.Setenv("SOPS_AGE_KEY", "AGE-SECRET-KEY-TEST")
		t.Setenv("SOPS_AGE_KEY_FILE", "")

		sops := NewSOPSOps()
		err := sops.CheckAgeKey()
		require.NoError(t, err)
	})

	t.Run("key found via SOPS_AGE_KEY_FILE env var", func(t *testing.T) {
		// Create a temp key file
		tmpDir := t.TempDir()
		keyFile := filepath.Join(tmpDir, "key.txt")
		require.NoError(t, os.WriteFile(keyFile, []byte("AGE-SECRET-KEY-TEST"), 0600))

		t.Setenv("SOPS_AGE_KEY", "")
		t.Setenv("SOPS_AGE_KEY_FILE", keyFile)

		sops := NewSOPSOps()
		err := sops.CheckAgeKey()
		require.NoError(t, err)
	})

	t.Run("SOPS_AGE_KEY_FILE set but file does not exist", func(t *testing.T) {
		t.Setenv("SOPS_AGE_KEY", "")
		t.Setenv("SOPS_AGE_KEY_FILE", "/nonexistent/path/key.txt")

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

		t.Setenv("SOPS_AGE_KEY", "")
		t.Setenv("SOPS_AGE_KEY_FILE", "")

		sops := NewSOPSOps()
		err = sops.CheckAgeKey()
		require.NoError(t, err)
	})

	t.Run("error when no key found", func(t *testing.T) {
		t.Setenv("SOPS_AGE_KEY", "")
		t.Setenv("SOPS_AGE_KEY_FILE", "")

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

func TestSanitizeDecryptError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		contains string
		isNil    bool
	}{
		{
			name:  "nil error returns nil",
			err:   nil,
			isNil: true,
		},
		{
			name:     "could not find pattern passes through",
			err:      errors.New("could not find decryption key"),
			contains: "could not find decryption key",
		},
		{
			name:     "no key found pattern passes through",
			err:      errors.New("no key found in key ring"),
			contains: "no key found",
		},
		{
			name:     "failed to get pattern passes through",
			err:      errors.New("failed to get data key"),
			contains: "failed to get",
		},
		{
			name:     "cannot find pattern passes through",
			err:      errors.New("Cannot find the sops config"),
			contains: "Cannot find",
		},
		{
			name:     "key not found pattern passes through",
			err:      errors.New("key not found in keyring"),
			contains: "key not found",
		},
		{
			name:     "permission denied pattern passes through",
			err:      errors.New("Permission denied: unable to read key"),
			contains: "Permission denied",
		},
		{
			name:     "no such file pattern passes through",
			err:      errors.New("no such file or directory"),
			contains: "no such file",
		},
		{
			name:     "unknown error returns generic message",
			err:      errors.New("AGE-SECRET-KEY-1QQQQQQQQQQQQ was used to encrypt"),
			contains: "decryption failed - check age key configuration",
		},
		{
			name:     "long safe error is truncated",
			err:      errors.New("could not find " + strings.Repeat("x", 250)),
			contains: "... (truncated)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeDecryptError(tt.err)
			if tt.isNil {
				assert.NoError(t, result)
				return
			}
			require.Error(t, result)
			assert.Contains(t, result.Error(), tt.contains)
		})
	}
}
