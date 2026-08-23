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
	return cancelComposeProcessWithOps(
		process,
		grace,
		composeCancelPollInterval,
		syscall.Getpgid,
		syscall.Kill,
	)
}

func cancelComposeProcessWithOps(
	process *os.Process,
	grace time.Duration,
	pollInterval time.Duration,
	getpgid func(int) (int, error),
	kill func(int, syscall.Signal) error,
) error {
	pgid, err := getpgid(process.Pid)
	if isComposeProcessDone(err) {
		return os.ErrProcessDone
	}
	if err != nil {
		return err
	}

	if err := kill(-pgid, syscall.SIGTERM); err != nil {
		if isComposeProcessDone(err) {
			return os.ErrProcessDone
		}
		return err
	}

	graceTimer := time.NewTimer(grace)
	defer graceTimer.Stop()
	poll := time.NewTicker(pollInterval)
	defer poll.Stop()

	for {
		select {
		case <-graceTimer.C:
			// Cmd.WaitDelay can only kill the direct process. Escalate against the
			// group here so an unresponsive Compose plugin cannot survive its wrapper.
			if err := kill(-pgid, syscall.SIGKILL); err != nil && !isComposeProcessDone(err) {
				return err
			}
			return nil
		case <-poll.C:
			if err := kill(-pgid, 0); isComposeProcessDone(err) {
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
