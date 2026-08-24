package alert

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider is a test provider that tracks sent alerts.
type mockProvider struct {
	name       string
	configured bool
	shouldFail bool
	mu         sync.Mutex
	alerts     []*Alert
}

func newMockProvider(name string, configured bool) *mockProvider {
	return &mockProvider{
		name:       name,
		configured: configured,
		alerts:     make([]*Alert, 0),
	}
}

func (m *mockProvider) Name() string {
	return m.name
}

func (m *mockProvider) IsConfigured() bool {
	return m.configured
}

func (m *mockProvider) Send(_ context.Context, alert *Alert) error {
	if m.shouldFail {
		return errors.New("mock send failed")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.alerts = append(m.alerts, alert)
	return nil
}

func (m *mockProvider) getAlerts() []*Alert {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*Alert{}, m.alerts...)
}

func TestManager_AddProvider(t *testing.T) {
	t.Run("adds configured provider", func(t *testing.T) {
		m := NewManager()
		p := newMockProvider("test", true)

		m.AddProvider(p)

		assert.True(t, m.HasProviders())
		assert.Equal(t, []string{"test"}, m.ProviderNames())
	})

	t.Run("ignores unconfigured provider", func(t *testing.T) {
		m := NewManager()
		p := newMockProvider("test", false)

		m.AddProvider(p)

		assert.False(t, m.HasProviders())
		assert.Empty(t, m.ProviderNames())
	})
}

func TestManager_Send(t *testing.T) {
	t.Run("sends to all providers", func(t *testing.T) {
		m := NewManager()
		p1 := newMockProvider("provider1", true)
		p2 := newMockProvider("provider2", true)

		m.AddProvider(p1)
		m.AddProvider(p2)

		alert := &Alert{
			Title:    "Test Alert",
			Message:  "This is a test",
			Severity: SeverityInfo,
			Source:   "test",
		}

		err := m.Send(context.Background(), alert)
		require.NoError(t, err)

		assert.Len(t, p1.getAlerts(), 1)
		assert.Len(t, p2.getAlerts(), 1)
		assert.Equal(t, "Test Alert", p1.getAlerts()[0].Title)
	})

	t.Run("returns nil with no providers", func(t *testing.T) {
		m := NewManager()

		err := m.Send(context.Background(), &Alert{Title: "Test"})
		assert.NoError(t, err)
	})

	t.Run("aggregates errors from multiple providers", func(t *testing.T) {
		m := NewManager()
		p1 := newMockProvider("provider1", true)
		p1.shouldFail = true
		p2 := newMockProvider("provider2", true)
		p2.shouldFail = true

		m.AddProvider(p1)
		m.AddProvider(p2)

		err := m.Send(context.Background(), &Alert{Title: "Test"})
		require.Error(t, err)

		// Both provider names should appear in the error.
		assert.Contains(t, err.Error(), "provider1")
		assert.Contains(t, err.Error(), "provider2")
	})

	t.Run("continues sending even if one provider fails", func(t *testing.T) {
		m := NewManager()
		p1 := newMockProvider("failing", true)
		p1.shouldFail = true
		p2 := newMockProvider("working", true)

		m.AddProvider(p1)
		m.AddProvider(p2)

		err := m.Send(context.Background(), &Alert{Title: "Test"})
		require.Error(t, err)

		// Working provider should still receive the alert.
		assert.Len(t, p2.getAlerts(), 1)
	})
}

func TestManager_SendDeploySuccess(t *testing.T) {
	t.Run("basic with services and duration", func(t *testing.T) {
		m := NewManager()
		p := newMockProvider("test", true)
		m.AddProvider(p)

		services := []string{"traefik", "authelia", "portainer"}
		duration := 45 * time.Second

		err := m.SendDeploySuccess(context.Background(), "abc123def456", "unraid", services, duration)
		require.NoError(t, err)

		alerts := p.getAlerts()
		require.Len(t, alerts, 1)

		a := alerts[0]
		assert.Equal(t, "Deployment Successful [unraid]", a.Title)
		assert.Contains(t, a.Message, "abc123de") // Short commit.
		assert.Contains(t, a.Message, "unraid")
		assert.Contains(t, a.Message, "45s")
		assert.Contains(t, a.Message, "traefik, authelia, portainer")
		assert.Equal(t, SeverityInfo, a.Severity)
		assert.Equal(t, "reconcile", a.Source)
		assert.Equal(t, "abc123def456", a.Metadata["commit"])
		assert.Equal(t, "unraid", a.Metadata["target"])
		assert.Equal(t, "45s", a.Metadata["duration"])
		assert.Equal(t, "traefik, authelia, portainer", a.Metadata["services"])
		assert.Equal(t, "3", a.Metadata["service_count"])
	})

	t.Run("no services omits service metadata", func(t *testing.T) {
		m := NewManager()
		p := newMockProvider("test", true)
		m.AddProvider(p)

		err := m.SendDeploySuccess(context.Background(), "abc123def456", "unraid", nil, 10*time.Second)
		require.NoError(t, err)

		alerts := p.getAlerts()
		require.Len(t, alerts, 1)

		a := alerts[0]
		assert.NotContains(t, a.Message, "Services:")
		assert.NotContains(t, a.Metadata, "services")
		assert.NotContains(t, a.Metadata, "service_count")
	})

	t.Run("short commit not truncated", func(t *testing.T) {
		m := NewManager()
		p := newMockProvider("test", true)
		m.AddProvider(p)

		err := m.SendDeploySuccess(context.Background(), "abc", "unraid", nil, 5*time.Second)
		require.NoError(t, err)

		alerts := p.getAlerts()
		require.Len(t, alerts, 1)
		assert.Contains(t, alerts[0].Message, "abc")
	})
}

func TestManager_SendDeployFailure(t *testing.T) {
	t.Run("basic with services and duration", func(t *testing.T) {
		m := NewManager()
		p := newMockProvider("test", true)
		m.AddProvider(p)

		services := []string{"traefik", "authelia"}
		duration := 2 * time.Minute

		err := m.SendDeployFailure(context.Background(), "abc123def456", "unraid", "connection timeout", services, duration)
		require.NoError(t, err)

		alerts := p.getAlerts()
		require.Len(t, alerts, 1)

		a := alerts[0]
		assert.Equal(t, "Deployment Failed [unraid]", a.Title)
		assert.Contains(t, a.Message, "connection timeout")
		assert.Contains(t, a.Message, "2m0s")
		assert.Contains(t, a.Message, "traefik, authelia")
		assert.Equal(t, SeverityError, a.Severity)
		assert.Equal(t, "connection timeout", a.Metadata["error"])
		assert.Equal(t, "2m0s", a.Metadata["duration"])
		assert.Equal(t, "traefik, authelia", a.Metadata["services"])
		assert.Equal(t, "2", a.Metadata["service_count"])
	})

	t.Run("no services omits service metadata", func(t *testing.T) {
		m := NewManager()
		p := newMockProvider("test", true)
		m.AddProvider(p)

		err := m.SendDeployFailure(context.Background(), "abc123def456", "unraid", "timeout", nil, 30*time.Second)
		require.NoError(t, err)

		alerts := p.getAlerts()
		require.Len(t, alerts, 1)

		a := alerts[0]
		assert.NotContains(t, a.Message, "Services:")
		assert.NotContains(t, a.Metadata, "services")
		assert.NotContains(t, a.Metadata, "service_count")
	})
}

func TestManager_SendDeployRecovery(t *testing.T) {
	m := NewManager()
	p := newMockProvider("test", true)
	m.AddProvider(p)

	err := m.SendDeployRecovery(context.Background(), "abc123def456", "unraid", 5)
	require.NoError(t, err)

	alerts := p.getAlerts()
	require.Len(t, alerts, 1)

	alert := alerts[0]
	assert.Equal(t, "Deployment Recovered [unraid]", alert.Title)
	assert.Contains(t, alert.Message, "abc123de")
	assert.Contains(t, alert.Message, "5 prior failure(s)")
	assert.Equal(t, SeverityInfo, alert.Severity)
	assert.Equal(t, "reconcile", alert.Source)
	assert.Equal(t, "5", alert.Metadata["prior_failures"])
}

func TestManager_SendRollbackSuccess(t *testing.T) {
	m := NewManager()
	p := newMockProvider("test", true)
	m.AddProvider(p)

	err := m.SendRollbackSuccess(context.Background(), "unraid", "backup-2024-01-01")
	require.NoError(t, err)

	alerts := p.getAlerts()
	require.Len(t, alerts, 1)

	alert := alerts[0]
	assert.Equal(t, "Rollback Successful [unraid]", alert.Title)
	assert.Equal(t, SeverityWarning, alert.Severity)
	assert.Equal(t, "backup-2024-01-01", alert.Metadata["backup"])
}

func TestManager_SendRollbackFailure(t *testing.T) {
	m := NewManager()
	p := newMockProvider("test", true)
	m.AddProvider(p)

	err := m.SendRollbackFailure(context.Background(), "unraid", "backup corrupted")
	require.NoError(t, err)

	alerts := p.getAlerts()
	require.Len(t, alerts, 1)

	alert := alerts[0]
	assert.Equal(t, "CRITICAL: Rollback Failed [unraid]", alert.Title)
	assert.Equal(t, SeverityCritical, alert.Severity)
	assert.Contains(t, alert.Message, "Manual intervention required")
}

func TestManager_SendDoctorAlert(t *testing.T) {
	tests := []struct {
		name          string
		severity      Severity
		expectedTitle string
	}{
		{"critical", SeverityCritical, "CRITICAL: Health Check Failed"},
		{"error", SeverityError, "Health Check Errors"},
		{"warning", SeverityWarning, "Health Check Warnings"},
		{"info", SeverityInfo, "Health Check Complete"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := NewManager()
			p := newMockProvider("test", true)
			m.AddProvider(p)

			issues := []string{"Issue 1", "Issue 2"}
			err := m.SendDoctorAlert(context.Background(), tc.severity, issues)
			require.NoError(t, err)

			alerts := p.getAlerts()
			require.Len(t, alerts, 1)
			assert.Equal(t, tc.expectedTitle, alerts[0].Title)
			assert.Equal(t, "doctor", alerts[0].Source)
			assert.Equal(t, "2", alerts[0].Metadata["issue_count"])
		})
	}
}

func TestManager_SendDriftResolved(t *testing.T) {
	m := NewManager()
	p := newMockProvider("test", true)
	m.AddProvider(p)

	resolved := []string{"traefik:unhealthy", "authelia:missing"}
	err := m.SendDriftResolved(context.Background(), "unraid", resolved)
	require.NoError(t, err)

	alerts := p.getAlerts()
	require.Len(t, alerts, 1)

	a := alerts[0]
	assert.Equal(t, "Drift Resolved [unraid]", a.Title)
	assert.Contains(t, a.Message, "unraid")
	assert.Contains(t, a.Message, "traefik:unhealthy")
	assert.Equal(t, SeverityInfo, a.Severity)
	assert.Equal(t, "drift", a.Source)
	assert.Equal(t, "2", a.Metadata["resolved_count"])
}

func TestManager_SendDriftSelfHealExhaustedIsBoundedAndOpaque(t *testing.T) {
	m := NewManager()
	p := newMockProvider("test", true)
	m.AddProvider(p)

	err := m.SendDriftSelfHealExhausted(context.Background(), "unraid", "0123456789ab", 7, 3)
	require.NoError(t, err)

	alerts := p.getAlerts()
	require.Len(t, alerts, 1)
	a := alerts[0]
	assert.Equal(t, "Drift Self-Heal Exhausted [unraid]", a.Title)
	assert.Equal(t, SeverityError, a.Severity)
	assert.Equal(t, "self-heal-exhausted", a.Source)
	assert.Equal(t, "0123456789ab", a.Metadata["signature_id"])
	assert.Equal(t, "7", a.Metadata["drift_count"])
	assert.Equal(t, "3", a.Metadata["attempts"])
	assert.NotContains(t, a.Message, "service")
	assert.NotContains(t, a.Metadata, "services")
}

func TestManager_SendUnhealthyContainers(t *testing.T) {
	m := NewManager()
	p := newMockProvider("test", true)
	m.AddProvider(p)

	err := m.SendUnhealthyContainers(context.Background(), "unraid", []string{"traefik", "authelia"})
	require.NoError(t, err)

	alerts := p.getAlerts()
	require.Len(t, alerts, 1)

	a := alerts[0]
	assert.Equal(t, "Unhealthy Containers Detected [unraid]", a.Title)
	assert.Contains(t, a.Message, "traefik, authelia")
	assert.Contains(t, a.Message, "unraid")
	assert.Equal(t, SeverityWarning, a.Severity)
	assert.Equal(t, "reconcile", a.Source)
	assert.Equal(t, "2", a.Metadata["count"])
	assert.Equal(t, "traefik, authelia", a.Metadata["containers"])
	assert.Equal(t, "unraid", a.Metadata["target"])
}

func TestManager_SendDriftDetected(t *testing.T) {
	m := NewManager()
	p := newMockProvider("test", true)
	m.AddProvider(p)

	err := m.SendDriftDetected(context.Background(), "unraid", []string{"traefik:unhealthy", "authelia:missing"})
	require.NoError(t, err)

	alerts := p.getAlerts()
	require.Len(t, alerts, 1)

	a := alerts[0]
	assert.Equal(t, "Drift Detected [unraid]", a.Title)
	assert.Contains(t, a.Message, "traefik:unhealthy, authelia:missing")
	assert.Contains(t, a.Message, "unraid")
	assert.Equal(t, SeverityWarning, a.Severity)
	assert.Equal(t, "drift", a.Source)
	assert.Equal(t, "2", a.Metadata["drift_count"])
	assert.Equal(t, "unraid", a.Metadata["target"])
}

func TestSeverityConstants(t *testing.T) {
	assert.Equal(t, Severity("info"), SeverityInfo)
	assert.Equal(t, Severity("warning"), SeverityWarning)
	assert.Equal(t, Severity("error"), SeverityError)
	assert.Equal(t, Severity("critical"), SeverityCritical)
}

func TestTargetTitle(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		target   string
		expected string
	}{
		{"named target appends suffix", "Deployment Successful", "unraid", "Deployment Successful [unraid]"},
		{"local target omits suffix", "Deployment Failed", "local", "Deployment Failed"},
		{"empty target omits suffix", "Drift Detected", "", "Drift Detected"},
		{"critical with suffix", "CRITICAL: Rollback Failed", "pi", "CRITICAL: Rollback Failed [pi]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, targetTitle(tt.title, tt.target))
		})
	}
}

func TestManager_SendDeploySuccess_LocalTarget(t *testing.T) {
	p := newMockProvider("test", true)
	mgr := NewManager()
	mgr.AddProvider(p)

	err := mgr.SendDeploySuccess(context.Background(), "abc123", "local", []string{"traefik"}, 30*time.Second)
	require.NoError(t, err)

	alerts := p.getAlerts()
	require.Len(t, alerts, 1)
	assert.Equal(t, "Deployment Successful", alerts[0].Title, "local target should not have suffix")
}
