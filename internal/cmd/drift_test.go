package cmd

import (
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/reconcile"
)

func TestDriftCmd_Registration(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"drift"})
	require.NoError(t, err)
	assert.Equal(t, "drift", cmd.Name())
}

func TestDriftCmd_Flags(t *testing.T) {
	t.Run("drift --help shows description", func(t *testing.T) {
		output, err := executeCmd(t, "drift", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "drift")
		assert.Contains(t, output, "declared")
	})

	t.Run("drift --help shows examples", func(t *testing.T) {
		output, err := executeCmd(t, "drift", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "bosun drift")
		assert.Contains(t, output, "--live")
		assert.Contains(t, output, "--json")
	})
}

func TestDriftCmd_NoAliases(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"drift"})
	require.NoError(t, err)
	assert.Empty(t, cmd.Aliases, "drift command should have no aliases")
}

// captureStdout redirects os.Stdout to a pipe, runs fn, then returns what was written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)

	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old

	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
}

func TestPrintDriftJSON_Clean(t *testing.T) {
	state := &reconcile.DeployState{
		LastDeployedCommit: "abc123def456",
		DeployedAt:         time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
		DeclaredServices: []reconcile.DeclaredService{
			{Name: "web", Image: "nginx:1.25"},
			{Name: "api", Image: "myapp:v2"},
		},
		DriftItems: nil,
	}

	// Set the package-level driftJSON flag
	oldFlag := driftJSON
	driftJSON = true
	defer func() { driftJSON = oldFlag }()

	output := captureStdout(t, func() {
		printDriftJSON(state)
	})

	var result driftJSONOutput
	require.NoError(t, json.Unmarshal([]byte(output), &result))

	assert.Equal(t, "clean", result.Status)
	assert.Equal(t, 2, result.DeclaredCount)
	assert.Equal(t, 0, result.DriftItemCount)
	assert.Empty(t, result.Items)
	assert.Equal(t, "abc123def456", result.DeployedCommit)
	assert.NotNil(t, result.DeployedAt)
}

func TestPrintDriftJSON_Drifted(t *testing.T) {
	checkedAt := time.Date(2024, 6, 15, 13, 0, 0, 0, time.UTC)
	state := &reconcile.DeployState{
		LastDeployedCommit: "abc123def456",
		DeployedAt:         time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
		DriftCheckedAt:     checkedAt,
		DeclaredServices: []reconcile.DeclaredService{
			{Name: "web", Image: "nginx:1.25"},
		},
		DriftItems: []reconcile.DriftItem{
			{Service: "web", Type: reconcile.DriftImageMismatch, Declared: "nginx:1.25", Actual: "nginx:1.24"},
		},
	}

	oldFlag := driftJSON
	driftJSON = true
	defer func() { driftJSON = oldFlag }()

	output := captureStdout(t, func() {
		printDriftJSON(state)
	})

	var result driftJSONOutput
	require.NoError(t, json.Unmarshal([]byte(output), &result))

	assert.Equal(t, "drifted", result.Status)
	assert.Equal(t, 1, result.DeclaredCount)
	assert.Equal(t, 1, result.DriftItemCount)
	require.Len(t, result.Items, 1)
	assert.Equal(t, "web", result.Items[0].Service)
	assert.NotNil(t, result.CheckedAt)
}

func TestPrintDriftJSON_NoDeployTime(t *testing.T) {
	state := &reconcile.DeployState{
		LastDeployedCommit: "abc123",
		DeclaredServices:   nil,
		DriftItems:         nil,
	}

	oldFlag := driftJSON
	driftJSON = true
	defer func() { driftJSON = oldFlag }()

	output := captureStdout(t, func() {
		printDriftJSON(state)
	})

	var result driftJSONOutput
	require.NoError(t, json.Unmarshal([]byte(output), &result))

	assert.Nil(t, result.DeployedAt)
	assert.Nil(t, result.CheckedAt)
	assert.Equal(t, "clean", result.Status)
}

func TestPrintDriftHuman_Clean(t *testing.T) {
	state := &reconcile.DeployState{
		LastDeployedCommit: "abc123def456",
		DeployedAt:         time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
		DeclaredServices: []reconcile.DeclaredService{
			{Name: "web", Image: "nginx:1.25"},
		},
		DriftItems: nil,
	}

	// printDriftHuman uses ui.Green/Yellow/Red (fatih/color) which write to their own
	// output streams, not os.Stdout. Only fmt.Printf lines are captured via stdout.
	output := captureStdout(t, func() {
		printDriftHuman(state)
	})

	assert.Contains(t, output, "abc123de")
	assert.Contains(t, output, "Declared services: 1")
	assert.Contains(t, output, "Last checked:    never")
}

func TestPrintDriftHuman_WithDrift(t *testing.T) {
	state := &reconcile.DeployState{
		LastDeployedCommit: "abc123def456",
		DeployedAt:         time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
		DriftCheckedAt:     time.Now().Add(-5 * time.Minute),
		DeclaredServices: []reconcile.DeclaredService{
			{Name: "web", Image: "nginx:1.25"},
			{Name: "api", Image: "myapp:v2"},
		},
		DriftItems: []reconcile.DriftItem{
			{Service: "web", Type: reconcile.DriftImageMismatch, Declared: "nginx:1.25", Actual: "nginx:1.24"},
			{Service: "api", Type: reconcile.DriftMissing, Declared: "myapp:v2"},
			{Service: "db", Type: reconcile.DriftUnhealthy},
		},
	}

	// Only fmt.Printf output is captured; colored ui.Yellow/Red/Green output goes
	// to fatih/color's writer. We verify the plain-text portions (declared/actual lines).
	output := captureStdout(t, func() {
		printDriftHuman(state)
	})

	assert.Contains(t, output, "Deployed commit: abc123de")
	assert.Contains(t, output, "Declared services: 2")
	assert.Contains(t, output, "declared: nginx:1.25")
	assert.Contains(t, output, "actual:   nginx:1.24")
}

func TestPrintDriftHuman_NeverChecked(t *testing.T) {
	state := &reconcile.DeployState{
		LastDeployedCommit: "abc123def456",
		DeclaredServices: []reconcile.DeclaredService{
			{Name: "web", Image: "nginx:1.25"},
		},
	}

	output := captureStdout(t, func() {
		printDriftHuman(state)
	})

	assert.Contains(t, output, "Last checked:    never")
}

func TestPrintDriftStatus_RoutesToJSON(t *testing.T) {
	state := &reconcile.DeployState{
		LastDeployedCommit: "abc123def456",
		DeclaredServices: []reconcile.DeclaredService{
			{Name: "web", Image: "nginx:1.25"},
		},
	}

	oldFlag := driftJSON
	driftJSON = true
	defer func() { driftJSON = oldFlag }()

	output := captureStdout(t, func() {
		printDriftStatus(state)
	})

	// Should produce valid JSON since driftJSON=true
	var result driftJSONOutput
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.Equal(t, "clean", result.Status)
}

func TestPrintDriftStatus_RoutesToHuman(t *testing.T) {
	state := &reconcile.DeployState{
		LastDeployedCommit: "abc123def456",
		DeclaredServices: []reconcile.DeclaredService{
			{Name: "web", Image: "nginx:1.25"},
		},
	}

	oldFlag := driftJSON
	driftJSON = false
	defer func() { driftJSON = oldFlag }()

	output := captureStdout(t, func() {
		printDriftStatus(state)
	})

	// Should produce human-readable output
	assert.Contains(t, output, "Deployed commit: abc123de")
	assert.Contains(t, output, "Declared services: 1")
}

func TestDriftJSONOutput_Structure(t *testing.T) {
	checkedAt := "2024-06-15T12:00:00Z"
	deployedAt := "2024-06-15T11:00:00Z"

	out := driftJSONOutput{
		Status:         "drifted",
		CheckedAt:      &checkedAt,
		DeclaredCount:  3,
		DriftItemCount: 2,
		Items: []reconcile.DriftItem{
			{Service: "web", Type: reconcile.DriftMissing},
			{Service: "db", Type: reconcile.DriftUnhealthy},
		},
		DeployedCommit: "abc123",
		DeployedAt:     &deployedAt,
	}

	data, err := json.Marshal(out)
	require.NoError(t, err)

	var decoded driftJSONOutput
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, "drifted", decoded.Status)
	assert.Equal(t, 3, decoded.DeclaredCount)
	assert.Equal(t, 2, decoded.DriftItemCount)
	assert.Len(t, decoded.Items, 2)
	assert.Equal(t, "abc123", decoded.DeployedCommit)
	assert.NotNil(t, decoded.CheckedAt)
	assert.NotNil(t, decoded.DeployedAt)
}
