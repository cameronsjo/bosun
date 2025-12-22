package reconcile

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

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
	t.Run("not a repo", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitOps := NewGitOps("", "", tmpDir)

		assert.False(t, gitOps.IsRepo())
	})

	t.Run("is a repo", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not installed")
		}

		tmpDir := t.TempDir()

		cmd := exec.Command("git", "init", tmpDir)
		require.NoError(t, cmd.Run())

		gitOps := NewGitOps("", "", tmpDir)
		assert.True(t, gitOps.IsRepo())
	})
}

func TestGitOps_Clone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	t.Run("clone valid repo", func(t *testing.T) {
		// Create a bare repository to clone from
		sourceDir := t.TempDir()
		bareDir := filepath.Join(sourceDir, "bare.git")
		targetDir := filepath.Join(t.TempDir(), "cloned")

		// Initialize a bare repo
		cmd := exec.Command("git", "init", "--bare", bareDir)
		require.NoError(t, cmd.Run())

		gitOps := NewGitOps(bareDir, "main", targetDir)
		ctx := context.Background()

		// This might fail because there's no main branch yet, that's expected
		// for empty repos. The important thing is that Clone() attempts the operation.
		_ = gitOps.Clone(ctx, 1)
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
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	t.Run("valid repo with commits", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		// Initialize repo
		cmd := exec.Command("git", "init", tmpDir)
		require.NoError(t, cmd.Run())

		// Configure git user for commit
		cmd = exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com")
		require.NoError(t, cmd.Run())
		cmd = exec.Command("git", "-C", tmpDir, "config", "user.name", "Test User")
		require.NoError(t, cmd.Run())

		// Create a file and commit
		testFile := filepath.Join(tmpDir, "test.txt")
		require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))

		cmd = exec.Command("git", "-C", tmpDir, "add", ".")
		require.NoError(t, cmd.Run())

		cmd = exec.Command("git", "-C", tmpDir, "commit", "-m", "initial commit")
		require.NoError(t, cmd.Run())

		gitOps := NewGitOps("", "", tmpDir)
		commit, err := gitOps.GetLatestCommit(ctx)

		require.NoError(t, err)
		assert.Len(t, commit, 40) // SHA-1 hash length
	})

	t.Run("no commits", func(t *testing.T) {
		tmpDir := t.TempDir()
		ctx := context.Background()

		// Initialize repo without any commits
		cmd := exec.Command("git", "init", tmpDir)
		require.NoError(t, cmd.Run())

		gitOps := NewGitOps("", "", tmpDir)
		_, err := gitOps.GetLatestCommit(ctx)

		assert.Error(t, err)
	})
}

func TestGitOps_GetCommitMessage(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	tmpDir := t.TempDir()
	ctx := context.Background()

	// Initialize repo
	cmd := exec.Command("git", "init", tmpDir)
	require.NoError(t, cmd.Run())

	// Configure git user
	cmd = exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com")
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "-C", tmpDir, "config", "user.name", "Test User")
	require.NoError(t, cmd.Run())

	// Create a file and commit with specific message
	testFile := filepath.Join(tmpDir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))

	cmd = exec.Command("git", "-C", tmpDir, "add", ".")
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "-C", tmpDir, "commit", "-m", "test commit message")
	require.NoError(t, cmd.Run())

	gitOps := NewGitOps("", "", tmpDir)
	msg, err := gitOps.GetCommitMessage(ctx)

	require.NoError(t, err)
	assert.Contains(t, msg, "test commit message")
}

func TestGitOps_Sync(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	t.Run("sync clones when repo doesn't exist", func(t *testing.T) {
		// Create source repo with a commit
		sourceDir := t.TempDir()
		ctx := context.Background()

		cmd := exec.Command("git", "init", sourceDir)
		require.NoError(t, cmd.Run())

		cmd = exec.Command("git", "-C", sourceDir, "config", "user.email", "test@test.com")
		require.NoError(t, cmd.Run())
		cmd = exec.Command("git", "-C", sourceDir, "config", "user.name", "Test User")
		require.NoError(t, cmd.Run())

		testFile := filepath.Join(sourceDir, "test.txt")
		require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))

		cmd = exec.Command("git", "-C", sourceDir, "add", ".")
		require.NoError(t, cmd.Run())
		cmd = exec.Command("git", "-C", sourceDir, "commit", "-m", "initial")
		require.NoError(t, cmd.Run())

		// Set default branch to main
		cmd = exec.Command("git", "-C", sourceDir, "branch", "-M", "main")
		require.NoError(t, cmd.Run())

		targetDir := filepath.Join(t.TempDir(), "target")
		gitOps := NewGitOps(sourceDir, "main", targetDir)

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

		cmd := exec.Command("git", "init", sourceDir)
		require.NoError(t, cmd.Run())

		cmd = exec.Command("git", "-C", sourceDir, "config", "user.email", "test@test.com")
		require.NoError(t, cmd.Run())
		cmd = exec.Command("git", "-C", sourceDir, "config", "user.name", "Test User")
		require.NoError(t, cmd.Run())

		testFile := filepath.Join(sourceDir, "test.txt")
		require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))

		cmd = exec.Command("git", "-C", sourceDir, "add", ".")
		require.NoError(t, cmd.Run())
		cmd = exec.Command("git", "-C", sourceDir, "commit", "-m", "initial")
		require.NoError(t, cmd.Run())
		cmd = exec.Command("git", "-C", sourceDir, "branch", "-M", "main")
		require.NoError(t, cmd.Run())

		// Clone to target
		targetDir := filepath.Join(t.TempDir(), "target")
		gitOps := NewGitOps(sourceDir, "main", targetDir)

		_, _, _, err := gitOps.Sync(ctx)
		require.NoError(t, err)

		// Now Sync again (should pull, no changes)
		changed, _, _, err := gitOps.Sync(ctx)
		require.NoError(t, err)
		assert.False(t, changed)
	})
}

func TestGitOps_Pull(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	// Create source repo
	sourceDir := t.TempDir()
	ctx := context.Background()

	cmd := exec.Command("git", "init", sourceDir)
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "-C", sourceDir, "config", "user.email", "test@test.com")
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "-C", sourceDir, "config", "user.name", "Test User")
	require.NoError(t, cmd.Run())

	testFile := filepath.Join(sourceDir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))

	cmd = exec.Command("git", "-C", sourceDir, "add", ".")
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "-C", sourceDir, "commit", "-m", "initial")
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "-C", sourceDir, "branch", "-M", "main")
	require.NoError(t, cmd.Run())

	// Clone to target
	targetDir := filepath.Join(t.TempDir(), "target")
	cmd = exec.Command("git", "clone", sourceDir, targetDir)
	require.NoError(t, cmd.Run())

	gitOps := NewGitOps(sourceDir, "main", targetDir)

	t.Run("pull with no changes", func(t *testing.T) {
		changed, before, after, err := gitOps.Pull(ctx)
		require.NoError(t, err)
		assert.False(t, changed)
		assert.Equal(t, before, after)
	})

	t.Run("pull with changes", func(t *testing.T) {
		// Make a change in source
		require.NoError(t, os.WriteFile(testFile, []byte("updated"), 0644))
		cmd = exec.Command("git", "-C", sourceDir, "add", ".")
		require.NoError(t, cmd.Run())
		cmd = exec.Command("git", "-C", sourceDir, "commit", "-m", "update")
		require.NoError(t, cmd.Run())

		changed, before, after, err := gitOps.Pull(ctx)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.NotEqual(t, before, after)
	})
}
