package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// MigrationResult represents the result of migrating a single file.
type MigrationResult struct {
	// Path is the file path that was processed.
	Path string

	// Kind is the detected or inferred manifest kind.
	Kind string

	// WasVersioned indicates if the file already had apiVersion/kind.
	WasVersioned bool

	// Migrated indicates if the file was migrated (or would be in dry-run).
	Migrated bool

	// Error contains any error that occurred during migration.
	Error error
}

// MigrateOptions configures the migration behavior.
type MigrateOptions struct {
	// DryRun if true, don't write changes to disk.
	DryRun bool

	// Verbose if true, include more detail in results.
	Verbose bool
}

// MigrateToV1 adds apiVersion and kind fields to an unversioned manifest.
// It detects the manifest type based on content and adds appropriate headers.
// Returns the migrated content and the detected kind.
func MigrateToV1(data []byte) ([]byte, string, error) {
	// Check if already versioned
	versioned, err := IsVersioned(data)
	if err != nil {
		return nil, "", fmt.Errorf("check versioned: %w", err)
	}

	if versioned {
		// Already versioned, return as-is
		kind, err := GetManifestKind(data)
		if err != nil {
			return nil, "", err
		}
		return data, kind, nil
	}

	// Detect kind from content
	kind, err := detectKind(data)
	if err != nil {
		return nil, "", fmt.Errorf("detect kind: %w", err)
	}

	// Add version header
	header := fmt.Sprintf("apiVersion: %s\nkind: %s\n", APIVersionV1, kind)
	migrated := header + string(data)

	return []byte(migrated), kind, nil
}

// detectKind infers the manifest kind from its content.
func detectKind(data []byte) (string, error) {
	var content map[string]any
	if err := yaml.Unmarshal(data, &content); err != nil {
		return "", fmt.Errorf("parse content: %w", err)
	}

	// Stack: has 'include' key (list of service files)
	if _, hasInclude := content["include"]; hasInclude {
		return KindStack, nil
	}

	// Service: has 'name' + ('provisions' or 'needs' or 'services' or 'type')
	if _, hasName := content["name"]; hasName {
		if _, hasProvisions := content["provisions"]; hasProvisions {
			return KindService, nil
		}
		if _, hasNeeds := content["needs"]; hasNeeds {
			return KindService, nil
		}
		if _, hasServices := content["services"]; hasServices {
			return KindService, nil
		}
		if _, hasType := content["type"]; hasType {
			return KindService, nil
		}
	}

	// Provision: has 'compose', 'traefik', 'gatus', or 'includes' (provision inheritance)
	if _, hasCompose := content["compose"]; hasCompose {
		return KindProvision, nil
	}
	if _, hasTraefik := content["traefik"]; hasTraefik {
		return KindProvision, nil
	}
	if _, hasGatus := content["gatus"]; hasGatus {
		return KindProvision, nil
	}
	if _, hasIncludes := content["includes"]; hasIncludes {
		return KindProvision, nil
	}

	// Default to Provision for empty or unrecognized content
	return KindProvision, nil
}

// MigrateFile migrates a single manifest file to v1.
func MigrateFile(path string, opts MigrateOptions) (*MigrationResult, error) {
	result := &MigrationResult{Path: path}

	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		result.Error = fmt.Errorf("read file: %w", err)
		return result, result.Error
	}

	// Check if already versioned
	versioned, err := IsVersioned(data)
	if err != nil {
		result.Error = fmt.Errorf("check versioned: %w", err)
		return result, result.Error
	}

	result.WasVersioned = versioned
	if versioned {
		kind, _ := GetManifestKind(data)
		result.Kind = kind
		result.Migrated = false
		return result, nil
	}

	// Migrate
	migrated, kind, err := MigrateToV1(data)
	if err != nil {
		result.Error = fmt.Errorf("migrate: %w", err)
		return result, result.Error
	}

	result.Kind = kind
	result.Migrated = true

	// Write if not dry-run
	if !opts.DryRun {
		if err := os.WriteFile(path, migrated, 0644); err != nil {
			result.Error = fmt.Errorf("write file: %w", err)
			return result, result.Error
		}
	}

	return result, nil
}

// MigrateDirectory migrates all manifest files in a directory.
// Looks for .yml and .yaml files in the specified directories.
func MigrateDirectory(dirs []string, opts MigrateOptions) ([]*MigrationResult, error) {
	var results []*MigrationResult

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue // Skip non-existent directories
			}
			return results, fmt.Errorf("read directory %s: %w", dir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			name := entry.Name()
			ext := filepath.Ext(name)
			if ext != ".yml" && ext != ".yaml" {
				continue
			}

			path := filepath.Join(dir, name)
			result, _ := MigrateFile(path, opts)
			results = append(results, result)
		}
	}

	return results, nil
}

// ScanUnversioned finds all unversioned manifest files in the given directories.
func ScanUnversioned(dirs []string) ([]*MigrationResult, error) {
	var results []*MigrationResult

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return results, fmt.Errorf("read directory %s: %w", dir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			name := entry.Name()
			ext := filepath.Ext(name)
			if ext != ".yml" && ext != ".yaml" {
				continue
			}

			path := filepath.Join(dir, name)
			data, err := os.ReadFile(path)
			if err != nil {
				results = append(results, &MigrationResult{
					Path:  path,
					Error: err,
				})
				continue
			}

			versioned, err := IsVersioned(data)
			if err != nil {
				results = append(results, &MigrationResult{
					Path:  path,
					Error: err,
				})
				continue
			}

			if !versioned {
				kind, _ := detectKind(data)
				results = append(results, &MigrationResult{
					Path:         path,
					Kind:         kind,
					WasVersioned: false,
					Migrated:     false,
				})
			}
		}
	}

	return results, nil
}

// FormatMigrationSummary creates a human-readable summary of migration results.
func FormatMigrationSummary(results []*MigrationResult, dryRun bool) string {
	var sb strings.Builder

	var migrated, skipped, errors int
	for _, r := range results {
		if r.Error != nil {
			errors++
		} else if r.Migrated {
			migrated++
		} else {
			skipped++
		}
	}

	action := "Migrated"
	if dryRun {
		action = "Would migrate"
	}

	sb.WriteString(fmt.Sprintf("\n%s: %d files\n", action, migrated))
	sb.WriteString(fmt.Sprintf("Already versioned: %d files\n", skipped))
	if errors > 0 {
		sb.WriteString(fmt.Sprintf("Errors: %d files\n", errors))
	}

	if migrated > 0 {
		sb.WriteString("\nFiles requiring migration:\n")
		for _, r := range results {
			if r.Migrated {
				sb.WriteString(fmt.Sprintf("  - %s (detected: %s)\n", r.Path, r.Kind))
			}
		}
	}

	if errors > 0 {
		sb.WriteString("\nFiles with errors:\n")
		for _, r := range results {
			if r.Error != nil {
				sb.WriteString(fmt.Sprintf("  - %s: %v\n", r.Path, r.Error))
			}
		}
	}

	return sb.String()
}
