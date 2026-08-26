package tunnel

import (
	"bytes"
	"context"
	"os/exec"
)

// commandRunner isolates provider subprocesses so status parsing and fallback
// behavior can be tested without depending on host-installed tunnel binaries.
type commandRunner interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, string, error)
	Run(ctx context.Context, name string, args ...string) error
}

type execCommandRunner struct{}

func (execCommandRunner) Output(ctx context.Context, name string, args ...string) ([]byte, string, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	return output, stderr.String(), err
}

func (execCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}
