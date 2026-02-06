package reconcile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
)

// DefaultStateDir is the default directory for persistent deploy state.
// Uses /var/lib/bosun/ per FHS conventions for mutable application state.
const DefaultStateDir = "/var/lib/bosun"

// DefaultStateFile is the default filename for the deploy state file.
const DefaultStateFile = "deploy-state.json"

// MaxAttempts is the circuit breaker threshold — after this many consecutive
// failures on the same commit, stop retrying until a new commit or --force.
const MaxAttempts = 3

// currentSchemaVersion is the schema version written to new state files.
const currentSchemaVersion = 1

// DeployState tracks the last successful deployment and attempt history.
type DeployState struct {
	SchemaVersion      int       `json:"schema_version"`
	LastDeployedCommit string    `json:"last_deployed_commit,omitempty"`
	DeployedAt         time.Time `json:"deployed_at,omitempty"`
	Source             string    `json:"source,omitempty"`
	LastAttemptedCommit string   `json:"last_attempted_commit,omitempty"`
	AttemptCount       int       `json:"attempt_count,omitempty"`
}

// LoadState reads the deploy state file. Returns zero state on missing or
// corrupt files — this is correct fail-open behavior (triggers a full deploy).
func LoadState(path string) *DeployState {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &DeployState{SchemaVersion: currentSchemaVersion}
		}
		log.Warn().
			Str(log.FieldComponent, log.ComponentReconcile).
			Str(log.FieldPath, path).
			Err(err).
			Msg("Failed to read state file, treating as never deployed")
		return &DeployState{SchemaVersion: currentSchemaVersion}
	}

	var state DeployState
	if err := json.Unmarshal(data, &state); err != nil {
		log.Warn().
			Str(log.FieldComponent, log.ComponentReconcile).
			Str(log.FieldPath, path).
			Err(err).
			Msg("Corrupt state file, treating as never deployed")
		return &DeployState{SchemaVersion: currentSchemaVersion}
	}

	return &state
}

// SaveState atomically writes the deploy state to disk.
// Uses the pattern: write temp (same dir) → fsync temp → rename → fsync dir.
func SaveState(path string, state *DeployState) error {
	state.SchemaVersion = currentSchemaVersion

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)

	// Create temp file in same directory to avoid EXDEV on rename.
	tmp, err := os.CreateTemp(dir, ".deploy-state-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp state file: %w", err)
	}
	tmpPath := tmp.Name()

	// Clean up temp file on any error path.
	success := false
	defer func() {
		if !success {
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write temp state file: %w", err)
	}

	// Fsync the temp file to ensure data hits disk before rename.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to fsync temp state file: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp state file: %w", err)
	}

	// Atomic rename.
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to rename state file: %w", err)
	}

	// Fsync the directory to ensure the rename is durable.
	if err := fsyncDir(dir); err != nil {
		// Non-fatal: the rename succeeded, data is likely durable.
		log.Warn().
			Str(log.FieldComponent, log.ComponentReconcile).
			Str(log.FieldPath, dir).
			Err(err).
			Msg("Failed to fsync state directory (rename succeeded)")
	}

	success = true
	return nil
}

// fsyncDir opens a directory and fsyncs it to make metadata changes durable.
func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
