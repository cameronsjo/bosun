package daemon

import (
	"fmt"
	"time"

	"github.com/cameronsjo/bosun/internal/reconcile"
)

// targetRuntimeStatus records transient diagnostics that cannot be represented
// by a target's deploy-state file. In particular, remote targets are valid
// deployment targets but cannot be inspected through this daemon's local
// Docker socket.
type targetRuntimeStatus struct {
	// projectName is the effective Compose namespace after a reconciler has
	// hot-reloaded bosun.yaml. Keep it private and use it only to seed the next
	// target config; it is not an operator-facing credential or path.
	projectName string
	driftStatus string
	message     string
	checkedAt   time.Time
}

// TargetDriftStatus is the bounded drift summary exposed by detailed operator
// status views. It contains no paths, repository URLs, or raw Docker errors.
type TargetDriftStatus struct {
	Status    string    `json:"status"`
	CheckedAt time.Time `json:"checked_at,omitempty"`
	Items     int       `json:"items,omitempty"`
	Message   string    `json:"message,omitempty"`
}

// TargetHealthStatus is the per-target portion of detailed daemon status.
// Targets are emitted in startup configuration order.
type TargetHealthStatus struct {
	Name               string            `json:"name"`
	Status             string            `json:"status"`
	LastDeployedCommit string            `json:"last_deployed_commit,omitempty"`
	DeployedAt         time.Time         `json:"deployed_at,omitempty"`
	Drift              TargetDriftStatus `json:"drift"`
	CircuitBreaker     SubsystemStatus   `json:"circuit_breaker"`
	Message            string            `json:"message,omitempty"`
}

// targetSnapshot returns the immutable target topology used by daemon
// consumers. Daemons built directly in tests or by embedders pre-dating the
// startup snapshot fall back to resolving the current config once on the
// first consumer call.
func (d *Daemon) targetSnapshot() ([]reconcile.Target, error) {
	d.targetMu.RLock()
	if d.targetsResolved || len(d.targets) > 0 || d.targetResolveErr != nil {
		targets := append([]reconcile.Target(nil), d.targets...)
		err := d.targetResolveErr
		d.targetMu.RUnlock()
		return targets, err
	}
	d.targetMu.RUnlock()

	d.targetMu.Lock()
	defer d.targetMu.Unlock()
	if !d.targetsResolved && len(d.targets) == 0 && d.targetResolveErr == nil {
		var (
			targets []reconcile.Target
			err     error
		)
		if d.config == nil || d.config.ReconcileConfig == nil {
			targets = []reconcile.Target{{Name: reconcile.DefaultTargetName}}
		} else {
			targets, err = d.config.ReconcileConfig.ResolveTargets()
		}
		d.targets = append([]reconcile.Target(nil), targets...)
		d.targetResolveErr = err
		d.targetsResolved = true
	}
	return append([]reconcile.Target(nil), d.targets...), d.targetResolveErr
}

func (d *Daemon) recordTargetRuntime(name, driftStatus, message string, checkedAt time.Time) {
	d.targetRuntimeMu.Lock()
	if d.targetRuntime == nil {
		d.targetRuntime = make(map[string]targetRuntimeStatus)
	}
	status := d.targetRuntime[name]
	status.driftStatus = driftStatus
	status.message = message
	// An unavailable/error result is not a successful Docker sample. Clearing
	// the transient timestamp lets targetHealthStatuses retain the last
	// successful timestamp from the persisted state instead of reporting the
	// failure time as a fresh drift check.
	if driftStatus == StatusUnavailable {
		status.checkedAt = time.Time{}
	} else {
		status.checkedAt = checkedAt
	}
	d.targetRuntime[name] = status
	d.targetRuntimeMu.Unlock()
}

// recordTargetProjectName remembers the effective Compose namespace selected
// by a reconciler after its repo config reload. Runtime diagnostics are merged
// so recording a project name cannot erase a newer drift status (or vice versa).
func (d *Daemon) recordTargetProjectName(name, projectName string) {
	if projectName == "" {
		return
	}
	d.targetRuntimeMu.Lock()
	if d.targetRuntime == nil {
		d.targetRuntime = make(map[string]targetRuntimeStatus)
	}
	status := d.targetRuntime[name]
	status.projectName = projectName
	d.targetRuntime[name] = status
	d.targetRuntimeMu.Unlock()
}

// configForTarget returns the target-scoped config used by every daemon
// consumer. A successful reconciler may hot-reload the effective project name
// from repo config, so seed future reconcile, drift, and status reads from the
// private runtime snapshot rather than reverting to the startup value.
func (d *Daemon) configForTarget(target reconcile.Target) *reconcile.Config {
	if d.config == nil || d.config.ReconcileConfig == nil {
		return nil
	}
	targetConfig := d.config.ReconcileConfig.ConfigForTarget(target)
	if runtimeStatus, ok := d.targetRuntimeStatus(target.Name); ok && runtimeStatus.projectName != "" {
		targetConfig.ProjectName = runtimeStatus.projectName
	}
	return targetConfig
}

func (d *Daemon) targetRuntimeStatus(name string) (targetRuntimeStatus, bool) {
	d.targetRuntimeMu.RLock()
	status, ok := d.targetRuntime[name]
	d.targetRuntimeMu.RUnlock()
	return status, ok
}

// targetContext returns the alert-context sentinel for a configured target.
// The compatibility default intentionally returns empty so existing
// single-target alert titles remain unchanged; named targets are attributable.
func targetContext(target reconcile.Target) string {
	if target.IsDefault() {
		return ""
	}
	return target.Name
}

// targetHealthStatuses reads each target's independent state file and combines
// it with the daemon's transient drift capability status. It never opens
// Docker, which keeps remote-target status inspection local-state only.
func (d *Daemon) targetHealthStatuses(targets []reconcile.Target) []TargetHealthStatus {
	if len(targets) == 0 || d.config == nil || d.config.ReconcileConfig == nil {
		return nil
	}

	statuses := make([]TargetHealthStatus, 0, len(targets))
	for _, target := range targets {
		cfg := d.configForTarget(target)
		if cfg == nil {
			continue
		}
		state := &reconcile.DeployState{}
		if cfg.StateFile != "" {
			state = reconcile.LoadState(cfg.StateFile)
		}

		runtimeStatus, hasRuntime := d.targetRuntimeStatus(target.Name)
		drift := TargetDriftStatus{
			Status: computeDriftStatus(len(state.DriftItems), state.LastDeployedCommit),
		}
		if !state.DriftCheckedAt.IsZero() {
			drift.CheckedAt = state.DriftCheckedAt
		}
		if len(state.DriftItems) > 0 {
			drift.Items = len(state.DriftItems)
		}

		if target.TargetHost != "" {
			// A remote target's state remains useful for cached display, but
			// local periodic drift cannot assert that it is current.
			drift.Status = "unavailable"
			drift.Message = "remote target cannot be inspected by local Docker"
		}
		if hasRuntime {
			if runtimeStatus.driftStatus != "" {
				drift.Status = runtimeStatus.driftStatus
			}
			if !runtimeStatus.checkedAt.IsZero() {
				drift.CheckedAt = runtimeStatus.checkedAt
			}
			if runtimeStatus.message != "" {
				drift.Message = runtimeStatus.message
			}
		}

		cb := SubsystemStatus{Status: StatusClosed, Failures: state.AttemptCount}
		if state.AttemptCount >= reconcile.MaxAttempts &&
			(state.LastAttemptedCommit == "" || state.LastAttemptedCommit != state.LastDeployedCommit) {
			cb.Status = StatusOpen
		}

		status := StatusHealthy
		var message string
		switch {
		case drift.Status == "unavailable":
			status = StatusUnavailable
			message = drift.Message
		case drift.Status == "drifted":
			status = StatusDegraded
		case cb.Status == StatusOpen:
			status = StatusDegraded
			message = "circuit breaker is open"
		}

		statuses = append(statuses, TargetHealthStatus{
			Name:               target.Name,
			Status:             status,
			LastDeployedCommit: state.LastDeployedCommit,
			DeployedAt:         state.DeployedAt,
			Drift:              drift,
			CircuitBreaker:     cb,
			Message:            message,
		})
	}
	return statuses
}

// detailedTargetStatuses returns the target collection used by operator
// status endpoints without performing a live Docker check. The collection is
// omitted for the implicit or lone default to preserve the legacy response
// shape.
func (d *Daemon) detailedTargetStatuses() []TargetHealthStatus {
	targets, err := d.targetSnapshot()
	if err != nil || len(targets) == 0 {
		return nil
	}
	if len(targets) == 1 && targets[0].IsDefault() {
		return nil
	}
	return d.targetHealthStatuses(targets)
}

func aggregateTargetCircuitBreaker(statuses []TargetHealthStatus) SubsystemStatus {
	aggregate := SubsystemStatus{Status: StatusClosed}
	for _, status := range statuses {
		if status.CircuitBreaker.Failures > aggregate.Failures {
			aggregate.Failures = status.CircuitBreaker.Failures
		}
		if status.CircuitBreaker.Status == StatusOpen {
			aggregate.Status = StatusOpen
		}
	}
	return aggregate
}

func aggregateTargetStatus(statuses []TargetHealthStatus) SubsystemStatus {
	for _, status := range statuses {
		if status.Status == StatusUnhealthy {
			return SubsystemStatus{Status: StatusUnhealthy, Message: fmt.Sprintf("target %q is unhealthy", status.Name)}
		}
	}
	for _, status := range statuses {
		if status.Status == StatusUnavailable {
			return SubsystemStatus{Status: StatusDegraded, Message: fmt.Sprintf("target %q is unavailable", status.Name)}
		}
		if status.Status == StatusDegraded {
			return SubsystemStatus{Status: StatusDegraded, Message: fmt.Sprintf("target %q is degraded", status.Name)}
		}
	}
	return SubsystemStatus{Status: StatusHealthy}
}
