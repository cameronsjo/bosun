package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/cameronsjo/bosun/internal/ui"
)

var (
	breakerStateDir string
	breakerTarget   string
)

// breakerCmd is the parent command for circuit breaker operations.
var breakerCmd = &cobra.Command{
	Use:   "breaker",
	Short: "Manage the deploy circuit breaker",
	Long: `View and manage the deploy circuit breaker state.

The circuit breaker trips after 3 consecutive deploy failures on the same
commit, preventing infinite retry loops. Use these commands to inspect
the breaker state and reset it when the root cause has been resolved.

State directory resolution:
  The --state-dir flag sets the base directory for state files. If the
  BOSUN_STATE_DIR environment variable is set, it overrides --state-dir
  at runtime (same precedence pattern as other BOSUN_ env vars).

  Note: 'bosun drift --state-file' uses an explicit file path and does NOT
  honour BOSUN_STATE_DIR. Unifying this behaviour is tracked as a follow-up.`,
}

// breakerStatusCmd shows circuit breaker state.
var breakerStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show circuit breaker state",
	RunE:  runBreakerStatus,
}

// breakerResetCmd resets the circuit breaker.
var breakerResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset the circuit breaker to allow retries",
	Long: `Reset the deploy circuit breaker by clearing the failure counter.

This allows reconciliation to retry after the breaker has tripped.
Use this after resolving the root cause of deploy failures.`,
	RunE: runBreakerReset,
}

func init() {
	breakerCmd.PersistentFlags().StringVar(&breakerStateDir, "state-dir", reconcile.DefaultStateDir, "State directory")
	breakerCmd.PersistentFlags().StringVarP(&breakerTarget, "target", "t", "", "Named deployment target (from bosun.yaml targets: section)")

	breakerCmd.AddCommand(breakerStatusCmd)
	breakerCmd.AddCommand(breakerResetCmd)
	rootCmd.AddCommand(breakerCmd)
}

// resolveBreaker StateFile returns the state file for the active target, honoring
// the --target flag (and BOSUN_STATE_DIR override) in the same way that
// bosun drift does.
func resolveBreakerStateFile() string {
	dir := breakerStateDir
	if d := os.Getenv("BOSUN_STATE_DIR"); d != "" {
		dir = d
	}

	if breakerTarget != "" {
		targets := loadConfiguredTargets()
		for _, t := range targets {
			if t.Name == breakerTarget {
				return reconcile.TargetStateFile(dir, t)
			}
		}
		// Target not in config — still derive the state file by name.
		t := reconcile.Target{Name: breakerTarget}
		return reconcile.TargetStateFile(dir, t)
	}

	return filepath.Join(dir, reconcile.DefaultStateFile)
}

func runBreakerStatus(cmd *cobra.Command, args []string) error {
	stateFile := resolveBreakerStateFile()
	state := reconcile.LoadState(stateFile)

	if breakerTarget != "" {
		ui.Header("Target: %s", breakerTarget)
	}

	if state.AttemptCount == 0 {
		ui.Success("Circuit breaker: closed (no failures)")
		return nil
	}

	if state.AttemptCount >= reconcile.MaxAttempts {
		ui.Error("Circuit breaker: OPEN (%d consecutive failures on %s)",
			state.AttemptCount, truncateCommit(state.LastAttemptedCommit))
		ui.Info("  Run 'bosun breaker reset' to allow retries")
		return nil
	}

	ui.Warning("Circuit breaker: closed (%d/%d failures on %s)",
		state.AttemptCount, reconcile.MaxAttempts, truncateCommit(state.LastAttemptedCommit))
	return nil
}

func runBreakerReset(cmd *cobra.Command, args []string) error {
	stateFile := resolveBreakerStateFile()
	state := reconcile.LoadState(stateFile)

	if breakerTarget != "" {
		ui.Header("Target: %s", breakerTarget)
	}

	if state.AttemptCount == 0 {
		ui.Info("Circuit breaker already closed, nothing to reset")
		return nil
	}

	priorAttempts := state.AttemptCount
	priorCommit := state.LastAttemptedCommit

	state.AttemptCount = 0
	state.LastAttemptedCommit = ""
	state.LastAlertedAttempt = 0

	if err := reconcile.SaveState(stateFile, state); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	ui.Success("Circuit breaker reset (was: %d failures on %s)",
		priorAttempts, truncateCommit(priorCommit))
	return nil
}

func truncateCommit(hash string) string {
	if len(hash) > 7 {
		return hash[:7]
	}
	return hash
}
