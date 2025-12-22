// Package reconcile provides GitOps reconciliation functionality.
package reconcile

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
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

// Clone clones the repository with the specified depth.
// If depth is 0, a full clone is performed.
func (g *GitOps) Clone(ctx context.Context, depth int) error {
	args := []string{"clone", "--branch", g.Branch, "--single-branch"}
	if depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", depth))
	}
	args = append(args, g.RepoURL, g.Dir)

	cmd := exec.CommandContext(ctx, "git", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w: %s", err, stderr.String())
	}
	return nil
}

// Pull fetches and resets to the remote branch.
// Returns (changed, beforeCommit, afterCommit, error).
func (g *GitOps) Pull(ctx context.Context) (bool, string, string, error) {
	before, err := g.GetLatestCommit(ctx)
	if err != nil {
		return false, "", "", fmt.Errorf("failed to get current commit: %w", err)
	}

	// Fetch with depth 1 to minimize data transfer.
	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin", g.Branch, "--depth", "1")
	fetchCmd.Dir = g.Dir
	var fetchStderr bytes.Buffer
	fetchCmd.Stderr = &fetchStderr
	if err := fetchCmd.Run(); err != nil {
		return false, "", "", fmt.Errorf("git fetch failed: %w: %s", err, fetchStderr.String())
	}

	// Reset to remote branch.
	resetCmd := exec.CommandContext(ctx, "git", "reset", "--hard", "origin/"+g.Branch)
	resetCmd.Dir = g.Dir
	var resetStderr bytes.Buffer
	resetCmd.Stderr = &resetStderr
	if err := resetCmd.Run(); err != nil {
		return false, "", "", fmt.Errorf("git reset failed: %w: %s", err, resetStderr.String())
	}

	after, err := g.GetLatestCommit(ctx)
	if err != nil {
		return false, "", "", fmt.Errorf("failed to get new commit: %w", err)
	}

	return before != after, before, after, nil
}

// GetLatestCommit returns the current HEAD commit hash.
func (g *GitOps) GetLatestCommit(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = g.Dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w: %s", err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// GetCommitMessage returns the commit message for the current HEAD.
func (g *GitOps) GetCommitMessage(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "log", "--oneline", "-1")
	cmd.Dir = g.Dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git log failed: %w: %s", err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// IsRepo checks if the directory is a git repository.
func (g *GitOps) IsRepo() bool {
	gitDir := filepath.Join(g.Dir, ".git")
	cmd := exec.Command("test", "-d", gitDir)
	return cmd.Run() == nil
}

// Sync clones or pulls depending on whether repo exists.
// Returns (changed, beforeCommit, afterCommit, error).
// For fresh clones, changed is always true.
func (g *GitOps) Sync(ctx context.Context) (bool, string, string, error) {
	if !g.IsRepo() {
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
