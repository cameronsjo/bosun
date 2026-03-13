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
const currentSchemaVersion = 2

// DriftType classifies the kind of drift detected.
type DriftType string

const (
	// DriftMissing indicates a declared service is not running.
	DriftMissing DriftType = "missing"
	// DriftImageMismatch indicates a running service uses a different image than declared.
	DriftImageMismatch DriftType = "image_mismatch"
	// DriftUnhealthy indicates a declared service is running but unhealthy.
	DriftUnhealthy DriftType = "unhealthy"
)

// DeclaredService represents a service that should be running per the manifests.
type DeclaredService struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

// DriftItem describes a single drift between declared and actual state.
type DriftItem struct {
	Service  string    `json:"service"`
	Type     DriftType `json:"type"`
	Declared string    `json:"declared,omitempty"`
	Actual   string    `json:"actual,omitempty"`
}

// DeployState tracks the last successful deployment and attempt history.
type DeployState struct {
	SchemaVersion      int       `json:"schema_version"`
	LastDeployedCommit string    `json:"last_deployed_commit,omitempty"`
	DeployedAt         time.Time `json:"deployed_at,omitempty"`
	DeployCount        int       `json:"deploy_count,omitempty"`
	Source             string    `json:"source,omitempty"`
	LastAttemptedCommit string   `json:"last_attempted_commit,omitempty"`
	AttemptCount        int      `json:"attempt_count,omitempty"`
	LastAlertedAttempt  int      `json:"last_alerted_attempt,omitempty"`

	// Declared state snapshot from last successful deployment.
	DeclaredServices []DeclaredService `json:"declared_services,omitempty"`

	// Drift detection results from last check.
	DriftCheckedAt time.Time   `json:"drift_checked_at,omitempty"`
	DriftItems     []DriftItem `json:"drift_items,omitempty"`

	// Drift alert deduplication state.
	DriftAlertedAt    time.Time            `json:"drift_alerted_at,omitempty"`
	DriftAlertedItems map[string]time.Time `json:"drift_alerted_items,omitempty"`

	// Drift alert debounce state: tracks first-seen timestamps for items
	// within their debounce window. Items graduate to the dedup layer
	// when the debounce duration elapses, or are removed if drift resolves.
	DriftDebounceItems map[string]time.Time `json:"drift_debounce_items,omitempty"`

	// NeedsRedeploy indicates that a previous deploy partially succeeded
	// (configs were synced to disk) but compose up failed. When true, the
	// next reconcile will re-run the deploy pipeline even if there are no
	// new git changes, bypassing the shouldSkipDeploy check.
	NeedsRedeploy bool `json:"needs_redeploy,omitempty"`

	// Health verification results from last post-deploy check.
	HealthVerifiedAt         time.Time `json:"health_verified_at,omitempty"`
	HealthVerificationPassed bool      `json:"health_verification_passed,omitempty"`

	// Restart circuit breaker: tracks per-container restart counts to detect crash loops.
	RestartTracking map[string]RestartTrackingEntry `json:"restart_tracking,omitempty"`
}

// RestartTrackingEntry tracks the restart count for a container across drift checks.
type RestartTrackingEntry struct {
	RestartCount int       `json:"restart_count"`
	CheckedAt    time.Time `json:"checked_at"`
	Tripped      bool      `json:"tripped"`
	TrippedAt    time.Time `json:"tripped_at,omitempty"`
}

// alertThresholds defines the attempt counts at which failure alerts are sent.
// After the last threshold, alerts repeat every alertRepeatInterval attempts.
var alertThresholds = []int{1, 3, 10, 30}

const alertRepeatInterval = 30

// ShouldAlert returns true if a failure alert should be sent for the current
// attempt count, given the last attempt that triggered an alert.
// Schedule: alert on attempt 1, 3, 10, 30, then every 30th attempt.
// Circuit breaker activation (attempt == MaxAttempts) always alerts.
func ShouldAlert(attemptCount, lastAlertedAttempt int) bool {
	if attemptCount <= lastAlertedAttempt {
		return false
	}

	// Circuit breaker activation always alerts.
	if attemptCount == MaxAttempts {
		return true
	}

	// Check fixed thresholds.
	for _, t := range alertThresholds {
		if attemptCount == t {
			return true
		}
	}

	// After the last threshold, alert every alertRepeatInterval attempts.
	lastThreshold := alertThresholds[len(alertThresholds)-1]
	if attemptCount > lastThreshold && (attemptCount-lastThreshold)%alertRepeatInterval == 0 {
		return true
	}

	return false
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

// DriftAlertKey returns a deduplication key for a drift item in the format "service:type".
func DriftAlertKey(item DriftItem) string {
	return item.Service + ":" + string(item.Type)
}

// ShouldAlertDrift compares current drift items against previously alerted items
// and returns which items should trigger new alerts and which have resolved.
//
// An item triggers an alert if it is new (not in alertedItems) or if its cooldown
// has expired. A key is considered resolved if it exists in alertedItems but is
// absent from currentItems.
func ShouldAlertDrift(currentItems []DriftItem, alertedItems map[string]time.Time, cooldown time.Duration) (alertItems []DriftItem, resolvedKeys []string) {
	now := time.Now()

	// Build set of current keys for resolution detection.
	currentKeys := make(map[string]bool, len(currentItems))
	for _, item := range currentItems {
		key := DriftAlertKey(item)
		currentKeys[key] = true

		lastAlerted, exists := alertedItems[key]
		if !exists || now.Sub(lastAlerted) >= cooldown {
			alertItems = append(alertItems, item)
		}
	}

	// Find resolved: in alertedItems but not in currentItems.
	for key := range alertedItems {
		if !currentKeys[key] {
			resolvedKeys = append(resolvedKeys, key)
		}
	}

	return alertItems, resolvedKeys
}

// FilterDebounced filters drift items through the debounce layer. Items that have
// not persisted beyond the debounce duration are held back. Items that have persisted
// past the debounce window are returned (graduated) for normal dedup processing.
//
// The debounceItems map is mutated in place:
//   - New items are added with the current timestamp
//   - Graduated items (past window) are removed
//   - Items no longer in currentItems are removed (resolved)
//
// When debounce is zero (disabled), all items pass through unchanged.
func FilterDebounced(currentItems []DriftItem, debounceItems map[string]time.Time, debounce time.Duration) []DriftItem {
	// Zero debounce = disabled, pass everything through.
	if debounce == 0 {
		return currentItems
	}

	now := time.Now()

	// Build set of current keys for resolution cleanup.
	currentKeys := make(map[string]bool, len(currentItems))
	for _, item := range currentItems {
		currentKeys[DriftAlertKey(item)] = true
	}

	// Remove resolved items from debounce map (drift cleared before window expired).
	for key := range debounceItems {
		if !currentKeys[key] {
			delete(debounceItems, key)
		}
	}

	// Filter current items through debounce window.
	var graduated []DriftItem
	for _, item := range currentItems {
		key := DriftAlertKey(item)
		firstSeen, exists := debounceItems[key]
		if !exists {
			// New item: start debounce timer.
			debounceItems[key] = now
			continue
		}

		if now.Sub(firstSeen) >= debounce {
			// Debounce window elapsed: graduate to dedup layer.
			delete(debounceItems, key)
			graduated = append(graduated, item)
		}
		// Otherwise: still within window, hold back (filtered out).
	}

	return graduated
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
