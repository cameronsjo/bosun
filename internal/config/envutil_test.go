package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBosunEnv(t *testing.T) {
	tests := []struct {
		name     string
		bosunVar string // BOSUN_<name> value ("" = unset)
		legacyVar string // <name> value ("" = unset)
		want     string
	}{
		{
			name:      "BOSUN_ only set",
			bosunVar:  "https://bosun.example.com/repo",
			legacyVar: "",
			want:      "https://bosun.example.com/repo",
		},
		{
			name:      "legacy only set",
			bosunVar:  "",
			legacyVar: "https://legacy.example.com/repo",
			want:      "https://legacy.example.com/repo",
		},
		{
			name:      "both set — BOSUN_ wins",
			bosunVar:  "https://bosun.example.com/repo",
			legacyVar: "https://legacy.example.com/repo",
			want:      "https://bosun.example.com/repo",
		},
		{
			name:      "neither set — returns empty",
			bosunVar:  "",
			legacyVar: "",
			want:      "",
		},
		{
			name:      "empty BOSUN_ falls through to legacy",
			bosunVar:  "",
			legacyVar: "https://legacy.example.com/repo",
			want:      "https://legacy.example.com/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.bosunVar != "" {
				t.Setenv("BOSUN_REPO_URL", tt.bosunVar)
			} else {
				t.Setenv("BOSUN_REPO_URL", "")
			}
			if tt.legacyVar != "" {
				t.Setenv("REPO_URL", tt.legacyVar)
			} else {
				t.Setenv("REPO_URL", "")
			}
			got := BosunEnv("REPO_URL")
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBosunEnvBool(t *testing.T) {
	tests := []struct {
		name       string
		bosunVar   string
		legacyVar  string
		defaultVal bool
		want       bool
	}{
		// True values
		{"true string", "true", "", false, true},
		{"1 string", "1", "", false, true},
		{"yes string", "yes", "", false, true},
		{"on string", "on", "", false, true},
		{"TRUE uppercase", "TRUE", "", false, true},
		{"Yes mixed case", "Yes", "", false, true},

		// False values
		{"false string", "false", "", true, false},
		{"0 string", "0", "", true, false},
		{"no string", "no", "", true, false},
		{"off string", "off", "", true, false},
		{"FALSE uppercase", "FALSE", "", true, false},
		{"No mixed case", "No", "", true, false},

		// Neither set — returns default
		{"unset returns default true", "", "", true, true},
		{"unset returns default false", "", "", false, false},

		// BOSUN_ wins over legacy
		{
			name:       "BOSUN_ true wins over legacy false",
			bosunVar:   "true",
			legacyVar:  "false",
			defaultVal: false,
			want:       true,
		},
		{
			name:       "BOSUN_ false wins over legacy true",
			bosunVar:   "false",
			legacyVar:  "true",
			defaultVal: true,
			want:       false,
		},

		// Legacy only
		{
			name:       "legacy no disables",
			bosunVar:   "",
			legacyVar:  "no",
			defaultVal: true,
			want:       false,
		},
		{
			name:       "legacy yes enables",
			bosunVar:   "",
			legacyVar:  "yes",
			defaultVal: false,
			want:       true,
		},

		// Unrecognized value falls back to default
		{"unrecognized uses default true", "maybe", "", true, true},
		{"unrecognized uses default false", "maybe", "", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BOSUN_REMOVE_ORPHANS", tt.bosunVar)
			t.Setenv("REMOVE_ORPHANS", tt.legacyVar)
			got := BosunEnvBool("REMOVE_ORPHANS", tt.defaultVal)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBosunEnvDuration(t *testing.T) {
	tests := []struct {
		name       string
		bosunVar   string
		legacyVar  string
		defaultVal time.Duration
		want       time.Duration
	}{
		// Go duration strings
		{"30s duration", "30s", "", 0, 30 * time.Second},
		{"5m duration", "5m", "", 0, 5 * time.Minute},
		{"1h30m duration", "1h30m", "", 0, 90 * time.Minute},

		// Bare integer (seconds)
		{"bare integer 30", "30", "", 0, 30 * time.Second},
		{"bare integer 3600", "3600", "", 0, 3600 * time.Second},

		// Neither set — returns default
		{"unset returns default", "", "", 5 * time.Minute, 5 * time.Minute},

		// BOSUN_ wins over legacy
		{
			name:       "BOSUN_ 10m wins over legacy 5m",
			bosunVar:   "10m",
			legacyVar:  "5m",
			defaultVal: 0,
			want:       10 * time.Minute,
		},

		// Legacy only
		{
			name:       "legacy 60s used when BOSUN_ unset",
			bosunVar:   "",
			legacyVar:  "60s",
			defaultVal: 0,
			want:       60 * time.Second,
		},

		// Invalid value — returns default
		{"invalid returns default", "not-a-duration", "", 3 * time.Second, 3 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BOSUN_POLL_INTERVAL", tt.bosunVar)
			t.Setenv("POLL_INTERVAL", tt.legacyVar)
			got := BosunEnvDuration("POLL_INTERVAL", tt.defaultVal)
			assert.Equal(t, tt.want, got)
		})
	}
}
