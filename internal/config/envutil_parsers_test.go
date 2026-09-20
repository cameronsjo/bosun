package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// These three parsers exist so the daemon and the one-shot CLI cannot drift on
// a variable they both read directly. Their behavior is pinned here, in the
// package that owns them, rather than only through their callers.

func TestParseBoolValue(t *testing.T) {
	for _, tc := range []struct {
		in         string
		want       bool
		wantStrict bool
	}{
		{"1", true, true},
		{"true", true, true},
		{"TRUE", true, true},
		{"yes", true, true},
		{"on", true, true},
		{"0", false, true},
		{"false", false, true},
		{"no", false, true},
		{"off", false, true},
		{"", false, false},
		{"ture", false, false},
		{"2", false, false},
	} {
		t.Run(tc.in, func(t *testing.T) {
			value, ok := ParseBoolStrict(tc.in)
			require.Equal(t, tc.wantStrict, ok, "recognized")
			if ok {
				require.Equal(t, tc.want, value)
			}
			// An unrecognized value takes the caller's default, either way.
			require.Equal(t, tc.want || !tc.wantStrict, ParseBoolValue(tc.in, true))
			require.Equal(t, tc.want && tc.wantStrict, ParseBoolValue(tc.in, false))
		})
	}
}

func TestParseDurationValue(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"7s", 7 * time.Second, true},
		{"2m", 2 * time.Minute, true},
		{"11", 11 * time.Second, true}, // bare integer is seconds
		{"0", 0, true},
		{"-5s", -5 * time.Second, true},
		{"", 0, false},
		{"soon", 0, false},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := ParseDurationValue(tc.in)
			require.Equal(t, tc.ok, ok)
			if tc.ok {
				require.Equal(t, tc.want, got)
			}
		})
	}
}

func TestSplitAndTrim(t *testing.T) {
	require.Equal(t, []string{"a.yaml", "b.yaml"}, SplitAndTrim(" a.yaml , ,b.yaml "))
	require.Equal(t, []string{"only.yaml"}, SplitAndTrim("only.yaml"))
	require.Empty(t, SplitAndTrim(" , "))
	require.Empty(t, SplitAndTrim(""))
}

// A secrets variable that is set but names nothing must fail, not yield an
// empty list: an empty list skips SOPS, so templates render with blank secret
// values and deploy.
func TestSecretsFilesFromEnv(t *testing.T) {
	files, err := SecretsFilesFromEnv("SECRETS_FILES", " a.sops.yaml ,b.sops.yaml")
	require.NoError(t, err)
	require.Equal(t, []string{"a.sops.yaml", "b.sops.yaml"}, files)

	for _, raw := range []string{" ", ",", " , ", ",,"} {
		_, err := SecretsFilesFromEnv("BOSUN_SECRETS_FILE", raw)
		require.ErrorContains(t, err, "BOSUN_SECRETS_FILE is set but names no secrets file", "input %q", raw)
	}
}
