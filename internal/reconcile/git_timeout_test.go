package reconcile

import (
	"context"
	"net/url"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	xssh "golang.org/x/crypto/ssh"
)

// blackholedSSHRemote is an RFC 5737 TEST-NET-1 address. Connections to it are
// dropped rather than refused, so a dial against it hangs until something
// bounds it -- which is exactly the condition under test.
const blackholedSSHRemote = "ssh://git@192.0.2.1:22/test/repo.git"

// stubSSHAuth is a minimal ssh.AuthMethod carrying the four fields the dial
// timeout must not destroy.
type stubSSHAuth struct {
	cfg *xssh.ClientConfig
}

func (s *stubSSHAuth) Name() string   { return "stub-ssh" }
func (s *stubSSHAuth) String() string { return "stub-ssh" }
func (s *stubSSHAuth) ClientConfig() (*xssh.ClientConfig, error) {
	return s.cfg, nil
}

// closableSSHAuth additionally implements io.Closer, mirroring the real
// ssh-agent auth whose socket closeGitAuth must still close through the wrapper.
type closableSSHAuth struct {
	stubSSHAuth
	closed *bool
}

func (c *closableSSHAuth) Close() error {
	*c.closed = true
	return nil
}

func newStubClientConfig() *xssh.ClientConfig {
	return &xssh.ClientConfig{
		User:              "git",
		Auth:              []xssh.AuthMethod{xssh.Password("unused")},
		HostKeyCallback:   xssh.InsecureIgnoreHostKey(),
		HostKeyAlgorithms: []string{"ssh-ed25519"},
	}
}

// TestWithSSHDialTimeoutPreservesAuthFields is the test that catches the
// declined InstallProtocol approach.
//
// It asserts on the returned ClientConfig rather than by running a fetch on
// purpose: internal/reconcile has no live-SSH fixture, and a local-path or
// file:// remote never enters the SSH transport at all -- so a fetch-shaped
// test stays green under an implementation that blanks every auth field, and
// catches nothing.
func TestWithSSHDialTimeoutPreservesAuthFields(t *testing.T) {
	original := newStubClientConfig()
	auth := &stubSSHAuth{cfg: original}

	wrapped := withSSHDialTimeout(auth, 7*time.Second)

	sshWrapped, ok := wrapped.(ssh.AuthMethod)
	require.True(t, ok, "wrapped auth must still satisfy ssh.AuthMethod or go-git will not use it")

	cfg, err := sshWrapped.ClientConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "git", cfg.User, "User must survive the wrap")
	assert.Len(t, cfg.Auth, 1, "Auth must survive the wrap")
	assert.NotNil(t, cfg.HostKeyCallback, "HostKeyCallback must survive the wrap: blanking it fails every fetch with 'ssh: must specify HostKeyCallback'")
	assert.Equal(t, []string{"ssh-ed25519"}, cfg.HostKeyAlgorithms, "HostKeyAlgorithms must survive the wrap")
	assert.Equal(t, 7*time.Second, cfg.Timeout, "the dial timeout must actually be applied")
}

func TestWithSSHDialTimeoutForwardsClose(t *testing.T) {
	closed := false
	auth := &closableSSHAuth{
		stubSSHAuth: stubSSHAuth{cfg: newStubClientConfig()},
		closed:      &closed,
	}

	wrapped := withSSHDialTimeout(auth, time.Second)
	require.NoError(t, closeGitAuth(wrapped))
	assert.True(t, closed, "the wrapper must forward Close or the ssh-agent socket leaks")
}

func TestWithSSHDialTimeoutLeavesNonSSHAuthAlone(t *testing.T) {
	t.Run("nil auth", func(t *testing.T) {
		assert.Nil(t, withSSHDialTimeout(nil, time.Second))
	})

	t.Run("non-positive timeout is a no-op", func(t *testing.T) {
		auth := &stubSSHAuth{cfg: newStubClientConfig()}
		assert.Same(t, transport.AuthMethod(auth), withSSHDialTimeout(auth, 0))
	})
}

func TestGitOpsTimeoutDefaults(t *testing.T) {
	t.Run("unset uses package defaults", func(t *testing.T) {
		g := NewGitOps("ssh://git@example.com/x.git", "main", t.TempDir())
		assert.Equal(t, GitCloneTimeout, g.effectiveCloneTimeout())
		assert.Equal(t, GitFetchTimeout, g.effectiveFetchTimeout())
		assert.Equal(t, GitSSHDialTimeout, g.effectiveSSHDialTimeout())
	})

	t.Run("explicit values are honoured", func(t *testing.T) {
		g := &GitOps{CloneTimeout: time.Second, FetchTimeout: 2 * time.Second, SSHDialTimeout: 3 * time.Second}
		assert.Equal(t, time.Second, g.effectiveCloneTimeout())
		assert.Equal(t, 2*time.Second, g.effectiveFetchTimeout())
		assert.Equal(t, 3*time.Second, g.effectiveSSHDialTimeout())
	})

	t.Run("non-positive falls back rather than expiring instantly", func(t *testing.T) {
		g := &GitOps{CloneTimeout: 0, FetchTimeout: -time.Second, SSHDialTimeout: -1}
		assert.Equal(t, GitCloneTimeout, g.effectiveCloneTimeout())
		assert.Equal(t, GitFetchTimeout, g.effectiveFetchTimeout())
		assert.Equal(t, GitSSHDialTimeout, g.effectiveSSHDialTimeout())
	})
}

// initRepoWithOrigin builds the fixture Pull needs: a real repository with a
// commit and an origin remote. Pull runs validateBranch, IsDirty,
// GetLatestCommit and PlainOpen before it ever reaches the fetch.
func initRepoWithOrigin(t *testing.T, remoteURL string) string {
	t.Helper()
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = resolved
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, runErr := cmd.CombinedOutput()
		require.NoError(t, runErr, "git %v: %s", args, out)
	}

	run("init", "--initial-branch=main")
	run("commit", "--allow-empty", "-m", "seed")
	run("remote", "add", "origin", remoteURL)
	return resolved
}

// TestPullBoundedByDialTimeout is the enforcement proof.
//
// Against origin/main this hangs: nothing set ssh.ClientConfig.Timeout, so the
// dial to a blackholed address blocked until the kernel gave up, well past the
// declared GitFetchTimeout that the error message nonetheless named.
func TestPullBoundedByDialTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("network dial timing")
	}

	dir := initRepoWithOrigin(t, blackholedSSHRemote)

	g := NewGitOps(blackholedSSHRemote, "main", dir)
	g.SSHDialTimeout = 400 * time.Millisecond
	g.FetchTimeout = 30 * time.Second // deliberately far larger than the dial bound
	g.authResolver = func(string) (transport.AuthMethod, error) {
		return &stubSSHAuth{cfg: newStubClientConfig()}, nil
	}

	start := time.Now()
	_, _, _, err := g.Pull(context.Background())
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Less(t, elapsed, 5*time.Second,
		"the dial must be bounded by SSHDialTimeout, not left to the kernel or the fetch bound")
	assert.Greater(t, elapsed, 300*time.Millisecond,
		"a failure faster than the bound means something other than the timeout ended it")
}

// TestCloneTimeoutAppliesUnderLongerCallerDeadline covers the defect with the
// widest blast radius: Clone used to apply its own bound only when the caller
// context carried no deadline, and daemon.go wraps every reconcile cycle in a
// ReconcileTimeout deadline. On the daemon path the declared clone bound was
// therefore never applied, while the error text named it regardless.
func TestCloneTimeoutAppliesUnderLongerCallerDeadline(t *testing.T) {
	if testing.Short() {
		t.Skip("network dial timing")
	}

	dir := filepath.Join(t.TempDir(), "checkout")

	g := NewGitOps(blackholedSSHRemote, "main", dir)
	g.CloneTimeout = 500 * time.Millisecond
	g.SSHDialTimeout = 30 * time.Second // must not be what bounds this
	g.authResolver = func(string) (transport.AuthMethod, error) {
		return &stubSSHAuth{cfg: newStubClientConfig()}, nil
	}

	// The caller's deadline is much longer than CloneTimeout -- the daemon's shape.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	err := g.Clone(ctx, 1)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Less(t, elapsed, 5*time.Second,
		"CloneTimeout must apply even though the caller context already carried a longer deadline, and must cap the dial: before this fix the 30s SSHDialTimeout won and the clone took 30s")
	assert.Contains(t, err.Error(), "timed out",
		"the failure must be reported as a timeout")
}

func TestSanitizeGitURLRedactsQueryCredentials(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		mustNotHave string
		mustHave    string
	}{
		{
			name:        "token parameter",
			in:          "https://git.example.com/org/repo.git?token=s3cr3t-value",
			mustNotHave: "s3cr3t-value",
			mustHave:    "git.example.com/org/repo.git",
		},
		{
			name:        "access_token parameter",
			in:          "https://git.example.com/org/repo.git?access_token=abc123&ref=main",
			mustNotHave: "abc123",
			mustHave:    "ref=main",
		},
		{
			name:        "password parameter, mixed case",
			in:          "https://git.example.com/org/repo.git?Password=hunter2",
			mustNotHave: "hunter2",
			mustHave:    "git.example.com",
		},
		{
			name:        "userinfo still stripped",
			in:          "https://user:pw@git.example.com/org/repo.git",
			mustNotHave: "pw@",
			mustHave:    "git.example.com",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeGitURL(tc.in)
			assert.NotContains(t, got, tc.mustNotHave, "credential leaked into the sanitized URL")
			assert.Contains(t, got, tc.mustHave, "the repository must stay identifiable")
		})
	}
}

func TestSanitizeGitURLKeepsBenignQuery(t *testing.T) {
	got := SanitizeGitURL("https://git.example.com/org/repo.git?ref=main&depth=1")
	parsed, err := url.Parse(got)
	require.NoError(t, err)
	assert.Equal(t, "main", parsed.Query().Get("ref"))
	assert.Equal(t, "1", parsed.Query().Get("depth"))
}

// TestSanitizeGitURLRedactsUnknownParameters pins the allowlist inversion.
//
// A denylist of credential-looking names is never done: pat, jwt, bearer, sas
// and code are all real credential parameter names that a reasonable denylist
// misses, and the remediation is always "add one more". These cases exist so a
// future change back to a denylist fails here rather than in production.
func TestSanitizeGitURLRedactsUnknownParameters(t *testing.T) {
	for _, param := range []string{"pat", "jwt", "bearer", "sas", "code", "somethingnobodyhasthoughtof"} {
		t.Run(param, func(t *testing.T) {
			got := SanitizeGitURL("https://git.example.com/org/repo.git?" + param + "=s3cr3t-value")
			assert.NotContains(t, got, "s3cr3t-value",
				"every parameter value is redacted unless explicitly allowed")
			assert.Contains(t, got, "git.example.com/org/repo.git")
		})
	}
}

func TestSanitizeGitURLDropsFragment(t *testing.T) {
	got := SanitizeGitURL("https://git.example.com/org/repo.git#token=s3cr3t-value")
	assert.NotContains(t, got, "s3cr3t-value")
	assert.Contains(t, got, "git.example.com/org/repo.git")
}

// TestSanitizeGitURLKeepsSCPStyleLegible covers the form homelab actually uses.
// Before this, url.Parse rejected it and the timeout log said only "[redacted
// invalid repository URL]" -- losing repo identity in exactly the incident the
// log line exists to explain.
func TestSanitizeGitURLKeepsSCPStyleLegible(t *testing.T) {
	got := SanitizeGitURL("git@github.com:cameronsjo/homelab.git")
	assert.Equal(t, "ssh://github.com/cameronsjo/homelab.git", got)
	assert.NotContains(t, got, redactedGitURL)
}

func TestSanitizeGitURLStillRedactsTrulyMalformed(t *testing.T) {
	assert.Equal(t, redactedGitURL, SanitizeGitURL("ht tp://%zz"))
}

// TestFetchTimeoutCapsTheDial is the regression test for a bug this change
// introduced and code review caught: the dial cap was computed from the caller
// context, but Pull resolved its auth method *before* creating the fetch
// context, so a configured FetchTimeout never reached the dial. The effective
// bound was the larger of the two -- the exact failure the cap exists to stop.
func TestFetchTimeoutCapsTheDial(t *testing.T) {
	if testing.Short() {
		t.Skip("network dial timing")
	}

	dir := initRepoWithOrigin(t, blackholedSSHRemote)

	g := NewGitOps(blackholedSSHRemote, "main", dir)
	g.FetchTimeout = 400 * time.Millisecond
	g.SSHDialTimeout = 30 * time.Second // must NOT be what bounds this
	g.authResolver = func(string) (transport.AuthMethod, error) {
		return &stubSSHAuth{cfg: newStubClientConfig()}, nil
	}

	start := time.Now()
	_, _, _, err := g.Pull(context.Background())
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Less(t, elapsed, 5*time.Second,
		"FetchTimeout must cap the dial; without the cap this waits the full 30s SSHDialTimeout")
}

func TestDialTimeoutFor(t *testing.T) {
	g := &GitOps{SSHDialTimeout: 10 * time.Second}

	t.Run("operation timeout caps the dial", func(t *testing.T) {
		assert.Equal(t, 2*time.Second, g.dialTimeoutFor(context.Background(), 2*time.Second))
	})

	t.Run("dial timeout wins when it is smaller", func(t *testing.T) {
		assert.Equal(t, 10*time.Second, g.dialTimeoutFor(context.Background(), time.Minute))
	})

	t.Run("an expired context fails fast rather than waiting the full dial", func(t *testing.T) {
		// go-git's dial ignores the context entirely, so without a floor an
		// operation entered with a dead deadline still burns the dial timeout.
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
		defer cancel()
		assert.Equal(t, minDialTimeout, g.dialTimeoutFor(ctx, time.Minute))
	})
}

func TestEffectiveBound(t *testing.T) {
	t.Run("no deadline yields the operation timeout", func(t *testing.T) {
		assert.Equal(t, time.Minute, effectiveBound(context.Background(), time.Minute))
	})

	t.Run("an earlier caller deadline wins", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		assert.Less(t, effectiveBound(ctx, time.Minute), time.Minute,
			"the error must name the bound that actually expires, not the one that did not")
	})
}
