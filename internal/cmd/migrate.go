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

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate manifests to new format or add apiVersion fields",
	Long: `Migrate manifests to the current schema or convert to Helm-aligned format.

Subcommands:
  bosun migrate version   Add apiVersion/kind fields to unversioned manifests
  bosun migrate helm      Convert legacy provisions format to Helm-aligned charts

Examples:
  bosun migrate version        # Add apiVersion/kind (dry-run)
  bosun migrate version -w     # Apply version migration
  bosun migrate helm           # Convert to Helm format (dry-run)
  bosun migrate helm --force   # Apply Helm migration`,
}

var migrateVersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Add apiVersion and kind fields to manifests",
	Long: `Migrate manifests to add apiVersion/kind fields.

This command scans manifest directories and adds apiVersion/kind fields
to unversioned manifests. By default, it runs in dry-run mode showing
what would be changed without modifying files.

Examples:
  bosun migrate version        # Show which files need migration (dry-run)
  bosun migrate version -w     # Actually migrate files`,
	Run: runMigrateVersion,
}

var migrateHelmCmd = &cobra.Command{
	Use:   "helm",
	Short: "Convert legacy manifests to Helm-aligned format",
	Long: `Convert legacy provision-based manifests to Helm-aligned chart format.

This command will:
  1. Create charts/ directory structure
  2. Convert provisions/ to charts/templates/
  3. Convert services/*.yml to charts/<name>/Chart.yaml + values.yaml
  4. Convert stacks/*.yml to stacks/<name>/Stack.yaml

The legacy files are preserved. Use --force to overwrite existing charts.

Examples:
  bosun migrate helm           # Dry run - show what would be migrated
  bosun migrate helm --force   # Overwrite existing charts`,
	RunE: runMigrateHelm,
}

var (
	migrateWrite     bool
	migrateForce     bool
	migrateProvDir   string
	migrateServDir   string
	migrateStacksDir string
)

func init() {
	// Version subcommand flags
	migrateVersionCmd.Flags().BoolVarP(&migrateWrite, "write", "w", false, "Write changes to files (default is dry-run)")
	migrateVersionCmd.Flags().StringVar(&migrateProvDir, "provisions", "", "Provisions directory to scan")
	migrateVersionCmd.Flags().StringVar(&migrateServDir, "services", "", "Services directory to scan")
	migrateVersionCmd.Flags().StringVar(&migrateStacksDir, "stacks", "", "Stacks directory to scan")

	// Helm subcommand flags
	migrateHelmCmd.Flags().BoolVar(&migrateForce, "force", false, "Overwrite existing charts")

	// Add subcommands
	migrateCmd.AddCommand(migrateVersionCmd)
	migrateCmd.AddCommand(migrateHelmCmd)

	rootCmd.AddCommand(migrateCmd)
}

func runMigrateVersion(cmd *cobra.Command, args []string) {
	cfg, err := config.Load()
	if err != nil {
		ui.Red.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	// Use config paths if not specified
	provDir := migrateProvDir
	servDir := migrateServDir
	stacksDir := migrateStacksDir

	if provDir == "" {
		provDir = cfg.ProvisionsDir()
	}
	if servDir == "" {
		servDir = cfg.ServicesDir()
	}
	if stacksDir == "" {
		stacksDir = cfg.StacksDir()
	}

	dirs := []string{provDir, servDir, stacksDir}

	if migrateWrite {
		ui.Yellow.Println("Migrating manifests...")
	} else {
		ui.Blue.Println("Scanning for unversioned manifests (dry-run mode)...")
		fmt.Println("Use --write to apply changes")
		fmt.Println()
	}

	opts := manifest.MigrateOptions{
		DryRun:  !migrateWrite,
		Verbose: true,
	}

	results, err := manifest.MigrateDirectory(dirs, opts)
	if err != nil {
		ui.Red.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if len(results) == 0 {
		fmt.Println("No manifest files found in specified directories.")
		return
	}

	// Display results
	var migrated, skipped, errors int
	for _, r := range results {
		if r.Error != nil {
			errors++
			ui.Red.Printf("  ERROR: %s - %v\n", r.Path, r.Error)
		} else if r.Migrated {
			migrated++
			action := "would migrate"
			if migrateWrite {
				action = "migrated"
			}
			ui.Green.Printf("  %s: %s (kind: %s)\n", action, r.Path, r.Kind)
		} else if r.WasVersioned {
			skipped++
			fmt.Printf("  skipped: %s (already versioned)\n", r.Path)
		}
	}

	fmt.Println()

	// Summary
	if migrateWrite {
		ui.Green.Printf("Migrated: %d files\n", migrated)
	} else {
		ui.Blue.Printf("Would migrate: %d files\n", migrated)
	}
	fmt.Printf("Already versioned: %d files\n", skipped)
	if errors > 0 {
		ui.Red.Printf("Errors: %d files\n", errors)
	}

	if !migrateWrite && migrated > 0 {
		fmt.Println()
		ui.Yellow.Println("Run 'bosun migrate version --write' to apply changes.")
	}
}

func runMigrateHelm(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Check current format
	format := cfg.Format()
	if format == "helm" && !migrateForce {
		return fmt.Errorf("project already uses Helm-aligned format (use --force to re-migrate)")
	}

	if format == "unknown" {
		return fmt.Errorf("no manifest directory found")
	}

	isDryRun := !migrateForce && format != "helm"

	if isDryRun {
		ui.Blue.Println("Migrating from legacy to Helm-aligned format (dry-run)...")
		fmt.Println("Use --force to apply changes")
	} else {
		ui.Blue.Println("Migrating from legacy to Helm-aligned format...")
	}
	fmt.Println()

	// Create output directories
	chartsDir := cfg.ChartsDir()
	templatesDir := cfg.TemplatesDir()
	helmStacksDir := cfg.HelmStacksDir()

	if !isDryRun {
		if err := os.MkdirAll(templatesDir, 0755); err != nil {
			return fmt.Errorf("create templates directory: %w", err)
		}
		if err := os.MkdirAll(helmStacksDir, 0755); err != nil {
			return fmt.Errorf("create stacks directory: %w", err)
		}
	}

	// Migrate provisions -> templates
	if err := migrateProvisions(cfg.ProvisionsDir(), templatesDir, isDryRun); err != nil {
		return fmt.Errorf("migrate provisions: %w", err)
	}

	// Migrate services -> charts
	if err := migrateServices(cfg.ServicesDir(), chartsDir, isDryRun); err != nil {
		return fmt.Errorf("migrate services: %w", err)
	}

	// Migrate stacks
	if err := migrateStacks(cfg.StacksDir(), helmStacksDir, isDryRun); err != nil {
		return fmt.Errorf("migrate stacks: %w", err)
	}

	fmt.Println()
	if isDryRun {
		ui.Yellow.Println("Dry run complete. Use 'bosun migrate helm --force' to apply changes.")
	} else {
		ui.Green.Println("Migration complete!")
		fmt.Println()
		fmt.Println("Next steps:")
		fmt.Println("  1. Review the generated charts in charts/")
		fmt.Println("  2. Update templates to use Go template syntax: {{ .Values.port }} instead of ${port}")
		fmt.Println("  3. Test with 'bosun provision -n <stack>'")
		fmt.Println("  4. Remove legacy manifest/ directory when ready")
	}

	return nil
}

// migrateProvisions converts provisions to templates.
func migrateProvisions(provisionsDir, templatesDir string, isDryRun bool) error {
	entries, err := os.ReadDir(provisionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	ui.Blue.Println("Migrating provisions -> templates:")

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}

		srcPath := filepath.Join(provisionsDir, name)
		baseName := strings.TrimSuffix(strings.TrimSuffix(name, ".yml"), ".yaml")
		dstPath := filepath.Join(templatesDir, baseName+".yaml")

		content, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}

		// Convert ${var} to {{ .Values.var }} syntax
		converted := convertInterpolation(string(content))

		// Add apiVersion and kind if missing
		converted = ensureManifestHeader(converted, "Template")

		if isDryRun {
			fmt.Printf("  [DRY RUN] %s -> %s\n", srcPath, dstPath)
		} else {
			if _, err := os.Stat(dstPath); err == nil && !migrateForce {
				fmt.Printf("  [SKIP] %s (already exists)\n", dstPath)
				continue
			}

			if err := os.WriteFile(dstPath, []byte(converted), 0644); err != nil {
				return fmt.Errorf("write %s: %w", dstPath, err)
			}
			fmt.Printf("  %s -> %s\n", srcPath, dstPath)
		}
	}

	return nil
}

// migrateServices converts service manifests to charts.
func migrateServices(servicesDir, chartsDir string, isDryRun bool) error {
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	ui.Blue.Println("Migrating services -> charts:")

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}

		srcPath := filepath.Join(servicesDir, name)

		// Load legacy service manifest
		svcManifest, err := manifest.LoadServiceManifest(srcPath)
		if err != nil {
			ui.Yellow.Printf("  [WARN] Failed to parse %s: %v\n", name, err)
			continue
		}

		chartDir := filepath.Join(chartsDir, svcManifest.Name)
		chartPath := filepath.Join(chartDir, "Chart.yaml")
		valuesPath := filepath.Join(chartDir, "values.yaml")

		if isDryRun {
			fmt.Printf("  [DRY RUN] %s -> %s/\n", srcPath, chartDir)
			continue
		}

		if _, err := os.Stat(chartPath); err == nil && !migrateForce {
			fmt.Printf("  [SKIP] %s (chart already exists)\n", svcManifest.Name)
			continue
		}

		if err := os.MkdirAll(chartDir, 0755); err != nil {
			return fmt.Errorf("create chart directory %s: %w", chartDir, err)
		}

		// Create Chart.yaml
		chart := convertToChart(svcManifest)
		chartContent, err := yaml.Marshal(chart)
		if err != nil {
			return fmt.Errorf("marshal Chart.yaml for %s: %w", svcManifest.Name, err)
		}

		if err := os.WriteFile(chartPath, chartContent, 0644); err != nil {
			return fmt.Errorf("write Chart.yaml: %w", err)
		}

		// Create values.yaml
		if svcManifest.Config != nil && len(svcManifest.Config) > 0 {
			valuesContent, err := yaml.Marshal(svcManifest.Config)
			if err != nil {
				return fmt.Errorf("marshal values.yaml for %s: %w", svcManifest.Name, err)
			}

			if err := os.WriteFile(valuesPath, valuesContent, 0644); err != nil {
				return fmt.Errorf("write values.yaml: %w", err)
			}
		}

		fmt.Printf("  %s -> %s/\n", srcPath, chartDir)
	}

	return nil
}

// migrateStacks converts stack files to Helm-aligned format.
func migrateStacks(legacyStacksDir, helmStacksDir string, isDryRun bool) error {
	entries, err := os.ReadDir(legacyStacksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	ui.Blue.Println("Migrating stacks:")

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}

		srcPath := filepath.Join(legacyStacksDir, name)
		baseName := strings.TrimSuffix(strings.TrimSuffix(name, ".yml"), ".yaml")

		// Read and parse legacy stack
		content, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}

		var legacyStack manifest.Stack
		if err := yaml.Unmarshal(content, &legacyStack); err != nil {
			ui.Yellow.Printf("  [WARN] Failed to parse %s: %v\n", name, err)
			continue
		}

		// Create stack directory
		stackDir := filepath.Join(helmStacksDir, baseName)
		stackPath := filepath.Join(stackDir, "Stack.yaml")

		if isDryRun {
			fmt.Printf("  [DRY RUN] %s -> %s/\n", srcPath, stackDir)
			continue
		}

		if _, err := os.Stat(stackPath); err == nil && !migrateForce {
			fmt.Printf("  [SKIP] %s (stack already exists)\n", baseName)
			continue
		}

		if err := os.MkdirAll(stackDir, 0755); err != nil {
			return fmt.Errorf("create stack directory %s: %w", stackDir, err)
		}

		// Convert to Helm-aligned stack
		helmStack := convertToHelmStack(baseName, &legacyStack)
		stackContent, err := yaml.Marshal(helmStack)
		if err != nil {
			return fmt.Errorf("marshal Stack.yaml for %s: %w", baseName, err)
		}

		if err := os.WriteFile(stackPath, stackContent, 0644); err != nil {
			return fmt.Errorf("write Stack.yaml: %w", err)
		}

		fmt.Printf("  %s -> %s/\n", srcPath, stackDir)
	}

	return nil
}

// convertInterpolation converts ${var} to {{ .Values.var }} syntax.
func convertInterpolation(content string) string {
	// Replace ${name} with {{ .Chart.Name }}
	content = strings.ReplaceAll(content, "${name}", "{{ .Chart.Name }}")

	// Replace ${sidecar} with {{ .Values.sidecar }}
	content = strings.ReplaceAll(content, "${sidecar}", "{{ .Values.sidecar }}")

	// Replace other ${var} with {{ .Values.var }}
	result := content
	for {
		start := strings.Index(result, "${")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], "}")
		if end == -1 {
			break
		}

		varName := result[start+2 : start+end]
		// Skip if already converted
		if strings.HasPrefix(varName, "{") {
			result = result[:start] + "{{" + result[start+2:]
			continue
		}

		replacement := fmt.Sprintf("{{ .Values.%s }}", varName)
		result = result[:start] + replacement + result[start+end+1:]
	}

	return result
}

// ensureManifestHeader adds apiVersion and kind if missing.
func ensureManifestHeader(content, kind string) string {
	if strings.Contains(content, "apiVersion:") {
		return content
	}

	header := fmt.Sprintf("apiVersion: bosun.io/v1\nkind: %s\n", kind)
	return header + content
}

// convertToChart converts a legacy ServiceManifest to a Chart.
func convertToChart(svc *manifest.ServiceManifest) map[string]any {
	chart := map[string]any{
		"apiVersion": manifest.APIVersionV1,
		"kind":       manifest.KindChart,
		"name":       svc.Name,
	}

	// Convert provisions to templates
	if len(svc.Provisions) > 0 {
		chart["templates"] = svc.Provisions
	}

	// Convert needs/services to dependencies
	var deps []map[string]any
	for _, need := range svc.Needs {
		deps = append(deps, map[string]any{"name": need})
	}
	for name, svcConfig := range svc.Services {
		dep := map[string]any{"name": name}
		if version, ok := svcConfig["version"]; ok {
			dep["version"] = version
		}
		// Copy other config as values
		values := make(map[string]any)
		for k, v := range svcConfig {
			if k != "version" {
				values[k] = v
			}
		}
		if len(values) > 0 {
			dep["values"] = values
		}
		deps = append(deps, dep)
	}
	if len(deps) > 0 {
		chart["dependencies"] = deps
	}

	// Include compose overrides if present
	if svc.Compose != nil {
		chart["compose"] = svc.Compose
	}

	return chart
}

// convertToHelmStack converts a legacy Stack to Helm-aligned format.
func convertToHelmStack(name string, legacy *manifest.Stack) map[string]any {
	stack := map[string]any{
		"apiVersion": manifest.APIVersionV1,
		"kind":       manifest.KindStack,
		"name":       name,
	}

	// Convert include to charts
	if len(legacy.Include) > 0 {
		var charts []map[string]any
		for _, include := range legacy.Include {
			chartName := strings.TrimSuffix(strings.TrimSuffix(include, ".yml"), ".yaml")
			charts = append(charts, map[string]any{"name": chartName})
		}
		stack["charts"] = charts
	}

	// Preserve networks
	if legacy.Networks != nil {
		stack["networks"] = legacy.Networks
	}

	return stack
}
