package reconcile

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	gitclient "github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	xssh "golang.org/x/crypto/ssh"
	xagent "golang.org/x/crypto/ssh/agent"
)

func TestResolveGitAuth(t *testing.T) {
	t.Setenv("BOSUN_GIT_USERNAME", "")
	t.Setenv("BOSUN_GIT_TOKEN", "")
	t.Setenv("GIT_USERNAME", "legacy-user")
	t.Setenv("GIT_TOKEN", "legacy-token")

	t.Run("anonymous HTTPS ignores legacy aliases", func(t *testing.T) {
		auth, err := ResolveGitAuth("https://example.com/owner/repo.git")
		require.NoError(t, err)
		assert.Nil(t, auth)
		require.NoError(t, ValidateGitAuthentication("https://example.com/owner/repo.git"))
	})

	t.Run("paired credentials produce Basic auth", func(t *testing.T) {
		t.Setenv("BOSUN_GIT_USERNAME", "bosun-user")
		t.Setenv("BOSUN_GIT_TOKEN", "bosun-token")

		auth, err := ResolveGitAuth("HTTPS://example.com/owner/repo.git")
		require.NoError(t, err)
		basic, ok := auth.(*githttp.BasicAuth)
		require.True(t, ok)
		assert.Equal(t, "bosun-user", basic.Username)
		assert.Equal(t, "bosun-token", basic.Password)
	})

	tests := []struct {
		name      string
		repoURL   string
		username  string
		token     string
		wantError string
	}{
		{name: "missing username", repoURL: "https://example.com/repo.git", token: "secret", wantError: "BOSUN_GIT_USERNAME"},
		{name: "missing token", repoURL: "https://example.com/repo.git", username: "user", wantError: "BOSUN_GIT_TOKEN"},
		{name: "plain HTTP", repoURL: "http://example.com/repo.git", username: "user", token: "secret", wantError: "absolute https://"},
		{name: "SSH URL user", repoURL: "ssh://git@example.com/repo.git", username: "user", token: "secret", wantError: "absolute https://"},
		{name: "SSH URL password", repoURL: "ssh://git:password@example.com/repo.git", wantError: "userinfo"},
		{name: "SSH without userinfo", repoURL: "ssh://example.com/repo.git", username: "user", token: "secret", wantError: "absolute https://"},
		{name: "SCP SSH", repoURL: "git@example.com:owner/repo.git", username: "user", token: "secret", wantError: "absolute https://"},
		{name: "local path", repoURL: "/tmp/repo.git", username: "user", token: "secret", wantError: "absolute https://"},
		{name: "hostless HTTPS", repoURL: "https:///repo.git", username: "user", token: "secret", wantError: "absolute https://"},
		{name: "invalid HTTPS port", repoURL: "https://example.com:notaport/repo.git", username: "user", token: "secret", wantError: "malformed"},
		{name: "malformed URL", repoURL: "https://user%ZZ@example.com/repo.git", username: "user", token: "secret", wantError: "malformed"},
		{name: "username userinfo", repoURL: "https://embedded@example.com/repo.git", wantError: "userinfo"},
		{name: "password userinfo", repoURL: "https://embedded:password@example.com/repo.git", wantError: "userinfo"},
		{name: "percent encoded userinfo", repoURL: "https://embedded%40name:pass%3Aword@example.com/repo.git", wantError: "userinfo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BOSUN_GIT_USERNAME", tt.username)
			t.Setenv("BOSUN_GIT_TOKEN", tt.token)
			_, err := ResolveGitAuth(tt.repoURL)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantError)
			assert.NotContains(t, err.Error(), "secret")
			assert.NotContains(t, err.Error(), "password")
		})
	}

	t.Run("SCP syntax loads a valid explicit SSH key", func(t *testing.T) {
		t.Setenv("BOSUN_GIT_USERNAME", "")
		t.Setenv("BOSUN_GIT_TOKEN", "")
		t.Setenv("SSH_AUTH_SOCK", "")
		keyPath := filepath.Join(t.TempDir(), "deploy-key")
		writeTestSSHPrivateKey(t, keyPath)
		t.Setenv("BOSUN_SSH_KEY", keyPath)
		t.Setenv("HOME", t.TempDir())

		auth, err := ResolveGitAuth("git@example.com:owner/repo.git")
		require.NoError(t, err)
		assert.IsType(t, &gitssh.PublicKeys{}, auth)
	})
}

func TestResolveGitAuthValidatesExplicitSSHKey(t *testing.T) {
	t.Setenv("BOSUN_GIT_USERNAME", "")
	t.Setenv("BOSUN_GIT_TOKEN", "")
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "true")

	t.Run("valid key is accepted", func(t *testing.T) {
		keyPath := filepath.Join(t.TempDir(), "deploy-key")
		writeTestSSHPrivateKey(t, keyPath)
		t.Setenv("BOSUN_SSH_KEY", keyPath)

		auth, err := ResolveGitAuth("git@example.com:owner/repo.git")
		require.NoError(t, err)
		assert.IsType(t, &gitssh.PublicKeys{}, auth)
	})

	invalidCases := []struct {
		name       string
		prepare    func(t *testing.T, path string)
		wantReason string
	}{
		{name: "missing", wantReason: "does not exist"},
		{name: "directory", prepare: func(t *testing.T, path string) {
			require.NoError(t, os.Mkdir(path, 0700))
		}, wantReason: "not a regular file"},
		{name: "empty", prepare: func(t *testing.T, path string) {
			require.NoError(t, os.WriteFile(path, nil, 0600))
		}, wantReason: "is empty"},
		{name: "unparseable", prepare: func(t *testing.T, path string) {
			require.NoError(t, os.WriteFile(path, []byte("not a private key"), 0600))
		}, wantReason: "parseable private key"},
	}

	for _, tt := range invalidCases {
		t.Run(tt.name, func(t *testing.T) {
			keyPath := filepath.Join(t.TempDir(), "deploy-key")
			if tt.prepare != nil {
				tt.prepare(t, keyPath)
			}
			t.Setenv("BOSUN_SSH_KEY", keyPath)

			auth, err := ResolveGitAuth("ssh://example.com/owner/repo.git")
			require.Error(t, err)
			assert.Nil(t, auth)
			assert.Contains(t, err.Error(), "BOSUN_SSH_KEY")
			assert.Contains(t, err.Error(), keyPath)
			assert.Contains(t, err.Error(), tt.wantReason)
			assert.Contains(t, err.Error(), "Docker")
		})
	}
}

func TestResolveSSHKeyFileAuthConventionalCandidates(t *testing.T) {
	t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "true")

	t.Run("existing invalid candidate fails with its path when no fallback succeeds", func(t *testing.T) {
		invalidPath := filepath.Join(t.TempDir(), "deploy-key")
		require.NoError(t, os.Mkdir(invalidPath, 0700))
		missingPath := filepath.Join(t.TempDir(), "missing-key")

		auth, err := resolveSSHKeyFileAuth("", []string{invalidPath, missingPath})
		require.Error(t, err)
		assert.Nil(t, auth)
		assert.Contains(t, err.Error(), "SSH key candidate")
		assert.Contains(t, err.Error(), invalidPath)
		assert.Contains(t, err.Error(), "not a regular file")
		assert.Contains(t, err.Error(), "Docker")
	})

	t.Run("later valid candidate wins over an earlier invalid candidate", func(t *testing.T) {
		invalidPath := filepath.Join(t.TempDir(), "deploy-key")
		require.NoError(t, os.Mkdir(invalidPath, 0700))
		validPath := filepath.Join(t.TempDir(), "id_ed25519")
		writeTestSSHPrivateKey(t, validPath)

		auth, err := resolveSSHKeyFileAuth("", []string{invalidPath, validPath})
		require.NoError(t, err)
		assert.IsType(t, &gitssh.PublicKeys{}, auth)
	})

	t.Run("absent conventional candidates remain optional", func(t *testing.T) {
		missingPath := filepath.Join(t.TempDir(), "missing-key")

		auth, err := resolveSSHKeyFileAuth("", []string{missingPath})
		require.NoError(t, err)
		assert.Nil(t, auth)
	})
}

func TestResolveSSHKeyFileAuthInjectedReaderCandidateMatrix(t *testing.T) {
	t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "true")
	validKey := testSSHPrivateKeyPEM(t)

	tests := []struct {
		name         string
		explicitPath string
		candidates   []string
		results      map[string]struct {
			contents []byte
			err      error
		}
		wantAuth   bool
		wantReason string
	}{
		{
			name:       "all missing candidates remain optional",
			candidates: []string{"missing-a", "missing-b"},
			results: map[string]struct {
				contents []byte
				err      error
			}{
				"missing-a": {err: fmt.Errorf("does not exist: %w", os.ErrNotExist)},
				"missing-b": {err: fmt.Errorf("does not exist: %w", os.ErrNotExist)},
			},
		},
		{
			name:       "inspect failure surfaces when no candidate succeeds",
			candidates: []string{"inspect", "missing"},
			results: map[string]struct {
				contents []byte
				err      error
			}{
				"inspect": {err: errors.New("cannot be inspected: permission denied")},
				"missing": {err: fmt.Errorf("does not exist: %w", os.ErrNotExist)},
			},
			wantReason: "cannot be inspected",
		},
		{
			name:       "later valid candidate wins over read failure",
			candidates: []string{"unreadable", "valid"},
			results: map[string]struct {
				contents []byte
				err      error
			}{
				"unreadable": {err: errors.New("cannot be read: permission denied")},
				"valid":      {contents: validKey},
			},
			wantAuth: true,
		},
		{
			name:         "explicit read failure is authoritative",
			explicitPath: "explicit",
			candidates:   []string{"valid"},
			results: map[string]struct {
				contents []byte
				err      error
			}{
				"explicit": {err: errors.New("cannot be read: permission denied")},
				"valid":    {contents: validKey},
			},
			wantReason: "cannot be read",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := func(path string) ([]byte, error) {
				result, ok := tt.results[path]
				require.True(t, ok, "unexpected candidate %q", path)
				return result.contents, result.err
			}
			auth, err := resolveSSHKeyFileAuthForUser("deploy", tt.explicitPath, tt.candidates, reader)
			if tt.wantReason != "" {
				require.Error(t, err)
				assert.Nil(t, auth)
				assert.Contains(t, err.Error(), tt.wantReason)
				return
			}
			require.NoError(t, err)
			if !tt.wantAuth {
				assert.Nil(t, auth)
				return
			}
			publicKeys, ok := auth.(*gitssh.PublicKeys)
			require.True(t, ok)
			assert.Equal(t, "deploy", publicKeys.User)
		})
	}
}

func TestResolveGitAuthSSHPrecedenceAndProtocolIsolation(t *testing.T) {
	t.Setenv("BOSUN_GIT_USERNAME", "")
	t.Setenv("BOSUN_GIT_TOKEN", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "true")
	invalidKey := filepath.Join(t.TempDir(), "missing-deploy-key")
	t.Setenv("BOSUN_SSH_KEY", invalidKey)

	t.Run("HTTPS ignores unrelated invalid SSH key", func(t *testing.T) {
		t.Setenv("SSH_AUTH_SOCK", "")
		auth, err := ResolveGitAuth("https://example.com/owner/repo.git")
		require.NoError(t, err)
		assert.Nil(t, auth)
	})

	t.Run("HTTPS path containing SSH marker remains isolated", func(t *testing.T) {
		t.Setenv("SSH_AUTH_SOCK", "")
		auth, err := ResolveGitAuth("https://example.com/repos/ssh://archived.git")
		require.NoError(t, err)
		assert.Nil(t, auth)
	})

	t.Run("working agent wins over invalid explicit key", func(t *testing.T) {
		originalResolver := resolveSSHAgentAuth
		t.Cleanup(func() { resolveSSHAgentAuth = originalResolver })
		resolveSSHAgentAuth = func(user string) transport.AuthMethod {
			return &gitssh.PublicKeysCallback{User: user}
		}

		auth, err := ResolveGitAuth("git@example.com:owner/repo.git")
		require.NoError(t, err)
		assert.IsType(t, &gitssh.PublicKeysCallback{}, auth)
	})
}

func TestResolveGitAuthPreservesSSHUser(t *testing.T) {
	t.Setenv("BOSUN_GIT_USERNAME", "")
	t.Setenv("BOSUN_GIT_TOKEN", "")
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("BOSUN_SSH_INSECURE_HOST_KEY", "true")
	keyPath := filepath.Join(t.TempDir(), "deploy-key")
	writeTestSSHPrivateKey(t, keyPath)
	t.Setenv("BOSUN_SSH_KEY", keyPath)

	for _, repoURL := range []string{
		"deploy@example.com:owner/repo.git",
		"ssh://deploy@example.com/owner/repo.git",
	} {
		auth, err := ResolveGitAuth(repoURL)
		require.NoError(t, err)
		publicKeys, ok := auth.(*gitssh.PublicKeys)
		require.True(t, ok)
		assert.Equal(t, "deploy", publicKeys.User)
	}
}

func TestResolveGitAuthRecognizedSSHWithoutAuthFailsClosed(t *testing.T) {
	t.Setenv("BOSUN_GIT_USERNAME", "")
	t.Setenv("BOSUN_GIT_TOKEN", "")
	t.Setenv("SSH_AUTH_SOCK", "")

	originalKeyResolver := resolveSSHKeyAuth
	t.Cleanup(func() { resolveSSHKeyAuth = originalKeyResolver })
	resolveSSHKeyAuth = func(string) (transport.AuthMethod, error) { return nil, nil }

	auth, err := ResolveGitAuth("operator@example.com:owner/repo.git")
	require.Error(t, err)
	assert.Nil(t, auth)
	assert.Contains(t, err.Error(), "SSH authentication is unavailable")
}

func TestResolveSSHAgentAuthRequiresUsableSigners(t *testing.T) {
	t.Run("empty agent is closed and rejected", func(t *testing.T) {
		client, server := net.Pipe()
		closed := make(chan struct{})
		tracked := &closeTrackingConn{Conn: client, closed: closed}
		go func() { _ = xagent.ServeAgent(xagent.NewKeyring(), server) }()
		t.Cleanup(func() { _ = server.Close() })

		auth := resolveSSHAgentAuthWithDialer("deploy", "agent.sock", func(network, address string) (net.Conn, error) {
			assert.Equal(t, "unix", network)
			assert.Equal(t, "agent.sock", address)
			return tracked, nil
		})
		assert.Nil(t, auth)
		require.Len(t, tracked.recordedDeadlines(), 1)
		assert.False(t, tracked.recordedDeadlines()[0].IsZero())
		select {
		case <-closed:
		case <-time.After(time.Second):
			t.Fatal("unusable SSH agent connection was not closed")
		}
	})

	t.Run("agent with signer is accepted and preserves user", func(t *testing.T) {
		_, privateKey, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		keyring := xagent.NewKeyring()
		require.NoError(t, keyring.Add(xagent.AddedKey{PrivateKey: privateKey}))
		client, server := net.Pipe()
		tracked := &closeTrackingConn{Conn: client, closed: make(chan struct{})}
		go func() { _ = xagent.ServeAgent(keyring, server) }()
		t.Cleanup(func() {
			_ = tracked.Close()
			_ = server.Close()
		})

		auth := resolveSSHAgentAuthWithDialer("deploy", "agent.sock", func(string, string) (net.Conn, error) {
			return tracked, nil
		})
		callback, ok := auth.(*gitssh.PublicKeysCallback)
		require.True(t, ok)
		assert.Equal(t, "deploy", callback.User)
		signers, err := callback.Callback()
		require.NoError(t, err)
		assert.Len(t, signers, 1)
		deadlines := tracked.recordedDeadlines()
		require.Len(t, deadlines, 2)
		assert.False(t, deadlines[0].IsZero())
		assert.True(t, deadlines[1].IsZero())
	})
}

type closeTrackingConn struct {
	net.Conn
	closed chan struct{}
	once   sync.Once
	mu     sync.Mutex
	times  []time.Time
}

func (c *closeTrackingConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

func (c *closeTrackingConn) SetDeadline(deadline time.Time) error {
	c.mu.Lock()
	c.times = append(c.times, deadline)
	c.mu.Unlock()
	return c.Conn.SetDeadline(deadline)
}

func (c *closeTrackingConn) recordedDeadlines() []time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Time(nil), c.times...)
}

func TestDefaultSSHAuthBuilderNeverReturnsNilAuthWithoutError(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("BOSUN_SSH_KEY", "")

	originalKeyResolver := resolveSSHKeyAuth
	t.Cleanup(func() { resolveSSHKeyAuth = originalKeyResolver })
	resolveSSHKeyAuth = func(string) (transport.AuthMethod, error) { return nil, nil }

	auth, err := gitssh.DefaultAuthBuilder("git")
	require.Error(t, err)
	assert.Nil(t, auth)
	assert.Contains(t, err.Error(), "SSH authentication is unavailable")
}

func TestGitOpsInvalidSSHKeyFailsWithoutPanic(t *testing.T) {
	t.Setenv("BOSUN_GIT_USERNAME", "")
	t.Setenv("BOSUN_GIT_TOKEN", "")
	t.Setenv("SSH_AUTH_SOCK", "")
	keyPath := filepath.Join(t.TempDir(), "deploy-key")
	require.NoError(t, os.Mkdir(keyPath, 0700))
	t.Setenv("BOSUN_SSH_KEY", keyPath)
	t.Setenv("HOME", t.TempDir())

	g := NewGitOps("git@127.0.0.1:owner/repo.git", "main", filepath.Join(t.TempDir(), "clone"))
	err := g.Clone(context.Background(), 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve Git authentication")
	assert.Contains(t, err.Error(), keyPath)
}

func writeTestSSHPrivateKey(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, testSSHPrivateKeyPEM(t), 0600))
}

func testSSHPrivateKeyPEM(t *testing.T) []byte {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	block, err := xssh.MarshalPrivateKey(privateKey, "bosun test key")
	require.NoError(t, err)
	return pem.EncodeToMemory(block)
}

func TestGitAuthenticationRejectsBeforeNetwork(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	tests := []struct {
		name     string
		repoURL  string
		username string
		token    string
	}{
		{name: "partial", repoURL: server.URL + "/repo.git", username: "user"},
		{name: "userinfo", repoURL: strings.Replace(server.URL, "https://", "https://embedded:secret@", 1) + "/repo.git"},
		{name: "non HTTPS", repoURL: strings.Replace(server.URL, "https://", "http://", 1) + "/repo.git", username: "user", token: "token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests.Store(0)
			t.Setenv("BOSUN_GIT_USERNAME", tt.username)
			t.Setenv("BOSUN_GIT_TOKEN", tt.token)
			g := NewGitOps(tt.repoURL, "main", filepath.Join(t.TempDir(), "clone"))
			require.Error(t, g.Clone(context.Background(), 1))
			_, _, _, err := g.Pull(context.Background())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "Git authentication")
			assert.Zero(t, requests.Load())
		})
	}
}

func TestGitOpsAuthenticatedHTTPSCloneAndFetch(t *testing.T) {
	const username = "private-user"
	const token = "private-token"
	t.Setenv("BOSUN_GIT_USERNAME", username)
	t.Setenv("BOSUN_GIT_TOKEN", token)

	serverRoot := t.TempDir()
	sourceDir := t.TempDir()
	remoteDir := filepath.Join(serverRoot, "repo.git")
	sourceRepo, sourceTree := initializeGitSource(t, sourceDir)
	_, err := sourceRepo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{remoteDir}})
	require.NoError(t, err)
	_, err = git.PlainInit(remoteDir, true)
	require.NoError(t, err)
	require.NoError(t, sourceRepo.Push(&git.PushOptions{RemoteName: "origin", RefSpecs: []config.RefSpec{"refs/heads/master:refs/heads/master"}}))

	var mu sync.Mutex
	var observed []string
	var rejectAuthentication atomic.Bool
	backend := gitHTTPBackend(t, serverRoot)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotToken, ok := r.BasicAuth()
		if !ok || gotUser != username || gotToken != token {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		if rejectAuthentication.Load() {
			http.Error(w, fmt.Sprintf("rejected user=%s token=%s escaped=%s auth=%s",
				gotUser, gotToken, url.QueryEscape(gotToken), r.Header.Get("Authorization")), http.StatusUnauthorized)
			return
		}
		mu.Lock()
		observed = append(observed, r.Method+" "+r.URL.Path)
		mu.Unlock()
		backend.ServeHTTP(w, r)
	}))
	defer server.Close()
	restoreHTTPSProtocol(t, server.Client())

	targetDir := filepath.Join(t.TempDir(), "checkout")
	gitOps := NewGitOps(strings.Replace(server.URL, "https://", "HTTPS://", 1)+"/repo.git", "master", targetDir)
	require.NoError(t, gitOps.Clone(context.Background(), 1))

	writeAndCommit(t, sourceTree, sourceDir, "second.txt", "second", "second commit")
	require.NoError(t, sourceRepo.Push(&git.PushOptions{RemoteName: "origin", RefSpecs: []config.RefSpec{"refs/heads/master:refs/heads/master"}}))
	changed, before, after, err := gitOps.Pull(context.Background())
	require.NoError(t, err)
	assert.True(t, changed)
	assert.NotEqual(t, before, after)

	rejectAuthentication.Store(true)
	_, _, _, err = gitOps.Pull(context.Background())
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "authentication")
	for _, forbidden := range []string{
		username,
		token,
		url.QueryEscape(token),
		base64.StdEncoding.EncodeToString([]byte(username + ":" + token)),
	} {
		assert.NotContains(t, err.Error(), forbidden)
	}

	mu.Lock()
	requests := append([]string(nil), observed...)
	mu.Unlock()
	assert.GreaterOrEqual(t, len(requests), 4, "clone and fetch should each perform discovery and upload-pack requests")
}

func TestGitOpsAnonymousHTTPSAndRejectedAuthentication(t *testing.T) {
	t.Setenv("BOSUN_GIT_USERNAME", "")
	t.Setenv("BOSUN_GIT_TOKEN", "")

	var authorizationHeaders []string
	var mu sync.Mutex
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authorizationHeaders = append(authorizationHeaders, r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
		if user, password, ok := r.BasicAuth(); ok {
			http.Error(w, fmt.Sprintf("authentication rejected user=%s token=%s escaped=%s auth=%s",
				user, password, url.QueryEscape(password), r.Header.Get("Authorization")), http.StatusUnauthorized)
			return
		}
		http.Error(w, "authentication required", http.StatusUnauthorized)
	}))
	defer server.Close()
	restoreHTTPSProtocol(t, server.Client())

	g := NewGitOps(server.URL+"/repo.git", "main", filepath.Join(t.TempDir(), "clone"))
	err := g.Clone(context.Background(), 1)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "authentication")
	mu.Lock()
	require.NotEmpty(t, authorizationHeaders)
	assert.Empty(t, authorizationHeaders[0])
	mu.Unlock()

	t.Setenv("BOSUN_GIT_USERNAME", "rejected-user")
	t.Setenv("BOSUN_GIT_TOKEN", "rejected-token")
	g = NewGitOps(server.URL+"/repo.git", "main", filepath.Join(t.TempDir(), "authenticated-clone"))
	err = g.Clone(context.Background(), 1)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "authentication")
	assert.NotContains(t, err.Error(), "rejected-user")
	assert.NotContains(t, err.Error(), "rejected-token")
	assert.NotContains(t, err.Error(), url.QueryEscape("rejected-token"))
	assert.NotContains(t, err.Error(), base64.StdEncoding.EncodeToString([]byte("rejected-user:rejected-token")))
}

func TestAuthenticatedGitRedirectPolicy(t *testing.T) {
	const authorization = "Basic dXNlcjp0b2tlbg=="

	t.Run("same origin HTTPS preserves authorization", func(t *testing.T) {
		var redirectedHeader string
		server := httptest.NewTLSServer(nil)
		defer server.Close()
		mux := http.NewServeMux()
		mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/finish", http.StatusFound)
		})
		mux.HandleFunc("/finish", func(w http.ResponseWriter, r *http.Request) {
			redirectedHeader = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusNoContent)
		})
		server.Config.Handler = mux

		client := server.Client()
		client.CheckRedirect = checkGitRedirect
		req, err := http.NewRequest(http.MethodGet, server.URL+"/start", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", authorization)
		resp, err := client.Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		assert.Equal(t, authorization, redirectedHeader)
	})

	t.Run("downgrade is rejected before destination", func(t *testing.T) {
		var destinationRequests atomic.Int32
		destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			destinationRequests.Add(1)
		}))
		defer destination.Close()
		source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, destination.URL, http.StatusFound)
		}))
		defer source.Close()

		client := source.Client()
		client.CheckRedirect = checkGitRedirect
		req, err := http.NewRequest(http.MethodGet, source.URL, nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", authorization)
		resp, err := client.Do(req)
		require.Error(t, err)
		if resp != nil {
			require.NoError(t, resp.Body.Close())
		}
		assert.Zero(t, destinationRequests.Load())
	})

	t.Run("cross origin is rejected before destination", func(t *testing.T) {
		var destinationRequests atomic.Int32
		destination := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			destinationRequests.Add(1)
		}))
		defer destination.Close()
		source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, destination.URL, http.StatusFound)
		}))
		defer source.Close()

		transport := source.Client().Transport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // test servers only
		client := newGitHTTPClient(transport)
		req, err := http.NewRequest(http.MethodGet, source.URL, nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", authorization)
		resp, err := client.Do(req)
		require.Error(t, err)
		if resp != nil {
			require.NoError(t, resp.Body.Close())
		}
		assert.Zero(t, destinationRequests.Load())
	})

	t.Run("explicit default port is same origin", func(t *testing.T) {
		a, err := url.Parse("https://example.com/repo")
		require.NoError(t, err)
		b, err := url.Parse("https://EXAMPLE.com:443/redirect")
		require.NoError(t, err)
		assert.True(t, sameHTTPSOrigin(a, b))
	})
}

func TestGitRedirectPolicyEdgeCases(t *testing.T) {
	client := newGitHTTPClient(nil)
	assert.Equal(t, http.DefaultTransport, client.Transport)

	target, err := url.Parse("https://example.com/next")
	require.NoError(t, err)
	req := &http.Request{URL: target, Header: make(http.Header)}
	require.NoError(t, checkGitRedirect(req, nil))

	tooMany := make([]*http.Request, 10)
	require.ErrorContains(t, checkGitRedirect(req, tooMany), "10 redirects")

	originURL, err := url.Parse("https://example.com/start")
	require.NoError(t, err)
	origin := &http.Request{URL: originURL, Header: make(http.Header)}
	require.NoError(t, checkGitRedirect(req, []*http.Request{origin}), "anonymous redirect remains unchanged")

	origin.Header.Set("Authorization", "Basic dXNlcjp0b2tlbg==")
	userinfoTarget, err := url.Parse("https://embedded@example.com/next")
	require.NoError(t, err)
	userinfoReq := &http.Request{URL: userinfoTarget, Header: make(http.Header)}
	require.ErrorContains(t, checkGitRedirect(userinfoReq, []*http.Request{origin}), "redirect rejected")
	assert.Empty(t, userinfoReq.Header.Get("Authorization"))

	differentPort, err := url.Parse("https://example.com:8443/next")
	require.NoError(t, err)
	assert.False(t, sameHTTPSOrigin(originURL, differentPort))
}

func TestGitOpsAuthenticatedHTTPSRedirects(t *testing.T) {
	const username = "redirect-user"
	const token = "redirect-token"
	t.Setenv("BOSUN_GIT_USERNAME", username)
	t.Setenv("BOSUN_GIT_TOKEN", token)

	t.Run("clone follows same origin HTTPS redirect", func(t *testing.T) {
		serverRoot := t.TempDir()
		sourceDir := t.TempDir()
		remoteDir := filepath.Join(serverRoot, "repo.git")
		sourceRepo, _ := initializeGitSource(t, sourceDir)
		_, err := sourceRepo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{remoteDir}})
		require.NoError(t, err)
		_, err = git.PlainInit(remoteDir, true)
		require.NoError(t, err)
		require.NoError(t, sourceRepo.Push(&git.PushOptions{RemoteName: "origin", RefSpecs: []config.RefSpec{"refs/heads/master:refs/heads/master"}}))

		backend := gitHTTPBackend(t, serverRoot)
		var redirectedAuth string
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotUser, gotToken, ok := r.BasicAuth()
			if !ok || gotUser != username || gotToken != token {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/redirect/") {
				destination := strings.TrimPrefix(r.URL.Path, "/redirect")
				if r.URL.RawQuery != "" {
					destination += "?" + r.URL.RawQuery
				}
				http.Redirect(w, r, destination, http.StatusFound)
				return
			}
			redirectedAuth = r.Header.Get("Authorization")
			backend.ServeHTTP(w, r)
		}))
		defer server.Close()
		restoreHTTPSProtocol(t, server.Client())

		g := NewGitOps(server.URL+"/redirect/repo.git", "master", filepath.Join(t.TempDir(), "clone"))
		require.NoError(t, g.Clone(context.Background(), 1))
		assert.NotEmpty(t, redirectedAuth)
	})

	tests := []struct {
		name           string
		newDestination func(http.Handler) *httptest.Server
	}{
		{name: "downgrade", newDestination: httptest.NewServer},
		{name: "cross origin", newDestination: httptest.NewTLSServer},
	}
	for _, tt := range tests {
		t.Run(tt.name+" clone is rejected before destination", func(t *testing.T) {
			var destinationRequests atomic.Int32
			destination := tt.newDestination(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				destinationRequests.Add(1)
			}))
			defer destination.Close()
			var sourceAuthenticated atomic.Bool
			source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotUser, gotToken, ok := r.BasicAuth()
				sourceAuthenticated.Store(ok && gotUser == username && gotToken == token)
				http.Redirect(w, r, destination.URL+"/repo.git", http.StatusFound)
			}))
			defer source.Close()
			restoreHTTPSProtocol(t, source.Client())

			g := NewGitOps(source.URL+"/repo.git", "main", filepath.Join(t.TempDir(), "clone"))
			err := g.Clone(context.Background(), 1)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "redirect rejected")
			assert.True(t, sourceAuthenticated.Load())
			assert.Zero(t, destinationRequests.Load())
		})
	}
}

func TestGitOpsAuthenticatedHTTPSFetchRejectsUnsafeRedirects(t *testing.T) {
	const username = "fetch-redirect-user"
	const token = "fetch-redirect-token"
	t.Setenv("BOSUN_GIT_USERNAME", username)
	t.Setenv("BOSUN_GIT_TOKEN", token)

	tests := []struct {
		name           string
		newDestination func(http.Handler) *httptest.Server
	}{
		{name: "downgrade", newDestination: httptest.NewServer},
		{name: "cross origin", newDestination: httptest.NewTLSServer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serverRoot := t.TempDir()
			sourceDir := t.TempDir()
			remoteDir := filepath.Join(serverRoot, "repo.git")
			sourceRepo, sourceTree := initializeGitSource(t, sourceDir)
			_, err := sourceRepo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{remoteDir}})
			require.NoError(t, err)
			_, err = git.PlainInit(remoteDir, true)
			require.NoError(t, err)
			require.NoError(t, sourceRepo.Push(&git.PushOptions{RemoteName: "origin", RefSpecs: []config.RefSpec{"refs/heads/master:refs/heads/master"}}))

			var destinationRequests atomic.Int32
			destination := tt.newDestination(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				destinationRequests.Add(1)
			}))
			defer destination.Close()

			backend := gitHTTPBackend(t, serverRoot)
			var redirectFetch atomic.Bool
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotUser, gotToken, ok := r.BasicAuth()
				if !ok || gotUser != username || gotToken != token {
					http.Error(w, "authentication required", http.StatusUnauthorized)
					return
				}
				if redirectFetch.Load() {
					http.Redirect(w, r, destination.URL+r.URL.RequestURI(), http.StatusFound)
					return
				}
				backend.ServeHTTP(w, r)
			}))
			defer server.Close()
			restoreHTTPSProtocol(t, server.Client())

			gitOps := NewGitOps(server.URL+"/repo.git", "master", filepath.Join(t.TempDir(), "clone"))
			require.NoError(t, gitOps.Clone(context.Background(), 1))
			writeAndCommit(t, sourceTree, sourceDir, "second.txt", "second", "second commit")
			require.NoError(t, sourceRepo.Push(&git.PushOptions{RemoteName: "origin", RefSpecs: []config.RefSpec{"refs/heads/master:refs/heads/master"}}))

			redirectFetch.Store(true)
			_, _, _, err = gitOps.Pull(context.Background())
			require.ErrorContains(t, err, "redirect rejected")
			assert.Zero(t, destinationRequests.Load())
		})
	}
}

func TestGitPresentationSanitization(t *testing.T) {
	const username = "user+visible@example.com"
	const token = "token:/?secret"
	t.Setenv("BOSUN_GIT_USERNAME", username)
	t.Setenv("BOSUN_GIT_TOKEN", token)

	userinfoURL := "https://embedded%40user:embedded%3Atoken@example.com/repo.git"
	assert.Equal(t, "https://example.com/repo.git", SanitizeGitURL(userinfoURL))
	assert.Equal(t, redactedGitURL, SanitizeGitURL("https://bad%ZZ@example.com/repo.git"))

	basic := base64.StdEncoding.EncodeToString([]byte(username + ":" + token))
	unsafe := fmt.Sprintf("raw=%s token=%s query=%s path=%s auth=Basic %s url=%s",
		username, token, url.QueryEscape(token), url.PathEscape(username), basic, userinfoURL)
	safe := SanitizeGitText(unsafe)
	for _, forbidden := range []string{
		username,
		token,
		url.QueryEscape(token),
		url.PathEscape(username),
		basic,
		"embedded%40user",
		"embedded%3Atoken",
	} {
		assert.NotContains(t, safe, forbidden)
	}
	assert.Contains(t, safe, "https://example.com/repo.git")
	assert.Equal(t, "safe error", SanitizeGitError(fmt.Errorf("safe error")).Error())
	assert.NoError(t, SanitizeGitError(nil))
}

func TestGitPresentationSanitizationOverlappingCredentials(t *testing.T) {
	t.Setenv("BOSUN_GIT_USERNAME", "a")
	t.Setenv("BOSUN_GIT_TOKEN", "abc")

	safe := SanitizeGitText("username=a token=abc")
	assert.NotContains(t, safe, "abc")
	assert.NotContains(t, safe, "bc")
	assert.NotContains(t, safe, "[red[redacted]cted]")
}

func initializeGitSource(t *testing.T, dir string) (*git.Repository, *git.Worktree) {
	t.Helper()
	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	tree, err := repo.Worktree()
	require.NoError(t, err)
	writeAndCommit(t, tree, dir, "initial.txt", "initial", "initial commit")
	return repo, tree
}

func writeAndCommit(t *testing.T, tree *git.Worktree, dir, name, contents, message string) plumbing.Hash {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644))
	_, err := tree.Add(name)
	require.NoError(t, err)
	hash, err := tree.Commit(message, &git.CommitOptions{Author: &object.Signature{
		Name: "Bosun Test", Email: "bosun@example.com", When: time.Now(),
	}})
	require.NoError(t, err)
	return hash
}

func gitHTTPBackend(t *testing.T, projectRoot string) http.Handler {
	t.Helper()
	gitExecPath, err := exec.Command("git", "--exec-path").Output()
	require.NoError(t, err)
	return &cgi.Handler{
		Path: filepath.Join(strings.TrimSpace(string(gitExecPath)), "git-http-backend"),
		Env: []string{
			"GIT_HTTP_EXPORT_ALL=true",
			"GIT_PROJECT_ROOT=" + projectRoot,
		},
	}
}

func restoreHTTPSProtocol(t *testing.T, client *http.Client) {
	t.Helper()
	previous := gitclient.Protocols["https"]
	client.CheckRedirect = checkGitRedirect
	gitclient.InstallProtocol("https", githttp.NewClient(client))
	t.Cleanup(func() {
		gitclient.InstallProtocol("https", previous)
	})
}
