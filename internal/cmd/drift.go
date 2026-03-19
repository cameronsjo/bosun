package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/config"
	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/cameronsjo/bosun/internal/ui"
)

var (
	driftLive        bool
	driftJSON        bool
	driftStateFile   string
	driftProjectName string
	driftTarget      string
)

var driftCmd = &cobra.Command{
	Use:   "drift",
	Short: "Show drift between declared and actual state",
	Long: `Show drift between declared services (from manifests) and actual
running containers. By default reads cached drift status from the
deploy state file. Use --live to perform a fresh check against Docker.

Examples:
  bosun drift                       # Show last drift check result
  bosun drift --live                # Check Docker right now
  bosun drift --json                # Machine-readable output
  bosun drift --target=unraid       # Show drift for a specific target`,
	Run: runDrift,
}

func init() {
	driftCmd.Flags().BoolVar(&driftLive, "live", false, "Perform a live drift check against Docker")
	driftCmd.Flags().BoolVar(&driftJSON, "json", false, "Output as JSON")
	driftCmd.Flags().StringVar(&driftStateFile, "state-file", filepath.Join(reconcile.DefaultStateDir, reconcile.DefaultStateFile), "Path to deploy state file")
	driftCmd.Flags().StringVar(&driftProjectName, "project", "", "Docker Compose project name for filtering")
	driftCmd.Flags().StringVarP(&driftTarget, "target", "t", "", "Show drift for a specific named target")

	rootCmd.AddCommand(driftCmd)
}

// driftJSONOutput is the JSON representation of drift status.
type driftJSONOutput struct {
	Status         string                   `json:"status"`
	CheckedAt      *string                  `json:"checked_at,omitempty"`
	DeclaredCount  int                      `json:"declared_count"`
	DriftItemCount int                      `json:"drift_item_count"`
	Items          []reconcile.DriftItem    `json:"items"`
	DeployedCommit string                   `json:"deployed_commit,omitempty"`
	DeployedAt     *string                  `json:"deployed_at,omitempty"`
}

func runDrift(cmd *cobra.Command, args []string) {
	// If --target is specified, resolve the full target descriptor so state file,
	// project name, and live mode all use the correct target context.
	if driftTarget != "" {
		targets := loadConfiguredTargets()
		var resolved bool
		for _, t := range targets {
			if t.Name == driftTarget {
				stateDir := filepath.Dir(driftStateFile)
				driftStateFile = reconcile.TargetStateFile(stateDir, t)
				if driftProjectName == "" && t.ProjectName != "" {
					driftProjectName = t.ProjectName
				}
				resolved = true
				break
			}
		}
		if !resolved {
			// Target not in config — still derive the state file by name.
			stateDir := filepath.Dir(driftStateFile)
			t := reconcile.Target{Name: driftTarget}
			driftStateFile = reconcile.TargetStateFile(stateDir, t)
		}
	}

	// Check if we should show drift for all targets (no --target, multi-target config).
	if driftTarget == "" {
		targets := loadConfiguredTargets()
		if len(targets) > 1 {
			if driftJSON {
				runMultiTargetDriftJSON(targets)
				return
			}
			runMultiTargetDrift(targets)
			return
		}
	}

	state := reconcile.LoadState(driftStateFile)

	if state.LastDeployedCommit == "" {
		if driftJSON {
			out := driftJSONOutput{Status: "unknown", Items: []reconcile.DriftItem{}}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(out)
			return
		}
		ui.Warning("No deployments recorded yet. Run a reconciliation first.")
		return
	}

	if driftLive {
		runLiveDriftCheck(state)
		return
	}

	// Show cached drift status from state file.
	printDriftStatus(state)
}

// loadConfiguredTargets returns targets from config or env, or nil.
func loadConfiguredTargets() []reconcile.Target {
	if v := os.Getenv("BOSUN_TARGETS"); v != "" {
		var targets []reconcile.Target
		if err := json.Unmarshal([]byte(v), &targets); err == nil && len(targets) > 0 {
			return targets
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	return cfg.Targets()
}

// runMultiTargetDrift shows drift status for all configured targets (human output).
func runMultiTargetDrift(targets []reconcile.Target) {
	stateDir := filepath.Dir(driftStateFile)

	for i, t := range targets {
		if i > 0 {
			fmt.Println()
		}
		ui.Header("Target: %s", t.Name)

		sf := reconcile.TargetStateFile(stateDir, t)
		state := reconcile.LoadState(sf)

		if state.LastDeployedCommit == "" {
			ui.Warning("No deployments recorded for target %s", t.Name)
			continue
		}

		if driftLive {
			projectName := t.ProjectName
			runLiveDriftCheckForTarget(state, sf, projectName)
		} else {
			printDriftHuman(state)
		}
	}
}

// multiTargetDriftJSON is the JSON representation for multi-target drift.
type multiTargetDriftJSON struct {
	Targets []targetDriftJSON `json:"targets"`
}

// targetDriftJSON wraps a single target's drift output.
type targetDriftJSON struct {
	Target string `json:"target"`
	driftJSONOutput
}

// runMultiTargetDriftJSON emits a single JSON array for all targets.
func runMultiTargetDriftJSON(targets []reconcile.Target) {
	stateDir := filepath.Dir(driftStateFile)
	out := multiTargetDriftJSON{Targets: make([]targetDriftJSON, 0, len(targets))}

	for _, t := range targets {
		sf := reconcile.TargetStateFile(stateDir, t)
		state := reconcile.LoadState(sf)

		if driftLive && state.LastDeployedCommit != "" {
			projectName := t.ProjectName
			state = runLiveDriftCollect(state, sf, projectName)
		}

		entry := targetDriftJSON{Target: t.Name}
		if state.LastDeployedCommit == "" {
			entry.driftJSONOutput = driftJSONOutput{Status: "unknown", Items: []reconcile.DriftItem{}}
		} else {
			entry.driftJSONOutput = buildDriftJSON(state)
		}
		out.Targets = append(out.Targets, entry)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

func runLiveDriftCheck(state *reconcile.DeployState) {
	if len(state.DeclaredServices) == 0 {
		if driftJSON {
			out := driftJSONOutput{Status: "unknown", Items: []reconcile.DriftItem{}}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(out)
			return
		}
		ui.Warning("No declared services in state file. Deploy first to populate declared state.")
		return
	}

	state = runLiveDriftCollect(state, driftStateFile, driftProjectName)
	printDriftStatus(state)
}

// runLiveDriftCheckForTarget performs a live check and prints human output for a specific target.
func runLiveDriftCheckForTarget(state *reconcile.DeployState, stateFile, projectName string) {
	if len(state.DeclaredServices) == 0 {
		ui.Warning("No declared services in state file. Deploy first to populate declared state.")
		return
	}

	state = runLiveDriftCollect(state, stateFile, projectName)
	printDriftHuman(state)
}

// runLiveDriftCollect performs the live Docker check and updates the state file.
// Returns the updated state (for JSON consumers that need the result).
func runLiveDriftCollect(state *reconcile.DeployState, stateFile, projectName string) *reconcile.DeployState {
	client, err := docker.NewClient()
	if err != nil {
		ui.Fatal("Failed to connect to Docker: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	actual, err := reconcile.CollectActualState(ctx, client, projectName)
	if err != nil {
		ui.Fatal("Failed to collect actual state: %v", err)
	}

	report := reconcile.CompareDrift(state.DeclaredServices, actual)

	// Apply drift ignore rules from project config.
	ignoreRules := loadDriftIgnoreRules()
	if len(ignoreRules) > 0 {
		preFilterCount := len(report.Items)
		report.Items = reconcile.FilterIgnoredDriftItems(report.Items, ignoreRules)
		report.IgnoredCount = preFilterCount - len(report.Items)
	}

	// Update state file with live results.
	state.DriftCheckedAt = report.CheckedAt
	state.DriftItems = report.Items
	if err := reconcile.SaveState(stateFile, state); err != nil {
		ui.Warning("Failed to save drift results: %v", err)
	}

	return state
}

func printDriftStatus(state *reconcile.DeployState) {
	if driftJSON {
		printDriftJSON(state)
		return
	}
	printDriftHuman(state)
}

func printDriftJSON(state *reconcile.DeployState) {
	out := buildDriftJSON(state)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// buildDriftJSON constructs the JSON output struct from deploy state.
func buildDriftJSON(state *reconcile.DeployState) driftJSONOutput {
	status := "clean"
	if len(state.DriftItems) > 0 {
		status = "drifted"
	}

	out := driftJSONOutput{
		Status:         status,
		DeclaredCount:  len(state.DeclaredServices),
		DriftItemCount: len(state.DriftItems),
		Items:          state.DriftItems,
		DeployedCommit: state.LastDeployedCommit,
	}

	if !state.DriftCheckedAt.IsZero() {
		t := state.DriftCheckedAt.Format(time.RFC3339)
		out.CheckedAt = &t
	}
	if !state.DeployedAt.IsZero() {
		t := state.DeployedAt.Format(time.RFC3339)
		out.DeployedAt = &t
	}
	if out.Items == nil {
		out.Items = []reconcile.DriftItem{}
	}

	return out
}

func printDriftHuman(state *reconcile.DeployState) {
	ui.Header("=== Drift Status ===")
	fmt.Println()

	// Deployment info
	fmt.Printf("  Deployed commit: %s\n", state.LastDeployedCommit[:reconcile.MinLen(state.LastDeployedCommit, 8)])
	if !state.DeployedAt.IsZero() {
		fmt.Printf("  Deployed at:     %s\n", state.DeployedAt.Format(time.RFC3339))
	}
	fmt.Printf("  Declared services: %d\n", len(state.DeclaredServices))

	// Drift check time
	if !state.DriftCheckedAt.IsZero() {
		ago := time.Since(state.DriftCheckedAt).Round(time.Second)
		fmt.Printf("  Last checked:    %s ago\n", ago)
	} else {
		fmt.Printf("  Last checked:    never\n")
	}

	fmt.Println()

	if len(state.DriftItems) == 0 {
		_, _ = ui.Green.Printf("  %s No drift detected\n", "✓")
		fmt.Println()
		return
	}

	_, _ = ui.Yellow.Printf("  %s %d drift item(s) detected\n", "⚠", len(state.DriftItems))
	fmt.Println()

	for _, item := range state.DriftItems {
		switch item.Type {
		case reconcile.DriftMissing:
			_, _ = ui.Red.Printf("    ✗ %s: missing", item.Service)
			if item.Actual != "" {
				fmt.Printf(" (%s)", item.Actual)
			}
			fmt.Println()
		case reconcile.DriftImageMismatch:
			_, _ = ui.Yellow.Printf("    ⚠ %s: image mismatch\n", item.Service)
			fmt.Printf("        declared: %s\n", item.Declared)
			fmt.Printf("        actual:   %s\n", item.Actual)
		case reconcile.DriftUnhealthy:
			_, _ = ui.Red.Printf("    ✗ %s: unhealthy\n", item.Service)
		}
	}
	fmt.Println()
}

// loadDriftIgnoreRules loads drift ignore rules from environment variable
// (BOSUN_DRIFT_IGNORE) or project config. Env var takes precedence.
func loadDriftIgnoreRules() []reconcile.DriftIgnoreRule {
	// Environment variable override.
	if v := os.Getenv("BOSUN_DRIFT_IGNORE"); v != "" {
		var rules []reconcile.DriftIgnoreRule
		if err := json.Unmarshal([]byte(v), &rules); err != nil {
			ui.Warning("Failed to parse BOSUN_DRIFT_IGNORE: %v", err)
			return nil
		}
		return rules
	}

	// Fall back to project config.
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	return cfg.DriftIgnore()
}
