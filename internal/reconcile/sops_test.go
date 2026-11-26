package reconcile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sopslib "github.com/getsops/sops/v3"
	sopsaes "github.com/getsops/sops/v3/aes"
	sopsage "github.com/getsops/sops/v3/age"
	sopsconfig "github.com/getsops/sops/v3/config"
	sopsyaml "github.com/getsops/sops/v3/stores/yaml"
	"github.com/getsops/sops/v3/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/log"
)

const (
	testAgeRecipient = "age1lzd99uklcjnc0e7d860axevet2cz99ce9pq6tzuzd05l5nr28ams36nvun"
	testAgeIdentity  = "AGE-SECRET-KEY-1G0Q5K9TV4REQ3ZSQRMTMG8NSWQGYT0T7TZ33RAZEE0GZYVZN0APSU24RK7"
)

func TestNewSOPSOps(t *testing.T) {
	sops := NewSOPSOps()
	assert.NotNil(t, sops)
	assert.NotNil(t, sops.decryptFile)
}

func TestInferSOPSFormat(t *testing.T) {
	tests := []struct {
		path       string
		want       sopsFileFormat
		wantErr    bool
		wantBinary bool
	}{
		{path: "secrets.sops.yaml", want: sopsFormatYAML},
		{path: "secrets.yml", want: sopsFormatYAML},
		{path: "secrets.JSON", want: sopsFormatJSON},
		{path: "secrets.sops.env", want: sopsFormatDotenv},
		{path: "secrets.ini.sops", want: sopsFormatINI},
		{path: "secrets.yaml.sops", want: sopsFormatYAML},
		{path: "secrets.bin", wantErr: true, wantBinary: true},
		{path: "secrets.binary", wantErr: true, wantBinary: true},
		{path: "secrets", wantErr: true},
		{path: "secrets.txt", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, err := inferSOPSFormat(tt.path)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrUnsupportedSOPSFormat)
				assert.Contains(t, err.Error(), "supported extensions")
				if tt.wantBinary {
					assert.Contains(t, err.Error(), "binary SOPS files")
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSOPSOps_DecryptSupportedFormats(t *testing.T) {
	t.Setenv("SOPS_AGE_KEY", testAgeIdentity)

	yamlMetadata := `secret: ENC[AES256_GCM,data:c2VjcmV0,iv:a,tag:b,type:str]
sops:
  age:
    - recipient: age1example
      enc: encrypted-data-key
  lastmodified: "2026-08-22T16:00:00Z"
  mac: ENC[AES256_GCM,data:bWFj,iv:a,tag:b,type:str]
  version: 3.13.3
`
	jsonMetadata := `{"secret":"ENC[AES256_GCM,data:c2VjcmV0,iv:a,tag:b,type:str]","sops":{"age":[{"recipient":"age1example","enc":"encrypted-data-key"}],"lastmodified":"2026-08-22T16:00:00Z","mac":"ENC[AES256_GCM,data:bWFj,iv:a,tag:b,type:str]","version":"3.13.3"}}`
	dotenvMetadata := `SECRET=ENC[AES256_GCM,data:c2VjcmV0,iv:a,tag:b,type:str]
sops_age__list_0__map_recipient=age1example
sops_age__list_0__map_enc=encrypted-data-key
sops_lastmodified=2026-08-22T16:00:00Z
sops_mac=ENC[AES256_GCM,data:bWFj,iv:a,tag:b,type:str]
sops_version=3.13.3
`
	iniMetadata := `[secrets]
TOKEN=ENC[AES256_GCM,data:c2VjcmV0,iv:a,tag:b,type:str]

[sops]
age__list_0__map_recipient=age1example
age__list_0__map_enc=encrypted-data-key
lastmodified=2026-08-22T16:00:00Z
mac=ENC[AES256_GCM,data:bWFj,iv:a,tag:b,type:str]
version=3.13.3
`

	tests := []struct {
		name      string
		suffix    string
		encrypted string
		plaintext string
		want      map[string]any
		format    sopsFileFormat
	}{
		{name: "yaml", suffix: ".sops.yaml", encrypted: yamlMetadata, plaintext: "secret: value\n", want: map[string]any{"secret": "value"}, format: sopsFormatYAML},
		{name: "yml", suffix: ".yml", encrypted: yamlMetadata, plaintext: "secret: value\n", want: map[string]any{"secret": "value"}, format: sopsFormatYAML},
		{name: "json", suffix: ".sops.json", encrypted: jsonMetadata, plaintext: `{"secret":"value"}`, want: map[string]any{"secret": "value"}, format: sopsFormatJSON},
		{name: "dotenv", suffix: ".sops.env", encrypted: dotenvMetadata, plaintext: "TOKEN=value\nPORT=1234\n", want: map[string]any{"TOKEN": "value", "PORT": "1234"}, format: sopsFormatDotenv},
		{name: "ini", suffix: ".sops.ini", encrypted: iniMetadata, plaintext: "[database]\nuser=admin\nport=5432\n", want: map[string]any{"database": map[string]any{"user": "admin", "port": "5432"}}, format: sopsFormatINI},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "secrets"+tt.suffix)
			require.NoError(t, os.WriteFile(path, []byte(tt.encrypted), 0o600))

			var gotFormat string
			sops := &SOPSOps{decryptFile: func(_ string, format string) ([]byte, error) {
				gotFormat = format
				return []byte(tt.plaintext), nil
			}}
			decrypted, err := sops.Decrypt(context.Background(), path)
			require.NoError(t, err)
			assert.Equal(t, string(tt.format), gotFormat)

			var got map[string]any
			require.NoError(t, json.Unmarshal(decrypted, &got))
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("zero value uses the production decryptor", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "secrets.yaml")
		require.NoError(t, os.WriteFile(path, []byte(yamlMetadata), 0o600))
		_, err := (&SOPSOps{}).Decrypt(context.Background(), path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "sops decrypt failed")
	})

	t.Run("decrypt errors remain sanitized", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "secrets.yaml")
		require.NoError(t, os.WriteFile(path, []byte(yamlMetadata), 0o600))
		sops := &SOPSOps{decryptFile: func(string, string) ([]byte, error) {
			return nil, errors.New("sensitive implementation detail")
		}}
		_, err := sops.Decrypt(context.Background(), path)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "sensitive implementation detail")
		assert.ErrorIs(t, err, ErrSOPSDecryption)
	})

	t.Run("invalid decrypted data names its format", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "secrets.json")
		require.NoError(t, os.WriteFile(path, []byte(jsonMetadata), 0o600))
		sops := &SOPSOps{decryptFile: func(string, string) ([]byte, error) {
			return []byte("{"), nil
		}}
		_, err := sops.Decrypt(context.Background(), path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse decrypted json")
	})

	t.Run("invalid decrypted flat data does not leak plaintext", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "secrets.env")
		require.NoError(t, os.WriteFile(path, []byte(dotenvMetadata), 0o600))
		sops := &SOPSOps{decryptFile: func(string, string) ([]byte, error) {
			return []byte("TOP_SECRET_VALUE_WITHOUT_EQUALS"), nil
		}}
		_, err := sops.Decrypt(context.Background(), path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse decrypted dotenv")
		assert.NotContains(t, err.Error(), "TOP_SECRET_VALUE_WITHOUT_EQUALS")
	})
}

func TestSOPSOps_DecryptTamperedFileReportsIntegrityWithoutLeaks(t *testing.T) {
	path, encrypted := writeTamperedSOPSFixture(t)
	t.Setenv("SOPS_AGE_KEY", testAgeIdentity)
	t.Setenv("SOPS_AGE_KEY_FILE", "")

	// Prove the fixture reaches the real go-sops MAC verification path rather
	// than relying on a synthetic error string.
	_, upstreamErr := NewSOPSOps().decryptFile(path, string(sopsFormatYAML))
	require.Error(t, upstreamErr)
	assert.Contains(t, strings.ToLower(upstreamErr.Error()), "integrity")
	assert.Contains(t, strings.ToLower(upstreamErr.Error()), "expected mac")

	_, err := NewSOPSOps().Decrypt(context.Background(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSOPSIntegrity)
	assert.Contains(t, err.Error(), "restore it from a trusted source or re-encrypt it")

	for _, sensitive := range []string{
		"TOP_SECRET_PLAINTEXT",
		"expected mac",
		"got ",
		testAgeIdentity,
		testAgeRecipient,
		firstEncryptedValue(t, encrypted),
	} {
		assert.NotContains(t, err.Error(), sensitive)
	}
}

func TestSOPSOps_DecryptDebugLogUsesSanitizedCategory(t *testing.T) {
	path, _ := writeTamperedSOPSFixture(t)
	t.Setenv("SOPS_AGE_KEY", testAgeIdentity)
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	rawErr := errors.New(`Failed to verify data integrity. expected mac "DECRYPTED_MAC_SECRET", got "COMPUTED_MAC_SECRET" at /private/key/path`)
	sops := &SOPSOps{decryptFile: func(string, string) ([]byte, error) {
		return nil, rawErr
	}}

	var decryptErr error
	logs := captureSOPSDebugLogs(t, func() {
		_, decryptErr = sops.Decrypt(context.Background(), path)
	})

	require.Error(t, decryptErr)
	assert.ErrorIs(t, decryptErr, ErrSOPSIntegrity)
	assert.Contains(t, logs, "SOPS decryption failed")
	assert.Contains(t, logs, ErrSOPSIntegrity.Error())
	assert.Contains(t, logs, path)
	for _, sensitive := range []string{"DECRYPTED_MAC_SECRET", "COMPUTED_MAC_SECRET", "expected mac", "/private/key/path"} {
		assert.NotContains(t, logs, sensitive)
	}
}

func captureSOPSDebugLogs(t *testing.T, fn func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	originalStdout := os.Stdout
	restored := false
	t.Cleanup(func() {
		if restored {
			return
		}
		_ = writer.Close()
		os.Stdout = originalStdout
		log.Init(nil)
		_ = reader.Close()
	})
	os.Stdout = writer
	log.Init(&log.Options{Format: log.FormatJSON, Level: log.DebugLevel, LevelSet: true})

	fn()

	closeErr := writer.Close()
	os.Stdout = originalStdout
	log.Init(nil)
	restored = true
	require.NoError(t, closeErr)
	output, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	return string(output)
}

func writeTamperedSOPSFixture(t *testing.T) (string, []byte) {
	t.Helper()

	masterKey, err := sopsage.MasterKeyFromRecipient(testAgeRecipient)
	require.NoError(t, err)
	lastModified := time.Date(2026, time.August, 23, 0, 0, 0, 0, time.UTC)
	tree := sopslib.Tree{
		Branches: sopslib.TreeBranches{{
			{Key: "password", Value: "TOP_SECRET_PLAINTEXT"},
			{Key: "marker_unencrypted", Value: "original"},
		}},
		Metadata: sopslib.Metadata{
			KeyGroups:         []sopslib.KeyGroup{{masterKey}},
			LastModified:      lastModified,
			UnencryptedSuffix: "_unencrypted",
			Version:           version.Version,
		},
	}

	dataKey := bytes.Repeat([]byte{0x42}, 32)
	require.NoError(t, masterKey.Encrypt(dataKey))
	cipher := sopsaes.NewCipher()
	mac, err := tree.Encrypt(dataKey, cipher)
	require.NoError(t, err)
	tree.Metadata.MessageAuthenticationCode, err = cipher.Encrypt(mac, dataKey, lastModified.Format(time.RFC3339))
	require.NoError(t, err)

	storesConfig := sopsconfig.NewStoresConfig()
	encrypted, err := sopsyaml.NewStore(&storesConfig.YAML).EmitEncryptedFile(tree)
	require.NoError(t, err)
	require.NotContains(t, string(encrypted), "TOP_SECRET_PLAINTEXT")

	tampered := bytes.Replace(encrypted, []byte("marker_unencrypted: original"), []byte("marker_unencrypted: tampered"), 1)
	require.NotEqual(t, encrypted, tampered, "fixture mutation must change authenticated data")
	path := filepath.Join(t.TempDir(), "secrets.sops.yaml")
	require.NoError(t, os.WriteFile(path, tampered, 0o600))
	return path, encrypted
}

func firstEncryptedValue(t *testing.T, encrypted []byte) string {
	t.Helper()
	start := bytes.Index(encrypted, []byte("ENC[AES256_GCM"))
	require.NotEqual(t, -1, start)
	end := bytes.IndexByte(encrypted[start:], '\n')
	require.NotEqual(t, -1, end)
	return string(encrypted[start : start+end])
}

func TestDecodeSOPSPlaintext_Errors(t *testing.T) {
	tests := []struct {
		name   string
		format sopsFileFormat
		input  string
	}{
		{name: "yaml", format: sopsFormatYAML, input: "[unterminated"},
		{name: "json", format: sopsFormatJSON, input: "{"},
		{name: "dotenv", format: sopsFormatDotenv, input: "not-an-assignment"},
		{name: "ini", format: sopsFormatINI, input: "[unterminated"},
		{name: "unsupported", format: sopsFileFormat("binary"), input: "data"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeSOPSPlaintext([]byte(tt.input), tt.format)
			require.Error(t, err)
		})
	}
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

	t.Run("unsupported extension", func(t *testing.T) {
		err := ValidateSOPSFile(filepath.Join(t.TempDir(), "secrets.bin"))
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrUnsupportedSOPSFormat)
	})
}

func TestValidateSOPSFile_FlatFormats(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		content     string
		wantContain string
		wantNotSOPS bool
	}{
		{name: "invalid dotenv syntax", filename: "secrets.env", content: "not-an-assignment", wantContain: "invalid dotenv syntax"},
		{name: "dotenv without metadata", filename: "secrets.env", content: "TOKEN=value\n", wantContain: "does not contain 'sops' metadata", wantNotSOPS: true},
		{name: "invalid ini syntax", filename: "secrets.ini", content: "[unterminated", wantContain: "invalid ini syntax"},
		{name: "ini without metadata", filename: "secrets.ini", content: "[secrets]\nTOKEN=value\n", wantContain: "does not contain 'sops' metadata", wantNotSOPS: true},
		{
			name:     "dotenv without mac",
			filename: "secrets.env",
			content: "sops_age__list_0__map_recipient=age1example\n" +
				"sops_age__list_0__map_enc=encrypted-data-key\n" +
				"sops_lastmodified=2026-08-22T16:00:00Z\n",
			wantContain: "missing non-empty 'mac'",
			wantNotSOPS: true,
		},
		{
			name:     "dotenv without recipient",
			filename: "secrets.env",
			content: "sops_lastmodified=2026-08-22T16:00:00Z\n" +
				"sops_mac=ENC[AES256_GCM,data:bWFj,iv:a,tag:b,type:str]\n",
			wantContain: "invalid dotenv SOPS metadata",
			wantNotSOPS: true,
		},
		{
			name:     "dotenv recipient without encrypted data key",
			filename: "secrets.env",
			content: "sops_age__list_0__map_recipient=age1example\n" +
				"sops_lastmodified=2026-08-22T16:00:00Z\n" +
				"sops_mac=ENC[AES256_GCM,data:bWFj,iv:a,tag:b,type:str]\n",
			wantContain: "no key recipient",
			wantNotSOPS: true,
		},
		{
			name:     "dotenv with invalid lastmodified",
			filename: "secrets.env",
			content: "sops_age__list_0__map_recipient=age1example\n" +
				"sops_age__list_0__map_enc=encrypted-data-key\n" +
				"sops_lastmodified=yesterday\n" +
				"sops_mac=ENC[AES256_GCM,data:bWFj,iv:a,tag:b,type:str]\n",
			wantContain: "invalid dotenv SOPS metadata",
			wantNotSOPS: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tt.filename)
			require.NoError(t, os.WriteFile(path, []byte(tt.content), 0o600))
			err := ValidateSOPSFile(path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantContain)
			if tt.wantNotSOPS {
				assert.ErrorIs(t, err, ErrNotSOPSFile)
			}
		})
	}
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

	t.Run("unsupported format is reported before key discovery", func(t *testing.T) {
		t.Setenv("SOPS_AGE_KEY", "")
		t.Setenv("SOPS_AGE_KEY_FILE", "")

		_, err := (&SOPSOps{}).DecryptToMap(context.Background(), "secrets.bin")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrUnsupportedSOPSFormat)
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
	const testAgeIdentity = "AGE-SECRET-KEY-184JMZMVQH3E6U0PSL869004Y3U2NYV7R30EU99CSEDNPH02YUVFSZW44VU"

	t.Run("SOPS_AGE_KEY takes precedence over invalid key file", func(t *testing.T) {
		t.Setenv("SOPS_AGE_KEY", testAgeIdentity)
		t.Setenv("SOPS_AGE_KEY_FILE", t.TempDir())
		t.Setenv("HOME", t.TempDir())

		sops := NewSOPSOps()
		err := sops.CheckAgeKey()
		require.NoError(t, err)
	})

	t.Run("valid SOPS_AGE_KEY_FILE is accepted", func(t *testing.T) {
		keyFile := filepath.Join(t.TempDir(), "key.txt")
		require.NoError(t, os.WriteFile(keyFile, []byte(testAgeIdentity+"\n"), 0600))

		t.Setenv("SOPS_AGE_KEY", "")
		t.Setenv("SOPS_AGE_KEY_FILE", keyFile)
		t.Setenv("HOME", t.TempDir())

		sops := NewSOPSOps()
		err := sops.CheckAgeKey()
		require.NoError(t, err)
	})

	invalidCases := []struct {
		name       string
		prepare    func(t *testing.T, path string)
		wantReason string
	}{
		{name: "missing", wantReason: "does not exist"},
		{name: "directory", prepare: func(t *testing.T, path string) {
			require.NoError(t, os.Mkdir(path, 0700))
		}, wantReason: "not a regular file"},
		{name: "empty", prepare: func(t *testing.T, path string) {
			require.NoError(t, os.WriteFile(path, nil, 0600))
		}, wantReason: "is empty"},
		{name: "unparseable", prepare: func(t *testing.T, path string) {
			require.NoError(t, os.WriteFile(path, []byte("not an Age identity"), 0600))
		}, wantReason: "parseable Age identity"},
	}

	for _, tt := range invalidCases {
		t.Run("invalid SOPS_AGE_KEY_FILE "+tt.name, func(t *testing.T) {
			keyFile := filepath.Join(t.TempDir(), "key.txt")
			if tt.prepare != nil {
				tt.prepare(t, keyFile)
			}
			t.Setenv("SOPS_AGE_KEY", "")
			t.Setenv("SOPS_AGE_KEY_FILE", keyFile)
			t.Setenv("HOME", t.TempDir())

			err := NewSOPSOps().CheckAgeKey()
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrAgeKeyNotFound)
			assert.Contains(t, err.Error(), "SOPS_AGE_KEY_FILE")
			assert.Contains(t, err.Error(), keyFile)
			assert.Contains(t, err.Error(), tt.wantReason)
			assert.Contains(t, err.Error(), "Docker")
		})
	}

	t.Run("valid default identity file is accepted", func(t *testing.T) {
		homeDir := t.TempDir()
		defaultKeyPath := filepath.Join(homeDir, ".config", "sops", "age", "keys.txt")
		require.NoError(t, os.MkdirAll(filepath.Dir(defaultKeyPath), 0700))
		require.NoError(t, os.WriteFile(defaultKeyPath, []byte(testAgeIdentity+"\n"), 0600))

		t.Setenv("SOPS_AGE_KEY", "")
		t.Setenv("SOPS_AGE_KEY_FILE", "")
		t.Setenv("HOME", homeDir)

		require.NoError(t, NewSOPSOps().CheckAgeKey())
	})

	t.Run("error when no key found", func(t *testing.T) {
		t.Setenv("SOPS_AGE_KEY", "")
		t.Setenv("SOPS_AGE_KEY_FILE", "")
		t.Setenv("HOME", t.TempDir())

		sops := NewSOPSOps()
		err := sops.CheckAgeKey()
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrAgeKeyNotFound)
		assert.Contains(t, err.Error(), "To fix:")
		assert.Contains(t, err.Error(), "age-keygen")
	})
}

// TestSOPSOps_CheckAgeKeyInlineValue pins how CheckAgeKey judges the content
// of SOPS_AGE_KEY. The rules mirror the SOPS age key source: raw lines that are
// empty or start with "#" are skipped, every other line is split on
// whitespace, and every word must parse as exactly one identity. A value that
// yields no identity and no parse error is treated as unset, so the file
// sources are still consulted, as SOPS does. The value itself must never
// appear in the returned error or in any log line.
func TestSOPSOps_CheckAgeKeyInlineValue(t *testing.T) {
	const (
		firstIdentity  = "AGE-SECRET-KEY-184JMZMVQH3E6U0PSL869004Y3U2NYV7R30EU99CSEDNPH02YUVFSZW44VU"
		secondIdentity = "AGE-SECRET-KEY-1G0Q5K9TV4REQ3ZSQRMTMG8NSWQGYT0T7TZ33RAZEE0GZYVZN0APSU24RK7"
		// firstIdentity with its final character changed, so the bech32
		// checksum fails.
		badChecksumIdentity = "AGE-SECRET-KEY-184JMZMVQH3E6U0PSL869004Y3U2NYV7R30EU99CSEDNPH02YUVFSZW44VQ"
		inlineSentinel      = "not-a-key-SENTINEL-7f3a"
		armorBodySentinel   = "ARMORBODYSENTINEL"
		binaryBodySentinel  = "BINARYBODYSENTINEL"
		inlineErrorText     = "SOPS_AGE_KEY does not contain a parseable Age identity"
	)

	tests := []struct {
		name             string
		value            string
		withValidKeyFile bool
		wantErr          bool
		wantInlineError  bool
		wantLog          string
		mustNotEcho      []string
	}{
		{name: "single valid key", value: firstIdentity, mustNotEcho: []string{firstIdentity}},
		{name: "two keys separated by a space on one line", value: firstIdentity + " " + secondIdentity, mustNotEcho: []string{firstIdentity, secondIdentity}},
		{name: "comment line then key", value: "# note\n" + firstIdentity, mustNotEcho: []string{firstIdentity}},
		{name: "key followed by a hash word on the same line", value: firstIdentity + " #c", wantErr: true, wantInlineError: true, mustNotEcho: []string{firstIdentity}},
		{name: "unknown identity type", value: inlineSentinel, wantErr: true, wantInlineError: true, mustNotEcho: []string{inlineSentinel}},
		{name: "bad checksum", value: badChecksumIdentity, wantErr: true, wantInlineError: true, mustNotEcho: []string{badChecksumIdentity}},
		{
			name:            "leading space before armored header",
			value:           " -----BEGIN AGE ENCRYPTED FILE-----\n" + armorBodySentinel + "\n-----END AGE ENCRYPTED FILE-----",
			wantErr:         true,
			wantInlineError: true,
			mustNotEcho:     []string{armorBodySentinel},
		},
		{
			name:        "armored encrypted key is accepted with a warning",
			value:       "-----BEGIN AGE ENCRYPTED FILE-----\n" + armorBodySentinel + "\n-----END AGE ENCRYPTED FILE-----\n",
			wantLog:     `"key_format":"armored"`,
			mustNotEcho: []string{armorBodySentinel},
		},
		{
			name:        "binary encrypted key is accepted with a warning",
			value:       "age-encryption.org/v1\n-> X25519 " + binaryBodySentinel + "\n",
			wantLog:     `"key_format":"age-encryption"`,
			mustNotEcho: []string{binaryBodySentinel},
		},
		{name: "whitespace only falls through to a valid key file", value: "   ", withValidKeyFile: true},
		{name: "whitespace only with no other source", value: "   ", wantErr: true},
		{name: "comment only falls through to a valid key file", value: "# note", withValidKeyFile: true},
		{name: "comment lines only with no other source", value: "\n#x\n", wantErr: true},
		{name: "invalid value next to a valid key file", value: inlineSentinel, withValidKeyFile: true, wantErr: true, wantInlineError: true, mustNotEcho: []string{inlineSentinel}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("SOPS_AGE_KEY_FILE", "")
			if tt.withValidKeyFile {
				keyFile := filepath.Join(t.TempDir(), "key.txt")
				require.NoError(t, os.WriteFile(keyFile, []byte(secondIdentity+"\n"), 0o600))
				t.Setenv("SOPS_AGE_KEY_FILE", keyFile)
			}
			t.Setenv("SOPS_AGE_KEY", tt.value)

			var err error
			logs := captureSOPSDebugLogs(t, func() {
				err = NewSOPSOps().CheckAgeKey()
			})

			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrAgeKeyNotFound)
				if tt.wantInlineError {
					assert.Contains(t, err.Error(), inlineErrorText, "the error must name SOPS_AGE_KEY as the failing source")
				} else {
					assert.NotContains(t, err.Error(), inlineErrorText, "an empty inline value must fall through, not fail as SOPS_AGE_KEY")
				}
			} else {
				require.NoError(t, err)
			}
			if tt.wantLog != "" {
				assert.Contains(t, logs, tt.wantLog)
			}
			for _, secret := range tt.mustNotEcho {
				if err != nil {
					assert.NotContains(t, err.Error(), secret, "the error must not echo SOPS_AGE_KEY content")
				}
				assert.NotContains(t, logs, secret, "no log line may echo SOPS_AGE_KEY content")
			}
		})
	}
}

// TestClassifyInlineAgeKey_OverlongLineIsInvalid pins the one deliberate
// difference from SOPS: a line longer than the scanner's 64 KiB token limit is
// reported invalid instead of being silently cut short. The line is a comment,
// so without the scanner error check it would read as an empty value.
func TestClassifyInlineAgeKey_OverlongLineIsInvalid(t *testing.T) {
	value := "#" + strings.Repeat("A", 70*1024)
	assert.Equal(t, InlineAgeKeyInvalid, ClassifyInlineAgeKey(value))
}

func TestSOPSOps_CheckAgeKeyInjectedReaderErrors(t *testing.T) {
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", "/config/age-key.txt")

	for _, tt := range []struct {
		name       string
		readErr    error
		wantReason string
	}{
		{name: "inspect error", readErr: errors.New("cannot be inspected: permission denied"), wantReason: "cannot be inspected"},
		{name: "read error", readErr: errors.New("cannot be read: permission denied"), wantReason: "cannot be read"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sops := NewSOPSOps()
			sops.identityFileReader = func(path string) ([]byte, error) {
				assert.Equal(t, "/config/age-key.txt", path)
				return nil, tt.readErr
			}
			err := sops.CheckAgeKey()
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrAgeKeyNotFound)
			assert.Contains(t, err.Error(), tt.wantReason)
		})
	}
}

func TestValidateAgeIdentityForSecretsIsConditional(t *testing.T) {
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", filepath.Join(t.TempDir(), "missing-age-key"))
	t.Setenv("HOME", t.TempDir())

	require.NoError(t, ValidateAgeIdentityForSecrets(nil))
	require.NoError(t, ValidateAgeIdentityForSecrets([]string{"", "  "}))

	err := ValidateAgeIdentityForSecrets([]string{"secrets/prod.sops.yaml"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAgeKeyNotFound)
	assert.Contains(t, err.Error(), "SOPS_AGE_KEY_FILE")
}

func TestSanitizeDecryptError(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		want        error
		contains    string
		notContains []string
		isNil       bool
	}{
		{
			name:  "nil error returns nil",
			err:   nil,
			isNil: true,
		},
		{
			name:        "MAC mismatch is an integrity error",
			err:         errors.New(`Failed to verify data integrity. expected mac "DECRYPTED_MAC_SECRET", got "COMPUTED_MAC_SECRET"`),
			want:        ErrSOPSIntegrity,
			contains:    "corrupted or modified",
			notContains: []string{"DECRYPTED_MAC_SECRET", "COMPUTED_MAC_SECRET", "expected mac"},
		},
		{
			name:        "encrypted MAC authentication failure is an integrity error",
			err:         errors.New("Failed to decrypt original mac: Could not decrypt with AES_GCM: cipher: message authentication failed"),
			want:        ErrSOPSIntegrity,
			contains:    "restore it from a trusted source",
			notContains: []string{"AES_GCM", "message authentication failed"},
		},
		{
			name:        "data key recovery failure is a key error",
			err:         errors.New("Failed to get the data key required to decrypt the SOPS file: no identity matched AGE-SECRET-KEY-PRIVATE at /private/keys.txt"),
			want:        ErrSOPSKeyUnavailable,
			contains:    "configured Age identity matches",
			notContains: []string{"AGE-SECRET-KEY-PRIVATE", "/private/keys.txt", "no identity matched"},
		},
		{
			name:        "key file access failure is a key error",
			err:         errors.New("failed to load age identities: permission denied: /Users/operator/.config/sops/age/keys.txt"),
			want:        ErrSOPSKeyUnavailable,
			contains:    "key file is readable",
			notContains: []string{"permission denied", "/Users/operator"},
		},
		{
			name:        "malformed encrypted value is a format error",
			err:         errors.New("Error walking tree: Input string ENC[AES256_GCM,data:CIPHERTEXT_SECRET] does not match sops' data format"),
			want:        ErrMalformedSOPSData,
			contains:    "encrypted values or metadata are invalid",
			notContains: []string{"CIPHERTEXT_SECRET", "Input string"},
		},
		{
			name:        "unknown error is generic",
			err:         errors.New("AGE-SECRET-KEY-PRIVATE decrypted TOP_SECRET_VALUE from ENC[CIPHERTEXT_SECRET] at /private/repo/secrets.yaml"),
			want:        ErrSOPSDecryption,
			contains:    "validate the encrypted file with SOPS",
			notContains: []string{"AGE-SECRET-KEY-PRIVATE", "TOP_SECRET_VALUE", "CIPHERTEXT_SECRET", "/private/repo"},
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
			assert.ErrorIs(t, result, tt.want)
			assert.Contains(t, result.Error(), tt.contains)
			for _, value := range tt.notContains {
				assert.NotContains(t, result.Error(), value)
			}
		})
	}
}
