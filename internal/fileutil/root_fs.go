package fileutil

import (
	"io/fs"
	"os"
	"runtime"
	"sort"
	"strings"
)

// PinnedRootFS exposes a pinned root as an fs.FS for callers that need a
// descriptor-bound namespace. Directory entries are materialized with
// Root.Lstat so their metadata does not lazily reopen a replaced pathname.
func PinnedRootFS(root *os.Root) fs.FS {
	return pinnedRootFS{root: root}
}

type pinnedRootFS struct {
	root *os.Root
}

func (p pinnedRootFS) Open(name string) (fs.File, error) {
	if !validPinnedFSPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	file, err := p.root.FS().Open(name)
	if err != nil {
		return nil, err
	}
	return pinnedFile{File: file}, nil
}

// pinnedFile deliberately exposes only fs.File. In particular, callers must
// use pinnedRootFS.ReadDir rather than recovering the underlying *os.File and
// invoking its path-based directory-entry methods.
type pinnedFile struct {
	fs.File
}

func (p pinnedRootFS) Stat(name string) (fs.FileInfo, error) {
	if !validPinnedFSPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	return fs.Stat(p.root.FS(), name)
}

func (p pinnedRootFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return p.readDir(name, nil)
}

// readDir accepts a test-only seam that runs after the directory sub-root is
// opened and before its entries are classified. Production callers use
// ReadDir, which leaves the seam nil.
func (p pinnedRootFS) readDir(name string, afterOpen func()) ([]fs.DirEntry, error) {
	if !validPinnedFSPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}

	dirRoot, err := p.root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = dirRoot.Close() }()
	if afterOpen != nil {
		afterOpen()
	}
	dir, err := dirRoot.Open(".")
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }()

	names, err := dir.Readdirnames(-1)
	if err != nil {
		return nil, err
	}
	entries := make([]fs.DirEntry, 0, len(names))
	for _, child := range names {
		info, err := dirRoot.Lstat(child)
		if err != nil {
			return nil, err
		}
		entries = append(entries, fs.FileInfoToDirEntry(info))
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	return entries, nil
}

// validPinnedFSPath preserves the platform-specific path contract of
// os.Root.FS. On Windows, a backslash is a separator to os.Root even though
// io/fs.ValidPath accepts it as an ordinary filename character.
func validPinnedFSPath(name string) bool {
	return fs.ValidPath(name) && (runtime.GOOS != "windows" || !strings.ContainsRune(name, '\\'))
}
