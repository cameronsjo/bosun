package reconcile

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"time"
)

// dockerComposeCancelGrace bounds how long the Docker CLI and Compose plugin
// may unwind after a graceful termination signal before cancellation escalates.
// They convert the signal into context cancellation, which closes the daemon
// request instead of abandoning it when only the local wrapper process is
// killed.
const dockerComposeCancelGrace = 5 * time.Second

func dockerComposeCommandContext(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "docker", args...)
	configureComposeCancellation(cmd, dockerComposeCancelGrace)
	return cmd
}

func configureComposeCancellation(cmd *exec.Cmd, grace time.Duration) {
	prepareComposeCommand(cmd)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := cancelComposeProcess(cmd.Process, grace)
		if errors.Is(err, os.ErrProcessDone) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = grace
}
