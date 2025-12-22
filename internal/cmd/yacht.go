package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/config"
	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/ui"
)

var yachtCmd = &cobra.Command{
	Use:     "yacht",
	Aliases: []string{"hoist"},
	Short:   "Manage Docker Compose services",
	Long: `Yacht commands for managing Docker Compose services.

Commands:
  up        Start the yacht (docker compose up -d)
  down      Dock the yacht (docker compose down)
  restart   Quick turnaround (docker compose restart)
  status    Check if we're seaworthy`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var yachtUpCmd = &cobra.Command{
	Use:   "up [services...]",
	Short: "Start the yacht (docker compose up -d)",
	Long:  `Starts all services defined in the compose file. Checks for Traefik first.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		// Check traefik status
		dockerClient, err := docker.NewClient()
		if err != nil {
			ui.Warning("Could not connect to Docker: %v", err)
		} else {
			defer dockerClient.Close()

			if err := checkTraefik(ctx, dockerClient); err != nil {
				ui.Warning("%v", err)
			}
		}

		ui.Green.Println("Raising anchor...")
		compose := docker.NewComposeClient(cfg.ComposeFile)
		if err := compose.Up(ctx, args...); err != nil {
			return fmt.Errorf("compose up: %w", err)
		}

		ui.Success("Yacht is underway!")
		return nil
	},
}

var yachtDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Dock the yacht (docker compose down)",
	Long:  `Stops and removes all services defined in the compose file.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		ui.Yellow.Println("Dropping anchor...")
		compose := docker.NewComposeClient(cfg.ComposeFile)
		if err := compose.Down(ctx); err != nil {
			return fmt.Errorf("compose down: %w", err)
		}

		ui.Yellow.Println("Yacht is docked.")
		return nil
	},
}

var yachtRestartCmd = &cobra.Command{
	Use:   "restart [services...]",
	Short: "Quick turnaround (docker compose restart)",
	Long:  `Restarts all or specified services.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		ui.Blue.Println("Quick turnaround...")
		compose := docker.NewComposeClient(cfg.ComposeFile)
		if err := compose.Restart(ctx, args...); err != nil {
			return fmt.Errorf("compose restart: %w", err)
		}

		ui.Success("Turnaround complete!")
		return nil
	},
}

var yachtStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check if we're seaworthy",
	Long:  `Shows the status of all services in the compose file.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		compose := docker.NewComposeClient(cfg.ComposeFile)
		output, err := compose.Ps(ctx)
		if err != nil {
			return fmt.Errorf("compose ps: %w", err)
		}

		fmt.Print(output)
		return nil
	},
}

// checkTraefik verifies traefik is running and starts it if needed.
func checkTraefik(ctx context.Context, client *docker.Client) error {
	running := client.IsContainerRunning(ctx, "traefik")
	if running {
		ui.Success("Traefik is running")
		return nil
	}

	exists, err := client.Exists(ctx, "traefik")
	if err != nil {
		return fmt.Errorf("check traefik: %w", err)
	}

	if exists {
		ui.Warning("Traefik exists but is not running. Starting it first...")
		if err := client.Start(ctx, "traefik"); err != nil {
			return fmt.Errorf("start traefik: %w", err)
		}
		ui.Success("Traefik started")
		return nil
	}

	return fmt.Errorf("traefik not found - reverse proxy won't work until traefik is deployed")
}

func init() {
	yachtCmd.AddCommand(yachtUpCmd)
	yachtCmd.AddCommand(yachtDownCmd)
	yachtCmd.AddCommand(yachtRestartCmd)
	yachtCmd.AddCommand(yachtStatusCmd)

	rootCmd.AddCommand(yachtCmd)
}
