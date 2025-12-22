// Package cmd provides the CLI commands for bosun.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/ui"
)

const version = "0.2.0"

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "bosun",
	Short: "Helm for home - GitOps for Docker Compose",
	Long: `bosun - Helm for home

A nautical-themed GitOps toolkit for managing Docker Compose deployments
with Traefik, Gatus, and Homepage integration.

SETUP
  init                  Christen your yacht (interactive setup wizard)

YACHT COMMANDS
  yacht up              Start the yacht (docker compose up -d)
  yacht down            Dock the yacht (docker compose down)
  yacht restart         Quick turnaround
  yacht status          Check if we're seaworthy

CREW COMMANDS
  crew list             Show all hands on deck (docker ps)
  crew logs [name]      Tail crew member logs
  crew inspect [name]   Detailed crew info
  crew restart [name]   Send crew member for coffee break

MANIFEST COMMANDS
  provision [stack]     Render manifest to compose/traefik/gatus
    --dry-run, -n       Show what would be generated without writing
    --diff, -d          Show diff against existing output files
    --values, -f <file> Apply values overlay (e.g., prod.yaml)
  provisions            List available provisions
  create <tmpl> <name>  Scaffold new service (webapp, api, worker, static)

COMMS COMMANDS
  radio test            Test webhook endpoint
  radio status          Check Tailscale/tunnel status

DIAGNOSTICS
  status                Show yacht health dashboard
  log [n]               Show release history
  drift                 Detect config drift - git vs running state
  doctor                Pre-flight checks - is the ship seaworthy?
  lint                  Validate all manifests before deploy

EMERGENCY
  mayday                Show recent errors across all crew
    --rollback, -r      Rollback to a previous snapshot
    --list, -l          List available snapshots
  overboard [name]      Force remove a problematic container`,
	Version: version,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

// yarrCmd is the hidden easter egg command.
var yarrCmd = &cobra.Command{
	Use:    "yarr",
	Hidden: true,
	Short:  "Pirate mode",
	Run: func(cmd *cobra.Command, args []string) {
		ui.Yellow.Println("🏴\u200d☠️ Ahoy! Ye found the secret pirate mode!")
		fmt.Println("")
		fmt.Println("Command aliases for true pirates:")
		fmt.Println("  init       → christen")
		fmt.Println("  yacht      → hoist")
		fmt.Println("  crew       → scallywags")
		fmt.Println("  provision  → plunder")
		fmt.Println("  provisions → loot")
		fmt.Println("  create     → forge")
		fmt.Println("  radio      → parrot")
		fmt.Println("  status     → bridge")
		fmt.Println("  log        → ledger")
		fmt.Println("  drift      → compass")
		fmt.Println("  doctor     → checkup")
		fmt.Println("  lint       → inspect")
		fmt.Println("  mayday     → mutiny")
		fmt.Println("  overboard  → plank")
		fmt.Println("")
		ui.Blue.Println("Run 'bosun --help' for all commands.")
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	// Add hidden yarr command
	rootCmd.AddCommand(yarrCmd)

	// Version template
	rootCmd.SetVersionTemplate("bosun version {{.Version}}\n")
}
