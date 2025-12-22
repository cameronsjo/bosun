package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/ui"
)

// Radio command constants.
const (
	// RadioTestTimeout is the HTTP timeout for testing the webhook endpoint.
	RadioTestTimeout = 5 * time.Second
	// MaxOnlinePeersDisplay is the maximum number of online Tailscale peers to show.
	MaxOnlinePeersDisplay = 5
)

// radioCmd represents the radio command group.
var radioCmd = &cobra.Command{
	Use:     "radio",
	Aliases: []string{"parrot"},
	Short:   "Communication and connectivity commands",
	Long: `Radio commands for testing connectivity and checking tunnel status.

Commands:
  test      Test webhook endpoint
  status    Check Tailscale/tunnel status`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
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

// radioStatusCmd checks Tailscale/tunnel status.
var radioStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check Tailscale/tunnel status",
	Long:  "Display Tailscale connection status and tunnel information.",
	Run:   runRadioStatus,
}

// TailscaleStatus represents the JSON output of tailscale status --json.
type TailscaleStatus struct {
	BackendState string                 `json:"BackendState"`
	Self         TailscalePeer          `json:"Self"`
	Peer         map[string]TailscalePeer `json:"Peer"`
	MagicDNSSuffix string               `json:"MagicDNSSuffix"`
}

// TailscalePeer represents a peer in the Tailscale network.
type TailscalePeer struct {
	DNSName      string   `json:"DNSName"`
	HostName     string   `json:"HostName"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Online       bool     `json:"Online"`
	ExitNode     bool     `json:"ExitNode"`
	Active       bool     `json:"Active"`
}

func runRadioStatus(cmd *cobra.Command, args []string) {
	ui.Info("Checking comms...")

	// Check if tailscale is installed
	tailscalePath, err := exec.LookPath("tailscale")
	if err != nil {
		ui.Warning("Tailscale not installed locally")
		fmt.Println()
		fmt.Println("Install Tailscale from: https://tailscale.com/download")
		return
	}

	// Try to get JSON status first for structured output
	jsonCmd := exec.Command(tailscalePath, "status", "--json")
	jsonOutput, err := jsonCmd.Output()
	if err == nil {
		var status TailscaleStatus
		if err := json.Unmarshal(jsonOutput, &status); err == nil {
			displayTailscaleStatus(&status)
			return
		}
	}

	// Fall back to plain text output
	plainCmd := exec.Command(tailscalePath, "status")
	plainCmd.Stdout = os.Stdout
	plainCmd.Stderr = os.Stderr
	if err := plainCmd.Run(); err != nil {
		ui.Error("Failed to get Tailscale status: %v", err)
		os.Exit(1)
	}
}

// displayTailscaleStatus formats and displays the Tailscale status.
func displayTailscaleStatus(status *TailscaleStatus) {
	fmt.Println()

	// Connection state
	switch status.BackendState {
	case "Running":
		ui.Success("Tailscale is connected")
	case "Stopped":
		ui.Error("Tailscale is stopped")
		fmt.Println("  Run: tailscale up")
		return
	case "NeedsLogin":
		ui.Warning("Tailscale needs login")
		fmt.Println("  Run: tailscale login")
		return
	default:
		ui.Warning("Tailscale state: %s", status.BackendState)
	}

	// Self info
	fmt.Println()
	ui.Blue.Println("--- This Device ---")
	fmt.Printf("  Hostname: %s\n", status.Self.HostName)
	if len(status.Self.TailscaleIPs) > 0 {
		fmt.Printf("  IP: %s\n", status.Self.TailscaleIPs[0])
	}
	if status.Self.DNSName != "" {
		fmt.Printf("  DNS: %s\n", status.Self.DNSName)
	}

	// Network info
	if status.MagicDNSSuffix != "" {
		fmt.Printf("  Tailnet: %s\n", status.MagicDNSSuffix)
	}

	// Peer count
	onlinePeers := 0
	totalPeers := len(status.Peer)
	for _, peer := range status.Peer {
		if peer.Online {
			onlinePeers++
		}
	}

	fmt.Println()
	ui.Blue.Println("--- Network ---")
	fmt.Printf("  Peers: %d online / %d total\n", onlinePeers, totalPeers)

	// Show a few online peers
	if onlinePeers > 0 {
		fmt.Println()
		ui.Blue.Println("--- Online Peers ---")

		// Sort peer keys for deterministic output order
		peerKeys := make([]string, 0, len(status.Peer))
		for key := range status.Peer {
			peerKeys = append(peerKeys, key)
		}
		sort.Strings(peerKeys)

		count := 0
		for _, key := range peerKeys {
			peer := status.Peer[key]
			if !peer.Online {
				continue
			}
			if count >= MaxOnlinePeersDisplay {
				fmt.Printf("  ... and %d more\n", onlinePeers-MaxOnlinePeersDisplay)
				break
			}

			name := peer.HostName
			if name == "" {
				name = peer.DNSName
			}

			indicator := "*"
			if peer.ExitNode {
				indicator = "E"
			}

			ip := ""
			if len(peer.TailscaleIPs) > 0 {
				ip = peer.TailscaleIPs[0]
			}

			ui.Green.Printf("  %s %s", indicator, name)
			if ip != "" {
				fmt.Printf(" (%s)", ip)
			}
			fmt.Println()

			count++
		}
	}

	fmt.Println()
}

func init() {
	radioCmd.AddCommand(radioTestCmd)
	radioCmd.AddCommand(radioStatusCmd)

	rootCmd.AddCommand(radioCmd)
}
