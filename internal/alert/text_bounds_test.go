package alert

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

func TestTruncateUTF16(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxUnits int
		want     string
	}{
		{name: "in bound", text: "hello", maxUnits: 5, want: "hello"},
		{name: "ASCII overflow", text: "hello world", maxUnits: 8, want: "hello..."},
		{name: "supplementary boundary", text: strings.Repeat("🚀", 4), maxUnits: 8, want: strings.Repeat("🚀", 4)},
		{name: "supplementary overflow", text: strings.Repeat("🚀", 5), maxUnits: 8, want: strings.Repeat("🚀", 2) + truncationSuffix},
		{name: "cannot fit suffix", text: "long", maxUnits: 2, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateUTF16(tt.text, tt.maxUnits)
			assert.Equal(t, tt.want, got)
			assert.LessOrEqual(t, utf16Units(got), tt.maxUnits)
			assert.True(t, utf8.ValidString(got))
		})
	}
}
