package cmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
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

	_ = w.Close()
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

// saveDriftFlags saves and restores all package-level drift flag vars.
func saveDriftFlags(t *testing.T) {
	t.Helper()
	oldLive, oldJSON, oldStateFile, oldProjectName, oldTarget :=
		driftLive, driftJSON, driftStateFile, driftProjectName, driftTarget
	t.Cleanup(func() {
		driftLive = oldLive
		driftJSON = oldJSON
		driftStateFile = oldStateFile
		driftProjectName = oldProjectName
		driftTarget = oldTarget
	})
}

// makeState creates a deploy state file in the given directory at the target-specific path.
func makeState(t *testing.T, dir string, target reconcile.Target, state *reconcile.DeployState) {
	t.Helper()
	sf := reconcile.TargetStateFile(dir, target)
	require.NoError(t, os.MkdirAll(filepath.Dir(sf), 0o755))
	require.NoError(t, reconcile.SaveState(sf, state))
}

func TestLoadConfiguredTargets(t *testing.T) {
	t.Run("from_env_valid_JSON", func(t *testing.T) {
		t.Setenv("BOSUN_TARGETS", `[{"name":"alpha"},{"name":"beta"}]`)
		targets := loadConfiguredTargets()
		require.Len(t, targets, 2)
		assert.Equal(t, "alpha", targets[0].Name)
		assert.Equal(t, "beta", targets[1].Name)
	})

	t.Run("from_env_invalid_JSON_returns_nil", func(t *testing.T) {
		t.Setenv("BOSUN_TARGETS", "not-json")
		targets := loadConfiguredTargets()
		assert.Nil(t, targets)
	})

	t.Run("from_env_empty_array_returns_nil", func(t *testing.T) {
		t.Setenv("BOSUN_TARGETS", "[]")
		targets := loadConfiguredTargets()
		assert.Nil(t, targets)
	})

	t.Run("no_env_no_config_returns_nil", func(t *testing.T) {
		t.Setenv("BOSUN_TARGETS", "")
		targets := loadConfiguredTargets()
		assert.Nil(t, targets)
	})
}

func TestRunMultiTargetDriftJSON(t *testing.T) {
	t.Run("two_targets_clean", func(t *testing.T) {
		saveDriftFlags(t)
		tmpDir := evalSymlinks(t, t.TempDir())
		driftStateFile = filepath.Join(tmpDir, reconcile.DefaultStateFile)
		driftLive = false

		targets := []reconcile.Target{
			{Name: "primary"},
			{Name: "secondary"},
		}
		for _, tgt := range targets {
			makeState(t, tmpDir, tgt, &reconcile.DeployState{
				LastDeployedCommit: "abc123",
				DeclaredServices:   []reconcile.DeclaredService{{Name: "web", Image: "nginx"}},
			})
		}

		output := captureStdout(t, func() {
			runMultiTargetDriftJSON(targets)
		})

		var result multiTargetDriftJSON
		require.NoError(t, json.Unmarshal([]byte(output), &result))
		require.Len(t, result.Targets, 2)
		assert.Equal(t, "primary", result.Targets[0].Target)
		assert.Equal(t, "clean", result.Targets[0].Status)
		assert.Equal(t, "secondary", result.Targets[1].Target)
		assert.Equal(t, "clean", result.Targets[1].Status)
	})

	t.Run("one_drifted_one_unknown", func(t *testing.T) {
		saveDriftFlags(t)
		tmpDir := evalSymlinks(t, t.TempDir())
		driftStateFile = filepath.Join(tmpDir, reconcile.DefaultStateFile)
		driftLive = false

		targets := []reconcile.Target{
			{Name: "alpha"},
			{Name: "beta"},
		}
		makeState(t, tmpDir, targets[0], &reconcile.DeployState{
			LastDeployedCommit: "abc123",
			DeclaredServices:   []reconcile.DeclaredService{{Name: "web", Image: "nginx"}},
			DriftItems:         []reconcile.DriftItem{{Service: "web", Type: reconcile.DriftMissing}},
		})
		// beta has no state file — LoadState returns empty commit

		output := captureStdout(t, func() {
			runMultiTargetDriftJSON(targets)
		})

		var result multiTargetDriftJSON
		require.NoError(t, json.Unmarshal([]byte(output), &result))
		require.Len(t, result.Targets, 2)
		assert.Equal(t, "drifted", result.Targets[0].Status)
		assert.Equal(t, "unknown", result.Targets[1].Status)
	})

	t.Run("items_array_never_null", func(t *testing.T) {
		saveDriftFlags(t)
		tmpDir := evalSymlinks(t, t.TempDir())
		driftStateFile = filepath.Join(tmpDir, reconcile.DefaultStateFile)
		driftLive = false

		targets := []reconcile.Target{{Name: "clean-target"}}
		makeState(t, tmpDir, targets[0], &reconcile.DeployState{
			LastDeployedCommit: "abc123",
			DeclaredServices:   []reconcile.DeclaredService{{Name: "web", Image: "nginx"}},
		})

		output := captureStdout(t, func() {
			runMultiTargetDriftJSON(targets)
		})

		// Verify items is [] not null in raw JSON
		assert.Contains(t, output, `"items": []`)
	})
}

func TestRunMultiTargetDrift_Human(t *testing.T) {
	t.Run("two_targets_both_deployed", func(t *testing.T) {
		saveDriftFlags(t)
		tmpDir := evalSymlinks(t, t.TempDir())
		driftStateFile = filepath.Join(tmpDir, reconcile.DefaultStateFile)
		driftLive = false
		driftJSON = false

		targets := []reconcile.Target{
			{Name: "unraid"},
			{Name: "pi"},
		}
		for _, tgt := range targets {
			makeState(t, tmpDir, tgt, &reconcile.DeployState{
				LastDeployedCommit: "abc123",
				DeclaredServices:   []reconcile.DeclaredService{{Name: "web", Image: "nginx"}},
			})
		}

		output := captureStdout(t, func() {
			runMultiTargetDrift(targets)
		})

		assert.Contains(t, output, "Deployed commit: abc123")
	})

	t.Run("one_deployed_one_not", func(t *testing.T) {
		saveDriftFlags(t)
		tmpDir := evalSymlinks(t, t.TempDir())
		driftStateFile = filepath.Join(tmpDir, reconcile.DefaultStateFile)
		driftLive = false
		driftJSON = false

		targets := []reconcile.Target{
			{Name: "deployed"},
			{Name: "fresh"},
		}
		makeState(t, tmpDir, targets[0], &reconcile.DeployState{
			LastDeployedCommit: "abc123",
			DeclaredServices:   []reconcile.DeclaredService{{Name: "web", Image: "nginx"}},
		})
		// fresh has no state file

		output := captureStdout(t, func() {
			runMultiTargetDrift(targets)
		})

		assert.Contains(t, output, "Deployed commit: abc123")
		// The "No deployments recorded" warning goes through ui.Warning (colored output),
		// not fmt.Printf, so it won't appear in stdout capture.
	})
}

func TestRunDrift_MultiTargetAutoDetection(t *testing.T) {
	t.Run("multi_target_routes_to_json", func(t *testing.T) {
		saveDriftFlags(t)
		tmpDir := evalSymlinks(t, t.TempDir())
		driftStateFile = filepath.Join(tmpDir, reconcile.DefaultStateFile)
		driftTarget = ""
		driftJSON = true
		driftLive = false

		t.Setenv("BOSUN_TARGETS", `[{"name":"a"},{"name":"b"}]`)
		makeState(t, tmpDir, reconcile.Target{Name: "a"}, &reconcile.DeployState{
			LastDeployedCommit: "commit-a",
			DeclaredServices:   []reconcile.DeclaredService{{Name: "web", Image: "nginx"}},
		})
		makeState(t, tmpDir, reconcile.Target{Name: "b"}, &reconcile.DeployState{
			LastDeployedCommit: "commit-b",
			DeclaredServices:   []reconcile.DeclaredService{{Name: "api", Image: "app:v1"}},
		})

		output := captureStdout(t, func() {
			runDrift(nil, nil)
		})

		var result multiTargetDriftJSON
		require.NoError(t, json.Unmarshal([]byte(output), &result), "should produce valid multi-target JSON")
		require.Len(t, result.Targets, 2)
		assert.Equal(t, "a", result.Targets[0].Target)
		assert.Equal(t, "b", result.Targets[1].Target)
	})

	t.Run("single_target_falls_through_to_normal", func(t *testing.T) {
		saveDriftFlags(t)
		tmpDir := evalSymlinks(t, t.TempDir())
		driftStateFile = filepath.Join(tmpDir, reconcile.DefaultStateFile)
		driftTarget = ""
		driftJSON = true
		driftLive = false

		t.Setenv("BOSUN_TARGETS", `[{"name":"solo"}]`)
		// Write state at the DEFAULT path since single target falls through
		require.NoError(t, reconcile.SaveState(driftStateFile, &reconcile.DeployState{
			LastDeployedCommit: "commit-solo",
			DeclaredServices:   []reconcile.DeclaredService{{Name: "web", Image: "nginx"}},
		}))

		output := captureStdout(t, func() {
			runDrift(nil, nil)
		})

		// Single-target path produces driftJSONOutput, not multiTargetDriftJSON
		var result driftJSONOutput
		require.NoError(t, json.Unmarshal([]byte(output), &result))
		assert.Equal(t, "clean", result.Status)
	})
}

func TestRunDrift_TargetFlag(t *testing.T) {
	t.Run("target_found_resolves_project_name", func(t *testing.T) {
		saveDriftFlags(t)
		tmpDir := evalSymlinks(t, t.TempDir())
		driftStateFile = filepath.Join(tmpDir, reconcile.DefaultStateFile)
		driftTarget = "nas"
		driftJSON = true
		driftLive = false
		driftProjectName = ""

		t.Setenv("BOSUN_TARGETS", `[{"name":"nas","project_name":"homelab"}]`)
		makeState(t, tmpDir, reconcile.Target{Name: "nas"}, &reconcile.DeployState{
			LastDeployedCommit: "abc123",
			DeclaredServices:   []reconcile.DeclaredService{{Name: "web", Image: "nginx"}},
		})

		output := captureStdout(t, func() {
			runDrift(nil, nil)
		})

		var result driftJSONOutput
		require.NoError(t, json.Unmarshal([]byte(output), &result))
		assert.Equal(t, "clean", result.Status)
		assert.Equal(t, "abc123", result.DeployedCommit)
	})

	t.Run("target_not_in_config_derives_by_name", func(t *testing.T) {
		saveDriftFlags(t)
		tmpDir := evalSymlinks(t, t.TempDir())
		driftStateFile = filepath.Join(tmpDir, reconcile.DefaultStateFile)
		driftTarget = "unknown"
		driftJSON = true
		driftLive = false

		t.Setenv("BOSUN_TARGETS", "")
		// Create state at derived path
		makeState(t, tmpDir, reconcile.Target{Name: "unknown"}, &reconcile.DeployState{
			LastDeployedCommit: "xyz789",
			DeclaredServices:   []reconcile.DeclaredService{{Name: "app", Image: "app:v1"}},
		})

		output := captureStdout(t, func() {
			runDrift(nil, nil)
		})

		var result driftJSONOutput
		require.NoError(t, json.Unmarshal([]byte(output), &result))
		assert.Equal(t, "clean", result.Status)
		assert.Equal(t, "xyz789", result.DeployedCommit)
	})

	t.Run("no_deployment_returns_unknown_json", func(t *testing.T) {
		saveDriftFlags(t)
		tmpDir := evalSymlinks(t, t.TempDir())
		driftStateFile = filepath.Join(tmpDir, reconcile.DefaultStateFile)
		driftTarget = "empty"
		driftJSON = true
		driftLive = false

		t.Setenv("BOSUN_TARGETS", "")
		// No state file exists

		output := captureStdout(t, func() {
			runDrift(nil, nil)
		})

		var result driftJSONOutput
		require.NoError(t, json.Unmarshal([]byte(output), &result))
		assert.Equal(t, "unknown", result.Status)
	})
}

func TestBuildDriftJSON(t *testing.T) {
	t.Run("clean_state", func(t *testing.T) {
		state := &reconcile.DeployState{
			LastDeployedCommit: "abc123",
			DeclaredServices:   []reconcile.DeclaredService{{Name: "web", Image: "nginx"}},
		}
		out := buildDriftJSON(state)
		assert.Equal(t, "clean", out.Status)
		assert.Equal(t, 1, out.DeclaredCount)
		assert.Equal(t, 0, out.DriftItemCount)
		assert.NotNil(t, out.Items)
		assert.Empty(t, out.Items)
	})

	t.Run("drifted_state", func(t *testing.T) {
		state := &reconcile.DeployState{
			LastDeployedCommit: "abc123",
			DeclaredServices:   []reconcile.DeclaredService{{Name: "web", Image: "nginx"}},
			DriftItems:         []reconcile.DriftItem{{Service: "web", Type: reconcile.DriftMissing}},
		}
		out := buildDriftJSON(state)
		assert.Equal(t, "drifted", out.Status)
		assert.Equal(t, 1, out.DriftItemCount)
		require.Len(t, out.Items, 1)
	})

	t.Run("timestamps_formatted", func(t *testing.T) {
		checkedAt := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
		deployedAt := time.Date(2026, 3, 19, 11, 0, 0, 0, time.UTC)
		state := &reconcile.DeployState{
			LastDeployedCommit: "abc123",
			DriftCheckedAt:     checkedAt,
			DeployedAt:         deployedAt,
		}
		out := buildDriftJSON(state)
		require.NotNil(t, out.CheckedAt)
		require.NotNil(t, out.DeployedAt)
		assert.Contains(t, *out.CheckedAt, "2026-03-19")
		assert.Contains(t, *out.DeployedAt, "2026-03-19")
	})
}

func TestLoadDriftIgnoreRules(t *testing.T) {
	t.Run("from_env_valid", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_IGNORE", `[{"service":"traefik","type":"image_mismatch"}]`)
		rules := loadDriftIgnoreRules()
		require.Len(t, rules, 1)
		assert.Equal(t, "traefik", rules[0].Service)
	})

	t.Run("from_env_invalid_JSON_returns_nil", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_IGNORE", "not-json")
		rules := loadDriftIgnoreRules()
		assert.Nil(t, rules)
	})

	t.Run("no_env_no_config_returns_nil", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_IGNORE", "")
		rules := loadDriftIgnoreRules()
		assert.Nil(t, rules)
	})
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
