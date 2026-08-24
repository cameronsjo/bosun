//go:build !aix && !darwin && !dragonfly && !freebsd && !illumos && !linux && !netbsd && !openbsd && !solaris && !windows

package reconcile

import "io/fs"

func platformTransferFileIdentity(_ string, _ fs.FileInfo) (transferFileIdentity, bool) {
	return transferFileIdentity{}, false
}
