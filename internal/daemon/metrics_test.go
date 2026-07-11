package daemon

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsRegistration(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := newMetrics(reg)

	// Verify all metrics are registered by gathering them.
	families, err := reg.Gather()
	require.NoError(t, err)

	names := make(map[string]bool)
	for _, f := range families {
		names[f.GetName()] = true
	}

	// Gauges always present even at zero.
	assert.True(t, names["bosun_ready"], "bosun_ready should be registered")
	assert.True(t, names["bosun_uptime_seconds"], "bosun_uptime_seconds should be registered")
	assert.True(t, names["bosun_last_reconcile_timestamp"], "bosun_last_reconcile_timestamp should be registered")
	assert.True(t, names["bosun_reconcile_errors_total"], "bosun_reconcile_errors_total should be registered")

	// Ensure metrics struct is populated.
	assert.NotNil(t, m.ReconcileTotal)
	assert.NotNil(t, m.WebhookTriggersTotal)
	assert.NotNil(t, m.DriftChecksTotal)
}

func TestRecordReconcileSuccess(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := newMetrics(reg)

	m.RecordReconcileSuccess(1.5)
	m.RecordReconcileSuccess(2.0)

	count := testutil.ToFloat64(m.ReconcileTotal.WithLabelValues("success"))
	assert.Equal(t, 2.0, count, "success counter should be 2")

	errGauge := testutil.ToFloat64(m.LastReconcileErrors)
	assert.Equal(t, 0.0, errGauge, "errors gauge should be 0 after success")
}

func TestRecordReconcileFailure(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := newMetrics(reg)

	m.RecordReconcileFailure(3.0)

	count := testutil.ToFloat64(m.ReconcileTotal.WithLabelValues("failure"))
	assert.Equal(t, 1.0, count, "failure counter should be 1")

	errGauge := testutil.ToFloat64(m.LastReconcileErrors)
	assert.Equal(t, 1.0, errGauge, "errors gauge should be 1 after failure")
}

func TestRecordReconcileSuccessAfterFailure(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := newMetrics(reg)

	m.RecordReconcileFailure(1.0)
	assert.Equal(t, 1.0, testutil.ToFloat64(m.LastReconcileErrors))

	m.RecordReconcileSuccess(1.0)
	assert.Equal(t, 0.0, testutil.ToFloat64(m.LastReconcileErrors),
		"errors gauge should reset to 0 after success")
}

func TestRecordWebhookTrigger(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := newMetrics(reg)

	m.RecordWebhookTrigger("github")
	m.RecordWebhookTrigger("github")
	m.RecordWebhookTrigger("manual")

	assert.Equal(t, 2.0, testutil.ToFloat64(m.WebhookTriggersTotal.WithLabelValues("github")))
	assert.Equal(t, 1.0, testutil.ToFloat64(m.WebhookTriggersTotal.WithLabelValues("manual")))
}

func TestRecordDriftCheck(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := newMetrics(reg)

	m.RecordDriftCheck(true)
	m.RecordDriftCheck(false)
	m.RecordDriftCheck(true)

	assert.Equal(t, 2.0, testutil.ToFloat64(m.DriftChecksTotal.WithLabelValues("drift")))
	assert.Equal(t, 1.0, testutil.ToFloat64(m.DriftChecksTotal.WithLabelValues("clean")))
}

func TestSetReady(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := newMetrics(reg)

	m.SetReady(true)
	assert.Equal(t, 1.0, testutil.ToFloat64(m.Ready))

	m.SetReady(false)
	assert.Equal(t, 0.0, testutil.ToFloat64(m.Ready))
}

func TestSetUptime(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := newMetrics(reg)

	m.SetUptime(42.5)
	assert.Equal(t, 42.5, testutil.ToFloat64(m.UptimeSeconds))
}

func TestSetLastReconcileTime(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := newMetrics(reg)

	m.SetLastReconcileTime(1710000000.0)
	assert.Equal(t, 1710000000.0, testutil.ToFloat64(m.LastReconcileTime))
}

func TestMetricsEndpointServesPrometheus(t *testing.T) {
	_, s := newTestDaemon(t)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	// Use the full server handler to exercise the /metrics route.
	handler := s.metricsMiddleware(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// Simulate what promhttp.HandlerFor would do by gathering from the registry.
			families, err := s.registry.Gather()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			for _, f := range families {
				_, _ = io.WriteString(w, f.GetName()+"\n")
			}
		}),
	)
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	body := w.Body.String()
	assert.Contains(t, body, "bosun_ready")
	assert.Contains(t, body, "bosun_uptime_seconds")
}

func TestMetricsEndpointRejectsPost(t *testing.T) {
	_, s := newTestDaemon(t)

	req := httptest.NewRequest(http.MethodPost, "/metrics", nil)
	w := httptest.NewRecorder()

	handler := s.metricsMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner handler should not be called for POST")
	}))
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestWebhookTriggersIncrementMetric(t *testing.T) {
	_, s := newUnauthenticatedTestDaemon(t)

	// Fire a generic webhook
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	s.handleWebhook(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)

	count := testutil.ToFloat64(s.metrics.WebhookTriggersTotal.WithLabelValues("generic"))
	assert.Equal(t, 1.0, count, "generic webhook counter should be 1")
}

func TestManualTriggerIncrementsMetric(t *testing.T) {
	_, s := newUnauthenticatedTestDaemon(t)

	req := httptest.NewRequest(http.MethodPost, "/webhook/manual", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	s.handleManualTrigger(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)

	count := testutil.ToFloat64(s.metrics.WebhookTriggersTotal.WithLabelValues("manual"))
	assert.Equal(t, 1.0, count, "manual webhook counter should be 1")
}
