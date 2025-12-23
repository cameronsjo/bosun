package cmd

import (
	"bytes"
	"context"
	"testing"
)

// resetRootCmd resets the root command state for test isolation.
// This must be called at the beginning of each test to ensure
// cobra command state doesn't leak between tests.
func resetRootCmd(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := new(bytes.Buffer)
	// Reset args to empty slice (not nil, which would use os.Args)
	rootCmd.SetArgs([]string{})
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	// Reset all subcommands' context and flags
	for _, cmd := range rootCmd.Commands() {
		cmd.SetContext(context.TODO())
		cmd.ResetFlags()
	}
	return buf
}

// executeCmd executes the root command with the given args and returns the output.
// This handles proper state reset between test executions.
func executeCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	buf := new(bytes.Buffer)
	// Important: Set args BEFORE setting output buffers
	rootCmd.SetArgs(args)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	err := rootCmd.Execute()
	return buf.String(), err
}
