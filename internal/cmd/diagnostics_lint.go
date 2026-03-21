// Package cmd provides the CLI commands for bosun.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/cameronsjo/bosun/internal/config"
	"github.com/cameronsjo/bosun/internal/ui"
)

// serviceNameRegex matches docker-compose service names
var serviceNameRegex = regexp.MustCompile(`(?m)^    ([a-z][a-z0-9-]+):$`)

// ComposeFile represents a Docker Compose file structure for YAML parsing.
type ComposeFile struct {
	Services map[string]struct {
		Image string `yaml:"image"`
	} `yaml:"services"`
}

// lintCmd validates manifests before deploy.
var lintCmd = &cobra.Command{
	Use:     "lint [target]",
	Aliases: []string{"inspect"},
	Short:   "Validate all manifests before deploy",
	Long:    "Validate provisions, services, dependencies, and port conflicts.",
	Args:    cobra.MaximumNArgs(1),
	Run:     runLint,
}

func runLint(cmd *cobra.Command, args []string) {
	_, _ = ui.Blue.Println("Linting manifests...")
	fmt.Println()

	cfg, err := config.Load()
	if err != nil {
		ui.Error("Failed to load config: %v", err)
		os.Exit(1)
	}

	if _, err := os.Stat(cfg.ManifestDir); os.IsNotExist(err) {
		ui.Error("Manifest directory not found")
		os.Exit(1)
	}

	errors := 0

	// Check provisions exist
	provisionsDir := cfg.ProvisionsDir()
	if _, err := os.Stat(provisionsDir); os.IsNotExist(err) {
		ui.Error("Provisions directory not found")
		errors++
	} else {
		files, _ := filepath.Glob(filepath.Join(provisionsDir, "*.yml"))
		_, _ = ui.Green.Printf("* Found %d provisions\n", len(files))
	}

	// Validate services
	servicesDir := cfg.ServicesDir()
	if _, err := os.Stat(servicesDir); err == nil {
		fmt.Println()
		fmt.Println("Validating services:")
		serviceFiles, _ := filepath.Glob(filepath.Join(servicesDir, "*.yml"))

		for _, serviceFile := range serviceFiles {
			name := filepath.Base(serviceFile)
			if validateServiceFile(serviceFile, cfg.ManifestDir) {
				_, _ = ui.Green.Printf("  * %s\n", name)
			} else {
				_, _ = ui.Red.Printf("  x %s\n", name)
				errors++
			}
		}
	}

	// Validate stacks
	stacksDir := cfg.StacksDir()
	if _, err := os.Stat(stacksDir); err == nil {
		fmt.Println()
		fmt.Println("Validating stacks:")
		stackFiles, _ := filepath.Glob(filepath.Join(stacksDir, "*.yml"))

		for _, stackFile := range stackFiles {
			name := filepath.Base(stackFile)
			if validateStackFile(stackFile, cfg.ManifestDir) {
				_, _ = ui.Green.Printf("  * %s\n", name)
			} else {
				_, _ = ui.Red.Printf("  x %s\n", name)
				errors++
			}
		}
	}

	// Check dependencies
	fmt.Println()
	fmt.Println("Validating dependencies:")
	depWarnings := checkDependencies(cfg)
	if depWarnings == 0 {
		_, _ = ui.Green.Println("  * All dependencies look correct")
	}

	// Check port conflicts
	fmt.Println()
	fmt.Println("Checking for port conflicts:")
	portConflicts := checkPortConflicts(cfg)
	if portConflicts == 0 {
		_, _ = ui.Green.Println("  * No port conflicts detected")
	} else {
		errors += portConflicts
	}

	// Check for dependency cycles
	fmt.Println()
	fmt.Println("Checking for dependency cycles:")
	cycles := checkDependencyCycles(cfg)
	if len(cycles) == 0 {
		_, _ = ui.Green.Println("  * No dependency cycles detected")
	} else {
		for _, cycle := range cycles {
			_, _ = ui.Red.Printf("  x Cycle detected: %s\n", cycle)
		}
		errors += len(cycles)
	}

	// Summary
	fmt.Println()
	if errors > 0 {
		_, _ = ui.Red.Printf("Found %d error(s). Fix before deploying.\n", errors)
		os.Exit(1)
	} else {
		_, _ = ui.Green.Println("* All manifests valid!")
	}
}

func validateServiceFile(filename, _ string) bool {
	content, err := os.ReadFile(filename)
	if err != nil {
		return false
	}

	var doc struct {
		Name       string `yaml:"name"`
		Provisions []any  `yaml:"provisions"`
	}
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return false
	}

	return doc.Name != "" && len(doc.Provisions) > 0
}

func validateStackFile(filename, _ string) bool {
	content, err := os.ReadFile(filename)
	if err != nil {
		return false
	}

	// Validate the file contains parseable YAML (or is empty, which is a warning not an error)
	var doc map[string]any
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return false
	}

	return true
}

func checkDependencies(cfg *config.Config) int {
	warnings := 0

	// Check rendered compose files in output directory
	composeDir := filepath.Join(cfg.OutputDir(), "compose")
	composeFiles, _ := filepath.Glob(filepath.Join(composeDir, "*.yml"))

	for _, composeFile := range composeFiles {
		stackName := strings.TrimSuffix(filepath.Base(composeFile), ".yml")

		rendered, err := os.ReadFile(composeFile)
		if err != nil {
			continue
		}

		content := string(rendered)

		// Extract service names using package-level regex
		services := serviceNameRegex.FindAllStringSubmatch(content, -1)

		for _, match := range services {
			svc := match[1]

			// Check: services ending with -db should have parent depending on them
			if strings.HasSuffix(svc, "-db") {
				parent := strings.TrimSuffix(svc, "-db")
				// Check if parent exists and has depends_on
				parentSection := extractSection(content, parent)
				if parentSection != "" && !strings.Contains(parentSection, "depends_on:") {
					_, _ = ui.Yellow.Printf("  ! %s: %s may be missing depends_on: %s\n", stackName, parent, svc)
					warnings++
				}
			}

			// Check: services with traefik labels should be on proxynet
			svcSection := extractSection(content, svc)
			if strings.Contains(svcSection, "traefik.enable") && !strings.Contains(svcSection, "proxynet") {
				_, _ = ui.Yellow.Printf("  ! %s: %s has traefik labels but may not be on proxynet\n", stackName, svc)
				warnings++
			}
		}
	}

	return warnings
}

func checkPortConflicts(cfg *config.Config) int {
	conflicts := 0
	portMap := make(map[int]string) // port -> service@stack

	// Check rendered compose files in output directory (most accurate)
	composeDir := filepath.Join(cfg.OutputDir(), "compose")
	composeFiles, _ := filepath.Glob(filepath.Join(composeDir, "*.yml"))

	for _, composeFile := range composeFiles {
		stackName := strings.TrimSuffix(filepath.Base(composeFile), ".yml")
		servicePorts := extractPorts(composeFile)

		for port, serviceName := range servicePorts {
			identifier := serviceName + "@" + stackName
			if existing, ok := portMap[port]; ok && existing != identifier {
				_, _ = ui.Yellow.Printf("  ! Port %d claimed by multiple services (%s and %s)\n", port, existing, identifier)
				conflicts++
			} else {
				portMap[port] = identifier
			}
		}
	}

	// If no rendered files, fall back to dry-run rendering of stacks
	if len(composeFiles) == 0 {
		conflicts += checkPortConflictsFromStacks(cfg, portMap)
	}

	return conflicts
}

// checkPortConflictsFromStacks checks port conflicts from raw stack files.
// This is a fallback when no rendered compose files exist.
// Without rendered files, we cannot reliably detect port conflicts.
func checkPortConflictsFromStacks(_ *config.Config, _ map[int]string) int {
	// Port conflict detection requires rendered compose files.
	// Run 'bosun provision' first to generate them.
	return 0
}

// checkDependencyCycles checks rendered compose files for dependency cycles.
func checkDependencyCycles(cfg *config.Config) []string {
	var allCycles []string

	// Check rendered compose files in output directory
	composeDir := filepath.Join(cfg.OutputDir(), "compose")
	composeFiles, _ := filepath.Glob(filepath.Join(composeDir, "*.yml"))

	for _, composeFile := range composeFiles {
		depGraph := extractDependencyGraph(composeFile)
		if len(depGraph) == 0 {
			continue
		}

		cycles := detectCycles(depGraph)
		allCycles = append(allCycles, cycles...)
	}

	return allCycles
}

func extractSection(content, serviceName string) string {
	lines := strings.Split(content, "\n")
	inSection := false
	var section strings.Builder

	for _, line := range lines {
		if strings.HasPrefix(line, "    "+serviceName+":") {
			inSection = true
			section.WriteString(line + "\n")
			continue
		}
		if inSection {
			if strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "      ") {
				// New service at same level
				break
			}
			section.WriteString(line + "\n")
		}
	}

	return section.String()
}

func init() {
	rootCmd.AddCommand(lintCmd)
}
