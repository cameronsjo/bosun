package daemon

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds all Prometheus metrics for the bosun daemon.
type Metrics struct {
	// Reconciliation counters
	ReconcileTotal   *prometheus.CounterVec
	ReconcileSeconds *prometheus.HistogramVec

	// Webhook trigger counter
	WebhookTriggersTotal *prometheus.CounterVec

	// Drift check counter
	DriftChecksTotal *prometheus.CounterVec

	// Gauges for current state
	Ready               prometheus.Gauge
	UptimeSeconds       prometheus.Gauge
	LastReconcileTime   prometheus.Gauge
	LastReconcileErrors prometheus.Gauge
}

// newMetrics creates and registers all Prometheus metrics on the given registry.
func newMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		ReconcileTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "bosun",
				Name:      "reconcile_total",
				Help:      "Total number of reconciliation runs by result.",
			},
			[]string{"result"},
		),
		ReconcileSeconds: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "bosun",
				Name:      "reconcile_duration_seconds",
				Help:      "Duration of reconciliation runs in seconds.",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"result"},
		),
		WebhookTriggersTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "bosun",
				Name:      "webhook_triggers_total",
				Help:      "Total number of webhook triggers by source.",
			},
			[]string{"source"},
		),
		DriftChecksTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "bosun",
				Name:      "drift_checks_total",
				Help:      "Total number of drift checks by result.",
			},
			[]string{"result"},
		),
		Ready: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "bosun",
				Name:      "ready",
				Help:      "Whether the daemon is ready (1 = ready, 0 = not ready).",
			},
		),
		UptimeSeconds: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "bosun",
				Name:      "uptime_seconds",
				Help:      "Daemon uptime in seconds.",
			},
		),
		LastReconcileTime: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "bosun",
				Name:      "last_reconcile_timestamp",
				Help:      "Unix timestamp of the last reconciliation.",
			},
		),
		LastReconcileErrors: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "bosun",
				Name:      "reconcile_errors_total",
				Help:      "Whether the last reconciliation had an error (1 = error, 0 = ok).",
			},
		),
	}

	reg.MustRegister(
		m.ReconcileTotal,
		m.ReconcileSeconds,
		m.WebhookTriggersTotal,
		m.DriftChecksTotal,
		m.Ready,
		m.UptimeSeconds,
		m.LastReconcileTime,
		m.LastReconcileErrors,
	)

	return m
}

// RecordReconcileSuccess records a successful reconciliation with its duration.
func (m *Metrics) RecordReconcileSuccess(durationSeconds float64) {
	m.ReconcileTotal.WithLabelValues("success").Inc()
	m.ReconcileSeconds.WithLabelValues("success").Observe(durationSeconds)
	m.LastReconcileErrors.Set(0)
}

// RecordReconcileFailure records a failed reconciliation with its duration.
func (m *Metrics) RecordReconcileFailure(durationSeconds float64) {
	m.ReconcileTotal.WithLabelValues("failure").Inc()
	m.ReconcileSeconds.WithLabelValues("failure").Observe(durationSeconds)
	m.LastReconcileErrors.Set(1)
}

// RecordWebhookTrigger records a webhook trigger by source.
func (m *Metrics) RecordWebhookTrigger(source string) {
	m.WebhookTriggersTotal.WithLabelValues(source).Inc()
}

// RecordDriftCheck records a drift check result.
func (m *Metrics) RecordDriftCheck(hasDrift bool) {
	if hasDrift {
		m.DriftChecksTotal.WithLabelValues("drift").Inc()
	} else {
		m.DriftChecksTotal.WithLabelValues("clean").Inc()
	}
}

// SetReady updates the ready gauge.
func (m *Metrics) SetReady(ready bool) {
	if ready {
		m.Ready.Set(1)
	} else {
		m.Ready.Set(0)
	}
}

// SetUptime updates the uptime gauge.
func (m *Metrics) SetUptime(seconds float64) {
	m.UptimeSeconds.Set(seconds)
}

// SetLastReconcileTime updates the last reconcile timestamp gauge.
func (m *Metrics) SetLastReconcileTime(unixTimestamp float64) {
	m.LastReconcileTime.Set(unixTimestamp)
}
