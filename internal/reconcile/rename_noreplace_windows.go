//go:build windows

package reconcile

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fileRenameInformation is the variable-length FILE_RENAME_INFORMATION
// header. The trailing FileName bytes are supplied in the buffer passed to
// NtSetInformationFile, rather than through this one-element placeholder.
type fileRenameInformation struct {
	ReplaceIfExists uint32
	RootDirectory   windows.Handle
	FileNameLength  uint32
	FileName        [1]uint16
}

func renameNoReplace(oldPath, newPath string) error {
	oldPtr, err := windows.UTF16PtrFromString(oldPath)
	if err != nil {
		return err
	}
	newPtr, err := windows.UTF16PtrFromString(newPath)
	if err != nil {
		return err
	}
	return windows.MoveFile(oldPtr, newPtr)
}

func renameNoReplaceAt(oldDir *os.File, oldName string, newDir *os.File, newName string) error {
	defer runtime.KeepAlive(oldDir)
	defer runtime.KeepAlive(newDir)
	if oldName == "" || newName == "" {
		return syscall.EINVAL
	}
	oldNameUTF16, err := windows.NewNTUnicodeString(oldName)
	if err != nil {
		return err
	}
	objectAttributes := &windows.OBJECT_ATTRIBUTES{
		Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory: windows.Handle(oldDir.Fd()),
		ObjectName:    oldNameUTF16,
	}

	// Open the source relative to the already-open old parent. DELETE access is
	// required by the rename operation; FILE_OPEN_REPARSE_POINT keeps the
	// operation bound to the named entry instead of following a final reparse
	// point. This mirrors the Go runtime's Windows Renameat implementation.
	var source windows.Handle
	err = windows.NtCreateFile(
		&source,
		windows.SYNCHRONIZE|windows.DELETE,
		objectAttributes,
		&windows.IO_STATUS_BLOCK{},
		nil,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.FILE_OPEN,
		windows.FILE_OPEN_REPARSE_POINT|windows.FILE_OPEN_FOR_BACKUP_INTENT|windows.FILE_SYNCHRONOUS_IO_NONALERT,
		0,
		0,
	)
	if err != nil {
		if status, ok := err.(windows.NTStatus); ok {
			return status.Errno()
		}
		return err
	}
	defer func() { _ = windows.CloseHandle(source) }()

	newNameUTF16, err := windows.UTF16FromString(newName)
	if err != nil {
		return err
	}
	fileNameLength := (len(newNameUTF16) - 1) * 2
	var dummy fileRenameInformation
	bufferSize := int(unsafe.Offsetof(dummy.FileName)) + fileNameLength
	allocationSize := bufferSize
	if allocationSize < int(unsafe.Sizeof(dummy)) {
		allocationSize = int(unsafe.Sizeof(dummy))
	}
	buffer := make([]byte, allocationSize)
	info := (*fileRenameInformation)(unsafe.Pointer(&buffer[0]))
	info.RootDirectory = windows.Handle(newDir.Fd())
	info.FileNameLength = uint32(fileNameLength)
	copy(unsafe.Slice(&info.FileName[0], fileNameLength/2), newNameUTF16[:len(newNameUTF16)-1])

	// ReplaceIfExists remains zero: an existing destination must make the
	// operation fail, preserving the transition rollback/no-clobber contract.
	err = windows.NtSetInformationFile(
		source,
		&windows.IO_STATUS_BLOCK{},
		&buffer[0],
		uint32(bufferSize),
		windows.FileRenameInformation,
	)
	if status, ok := err.(windows.NTStatus); ok {
		return status.Errno()
	}
	return err
}
