package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/cameronsjo/bosun/internal/tunnel"
)

func TestRadioCmd_Help(t *testing.T) {
	t.Run("radio --help", func(t *testing.T) {
		output, err := executeCmd(t, "radio", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "connectivity")
		assert.Contains(t, output, "test")
		assert.Contains(t, output, "status")
	})
}

func TestRadioCmd_Aliases(t *testing.T) {
	t.Run("parrot alias", func(t *testing.T) {
		_, err := executeCmd(t, "parrot", "--help")
		assert.NoError(t, err)
	})
}

func TestRadioTestCmd_Help(t *testing.T) {
	t.Run("radio test --help", func(t *testing.T) {
		output, err := executeCmd(t, "radio", "test", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "localhost:8080")
	})
}

func TestRadioStatusCmd_Help(t *testing.T) {
	t.Run("radio status --help", func(t *testing.T) {
		output, err := executeCmd(t, "radio", "status", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "Tailscale")
		assert.Contains(t, output, "tunnel")
	})
}

func TestTailscaleStatus_Structure(t *testing.T) {
	t.Run("TailscaleStatus fields", func(t *testing.T) {
		status := TailscaleStatus{
			BackendState:   "Running",
			MagicDNSSuffix: "tailnet.ts.net",
			Self: TailscalePeer{
				DNSName:      "myhost.tailnet.ts.net",
				HostName:     "myhost",
				TailscaleIPs: []string{"100.100.100.1"},
				Online:       true,
				Active:       true,
			},
			Peer: map[string]TailscalePeer{
				"peer1": {
					DNSName:      "peer1.tailnet.ts.net",
					HostName:     "peer1",
					TailscaleIPs: []string{"100.100.100.2"},
					Online:       true,
				},
			},
		}

		assert.Equal(t, "Running", status.BackendState)
		assert.Equal(t, "tailnet.ts.net", status.MagicDNSSuffix)
		assert.Equal(t, "myhost", status.Self.HostName)
		assert.Len(t, status.Peer, 1)
	})
}

func TestTailscalePeer_Structure(t *testing.T) {
	peer := TailscalePeer{
		DNSName:      "host.tailnet.ts.net",
		HostName:     "host",
		TailscaleIPs: []string{"100.100.100.1", "fd7a:1234::1"},
		Online:       true,
		ExitNode:     false,
		Active:       true,
	}

	assert.Equal(t, "host.tailnet.ts.net", peer.DNSName)
	assert.Equal(t, "host", peer.HostName)
	assert.Len(t, peer.TailscaleIPs, 2)
	assert.True(t, peer.Online)
	assert.False(t, peer.ExitNode)
	assert.True(t, peer.Active)
}

func TestDisplayTailscaleStatus(t *testing.T) {
	t.Run("display running status", func(t *testing.T) {
		status := &TailscaleStatus{
			BackendState:   "Running",
			MagicDNSSuffix: "tailnet.ts.net",
			Self: TailscalePeer{
				HostName:     "myhost",
				TailscaleIPs: []string{"100.100.100.1"},
				DNSName:      "myhost.tailnet.ts.net",
			},
			Peer: map[string]TailscalePeer{
				"peer1": {
					HostName: "peer1",
					Online:   true,
				},
			},
		}

		// This function prints to stdout, so we just verify it doesn't panic
		displayTailscaleStatus(status)
	})

	t.Run("display stopped status", func(t *testing.T) {
		status := &TailscaleStatus{
			BackendState: "Stopped",
		}

		displayTailscaleStatus(status)
	})

	t.Run("display needs login status", func(t *testing.T) {
		status := &TailscaleStatus{
			BackendState: "NeedsLogin",
		}

		displayTailscaleStatus(status)
	})

	t.Run("display with exit node", func(t *testing.T) {
		status := &TailscaleStatus{
			BackendState: "Running",
			Self: TailscalePeer{
				HostName: "myhost",
			},
			Peer: map[string]TailscalePeer{
				"exitnode": {
					HostName: "exitnode",
					Online:   true,
					ExitNode: true,
				},
			},
		}

		displayTailscaleStatus(status)
	})

	t.Run("display many peers", func(t *testing.T) {
		status := &TailscaleStatus{
			BackendState: "Running",
			Self: TailscalePeer{
				HostName: "myhost",
			},
			Peer: make(map[string]TailscalePeer),
		}

		// Add 10 online peers
		for i := 0; i < 10; i++ {
			name := string(rune('a' + i))
			status.Peer[name] = TailscalePeer{
				HostName: "peer" + name,
				Online:   true,
			}
		}

		displayTailscaleStatus(status)
	})
}

func TestRadioTest_MockServer(t *testing.T) {
	t.Run("successful health check", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		// Note: The actual runRadioTest function hardcodes localhost:8080
		// For proper testing, the URL should be configurable or injected.
		// This test demonstrates the mock server pattern.
		resp, err := http.Get(server.URL + "/health")
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		_ = resp.Body.Close()
	})

	t.Run("failed health check", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()

		resp, err := http.Get(server.URL + "/health")
		assert.NoError(t, err)
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
		_ = resp.Body.Close()
	})
}

func TestDisplayInstallInstructions(t *testing.T) {
	t.Run("tailscale", func(t *testing.T) {
		// Verify doesn't panic
		displayInstallInstructions("tailscale")
	})

	t.Run("cloudflare", func(t *testing.T) {
		displayInstallInstructions("cloudflare")
	})

	t.Run("unknown provider", func(t *testing.T) {
		displayInstallInstructions("wireguard")
	})
}

func TestDisplayConnectionState(t *testing.T) {
	t.Run("running", func(t *testing.T) {
		status := &tunnel.Status{Provider: "tailscale", BackendState: "Running"}
		displayConnectionState(status)
	})

	t.Run("stopped", func(t *testing.T) {
		status := &tunnel.Status{Provider: "tailscale", BackendState: "Stopped"}
		displayConnectionState(status)
	})

	t.Run("needs login", func(t *testing.T) {
		status := &tunnel.Status{Provider: "tailscale", BackendState: "NeedsLogin"}
		displayConnectionState(status)
	})

	t.Run("disconnected", func(t *testing.T) {
		status := &tunnel.Status{Provider: "cloudflare", BackendState: "Disconnected"}
		displayConnectionState(status)
	})

	t.Run("unknown state", func(t *testing.T) {
		status := &tunnel.Status{Provider: "tailscale", BackendState: "Unknown"}
		displayConnectionState(status)
	})

	t.Run("default connected", func(t *testing.T) {
		status := &tunnel.Status{Provider: "tailscale", BackendState: "CustomState", Connected: true}
		displayConnectionState(status)
	})

	t.Run("default disconnected", func(t *testing.T) {
		status := &tunnel.Status{Provider: "tailscale", BackendState: "CustomState", Connected: false}
		displayConnectionState(status)
	})
}

func TestDisplayTunnelStatus(t *testing.T) {
	t.Run("full status with peers", func(t *testing.T) {
		status := &tunnel.Status{
			Provider:     "tailscale",
			BackendState: "Running",
			Connected:    true,
			Hostname:     "myhost",
			IP:           "100.100.100.1",
			TailnetName:  "tailnet.ts.net",
			Peers: []tunnel.Peer{
				{Name: "peer1", IP: "100.100.100.2", Online: true},
				{Name: "peer2", IP: "100.100.100.3", Online: false},
			},
		}
		displayTunnelStatus(status)
	})

	t.Run("minimal status", func(t *testing.T) {
		status := &tunnel.Status{
			Provider:     "cloudflare",
			BackendState: "Running",
			Connected:    true,
		}
		displayTunnelStatus(status)
	})
}

func TestDisplayStartInstructions(t *testing.T) {
	t.Run("tailscale", func(t *testing.T) {
		displayStartInstructions("tailscale")
	})

	t.Run("cloudflare", func(t *testing.T) {
		displayStartInstructions("cloudflare")
	})

	t.Run("unknown", func(t *testing.T) {
		displayStartInstructions("wireguard")
	})
}

func TestDisplayLoginInstructions(t *testing.T) {
	t.Run("tailscale", func(t *testing.T) {
		displayLoginInstructions("tailscale")
	})

	t.Run("cloudflare", func(t *testing.T) {
		displayLoginInstructions("cloudflare")
	})

	t.Run("unknown", func(t *testing.T) {
		displayLoginInstructions("wireguard")
	})
}

func TestDisplayPeerInfo(t *testing.T) {
	t.Run("mixed online and offline", func(t *testing.T) {
		peers := []tunnel.Peer{
			{Name: "host-a", IP: "100.100.100.1", Online: true},
			{Name: "host-b", IP: "100.100.100.2", Online: false},
			{Name: "host-c", IP: "100.100.100.3", Online: true, ExitNode: true},
		}
		displayPeerInfo(peers)
	})

	t.Run("all offline", func(t *testing.T) {
		peers := []tunnel.Peer{
			{Name: "host-a", Online: false},
			{Name: "host-b", Online: false},
		}
		displayPeerInfo(peers)
	})

	t.Run("peer without name uses DNSName", func(t *testing.T) {
		peers := []tunnel.Peer{
			{DNSName: "peer.tailnet.ts.net", IP: "100.100.100.1", Online: true},
		}
		displayPeerInfo(peers)
	})
}

func TestCapitalizeFirst(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "lowercase word",
			input: "tailscale",
			want:  "Tailscale",
		},
		{
			name:  "already capitalized",
			input: "Tailscale",
			want:  "Tailscale",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "single lowercase char",
			input: "a",
			want:  "A",
		},
		{
			name:  "single uppercase char",
			input: "A",
			want:  "A",
		},
		{
			name:  "cloudflare",
			input: "cloudflare",
			want:  "Cloudflare",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := capitalizeFirst(tt.input)
			assert.Equal(t, tt.want, got, "capitalizeFirst(%q)", tt.input)
		})
	}
}
