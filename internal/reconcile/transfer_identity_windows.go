//go:build windows

package reconcile

import (
	"io/fs"

	"golang.org/x/sys/windows"
)

func platformTransferFileIdentity(path string, _ fs.FileInfo) (transferFileIdentity, bool) {
	file, err := openPinnedPath(path)
	if err != nil {
		return transferFileIdentity{}, false
	}
	defer func() { _ = file.Close() }()

	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info); err != nil {
		return transferFileIdentity{}, false
	}
	return transferFileIdentity{
		volume: uint64(info.VolumeSerialNumber),
		file:   uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow),
	}, true
}
