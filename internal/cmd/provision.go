package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/config"
	"github.com/cameronsjo/bosun/internal/lock"
	"github.com/cameronsjo/bosun/internal/manifest"
	"github.com/cameronsjo/bosun/internal/ui"
)

// Provision command display limits.
const (
	// MaxDiffOutputLines is the maximum number of lines to show in diff output before truncation.
	MaxDiffOutputLines = 50
)

var (
	provisionDryRun bool
	provisionDiff   bool
	provisionValues string
)

// provisionCmd renders manifest to compose/traefik/gatus.
var provisionCmd = &cobra.Command{
	Use:     "provision [stack|chart]",
	Aliases: []string{"plunder", "loot", "forge"},
	Short:   "Render manifest to compose/traefik/gatus",
	Long: `Render a stack or chart manifest into compose, traefik, and gatus outputs.

Supports both legacy (provisions) and Helm-aligned (charts) formats.
The format is auto-detected based on directory structure.

Examples:
  bosun provision core           # Render the 'core' stack
  bosun provision -n core        # Dry run - show output without writing
  bosun provision -d core        # Show diff against existing files
  bosun provision -f prod.yaml   # Apply values overlay`,
	Args: cobra.MaximumNArgs(1),
	RunE: runProvision,
}

// provisionsCmd lists available provisions (legacy format).
var provisionsCmd = &cobra.Command{
	Use:   "provisions",
	Short: "List available provisions (legacy format)",
	Long:  `List all available provision templates in the provisions directory.`,
	RunE:  runListProvisions,
}

// chartCmd is the parent command for chart operations.
var chartCmd = &cobra.Command{
	Use:   "chart",
	Short: "Manage charts (Helm-aligned format)",
	Long:  `Commands for managing Helm-aligned chart definitions.`,
}

// chartListCmd lists available charts.
var chartListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available charts",
	Long:  `List all available charts in the charts directory.`,
	RunE:  runListCharts,
}

// chartShowCmd shows details about a chart.
var chartShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show chart details",
	Long:  `Display detailed information about a chart including templates and dependencies.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runShowChart,
}

// templateCmd is the parent command for template operations.
var templateCmd = &cobra.Command{
	Use:     "template",
	Aliases: []string{"templates"},
	Short:   "Manage templates (Helm-aligned format)",
	Long:    `Commands for managing Helm-aligned template definitions.`,
}

// templateListCmd lists available templates.
var templateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available templates",
	Long:  `List all available templates in the templates directory.`,
	RunE:  runListTemplates,
}

// createCmd scaffolds a new service from a template.
var createCmd = &cobra.Command{
	Use:   "create <template> <name>",
	Short: "Scaffold new service from template",
	Long: `Create a new service manifest from a template.

Available templates:
  webapp    Web application with Traefik routing
  api       API service with health checks
  worker    Background worker service
  static    Static file server`,
	Args: cobra.ExactArgs(2),
	RunE: runCreate,
}

func init() {
	// Provision command flags
	provisionCmd.Flags().BoolVarP(&provisionDryRun, "dry-run", "n", false, "Show what would be generated without writing")
	provisionCmd.Flags().BoolVarP(&provisionDiff, "diff", "d", false, "Show diff against existing output files")
	provisionCmd.Flags().StringVarP(&provisionValues, "values", "f", "", "Apply values overlay file (YAML)")

	// Chart subcommands
	chartCmd.AddCommand(chartListCmd)
	chartCmd.AddCommand(chartShowCmd)

	// Template subcommands
	templateCmd.AddCommand(templateListCmd)

	// Add commands to root
	rootCmd.AddCommand(provisionCmd)
	rootCmd.AddCommand(provisionsCmd)
	rootCmd.AddCommand(chartCmd)
	rootCmd.AddCommand(templateCmd)
	rootCmd.AddCommand(createCmd)
}

func runProvision(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Load values overlay if provided
	var valuesOverlay map[string]any
	if provisionValues != "" {
		valuesOverlay, err = manifest.LoadValuesOverlay(provisionValues)
		if err != nil {
			return fmt.Errorf("load values: %w", err)
		}
	}

	if len(args) == 0 {
		return fmt.Errorf("stack or chart name required (e.g., 'bosun provision core')")
	}

	name := args[0]

	// Detect format and render accordingly
	format := cfg.Format()
	var output *manifest.RenderOutput
	var outputName string

	switch format {
	case "helm":
		output, outputName, err = provisionHelm(cfg, name, valuesOverlay)
	case "legacy":
		output, outputName, err = provisionLegacy(cfg, name, valuesOverlay)
	default:
		return fmt.Errorf("unknown project format (no charts/ or provisions/ directory found)")
	}

	if err != nil {
		return err
	}

	if provisionDryRun {
		yamlOutput, err := manifest.RenderToYAML(output)
		if err != nil {
			return fmt.Errorf("render yaml: %w", err)
		}
		fmt.Print(yamlOutput)
		return nil
	}

	if provisionDiff {
		return showDiff(output, cfg.OutputDir(), outputName)
	}

	// Acquire provision lock to prevent concurrent writes
	lockDir := cfg.ManifestDir
	if format == "helm" {
		lockDir = cfg.ChartsDir()
	}
	provisionLock := lock.New(lockDir, "provision")
	if err := provisionLock.Acquire(); err != nil {
		return fmt.Errorf("acquire provision lock: %w", err)
	}
	defer func() { _ = provisionLock.Release() }()

	if err := manifest.WriteOutputs(output, cfg.OutputDir(), outputName); err != nil {
		return fmt.Errorf("write outputs: %w", err)
	}

	ui.Green.Printf("Successfully provisioned %s\n", outputName)
	return nil
}

// provisionHelm renders using Helm-aligned format.
func provisionHelm(cfg *config.Config, name string, valuesOverlay map[string]any) (*manifest.RenderOutput, string, error) {
	loader, err := manifest.NewChartLoader(cfg.ChartsDir())
	if err != nil {
		return nil, "", fmt.Errorf("create chart loader: %w", err)
	}

	// Check for stack first
	stackPath := filepath.Join(cfg.HelmStacksDir(), name, "Stack.yaml")
	if _, err := os.Stat(stackPath); err == nil {
		ui.Blue.Printf("Rendering stack: %s (Helm-aligned)\n", name)
		output, err := loader.RenderStack(stackPath, valuesOverlay)
		if err != nil {
			return nil, "", fmt.Errorf("render stack: %w", err)
		}
		return output, name, nil
	}

	// Check for stack file without directory
	stackFilePath := filepath.Join(cfg.HelmStacksDir(), name+".yaml")
	if _, err := os.Stat(stackFilePath); err == nil {
		ui.Blue.Printf("Rendering stack: %s (Helm-aligned)\n", name)
		output, err := loader.RenderStack(stackFilePath, valuesOverlay)
		if err != nil {
			return nil, "", fmt.Errorf("render stack: %w", err)
		}
		return output, name, nil
	}

	// Try as chart
	if loader.ChartExists(name) {
		ui.Blue.Printf("Rendering chart: %s (Helm-aligned)\n", name)
		output, err := loader.RenderChart(name, valuesOverlay)
		if err != nil {
			return nil, "", fmt.Errorf("render chart: %w", err)
		}
		return output, name, nil
	}

	return nil, "", fmt.Errorf("stack or chart not found: %s", name)
}

// provisionLegacy renders using legacy provisions format.
func provisionLegacy(cfg *config.Config, name string, valuesOverlay map[string]any) (*manifest.RenderOutput, string, error) {
	// Check if it's a stack or service
	stackPath := filepath.Join(cfg.StacksDir(), name+".yml")
	servicePath := filepath.Join(cfg.ServicesDir(), name+".yml")

	if _, err := os.Stat(stackPath); err == nil {
		// Render stack
		ui.Blue.Printf("Rendering stack: %s (legacy)\n", name)
		output, err := manifest.RenderStack(stackPath, cfg.ProvisionsDir(), cfg.ServicesDir(), valuesOverlay)
		if err != nil {
			return nil, "", fmt.Errorf("render stack: %w", err)
		}
		return output, name, nil
	}

	if _, err := os.Stat(servicePath); err == nil {
		// Render single service
		ui.Blue.Printf("Rendering service: %s (legacy)\n", name)
		svcManifest, err := manifest.LoadServiceManifest(servicePath)
		if err != nil {
			return nil, "", fmt.Errorf("load service: %w", err)
		}

		// Apply values overlay
		if valuesOverlay != nil {
			if svcManifest.Config == nil {
				svcManifest.Config = make(map[string]any)
			}
			svcManifest.Config = manifest.DeepMerge(svcManifest.Config, valuesOverlay)
		}

		output, err := manifest.RenderService(svcManifest, cfg.ProvisionsDir())
		if err != nil {
			return nil, "", fmt.Errorf("render service: %w", err)
		}
		return output, svcManifest.Name, nil
	}

	return nil, "", fmt.Errorf("stack or service not found: %s", name)
}

func runListProvisions(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	provisions, err := manifest.ListProvisions(cfg.ProvisionsDir())
	if err != nil {
		return fmt.Errorf("list provisions: %w", err)
	}

	if len(provisions) == 0 {
		fmt.Println("No provisions found")
		return nil
	}

	ui.Blue.Println("Available provisions:")
	for _, p := range provisions {
		fmt.Printf("  - %s\n", p)
	}

	return nil
}

func runCreate(cmd *cobra.Command, args []string) error {
	template := args[0]
	name := args[1]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Validate template
	validTemplates := map[string]bool{
		"webapp": true,
		"api":    true,
		"worker": true,
		"static": true,
	}

	if !validTemplates[template] {
		return fmt.Errorf("unknown template: %s (available: webapp, api, worker, static)", template)
	}

	// Create service manifest
	servicePath := filepath.Join(cfg.ServicesDir(), name+".yml")
	if _, err := os.Stat(servicePath); err == nil {
		return fmt.Errorf("service already exists: %s", servicePath)
	}

	content := generateServiceTemplate(template, name)

	if err := os.MkdirAll(cfg.ServicesDir(), 0755); err != nil {
		return fmt.Errorf("create services directory: %w", err)
	}

	if err := os.WriteFile(servicePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write service file: %w", err)
	}

	ui.Green.Printf("Created service: %s\n", servicePath)
	fmt.Printf("Edit the file and run 'bosun provision %s' to generate outputs\n", name)

	return nil
}

func generateServiceTemplate(template, name string) string {
	templates := map[string]string{
		"webapp": `name: %s
provisions:
  - webapp
config:
  port: 8080
  domain: %s.example.com
`,
		"api": `name: %s
provisions:
  - api
config:
  port: 8080
  health_path: /health
`,
		"worker": `name: %s
provisions:
  - worker
config:
  replicas: 1
`,
		"static": `name: %s
provisions:
  - static
config:
  root: /var/www/html
`,
	}

	return fmt.Sprintf(templates[template], name, name)
}

func showDiff(output *manifest.RenderOutput, outputDir, stackName string) error {
	// For now, just show a placeholder - full diff implementation would compare
	// generated YAML against existing files
	ui.Yellow.Println("Diff mode not yet implemented")
	ui.Blue.Println("Would compare generated output against:")

	targets := []struct {
		name     string
		filename string
	}{
		{"compose", stackName + ".yml"},
		{"traefik", "dynamic.yml"},
		{"gatus", "endpoints.yml"},
	}

	for _, t := range targets {
		path := filepath.Join(outputDir, t.name, t.filename)
		if _, err := os.Stat(path); err == nil {
			fmt.Printf("  - %s\n", path)
		} else {
			fmt.Printf("  - %s (new file)\n", path)
		}
	}

	// Show what would be generated
	fmt.Println()
	ui.Blue.Println("Generated output:")
	yamlOutput, err := manifest.RenderToYAML(output)
	if err != nil {
		return err
	}

	// Truncate long output
	lines := strings.Split(yamlOutput, "\n")
	if len(lines) > MaxDiffOutputLines {
		fmt.Println(strings.Join(lines[:MaxDiffOutputLines], "\n"))
		fmt.Printf("... (%d more lines)\n", len(lines)-MaxDiffOutputLines)
	} else {
		fmt.Print(yamlOutput)
	}

	return nil
}

// runListCharts lists all available charts.
func runListCharts(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if cfg.Format() != "helm" {
		return fmt.Errorf("project uses legacy format (no charts/ directory)")
	}

	loader, err := manifest.NewChartLoader(cfg.ChartsDir())
	if err != nil {
		return fmt.Errorf("create chart loader: %w", err)
	}

	charts, err := loader.ListCharts()
	if err != nil {
		return fmt.Errorf("list charts: %w", err)
	}

	if len(charts) == 0 {
		fmt.Println("No charts found")
		return nil
	}

	ui.Blue.Println("Available charts:")
	for _, c := range charts {
		chart, err := loader.GetChartInfo(c)
		if err != nil {
			fmt.Printf("  - %s\n", c)
			continue
		}

		if chart.Version != "" {
			fmt.Printf("  - %s (%s)\n", c, chart.Version)
		} else {
			fmt.Printf("  - %s\n", c)
		}

		if chart.Description != "" {
			fmt.Printf("      %s\n", chart.Description)
		}
	}

	return nil
}

// runShowChart shows details about a specific chart.
func runShowChart(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if cfg.Format() != "helm" {
		return fmt.Errorf("project uses legacy format (no charts/ directory)")
	}

	loader, err := manifest.NewChartLoader(cfg.ChartsDir())
	if err != nil {
		return fmt.Errorf("create chart loader: %w", err)
	}

	chartName := args[0]
	chart, err := loader.GetChartInfo(chartName)
	if err != nil {
		return fmt.Errorf("load chart: %w", err)
	}

	ui.Blue.Printf("Chart: %s\n", chart.Name)
	if chart.Version != "" {
		fmt.Printf("Version: %s\n", chart.Version)
	}
	if chart.Description != "" {
		fmt.Printf("Description: %s\n", chart.Description)
	}
	if chart.Homepage != "" {
		fmt.Printf("Homepage: %s\n", chart.Homepage)
	}

	if len(chart.Templates) > 0 {
		fmt.Println("\nTemplates:")
		for _, t := range chart.Templates {
			fmt.Printf("  - %s\n", t)
		}
	}

	if len(chart.Dependencies) > 0 {
		fmt.Println("\nDependencies:")
		for _, d := range chart.Dependencies {
			if d.Version != "" {
				fmt.Printf("  - %s:%s\n", d.Name, d.Version)
			} else {
				fmt.Printf("  - %s\n", d.Name)
			}
		}
	}

	return nil
}

// runListTemplates lists all available templates.
func runListTemplates(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if cfg.Format() != "helm" {
		return fmt.Errorf("project uses legacy format (use 'bosun provisions' instead)")
	}

	loader, err := manifest.NewChartLoader(cfg.ChartsDir())
	if err != nil {
		return fmt.Errorf("create chart loader: %w", err)
	}

	templates, err := loader.ListTemplates()
	if err != nil {
		return fmt.Errorf("list templates: %w", err)
	}

	if len(templates) == 0 {
		fmt.Println("No templates found")
		return nil
	}

	ui.Blue.Println("Available templates:")
	for _, t := range templates {
		fmt.Printf("  - %s\n", t)
	}

	return nil
}
