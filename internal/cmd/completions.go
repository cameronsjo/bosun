package cmd

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/cameronsjo/bosun/internal/config"
	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/snapshot"
)

// Completion timeout to avoid hanging shell.
const completionTimeout = 2 * time.Second

// completeContainerNames returns a completion function that completes container names.
// If runningOnly is true, only running containers are returned.
func completeContainerNames(runningOnly bool) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// Don't complete if we already have an argument
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		ctx, cancel := context.WithTimeout(context.Background(), completionTimeout)
		defer cancel()

		client, err := docker.NewClient()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		defer client.Close()

		containers, err := client.ListContainers(ctx, runningOnly)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}

		var names []string
		for _, c := range containers {
			if strings.HasPrefix(c.Name, toComplete) {
				names = append(names, c.Name)
			}
		}

		return names, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeStackNames returns a completion function that completes stack/service names.
func completeStackNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Don't complete if we already have an argument
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var names []string

	// Add stacks
	if entries, err := os.ReadDir(cfg.StacksDir()); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".yml") {
				name := strings.TrimSuffix(e.Name(), ".yml")
				if strings.HasPrefix(name, toComplete) {
					names = append(names, name)
				}
			}
		}
	}

	// Add services
	if entries, err := os.ReadDir(cfg.ServicesDir()); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".yml") {
				name := strings.TrimSuffix(e.Name(), ".yml")
				if strings.HasPrefix(name, toComplete) {
					names = append(names, name)
				}
			}
		}
	}

	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeComposeServices returns a completion function that completes compose service names.
func completeComposeServices(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg, err := config.Load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	data, err := os.ReadFile(cfg.ComposeFile)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var compose composeServices
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var names []string
	for name := range compose.Services {
		if strings.HasPrefix(name, toComplete) {
			names = append(names, name)
		}
	}

	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeSnapshotNames returns a completion function that completes snapshot names.
func completeSnapshotNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg, err := config.Load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	snapshots, err := snapshot.List(cfg.ManifestDir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var names []string
	// Add "interactive" as first option
	if strings.HasPrefix("interactive", toComplete) {
		names = append(names, "interactive")
	}

	for _, snap := range snapshots {
		if strings.HasPrefix(snap.Name, toComplete) {
			names = append(names, snap.Name)
		}
	}

	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeBackupNames returns a completion function that completes backup names.
func completeBackupNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Don't complete if we already have an argument
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	backupDir := getBackupDir()
	backups, err := getBackups(backupDir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var names []string
	for _, backup := range backups {
		if strings.HasPrefix(backup.Name, toComplete) {
			names = append(names, backup.Name)
		}
	}

	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeTemplateNames returns a completion function that completes create template names.
func completeTemplateNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Don't complete if we already have a template argument
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	templates := []string{"webapp", "api", "worker", "static"}
	var names []string
	for _, t := range templates {
		if strings.HasPrefix(t, toComplete) {
			names = append(names, t)
		}
	}

	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeProvisionNames returns a completion function that completes provision template names.
func completeProvisionNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg, err := config.Load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	entries, err := os.ReadDir(cfg.ProvisionsDir())
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Remove .yml or .yaml extension
		name = strings.TrimSuffix(name, ".yml")
		name = strings.TrimSuffix(name, ".yaml")
		if strings.HasPrefix(name, toComplete) {
			names = append(names, name)
		}
	}

	return names, cobra.ShellCompDirectiveNoFileComp
}

// registerCompletions registers all dynamic completions for commands.
// This is called from init() to set up completions after all commands are defined.
func registerCompletions() {
	// Crew commands - complete container names
	crewLogsCmd.ValidArgsFunction = completeContainerNames(true)
	crewInspectCmd.ValidArgsFunction = completeContainerNames(false)
	crewRestartCmd.ValidArgsFunction = completeContainerNames(true)

	// Provision command - complete stack/service names
	provisionCmd.ValidArgsFunction = completeStackNames

	// Yacht commands - complete compose service names
	yachtUpCmd.ValidArgsFunction = completeComposeServices
	yachtRestartCmd.ValidArgsFunction = completeComposeServices

	// Emergency commands
	overboardCmd.ValidArgsFunction = completeContainerNames(false)
	restoreCmd.ValidArgsFunction = completeBackupNames

	// Create command - complete template names
	createCmd.ValidArgsFunction = completeTemplateNames

	// Register flag completions
	if err := maydayCmd.RegisterFlagCompletionFunc("rollback", completeSnapshotNames); err != nil {
		// Silently ignore - completions are optional
		_ = err
	}
}

// init registers completions after all commands are set up.
// Using init with a higher-order ensures this runs after other inits.
func init() {
	// Use a deferred registration via cobra.OnInitialize to ensure
	// all commands are registered before we add completions
	cobra.OnInitialize(registerCompletions)
}
