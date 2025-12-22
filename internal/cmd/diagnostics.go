// Package cmd provides the CLI commands for bosun.
package cmd

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/config"
	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/ui"
)

// Command timeouts
const (
	doctorCheckTimeout = 10 * time.Second
	httpClientTimeout  = 5 * time.Second
	dockerPingTimeout  = 5 * time.Second
)

// Infrastructure containers that are shown separately in status.
var infraContainers = []string{"traefik", "authelia", "gatus"}

// statusCmd shows the yacht health dashboard.
var statusCmd = &cobra.Command{
	Use:     "status",
	Aliases: []string{"bridge"},
	Short:   "Show yacht health dashboard",
	Long:    "Display crew status, infrastructure health, resource usage, and recent activity.",
	Run:     runStatus,
}

func runStatus(cmd *cobra.Command, args []string) {
	ui.Blue.Println("Yacht Status Dashboard")
	fmt.Println()

	ctx := context.Background()
	client, err := docker.NewClient()
	if err != nil {
		ui.Error("Docker not available: %v", err)
		return
	}
	defer client.Close()

	// Crew Status
	ui.Blue.Println("--- Crew Status ---")
	running, total, unhealthy, err := client.CountContainers(ctx)
	if err != nil {
		ui.Error("Failed to count containers: %v", err)
	} else {
		ui.Green.Printf("  Containers: ")
		fmt.Printf("%d running / %d total\n", running, total)

		if unhealthy > 0 {
			ui.Red.Printf("  Health: %d unhealthy\n", unhealthy)
			// Show unhealthy containers
			containers, _ := client.ListContainers(ctx, true)
			for _, ctr := range containers {
				if ctr.Health == "unhealthy" {
					fmt.Printf("    %s: %s\n", ctr.Name, ctr.Status)
				}
			}
		} else {
			ui.Green.Println("  Health: All healthy")
		}
	}

	// Infrastructure
	fmt.Println()
	ui.Blue.Println("--- Infrastructure ---")
	for _, name := range infraContainers {
		if client.IsContainerRunning(ctx, name) {
			ctr, _ := client.GetContainerByName(ctx, name)
			health := ctr.Health
			if health == "" {
				health = "running"
			}
			if health == "healthy" || health == "running" {
				ui.Green.Printf("  * %s\n", name)
			} else {
				ui.Yellow.Printf("  * %s (%s)\n", name, health)
			}
		} else {
			ui.Red.Printf("  o %s (not running)\n", name)
		}
	}

	// Applications (non-infra containers)
	fmt.Println()
	ui.Blue.Println("--- Applications ---")
	containers, _ := client.ListContainers(ctx, true)
	for _, ctr := range containers {
		// Skip infra containers
		isInfra := false
		for _, infra := range infraContainers {
			if ctr.Name == infra {
				isInfra = true
				break
			}
		}
		if isInfra || ctr.Name == "bosun" {
			continue
		}

		health := ctr.Health
		if health == "" {
			health = "running"
		}

		if health == "healthy" || health == "running" {
			ui.Green.Printf("  * %s (%s)\n", ctr.Name, ctr.Status)
		} else if health == "unhealthy" {
			ui.Red.Printf("  * %s (unhealthy)\n", ctr.Name)
		} else {
			ui.Yellow.Printf("  * %s (%s)\n", ctr.Name, health)
		}
	}

	// Resources
	fmt.Println()
	ui.Blue.Println("--- Resources ---")
	stats, err := client.GetAllContainerStats(ctx)
	if err == nil && len(stats) > 0 {
		var totalMem, totalCPU float64
		for _, s := range stats {
			totalMem += s.MemPercent
			totalCPU += s.CPUPercent
		}
		fmt.Printf("  Memory: %.1f%% used by containers\n", totalMem)
		fmt.Printf("  CPU: %.1f%% used by containers\n", totalCPU)
	} else {
		ui.Yellow.Println("  No container stats available")
	}

	// Disk usage
	diskUsage, err := client.DiskUsage(ctx)
	if err == nil {
		var volumeSize int64
		for _, v := range diskUsage.Volumes {
			volumeSize += v.UsageData.Size
		}
		fmt.Printf("  Volumes: %s\n", formatBytes(volumeSize))
	}

	// Recent Activity
	fmt.Println()
	ui.Blue.Println("--- Recent Activity ---")
	allContainers, _ := client.ListContainers(ctx, false)
	count := 0
	for _, ctr := range allContainers {
		if count >= 5 {
			break
		}
		fmt.Printf("  %s: %s\n", ctr.Name, ctr.Status)
		count++
	}
	fmt.Println()
}

// logCmd shows release history.
var logCmd = &cobra.Command{
	Use:     "log [n]",
	Aliases: []string{"ledger"},
	Short:   "Show release history",
	Long:    "Display recent manifest changes, provisions, and deploy tags.",
	Args:    cobra.MaximumNArgs(1),
	Run:     runLog,
}

func runLog(cmd *cobra.Command, args []string) {
	count := 10
	if len(args) > 0 {
		if n, err := strconv.Atoi(args[0]); err == nil && n > 0 {
			count = n
		}
	}

	cfg, err := config.Load()
	if err != nil {
		ui.Error("Failed to load config: %v", err)
		return
	}

	ui.Blue.Println("Release History")
	fmt.Println()

	// Recent Manifest Changes
	ui.Blue.Println("--- Recent Manifest Changes ---")
	gitLog := exec.Command("git", "-C", cfg.Root, "log", "--oneline",
		fmt.Sprintf("-n%d", count),
		"--format=  %C(yellow)%h%C(reset) %s %C(dim)(%cr)%C(reset)",
		"--", "manifest/")
	gitLog.Stdout = os.Stdout
	gitLog.Stderr = os.Stderr
	if err := gitLog.Run(); err != nil {
		fmt.Println("  No manifest changes found")
	}

	fmt.Println()

	// Last Provisions
	ui.Blue.Println("--- Last Provisions ---")
	outputDir := cfg.OutputDir()
	if info, err := os.Stat(outputDir); err == nil && info.IsDir() {
		showProvisionTimestamps(outputDir, cfg.ManifestDir)
	} else {
		fmt.Println("  No provisions rendered yet")
	}

	fmt.Println()

	// Deploy Tags
	ui.Blue.Println("--- Deploy Tags ---")
	tagsCmd := exec.Command("git", "-C", cfg.Root, "tag", "-l", "--sort=-creatordate")
	output, err := tagsCmd.Output()
	if err == nil && len(output) > 0 {
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for i, tag := range lines {
			if i >= 5 {
				break
			}
			// Get tag date
			dateCmd := exec.Command("git", "-C", cfg.Root, "log", "-1", "--format=%cr", tag)
			date, _ := dateCmd.Output()
			fmt.Printf("  %s (%s)\n", tag, strings.TrimSpace(string(date)))
		}
	} else {
		fmt.Println("  No deploy tags found")
		fmt.Println("  Tip: Use 'git tag v1.0.0' to mark releases")
	}

	fmt.Println()
}

// driftCmd detects config drift between manifests and running state.
var driftCmd = &cobra.Command{
	Use:     "drift",
	Aliases: []string{"compass"},
	Short:   "Detect config drift - git vs running state",
	Long:    "Compare manifest services vs running containers, detect image mismatches and orphans.",
	Run:     runDrift,
}

func runDrift(cmd *cobra.Command, args []string) {
	ui.Blue.Println("Checking for drift...")
	fmt.Println()

	ctx := context.Background()
	client, err := docker.NewClient()
	if err != nil {
		ui.Error("Docker not available: %v", err)
		os.Exit(1)
	}
	defer client.Close()

	cfg, err := config.Load()
	if err != nil {
		ui.Error("Failed to load config: %v", err)
		os.Exit(1)
	}

	hasDrift := false

	// Get running containers
	containers, err := client.ListContainers(ctx, true)
	if err != nil {
		ui.Error("Failed to list containers: %v", err)
		os.Exit(1)
	}

	runningNames := make(map[string]string) // name -> image
	for _, ctr := range containers {
		runningNames[ctr.Name] = ctr.Image
	}

	if len(runningNames) == 0 {
		ui.Yellow.Println("No containers running")
		return
	}

	ui.Blue.Println("--- Container Drift ---")

	// Check each stack's compose file
	composeDir := filepath.Join(cfg.OutputDir(), "compose")
	stackFiles, _ := filepath.Glob(filepath.Join(composeDir, "*.yml"))

	allExpected := make(map[string]bool)

	for _, stackFile := range stackFiles {
		stackName := strings.TrimSuffix(filepath.Base(stackFile), ".yml")
		expected := extractServicesFromCompose(stackFile)

		for svc, expectedImage := range expected {
			allExpected[svc] = true

			runningImage, isRunning := runningNames[svc]
			if isRunning {
				if expectedImage != "" && runningImage != expectedImage {
					ui.Yellow.Printf("  ~ %s: image drift\n", svc)
					fmt.Printf("      Expected: %s\n", expectedImage)
					fmt.Printf("      Running:  %s\n", runningImage)
					hasDrift = true
				} else {
					ui.Green.Printf("  * %s\n", svc)
				}
			} else {
				ui.Red.Printf("  x %s: not running (expected by %s)\n", svc, stackName)
				hasDrift = true
			}
		}
	}

	// Check for orphaned containers
	fmt.Println()
	ui.Blue.Println("--- Orphaned Containers ---")
	orphansFound := false
	for name := range runningNames {
		// Skip known infrastructure
		isInfra := false
		for _, infra := range append(infraContainers, "bosun") {
			if name == infra {
				isInfra = true
				break
			}
		}
		if isInfra {
			continue
		}

		if !allExpected[name] {
			ui.Yellow.Printf("  ? %s: not in any manifest\n", name)
			orphansFound = true
			hasDrift = true
		}
	}

	if !orphansFound {
		ui.Green.Println("  * No orphaned containers")
	}

	fmt.Println()
	if hasDrift {
		ui.Yellow.Println("Drift detected. Run 'bosun yacht up' to reconcile.")
		os.Exit(1)
	} else {
		ui.Green.Println("* No drift - running state matches manifests")
	}
}

// doctorCmd runs pre-flight checks.
var doctorCmd = &cobra.Command{
	Use:     "doctor",
	Aliases: []string{"checkup"},
	Short:   "Pre-flight checks - is the ship seaworthy?",
	Long:    "Run diagnostic checks for Docker, Git, SOPS, and other dependencies.",
	Run:     runDoctor,
}

func runDoctor(cmd *cobra.Command, args []string) {
	ui.Blue.Println("Running pre-flight checks...")
	fmt.Println()

	passed := 0
	failed := 0
	warned := 0

	// Check: Docker running (with timeout)
	ctx, cancel := context.WithTimeout(context.Background(), dockerPingTimeout)
	client, err := docker.NewClient()
	if err == nil {
		if err := client.Ping(ctx); err == nil {
			ui.Green.Println("  * Docker is running")
			passed++
		} else {
			ui.Red.Println("  x Docker is not running")
			failed++
		}
		client.Close()
	} else {
		ui.Red.Println("  x Docker is not running")
		failed++
	}
	cancel()

	// Check: Docker Compose v2
	composeCmd := exec.Command("docker", "compose", "version", "--short")
	if output, err := composeCmd.Output(); err == nil {
		version := strings.TrimSpace(string(output))
		ui.Green.Printf("  * Docker Compose v2 (%s)\n", version)
		passed++
	} else {
		ui.Red.Println("  x Docker Compose v2 not found")
		failed++
	}

	// Check: Git
	if _, err := exec.LookPath("git"); err == nil {
		ui.Green.Println("  * Git is installed")
		passed++
	} else {
		ui.Red.Println("  x Git not found")
		failed++
	}

	// Check: Project root
	cfg, err := config.Load()
	if err == nil {
		ui.Green.Printf("  * Project root found: %s\n", cfg.Root)
		passed++
	} else {
		ui.Yellow.Println("  ! Project root not found (run from project directory)")
		warned++
	}

	// Check: Age key
	ageKeyFile := os.Getenv("SOPS_AGE_KEY_FILE")
	if ageKeyFile == "" {
		home, _ := os.UserHomeDir()
		ageKeyFile = filepath.Join(home, ".config", "sops", "age", "keys.txt")
	}
	if _, err := os.Stat(ageKeyFile); err == nil {
		ui.Green.Printf("  * Age key found: %s\n", ageKeyFile)
		passed++
	} else {
		ui.Yellow.Printf("  ! Age key not found at %s\n", ageKeyFile)
		ui.Blue.Printf("      Run: age-keygen -o %s\n", ageKeyFile)
		warned++
	}

	// Check: SOPS
	if sopsPath, err := exec.LookPath("sops"); err == nil {
		versionCmd := exec.Command(sopsPath, "--version")
		if output, err := versionCmd.Output(); err == nil {
			version := strings.TrimSpace(string(output))
			ui.Green.Printf("  * SOPS is installed (%s)\n", version)
			passed++
		} else {
			ui.Green.Println("  * SOPS is installed")
			passed++
		}
	} else {
		ui.Yellow.Println("  ! SOPS not found (needed for secrets)")
		warned++
	}

	// Check: uv (optional now with Go)
	if _, err := exec.LookPath("uv"); err == nil {
		ui.Green.Println("  * uv is installed")
		passed++
	} else {
		ui.Yellow.Println("  ! uv not found (needed for manifest rendering)")
		warned++
	}

	// Check: Manifest directory
	if cfg != nil {
		manifestPy := filepath.Join(cfg.ManifestDir, "manifest.py")
		if _, err := os.Stat(manifestPy); err == nil {
			ui.Green.Println("  * Manifest directory found")
			passed++
		} else if _, err := os.Stat(cfg.ManifestDir); err == nil {
			ui.Green.Println("  * Manifest directory found")
			passed++
		} else {
			ui.Yellow.Println("  ! Manifest directory not found")
			warned++
		}
	}

	// Check: Webhook endpoint (with timeout)
	httpClient := &http.Client{Timeout: httpClientTimeout}
	resp, err := httpClient.Get("http://localhost:8080/health")
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			ui.Green.Println("  * Webhook endpoint responding")
			passed++
		} else {
			ui.Yellow.Println("  ! Webhook not responding (bosun container not running?)")
			warned++
		}
	} else {
		ui.Yellow.Println("  ! Webhook not responding (bosun container not running?)")
		warned++
	}

	// Summary
	fmt.Println()
	fmt.Printf("Summary: ")
	ui.Green.Printf("%d passed", passed)
	fmt.Printf(", ")
	ui.Yellow.Printf("%d warnings", warned)
	fmt.Printf(", ")
	ui.Red.Printf("%d failed\n", failed)

	if failed > 0 {
		fmt.Println()
		ui.Red.Println("Ship not seaworthy! Fix errors above.")
		os.Exit(1)
	} else if warned > 0 {
		fmt.Println()
		ui.Yellow.Println("Ship can sail, but check warnings.")
	} else {
		fmt.Println()
		ui.Green.Println("All systems go! Ready to sail.")
	}
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
	ui.Blue.Println("Linting manifests...")
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
		ui.Green.Printf("* Found %d provisions\n", len(files))
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
				ui.Green.Printf("  * %s\n", name)
			} else {
				ui.Red.Printf("  x %s\n", name)
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
				ui.Green.Printf("  * %s\n", name)
			} else {
				ui.Red.Printf("  x %s\n", name)
				errors++
			}
		}
	}

	// Check dependencies
	fmt.Println()
	fmt.Println("Validating dependencies:")
	depWarnings := checkDependencies(cfg)
	if depWarnings == 0 {
		ui.Green.Println("  * All dependencies look correct")
	}

	// Check port conflicts
	fmt.Println()
	fmt.Println("Checking for port conflicts:")
	portConflicts := checkPortConflicts(cfg)
	if portConflicts == 0 {
		ui.Green.Println("  * No port conflicts detected")
	} else {
		errors += portConflicts
	}

	// Summary
	fmt.Println()
	if errors > 0 {
		ui.Red.Printf("Found %d error(s). Fix before deploying.\n", errors)
		os.Exit(1)
	} else {
		ui.Green.Println("* All manifests valid!")
	}
}

// Helper functions

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func showProvisionTimestamps(outputDir, manifestDir string) {
	count := 0
	filepath.WalkDir(outputDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".yml") {
			return nil
		}
		if count >= 10 {
			return nil
		}
		info, _ := d.Info()
		relPath, _ := filepath.Rel(manifestDir, path)
		fmt.Printf("  %s  (%s)\n", relPath, info.ModTime().Format("2006-01-02 15:04"))
		count++
		return nil
	})
}

func extractServicesFromCompose(filename string) map[string]string {
	services := make(map[string]string)

	file, err := os.Open(filename)
	if err != nil {
		return services
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	inServices := false
	currentService := ""
	serviceIndent := 0

	serviceRegex := regexp.MustCompile(`^(\s*)([a-z][a-z0-9-]+):$`)
	imageRegex := regexp.MustCompile(`^\s*image:\s*(.+)$`)

	for scanner.Scan() {
		line := scanner.Text()

		// Check for services: section
		if strings.TrimSpace(line) == "services:" {
			inServices = true
			continue
		}

		if !inServices {
			continue
		}

		// Check for service definition (2-space indent under services)
		if matches := serviceRegex.FindStringSubmatch(line); matches != nil {
			indent := len(matches[1])
			if indent == 2 { // Service definition
				currentService = matches[2]
				serviceIndent = indent
				services[currentService] = ""
			} else if indent <= serviceIndent && currentService != "" {
				// New section at same or lower indent
				currentService = ""
			}
		}

		// Check for image: line
		if currentService != "" {
			if matches := imageRegex.FindStringSubmatch(line); matches != nil {
				services[currentService] = strings.TrimSpace(matches[1])
			}
		}
	}

	return services
}

func validateServiceFile(filename, manifestDir string) bool {
	content, err := os.ReadFile(filename)
	if err != nil {
		return false
	}

	hasName := strings.Contains(string(content), "name:")
	hasProvisions := strings.Contains(string(content), "provisions:")

	if !hasName || !hasProvisions {
		return false
	}

	// Try dry-run render if uv is available
	if _, err := exec.LookPath("uv"); err == nil {
		cmd := exec.Command("uv", "run", "manifest.py", "render", filename, "--dry-run")
		cmd.Dir = manifestDir
		if err := cmd.Run(); err != nil {
			return false
		}
	}

	return true
}

func validateStackFile(filename, manifestDir string) bool {
	content, err := os.ReadFile(filename)
	if err != nil {
		return false
	}

	hasInclude := strings.Contains(string(content), "include:")

	if !hasInclude {
		return true // Stacks without include are warnings, not errors
	}

	// Try dry-run render if uv is available
	if _, err := exec.LookPath("uv"); err == nil {
		cmd := exec.Command("uv", "run", "manifest.py", "render", filename, "--dry-run")
		cmd.Dir = manifestDir
		if err := cmd.Run(); err != nil {
			return false
		}
	}

	return true
}

func checkDependencies(cfg *config.Config) int {
	warnings := 0

	stacksDir := cfg.StacksDir()
	stackFiles, _ := filepath.Glob(filepath.Join(stacksDir, "*.yml"))

	for _, stackFile := range stackFiles {
		stackName := strings.TrimSuffix(filepath.Base(stackFile), ".yml")

		// Try to render and check
		if _, err := exec.LookPath("uv"); err != nil {
			continue
		}

		cmd := exec.Command("uv", "run", "manifest.py", "render", stackFile, "--dry-run")
		cmd.Dir = cfg.ManifestDir
		output, err := cmd.Output()
		if err != nil {
			continue
		}

		rendered := string(output)

		// Extract service names
		serviceRegex := regexp.MustCompile(`(?m)^    ([a-z][a-z0-9-]+):$`)
		services := serviceRegex.FindAllStringSubmatch(rendered, -1)

		for _, match := range services {
			svc := match[1]

			// Check: services ending with -db should have parent depending on them
			if strings.HasSuffix(svc, "-db") {
				parent := strings.TrimSuffix(svc, "-db")
				// Check if parent exists and has depends_on
				parentSection := extractSection(rendered, parent)
				if parentSection != "" && !strings.Contains(parentSection, "depends_on:") {
					ui.Yellow.Printf("  ! %s: %s may be missing depends_on: %s\n", stackName, parent, svc)
					warnings++
				}
			}

			// Check: services with traefik labels should be on proxynet
			svcSection := extractSection(rendered, svc)
			if strings.Contains(svcSection, "traefik.enable") && !strings.Contains(svcSection, "proxynet") {
				ui.Yellow.Printf("  ! %s: %s has traefik labels but may not be on proxynet\n", stackName, svc)
				warnings++
			}
		}
	}

	return warnings
}

func checkPortConflicts(cfg *config.Config) int {
	conflicts := 0
	portMap := make(map[int]string) // port -> stack name

	stacksDir := cfg.StacksDir()
	stackFiles, _ := filepath.Glob(filepath.Join(stacksDir, "*.yml"))

	portRegex := regexp.MustCompile(`(?:loadbalancer\.server\.port|"(\d+):)`)

	for _, stackFile := range stackFiles {
		stackName := strings.TrimSuffix(filepath.Base(stackFile), ".yml")

		if _, err := exec.LookPath("uv"); err != nil {
			continue
		}

		cmd := exec.Command("uv", "run", "manifest.py", "render", stackFile, "--dry-run")
		cmd.Dir = cfg.ManifestDir
		output, err := cmd.Output()
		if err != nil {
			continue
		}

		// Find all ports
		matches := portRegex.FindAllStringSubmatch(string(output), -1)
		for _, match := range matches {
			portStr := match[1]
			if portStr == "" {
				continue
			}
			port, err := strconv.Atoi(portStr)
			if err != nil || port < 1000 {
				continue
			}

			if existing, ok := portMap[port]; ok {
				ui.Yellow.Printf("  ! Port %d claimed by multiple services (%s and %s)\n", port, existing, stackName)
				conflicts++
			} else {
				portMap[port] = stackName
			}
		}
	}

	return conflicts
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
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(logCmd)
	rootCmd.AddCommand(driftCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(lintCmd)
}
