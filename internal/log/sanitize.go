package log

import (
	"strings"
	"unicode"
)

// SanitizeForOutput strips the runes that let untrusted text forge or visually
// rewrite an operator-facing line: Unicode control characters (newline,
// carriage return, ESC and the rest of C0/C1, so neither a fake log record nor
// an ANSI escape survives), format characters (Cf — zero-width joiners and the
// bidirectional overrides), and the line and paragraph separators. Spaces and
// printable Unicode survive unchanged, and invalid UTF-8 decodes to U+FFFD
// instead of reaching the sink as raw bytes.
//
// The result is uncapped; callers bound length themselves, either with
// SanitizeForOutputCapped or with their own cap applied to the return value.
// It lives in this package rather than in a caller because both internal/daemon
// (webhook attribution) and internal/reconcile (container health output) need
// it, and daemon already imports reconcile — a helper in either of them would
// be an import cycle for the other.
func SanitizeForOutput(s string) string {
	return sanitizeForOutput(s, -1)
}

// SanitizeForOutputCapped sanitizes as SanitizeForOutput does and keeps at most
// maxRunes of the surviving code points. Stripped runes do not count against
// the cap. A negative maxRunes means no cap; zero returns the empty string.
func SanitizeForOutputCapped(s string, maxRunes int) string {
	return sanitizeForOutput(s, maxRunes)
}

func sanitizeForOutput(s string, maxRunes int) string {
	var sanitized strings.Builder
	kept := 0
	for _, r := range s {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			continue
		}
		if maxRunes >= 0 && kept == maxRunes {
			break
		}
		sanitized.WriteRune(r)
		kept++
	}
	return sanitized.String()
}
