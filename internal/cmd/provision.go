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

var (
	provisionDryRun bool
	provisionDiff   bool
	provisionValues string
)

// provisionCmd renders manifest to compose/traefik/gatus.
var provisionCmd = &cobra.Command{
	Use:     "provision [stack]",
	Aliases: []string{"plunder", "loot", "forge"},
	Short:   "Render manifest to compose/traefik/gatus",
	Long: `Render a stack or service manifest into compose, traefik, and gatus outputs.

Examples:
  bosun provision core           # Render the 'core' stack
  bosun provision -n core        # Dry run - show output without writing
  bosun provision -d core        # Show diff against existing files
  bosun provision -f prod.yaml   # Apply values overlay`,
	Args: cobra.MaximumNArgs(1),
	RunE: runProvision,
}

// provisionsCmd lists available provisions.
var provisionsCmd = &cobra.Command{
	Use:   "provisions",
	Short: "List available provisions",
	Long:  `List all available provision templates in the provisions directory.`,
	RunE:  runListProvisions,
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

	// Add commands to root
	rootCmd.AddCommand(provisionCmd)
	rootCmd.AddCommand(provisionsCmd)
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

	var output *manifest.RenderOutput
	var stackName string

	if len(args) == 0 {
		// No argument - look for default stack or show usage
		return fmt.Errorf("stack name required (e.g., 'bosun provision core')")
	}

	stackName = args[0]

	// Check if it's a stack or service
	stackPath := filepath.Join(cfg.StacksDir(), stackName+".yml")
	servicePath := filepath.Join(cfg.ServicesDir(), stackName+".yml")

	if _, err := os.Stat(stackPath); err == nil {
		// Render stack
		output, err = manifest.RenderStack(stackPath, cfg.ProvisionsDir(), cfg.ServicesDir(), valuesOverlay)
		if err != nil {
			return fmt.Errorf("render stack: %w", err)
		}
	} else if _, err := os.Stat(servicePath); err == nil {
		// Render single service
		svcManifest, err := manifest.LoadServiceManifest(servicePath)
		if err != nil {
			return fmt.Errorf("load service: %w", err)
		}

		// Apply values overlay
		if valuesOverlay != nil {
			if svcManifest.Config == nil {
				svcManifest.Config = make(map[string]any)
			}
			svcManifest.Config = manifest.DeepMerge(svcManifest.Config, valuesOverlay)
		}

		output, err = manifest.RenderService(svcManifest, cfg.ProvisionsDir())
		if err != nil {
			return fmt.Errorf("render service: %w", err)
		}
		stackName = svcManifest.Name
	} else {
		return fmt.Errorf("stack or service not found: %s", stackName)
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
		return showDiff(output, cfg.OutputDir(), stackName)
	}

	// Acquire provision lock to prevent concurrent writes
	provisionLock := lock.New(cfg.ManifestDir, "provision")
	if err := provisionLock.Acquire(); err != nil {
		return fmt.Errorf("acquire provision lock: %w", err)
	}
	defer provisionLock.Release()

	if err := manifest.WriteOutputs(output, cfg.OutputDir(), stackName); err != nil {
		return fmt.Errorf("write outputs: %w", err)
	}

	ui.Green.Printf("Successfully provisioned %s\n", stackName)
	return nil
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
	if len(lines) > 50 {
		fmt.Println(strings.Join(lines[:50], "\n"))
		fmt.Printf("... (%d more lines)\n", len(lines)-50)
	} else {
		fmt.Print(yamlOutput)
	}

	return nil
}
