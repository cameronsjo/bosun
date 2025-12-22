// Package cmd provides the CLI commands for bosun.
package cmd

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/config"
	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/snapshot"
	"github.com/cameronsjo/bosun/internal/ui"
)

var (
	maydayList     bool
	maydayRollback string
	restoreList    bool
)

// maydayCmd handles emergency situations.
var maydayCmd = &cobra.Command{
	Use:     "mayday",
	Aliases: []string{"mutiny"},
	Short:   "Show recent errors across all crew",
	Long: `Emergency command to show recent errors from container logs.

By default, shows recent errors from all running containers.
Use --list to show available snapshots for rollback.
Use --rollback to restore a previous snapshot.`,
	Run: runMayday,
}

func runMayday(cmd *cobra.Command, args []string) {
	cfg, err := config.Load()
	if err != nil {
		// Config not required for basic error viewing
		cfg = nil
	}

	if maydayList {
		showSnapshots(cfg)
		return
	}

	if maydayRollback != "" {
		doRollback(cfg, maydayRollback)
		return
	}

	// Default: show recent errors
	showRecentErrors()
}

func showRecentErrors() {
	ui.Mayday("MAYDAY - Recent errors across all crew:")
	fmt.Println()

	ctx := context.Background()
	client, err := docker.NewClient()
	if err != nil {
		ui.Error("Docker not available: %v", err)
		return
	}
	defer client.Close()

	containers, err := client.ListContainers(ctx, true)
	if err != nil {
		ui.Error("Failed to list containers: %v", err)
		return
	}

	errorRegex := regexp.MustCompile(`(?i)(error|fatal|panic|exception)`)
	errorCount := 0
	maxErrors := 50

	for _, ctr := range containers {
		logs, err := client.GetContainerLogs(ctx, ctr.Name, 20)
		if err != nil {
			continue
		}

		lines := strings.Split(logs, "\n")
		for _, line := range lines {
			if errorCount >= maxErrors {
				break
			}
			// Clean up Docker log prefix (first 8 bytes are header)
			cleanLine := stripDockerLogPrefix(line)
			if errorRegex.MatchString(cleanLine) {
				ui.Red.Printf("[%s] %s\n", ctr.Name, cleanLine)
				errorCount++
			}
		}
	}

	if errorCount == 0 {
		ui.Green.Println("No recent errors found")
	}
}

func showSnapshots(cfg *config.Config) {
	if cfg == nil {
		ui.Yellow.Println("No snapshots found")
		fmt.Println("Snapshots are created automatically before each provision")
		return
	}

	snapshots, err := snapshot.List(cfg.ManifestDir)
	if err != nil {
		ui.Error("Failed to list snapshots: %v", err)
		return
	}

	if len(snapshots) == 0 {
		ui.Yellow.Println("No snapshots found")
		fmt.Println("Snapshots are created automatically before each provision")
		return
	}

	ui.Package("Available snapshots:")
	fmt.Println()

	for i, snap := range snapshots {
		if i >= 10 {
			remaining := len(snapshots) - 10
			fmt.Printf("  ... and %d more\n", remaining)
			break
		}

		ui.Green.Printf("  %s\n", snap.Name)
		fmt.Printf("    Created: %s\n", snap.Created.Format("2006-01-02 15:04:05"))
		fmt.Printf("    Files: %d\n", snap.FileCount)
		fmt.Println()
	}
}

func doRollback(cfg *config.Config, target string) {
	if cfg == nil {
		ui.Error("Project root not found")
		os.Exit(1)
	}

	snapshots, err := snapshot.List(cfg.ManifestDir)
	if err != nil {
		ui.Error("Failed to list snapshots: %v", err)
		os.Exit(1)
	}

	if len(snapshots) == 0 {
		ui.Error("No snapshots available")
		os.Exit(1)
	}

	// If target is empty or "interactive", prompt user
	if target == "" || target == "interactive" {
		target = promptForSnapshot(snapshots)
		if target == "" {
			fmt.Println("Aborted.")
			return
		}
	}

	// Verify target exists
	found := false
	for _, snap := range snapshots {
		if snap.Name == target {
			found = true
			break
		}
	}
	if !found {
		ui.Error("Snapshot not found: %s", target)
		os.Exit(1)
	}

	ui.Yellow.Printf("Rolling back to: %s\n", target)
	fmt.Println()

	if err := snapshot.Restore(cfg.ManifestDir, target); err != nil {
		ui.Error("Rollback failed: %v", err)
		os.Exit(1)
	}

	ui.Success("Rollback complete")
	fmt.Println()

	// Show restored files
	files, _ := snapshot.GetRestoredFiles(cfg.ManifestDir)
	if len(files) > 0 {
		fmt.Println("Restored files:")
		for _, f := range files {
			fmt.Printf("  - %s\n", f)
		}
		fmt.Println()
	}

	ui.Yellow.Println("Note: Run 'bosun yacht up' to apply restored configuration")
}

func promptForSnapshot(snapshots []snapshot.SnapshotInfo) string {
	ui.Blue.Println("Available snapshots:")
	fmt.Println()

	maxShow := 5
	if len(snapshots) < maxShow {
		maxShow = len(snapshots)
	}

	for i := 0; i < maxShow; i++ {
		snap := snapshots[i]
		fmt.Printf("  %d) %s (%s)\n", i+1, snap.Name, snap.Created.Format("2006-01-02 15:04:05"))
	}
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Select snapshot (1-5, or name): ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "" {
		return ""
	}

	// Check if input is a number
	if n, err := strconv.Atoi(input); err == nil && n >= 1 && n <= maxShow {
		return snapshots[n-1].Name
	}

	// Otherwise treat as snapshot name
	return input
}

func stripDockerLogPrefix(line string) string {
	// Docker multiplexed log format has 8-byte header
	// First byte: stream type (1=stdout, 2=stderr)
	// Bytes 2-4: reserved
	// Bytes 5-8: length (big-endian uint32)
	if len(line) >= 8 {
		// Check if this looks like a multiplexed header
		if line[0] == 1 || line[0] == 2 {
			return line[8:]
		}
	}
	return line
}

// overboardCmd forcefully removes a container.
var overboardCmd = &cobra.Command{
	Use:     "overboard [name]",
	Aliases: []string{"plank"},
	Short:   "Force remove a problematic container",
	Long:    "Forcefully remove a container by name. Use with caution!",
	Args:    cobra.ExactArgs(1),
	Run:     runOverboard,
}

func runOverboard(cmd *cobra.Command, args []string) {
	name := args[0]

	ctx := context.Background()
	client, err := docker.NewClient()
	if err != nil {
		ui.Error("Docker not available: %v", err)
		os.Exit(1)
	}
	defer client.Close()

	ui.Red.Printf("Man overboard! Removing %s...\n", name)

	if err := client.RemoveContainer(ctx, name); err != nil {
		ui.Error("Failed to remove container: %v", err)
		os.Exit(1)
	}

	ui.Success("Container %s removed", name)
}

// restoreCmd restores from a reconcile backup.
var restoreCmd = &cobra.Command{
	Use:   "restore [backup-name]",
	Short: "Restore from a reconcile backup",
	Long: `Restore infrastructure configs from a previous backup.

Use 'bosun restore --list' to see available backups.
Backups are created automatically by the reconcile command before each deployment.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRestore,
}

// BackupInfo contains information about a backup.
type BackupInfo struct {
	Name    string
	Path    string
	HasTar  bool
	ModTime string
}

func runRestore(cmd *cobra.Command, args []string) error {
	// Determine backup directory
	backupDir := getBackupDir()

	if restoreList {
		return listBackups(backupDir)
	}

	if len(args) == 0 {
		return fmt.Errorf("backup name required. Use --list to see available backups")
	}

	backupName := args[0]
	return doRestore(backupDir, backupName)
}

// getBackupDir returns the backup directory path.
func getBackupDir() string {
	// Check environment variable first
	if dir := os.Getenv("BACKUP_DIR"); dir != "" {
		return dir
	}

	// Check for project-level .bosun/backups
	cfg, err := config.Load()
	if err == nil {
		projectBackups := filepath.Join(cfg.Root, ".bosun", "backups")
		if _, err := os.Stat(projectBackups); err == nil {
			return projectBackups
		}
	}

	// Default to /app/backups (container mode)
	return "/app/backups"
}

func listBackups(backupDir string) error {
	backups, err := getBackups(backupDir)
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}

	if len(backups) == 0 {
		ui.Yellow.Println("No backups found")
		fmt.Printf("Backup directory: %s\n", backupDir)
		fmt.Println("Backups are created automatically by 'bosun reconcile' before each deployment.")
		return nil
	}

	ui.Blue.Println("Available backups:")
	fmt.Println()

	for i, backup := range backups {
		if i >= 10 {
			remaining := len(backups) - 10
			fmt.Printf("  ... and %d more\n", remaining)
			break
		}

		statusIcon := "*"
		if !backup.HasTar {
			statusIcon = "!"
		}
		ui.Green.Printf("  %s %s\n", statusIcon, backup.Name)
		fmt.Printf("      Modified: %s\n", backup.ModTime)
		if !backup.HasTar {
			ui.Yellow.Printf("      Warning: configs.tar.gz missing\n")
		}
	}

	fmt.Println()
	fmt.Printf("Backup directory: %s\n", backupDir)
	return nil
}

func getBackups(backupDir string) ([]BackupInfo, error) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var backups []BackupInfo
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "backup-") {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		backupPath := filepath.Join(backupDir, e.Name())
		tarPath := filepath.Join(backupPath, "configs.tar.gz")
		hasTar := false
		if _, err := os.Stat(tarPath); err == nil {
			hasTar = true
		}

		backups = append(backups, BackupInfo{
			Name:    e.Name(),
			Path:    backupPath,
			HasTar:  hasTar,
			ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
		})
	}

	// Sort by name descending (newest first, since names include timestamp)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Name > backups[j].Name
	})

	return backups, nil
}

func doRestore(backupDir, backupName string) error {
	backupPath := filepath.Join(backupDir, backupName)

	// Validate backup exists
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup not found: %s", backupName)
	}

	// Validate configs.tar.gz exists
	tarPath := filepath.Join(backupPath, "configs.tar.gz")
	if _, err := os.Stat(tarPath); os.IsNotExist(err) {
		return fmt.Errorf("backup incomplete: configs.tar.gz not found in %s", backupName)
	}

	ui.Yellow.Printf("Restoring from backup: %s\n", backupName)
	fmt.Println()

	// Determine target directory (appdata)
	targetDir := getAppdataDir()
	if targetDir == "" {
		return fmt.Errorf("could not determine appdata directory")
	}

	// Create staging directory
	stagingDir, err := os.MkdirTemp("", "bosun-restore-*")
	if err != nil {
		return fmt.Errorf("failed to create staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	// Extract backup to staging
	ui.Info("  Extracting backup...")
	if err := extractTarGz(tarPath, stagingDir); err != nil {
		return fmt.Errorf("failed to extract backup: %w", err)
	}

	// Show what will be restored
	var restoredFiles []string
	err = filepath.Walk(stagingDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			relPath, _ := filepath.Rel(stagingDir, path)
			restoredFiles = append(restoredFiles, relPath)
		}
		return nil
	})
	if err != nil {
		ui.Warning("Could not enumerate restored files: %v", err)
	}

	if len(restoredFiles) > 0 {
		fmt.Println("  Files to restore:")
		for _, f := range restoredFiles {
			if len(restoredFiles) > 10 && len(f) > 0 {
				fmt.Printf("    - %s\n", f)
			} else {
				fmt.Printf("    - %s\n", f)
			}
		}
		fmt.Println()
	}

	// Deploy files from staging to target
	ui.Info("  Deploying restored configs...")
	if err := deployRestoredConfigs(stagingDir, targetDir); err != nil {
		return fmt.Errorf("failed to deploy restored configs: %w", err)
	}

	// Run compose up if compose file exists
	composeFile := filepath.Join(targetDir, "compose", "core.yml")
	if _, err := os.Stat(composeFile); err == nil {
		ui.Info("  Restarting services...")
		if err := runComposeUp(composeFile); err != nil {
			ui.Warning("Could not restart services: %v", err)
			ui.Yellow.Println("  Run 'docker compose -f " + composeFile + " up -d' manually")
		}
	}

	ui.Success("Restore complete!")
	fmt.Println()
	return nil
}

func getAppdataDir() string {
	// Check environment variable
	if dir := os.Getenv("LOCAL_APPDATA"); dir != "" {
		return dir
	}

	// Check for local mount (container mode)
	if _, err := os.Stat("/mnt/appdata"); err == nil {
		return "/mnt/appdata"
	}

	// Check for remote appdata path
	if _, err := os.Stat("/mnt/user/appdata"); err == nil {
		return "/mnt/user/appdata"
	}

	return ""
}

func extractTarGz(tarPath, destDir string) error {
	file, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Sanitize path to prevent directory traversal
		target := filepath.Join(destDir, header.Name)
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid file path in archive: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			outFile, err := os.Create(target)
			if err != nil {
				return err
			}
			// Use io.CopyN to limit copy size as a security measure
			const maxFileSize = 100 * 1024 * 1024 // 100MB max per file
			if _, err := io.CopyN(outFile, tr, maxFileSize); err != nil && err != io.EOF {
				outFile.Close()
				return err
			}
			outFile.Close()
		}
	}

	return nil
}

func deployRestoredConfigs(stagingDir, targetDir string) error {
	// Use rsync for deployment
	cmd := exec.Command("rsync", "-av", stagingDir+"/", targetDir+"/")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runComposeUp(composeFile string) error {
	cmd := exec.Command("docker", "compose", "-f", composeFile, "up", "-d", "--remove-orphans")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func init() {
	maydayCmd.Flags().BoolVarP(&maydayList, "list", "l", false, "List available snapshots")
	maydayCmd.Flags().StringVarP(&maydayRollback, "rollback", "r", "", "Rollback to a snapshot (use 'interactive' for menu)")

	restoreCmd.Flags().BoolVarP(&restoreList, "list", "l", false, "List available backups")

	rootCmd.AddCommand(maydayCmd)
	rootCmd.AddCommand(overboardCmd)
	rootCmd.AddCommand(restoreCmd)
}
