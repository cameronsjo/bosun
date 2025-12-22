// Package reconcile provides GitOps reconciliation functionality.
package reconcile

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	xssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// Git operation timeouts
const (
	GitCloneTimeout = 5 * time.Minute
	GitFetchTimeout = 2 * time.Minute
	GitLocalTimeout = 30 * time.Second
)

// GitOps represents git operations for the reconciliation workflow.
type GitOps struct {
	// RepoURL is the git repository URL to clone.
	RepoURL string
	// Branch is the branch to checkout/track.
	Branch string
	// Dir is the local directory for the repository.
	Dir string
}

// NewGitOps creates a new GitOps instance.
func NewGitOps(url, branch, dir string) *GitOps {
	return &GitOps{
		RepoURL: url,
		Branch:  branch,
		Dir:     dir,
	}
}

// getSSHAuth attempts to get SSH authentication from the SSH agent.
// Returns nil if no SSH agent is available (falls back to default auth).
func getSSHAuth(url string) (transport.AuthMethod, error) {
	// Only use SSH auth for SSH URLs
	if !strings.HasPrefix(url, "git@") && !strings.Contains(url, "ssh://") {
		return nil, nil
	}

	// Try to connect to SSH agent
	socket := os.Getenv("SSH_AUTH_SOCK")
	if socket == "" {
		return nil, nil
	}

	conn, err := net.Dial("unix", socket)
	if err != nil {
		return nil, nil
	}

	agentClient := agent.NewClient(conn)
	auth := &ssh.PublicKeysCallback{
		User: "git",
		Callback: func() ([]xssh.Signer, error) {
			signers, err := agentClient.Signers()
			if err != nil {
				return nil, err
			}
			return signers, nil
		},
		HostKeyCallbackHelper: ssh.HostKeyCallbackHelper{
			HostKeyCallback: xssh.InsecureIgnoreHostKey(),
		},
	}

	return auth, nil
}

// Clone clones the repository with the specified depth.
// If depth is 0, a full clone is performed.
// Uses GitCloneTimeout if the parent context has no deadline.
func (g *GitOps) Clone(ctx context.Context, depth int) error {
	if err := validateBranch(g.Branch); err != nil {
		return fmt.Errorf("invalid branch: %w", err)
	}

	// Apply timeout if context doesn't have one
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, GitCloneTimeout)
		defer cancel()
	}

	auth, err := getSSHAuth(g.RepoURL)
	if err != nil {
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
				fmt.Fprintf(os.Stderr, "warning: failed to clean up partial clone at %s: %v\n", g.Dir, removeErr)
			}
		}
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("git clone timed out after %v", GitCloneTimeout)
		}
		return fmt.Errorf("git clone failed: %w", err)
	}

	return nil
}

// Pull fetches and resets to the remote branch.
// Returns (changed, beforeCommit, afterCommit, error).
// Uses GitFetchTimeout for network operations.
func (g *GitOps) Pull(ctx context.Context) (bool, string, string, error) {
	if err := validateBranch(g.Branch); err != nil {
		return false, "", "", fmt.Errorf("invalid branch: %w", err)
	}

	// Check for uncommitted changes before doing anything
	if dirty, err := g.IsDirty(ctx); err != nil {
		return false, "", "", fmt.Errorf("failed to check repository status: %w", err)
	} else if dirty {
		return false, "", "", fmt.Errorf("repository has uncommitted changes; clean the working directory before syncing")
	}

	before, err := g.GetLatestCommit(ctx)
	if err != nil {
		return false, "", "", fmt.Errorf("failed to get current commit: %w", err)
	}

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
		Depth:      1,
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

	return before != after, before, after, nil
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
	if !g.IsRepo(ctx) {
		if err := g.Clone(ctx, 1); err != nil {
			return false, "", "", err
		}
		commit, err := g.GetLatestCommit(ctx)
		if err != nil {
			return false, "", "", err
		}
		return true, "", commit, nil
	}
	return g.Pull(ctx)
}
