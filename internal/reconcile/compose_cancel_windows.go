//go:build windows

package reconcile

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func prepareComposeCommand(cmd *exec.Cmd) {
	// Windows cannot deliver os.Interrupt through Process.Signal. A new process
	// group lets Bosun target the Docker CLI with Ctrl-Break, which the CLI treats
	// as context cancellation. WaitDelay still force-kills it if no console is
	// attached or it does not exit during the grace period.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

func signalComposeProcess(process *os.Process) error {
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(process.Pid))
}
