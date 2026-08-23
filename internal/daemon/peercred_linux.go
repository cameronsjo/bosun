//go:build linux

package daemon

import (
	"context"
	"net"
	"net/http"

	"golang.org/x/sys/unix"
)

// wrapListenerWithPeerCred wraps a Unix socket listener to inject peer credentials.
func wrapListenerWithPeerCred(listener net.Listener) net.Listener {
	return &peerCredListener{Listener: listener}
}

type peerCredListener struct {
	net.Listener
}

func (l *peerCredListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	// Get peer credentials using SO_PEERCRED
	if unixConn, ok := conn.(*net.UnixConn); ok {
		if credentials, ok := getPeerCredentials(unixConn); ok {
			return &peerCredConn{Conn: conn, credentials: credentials}, nil
		}
	}

	return conn, nil
}

type peerCredConn struct {
	net.Conn
	credentials peerCredentials
}

// getPeerCredentials extracts UID/GID/PID from a Unix socket connection.
func getPeerCredentials(conn *net.UnixConn) (peerCredentials, bool) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return peerCredentials{}, false
	}

	var cred *unix.Ucred
	var credErr error

	err = raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})

	if err != nil || credErr != nil || cred == nil {
		return peerCredentials{}, false
	}

	return peerCredentials{UID: cred.Uid, GID: cred.Gid, PID: cred.Pid}, true
}

// InjectPeerCred is a ConnContext function for http.Server that injects peer credentials.
func InjectPeerCred(ctx context.Context, c net.Conn) context.Context {
	if pc, ok := c.(*peerCredConn); ok {
		return context.WithValue(ctx, peerCredKey, pc.credentials)
	}
	return ctx
}

func peerCredentialSupportAvailable() bool { return true }

// WrapServerForPeerCred configures the HTTP server to use peer credentials.
func WrapServerForPeerCred(srv *http.Server, listener net.Listener) net.Listener {
	srv.ConnContext = InjectPeerCred
	return wrapListenerWithPeerCred(listener)
}
