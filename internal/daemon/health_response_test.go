package daemon

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireBoundedHealthResponse(t *testing.T, body []byte) HealthResponse {
	t.Helper()

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &fields))
	assert.ElementsMatch(t, []string{"status", "ready", "uptime"}, mapKeys(fields))

	var response HealthResponse
	require.NoError(t, json.Unmarshal(body, &response))
	return response
}

func mapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func TestPublicHealthResponse_BoundsDetailedStatus(t *testing.T) {
	const sensitive = "https://secret.example/repo /srv/private/compose.yaml"
	status := HealthStatus{
		Status:        StatusDegraded,
		Ready:         true,
		LastReconcile: time.Unix(1, 0),
		LastError:     sensitive,
		Uptime:        3 * time.Minute,
		Subsystems: map[string]SubsystemStatus{
			"circuit_breaker": {
				Status:   StatusOpen,
				Message:  sensitive,
				Failures: 9,
			},
		},
	}

	body, err := json.Marshal(publicHealthResponse(status))
	require.NoError(t, err)
	response := requireBoundedHealthResponse(t, body)

	assert.Equal(t, StatusDegraded, response.Status)
	assert.True(t, response.Ready)
	assert.Equal(t, 3*time.Minute, response.Uptime)
	assert.NotContains(t, string(body), sensitive)
	assert.NotContains(t, string(body), "last_error")
	assert.NotContains(t, string(body), "last_reconcile")
	assert.NotContains(t, string(body), "subsystems")
	assert.NotContains(t, string(body), "circuit_breaker")
}
