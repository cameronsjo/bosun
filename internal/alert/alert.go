// Package alert provides a native alerting system with multiple provider support.
package alert

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
)

// Severity levels for alerts.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityError    Severity = "error"
	SeverityCritical Severity = "critical"
)

// Alert represents a notification to send.
type Alert struct {
	Title    string            // Short title/subject
	Message  string            // Full message body
	Severity Severity          // Alert severity
	Source   string            // What generated this (e.g., "reconcile", "doctor")
	Metadata map[string]string // Additional context (commit, host, etc.)
}

// Provider interface for alert backends.
type Provider interface {
	Name() string
	Send(ctx context.Context, alert *Alert) error
	IsConfigured() bool
}

// Alerter is an alias for Provider for backward compatibility.
type Alerter = Provider

// Manager handles multiple alert providers.
type Manager struct {
	providers []Provider
}

// NewManager creates a new alert manager.
func NewManager() *Manager {
	return &Manager{providers: make([]Provider, 0)}
}

// AddProvider adds a provider if it is configured.
func (m *Manager) AddProvider(p Provider) {
	if p.IsConfigured() {
		m.providers = append(m.providers, p)
	}
}

// Send sends an alert to all configured providers.
// Returns an aggregated error if any provider fails.
func (m *Manager) Send(ctx context.Context, alert *Alert) error {
	if len(m.providers) == 0 {
		return nil
	}

	start := time.Now()
	log.Debug().
		Str("title", alert.Title).
		Str("severity", string(alert.Severity)).
		Str("source", alert.Source).
		Int("provider_count", len(m.providers)).
		Msg("Sending alert to providers")

	var errs []error
	var successCount int
	for _, p := range m.providers {
		providerStart := time.Now()
		if err := p.Send(ctx, alert); err != nil {
			log.Error().
				Err(err).
				Str("provider", p.Name()).
				Str("title", alert.Title).
				Int64(log.FieldDurationMS, time.Since(providerStart).Milliseconds()).
				Msg("Alert provider failed")
			errs = append(errs, fmt.Errorf("%s: %w", p.Name(), err))
		} else {
			successCount++
			log.Debug().
				Str("provider", p.Name()).
				Int64(log.FieldDurationMS, time.Since(providerStart).Milliseconds()).
				Msg("Alert sent successfully")
		}
	}

	if len(errs) > 0 {
		log.Warn().
			Int("failed", len(errs)).
			Int("succeeded", successCount).
			Int("total", len(m.providers)).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("Alert delivery partially failed")
		return fmt.Errorf("alert errors: %w", errors.Join(errs...))
	}

	log.Info().
		Str("title", alert.Title).
		Str("severity", string(alert.Severity)).
		Int("provider_count", len(m.providers)).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Alert sent to all providers")

	return nil
}

// HasProviders returns true if at least one provider is configured.
func (m *Manager) HasProviders() bool {
	return len(m.providers) > 0
}

// ProviderNames returns the names of all configured providers.
func (m *Manager) ProviderNames() []string {
	names := make([]string, len(m.providers))
	for i, p := range m.providers {
		names[i] = p.Name()
	}
	return names
}

// SendDeploySuccess sends a deployment success notification.
func (m *Manager) SendDeploySuccess(ctx context.Context, commit, target string) error {
	shortCommit := commit
	if len(commit) > 8 {
		shortCommit = commit[:8]
	}

	return m.Send(ctx, &Alert{
		Title:    "Deployment Successful",
		Message:  fmt.Sprintf("Successfully deployed commit %s to %s", shortCommit, target),
		Severity: SeverityInfo,
		Source:   "reconcile",
		Metadata: map[string]string{"commit": commit, "target": target},
	})
}

// SendDeployFailure sends a deployment failure notification.
func (m *Manager) SendDeployFailure(ctx context.Context, commit, target, reason string) error {
	shortCommit := commit
	if len(commit) > 8 {
		shortCommit = commit[:8]
	}

	return m.Send(ctx, &Alert{
		Title:    "Deployment Failed",
		Message:  fmt.Sprintf("Failed to deploy commit %s to %s: %s", shortCommit, target, reason),
		Severity: SeverityError,
		Source:   "reconcile",
		Metadata: map[string]string{"commit": commit, "target": target, "error": reason},
	})
}

// SendRollbackSuccess sends a rollback success notification.
func (m *Manager) SendRollbackSuccess(ctx context.Context, target, backupName string) error {
	return m.Send(ctx, &Alert{
		Title:    "Rollback Successful",
		Message:  fmt.Sprintf("Successfully rolled back %s to backup %s", target, backupName),
		Severity: SeverityWarning,
		Source:   "reconcile",
		Metadata: map[string]string{"target": target, "backup": backupName},
	})
}

// SendRollbackFailure sends a rollback failure notification (critical severity).
func (m *Manager) SendRollbackFailure(ctx context.Context, target, reason string) error {
	return m.Send(ctx, &Alert{
		Title:    "CRITICAL: Rollback Failed",
		Message:  fmt.Sprintf("Failed to rollback %s: %s. Manual intervention required!", target, reason),
		Severity: SeverityCritical,
		Source:   "reconcile",
		Metadata: map[string]string{"target": target, "error": reason},
	})
}

// SendDeployRecovery sends a notification when deployment succeeds after consecutive failures.
func (m *Manager) SendDeployRecovery(ctx context.Context, commit, target string, priorFailures int) error {
	shortCommit := commit
	if len(commit) > 8 {
		shortCommit = commit[:8]
	}

	return m.Send(ctx, &Alert{
		Title:    "Deployment Recovered",
		Message:  fmt.Sprintf("Deployment of commit %s to %s succeeded after %d prior failure(s)", shortCommit, target, priorFailures),
		Severity: SeverityInfo,
		Source:   "reconcile",
		Metadata: map[string]string{
			"commit":         commit,
			"target":         target,
			"prior_failures": fmt.Sprintf("%d", priorFailures),
		},
	})
}

// SendUnhealthyContainers sends a warning alert listing containers found unhealthy post-deploy.
func (m *Manager) SendUnhealthyContainers(ctx context.Context, target string, containers []string) error {
	summary := strings.Join(containers, ", ")
	return m.Send(ctx, &Alert{
		Title:    "Unhealthy Containers Detected",
		Message:  fmt.Sprintf("Post-deploy health check on %s found unhealthy containers: %s", target, summary),
		Severity: SeverityWarning,
		Source:   "reconcile",
		Metadata: map[string]string{
			"target":     target,
			"containers": summary,
			"count":      fmt.Sprintf("%d", len(containers)),
		},
	})
}

// SendDriftDetected sends an alert for detected drift between declared and actual state.
func (m *Manager) SendDriftDetected(ctx context.Context, target string, driftItems []string) error {
	summary := strings.Join(driftItems, ", ")
	return m.Send(ctx, &Alert{
		Title:    "Drift Detected",
		Message:  fmt.Sprintf("Drift detected on %s: %s", target, summary),
		Severity: SeverityWarning,
		Source:   "drift",
		Metadata: map[string]string{"target": target, "drift_count": fmt.Sprintf("%d", len(driftItems))},
	})
}

// SendDoctorAlert sends a health check alert.
func (m *Manager) SendDoctorAlert(ctx context.Context, severity Severity, issues []string) error {
	var title string
	switch severity {
	case SeverityCritical:
		title = "CRITICAL: Health Check Failed"
	case SeverityError:
		title = "Health Check Errors"
	case SeverityWarning:
		title = "Health Check Warnings"
	default:
		title = "Health Check Complete"
	}

	return m.Send(ctx, &Alert{
		Title:    title,
		Message:  strings.Join(issues, "\n"),
		Severity: severity,
		Source:   "doctor",
		Metadata: map[string]string{"issue_count": fmt.Sprintf("%d", len(issues))},
	})
}
