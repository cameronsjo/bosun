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

func useEnvOnlyKnownHostsCandidate(t *testing.T) {
	t.Helper()
	setKnownHostsCandidates(t, func(env string) []string {
		if env == "" {
			return nil
		}
		return []string{env}
	})
}

func useNoKnownHosts(t *testing.T) {
	t.Helper()
	useEnvOnlyKnownHostsCandidate(t)
	t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "")
	t.Setenv("BOSUN_SSH_KNOWN_HOSTS", "")
}

func useVerifiedKnownHosts(t *testing.T) string {
	t.Helper()
	path := writeValidKnownHosts(t)
	useEnvOnlyKnownHostsCandidate(t)
	t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "")
	t.Setenv("BOSUN_SSH_KNOWN_HOSTS", path)
	return path
}

func freshHostKey(t *testing.T) xssh.PublicKey {
	t.Helper()
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	hostKey, err := xssh.NewPublicKey(publicKey)
	require.NoError(t, err)
	return hostKey
}

func TestGetHostKeyCallbackFailsClosed(t *testing.T) {
	t.Run("no known_hosts candidate fails closed", func(t *testing.T) {
		useNoKnownHosts(t)

		callback, err := getHostKeyCallback()
		require.Error(t, err)
		assert.Nil(t, callback)
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
		setKnownHostsCandidates(t, func(string) []string { return []string{malformed, valid} })
		t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "")
		t.Setenv("BOSUN_SSH_KNOWN_HOSTS", malformed)

		callback, err := getHostKeyCallback()
		require.Error(t, err)
		assert.Nil(t, callback)
		assert.Contains(t, err.Error(), malformed)
	})

	t.Run("candidate inspection failure is fatal rather than skipped", func(t *testing.T) {
		fileAsParent := filepath.Join(t.TempDir(), "not-a-directory")
		require.NoError(t, os.WriteFile(fileAsParent, []byte("x"), 0o600))
		uninspectable := filepath.Join(fileAsParent, "known_hosts")
		valid := writeValidKnownHosts(t)
		setKnownHostsCandidates(t, func(string) []string { return []string{uninspectable, valid} })
		t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "")
		t.Setenv("BOSUN_SSH_KNOWN_HOSTS", uninspectable)

		callback, err := getHostKeyCallback()
		require.Error(t, err)
		assert.Nil(t, callback)
		assert.Contains(t, err.Error(), uninspectable)
		assert.Contains(t, err.Error(), "could not be inspected")
	})

	t.Run("valid known_hosts yields a verifying callback", func(t *testing.T) {
		useVerifiedKnownHosts(t)

		callback, err := getHostKeyCallback()
		require.NoError(t, err)
		require.NotNil(t, callback)
		addr := &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 22}
		assert.Error(t, callback("example.com:22", addr, freshHostKey(t)))
	})

	t.Run("explicit insecure opt-out is preserved", func(t *testing.T) {
		useNoKnownHosts(t)
		t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "true")

		callback, err := getHostKeyCallback()
		require.NoError(t, err)
		require.NotNil(t, callback)
		addr := &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 22}
		assert.NoError(t, callback("example.com:22", addr, freshHostKey(t)))
	})
}

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

	t.Run("validation rejects it before the pipeline", func(t *testing.T) {
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
	assert.Nil(t, auth)
	assert.Contains(t, err.Error(), "known_hosts")
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("the agent socket was not closed after host-key policy refusal")
	}
}
