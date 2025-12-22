package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

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

		// Validate compose file before operations
		if err := validateComposeFile(cfg.ComposeFile); err != nil {
			return fmt.Errorf("%w. Run 'docker compose config' to debug", err)
		}

		// Validate service names if provided
		if len(args) > 0 {
			if err := validateServiceNames(cfg.ComposeFile, args); err != nil {
				return err
			}
		}

		// Check traefik status
		// NOTE: Docker client is optional here - we continue even if it fails.
		// This allows yacht up to work in environments where Docker API isn't accessible
		// but docker compose commands still work (e.g., remote Docker hosts).
		if err := withDockerClientContext(ctx, func(client *docker.Client) error {
			return checkTraefik(ctx, client)
		}); err != nil {
			ui.Warning("%v", err)
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

		// Validate compose file before operations
		if err := validateComposeFile(cfg.ComposeFile); err != nil {
			return fmt.Errorf("%w. Run 'docker compose config' to debug", err)
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

		// Validate compose file before operations
		if err := validateComposeFile(cfg.ComposeFile); err != nil {
			return fmt.Errorf("%w. Run 'docker compose config' to debug", err)
		}

		// Validate service names if provided
		if len(args) > 0 {
			if err := validateServiceNames(cfg.ComposeFile, args); err != nil {
				return err
			}
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

// validateComposeFile validates that a compose file exists and has valid syntax.
func validateComposeFile(composePath string) error {
	// Check file exists
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		return fmt.Errorf("compose file not found: %s", composePath)
	}

	// Run docker compose config --quiet to validate syntax
	cmd := exec.Command("docker", "compose", "-f", composePath, "config", "--quiet")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Parse error output for actionable message
		errMsg := strings.TrimSpace(string(output))
		if errMsg == "" {
			errMsg = err.Error()
		}
		return fmt.Errorf("invalid compose file: %s", errMsg)
	}

	return nil
}

// composeServices represents a Docker Compose file for extracting service names.
type composeServices struct {
	Services map[string]any `yaml:"services"`
}

// validateServiceNames validates that all provided service names exist in the compose file.
func validateServiceNames(composePath string, services []string) error {
	if len(services) == 0 {
		return nil
	}

	// Read compose file
	data, err := os.ReadFile(composePath)
	if err != nil {
		return fmt.Errorf("read compose file: %w", err)
	}

	// Parse YAML to get service names
	var compose composeServices
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return fmt.Errorf("parse compose file: %w", err)
	}

	// Build list of valid service names
	validNames := make(map[string]bool)
	var validList []string
	for name := range compose.Services {
		validNames[name] = true
		validList = append(validList, name)
	}

	// Check each provided service name
	var invalidNames []string
	for _, svc := range services {
		if !validNames[svc] {
			invalidNames = append(invalidNames, svc)
		}
	}

	if len(invalidNames) > 0 {
		return fmt.Errorf("unknown services: %s. Valid services: %s",
			strings.Join(invalidNames, ", "),
			strings.Join(validList, ", "))
	}

	return nil
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
