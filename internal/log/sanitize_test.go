package log

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeForOutput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
		{
			name:  "printable text and spaces survive",
			input: "curl: (7) Failed to connect to localhost port 8080",
			want:  "curl: (7) Failed to connect to localhost port 8080",
		},
		{
			name:  "newline and carriage return cannot forge a second line",
			input: "probe failed\r\nAll critical containers healthy",
			want:  "probe failedAll critical containers healthy",
		},
		{
			name:  "ANSI escape loses its introducer",
			input: "probe failed\x1b[2Kcleared",
			want:  "probe failed[2Kcleared",
		},
		{
			name:  "tab, NEL, and C1 CSI removed",
			input: "a\tb\u0085c\u009bd",
			want:  "abcd",
		},
		{
			name:  "format characters removed (RLO, ZWJ)",
			input: "admin\u202ecba\u200d",
			want:  "admincba",
		},
		{
			name:  "line and paragraph separators removed",
			input: "a\u2028b\u2029c",
			want:  "abc",
		},
		{
			name:  "printable unicode survives",
			input: "déployé \U0001F680 ok",
			want:  "déployé \U0001F680 ok",
		},
		{
			name:  "invalid utf-8 becomes the replacement rune",
			input: string([]byte{0xff, 'o', 'k'}),
			want:  "\uFFFDok",
		},
		{
			name:  "uncapped: long input is returned whole",
			input: strings.Repeat("x", 1000),
			want:  strings.Repeat("x", 1000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, SanitizeForOutput(tt.input))
		})
	}
}

func TestSanitizeForOutputCapped(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxRunes int
		want     string
	}{
		{
			name:     "cap counts surviving runes, not stripped ones",
			input:    strings.Repeat("\n", 50) + strings.Repeat("a", 10),
			maxRunes: 5,
			want:     "aaaaa",
		},
		{
			name:     "cap counts code points, not bytes",
			input:    strings.Repeat("\U0001F680", 10),
			maxRunes: 3,
			want:     strings.Repeat("\U0001F680", 3),
		},
		{
			name:     "input shorter than the cap is untouched",
			input:    "cameron",
			maxRunes: 256,
			want:     "cameron",
		},
		{
			name:     "zero cap yields the empty string",
			input:    "cameron",
			maxRunes: 0,
			want:     "",
		},
		{
			name:     "negative cap means no cap",
			input:    strings.Repeat("a", 500),
			maxRunes: -1,
			want:     strings.Repeat("a", 500),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, SanitizeForOutputCapped(tt.input, tt.maxRunes))
		})
	}
}
