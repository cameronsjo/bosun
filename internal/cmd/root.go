// Package cmd provides the CLI commands for bosun.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/ui"
)

// Version information - set by goreleaser ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "bosun",
	Short: "Helm for home - GitOps for Docker Compose",
	Long: `bosun - Helm for home

A nautical-themed GitOps toolkit for managing Docker Compose deployments
with Traefik, Gatus, and Homepage integration.

SETUP
  init                  Christen your yacht (interactive setup wizard)
    --systemd           Generate systemd unit files for daemon mode

DAEMON COMMANDS
  daemon                Run the GitOps daemon (long-running service)
  trigger               Trigger reconciliation via daemon
  daemon-status         Show daemon status
  webhook               Run standalone webhook receiver
  validate              Validate configuration and connectivity
    --full              Run full dry-run reconciliation

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

TEMPLATE COMMANDS
  render [files...]     Render .tmpl files with SOPS secrets
    --secrets, -s       SOPS secrets file
    --output, -o        Output directory (stdout if not set)

COMMS COMMANDS
  radio test            Test webhook endpoint
  radio status          Check Tailscale/tunnel status

ALERT COMMANDS
  alert status          Show configured alert providers
  alert test            Send test alert to providers
    --provider, -p      Test specific provider (discord, sendgrid, twilio)
    --message, -m       Custom test message
    --severity, -s      Alert severity (info, warning, error)

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
  overboard [name]      Force remove a problematic container

MAINTENANCE
  update                Update bosun to the latest version
    --check             Only check for updates, don't install`,
	Version: version,
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

// yarrCmd is the hidden easter egg command.
var yarrCmd = &cobra.Command{
	Use:    "yarr",
	Hidden: true,
	Short:  "Pirate mode",
	Run: func(cmd *cobra.Command, args []string) {
		ui.Yellow.Println("Ahoy! Ye found the secret pirate mode!")
		fmt.Println("")
		fmt.Println("Command aliases for true pirates:")
		fmt.Println("  init       → christen")
		fmt.Println("  yacht      → hoist")
		fmt.Println("  crew       → scallywags")
		fmt.Println("  provision  → plunder")
		fmt.Println("  provisions → loot")
		fmt.Println("  create     → forge")
		fmt.Println("  radio      → parrot")
		fmt.Println("  alert      → horn")
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

	// Version template with build info
	rootCmd.SetVersionTemplate(fmt.Sprintf("bosun version {{.Version}}\ncommit: %s\nbuilt: %s\n", commit, date))

	// Add completion command
	rootCmd.AddCommand(completionCmd)
}

// completionCmd generates shell completion scripts.
var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Long: `Generate shell completion scripts for bosun.

To load completions:

Bash:
  $ source <(bosun completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ bosun completion bash > /etc/bash_completion.d/bosun
  # macOS:
  $ bosun completion bash > $(brew --prefix)/etc/bash_completion.d/bosun

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. Execute once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ bosun completion zsh > "${fpath[1]}/_bosun"

  # You may need to start a new shell for this to take effect.

Fish:
  $ bosun completion fish | source

  # To load completions for each session, execute once:
  $ bosun completion fish > ~/.config/fish/completions/bosun.fish

PowerShell:
  PS> bosun completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, run:
  PS> bosun completion powershell > bosun.ps1
  # and source this file from your PowerShell profile.
`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	Run: func(cmd *cobra.Command, args []string) {
		switch args[0] {
		case "bash":
			_ = cmd.Root().GenBashCompletion(os.Stdout)
		case "zsh":
			_ = cmd.Root().GenZshCompletion(os.Stdout)
		case "fish":
			_ = cmd.Root().GenFishCompletion(os.Stdout, true)
		case "powershell":
			_ = cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
		}
	},
}
