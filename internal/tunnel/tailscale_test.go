package tunnel

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestTailscale_Status_DeterministicResponses(t *testing.T) {
	tests := []struct {
		name          string
		output        string
		stderr        string
		err           error
		wantStatus    *Status
		wantErr       bool
		wantCachedDNS string
	}{
		{
			name: "running status maps self and peers",
			output: `{
				"BackendState":"Running",
				"MagicDNSSuffix":"tail.example.ts.net",
				"Self":{"HostName":"bosun","DNSName":"bosun.tail.example.ts.net.","TailscaleIPs":["100.64.0.1"]},
				"Peer":{
					"node-key:a":{"HostName":"alpha","DNSName":"alpha.tail.example.ts.net.","TailscaleIPs":["100.64.0.2"],"Online":true,"Active":true},
					"node-key:b":{"HostName":"beta","DNSName":"beta.tail.example.ts.net.","TailscaleIPs":[],"ExitNode":true}
				}
			}`,
			stderr: "diagnostic\n",
			wantStatus: &Status{
				Connected:    true,
				Hostname:     "bosun",
				IP:           "100.64.0.1",
				Provider:     "tailscale",
				BackendState: "Running",
				TailnetName:  "tail.example.ts.net",
			},
			wantCachedDNS: "bosun.tail.example.ts.net.",
		},
		{
			name:       "exit one means disconnected",
			err:        stubExitError(1),
			wantStatus: &Status{Provider: "tailscale", BackendState: "Unknown"},
		},
		{
			name:    "other command failure propagates",
			stderr:  "boom",
			err:     errors.New("command failed"),
			wantErr: true,
		},
		{
			name:    "invalid JSON propagates",
			output:  `{not-json}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &stubCommandRunner{outputFn: func(context.Context, string, ...string) ([]byte, string, error) {
				return []byte(tt.output), tt.stderr, tt.err
			}}
			ts := NewTailscaleWithPath("tailscale-test")
			ts.runner = runner

			status, err := ts.Status(context.Background())
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, status)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, status)
			assert.Equal(t, tt.wantStatus.Connected, status.Connected)
			assert.Equal(t, tt.wantStatus.Hostname, status.Hostname)
			assert.Equal(t, tt.wantStatus.IP, status.IP)
			assert.Equal(t, tt.wantStatus.Provider, status.Provider)
			assert.Equal(t, tt.wantStatus.BackendState, status.BackendState)
			assert.Equal(t, tt.wantStatus.TailnetName, status.TailnetName)
			assert.Equal(t, tt.wantCachedDNS, ts.getCachedHostname())
			if tt.wantStatus.Connected {
				require.Len(t, status.Peers, 2)
				peersByName := map[string]Peer{}
				for _, peer := range status.Peers {
					peersByName[peer.Name] = peer
				}
				assert.Equal(t, "100.64.0.2", peersByName["alpha"].IP)
				assert.True(t, peersByName["alpha"].Online)
				assert.True(t, peersByName["alpha"].Active)
				assert.Empty(t, peersByName["beta"].IP)
				assert.True(t, peersByName["beta"].ExitNode)
			}
			assert.Equal(t, []commandCall{{name: "tailscale-test", args: []string{"status", "--json"}}}, runner.recordedCalls())
		})
	}
}

func TestTailscale_GetHostname_ReturnsStableDNSName(t *testing.T) {
	runner := &stubCommandRunner{outputFn: func(context.Context, string, ...string) ([]byte, string, error) {
		return []byte(`{"BackendState":"Running","Self":{"HostName":"bosun","DNSName":"bosun.tail.example.ts.net."}}`), "", nil
	}}
	ts := NewTailscaleWithPath("tailscale-test")
	ts.runner = runner

	assert.Equal(t, "bosun.tail.example.ts.net.", ts.GetHostname())
	assert.Equal(t, "bosun.tail.example.ts.net.", ts.GetHostname())
	assert.Len(t, runner.recordedCalls(), 1, "the second lookup should use the cache")
}

func TestTailscale_GetHostname_FallsBackAndHandlesFailure(t *testing.T) {
	tests := []struct {
		name   string
		output string
		err    error
		want   string
	}{
		{name: "short hostname without DNS name", output: `{"BackendState":"Running","Self":{"HostName":"bosun"}}`, want: "bosun"},
		{name: "status failure", err: errors.New("status failed"), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := NewTailscaleWithPath("tailscale-test")
			ts.runner = &stubCommandRunner{outputFn: func(context.Context, string, ...string) ([]byte, string, error) {
				return []byte(tt.output), "", tt.err
			}}
			assert.Equal(t, tt.want, ts.GetHostname())
		})
	}
}

func TestTailscale_HostnameCache_ConcurrentStatusAndReads(t *testing.T) {
	runner := &stubCommandRunner{outputFn: func(context.Context, string, ...string) ([]byte, string, error) {
		return []byte(`{"BackendState":"Running","Self":{"HostName":"bosun","DNSName":"bosun.tail.example.ts.net."}}`), "", nil
	}}
	ts := NewTailscaleWithPath("tailscale-test")
	ts.runner = runner

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := ts.Status(context.Background())
			assert.NoError(t, err)
		}()
		go func() {
			defer wg.Done()
			_ = ts.GetHostname()
		}()
	}
	wg.Wait()
	assert.Equal(t, "bosun.tail.example.ts.net.", ts.GetHostname())
}

func TestTailscale_GetPlainStatus_DeterministicResponses(t *testing.T) {
	tests := []struct {
		name   string
		output string
		stderr string
		err    error
	}{
		{name: "success", output: "100.64.0.1 bosun\n", stderr: "diagnostic\n"},
		{name: "failure", stderr: "boom", err: errors.New("command failed")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := NewTailscaleWithPath("tailscale-test")
			ts.runner = &stubCommandRunner{outputFn: func(context.Context, string, ...string) ([]byte, string, error) {
				return []byte(tt.output), tt.stderr, tt.err
			}}
			output, err := ts.GetPlainStatus(context.Background())
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
				assert.Empty(t, output)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.output, output)
		})
	}
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
