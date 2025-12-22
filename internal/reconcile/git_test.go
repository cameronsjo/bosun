package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGitOps(t *testing.T) {
	gitOps := NewGitOps("https://github.com/test/repo.git", "main", "/tmp/test")

	assert.Equal(t, "https://github.com/test/repo.git", gitOps.RepoURL)
	assert.Equal(t, "main", gitOps.Branch)
	assert.Equal(t, "/tmp/test", gitOps.Dir)
}

func TestGitOps_IsRepo(t *testing.T) {
	ctx := context.Background()

	t.Run("not a repo", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitOps := NewGitOps("", "", tmpDir)

		assert.False(t, gitOps.IsRepo(ctx))
	})

	t.Run("is a repo", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Initialize repo using go-git
		_, err := git.PlainInit(tmpDir, false)
		require.NoError(t, err)

		gitOps := NewGitOps("", "", tmpDir)
		assert.True(t, gitOps.IsRepo(ctx))
	})
}

func TestGitOps_Clone(t *testing.T) {
	t.Run("clone valid repo", func(t *testing.T) {
		// Create a source repository with a commit
		sourceDir := t.TempDir()
		targetDir := filepath.Join(t.TempDir(), "cloned")

		// Initialize source repo with go-git and create a commit
		repo, err := git.PlainInit(sourceDir, false)
		require.NoError(t, err)

		// Create a file and commit
		testFile := filepath.Join(sourceDir, "test.txt")
		require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))

		worktree, err := repo.Worktree()
		require.NoError(t, err)

		_, err = worktree.Add("test.txt")
		require.NoError(t, err)

		_, err = worktree.Commit("initial commit", &git.CommitOptions{
			Author: &object.Signature{
				Name:  "Test User",
				Email: "test@test.com",
				When:  time.Now(),
			},
		})
		require.NoError(t, err)

		gitOps := NewGitOps(sourceDir, "master", targetDir)
		ctx := context.Background()

		err = gitOps.Clone(ctx, 1)
		require.NoError(t, err)

		// Verify the clone succeeded
		assert.True(t, gitOps.IsRepo(ctx))
	})

	t.Run("clone with invalid url", func(t *testing.T) {
		targetDir := filepath.Join(t.TempDir(), "cloned")
		gitOps := NewGitOps("invalid://not-a-url", "main", targetDir)
		ctx := context.Background()

		err := gitOps.Clone(ctx, 1)
		assert.Error(t, err)
	})
}

func TestGitOps_GetLatestCommit(t *testing.T) {
	t.Run("valid repo with commits", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		// Initialize repo with go-git
		repo, err := git.PlainInit(tmpDir, false)
		require.NoError(t, err)

		// Create a file and commit
		testFile := filepath.Join(tmpDir, "test.txt")
		require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))

		worktree, err := repo.Worktree()
		require.NoError(t, err)

		_, err = worktree.Add("test.txt")
		require.NoError(t, err)

		_, err = worktree.Commit("initial commit", &git.CommitOptions{
			Author: &object.Signature{
				Name:  "Test User",
				Email: "test@test.com",
				When:  time.Now(),
			},
		})
		require.NoError(t, err)

		gitOps := NewGitOps("", "", tmpDir)
		commit, err := gitOps.GetLatestCommit(ctx)

		require.NoError(t, err)
		assert.Len(t, commit, 40) // SHA-1 hash length
	})

	t.Run("no commits", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		// Initialize repo without any commits
		_, err := git.PlainInit(tmpDir, false)
		require.NoError(t, err)

		gitOps := NewGitOps("", "", tmpDir)
		_, err = gitOps.GetLatestCommit(ctx)

		assert.Error(t, err)
	})
}

func TestGitOps_GetCommitMessage(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	// Initialize repo with go-git
	repo, err := git.PlainInit(tmpDir, false)
	require.NoError(t, err)

	// Create a file and commit with specific message
	testFile := filepath.Join(tmpDir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))

	worktree, err := repo.Worktree()
	require.NoError(t, err)

	_, err = worktree.Add("test.txt")
	require.NoError(t, err)

	_, err = worktree.Commit("test commit message", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test User",
			Email: "test@test.com",
			When:  time.Now(),
		},
	})
	require.NoError(t, err)

	gitOps := NewGitOps("", "", tmpDir)
	msg, err := gitOps.GetCommitMessage(ctx)

	require.NoError(t, err)
	assert.Contains(t, msg, "test commit message")
}

func TestGitOps_IsDirty(t *testing.T) {
	ctx := context.Background()

	t.Run("clean repo", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Initialize repo with go-git and commit
		repo, err := git.PlainInit(tmpDir, false)
		require.NoError(t, err)

		testFile := filepath.Join(tmpDir, "test.txt")
		require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))

		worktree, err := repo.Worktree()
		require.NoError(t, err)

		_, err = worktree.Add("test.txt")
		require.NoError(t, err)

		_, err = worktree.Commit("initial commit", &git.CommitOptions{
			Author: &object.Signature{
				Name:  "Test User",
				Email: "test@test.com",
				When:  time.Now(),
			},
		})
		require.NoError(t, err)

		gitOps := NewGitOps("", "", tmpDir)
		dirty, err := gitOps.IsDirty(ctx)
		require.NoError(t, err)
		assert.False(t, dirty)
	})

	t.Run("dirty repo", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Initialize repo with go-git and commit
		repo, err := git.PlainInit(tmpDir, false)
		require.NoError(t, err)

		testFile := filepath.Join(tmpDir, "test.txt")
		require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))

		worktree, err := repo.Worktree()
		require.NoError(t, err)

		_, err = worktree.Add("test.txt")
		require.NoError(t, err)

		_, err = worktree.Commit("initial commit", &git.CommitOptions{
			Author: &object.Signature{
				Name:  "Test User",
				Email: "test@test.com",
				When:  time.Now(),
			},
		})
		require.NoError(t, err)

		// Now make a change without committing
		require.NoError(t, os.WriteFile(testFile, []byte("modified"), 0644))

		gitOps := NewGitOps("", "", tmpDir)
		dirty, err := gitOps.IsDirty(ctx)
		require.NoError(t, err)
		assert.True(t, dirty)
	})
}

func TestGitOps_Sync(t *testing.T) {
	t.Run("sync clones when repo doesn't exist", func(t *testing.T) {
		// Create source repo with a commit
		sourceDir := t.TempDir()
		ctx := context.Background()

		repo, err := git.PlainInit(sourceDir, false)
		require.NoError(t, err)

		testFile := filepath.Join(sourceDir, "test.txt")
		require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))

		worktree, err := repo.Worktree()
		require.NoError(t, err)

		_, err = worktree.Add("test.txt")
		require.NoError(t, err)

		_, err = worktree.Commit("initial", &git.CommitOptions{
			Author: &object.Signature{
				Name:  "Test User",
				Email: "test@test.com",
				When:  time.Now(),
			},
		})
		require.NoError(t, err)

		targetDir := filepath.Join(t.TempDir(), "target")
		gitOps := NewGitOps(sourceDir, "master", targetDir)

		changed, before, after, err := gitOps.Sync(ctx)
		require.NoError(t, err)

		assert.True(t, changed)
		assert.Empty(t, before)
		assert.NotEmpty(t, after)
	})

	t.Run("sync pulls when repo exists", func(t *testing.T) {
		// Create source repo
		sourceDir := t.TempDir()
		ctx := context.Background()

		repo, err := git.PlainInit(sourceDir, false)
		require.NoError(t, err)

		testFile := filepath.Join(sourceDir, "test.txt")
		require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))

		worktree, err := repo.Worktree()
		require.NoError(t, err)

		_, err = worktree.Add("test.txt")
		require.NoError(t, err)

		_, err = worktree.Commit("initial", &git.CommitOptions{
			Author: &object.Signature{
				Name:  "Test User",
				Email: "test@test.com",
				When:  time.Now(),
			},
		})
		require.NoError(t, err)

		// Clone to target
		targetDir := filepath.Join(t.TempDir(), "target")
		gitOps := NewGitOps(sourceDir, "master", targetDir)

		_, _, _, err = gitOps.Sync(ctx)
		require.NoError(t, err)

		// Now Sync again (should pull, no changes)
		changed, _, _, err := gitOps.Sync(ctx)
		require.NoError(t, err)
		assert.False(t, changed)
	})
}

func TestGitOps_Pull(t *testing.T) {
	// Create source repo
	sourceDir := t.TempDir()
	ctx := context.Background()

	repo, err := git.PlainInit(sourceDir, false)
	require.NoError(t, err)

	testFile := filepath.Join(sourceDir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))

	worktree, err := repo.Worktree()
	require.NoError(t, err)

	_, err = worktree.Add("test.txt")
	require.NoError(t, err)

	_, err = worktree.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test User",
			Email: "test@test.com",
			When:  time.Now(),
		},
	})
	require.NoError(t, err)

	// Clone to target using go-git
	targetDir := filepath.Join(t.TempDir(), "target")
	_, err = git.PlainClone(targetDir, false, &git.CloneOptions{
		URL: sourceDir,
	})
	require.NoError(t, err)

	gitOps := NewGitOps(sourceDir, "master", targetDir)

	t.Run("pull with no changes", func(t *testing.T) {
		changed, before, after, err := gitOps.Pull(ctx)
		require.NoError(t, err)
		assert.False(t, changed)
		assert.Equal(t, before, after)
	})

	t.Run("pull with changes", func(t *testing.T) {
		// Make a change in source
		require.NoError(t, os.WriteFile(testFile, []byte("updated"), 0644))

		worktree, err := repo.Worktree()
		require.NoError(t, err)

		_, err = worktree.Add("test.txt")
		require.NoError(t, err)

		_, err = worktree.Commit("update", &git.CommitOptions{
			Author: &object.Signature{
				Name:  "Test User",
				Email: "test@test.com",
				When:  time.Now(),
			},
		})
		require.NoError(t, err)

		changed, before, after, err := gitOps.Pull(ctx)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.NotEqual(t, before, after)
	})
}
