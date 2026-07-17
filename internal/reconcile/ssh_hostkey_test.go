package reconcile

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHostKeyOptions locks in the deploy-path host-key policy and its
// precedence, which must mirror git.go's getHostKeyCallback: insecure wins over
// known_hosts, the neutral default is TOFU (accept-new), and the insecure
// opt-out matches ONLY a case-insensitive "true".
func TestHostKeyOptions(t *testing.T) {
	tests := []struct {
		name       string
		knownHosts string
		insecure   string
		want       []string
	}{
		{
			name: "neither set defaults to TOFU accept-new",
			want: []string{"-o", "StrictHostKeyChecking=accept-new"},
		},
		{
			name:       "known_hosts set pins strict verification against the file",
			knownHosts: "/config/known_hosts",
			want:       []string{"-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=/config/known_hosts"},
		},
		{
			name:     "insecure true disables verification",
			insecure: "true",
			want:     []string{"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null"},
		},
		{
			name:     "insecure TRUE is case-insensitive",
			insecure: "TRUE",
			want:     []string{"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null"},
		},
		{
			name:     "insecure True is case-insensitive",
			insecure: "True",
			want:     []string{"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null"},
		},
		{
			name:     "insecure 1 is not treated as true",
			insecure: "1",
			want:     []string{"-o", "StrictHostKeyChecking=accept-new"},
		},
		{
			name:     "insecure yes is not treated as true",
			insecure: "yes",
			want:     []string{"-o", "StrictHostKeyChecking=accept-new"},
		},
		{
			name:       "insecure true wins over known_hosts (precedence mirrors git.go)",
			knownHosts: "/config/known_hosts",
			insecure:   "true",
			want:       []string{"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null"},
		},
		{
			name:       "non-true insecure falls through to known_hosts",
			knownHosts: "/config/known_hosts",
			insecure:   "false",
			want:       []string{"-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=/config/known_hosts"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set both explicitly so the ambient environment can't leak in;
			// an empty value reads as unset for our getenv checks.
			t.Setenv("BOSUN_SSH_KNOWN_HOSTS", tt.knownHosts)
			t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", tt.insecure)
			assert.Equal(t, tt.want, hostKeyOptions())
		})
	}
}

// TestSSHExecCommand_AppliesHostKeyOptions proves the policy flags actually land
// in the built ssh command line, ahead of the caller's host + remote command.
func TestSSHExecCommand_AppliesHostKeyOptions(t *testing.T) {
	t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "")
	t.Setenv("BOSUN_SSH_KNOWN_HOSTS", "/config/known_hosts")

	cmd := sshExecCommand(context.Background(), "user@host", "mkdir", "-p", "/srv/app")

	require.NotNil(t, cmd)
	assert.Equal(t, []string{
		"ssh",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "UserKnownHostsFile=/config/known_hosts",
		"user@host", "mkdir", "-p", "/srv/app",
	}, cmd.Args)
}

// TestSCPExecCommand_AppliesHostKeyOptions is the scp counterpart: the same
// policy flags precede the caller's scp args.
func TestSCPExecCommand_AppliesHostKeyOptions(t *testing.T) {
	t.Setenv("BOSUN_SSH_KNOWN_HOSTS", "")
	t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "true")

	cmd := scpExecCommand(context.Background(), "-q", "/local/file", "user@host:/remote/file")

	require.NotNil(t, cmd)
	assert.Equal(t, []string{
		"scp",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-q", "/local/file", "user@host:/remote/file",
	}, cmd.Args)
}
