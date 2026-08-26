package tunnel

import (
	"context"
	"encoding/json"
	"os/exec"
	"sync"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
)

// Tailscale implements the Provider interface for Tailscale.
type Tailscale struct {
	// binaryPath is the path to the tailscale binary.
	binaryPath string

	// runner executes tailscale commands.
	runner commandRunner

	// cachedHostname stores the hostname from the last status check. It is
	// guarded because status checks and UI reads may run concurrently.
	hostnameMu     sync.RWMutex
	cachedHostname string
}

// tailscaleStatus represents the JSON output of `tailscale status --json`.
type tailscaleStatus struct {
	BackendState   string                   `json:"BackendState"`
	Self           tailscalePeer            `json:"Self"`
	Peer           map[string]tailscalePeer `json:"Peer"`
	MagicDNSSuffix string                   `json:"MagicDNSSuffix"`
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
	logger := log.Component(log.ComponentTunnel)
	logger.Debug().Msg("Preparing to initialize Tailscale provider")

	path, err := exec.LookPath("tailscale")
	if err != nil {
		logger.Warn().
			Err(err).
			Msg("Tailscale binary not found in PATH")
		return nil, ErrNotInstalled{Provider: "Tailscale"}
	}

	logger.Debug().
		Str("path", path).
		Msg("Successfully found Tailscale binary")

	return &Tailscale{
		binaryPath: path,
		runner:     execCommandRunner{},
	}, nil
}

// NewTailscaleWithPath creates a new Tailscale provider with a custom binary path.
// This is useful for testing or when the binary is not in PATH.
func NewTailscaleWithPath(binaryPath string) *Tailscale {
	return &Tailscale{
		binaryPath: binaryPath,
		runner:     execCommandRunner{},
	}
}

func (t *Tailscale) commandRunner() commandRunner {
	if t.runner != nil {
		return t.runner
	}
	return execCommandRunner{}
}

func (t *Tailscale) getCachedHostname() string {
	t.hostnameMu.RLock()
	defer t.hostnameMu.RUnlock()
	return t.cachedHostname
}

func (t *Tailscale) setCachedHostname(hostname string) {
	t.hostnameMu.Lock()
	t.cachedHostname = hostname
	t.hostnameMu.Unlock()
}

// Name returns the provider name.
func (t *Tailscale) Name() string {
	return string(ProviderTailscale)
}

// Status returns the current Tailscale status.
func (t *Tailscale) Status(ctx context.Context) (*Status, error) {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentTunnel)

	logger.Debug().Str(log.FieldOperation, "status").Msg("Executing tailscale status")

	output, stderr, err := t.commandRunner().Output(ctx, t.binaryPath, "status", "--json")
	if err != nil {
		// Check if Tailscale is not connected
		if exitErr, ok := err.(interface{ ExitCode() int }); ok && exitErr.ExitCode() == 1 {
			logger.Debug().
				Str(log.FieldOperation, "status").
				Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
				Msg("Tailscale status check: not connected (exit code 1)")
			return &Status{
				Connected:    false,
				Provider:     string(ProviderTailscale),
				BackendState: "Unknown",
			}, nil
		}
		logger.Error().
			Err(err).
			Str(log.FieldOperation, "status").
			Str("stderr", stderr).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("Failed to execute tailscale status command")
		return nil, err
	}

	if stderr != "" {
		logger.Debug().
			Str("stderr", stderr).
			Msg("tailscale status command produced stderr output")
	}

	var tsStatus tailscaleStatus
	if err := json.Unmarshal(output, &tsStatus); err != nil {
		logger.Warn().
			Err(err).
			Msg("Failed to parse tailscale status JSON output")
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
		t.setCachedHostname(tsStatus.Self.DNSName)
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

	logger.Info().
		Str(log.FieldOperation, "status").
		Str("backend_state", tsStatus.BackendState).
		Bool("connected", status.Connected).
		Int("peers", len(status.Peers)).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Tailscale status check completed")

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
	if hostname := t.getCachedHostname(); hostname != "" {
		return hostname
	}

	// Try to get the hostname from status
	ctx := context.Background()
	status, err := t.Status(ctx)
	if err != nil {
		return ""
	}
	if hostname := t.getCachedHostname(); hostname != "" {
		return hostname
	}

	return status.Hostname
}

// GetPlainStatus runs `tailscale status` and returns the plain text output.
// This is useful when JSON parsing fails or for display purposes.
func (t *Tailscale) GetPlainStatus(ctx context.Context) (string, error) {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentTunnel)

	logger.Debug().Str(log.FieldOperation, "plain_status").Msg("Executing tailscale status for plain text output")

	output, stderr, err := t.commandRunner().Output(ctx, t.binaryPath, "status")
	if err != nil {
		logger.Error().
			Err(err).
			Str(log.FieldOperation, "plain_status").
			Str("stderr", stderr).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("Failed to execute tailscale plain status command")
		return "", err
	}

	if stderr != "" {
		logger.Debug().
			Str("stderr", stderr).
			Msg("tailscale plain status command produced stderr output")
	}

	logger.Debug().
		Str(log.FieldOperation, "plain_status").
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Successfully retrieved Tailscale plain status")

	return string(output), nil
}
