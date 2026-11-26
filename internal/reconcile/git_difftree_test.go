package reconcile

import (
	"context"
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

// subtreeHash returns the hash of the directory entry name in commitHash's
// root tree.
func subtreeHash(t *testing.T, dir, commitHash, name string) plumbing.Hash {
	t.Helper()
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	commit, err := repo.CommitObject(plumbing.NewHash(commitHash))
	require.NoError(t, err)
	tree, err := commit.Tree()
	require.NoError(t, err)
	entry, err := tree.FindEntry(name)
	require.NoError(t, err)
	return entry.Hash
}

// go-git's non-recursive TreeWalker turns a GetTree error into io.EOF
// (plumbing/object/tree.go, TreeWalker.Next), so DiffTree silently truncates the
// listing of a directory whose subtree object is missing and returns no error.
// DiffFiles must detect the incomplete from-tree and report
// ErrCommitUnavailable so callers take the full-deploy, all-hooks path instead
// of trusting a diff that is missing the deletion under a/.
func TestDiffFiles_MissingFromSubtreeIsCommitUnavailable(t *testing.T) {
	dir, commit1 := testRepoWithCommits(t, map[string]string{
		"a/old.txt":  "removed in the second commit",
		"z/keep.txt": "unchanged",
	})

	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(dir, "a", "old.txt")))
	_, err = wt.Remove("a/old.txt")
	require.NoError(t, err)
	hash2, err := wt.Commit("remove a/old.txt", &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@test.com", When: time.Now()},
	})
	require.NoError(t, err)

	// The fixture's objects are loose (go-git PlainInit never packs), so
	// removing the file removes the object.
	require.NoError(t, os.Remove(looseObjectPath(dir, subtreeHash(t, dir, commit1, "a"))))

	gitOps := NewGitOps("", "", dir)
	files, err := gitOps.DiffFiles(context.Background(), commit1, hash2.String())

	require.Error(t, err, "an incomplete from-tree must not produce a diff; got files %v", files)
	assert.ErrorIs(t, err, ErrCommitUnavailable)
	assert.Contains(t, err.Error(), commit1[:8])
}

// A corrupt (present but undecodable) subtree is a repository fault, not
// insufficient history, so it must not be classified as ErrCommitUnavailable.
func TestDiffFiles_CorruptFromSubtreeIsNotCommitUnavailable(t *testing.T) {
	dir, commits := testRepoHistoryWithSubdir(t)
	treePath := looseObjectPath(dir, subtreeHash(t, dir, commits[0], "a"))
	require.NoError(t, os.Chmod(treePath, 0o644))
	require.NoError(t, os.WriteFile(treePath, []byte("not a git object"), 0o644))

	gitOps := NewGitOps("", "", dir)
	_, err := gitOps.DiffFiles(context.Background(), commits[0], commits[1])

	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrCommitUnavailable)
	assert.Contains(t, err.Error(), "inspect tree for "+commits[0][:8])
}

// testRepoHistoryWithSubdir returns a repo with two commits: the first holds
// a/old.txt and z/keep.txt, the second deletes a/old.txt and changes
// z/keep.txt, so the a/ subtree object exists only in the first commit's tree.
func testRepoHistoryWithSubdir(t *testing.T) (string, []string) {
	t.Helper()
	dir, commit1 := testRepoWithCommits(t, map[string]string{
		"a/old.txt":  "old",
		"z/keep.txt": "v1",
	})
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(dir, "a", "old.txt")))
	_, err = wt.Remove("a/old.txt")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "z", "keep.txt"), []byte("v2"), 0o644))
	_, err = wt.Add("z/keep.txt")
	require.NoError(t, err)
	hash2, err := wt.Commit("second", &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@test.com", When: time.Now()},
	})
	require.NoError(t, err)
	return dir, []string{commit1, hash2.String()}
}
