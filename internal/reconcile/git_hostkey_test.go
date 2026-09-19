package reconcile

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	xssh "golang.org/x/crypto/ssh"
	xagent "golang.org/x/crypto/ssh/agent"
)

// writeValidKnownHosts writes a known_hosts file that knownhosts.New can
// actually parse. A hand-written placeholder key blob does not parse, so the
// key material is generated.
func writeValidKnownHosts(t *testing.T) string {
	t.Helper()
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	hostKey, err := xssh.NewPublicKey(publicKey)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "known_hosts")
	line := "example.com " + string(xssh.MarshalAuthorizedKey(hostKey))
	require.NoError(t, os.WriteFile(path, []byte(line), 0o600))
	return path
}

// useVerifiedKnownHosts points the host-key policy at a real, parseable
// known_hosts file for the duration of a test, so SSH auth resolution runs
// under genuine verification rather than the BOSUN_SSH_INSECURE_HOST_KEY
// opt-out. The candidate list is injected so the outcome cannot depend on
// whether /config/known_hosts happens to exist on the host.
func useVerifiedKnownHosts(t *testing.T) string {
	t.Helper()
	path := writeValidKnownHosts(t)
	useEnvOnlyKnownHostsCandidate(t)
	t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "")
	t.Setenv("BOSUN_SSH_KNOWN_HOSTS", path)
	return path
}

// useEnvOnlyKnownHostsCandidate makes BOSUN_SSH_KNOWN_HOSTS the only candidate,
// so a test that wants "no known_hosts anywhere" gets it even on a host or
// container image where /config/known_hosts exists.
func useEnvOnlyKnownHostsCandidate(t *testing.T) {
	t.Helper()
	setKnownHostsCandidates(t, func(env string) []string {
		if env == "" {
			return nil
		}
		return []string{env}
	})
}

// useNoKnownHosts is the exploit precondition from the finding: verification is
// configured nowhere and the insecure opt-out is not set.
func useNoKnownHosts(t *testing.T) {
	t.Helper()
	useEnvOnlyKnownHostsCandidate(t)
	t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "")
	t.Setenv("BOSUN_SSH_KNOWN_HOSTS", "")
}

// TestGetHostKeyCallbackFailsClosed pins the control this file exists for: the
// only configuration that disables SSH host key verification is the explicit
// BOSUN_SSH_INSECURE_HOST_KEY=true opt-out. A missing or unparseable
// known_hosts file returns an error, never InsecureIgnoreHostKey.
func TestGetHostKeyCallbackFailsClosed(t *testing.T) {
	t.Run("no known_hosts candidate fails closed", func(t *testing.T) {
		useNoKnownHosts(t)

		callback, err := getHostKeyCallback()
		require.Error(t, err)
		assert.Nil(t, callback, "a nil callback is what keeps go-git from connecting unverified")
		assert.Contains(t, err.Error(), "known_hosts")
		assert.Contains(t, err.Error(), "BOSUN_SSH_INSECURE_HOST_KEY")
	})

	t.Run("absent configured known_hosts fails closed", func(t *testing.T) {
		useEnvOnlyKnownHostsCandidate(t)
		t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "")
		t.Setenv("BOSUN_SSH_KNOWN_HOSTS", filepath.Join(t.TempDir(), "absent"))

		callback, err := getHostKeyCallback()
		require.Error(t, err)
		assert.Nil(t, callback)
	})

	t.Run("unparseable known_hosts is fatal rather than skipped", func(t *testing.T) {
		malformed := filepath.Join(t.TempDir(), "known_hosts")
		require.NoError(t, os.WriteFile(malformed, []byte("this is not a known_hosts entry\n"), 0o600))
		valid := writeValidKnownHosts(t)
		// Both candidates are offered, and the malformed one comes first: a
		// broken pin must not silently fall through to another file.
		setKnownHostsCandidates(t, func(string) []string { return []string{malformed, valid} })
		t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "")
		t.Setenv("BOSUN_SSH_KNOWN_HOSTS", malformed)

		callback, err := getHostKeyCallback()
		require.Error(t, err)
		assert.Nil(t, callback)
		assert.Contains(t, err.Error(), malformed)
	})

	t.Run("valid known_hosts yields a verifying callback", func(t *testing.T) {
		useVerifiedKnownHosts(t)

		callback, err := getHostKeyCallback()
		require.NoError(t, err)
		require.NotNil(t, callback)
		// A callback that verifies rejects a host key it has never seen.
		unknownKey := freshHostKey(t)
		addr := &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 22}
		assert.Error(t, callback("example.com:22", addr, unknownKey),
			"the resolved callback must actually verify, not ignore")
	})

	t.Run("explicit insecure opt-out is preserved", func(t *testing.T) {
		useNoKnownHosts(t)
		t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "true")

		callback, err := getHostKeyCallback()
		require.NoError(t, err)
		require.NotNil(t, callback)
		unknownKey := freshHostKey(t)
		addr := &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 22}
		assert.NoError(t, callback("example.com:22", addr, unknownKey),
			"BOSUN_SSH_INSECURE_HOST_KEY=true remains the documented escape hatch")
	})
}

func freshHostKey(t *testing.T) xssh.PublicKey {
	t.Helper()
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	hostKey, err := xssh.NewPublicKey(publicKey)
	require.NoError(t, err)
	return hostKey
}

// TestResolveGitAuthFailsClosedWithoutKnownHosts proves the error reaches the
// callers that gate startup, so an SSH repository with no host-key policy is
// refused before any Git operation runs — rather than cloning deployable
// content from an unauthenticated peer.
func TestResolveGitAuthFailsClosedWithoutKnownHosts(t *testing.T) {
	setSSHKeyOnlyAuth := func(t *testing.T) {
		t.Helper()
		t.Setenv("BOSUN_GIT_USERNAME", "")
		t.Setenv("BOSUN_GIT_TOKEN", "")
		t.Setenv("SSH_AUTH_SOCK", "")
		t.Setenv("HOME", t.TempDir())
		keyPath := filepath.Join(t.TempDir(), "deploy-key")
		writeTestSSHPrivateKey(t, keyPath)
		t.Setenv("BOSUN_SSH_KEY", keyPath)
	}

	t.Run("key-file auth is refused", func(t *testing.T) {
		setSSHKeyOnlyAuth(t)
		useNoKnownHosts(t)

		auth, err := ResolveGitAuth("git@example.com:owner/repo.git")
		require.Error(t, err)
		assert.Nil(t, auth)
		assert.Contains(t, err.Error(), "known_hosts")
	})

	t.Run("daemon startup validation rejects it", func(t *testing.T) {
		setSSHKeyOnlyAuth(t)
		useNoKnownHosts(t)

		err := ValidateGitAuthentication("ssh://git@example.com/owner/repo.git")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "known_hosts")
	})

	t.Run("a valid known_hosts still resolves auth", func(t *testing.T) {
		setSSHKeyOnlyAuth(t)
		useVerifiedKnownHosts(t)

		auth, err := ResolveGitAuth("git@example.com:owner/repo.git")
		require.NoError(t, err)
		assert.NotNil(t, auth)
	})

	t.Run("the insecure opt-out still resolves auth", func(t *testing.T) {
		setSSHKeyOnlyAuth(t)
		useNoKnownHosts(t)
		t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "true")

		auth, err := ResolveGitAuth("git@example.com:owner/repo.git")
		require.NoError(t, err)
		assert.NotNil(t, auth)
	})
}

// TestResolveSSHAgentAuthFailsClosedWithoutKnownHosts covers the agent leg of
// the same control. This is the leg the finding's exploit scenario turns on:
// without it, a usable agent hands every loaded identity to whatever answers
// the connection.
func TestResolveSSHAgentAuthFailsClosedWithoutKnownHosts(t *testing.T) {
	useNoKnownHosts(t)

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	keyring := xagent.NewKeyring()
	require.NoError(t, keyring.Add(xagent.AddedKey{PrivateKey: privateKey}))
	client, server := net.Pipe()
	closed := make(chan struct{})
	tracked := &closeTrackingConn{Conn: client, closed: closed}
	go func() { _ = xagent.ServeAgent(keyring, server) }()
	t.Cleanup(func() { _ = server.Close() })

	auth, err := resolveSSHAgentAuthWithDialer("deploy", "agent.sock", func(string, string) (net.Conn, error) {
		return tracked, nil
	})

	require.Error(t, err)
	assert.Nil(t, auth, "a usable agent must not be handed to an unverified peer")
	assert.Contains(t, err.Error(), "known_hosts")
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("the agent socket was not closed after the host-key policy refused")
	}
}
