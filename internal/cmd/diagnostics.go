// Package cmd provides the CLI commands for bosun.
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/config"
	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/ui"
)

// Command timeouts and display limits.
const (
	doctorCheckTimeout = 10 * time.Second
	httpClientTimeout  = 5 * time.Second
	dockerPingTimeout  = 5 * time.Second
	gitCommandTimeout  = 10 * time.Second

	// Display limits for diagnostics commands.
	// MaxRecentActivityDisplay is the maximum number of recent containers to show.
	MaxRecentActivityDisplay = 5
	// MaxDeployTagsDisplay is the maximum number of deploy tags to show.
	MaxDeployTagsDisplay = 5
	// MaxProvisionTimestamps is the maximum number of provision timestamps to show.
	MaxProvisionTimestamps = 10
	// DefaultLogHistoryCount is the default number of log history entries to show.
	DefaultLogHistoryCount = 10
)

// CheckResult holds the results of a diagnostic check.
type CheckResult struct {
	Passed int
	Failed int
	Warned int
}

// Add combines two CheckResults.
func (r *CheckResult) Add(other CheckResult) {
	r.Passed += other.Passed
	r.Failed += other.Failed
	r.Warned += other.Warned
}

// statusCmd shows the yacht health dashboard.
var statusCmd = &cobra.Command{
	Use:     "status",
	Aliases: []string{"bridge"},
	Short:   "Show yacht health dashboard",
	Long:    "Display crew status, infrastructure health, resource usage, and recent activity.",
	Run:     runStatus,
}

func runStatus(cmd *cobra.Command, args []string) {
	_, _ = ui.Blue.Println("Yacht Status Dashboard")
	fmt.Println()

	// Load config for infrastructure container list
	cfg, cfgErr := config.Load()
	var infraContainers []string
	if cfgErr == nil {
		infraContainers = cfg.InfraContainers()
	} else {
		// Fall back to defaults if config can't be loaded
		infraContainers = []string{"traefik", "authelia", "gatus"}
	}

	err := withDockerClient(func(ctx context.Context, client *docker.Client) error {
		// Crew Status
		_, _ = ui.Blue.Println("--- Crew Status ---")
		running, total, unhealthy, err := client.CountContainers(ctx)
		if err != nil {
			ui.Error("Failed to count containers: %v", err)
		} else {
			_, _ = ui.Green.Printf("  Containers: ")
			fmt.Printf("%d running / %d total\n", running, total)

			if unhealthy > 0 {
				_, _ = ui.Red.Printf("  Health: %d unhealthy\n", unhealthy)
				// Show unhealthy containers
				containers, _ := client.ListContainers(ctx, true)
				for _, ctr := range containers {
					if ctr.Health == "unhealthy" {
						fmt.Printf("    %s: %s\n", ctr.Name, ctr.Status)
					}
				}
			} else {
				_, _ = ui.Green.Println("  Health: All healthy")
			}
		}

		// Infrastructure
		fmt.Println()
		_, _ = ui.Blue.Println("--- Infrastructure ---")
		for _, name := range infraContainers {
			if client.IsContainerRunning(ctx, name) {
				ctr, _ := client.GetContainerByName(ctx, name)
				health := ctr.Health
				if health == "" {
					health = "running"
				}
				if health == "healthy" || health == "running" {
					_, _ = ui.Green.Printf("  * %s\n", name)
				} else {
					_, _ = ui.Yellow.Printf("  * %s (%s)\n", name, health)
				}
			} else {
				_, _ = ui.Red.Printf("  o %s (not running)\n", name)
			}
		}

		// Applications (non-infra containers)
		fmt.Println()
		_, _ = ui.Blue.Println("--- Applications ---")
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

			if health == "healthy" || health == "running" { //nolint:staticcheck
				_, _ = ui.Green.Printf("  * %s (%s)\n", ctr.Name, ctr.Status)
			} else if health == "unhealthy" {
				_, _ = ui.Red.Printf("  * %s (unhealthy)\n", ctr.Name)
			} else {
				_, _ = ui.Yellow.Printf("  * %s (%s)\n", ctr.Name, health)
			}
		}

		// Resources
		fmt.Println()
		_, _ = ui.Blue.Println("--- Resources ---")
		stats, err := client.GetAllContainerStats(ctx)
		if err == nil && len(stats) > 0 {
			var totalMem, totalCPU float64
			for _, s := range stats {
				totalMem += s.MemPercent
				totalCPU += s.CPUPercent
			}
			fmt.Printf("  Memory: %.1f%% used by containers\n", totalMem)
			// Note: CPU can exceed 100% on multi-core systems (sum of all cores' usage)
			fmt.Printf("  CPU: %.1f%% used by containers\n", totalCPU)
		} else {
			_, _ = ui.Yellow.Println("  No container stats available")
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
		_, _ = ui.Blue.Println("--- Recent Activity ---")
		allContainers, _ := client.ListContainers(ctx, false)
		count := 0
		for _, ctr := range allContainers {
			if count >= MaxRecentActivityDisplay {
				break
			}
			fmt.Printf("  %s: %s\n", ctr.Name, ctr.Status)
			count++
		}
		fmt.Println()
		return nil
	})

	if err != nil {
		ui.Error("Docker not available: %v", err)
	}
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
	count := DefaultLogHistoryCount
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

	_, _ = ui.Blue.Println("Release History")
	fmt.Println()

	// Create context with timeout for git commands
	ctx, cancel := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer cancel()

	// Recent Manifest Changes
	_, _ = ui.Blue.Println("--- Recent Manifest Changes ---")
	gitLog := exec.CommandContext(ctx, "git", "-C", cfg.Root, "log", "--oneline",
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
	_, _ = ui.Blue.Println("--- Last Provisions ---")
	outputDir := cfg.OutputDir()
	if info, err := os.Stat(outputDir); err == nil && info.IsDir() {
		showProvisionTimestamps(outputDir, cfg.ManifestDir)
	} else {
		fmt.Println("  No provisions rendered yet")
	}

	fmt.Println()

	// Deploy Tags
	_, _ = ui.Blue.Println("--- Deploy Tags ---")
	tagsCmd := exec.CommandContext(ctx, "git", "-C", cfg.Root, "tag", "-l", "--sort=-creatordate")
	output, err := tagsCmd.Output()
	if err == nil && len(output) > 0 {
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for i, tag := range lines {
			if i >= MaxDeployTagsDisplay {
				break
			}
			// Get tag date
			dateCmd := exec.CommandContext(ctx, "git", "-C", cfg.Root, "log", "-1", "--format=%cr", tag)
			date, _ := dateCmd.Output()
			fmt.Printf("  %s (%s)\n", tag, strings.TrimSpace(string(date)))
		}
	} else {
		fmt.Println("  No deploy tags found")
		fmt.Println("  Tip: Use 'git tag v1.0.0' to mark releases")
	}

	fmt.Println()
}

// Helper functions

func formatBytes(bytes int64) string {
	const unit = 1024
	// Guard against negative values - display as N/A instead of negative size
	if bytes < 0 {
		return "N/A"
	}
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
	_ = filepath.WalkDir(outputDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".yml") {
			return nil
		}
		if count >= MaxProvisionTimestamps {
			return nil
		}
		info, _ := d.Info()
		relPath, _ := filepath.Rel(manifestDir, path)
		fmt.Printf("  %s  (%s)\n", relPath, info.ModTime().Format("2006-01-02 15:04"))
		count++
		return nil
	})
}

func init() {
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(logCmd)
}
