package reconcile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGitOps(t *testing.T) {
	t.Run("uses default fetch depth when unset", func(t *testing.T) {
		t.Setenv("BOSUN_GIT_FETCH_DEPTH", "")
		gitOps := NewGitOps("https://github.com/test/repo.git", "main", "/tmp/test")

		assert.Equal(t, "https://github.com/test/repo.git", gitOps.RepoURL)
		assert.Equal(t, "main", gitOps.Branch)
		assert.Equal(t, "/tmp/test", gitOps.Dir)
		assert.Equal(t, DefaultGitFetchDepth, gitOps.FetchDepth)
	})

	t.Run("falls back to default fetch depth for invalid environment", func(t *testing.T) {
		t.Setenv("BOSUN_GIT_FETCH_DEPTH", "invalid")

		gitOps := NewGitOps("https://github.com/test/repo.git", "main", "/tmp/test")

		assert.Equal(t, DefaultGitFetchDepth, gitOps.FetchDepth)
	})
}

func TestParseGitFetchDepth(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    int
		wantErr bool
	}{
		{name: "unset uses default", value: "", want: DefaultGitFetchDepth},
		{name: "explicit default", value: "1", want: 1},
		{name: "deeper history", value: "50", want: 50},
		{name: "zero rejected", value: "0", want: DefaultGitFetchDepth, wantErr: true},
		{name: "negative rejected", value: "-2", want: DefaultGitFetchDepth, wantErr: true},
		{name: "non numeric rejected", value: "deep", want: DefaultGitFetchDepth, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseGitFetchDepth(tt.value)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGitOpsEffectiveFetchDepth(t *testing.T) {
	assert.Equal(t, DefaultGitFetchDepth, (&GitOps{}).effectiveFetchDepth())
	assert.Equal(t, 7, (&GitOps{FetchDepth: 7}).effectiveFetchDepth())
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

func TestGitOps_IsDirtyErrors(t *testing.T) {
	t.Run("not a repo returns error", func(t *testing.T) {
		gitOps := NewGitOps("", "", t.TempDir())
		_, err := gitOps.IsDirty(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to open repository")
	})

	t.Run("cancelled context returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		repo, err := git.PlainInit(tmpDir, false)
		require.NoError(t, err)

		testFile := filepath.Join(tmpDir, "test.txt")
		require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))

		wt, err := repo.Worktree()
		require.NoError(t, err)
		_, err = wt.Add("test.txt")
		require.NoError(t, err)
		_, err = wt.Commit("init", &git.CommitOptions{
			Author: &object.Signature{Name: "Test", Email: "t@t.com", When: time.Now()},
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		gitOps := NewGitOps("", "", tmpDir)
		_, err = gitOps.IsDirty(ctx)
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestGitOps_GetLatestCommitErrors(t *testing.T) {
	t.Run("not a repo", func(t *testing.T) {
		gitOps := NewGitOps("", "", t.TempDir())
		_, err := gitOps.GetLatestCommit(context.Background())
		assert.Error(t, err)
	})

	t.Run("cancelled context", func(t *testing.T) {
		tmpDir := t.TempDir()
		repo, err := git.PlainInit(tmpDir, false)
		require.NoError(t, err)

		testFile := filepath.Join(tmpDir, "test.txt")
		require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))

		wt, err := repo.Worktree()
		require.NoError(t, err)
		_, err = wt.Add("test.txt")
		require.NoError(t, err)
		_, err = wt.Commit("init", &git.CommitOptions{
			Author: &object.Signature{Name: "Test", Email: "t@t.com", When: time.Now()},
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		gitOps := NewGitOps("", "", tmpDir)
		_, err = gitOps.GetLatestCommit(ctx)
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestGitOps_GetCommitMessageErrors(t *testing.T) {
	t.Run("not a repo", func(t *testing.T) {
		gitOps := NewGitOps("", "", t.TempDir())
		_, err := gitOps.GetCommitMessage(context.Background())
		assert.Error(t, err)
	})

	t.Run("cancelled context", func(t *testing.T) {
		tmpDir := t.TempDir()
		repo, err := git.PlainInit(tmpDir, false)
		require.NoError(t, err)

		testFile := filepath.Join(tmpDir, "test.txt")
		require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))

		wt, err := repo.Worktree()
		require.NoError(t, err)
		_, err = wt.Add("test.txt")
		require.NoError(t, err)
		_, err = wt.Commit("init", &git.CommitOptions{
			Author: &object.Signature{Name: "Test", Email: "t@t.com", When: time.Now()},
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		gitOps := NewGitOps("", "", tmpDir)
		_, err = gitOps.GetCommitMessage(ctx)
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("no commits", func(t *testing.T) {
		tmpDir := t.TempDir()
		_, err := git.PlainInit(tmpDir, false)
		require.NoError(t, err)

		gitOps := NewGitOps("", "", tmpDir)
		_, err = gitOps.GetCommitMessage(context.Background())
		assert.Error(t, err)
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

// testRepoWithCommits creates a temp git repo with the given files committed.
// Returns the repo directory and the commit hash.
func testRepoWithCommits(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)

	wt, err := repo.Worktree()
	require.NoError(t, err)

	for name, content := range files {
		fullPath := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))
		_, err = wt.Add(name)
		require.NoError(t, err)
	}

	hash, err := wt.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@test.com",
			When:  time.Now(),
		},
	})
	require.NoError(t, err)

	return dir, hash.String()
}

func TestGitOps_DiffFiles(t *testing.T) {
	ctx := context.Background()

	t.Run("diff between two commits with changed files", func(t *testing.T) {
		// Create initial commit
		dir, commit1 := testRepoWithCommits(t, map[string]string{
			"file1.txt": "original",
			"file2.txt": "content",
		})

		// Open repo and make a second commit
		repo, err := git.PlainOpen(dir)
		require.NoError(t, err)
		wt, err := repo.Worktree()
		require.NoError(t, err)

		// Modify file1 and add file3
		require.NoError(t, os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("modified"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "file3.txt"), []byte("new file"), 0644))
		_, err = wt.Add("file1.txt")
		require.NoError(t, err)
		_, err = wt.Add("file3.txt")
		require.NoError(t, err)

		hash2, err := wt.Commit("second commit", &git.CommitOptions{
			Author: &object.Signature{
				Name:  "Test",
				Email: "test@test.com",
				When:  time.Now(),
			},
		})
		require.NoError(t, err)
		commit2 := hash2.String()

		gitOps := NewGitOps("", "", dir)
		files, err := gitOps.DiffFiles(ctx, commit1, commit2)
		require.NoError(t, err)
		assert.Len(t, files, 2)
		assert.Contains(t, files, "file1.txt")
		assert.Contains(t, files, "file3.txt")
	})

	t.Run("diff with empty fromCommit returns all files in toCommit", func(t *testing.T) {
		dir, commitHash := testRepoWithCommits(t, map[string]string{
			"a.txt":     "aaa",
			"b.txt":     "bbb",
			"dir/c.txt": "ccc",
		})

		gitOps := NewGitOps("", "", dir)
		files, err := gitOps.DiffFiles(ctx, "", commitHash)
		require.NoError(t, err)
		assert.Len(t, files, 3)
		assert.Contains(t, files, "a.txt")
		assert.Contains(t, files, "b.txt")
		assert.Contains(t, files, "dir/c.txt")
	})

	t.Run("diff with deleted file uses From name", func(t *testing.T) {
		dir, commit1 := testRepoWithCommits(t, map[string]string{
			"keep.txt":   "staying",
			"delete.txt": "going away",
		})

		repo, err := git.PlainOpen(dir)
		require.NoError(t, err)
		wt, err := repo.Worktree()
		require.NoError(t, err)

		// Delete a file
		require.NoError(t, os.Remove(filepath.Join(dir, "delete.txt")))
		_, err = wt.Remove("delete.txt")
		require.NoError(t, err)

		hash2, err := wt.Commit("delete file", &git.CommitOptions{
			Author: &object.Signature{
				Name:  "Test",
				Email: "test@test.com",
				When:  time.Now(),
			},
		})
		require.NoError(t, err)

		gitOps := NewGitOps("", "", dir)
		files, err := gitOps.DiffFiles(ctx, commit1, hash2.String())
		require.NoError(t, err)
		assert.Contains(t, files, "delete.txt")
	})

	t.Run("diff with no changes returns empty", func(t *testing.T) {
		dir, commitHash := testRepoWithCommits(t, map[string]string{
			"file.txt": "content",
		})

		gitOps := NewGitOps("", "", dir)
		files, err := gitOps.DiffFiles(ctx, commitHash, commitHash)
		require.NoError(t, err)
		assert.Empty(t, files)
	})

	t.Run("invalid toCommit returns error", func(t *testing.T) {
		dir, _ := testRepoWithCommits(t, map[string]string{
			"file.txt": "content",
		})

		gitOps := NewGitOps("", "", dir)
		_, err := gitOps.DiffFiles(ctx, "", "0000000000000000000000000000000000000000")
		assert.Error(t, err)
	})

	t.Run("shallow clone reports unavailable previous commit with sentinel", func(t *testing.T) {
		sourceDir, commits := testRepoHistory(t, 3)
		targetDir := filepath.Join(t.TempDir(), "target")
		_, err := git.PlainClone(targetDir, false, &git.CloneOptions{
			URL:          sourceDir,
			Depth:        1,
			SingleBranch: true,
		})
		require.NoError(t, err)

		gitOps := NewGitOps(sourceDir, "master", targetDir)
		_, err = gitOps.DiffFiles(ctx, commits[0], commits[2])

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrCommitUnavailable)
		assert.Contains(t, err.Error(), commits[0][:8])
	})

	t.Run("corrupt previous commit error is not classified as unavailable", func(t *testing.T) {
		dir, commits := testRepoHistory(t, 1)
		corruptHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		corruptPath := looseObjectPath(dir, plumbing.NewHash(corruptHash))
		require.NoError(t, os.MkdirAll(filepath.Dir(corruptPath), 0755))
		require.NoError(t, os.WriteFile(corruptPath, []byte("not a git object"), 0644))

		gitOps := NewGitOps("", "", dir)
		_, err := gitOps.DiffFiles(ctx, corruptHash, commits[0])

		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrCommitUnavailable)
		assert.Contains(t, err.Error(), "resolve from-commit aaaaaaaa")
	})

	t.Run("missing previous tree reports unavailable commit with sentinel", func(t *testing.T) {
		dir, commits := testRepoHistory(t, 2)
		treeHash := commitTreeHash(t, dir, commits[0])
		require.NoError(t, os.Remove(looseObjectPath(dir, treeHash)))

		gitOps := NewGitOps("", "", dir)
		_, err := gitOps.DiffFiles(ctx, commits[0], commits[1])

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrCommitUnavailable)
		assert.Contains(t, err.Error(), "get tree for "+commits[0][:8])
	})

	t.Run("corrupt previous tree error is not classified as unavailable", func(t *testing.T) {
		dir, commits := testRepoHistory(t, 2)
		treeHash := commitTreeHash(t, dir, commits[0])
		treePath := looseObjectPath(dir, treeHash)
		require.NoError(t, os.Chmod(treePath, 0644))
		require.NoError(t, os.WriteFile(treePath, []byte("not a git object"), 0644))

		gitOps := NewGitOps("", "", dir)
		_, err := gitOps.DiffFiles(ctx, commits[0], commits[1])

		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrCommitUnavailable)
		assert.Contains(t, err.Error(), "get tree for "+commits[0][:8])
	})
}

func TestGitOps_ConfiguredFetchDepth(t *testing.T) {
	ctx := context.Background()

	t.Run("configured depth is used for initial sync clone", func(t *testing.T) {
		t.Setenv("BOSUN_GIT_FETCH_DEPTH", "3")
		sourceDir, commits := testRepoHistory(t, 3)
		targetDir := filepath.Join(t.TempDir(), "target")
		gitOps := NewGitOps(sourceDir, "master", targetDir)

		changed, before, after, err := gitOps.Sync(ctx)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Empty(t, before)
		assert.Equal(t, commits[2], after)
		assert.Equal(t, 3, gitOps.FetchDepth)

		files, err := gitOps.DiffFiles(ctx, commits[0], commits[2])
		require.NoError(t, err)
		assert.Equal(t, []string{"version.txt"}, files)
	})

	t.Run("configured fetch deepens an existing shallow clone", func(t *testing.T) {
		t.Setenv("BOSUN_GIT_FETCH_DEPTH", "3")
		sourceDir, commits := testRepoHistory(t, 3)
		targetDir := filepath.Join(t.TempDir(), "target")
		_, err := git.PlainClone(targetDir, false, &git.CloneOptions{
			URL:          sourceDir,
			Depth:        1,
			SingleBranch: true,
		})
		require.NoError(t, err)

		sourceRepo, err := git.PlainOpen(sourceDir)
		require.NoError(t, err)
		sourceWorktree, err := sourceRepo.Worktree()
		require.NoError(t, err)
		commit4 := commitTestVersion(t, sourceDir, sourceWorktree, 4)

		gitOps := NewGitOps(sourceDir, "master", targetDir)
		changed, before, after, err := gitOps.Pull(ctx)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, commits[2], before)
		assert.Equal(t, commit4, after)

		files, err := gitOps.DiffFiles(ctx, commits[1], commit4)
		require.NoError(t, err)
		assert.Equal(t, []string{"version.txt"}, files)
	})

	t.Run("configured fetch deepens when the remote tip is unchanged", func(t *testing.T) {
		t.Setenv("BOSUN_GIT_FETCH_DEPTH", "3")
		sourceDir, commits := testRepoHistory(t, 3)
		targetDir := filepath.Join(t.TempDir(), "target")
		_, err := git.PlainClone(targetDir, false, &git.CloneOptions{
			URL:          sourceDir,
			Depth:        1,
			SingleBranch: true,
		})
		require.NoError(t, err)

		gitOps := NewGitOps(sourceDir, "master", targetDir)
		changed, before, after, err := gitOps.Pull(ctx)
		require.NoError(t, err)
		assert.False(t, changed)
		assert.Equal(t, commits[2], before)
		assert.Equal(t, commits[2], after)

		files, err := gitOps.DiffFiles(ctx, commits[0], commits[2])
		require.NoError(t, err)
		assert.Equal(t, []string{"version.txt"}, files)
	})
}

func testRepoHistory(t *testing.T, count int) (string, []string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	worktree, err := repo.Worktree()
	require.NoError(t, err)

	commits := make([]string, 0, count)
	for version := 1; version <= count; version++ {
		commits = append(commits, commitTestVersion(t, dir, worktree, version))
	}
	return dir, commits
}

func commitTestVersion(t *testing.T, dir string, worktree *git.Worktree, version int) string {
	t.Helper()
	name := "version.txt"
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(fmt.Sprintf("version %d\n", version)), 0644))
	_, err := worktree.Add(name)
	require.NoError(t, err)
	hash, err := worktree.Commit(fmt.Sprintf("version %d", version), &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@test.com",
			When:  time.Unix(int64(version), 0),
		},
	})
	require.NoError(t, err)
	return hash.String()
}

func commitTreeHash(t *testing.T, dir, commitHash string) plumbing.Hash {
	t.Helper()
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	commit, err := repo.CommitObject(plumbing.NewHash(commitHash))
	require.NoError(t, err)
	return commit.TreeHash
}

func looseObjectPath(dir string, hash plumbing.Hash) string {
	encoded := hash.String()
	return filepath.Join(dir, ".git", "objects", encoded[:2], encoded[2:])
}

func TestGitOps_RemoteBranchExists(t *testing.T) {
	ctx := context.Background()

	// Create a source repo with a commit on master
	sourceDir := t.TempDir()
	sourceDir, err := filepath.EvalSymlinks(sourceDir)
	require.NoError(t, err)

	sourceRepo, err := git.PlainInit(sourceDir, false)
	require.NoError(t, err)

	testFile := filepath.Join(sourceDir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))

	wt, err := sourceRepo.Worktree()
	require.NoError(t, err)
	_, err = wt.Add("test.txt")
	require.NoError(t, err)
	_, err = wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@test.com",
			When:  time.Now(),
		},
	})
	require.NoError(t, err)

	// Clone to target
	targetDir := filepath.Join(t.TempDir(), "target")
	_, err = git.PlainClone(targetDir, false, &git.CloneOptions{URL: sourceDir})
	require.NoError(t, err)

	gitOps := NewGitOps(sourceDir, "master", targetDir)

	t.Run("existing branch returns true", func(t *testing.T) {
		exists, err := gitOps.RemoteBranchExists(ctx, "master")
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("non-existent branch returns false with nil error", func(t *testing.T) {
		exists, err := gitOps.RemoteBranchExists(ctx, "nonexistent-branch")
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("cancelled context returns error", func(t *testing.T) {
		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel()

		_, err := gitOps.RemoteBranchExists(cancelledCtx, "master")
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("invalid repo dir returns error", func(t *testing.T) {
		badOps := NewGitOps("", "", "/nonexistent/dir")
		_, err := badOps.RemoteBranchExists(ctx, "master")
		assert.Error(t, err)
	})
}

func TestGitOps_PullErrorPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("pull with invalid branch returns error", func(t *testing.T) {
		gitOps := NewGitOps("", "", t.TempDir())
		gitOps.Branch = "--upload-pack=evil" // Starts with '-', rejected by validateBranch

		_, _, _, err := gitOps.Pull(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid branch")
	})

	t.Run("pull on non-repo returns error", func(t *testing.T) {
		gitOps := NewGitOps("", "main", t.TempDir())

		_, _, _, err := gitOps.Pull(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get current commit")
	})

	t.Run("pull with dirty working tree warns and continues", func(t *testing.T) {
		// Create source repo
		sourceDir := t.TempDir()
		sourceDir, err := filepath.EvalSymlinks(sourceDir)
		require.NoError(t, err)

		sourceRepo, err := git.PlainInit(sourceDir, false)
		require.NoError(t, err)

		wt, err := sourceRepo.Worktree()
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "test.txt"), []byte("content"), 0644))
		_, err = wt.Add("test.txt")
		require.NoError(t, err)
		_, err = wt.Commit("initial", &git.CommitOptions{
			Author: &object.Signature{Name: "Test", Email: "t@t.com", When: time.Now()},
		})
		require.NoError(t, err)

		// Clone to target
		targetDir := filepath.Join(t.TempDir(), "target")
		targetDir, err = filepath.EvalSymlinks(filepath.Dir(targetDir))
		require.NoError(t, err)
		targetDir = filepath.Join(targetDir, "target")

		_, err = git.PlainClone(targetDir, false, &git.CloneOptions{URL: sourceDir})
		require.NoError(t, err)

		// Make target dirty by modifying a tracked file
		require.NoError(t, os.WriteFile(filepath.Join(targetDir, "test.txt"), []byte("dirty"), 0644))

		gitOps := NewGitOps(sourceDir, "master", targetDir)

		// Pull should succeed despite dirty working tree (hard reset discards changes)
		changed, _, _, err := gitOps.Pull(ctx)
		require.NoError(t, err)
		assert.False(t, changed)
	})
}
