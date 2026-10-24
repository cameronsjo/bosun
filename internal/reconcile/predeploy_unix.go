//go:build !windows

package reconcile

import "syscall"

// killPreDeployHookGroup sends SIGKILL to the hook's process group. The hook
// runs as a group leader (Setpgid), so its pid is the group id. An already
// empty group (ESRCH) is the normal case.
func killPreDeployHookGroup(pid int) {
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL) // best effort: the group is usually gone
}
