package manifest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrateToV1_MarkerWithoutTrailingNewline pins that a file whose last line
// is the document marker, with no line ending, still gets the header on its own
// lines. Without a newline added after the marker the output reads
// "---apiVersion: ...", which the verification rejects, so the file is reported
// as an error instead of migrated.
func TestMigrateToV1_MarkerWithoutTrailingNewline(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "marker alone",
			input: "---",
			want:  "---\napiVersion: bosun.io/v1\nkind: Provision\n",
		},
		{
			name:  "marker alone with CRLF elsewhere",
			input: "# note\r\n---",
			want:  "# note\r\n---\r\napiVersion: bosun.io/v1\r\nkind: Provision\r\n",
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
}
