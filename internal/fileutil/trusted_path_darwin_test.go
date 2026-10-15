//go:build darwin

package fileutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeTrustedAliasTarget(t *testing.T) {
	tests := []struct {
		name, aliasName, target, want string
	}{
		{name: "relative var alias", aliasName: "/var", target: "private/var", want: "/private/var"},
		{name: "relative tmp alias", aliasName: "/tmp", target: "private/tmp", want: "/private/tmp"},
		{name: "absolute target", aliasName: "/var", target: "/private/var", want: "/private/var"},
		{name: "relative parent traversal", aliasName: "/var", target: "../private/var", want: "/private/var"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeTrustedAliasTarget(tt.aliasName, tt.target))
		})
	}
}
