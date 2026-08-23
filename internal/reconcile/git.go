// Package reconcile provides GitOps reconciliation functionality.
package reconcile

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	xssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/cameronsjo/bosun/internal/log"
)

func init() {
	// Override go-git's default SSH auth builder to gracefully handle missing SSH agents.
	// By default, go-git calls NewSSHAgentAuth() which errors if SSH_AUTH_SOCK isn't set.
	// We use our own auth chain: SSH agent -> key files (BOSUN_SSH_KEY, /config/*, ~/.ssh/*).
	ssh.DefaultAuthBuilder = func(user string) (ssh.AuthMethod, error) {
		// Try SSH agent first
		if auth := getSSHAgentAuth(); auth != nil {
			if sshAuth, ok := auth.(ssh.AuthMethod); ok {
				return sshAuth, nil
			}
		}
		// Fall back to key file auth
		keyAuth, err := getSSHKeyFileAuth()
		if err != nil {
			return nil, err
		}
		if keyAuth != nil {
			if sshAuth, ok := keyAuth.(ssh.AuthMethod); ok {
				return sshAuth, nil
			}
		}
		return nil, nil
	}
}

// Git operation timeouts
const (
	GitCloneTimeout      = 5 * time.Minute
	GitFetchTimeout      = 2 * time.Minute
	GitLocalTimeout      = 30 * time.Second
	DefaultGitFetchDepth = 1
)

// ErrCommitUnavailable indicates that a requested diff endpoint is not present
// in the local object database, usually because the checkout is shallow.
var ErrCommitUnavailable = errors.New("commit unavailable in local git history")

// GitOps represents git operations for the reconciliation workflow.
type GitOps struct {
	// RepoURL is the git repository URL to clone.
	RepoURL string
	// Branch is the branch to checkout/track.
	Branch string
	// Dir is the local directory for the repository.
	Dir string
	// FetchDepth controls clone and fetch history depth. Values below 1 use
	// DefaultGitFetchDepth.
	FetchDepth int
}

// NewGitOps creates a new GitOps instance.
func NewGitOps(url, branch, dir string) *GitOps {
	fetchDepth, err := parseGitFetchDepth(os.Getenv("BOSUN_GIT_FETCH_DEPTH"))
	if err != nil {
		logger := log.Component(log.ComponentGit)
		logger.Warn().
			Err(err).
			Str("env", "BOSUN_GIT_FETCH_DEPTH").
			Int("fallback_depth", DefaultGitFetchDepth).
			Msg("Invalid git fetch depth, using default")
	}
	return &GitOps{
		RepoURL:    url,
		Branch:     branch,
		Dir:        dir,
		FetchDepth: fetchDepth,
	}
}

func parseGitFetchDepth(value string) (int, error) {
	if value == "" {
		return DefaultGitFetchDepth, nil
	}
	depth, err := strconv.Atoi(value)
	if err != nil || depth < 1 {
		return DefaultGitFetchDepth, fmt.Errorf("must be a positive integer, got %q", value)
	}
	return depth, nil
}

func (g *GitOps) effectiveFetchDepth() int {
	if g.FetchDepth < 1 {
		return DefaultGitFetchDepth
	}
	return g.FetchDepth
}

// getSSHAuth attempts to get SSH authentication from:
// 1. SSH agent (SSH_AUTH_SOCK)
// 2. Deploy key file (BOSUN_SSH_KEY or default paths)
// Returns nil if no SSH auth is available (falls back to default auth).
func getSSHAuth(url string) (transport.AuthMethod, error) {
	// Only use SSH auth for SSH URLs
	if !strings.HasPrefix(url, "git@") && !strings.Contains(url, "ssh://") {
		return nil, nil
	}

	// Try SSH agent first
	if auth := getSSHAgentAuth(); auth != nil {
		return auth, nil
	}

	// Fall back to key file
	return getSSHKeyFileAuth()
}

// getHostKeyCallback returns an SSH host key callback for verifying server identity.
// Search order: BOSUN_SSH_KNOWN_HOSTS env var, /config/known_hosts.
// ~/.ssh/known_hosts is intentionally excluded — ephemeral user-profile entries
// (e.g. from manual ssh commands inside a container) can cause go-git key mismatches.
// If BOSUN_SSH_INSECURE_HOST_KEY=true, verification is skipped entirely.
// If no known_hosts file is found, falls back to insecure with a warning.
func getHostKeyCallback() xssh.HostKeyCallback {
	logger := log.Component(log.ComponentGit) // No ctx available in this utility function.

	if strings.EqualFold(os.Getenv("BOSUN_SSH_INSECURE_HOST_KEY"), "true") {
		logger.Warn().Msg("SSH host key verification disabled via BOSUN_SSH_INSECURE_HOST_KEY")
		return xssh.InsecureIgnoreHostKey()
	}

	knownHostsPaths := buildKnownHostsPaths(os.Getenv("BOSUN_SSH_KNOWN_HOSTS"))

	for _, path := range knownHostsPaths {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		callback, err := knownhosts.New(path)
		if err != nil {
			logger.Warn().Err(err).Str(log.FieldPath, path).Msg("Failed to parse known_hosts file, trying next")
			continue
		}
		logger.Debug().Str(log.FieldPath, path).Msg("Using known_hosts for SSH host key verification")
		return callback
	}

	logger.Warn().Msg("No known_hosts file found, SSH host key verification disabled. Set BOSUN_SSH_KNOWN_HOSTS or place known_hosts at /config/known_hosts")
	return xssh.InsecureIgnoreHostKey()
}

// getSSHAgentAuth attempts to get auth from SSH agent.
func getSSHAgentAuth() transport.AuthMethod {
	logger := log.Component(log.ComponentGit)
	socket := os.Getenv("SSH_AUTH_SOCK")
	if socket == "" {
		logger.Debug().Msg("Skipping SSH agent auth. Reason: SSH_AUTH_SOCK not set")
		return nil
	}

	conn, err := net.Dial("unix", socket)
	if err != nil {
		logger.Debug().Err(err).Str("socket", socket).Msg("Skipping SSH agent auth. Reason: cannot connect to agent socket")
		return nil
	}

	logger.Debug().Str("socket", socket).Msg("Successfully connected to SSH agent")
	agentClient := agent.NewClient(conn)
	return &ssh.PublicKeysCallback{
		User: "git",
		Callback: func() ([]xssh.Signer, error) {
			return agentClient.Signers()
		},
		HostKeyCallbackHelper: ssh.HostKeyCallbackHelper{
			HostKeyCallback: getHostKeyCallback(),
		},
	}
}

// getSSHKeyFileAuth attempts to get auth from a key file.
// Checks BOSUN_SSH_KEY env var, then common paths.
func getSSHKeyFileAuth() (transport.AuthMethod, error) {
	logger := log.Component(log.ComponentGit)
	keyPaths := buildSSHKeyPaths(os.Getenv("BOSUN_SSH_KEY"), os.Getenv("HOME"))

	for _, keyPath := range keyPaths {
		if _, err := os.Stat(keyPath); err != nil {
			logger.Debug().Str(log.FieldPath, keyPath).Msg("Skipping SSH key. Reason: file not found")
			continue
		}

		auth, err := ssh.NewPublicKeysFromFile("git", keyPath, "")
		if err != nil {
			logger.Debug().Err(err).Str(log.FieldPath, keyPath).Msg("Skipping SSH key. Reason: failed to parse key file")
			continue
		}
		auth.HostKeyCallback = getHostKeyCallback()
		logger.Debug().Str(log.FieldPath, keyPath).Msg("Successfully loaded SSH key file")
		return auth, nil
	}

	logger.Debug().Msg("No SSH key file found in any candidate path")
	return nil, nil
}

// Clone clones the repository with the specified depth.
// If depth is 0, a full clone is performed.
// Uses GitCloneTimeout if the parent context has no deadline.
func (g *GitOps) Clone(ctx context.Context, depth int) error {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentGit)

	if err := validateBranch(g.Branch); err != nil {
		return fmt.Errorf("invalid branch: %w", err)
	}

	logger.Info().
		Str(log.FieldOperation, "clone").
		Str(log.FieldBranch, g.Branch).
		Str(log.FieldPath, g.Dir).
		Int("depth", depth).
		Msg("Cloning repository")

	// Apply timeout if context doesn't have one
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, GitCloneTimeout)
		defer cancel()
	}

	auth, err := getSSHAuth(g.RepoURL)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get SSH auth")
		return fmt.Errorf("failed to get SSH auth: %w", err)
	}

	cloneOpts := &git.CloneOptions{
		URL:           g.RepoURL,
		ReferenceName: plumbing.NewBranchReferenceName(g.Branch),
		SingleBranch:  true,
		Auth:          auth,
	}

	if depth > 0 {
		cloneOpts.Depth = depth
	}

	_, err = git.PlainCloneContext(ctx, g.Dir, false, cloneOpts)
	if err != nil {
		// Clean up partial clone on failure
		if _, statErr := os.Stat(g.Dir); statErr == nil {
			if removeErr := os.RemoveAll(g.Dir); removeErr != nil {
				logger.Warn().Err(removeErr).Str(log.FieldPath, g.Dir).Msg("Failed to clean up partial clone")
			}
		}
		if ctx.Err() == context.DeadlineExceeded {
			logger.Error().
				Str(log.FieldOperation, "clone").
				Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
				Msg("Git clone timed out")
			return fmt.Errorf("git clone timed out after %v", GitCloneTimeout)
		}
		logger.Error().
			Err(err).
			Str(log.FieldOperation, "clone").
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("Git clone failed")
		return fmt.Errorf("git clone failed: %w", err)
	}

	logger.Info().
		Str(log.FieldOperation, "clone").
		Str(log.FieldBranch, g.Branch).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Repository cloned successfully")

	return nil
}

// Pull fetches and resets to the remote branch.
// Returns (changed, beforeCommit, afterCommit, error).
// Uses GitFetchTimeout for network operations.
func (g *GitOps) Pull(ctx context.Context) (bool, string, string, error) {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentGit)

	if err := validateBranch(g.Branch); err != nil {
		logger.Error().Err(err).Str(log.FieldBranch, g.Branch).Msg("Failed to pull repository. Reason: invalid branch")
		return false, "", "", fmt.Errorf("invalid branch: %w", err)
	}

	logger.Debug().
		Str(log.FieldBranch, g.Branch).
		Str(log.FieldURL, g.RepoURL).
		Msg("Preparing to pull repository")

	// Check for uncommitted changes. Warn but continue — the hard reset below
	// will discard them. These are typically stale artifacts from a previous
	// reconciliation (template renders, symlink diffs on FUSE mounts, etc.).
	if dirty, err := g.IsDirty(ctx); err != nil {
		logger.Warn().Err(err).Str(log.FieldPath, g.Dir).Msg("Could not check repository status, proceeding with pull")
	} else if dirty {
		logger.Warn().Str(log.FieldPath, g.Dir).Msg("Repository has uncommitted changes, will be discarded by hard reset")
	}

	before, err := g.GetLatestCommit(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to pull repository. Reason: cannot get current commit")
		return false, "", "", fmt.Errorf("failed to get current commit: %w", err)
	}
	logger.Debug().Str(log.FieldCommit, before).Msg("Current HEAD commit")

	// Open the repository
	repo, err := git.PlainOpen(g.Dir)
	if err != nil {
		return false, "", "", fmt.Errorf("failed to open repository: %w", err)
	}

	auth, authErr := getSSHAuth(g.RepoURL)
	if authErr != nil {
		return false, "", "", fmt.Errorf("failed to get SSH auth: %w", authErr)
	}

	// Fetch with timeout
	fetchCtx, fetchCancel := context.WithTimeout(ctx, GitFetchTimeout)
	defer fetchCancel()

	fetchOpts := &git.FetchOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{config.RefSpec(fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", g.Branch, g.Branch))},
		Depth:      g.effectiveFetchDepth(),
		Auth:       auth,
	}

	if err := repo.FetchContext(fetchCtx, fetchOpts); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		if fetchCtx.Err() == context.DeadlineExceeded {
			return false, "", "", fmt.Errorf("git fetch timed out after %v", GitFetchTimeout)
		}
		return false, "", "", fmt.Errorf("git fetch failed: %w", err)
	}

	// Verify that origin/branch exists after fetch
	if exists, err := g.RemoteBranchExists(ctx, g.Branch); err != nil {
		return false, "", "", fmt.Errorf("failed to verify remote branch: %w", err)
	} else if !exists {
		return false, "", "", fmt.Errorf("remote branch origin/%s does not exist", g.Branch)
	}

	// Get worktree and reset to remote branch
	worktree, err := repo.Worktree()
	if err != nil {
		return false, "", "", fmt.Errorf("failed to get worktree: %w", err)
	}

	// Get the remote branch reference
	remoteRef, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", g.Branch), true)
	if err != nil {
		return false, "", "", fmt.Errorf("failed to get remote reference: %w", err)
	}

	// Reset to remote branch (hard reset)
	resetCtx, resetCancel := context.WithTimeout(ctx, GitLocalTimeout)
	defer resetCancel()

	// Check context before reset (go-git Reset doesn't take context)
	select {
	case <-resetCtx.Done():
		return false, "", "", fmt.Errorf("context cancelled before reset")
	default:
	}

	err = worktree.Reset(&git.ResetOptions{
		Commit: remoteRef.Hash(),
		Mode:   git.HardReset,
	})
	if err != nil {
		return false, "", "", fmt.Errorf("git reset failed: %w", err)
	}

	after, err := g.GetLatestCommit(ctx)
	if err != nil {
		return false, "", "", fmt.Errorf("failed to get new commit: %w", err)
	}

	changed := before != after
	logger.Info().
		Str(log.FieldOperation, "pull").
		Str(log.FieldBranch, g.Branch).
		Bool("changed", changed).
		Str("commit_before", before[:7]).
		Str("commit_after", after[:7]).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Pull completed")

	return changed, before, after, nil
}

// IsDirty checks if the repository has uncommitted changes.
func (g *GitOps) IsDirty(ctx context.Context) (bool, error) {
	repo, err := git.PlainOpen(g.Dir)
	if err != nil {
		return false, fmt.Errorf("failed to open repository: %w", err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return false, fmt.Errorf("failed to get worktree: %w", err)
	}

	// Check context before status (go-git Status doesn't take context)
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	status, err := worktree.Status()
	if err != nil {
		return false, fmt.Errorf("failed to get status: %w", err)
	}

	// If status is not clean, there are uncommitted changes
	return !status.IsClean(), nil
}

// RemoteBranchExists checks if a remote branch exists.
func (g *GitOps) RemoteBranchExists(ctx context.Context, branch string) (bool, error) {
	repo, err := git.PlainOpen(g.Dir)
	if err != nil {
		return false, fmt.Errorf("failed to open repository: %w", err)
	}

	// Check context
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	refName := plumbing.NewRemoteReferenceName("origin", branch)
	_, err = repo.Reference(refName, true)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("failed to get reference: %w", err)
	}

	return true, nil
}

// GetLatestCommit returns the current HEAD commit hash.
func (g *GitOps) GetLatestCommit(ctx context.Context) (string, error) {
	repo, err := git.PlainOpen(g.Dir)
	if err != nil {
		return "", fmt.Errorf("failed to open repository: %w", err)
	}

	// Check context
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	head, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD: %w", err)
	}

	return head.Hash().String(), nil
}

// GetCommitMessage returns the commit message for the current HEAD.
func (g *GitOps) GetCommitMessage(ctx context.Context) (string, error) {
	repo, err := git.PlainOpen(g.Dir)
	if err != nil {
		return "", fmt.Errorf("failed to open repository: %w", err)
	}

	// Check context
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	head, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD: %w", err)
	}

	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return "", fmt.Errorf("failed to get commit: %w", err)
	}

	// Format similar to git log --oneline: "<short-hash> <subject>"
	shortHash := head.Hash().String()[:7]
	subject := strings.Split(commit.Message, "\n")[0]
	return fmt.Sprintf("%s %s", shortHash, subject), nil
}

// IsRepoCheckTimeout is the timeout for checking if a directory is a git repository.
const IsRepoCheckTimeout = 2 * time.Second

// IsRepo checks if the directory is a git repository.
// Uses the provided context for timeout control.
func (g *GitOps) IsRepo(ctx context.Context) bool {
	// Apply a short timeout for this check if context doesn't have one
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, IsRepoCheckTimeout)
		defer cancel()
	}

	// Check context
	select {
	case <-ctx.Done():
		return false
	default:
	}

	// Try to open as a git repository
	_, err := git.PlainOpen(g.Dir)
	if err == nil {
		return true
	}

	// Fall back to checking if .git directory exists
	gitDir := filepath.Join(g.Dir, ".git")
	info, statErr := os.Stat(gitDir)
	return statErr == nil && info.IsDir()
}

// Sync clones or pulls depending on whether repo exists.
// Returns (changed, beforeCommit, afterCommit, error).
// For fresh clones, changed is always true.
func (g *GitOps) Sync(ctx context.Context) (bool, string, string, error) {
	logger := log.ComponentCtx(ctx, log.ComponentGit)

	if !g.IsRepo(ctx) {
		logger.Info().Str(log.FieldPath, g.Dir).Msg("Repository not found, cloning")
		if err := g.Clone(ctx, g.effectiveFetchDepth()); err != nil {
			return false, "", "", err
		}
		commit, err := g.GetLatestCommit(ctx)
		if err != nil {
			return false, "", "", err
		}
		logger.Info().Str(log.FieldCommit, commit[:7]).Msg("Fresh clone completed")
		return true, "", commit, nil
	}
	logger.Debug().Str(log.FieldPath, g.Dir).Msg("Repository exists, pulling")
	return g.Pull(ctx)
}

// DiffFiles returns the list of changed file paths between two commits.
// If fromCommit is empty, returns all files in toCommit.
func (g *GitOps) DiffFiles(ctx context.Context, fromCommit, toCommit string) ([]string, error) {
	logger := log.ComponentCtx(ctx, log.ComponentGit)

	logger.Debug().
		Str(log.FieldOperation, "diff").
		Str("from_commit", fromCommit[:MinLen(fromCommit, 8)]).
		Str("to_commit", toCommit[:MinLen(toCommit, 8)]).
		Msg("Preparing to diff commits")

	repo, err := git.PlainOpen(g.Dir)
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}

	toHash := plumbing.NewHash(toCommit)
	toObj, err := repo.CommitObject(toHash)
	if err != nil {
		return nil, fmt.Errorf("resolve to-commit %s: %w", toCommit[:MinLen(toCommit, 8)], err)
	}
	toTree, err := toObj.Tree()
	if err != nil {
		return nil, fmt.Errorf("get tree for %s: %w", toCommit[:MinLen(toCommit, 8)], err)
	}

	var fromTree *object.Tree
	if fromCommit != "" {
		fromHash := plumbing.NewHash(fromCommit)
		fromObj, err := repo.CommitObject(fromHash)
		if err != nil {
			if errors.Is(err, plumbing.ErrObjectNotFound) {
				return nil, fmt.Errorf("%w: resolve from-commit %s: %v",
					ErrCommitUnavailable, fromCommit[:MinLen(fromCommit, 8)], err)
			}
			return nil, fmt.Errorf("resolve from-commit %s: %w", fromCommit[:MinLen(fromCommit, 8)], err)
		}
		fromTree, err = fromObj.Tree()
		if err != nil {
			if errors.Is(err, plumbing.ErrObjectNotFound) {
				return nil, fmt.Errorf("%w: get tree for %s: %v",
					ErrCommitUnavailable, fromCommit[:MinLen(fromCommit, 8)], err)
			}
			return nil, fmt.Errorf("get tree for %s: %w", fromCommit[:MinLen(fromCommit, 8)], err)
		}
	}

	changes, err := object.DiffTree(fromTree, toTree)
	if err != nil {
		return nil, fmt.Errorf("diff trees: %w", err)
	}

	var files []string
	for _, change := range changes {
		name := change.To.Name
		if name == "" {
			name = change.From.Name
		}
		files = append(files, name)
	}

	logger.Debug().
		Str(log.FieldOperation, "diff").
		Int("changed_files", len(files)).
		Msg("Successfully diffed commits")

	return files, nil
}
