package manifest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrateToV1_HeaderPlacement covers where the header lands for inputs with
// little or no body, and the marker line it cannot place a header after.
func TestMigrateToV1_HeaderPlacement(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty file gets a header",
			input: "",
			want:  "apiVersion: bosun.io/v1\nkind: Provision\n",
		},
		{
			name:  "comment-only file gets the header first",
			input: "# nothing here yet\n",
			want:  "apiVersion: bosun.io/v1\nkind: Provision\n# nothing here yet\n",
		},
		{
			name:  "a lone marker keeps it first",
			input: "---\n",
			want:  "---\napiVersion: bosun.io/v1\nkind: Provision\n",
		},
		{
			name:  "apiVersion on the last line without a newline",
			input: "apiVersion: bosun.io/v1",
			want:  "apiVersion: bosun.io/v1\nkind: Provision\n",
		},
		{
			name:  "body without a trailing newline",
			input: "name: x\nprovisions: [a]",
			want:  "apiVersion: bosun.io/v1\nkind: Service\nname: x\nprovisions: [a]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := MigrateToV1([]byte(tt.input))

			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
			assertOneVersionedDocument(t, got)
		})
	}

	t.Run("a marker line that carries content is rejected", func(t *testing.T) {
		got, _, err := MigrateToV1([]byte("--- # first\nname: x\nprovisions: [a]\n"))

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "document marker")
	})
}
