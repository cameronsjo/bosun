package tunnel

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewTailscale(t *testing.T) {
	ts, err := NewTailscale()
	if err != nil {
		// Expected if tailscale is not installed
		notInstalled, ok := err.(ErrNotInstalled)
		assert.True(t, ok, "expected ErrNotInstalled error")
		assert.Equal(t, "Tailscale", notInstalled.Provider)
		return
	}

	assert.NotNil(t, ts)
	assert.NotEmpty(t, ts.binaryPath)
}

func TestNewTailscaleWithPath(t *testing.T) {
	ts := NewTailscaleWithPath("/custom/path/tailscale")
	assert.NotNil(t, ts)
	assert.Equal(t, "/custom/path/tailscale", ts.binaryPath)
}

func TestTailscale_Name(t *testing.T) {
	ts := NewTailscaleWithPath("/bin/tailscale")
	assert.Equal(t, "tailscale", ts.Name())
}

func TestTailscale_GetHostname_Empty(t *testing.T) {
	ts := NewTailscaleWithPath("/nonexistent/tailscale")
	// Should return empty string when no status has been fetched
	assert.Empty(t, ts.GetHostname())
}

func TestTailscale_Status_InvalidBinary(t *testing.T) {
	ts := NewTailscaleWithPath("/nonexistent/tailscale")
	ctx := context.Background()

	status, err := ts.Status(ctx)
	assert.Error(t, err)
	assert.Nil(t, status)
}

func TestTailscale_IsConnected_InvalidBinary(t *testing.T) {
	ts := NewTailscaleWithPath("/nonexistent/tailscale")
	ctx := context.Background()

	connected := ts.IsConnected(ctx)
	assert.False(t, connected)
}

func TestTailscale_GetPlainStatus_InvalidBinary(t *testing.T) {
	ts := NewTailscaleWithPath("/nonexistent/tailscale")
	ctx := context.Background()

	output, err := ts.GetPlainStatus(ctx)
	assert.Error(t, err)
	assert.Empty(t, output)
}

// Integration tests - only run if tailscale is installed
func TestTailscale_Integration(t *testing.T) {
	ts, err := NewTailscale()
	if err != nil {
		t.Skip("Tailscale not installed, skipping integration tests")
	}

	t.Run("Status", func(t *testing.T) {
		ctx := context.Background()
		status, err := ts.Status(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, status)
		assert.Equal(t, "tailscale", status.Provider)
		// BackendState should be set regardless of connection status
		assert.NotEmpty(t, status.BackendState)
	})

	t.Run("IsConnected", func(t *testing.T) {
		ctx := context.Background()
		// This will return true or false depending on Tailscale state
		_ = ts.IsConnected(ctx)
	})

	t.Run("GetPlainStatus", func(t *testing.T) {
		ctx := context.Background()
		output, err := ts.GetPlainStatus(ctx)
		// May fail if tailscale is not logged in
		if err == nil {
			assert.NotEmpty(t, output)
		}
	})
}
