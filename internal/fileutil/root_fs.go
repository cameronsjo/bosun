package fileutil

import (
	"io/fs"
	"os"
)

// PinnedRootFS exposes a pinned root as an fs.FS for callers that need a
// descriptor-bound namespace.
//
// This checkpoint intentionally delegates directly to os.Root.FS. The
// follow-up adapter must replace its lazy directory-entry metadata path with
// Root.Lstat calls so Info remains bound after a pathname replacement.
func PinnedRootFS(root *os.Root) fs.FS {
	return root.FS()
}
