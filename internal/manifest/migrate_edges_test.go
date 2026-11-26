package manifest

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// assertOneVersionedDocument checks the Migration Output Validity requirement
// directly: the data is a single YAML document with a non-empty apiVersion and
// kind. A header prepended in front of a leading document marker produces two
// documents, so the second decode would not return io.EOF.
func assertOneVersionedDocument(t *testing.T, data []byte) {
	t.Helper()

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var first map[string]any
	require.NoError(t, decoder.Decode(&first), "the migrated content must parse")
	assert.NotEmpty(t, first["apiVersion"], "apiVersion")
	assert.NotEmpty(t, first["kind"], "kind")

	var second map[string]any
	assert.ErrorIs(t, decoder.Decode(&second), io.EOF, "the migrated content must be one document")
}

// TestMigrateToV1_EdgeCases pins the output for the inputs that used to be
// rewritten into a broken manifest: a leading document marker, a header with
// only one of its two keys, and a header key with an empty value.
func TestMigrateToV1_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     string
		wantKind string
	}{
		{
			name:     "leading document marker: the header follows the marker",
			input:    "---\nname: x\nprovisions: [a]\n",
			want:     "---\napiVersion: bosun.io/v1\nkind: Service\nname: x\nprovisions: [a]\n",
			wantKind: KindService,
		},
		{
			name:     "comment before the marker stays first",
			input:    "# managed by hand\n---\nname: x\nprovisions: [a]\n",
			want:     "# managed by hand\n---\napiVersion: bosun.io/v1\nkind: Service\nname: x\nprovisions: [a]\n",
			wantKind: KindService,
		},
		{
			name:     "CRLF line endings are kept",
			input:    "---\r\nname: x\r\nprovisions: [a]\r\n",
			want:     "---\r\napiVersion: bosun.io/v1\r\nkind: Service\r\nname: x\r\nprovisions: [a]\r\n",
			wantKind: KindService,
		},
		{
			name:     "apiVersion only: kind is added after it",
			input:    "apiVersion: bosun.io/v1\nname: x\nprovisions: [a]\n",
			want:     "apiVersion: bosun.io/v1\nkind: Service\nname: x\nprovisions: [a]\n",
			wantKind: KindService,
		},
		{
			name:     "kind only: apiVersion is added before it",
			input:    "kind: Service\nname: x\nprovisions: [a]\n",
			want:     "apiVersion: bosun.io/v1\nkind: Service\nname: x\nprovisions: [a]\n",
			wantKind: KindService,
		},
		{
			name:     "an existing kind is kept even when the content suggests another",
			input:    "kind: Provision\nname: x\nprovisions: [a]\n",
			want:     "apiVersion: bosun.io/v1\nkind: Provision\nname: x\nprovisions: [a]\n",
			wantKind: KindProvision,
		},
		{
			name:     "null kind is replaced in place",
			input:    "apiVersion: bosun.io/v1\nkind:\nname: x\nprovisions: [a]\n",
			want:     "apiVersion: bosun.io/v1\nkind: Service\nname: x\nprovisions: [a]\n",
			wantKind: KindService,
		},
		{
			name:     "empty-string kind is replaced in place",
			input:    "apiVersion: bosun.io/v1\nkind: \"\"\nname: x\nprovisions: [a]\n",
			want:     "apiVersion: bosun.io/v1\nkind: Service\nname: x\nprovisions: [a]\n",
			wantKind: KindService,
		},
		{
			name:     "empty apiVersion is replaced in place",
			input:    "apiVersion: \"\"\nkind: Service\nname: x\nprovisions: [a]\n",
			want:     "apiVersion: bosun.io/v1\nkind: Service\nname: x\nprovisions: [a]\n",
			wantKind: KindService,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, kind, err := MigrateToV1([]byte(tt.input))

			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
			assert.Equal(t, tt.wantKind, kind)
			assertOneVersionedDocument(t, got)
		})
	}
}

// TestMigrateToV1_RejectsWhatItCannotMigrate pins that an input that cannot be
// migrated into one versioned document is an error, never a rewritten file.
func TestMigrateToV1_RejectsWhatItCannotMigrate(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "several documents",
			input: "name: a\nprovisions: [x]\n---\nname: b\nprovisions: [y]\n",
		},
		{
			name:  "several documents behind a leading marker",
			input: "---\nname: a\nprovisions: [x]\n---\nname: b\nprovisions: [y]\n",
		},
		{
			name:  "flow-style top level cannot take a header line",
			input: "{name: x, provisions: [a]}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := MigrateToV1([]byte(tt.input))

			require.Error(t, err)
			assert.Nil(t, got)
		})
	}
}

// TestMigrateFile_EdgeCases pins what reaches the disk. Before the fix a
// half-versioned or marker-prefixed file was rewritten into a file that no
// longer parsed, and a multi-document file was rewritten without an error.
func TestMigrateFile_EdgeCases(t *testing.T) {
	t.Run("write mode completes a half-versioned file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "service.yml")
		require.NoError(t, os.WriteFile(path, []byte("apiVersion: bosun.io/v1\nname: x\nprovisions: [a]\n"), 0o644))

		result, err := MigrateFile(path, MigrateOptions{DryRun: false})

		require.NoError(t, err)
		assert.True(t, result.Migrated)
		data, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		assert.Equal(t, "apiVersion: bosun.io/v1\nkind: Service\nname: x\nprovisions: [a]\n", string(data))
		assertOneVersionedDocument(t, data)
	})

	t.Run("write mode keeps a leading marker", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "service.yml")
		require.NoError(t, os.WriteFile(path, []byte("---\nname: x\nprovisions: [a]\n"), 0o644))

		_, err := MigrateFile(path, MigrateOptions{DryRun: false})

		require.NoError(t, err)
		data, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		assertOneVersionedDocument(t, data)
	})

	for _, dryRun := range []bool{true, false} {
		name := "write mode"
		if dryRun {
			name = "dry run"
		}
		t.Run(name+" reports a multi-document file and leaves it unchanged", func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "service.yml")
			original := "name: a\nprovisions: [x]\n---\nname: b\nprovisions: [y]\n"
			require.NoError(t, os.WriteFile(path, []byte(original), 0o644))

			result, err := MigrateFile(path, MigrateOptions{DryRun: dryRun})

			require.Error(t, err)
			assert.Error(t, result.Error)
			assert.False(t, result.Migrated, "a dry run must not say the file would migrate")
			data, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			assert.Equal(t, original, string(data))
		})
	}
}

// TestMigrateDirectory_ContinuesPastABadFile pins that one file that cannot be
// migrated is reported without stopping the others.
func TestMigrateDirectory_ContinuesPastABadFile(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "a-bad.yml")
	good := filepath.Join(dir, "b-good.yml")
	require.NoError(t, os.WriteFile(bad, []byte("name: a\nprovisions: [x]\n---\nname: b\nprovisions: [y]\n"), 0o644))
	require.NoError(t, os.WriteFile(good, []byte("name: g\nprovisions: [a]\n"), 0o644))

	results, err := MigrateDirectory([]string{dir}, MigrateOptions{DryRun: false})

	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, bad, results[0].Path)
	assert.Error(t, results[0].Error)
	assert.Equal(t, good, results[1].Path)
	assert.NoError(t, results[1].Error)
	assert.True(t, results[1].Migrated)
}
