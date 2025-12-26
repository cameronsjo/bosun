package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsValidGitURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		// Valid HTTPS URLs
		{
			name: "https github",
			url:  "https://github.com/user/repo",
			want: true,
		},
		{
			name: "https gitlab",
			url:  "https://gitlab.com/user/repo.git",
			want: true,
		},
		{
			name: "https with port",
			url:  "https://git.example.com:8443/repo",
			want: true,
		},

		// Valid HTTP URLs
		{
			name: "http url",
			url:  "http://git.internal/repo",
			want: true,
		},

		// Valid SSH URLs (git@)
		{
			name: "git@ github",
			url:  "git@github.com:user/repo.git",
			want: true,
		},
		{
			name: "git@ gitlab",
			url:  "git@gitlab.com:group/project.git",
			want: true,
		},

		// Valid ssh:// URLs
		{
			name: "ssh protocol",
			url:  "ssh://git@github.com/user/repo.git",
			want: true,
		},
		{
			name: "ssh with port",
			url:  "ssh://git@example.com:22/repo.git",
			want: true,
		},

		// Valid git:// URLs
		{
			name: "git protocol",
			url:  "git://github.com/user/repo.git",
			want: true,
		},

		// Valid file:// URLs
		{
			name: "file protocol",
			url:  "file:///path/to/repo",
			want: true,
		},
		{
			name: "file protocol relative",
			url:  "file://./local/repo",
			want: true,
		},

		// Invalid URLs - too short
		{
			name: "empty string",
			url:  "",
			want: false,
		},
		{
			name: "too short - 1 char",
			url:  "a",
			want: false,
		},
		{
			name: "too short - 4 chars",
			url:  "http",
			want: false,
		},
		{
			name: "exactly 5 chars but invalid",
			url:  "abcde",
			want: false,
		},

		// Invalid URLs - wrong prefix
		{
			name: "ftp protocol",
			url:  "ftp://example.com/repo",
			want: false,
		},
		{
			name: "plain path",
			url:  "/path/to/repo",
			want: false,
		},
		{
			name: "relative path",
			url:  "./repo",
			want: false,
		},
		{
			name: "just hostname",
			url:  "github.com/user/repo",
			want: false,
		},
		{
			name: "mailto url",
			url:  "mailto:user@example.com",
			want: false,
		},

		// Edge cases
		{
			name: "https only",
			url:  "https://",
			want: true, // Has valid prefix, passes current implementation
		},
		{
			name: "git@ only",
			url:  "git@x",
			want: true, // Has valid prefix
		},
		{
			name: "case sensitive - HTTPS",
			url:  "HTTPS://github.com/repo",
			want: false, // Prefix matching is case-sensitive
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidGitURL(tt.url)
			assert.Equal(t, tt.want, got, "isValidGitURL(%q)", tt.url)
		})
	}
}
