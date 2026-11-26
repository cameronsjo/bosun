package manifest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVerifyMigration pins the guard that stands between MigrateToV1 and a file
// write: a result that is not one versioned document with the same body is
// rejected. The header placement never produces these results, so they are fed
// in directly.
func TestVerifyMigration(t *testing.T) {
	const valid = "apiVersion: bosun.io/v1\nkind: Service\nname: x\n"

	tests := []struct {
		name     string
		original string
		migrated string
		wantErr  string
	}{
		{
			name:     "a header and the same body pass",
			original: "name: x\n",
			migrated: valid,
		},
		{
			name:     "a migrated result with two documents",
			original: "name: x\n",
			migrated: valid + "---\nname: y\n",
			wantErr:  "not one document",
		},
		{
			name:     "a migrated result that does not parse",
			original: "name: x\n",
			migrated: "apiVersion: [\n",
			wantErr:  "not one document",
		},
		{
			name:     "a header key that is not a string",
			original: "name: x\n",
			migrated: "apiVersion: [a]\nkind: Service\nname: x\n",
			wantErr:  "does not parse",
		},
		{
			name:     "a migrated result without the header",
			original: "name: x\n",
			migrated: "name: x\n",
			wantErr:  "does not carry both",
		},
		{
			name:     "a body that changed",
			original: "name: x\n",
			migrated: "apiVersion: bosun.io/v1\nkind: Service\nname: y\n",
			wantErr:  "differs from the input",
		},
		{
			name:     "an original that does not parse",
			original: "name: [\n",
			migrated: valid,
			wantErr:  "parse manifest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyMigration([]byte(tt.original), []byte(tt.migrated))

			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestHeaderHelpers_RejectInvalidYAML covers the parse errors the public entry
// point reports earlier, so the helpers stay safe to call on their own.
func TestHeaderHelpers_RejectInvalidYAML(t *testing.T) {
	t.Run("requireSingleDocument", func(t *testing.T) {
		err := requireSingleDocument([]byte("name: [unclosed\n"))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse manifest")
	})

	t.Run("addVersionHeader", func(t *testing.T) {
		got, kind, err := addVersionHeader([]byte("name: [unclosed\n"), KindService)

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Empty(t, kind)
	})
}
