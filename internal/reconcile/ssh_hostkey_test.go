package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTempKnownHosts creates a real known_hosts file on disk and returns its
// path. hostKeyOptions now resolves the file on disk (mirroring git.go's
// buildKnownHostsPaths), so tests must point at a path that actually exists.
func writeTempKnownHosts(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "known_hosts")
	require.NoError(t, os.WriteFile(path, []byte("example.com ssh-ed25519 AAAAC3Nz\n"), 0o644))
	return path
}

// TestHostKeyOptions locks in the deploy-path host-key policy and its
// precedence, which must mirror git.go's getHostKeyCallback: insecure wins over
// known_hosts, an existing known_hosts file pins strict verification, the
// insecure opt-out matches ONLY a case-insensitive "true", and the terminal
// case is TOFU (accept-new) — never git.go's insecure fallback.
//
// The accept-new expectations assume /config/known_hosts (the buildKnownHostsPaths
// fallback) does not exist on the test host, which holds on dev machines and CI.
func TestHostKeyOptions(t *testing.T) {
	realKnownHosts := writeTempKnownHosts(t)
	missingKnownHosts := filepath.Join(t.TempDir(), "absent")

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
			name:       "existing known_hosts file pins strict verification",
			knownHosts: realKnownHosts,
			want:       []string{"-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=" + realKnownHosts},
		},
		{
			name:       "known_hosts path that does not exist falls through to accept-new",
			knownHosts: missingKnownHosts,
			want:       []string{"-o", "StrictHostKeyChecking=accept-new"},
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
			name:       "insecure true wins over an existing known_hosts file (precedence mirrors git.go)",
			knownHosts: realKnownHosts,
			insecure:   "true",
			want:       []string{"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null"},
		},
		{
			name:       "non-true insecure falls through to the existing known_hosts file",
			knownHosts: realKnownHosts,
			insecure:   "false",
			want:       []string{"-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=" + realKnownHosts},
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
	kh := writeTempKnownHosts(t)
	t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "")
	t.Setenv("BOSUN_SSH_KNOWN_HOSTS", kh)

	cmd := sshExecCommand(context.Background(), "user@host", "mkdir", "-p", "/srv/app")

	require.NotNil(t, cmd)
	assert.Equal(t, []string{
		"ssh",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "UserKnownHostsFile=" + kh,
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

// installSSHCaptureShim installs a fake `ssh` ahead of PATH that appends its full
// argv to a capture file (one line) and then runs extraBody, so a deploy leg's
// host-key flags can be asserted at the argv level. Returns the capture path.
func installSSHCaptureShim(t *testing.T, extraBody string) string {
	t.Helper()
	dir := t.TempDir()
	capture := filepath.Join(dir, "argv.log")
	shim := "#!/bin/sh\n" +
		"echo \"$@\" >> \"" + capture + "\"\n" +
		extraBody + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ssh"), []byte(shim), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return capture
}

// assertLegCarriesStrictHostKey reads the captured ssh argv and asserts the
// strict host-key policy flags for the given known_hosts file are present.
func assertLegCarriesStrictHostKey(t *testing.T, capture, knownHosts string) {
	t.Helper()
	b, err := os.ReadFile(capture)
	require.NoError(t, err, "capturing ssh shim never ran — the leg did not exec ssh")
	got := string(b)
	assert.Contains(t, got, "-o StrictHostKeyChecking=yes")
	assert.Contains(t, got, "-o UserKnownHostsFile="+knownHosts)
}

// The four tests below cover the deploy-channel ssh legs outside ssh.go that
// must also route through sshExecCommand: remote compose up, its failure
// classifier, the container-signal path, and the secret-bearing backup tar
// stream. Each asserts the host-key policy lands on the wire.

func TestComposeUpRemote_CarriesHostKeyOptions(t *testing.T) {
	kh := writeTempKnownHosts(t)
	t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "")
	t.Setenv("BOSUN_SSH_KNOWN_HOSTS", kh)
	capture := installSSHCaptureShim(t, "exit 0")

	d := &DeployOps{ProjectName: "proj"}
	require.NoError(t, d.ComposeUpRemote(context.Background(), "user@testhost", "/srv/compose"))
	assertLegCarriesStrictHostKey(t, capture, kh)
}

func TestClassifyComposeFailureRemote_CarriesHostKeyOptions(t *testing.T) {
	kh := writeTempKnownHosts(t)
	t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "")
	t.Setenv("BOSUN_SSH_KNOWN_HOSTS", kh)
	// Empty JSON array -> zero entries -> classifier returns without error.
	capture := installSSHCaptureShim(t, "echo '[]'; exit 0")

	d := &DeployOps{}
	_, err := d.classifyComposeFailureRemote(context.Background(), "user@testhost", "/srv/compose")
	require.NoError(t, err)
	assertLegCarriesStrictHostKey(t, capture, kh)
}

func TestSignalContainerRemote_CarriesHostKeyOptions(t *testing.T) {
	kh := writeTempKnownHosts(t)
	t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "")
	t.Setenv("BOSUN_SSH_KNOWN_HOSTS", kh)
	capture := installSSHCaptureShim(t, "exit 0")

	d := &DeployOps{}
	require.NoError(t, d.SignalContainerRemote(context.Background(), "user@testhost", "authelia", "SIGHUP"))
	assertLegCarriesStrictHostKey(t, capture, kh)
}

func TestBackupRemote_CarriesHostKeyOptions(t *testing.T) {
	kh := writeTempKnownHosts(t)
	t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "")
	t.Setenv("BOSUN_SSH_KNOWN_HOSTS", kh)
	// Emit a real, non-empty gzip'd tar so VerifyBackup's integrity read passes.
	body := "tmp=$(mktemp -d)\n: > \"$tmp/f\"\ntar -czf - -C \"$tmp\" .\n"
	capture := installSSHCaptureShim(t, body)

	backupDir := t.TempDir()
	d := &DeployOps{}
	name, err := d.BackupRemote(context.Background(), "user@testhost", backupDir, []string{"/srv/appdata"})
	require.NoError(t, err)
	assert.NotEmpty(t, name)
	assertLegCarriesStrictHostKey(t, capture, kh)
}
