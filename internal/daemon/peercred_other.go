//go:build !linux

package daemon

import (
	"net"
	"net/http"
)

// WrapServerForPeerCred is a no-op on non-Linux platforms.
// SO_PEERCRED is Linux-specific.
func WrapServerForPeerCred(srv *http.Server, listener net.Listener) net.Listener {
	// No peer credential support on this platform
	return listener
}
