package reconcile

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestResolvedDriftAlertKeys(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		active  []DriftItem
		alerted map[string]time.Time
		want    []string
	}{
		{
			name:    "empty alerted map resolves nothing",
			active:  []DriftItem{{Service: "api", Type: DriftMissing}},
			alerted: nil,
			want:    nil,
		},
		{
			name:    "service still drifting is not resolved",
			active:  []DriftItem{{Service: "api", Type: DriftMissing}},
			alerted: map[string]time.Time{"api:missing": now},
			want:    nil,
		},
		{
			name:    "service gone from active drift is resolved",
			active:  nil,
			alerted: map[string]time.Time{"api:missing": now},
			want:    []string{"api:missing"},
		},
		{
			name:    "type transition on the same service is not a resolution",
			active:  []DriftItem{{Service: "api", Type: DriftUnhealthy}},
			alerted: map[string]time.Time{"api:missing": now},
			want:    nil,
		},
		{
			name:   "only resolved services are returned, sorted",
			active: []DriftItem{{Service: "api", Type: DriftMissing}},
			alerted: map[string]time.Time{
				"web:missing":    now,
				"api:missing":    now,
				"cache:missing":  now,
				"worker:missing": now,
			},
			want: []string{"cache:missing", "web:missing", "worker:missing"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ResolvedDriftAlertKeys(tt.active, tt.alerted))
		})
	}
}

// TestShouldAlertDrift_ResolvedKeysMatchHelper keeps the two definitions of
// "resolved" from drifting apart.
func TestShouldAlertDrift_ResolvedKeysMatchHelper(t *testing.T) {
	now := time.Now()
	active := []DriftItem{{Service: "api", Type: DriftMissing}}
	alerted := map[string]time.Time{"api:missing": now, "web:missing": now, "db:unhealthy": now}

	_, resolved := ShouldAlertDrift(active, active, alerted, time.Hour)

	assert.ElementsMatch(t, ResolvedDriftAlertKeys(active, alerted), resolved)
	assert.ElementsMatch(t, []string{"web:missing", "db:unhealthy"}, resolved)
}
