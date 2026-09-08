package daemon

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requestFrom(remoteAddr, forwardedFor string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/webhook", nil)
	r.RemoteAddr = remoteAddr
	if forwardedFor != "" {
		r.Header.Set("X-Forwarded-For", forwardedFor)
	}
	return r
}

func mustParseProxies(t *testing.T, entries ...string) *trustedProxies {
	t.Helper()
	tp, err := parseTrustedProxies(entries)
	require.NoError(t, err)
	return tp
}

func TestParseTrustedProxiesRejectsUnparseableEntries(t *testing.T) {
	tests := []struct {
		name  string
		entry string
	}{
		{"hostname", "proxy.internal"},
		{"empty string", ""},
		{"whitespace only", "   "},
		{"typo'd CIDR", "10.0.0.0/notanumber"},
		{"not an address", "trust-me"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Rejected rather than ignored: a silently dropped entry disables
			// attribution for exactly the sender the operator meant to trust,
			// and nothing says so.
			_, err := parseTrustedProxies([]string{tc.entry})
			assert.Error(t, err)
		})
	}
}

func TestParseTrustedProxiesAcceptsIPsAndCIDRs(t *testing.T) {
	tp := mustParseProxies(t, "172.18.0.5", "10.0.0.0/8", "2001:db8::1", "fd00::/8")
	assert.False(t, tp.empty())
}

func TestTrustedProxiesEmptyTrustsNothing(t *testing.T) {
	// An empty set means "trust nothing", never "trust everything". The
	// inverted reading is how this control usually fails.
	empty := mustParseProxies(t)
	assert.True(t, empty.empty())
	assert.False(t, empty.trusts("172.18.0.5:41234"))
	assert.False(t, (*trustedProxies)(nil).trusts("172.18.0.5:41234"))
}

func TestTrustedProxiesIgnoresSourcePort(t *testing.T) {
	// RemoteAddr is host:port. A raw string comparison never matches, and it
	// fails in the safe direction, so an absence-only assertion sails past it.
	tp := mustParseProxies(t, "172.18.0.5")
	assert.True(t, tp.trusts("172.18.0.5:41234"))
	assert.True(t, tp.trusts("172.18.0.5:59999"))
	assert.False(t, tp.trusts("172.18.0.6:41234"))
}

func TestTrustedProxiesMatchesIPv6(t *testing.T) {
	tp := mustParseProxies(t, "2001:db8::/32")
	assert.True(t, tp.trusts("[2001:db8::1]:41234"))
	assert.False(t, tp.trusts("[2001:dead::1]:41234"))
}

func TestForwardedForClient(t *testing.T) {
	trusted := mustParseProxies(t, "172.18.0.5", "10.0.0.0/8")

	t.Run("trusted proxy contributes the first element", func(t *testing.T) {
		got := forwardedForClient(requestFrom("172.18.0.5:41234", "203.0.113.9, 198.51.100.4"), trusted)
		assert.Equal(t, "203.0.113.9", got)
	})

	t.Run("untrusted peer sending a header is not believed", func(t *testing.T) {
		// This is the whole point of the control. Any container on the bridge
		// can send a well-formed header; parsing it as an IP does not make it
		// true, and recording it would make the next investigation confidently
		// wrong.
		got := forwardedForClient(requestFrom("192.168.1.50:41234", "203.0.113.9"), trusted)
		assert.Empty(t, got)
	})

	t.Run("no trusted proxies configured believes nobody", func(t *testing.T) {
		got := forwardedForClient(requestFrom("172.18.0.5:41234", "203.0.113.9"), mustParseProxies(t))
		assert.Empty(t, got)
	})

	t.Run("malformed first element omits the field and does not scan on", func(t *testing.T) {
		// Scanning onward would let a sender prepend a garbage element to
		// choose which origin gets attributed.
		got := forwardedForClient(requestFrom("172.18.0.5:41234", "not-an-ip, 203.0.113.9"), trusted)
		assert.Empty(t, got, "the second element must not be promoted")
	})

	t.Run("absent header yields nothing", func(t *testing.T) {
		assert.Empty(t, forwardedForClient(requestFrom("172.18.0.5:41234", ""), trusted))
	})

	t.Run("nil request is safe", func(t *testing.T) {
		assert.Empty(t, forwardedForClient(nil, trusted))
	})

	t.Run("CIDR membership is honoured", func(t *testing.T) {
		got := forwardedForClient(requestFrom("10.42.7.1:41234", "203.0.113.9"), trusted)
		assert.Equal(t, "203.0.113.9", got)
	})
}

func TestTrustedProxiesDescribe(t *testing.T) {
	t.Run("renders parsed prefixes for the startup log", func(t *testing.T) {
		tp := mustParseProxies(t, "172.18.0.5", "10.0.0.0/8")
		assert.Equal(t, []string{"172.18.0.5/32", "10.0.0.0/8"}, tp.describe())
	})

	t.Run("empty describes nothing", func(t *testing.T) {
		assert.Nil(t, mustParseProxies(t).describe())
		assert.Nil(t, (*trustedProxies)(nil).describe())
	})
}

func TestTrustedProxiesTrustsEverything(t *testing.T) {
	tests := []struct {
		name    string
		entries []string
		want    bool
	}{
		{"IPv4 default route", []string{"0.0.0.0/0"}, true},
		{"IPv6 default route", []string{"::/0"}, true},
		{"among narrower entries", []string{"172.18.0.5", "0.0.0.0/0"}, true},
		{"narrow prefixes only", []string{"10.0.0.0/8", "172.18.0.5"}, false},
		{"empty", nil, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, mustParseProxies(t, tc.entries...).trustsEverything())
		})
	}
	assert.False(t, (*trustedProxies)(nil).trustsEverything())
}

// TestValidateConfigRejectsBadTrustedProxies pins the fail-closed path: an
// unparseable list must stop startup, not log and continue with attribution
// silently off.
func TestValidateConfigRejectsBadTrustedProxies(t *testing.T) {
	t.Setenv("BOSUN_TRUSTED_PROXIES", "172.18.0.5, proxy.internal")
	cfg := ConfigFromEnv()

	require.Nil(t, cfg.TrustedProxies, "a partially-valid list trusts nothing")
	err := ValidateConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BOSUN_TRUSTED_PROXIES")
	assert.Contains(t, err.Error(), "proxy.internal", "the error names the offending entry")
}

func TestConfigFromEnvAcceptsValidTrustedProxies(t *testing.T) {
	t.Setenv("BOSUN_TRUSTED_PROXIES", "172.18.0.5, 10.0.0.0/8")
	cfg := ConfigFromEnv()

	require.NotNil(t, cfg.TrustedProxies)
	assert.True(t, cfg.TrustedProxies.trusts("10.1.2.3:4567"))
	assert.False(t, cfg.TrustedProxies.trusts("192.168.1.1:4567"))
}
