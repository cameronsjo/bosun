package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/config"
	"github.com/cameronsjo/bosun/internal/tunnel"
	"github.com/cameronsjo/bosun/internal/ui"
)

// Radio command constants.
const (
	// RadioTestTimeout is the HTTP timeout for testing the webhook endpoint.
	RadioTestTimeout = 5 * time.Second
	// MaxOnlinePeersDisplay is the maximum number of online tunnel peers to show.
	MaxOnlinePeersDisplay = 5
	// TunnelStatusTimeout is the timeout for tunnel status checks.
	TunnelStatusTimeout = 10 * time.Second
)

// radioCmd represents the radio command group.
var radioCmd = &cobra.Command{
	Use:     "radio",
	Aliases: []string{"parrot"},
	Short:   "Communication and connectivity commands",
	Long: `Radio commands for testing connectivity and checking tunnel status.

Commands:
  test      Test webhook endpoint
  status    Check tunnel status (Tailscale, Cloudflare, etc.)`,
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

// radioTestCmd tests the webhook endpoint.
var radioTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Test webhook endpoint",
	Long:  "Send a GET request to http://localhost:8080/health to verify the webhook receiver is running.",
	Run:   runRadioTest,
}

func runRadioTest(cmd *cobra.Command, args []string) {
	ui.Info("Testing radio...")

	client := &http.Client{
		Timeout: RadioTestTimeout,
	}

	resp, err := client.Get("http://localhost:8080/health")
	if err != nil {
		ui.Error("Radio silence. Check webhook container.")
		fmt.Printf("  Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		ui.Success("Radio is loud and clear!")
	} else {
		ui.Error("Radio responded with status %d", resp.StatusCode)
		os.Exit(1)
	}
}

// radioStatusCmd checks tunnel status.
var radioStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check tunnel status",
	Long:  "Display tunnel connection status and network information. Supports Tailscale, Cloudflare Tunnel, and other providers.",
	Run:   runRadioStatus,
}

func runRadioStatus(cmd *cobra.Command, args []string) {
	ui.Info("Checking comms...")

	// Load configuration to get tunnel provider
	cfg, err := config.Load()
	var providerName string
	if err != nil {
		// Default to Tailscale if config not available
		providerName = "tailscale"
	} else {
		providerName = cfg.TunnelProvider()
	}

	// Create the tunnel provider
	provider, err := tunnel.NewProvider(providerName)
	if err != nil {
		if notInstalled, ok := err.(tunnel.ErrNotInstalled); ok {
			ui.Warning("%s", notInstalled.Error())
			fmt.Println()
			displayInstallInstructions(providerName)
			return
		}
		ui.Error("Failed to create tunnel provider: %v", err)
		os.Exit(1)
	}

	// Get status with timeout
	ctx, cancel := context.WithTimeout(context.Background(), TunnelStatusTimeout)
	defer cancel()

	status, err := provider.Status(ctx)
	if err != nil {
		ui.Error("Failed to get tunnel status: %v", err)
		os.Exit(1)
	}

	// Display status based on provider type
	displayTunnelStatus(status)
}

// displayInstallInstructions shows installation instructions for the tunnel provider.
func displayInstallInstructions(providerName string) {
	switch providerName {
	case "tailscale":
		fmt.Println("Install Tailscale from: https://tailscale.com/download")
	case "cloudflare":
		fmt.Println("Install cloudflared from: https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/installation/")
	default:
		fmt.Printf("Install %s to use this tunnel provider.\n", providerName)
	}
}

// displayTunnelStatus formats and displays the tunnel status.
func displayTunnelStatus(status *tunnel.Status) {
	fmt.Println()

	// Connection state
	displayConnectionState(status)

	// Device info
	fmt.Println()
	ui.Blue.Println("--- This Device ---")
	if status.Hostname != "" {
		fmt.Printf("  Hostname: %s\n", status.Hostname)
	}
	if status.IP != "" {
		fmt.Printf("  IP: %s\n", status.IP)
	}
	if status.TailnetName != "" {
		fmt.Printf("  Network: %s\n", status.TailnetName)
	}

	// Peer information (for mesh networks like Tailscale)
	if len(status.Peers) > 0 {
		displayPeerInfo(status.Peers)
	}

	fmt.Println()
}

// displayConnectionState shows the connection state with appropriate messaging.
func displayConnectionState(status *tunnel.Status) {
	switch status.BackendState {
	case "Running":
		ui.Success("%s is connected", capitalizeFirst(status.Provider))
	case "Stopped":
		ui.Error("%s is stopped", capitalizeFirst(status.Provider))
		displayStartInstructions(status.Provider)
		return
	case "NeedsLogin":
		ui.Warning("%s needs login", capitalizeFirst(status.Provider))
		displayLoginInstructions(status.Provider)
		return
	case "Disconnected":
		ui.Warning("%s is disconnected", capitalizeFirst(status.Provider))
	case "Unknown":
		ui.Warning("%s state is unknown", capitalizeFirst(status.Provider))
	default:
		if status.Connected {
			ui.Success("%s is connected", capitalizeFirst(status.Provider))
		} else {
			ui.Warning("%s state: %s", capitalizeFirst(status.Provider), status.BackendState)
		}
	}
}

// displayStartInstructions shows how to start the tunnel.
func displayStartInstructions(provider string) {
	switch provider {
	case "tailscale":
		fmt.Println("  Run: tailscale up")
	case "cloudflare":
		fmt.Println("  Run: cloudflared tunnel run <tunnel-name>")
	}
}

// displayLoginInstructions shows how to log in to the tunnel.
func displayLoginInstructions(provider string) {
	switch provider {
	case "tailscale":
		fmt.Println("  Run: tailscale login")
	case "cloudflare":
		fmt.Println("  Run: cloudflared tunnel login")
	}
}

// displayPeerInfo shows peer information for mesh networks.
func displayPeerInfo(peers []tunnel.Peer) {
	onlinePeers := 0
	for _, peer := range peers {
		if peer.Online {
			onlinePeers++
		}
	}

	fmt.Println()
	ui.Blue.Println("--- Network ---")
	fmt.Printf("  Peers: %d online / %d total\n", onlinePeers, len(peers))

	if onlinePeers > 0 {
		fmt.Println()
		ui.Blue.Println("--- Online Peers ---")

		// Sort peers by name for deterministic output
		sortedPeers := make([]tunnel.Peer, len(peers))
		copy(sortedPeers, peers)
		sort.Slice(sortedPeers, func(i, j int) bool {
			return sortedPeers[i].Name < sortedPeers[j].Name
		})

		count := 0
		for _, peer := range sortedPeers {
			if !peer.Online {
				continue
			}
			if count >= MaxOnlinePeersDisplay {
				fmt.Printf("  ... and %d more\n", onlinePeers-MaxOnlinePeersDisplay)
				break
			}

			name := peer.Name
			if name == "" {
				name = peer.DNSName
			}

			indicator := "*"
			if peer.ExitNode {
				indicator = "E"
			}

			ui.Green.Printf("  %s %s", indicator, name)
			if peer.IP != "" {
				fmt.Printf(" (%s)", peer.IP)
			}
			fmt.Println()

			count++
		}
	}
}

// capitalizeFirst capitalizes the first letter of a string.
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	return string(s[0]-32) + s[1:]
}

// TailscaleStatus represents the JSON output of tailscale status --json.
// Kept for backwards compatibility with existing tests.
type TailscaleStatus struct {
	BackendState   string                   `json:"BackendState"`
	Self           TailscalePeer            `json:"Self"`
	Peer           map[string]TailscalePeer `json:"Peer"`
	MagicDNSSuffix string                   `json:"MagicDNSSuffix"`
}

// TailscalePeer represents a peer in the Tailscale network.
// Kept for backwards compatibility with existing tests.
type TailscalePeer struct {
	DNSName      string   `json:"DNSName"`
	HostName     string   `json:"HostName"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Online       bool     `json:"Online"`
	ExitNode     bool     `json:"ExitNode"`
	Active       bool     `json:"Active"`
}

// displayTailscaleStatus formats and displays the Tailscale status.
// Kept for backwards compatibility with existing tests.
func displayTailscaleStatus(status *TailscaleStatus) {
	// Convert to generic tunnel.Status and display
	tunnelStatus := &tunnel.Status{
		Provider:     "tailscale",
		BackendState: status.BackendState,
		Hostname:     status.Self.HostName,
		TailnetName:  status.MagicDNSSuffix,
	}

	if len(status.Self.TailscaleIPs) > 0 {
		tunnelStatus.IP = status.Self.TailscaleIPs[0]
	}

	tunnelStatus.Connected = status.BackendState == "Running"

	// Convert peers
	for _, tsPeer := range status.Peer {
		peer := tunnel.Peer{
			Name:     tsPeer.HostName,
			DNSName:  tsPeer.DNSName,
			Online:   tsPeer.Online,
			ExitNode: tsPeer.ExitNode,
			Active:   tsPeer.Active,
		}
		if len(tsPeer.TailscaleIPs) > 0 {
			peer.IP = tsPeer.TailscaleIPs[0]
		}
		tunnelStatus.Peers = append(tunnelStatus.Peers, peer)
	}

	displayTunnelStatus(tunnelStatus)
}

func init() {
	radioCmd.AddCommand(radioTestCmd)
	radioCmd.AddCommand(radioStatusCmd)

	rootCmd.AddCommand(radioCmd)
}
