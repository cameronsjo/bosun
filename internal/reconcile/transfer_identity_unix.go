//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package reconcile

import (
	"io/fs"
	"syscall"
)

func platformTransferFileIdentity(_ string, info fs.FileInfo) (transferFileIdentity, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return transferFileIdentity{}, false
	}
	return transferFileIdentity{volume: uint64(stat.Dev), file: uint64(stat.Ino)}, true
}
