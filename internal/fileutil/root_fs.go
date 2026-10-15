package fileutil

import (
	"io/fs"
	"os"
	"path"
	"sort"
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
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	return p.root.FS().Open(name)
}

func (p pinnedRootFS) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	return fs.Stat(p.root.FS(), name)
}

func (p pinnedRootFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}

	dir, err := p.root.Open(name)
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
		childPath := path.Join(name, child)
		info, err := p.root.Lstat(childPath)
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
