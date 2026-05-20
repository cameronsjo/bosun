package cmd

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/reconcile"
)

func TestBreakerCmd_Registration(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"breaker"})
	require.NoError(t, err)
	assert.Equal(t, "breaker", cmd.Name())
}

func TestBreakerCmd_HasTargetFlag(t *testing.T) {
	flag := breakerCmd.PersistentFlags().Lookup("target")
	require.NotNil(t, flag, "--target flag should be registered on breaker command")
	assert.Equal(t, "t", flag.Shorthand)
}

func TestBreakerStatusCmd_Registration(t *testing.T) {
	cmd, _, err := breakerCmd.Find([]string{"status"})
	require.NoError(t, err)
	assert.Equal(t, "status", cmd.Name())
}

func TestBreakerResetCmd_Registration(t *testing.T) {
	cmd, _, err := breakerCmd.Find([]string{"reset"})
	require.NoError(t, err)
	assert.Equal(t, "reset", cmd.Name())
}

func TestResolveBreakerStateFile_Default(t *testing.T) {
	t.Setenv("BOSUN_STATE_DIR", "")

	// Reset flag state to avoid cross-test pollution.
	orig := breakerTarget
	breakerTarget = ""
	defer func() { breakerTarget = orig }()

	origDir := breakerStateDir
	breakerStateDir = "/var/lib/bosun"
	defer func() { breakerStateDir = origDir }()

	got := resolveBreakerStateFile()
	assert.Equal(t, filepath.Join("/var/lib/bosun", reconcile.DefaultStateFile), got)
}

func TestResolveBreakerStateFile_WithTarget(t *testing.T) {
	t.Setenv("BOSUN_STATE_DIR", "")

	orig := breakerTarget
	breakerTarget = "unraid"
	defer func() { breakerTarget = orig }()

	origDir := breakerStateDir
	breakerStateDir = "/var/lib/bosun"
	defer func() { breakerStateDir = origDir }()

	got := resolveBreakerStateFile()
	// Named target should produce deploy-state-<name>.json
	assert.Equal(t, filepath.Join("/var/lib/bosun", "deploy-state-unraid.json"), got)
}

func TestResolveBreakerStateFile_StateDirEnvVar(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BOSUN_STATE_DIR", tmpDir)

	orig := breakerTarget
	breakerTarget = ""
	defer func() { breakerTarget = orig }()

	got := resolveBreakerStateFile()
	assert.Equal(t, filepath.Join(tmpDir, reconcile.DefaultStateFile), got)
}

func TestBreakerStatus_ClosedBreaker(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BOSUN_STATE_DIR", tmpDir)

	orig := breakerTarget
	breakerTarget = ""
	defer func() { breakerTarget = orig }()

	// No state file — breaker should report closed.
	err := runBreakerStatus(breakerStatusCmd, nil)
	require.NoError(t, err)
}

func TestBreakerReset_NothingToReset(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BOSUN_STATE_DIR", tmpDir)

	orig := breakerTarget
	breakerTarget = ""
	defer func() { breakerTarget = orig }()

	err := runBreakerReset(breakerResetCmd, nil)
	require.NoError(t, err)
}

func TestBreakerReset_ClearsFailureCount(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BOSUN_STATE_DIR", tmpDir)

	orig := breakerTarget
	breakerTarget = ""
	defer func() { breakerTarget = orig }()

	stateFile := filepath.Join(tmpDir, reconcile.DefaultStateFile)

	// Seed state with failures.
	state := &reconcile.DeployState{
		AttemptCount:        3,
		LastAttemptedCommit: "abc1234",
		LastAlertedAttempt:  3,
	}
	require.NoError(t, reconcile.SaveState(stateFile, state))

	err := runBreakerReset(breakerResetCmd, nil)
	require.NoError(t, err)

	// Verify state was cleared.
	cleared := reconcile.LoadState(stateFile)
	assert.Equal(t, 0, cleared.AttemptCount)
	assert.Equal(t, "", cleared.LastAttemptedCommit)
	assert.Equal(t, 0, cleared.LastAlertedAttempt)
}

func TestBreakerStatus_TargetStateFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BOSUN_STATE_DIR", tmpDir)

	orig := breakerTarget
	breakerTarget = "staging"
	defer func() { breakerTarget = orig }()

	origDir := breakerStateDir
	breakerStateDir = tmpDir
	defer func() { breakerStateDir = origDir }()

	// Seed failure state in the named target's state file.
	targetStateFile := filepath.Join(tmpDir, "deploy-state-staging.json")
	state := &reconcile.DeployState{
		AttemptCount:        3,
		LastAttemptedCommit: "deadbeef",
	}
	require.NoError(t, reconcile.SaveState(targetStateFile, state))

	// Status should read from the target-specific file (no error = success).
	err := runBreakerStatus(breakerStatusCmd, nil)
	require.NoError(t, err)
}
