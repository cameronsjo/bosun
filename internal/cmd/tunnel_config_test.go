package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/config"
)

// installStubCloudflared puts an executable cloudflared on a PATH that holds
// nothing else. Without pgrep on PATH the process fallback of the Cloudflare
// provider cannot report a tunnel as connected, so a pass can only come from
// the health endpoint in bosun.yaml.
func installStubCloudflared(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub cloudflared is a POSIX shell script")
	}

	dir := t.TempDir()
	stub := filepath.Join(dir, "cloudflared")
	require.NoError(t, os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	t.Setenv("PATH", dir)
}

// loadCloudflareTunnelProject writes a project whose bosun.yaml names the
// Cloudflare provider and a health endpoint, switches into it, and loads it.
func loadCloudflareTunnelProject(t *testing.T, healthEndpoint string) *config.Config {
	t.Helper()

	projectDir := evalSymlinks(t, t.TempDir())
	require.NoError(t, os.Mkdir(filepath.Join(projectDir, "manifest"), 0o755))
	body := "tunnel:\n" +
		"  provider: cloudflare\n" +
		"  hostname: tunnel.example.com\n" +
		"  health_endpoint: " + healthEndpoint + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "bosun.yaml"), []byte(body), 0o644))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(projectDir))

	cfg, err := config.Load()
	require.NoError(t, err)
	return cfg
}

// TestCheckTunnel_UsesCloudflareConfigFromProject pins that `bosun doctor`
// hands the tunnel settings from bosun.yaml to the Cloudflare provider. Before
// the fix the provider was built with an empty configuration, so the health
// endpoint was never consulted and the check fell through to a process lookup.
func TestCheckTunnel_UsesCloudflareConfigFromProject(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantPassed int
		wantWarned int
	}{
		{name: "healthy endpoint passes", statusCode: http.StatusOK, wantPassed: 1, wantWarned: 0},
		{name: "unhealthy endpoint warns", statusCode: http.StatusServiceUnavailable, wantPassed: 0, wantWarned: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hits.Add(1)
				w.WriteHeader(tt.statusCode)
			}))
			t.Cleanup(srv.Close)

			installStubCloudflared(t)
			cfg := loadCloudflareTunnelProject(t, srv.URL+"/health")

			result := checkTunnel(context.Background(), cfg)

			assert.Equal(t, tt.wantPassed, result.Passed)
			assert.Equal(t, tt.wantWarned, result.Warned)
			assert.Equal(t, 0, result.Failed)
			assert.Positive(t, hits.Load(), "the health endpoint from bosun.yaml was never requested")
		})
	}
}
