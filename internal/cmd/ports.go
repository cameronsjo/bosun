package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/cameronsjo/bosun/internal/config"
	"github.com/cameronsjo/bosun/internal/manifest"
	"github.com/cameronsjo/bosun/internal/ui"
)

var (
	portsService   string
	portsFreeRange string
)

// portsCmd lists all allocated ports and detects conflicts.
var portsCmd = &cobra.Command{
	Use:   "ports",
	Short: "Show allocated ports across all stacks",
	Long: `List all host-port allocations from rendered compose files and detect conflicts.

Examples:
  bosun ports                       # Show all allocated ports
  bosun ports --service traefik     # Show ports for a specific service
  bosun ports --free 8000-9000      # Show available ports in range`,
	RunE: runPorts,
}

func init() {
	portsCmd.Flags().StringVarP(&portsService, "service", "s", "", "Show ports for a specific service")
	portsCmd.Flags().StringVar(&portsFreeRange, "free", "", "Show available ports in range (e.g. 8000-9000)")

	rootCmd.AddCommand(portsCmd)
}

func runPorts(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	registry, err := buildPortRegistry(cfg)
	if err != nil {
		return fmt.Errorf("build port registry: %w", err)
	}

	if portsFreeRange != "" {
		return runPortsFree(registry, portsFreeRange)
	}

	if portsService != "" {
		return runPortsService(registry, portsService)
	}

	return runPortsList(registry)
}

// runPortsList prints all allocated ports grouped by service.
func runPortsList(registry *manifest.PortRegistry) error {
	entries := registry.Entries()
	conflicts := registry.Conflicts()

	if len(entries) == 0 {
		ui.Warning("No rendered compose files found. Run 'bosun provision' first.")
		return nil
	}

	ui.Header("Allocated Ports")
	fmt.Println()

	for _, e := range entries {
		line := fmt.Sprintf("  %5d/%-3s  %s@%s", e.Port, e.Protocol, e.ServiceName, e.StackName)
		if e.BindAddr != "" {
			line += fmt.Sprintf("  [%s]", e.BindAddr)
		}
		fmt.Println(line)
	}

	fmt.Println()

	if len(conflicts) == 0 {
		_, _ = ui.Green.Println("No port conflicts detected.")
		return nil
	}

	_, _ = ui.Red.Printf("%d port conflict(s) detected:\n", len(conflicts))
	fmt.Println()
	for _, c := range conflicts {
		_, _ = ui.Red.Printf("  ! Port %d/%s claimed by %s and %s\n",
			c.Key.Port, c.Key.Protocol,
			c.First.Qualifier(), c.Second.Qualifier())
	}
	fmt.Println()
	return fmt.Errorf("%d port conflict(s) found", len(conflicts))
}

// runPortsService prints all ports allocated by the named service.
func runPortsService(registry *manifest.PortRegistry, serviceName string) error {
	entries := registry.EntriesForService(serviceName)

	if len(entries) == 0 {
		ui.Warning("No ports found for service %q.", serviceName)
		return nil
	}

	ui.Header("Ports for service: %s", serviceName)
	fmt.Println()
	for _, e := range entries {
		line := fmt.Sprintf("  %5d/%-3s  (@%s)", e.Port, e.Protocol, e.StackName)
		if e.BindAddr != "" {
			line += fmt.Sprintf("  [%s]", e.BindAddr)
		}
		fmt.Println(line)
	}
	fmt.Println()
	return nil
}

// runPortsFree shows available ports in the requested range.
func runPortsFree(registry *manifest.PortRegistry, rangeStr string) error {
	parts := strings.SplitN(rangeStr, "-", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid range %q: expected format start-end (e.g. 8000-9000)", rangeStr)
	}

	var start, end int
	if _, err := fmt.Sscan(parts[0], &start); err != nil || start <= 0 {
		return fmt.Errorf("invalid range start %q", parts[0])
	}
	if _, err := fmt.Sscan(parts[1], &end); err != nil || end <= 0 {
		return fmt.Errorf("invalid range end %q", parts[1])
	}
	if start > end {
		return fmt.Errorf("range start %d must not exceed end %d", start, end)
	}

	free := registry.FreePorts(start, end)

	ui.Header("Free ports in %d-%d", start, end)
	fmt.Println()

	if len(free) == 0 {
		ui.Warning("No free ports in range %d-%d.", start, end)
		return nil
	}

	for i, p := range free {
		if i > 0 && i%10 == 0 {
			fmt.Println()
		}
		fmt.Printf("  %5d", p)
	}
	fmt.Println()
	fmt.Println()
	_, _ = ui.Green.Printf("%d free port(s) in range.\n", len(free))
	return nil
}

// =============================================================================
// Registry builder - reads rendered compose files from the output directory.
// =============================================================================

// buildPortRegistry loads all rendered compose files and builds a PortRegistry.
func buildPortRegistry(cfg *config.Config) (*manifest.PortRegistry, error) {
	registry := manifest.NewPortRegistry()

	composeDir := filepath.Join(cfg.OutputDir(), "compose")
	entries, err := os.ReadDir(composeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return registry, nil
		}
		return nil, fmt.Errorf("read compose output dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !isYAMLFile(entry.Name()) {
			continue
		}

		stackName := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		filePath := filepath.Join(composeDir, entry.Name())

		if err := addPortsFromCompose(registry, filePath, stackName); err != nil {
			// Non-fatal: log warning and continue with remaining stacks.
			ui.Warning("Failed to parse compose file %s: %v", entry.Name(), err)
		}
	}

	return registry, nil
}

// addPortsFromCompose reads a compose YAML file and registers all its port
// allocations in the registry.
func addPortsFromCompose(registry *manifest.PortRegistry, filePath, stackName string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	var compose manifest.ComposePortFile
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return fmt.Errorf("parse yaml: %w", err)
	}

	for serviceName, svc := range compose.Services {
		for _, rawPort := range svc.Ports {
			for _, parsed := range manifest.ParsePortEntry(rawPort) {
				registry.AddEntry(manifest.PortEntry{
					Port:        parsed.HostPort,
					Protocol:    parsed.Protocol,
					BindAddr:    parsed.BindAddr,
					ServiceName: serviceName,
					StackName:   stackName,
				})
			}
		}
	}

	return nil
}

// isYAMLFile returns true for .yml and .yaml extensions.
func isYAMLFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".yml" || ext == ".yaml"
}
