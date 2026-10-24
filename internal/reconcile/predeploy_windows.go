//go:build windows

package reconcile

// killPreDeployHookGroup is a no-op on Windows: there is no process-group
// kill equivalent to POSIX kill(-pgid). Cancellation still uses Ctrl-Break
// and WaitDelay (configureComposeCancellation).
func killPreDeployHookGroup(int) {}
