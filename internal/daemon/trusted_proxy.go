package daemon

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// trustedProxies is a parsed allowlist of proxies whose X-Forwarded-For header
// the daemon is willing to record.
//
// Empty means trust nothing. That is the default, and it is the direction this
// control has to fail: BOSUN_LISTEN_ADDR binds all interfaces by design, so any
// host that can reach the daemon may send a well-formed X-Forwarded-For.
// Parsing a value as an IP does not make it true.
type trustedProxies struct {
	nets []*net.IPNet
}

// parseTrustedProxies builds the allowlist from IP addresses and CIDR prefixes.
//
// A bare IP is treated as a single-host prefix. Anything that is neither -- a
// hostname, an empty string, a typo -- is rejected rather than ignored: a
// silently dropped entry disables attribution for the one sender the operator
// meant to trust, and nothing says so. Hostnames are refused outright because
// they would make a trust decision depend on a resolver.
func parseTrustedProxies(entries []string) (*trustedProxies, error) {
	tp := &trustedProxies{}
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			return nil, fmt.Errorf("trusted proxy entry is empty; expected an IP address or CIDR prefix")
		}
		if _, prefix, err := net.ParseCIDR(entry); err == nil {
			tp.nets = append(tp.nets, prefix)
			continue
		}
		ip := net.ParseIP(entry)
		if ip == nil {
			return nil, fmt.Errorf("trusted proxy entry %q is neither an IP address nor a CIDR prefix; hostnames are not accepted", entry)
		}
		bits := 32
		if ip.To4() == nil {
			bits = 128
		}
		tp.nets = append(tp.nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return tp, nil
}

// describe renders the parsed prefixes for a startup log line.
func (t *trustedProxies) describe() []string {
	if t.empty() {
		return nil
	}
	out := make([]string, 0, len(t.nets))
	for _, prefix := range t.nets {
		out = append(out, prefix.String())
	}
	return out
}

// trustsEverything reports whether any configured prefix admits every address.
// A legitimate operator choice, but one worth saying out loud.
func (t *trustedProxies) trustsEverything() bool {
	if t.empty() {
		return false
	}
	for _, prefix := range t.nets {
		if ones, _ := prefix.Mask.Size(); ones == 0 {
			return true
		}
	}
	return false
}

// empty reports whether nothing is trusted.
func (t *trustedProxies) empty() bool {
	return t == nil || len(t.nets) == 0
}

// trusts reports whether an http.Request.RemoteAddr belongs to a trusted proxy.
//
// RemoteAddr is "host:port", so the host must be split off before any
// comparison. Comparing the raw value never matches a configured prefix, and it
// fails in the safe direction -- forwarded_for is simply never emitted -- so a
// test that only asserts the field is absent passes straight over the bug.
func (t *trustedProxies) trusts(remoteAddr string) bool {
	if t.empty() {
		return false
	}
	ip := remoteAddrIP(remoteAddr)
	if ip == nil {
		return false
	}
	for _, prefix := range t.nets {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

// remoteAddrIP extracts the IP from a "host:port" RemoteAddr, tolerating a bare
// host for callers (and tests) that supply one.
func remoteAddrIP(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	return net.ParseIP(strings.Trim(host, "[]"))
}

// forwardedForClient returns the client address an X-Forwarded-For header
// claims, but only when the observed peer is a trusted proxy.
//
// The first element is the one that is read, and if it does not parse the field
// is dropped rather than scanning onward. Scanning would let a sender prepend a
// garbage element to choose which origin gets attributed -- turning a malformed
// header into a control surface.
//
// The returned value is never a substitute for the observed peer address. Log
// both, in separate fields, and never prefer this one.
func forwardedForClient(r *http.Request, trusted *trustedProxies) string {
	if r == nil || !trusted.trusts(r.RemoteAddr) {
		return ""
	}
	header := r.Header.Get("X-Forwarded-For")
	if header == "" {
		return ""
	}
	first := strings.TrimSpace(strings.Split(header, ",")[0])
	ip := net.ParseIP(strings.Trim(first, "[]"))
	if ip == nil {
		return ""
	}
	return ip.String()
}
