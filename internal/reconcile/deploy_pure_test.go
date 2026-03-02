package reconcile

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseSSHError(t *testing.T) {
	baseErr := errors.New("exit status 255")

	tests := []struct {
		name     string
		stderr   string
		host     string
		contains string
	}{
		{
			name:     "permission denied",
			stderr:   "Permission denied (publickey).",
			host:     "user@host",
			contains: "SSH authentication failed",
		},
		{
			name:     "connection refused",
			stderr:   "ssh: connect to host 192.168.1.1 port 22: Connection refused",
			host:     "root@192.168.1.1",
			contains: "SSH connection refused",
		},
		{
			name:     "host key verification failed",
			stderr:   "Host key verification failed.",
			host:     "user@server",
			contains: "SSH host key verification failed",
		},
		{
			name:     "no route to host",
			stderr:   "ssh: connect to host 10.0.0.1: No route to host",
			host:     "root@10.0.0.1",
			contains: "no route to host",
		},
		{
			name:     "connection timed out",
			stderr:   "ssh: connect to host 10.0.0.1 port 22: Connection timed out",
			host:     "root@10.0.0.1",
			contains: "timed out",
		},
		{
			name:     "name or service not known",
			stderr:   "ssh: Could not resolve hostname badhost: Name or service not known",
			host:     "root@badhost",
			contains: "cannot resolve hostname",
		},
		{
			name:     "unknown error falls through to default",
			stderr:   "something completely unexpected happened",
			host:     "user@host",
			contains: "SSH connection to user@host failed",
		},
		{
			name:     "case insensitive matching",
			stderr:   "PERMISSION DENIED (publickey).",
			host:     "user@host",
			contains: "SSH authentication failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseSSHError(baseErr, tt.stderr, tt.host)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.contains)
		})
	}
}

