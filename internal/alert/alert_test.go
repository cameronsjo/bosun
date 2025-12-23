package alert

import (
	"context"
	"errors"
	"sync"
	"testing"

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
	m := NewManager()
	p := newMockProvider("test", true)
	m.AddProvider(p)

	err := m.SendDeploySuccess(context.Background(), "abc123def456", "unraid")
	require.NoError(t, err)

	alerts := p.getAlerts()
	require.Len(t, alerts, 1)

	alert := alerts[0]
	assert.Equal(t, "Deployment Successful", alert.Title)
	assert.Contains(t, alert.Message, "abc123de") // Short commit.
	assert.Contains(t, alert.Message, "unraid")
	assert.Equal(t, SeverityInfo, alert.Severity)
	assert.Equal(t, "reconcile", alert.Source)
	assert.Equal(t, "abc123def456", alert.Metadata["commit"])
	assert.Equal(t, "unraid", alert.Metadata["target"])
}

func TestManager_SendDeployFailure(t *testing.T) {
	m := NewManager()
	p := newMockProvider("test", true)
	m.AddProvider(p)

	err := m.SendDeployFailure(context.Background(), "abc123def456", "unraid", "connection timeout")
	require.NoError(t, err)

	alerts := p.getAlerts()
	require.Len(t, alerts, 1)

	alert := alerts[0]
	assert.Equal(t, "Deployment Failed", alert.Title)
	assert.Contains(t, alert.Message, "connection timeout")
	assert.Equal(t, SeverityError, alert.Severity)
	assert.Equal(t, "connection timeout", alert.Metadata["error"])
}

func TestManager_SendDeploySuccess_ShortCommit(t *testing.T) {
	m := NewManager()
	p := newMockProvider("test", true)
	m.AddProvider(p)

	// Test with short commit that doesn't need truncation.
	err := m.SendDeploySuccess(context.Background(), "abc", "unraid")
	require.NoError(t, err)

	alerts := p.getAlerts()
	require.Len(t, alerts, 1)
	assert.Contains(t, alerts[0].Message, "abc")
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
	assert.Equal(t, "Rollback Successful", alert.Title)
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
	assert.Equal(t, "CRITICAL: Rollback Failed", alert.Title)
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

func TestSeverityConstants(t *testing.T) {
	assert.Equal(t, Severity("info"), SeverityInfo)
	assert.Equal(t, Severity("warning"), SeverityWarning)
	assert.Equal(t, Severity("error"), SeverityError)
	assert.Equal(t, Severity("critical"), SeverityCritical)
}
