package reconcile

import (
	"archive/tar"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLinkTargetWithinRoot_Symlink(t *testing.T) {
	type linkCase struct {
		name     string
		linkname string
		want     bool
	}
	tests := []linkCase{
		{name: "sibling", linkname: "other.yml", want: true},
		{name: "parent within root", linkname: "../top.yml", want: true},
		{name: "climbs out", linkname: "../../escape", want: false},
		// A slash-rooted target is absolute on Unix and rooted on Windows,
		// where IsAbs alone does not catch it after FromSlash.
		{name: "slash rooted", linkname: "/etc/passwd", want: false},
	}
	if runtime.GOOS == "windows" {
		tests = append(tests,
			linkCase{name: "backslash rooted", linkname: `\outside`, want: false},
			linkCase{name: "drive relative", linkname: `C:outside`, want: false},
		)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := linkTargetWithinRoot("compose/link", tar.TypeSymlink, tt.linkname)
			assert.Equal(t, tt.want, got)
		})
	}
}
