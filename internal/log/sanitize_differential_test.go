package log

import (
	"math/rand/v2"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/require"
)

// Keep the shipped webhook implementation as a fixed compatibility reference
// for this refactor, including its historical 256-rune cap. Property tests in
// sanitize_test.go independently pin stripping and configurable cap behavior.
func priorWebhookSanitizer(name string) string {
	var sanitized strings.Builder
	kept := 0
	for _, r := range name {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			continue
		}
		if kept == 256 {
			break
		}
		sanitized.WriteRune(r)
		kept++
	}
	return sanitized.String()
}

func TestSanitizeForOutputDifferential(t *testing.T) {
	generator := rand.New(rand.NewPCG(668, 671))
	alphabet := []rune{'a', ' ', '\n', '\r', '\t', '\x1b', '\u0085', '\u202e', '\u200d', '\u2028', '\u2029', 'é', '🚀', '\ufffd'}
	for i := 0; i < 40013; i++ {
		var input strings.Builder
		for j, length := 0, generator.IntN(600); j < length; j++ {
			if generator.IntN(8) == 0 {
				input.WriteByte(byte(generator.IntN(256)))
			} else {
				input.WriteRune(alphabet[generator.IntN(len(alphabet))])
			}
		}
		value := input.String()
		require.Equal(t, priorWebhookSanitizer(value), SanitizeForOutputCapped(value, 256), "input %d", i)
	}
}
