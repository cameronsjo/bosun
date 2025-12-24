//go:build linux

package daemon

import (
	"context"
	"fmt"
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
		if cred := getPeerCredentials(unixConn); cred != "" {
			return &peerCredConn{Conn: conn, peerCred: cred}, nil
		}
	}

	return conn, nil
}

type peerCredConn struct {
	net.Conn
	peerCred string
}

// getPeerCredentials extracts UID/GID/PID from a Unix socket connection.
func getPeerCredentials(conn *net.UnixConn) string {
	raw, err := conn.SyscallConn()
	if err != nil {
		return ""
	}

	var cred *unix.Ucred
	var credErr error

	err = raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})

	if err != nil || credErr != nil || cred == nil {
		return ""
	}

	return fmt.Sprintf("uid=%d,gid=%d,pid=%d", cred.Uid, cred.Gid, cred.Pid)
}

// InjectPeerCred is a ConnContext function for http.Server that injects peer credentials.
func InjectPeerCred(ctx context.Context, c net.Conn) context.Context {
	if pc, ok := c.(*peerCredConn); ok {
		return context.WithValue(ctx, peerCredKey, pc.peerCred)
	}
	return ctx
}

// WrapServerForPeerCred configures the HTTP server to use peer credentials.
func WrapServerForPeerCred(srv *http.Server, listener net.Listener) net.Listener {
	srv.ConnContext = InjectPeerCred
	return wrapListenerWithPeerCred(listener)
}
