//go:build !windows

package reconcile

import (
	"os"
	"os/exec"
	"syscall"
)

func prepareComposeCommand(_ *exec.Cmd) {}

func signalComposeProcess(process *os.Process) error { return process.Signal(syscall.SIGTERM) }
