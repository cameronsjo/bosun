package tunnel

import (
	"context"
	"encoding/json"
	"os/exec"
)

// Tailscale implements the Provider interface for Tailscale.
type Tailscale struct {
	// binaryPath is the path to the tailscale binary.
	binaryPath string

	// cachedHostname stores the hostname from the last status check.
	cachedHostname string
}

// tailscaleStatus represents the JSON output of `tailscale status --json`.
type tailscaleStatus struct {
	BackendState   string                     `json:"BackendState"`
	Self           tailscalePeer              `json:"Self"`
	Peer           map[string]tailscalePeer   `json:"Peer"`
	MagicDNSSuffix string                     `json:"MagicDNSSuffix"`
}

// tailscalePeer represents a peer in the Tailscale network.
type tailscalePeer struct {
	DNSName      string   `json:"DNSName"`
	HostName     string   `json:"HostName"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Online       bool     `json:"Online"`
	ExitNode     bool     `json:"ExitNode"`
	Active       bool     `json:"Active"`
}

// NewTailscale creates a new Tailscale provider.
// Returns an error if Tailscale is not installed.
func NewTailscale() (*Tailscale, error) {
	path, err := exec.LookPath("tailscale")
	if err != nil {
		return nil, ErrNotInstalled{Provider: "Tailscale"}
	}

	return &Tailscale{
		binaryPath: path,
	}, nil
}

// NewTailscaleWithPath creates a new Tailscale provider with a custom binary path.
// This is useful for testing or when the binary is not in PATH.
func NewTailscaleWithPath(binaryPath string) *Tailscale {
	return &Tailscale{
		binaryPath: binaryPath,
	}
}

// Name returns the provider name.
func (t *Tailscale) Name() string {
	return string(ProviderTailscale)
}

// Status returns the current Tailscale status.
func (t *Tailscale) Status(ctx context.Context) (*Status, error) {
	cmd := exec.CommandContext(ctx, t.binaryPath, "status", "--json")
	output, err := cmd.Output()
	if err != nil {
		// Check if Tailscale is not connected
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return &Status{
				Connected:    false,
				Provider:     string(ProviderTailscale),
				BackendState: "Unknown",
			}, nil
		}
		return nil, err
	}

	var tsStatus tailscaleStatus
	if err := json.Unmarshal(output, &tsStatus); err != nil {
		return nil, err
	}

	// Convert to generic Status
	status := &Status{
		Connected:    tsStatus.BackendState == "Running",
		Provider:     string(ProviderTailscale),
		BackendState: tsStatus.BackendState,
		TailnetName:  tsStatus.MagicDNSSuffix,
	}

	// Set hostname and IP from Self
	if tsStatus.Self.HostName != "" {
		status.Hostname = tsStatus.Self.HostName
	}
	if tsStatus.Self.DNSName != "" {
		t.cachedHostname = tsStatus.Self.DNSName
	}
	if len(tsStatus.Self.TailscaleIPs) > 0 {
		status.IP = tsStatus.Self.TailscaleIPs[0]
	}

	// Convert peers
	for _, tsPeer := range tsStatus.Peer {
		peer := Peer{
			Name:     tsPeer.HostName,
			DNSName:  tsPeer.DNSName,
			Online:   tsPeer.Online,
			ExitNode: tsPeer.ExitNode,
			Active:   tsPeer.Active,
		}
		if len(tsPeer.TailscaleIPs) > 0 {
			peer.IP = tsPeer.TailscaleIPs[0]
		}
		status.Peers = append(status.Peers, peer)
	}

	return status, nil
}

// IsConnected returns true if Tailscale is connected.
func (t *Tailscale) IsConnected(ctx context.Context) bool {
	status, err := t.Status(ctx)
	if err != nil {
		return false
	}
	return status.Connected
}

// GetHostname returns the Tailscale DNS name.
func (t *Tailscale) GetHostname() string {
	if t.cachedHostname != "" {
		return t.cachedHostname
	}

	// Try to get the hostname from status
	ctx := context.Background()
	status, err := t.Status(ctx)
	if err != nil {
		return ""
	}

	return status.Hostname
}

// GetPlainStatus runs `tailscale status` and returns the plain text output.
// This is useful when JSON parsing fails or for display purposes.
func (t *Tailscale) GetPlainStatus(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, t.binaryPath, "status")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}
