//go:build !windows

package reconcile

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// composeCancelPollInterval keeps cooperative cancellation responsive without
// spinning while the Docker CLI and its Compose plugin unwind.
const composeCancelPollInterval = 10 * time.Millisecond

func prepareComposeCommand(cmd *exec.Cmd) {
	// Docker invokes Compose as a CLI plugin subprocess. Isolating the command in
	// a process group lets cancellation reach both the docker wrapper and that
	// plugin instead of orphaning the actual daemon client.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func cancelComposeProcess(process *os.Process, grace time.Duration) error {
	pgid, err := syscall.Getpgid(process.Pid)
	if isComposeProcessDone(err) {
		return os.ErrProcessDone
	}
	if err != nil {
		return err
	}

	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
		if isComposeProcessDone(err) {
			return os.ErrProcessDone
		}
		return err
	}

	graceTimer := time.NewTimer(grace)
	defer graceTimer.Stop()
	poll := time.NewTicker(composeCancelPollInterval)
	defer poll.Stop()

	for {
		select {
		case <-graceTimer.C:
			// Cmd.WaitDelay can only kill the direct process. Escalate against the
			// group here so an unresponsive Compose plugin cannot survive its wrapper.
			if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil && !isComposeProcessDone(err) {
				return err
			}
			return nil
		case <-poll.C:
			if err := syscall.Kill(-pgid, 0); isComposeProcessDone(err) {
				return nil
			} else if err != nil && !errors.Is(err, syscall.EPERM) {
				return err
			}
		}
	}
}

func isComposeProcessDone(err error) bool {
	return errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH)
}
