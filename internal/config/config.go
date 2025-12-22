// Package config handles project discovery and configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Config holds the bosun project configuration.
type Config struct {
	// Root is the project root directory (contains bosun/ or manifest/).
	Root string

	// ManifestDir is the path to the manifest directory.
	ManifestDir string

	// ComposeFile is the path to the main docker-compose.yml.
	ComposeFile string

	// SnapshotsDir is the path to the snapshots directory.
	SnapshotsDir string
}

// FindRoot searches upward from the current directory to find the project root.
// The project root is identified by the presence of a bosun/ or manifest/ directory.
func FindRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	for dir != "/" {
		// Check for bosun directory with docker-compose.yml
		bosunDir := filepath.Join(dir, "bosun")
		if info, err := os.Stat(bosunDir); err == nil && info.IsDir() {
			composeFile := filepath.Join(bosunDir, "docker-compose.yml")
			if _, err := os.Stat(composeFile); err == nil {
				return dir, nil
			}
		}

		// Check for manifest directory
		manifestDir := filepath.Join(dir, "manifest")
		if info, err := os.Stat(manifestDir); err == nil && info.IsDir() {
			return dir, nil
		}

		// Move up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("project root not found (no bosun/ or manifest/ directory)")
}

// Load finds the project root and returns a Config.
func Load() (*Config, error) {
	root, err := FindRoot()
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Root:         root,
		ManifestDir:  filepath.Join(root, "manifest"),
		ComposeFile:  filepath.Join(root, "bosun", "docker-compose.yml"),
		SnapshotsDir: filepath.Join(root, "manifest", ".bosun", "snapshots"),
	}

	return cfg, nil
}

// ProvisionsDir returns the path to the provisions directory.
func (c *Config) ProvisionsDir() string {
	return filepath.Join(c.ManifestDir, "provisions")
}

// ServicesDir returns the path to the services directory.
func (c *Config) ServicesDir() string {
	return filepath.Join(c.ManifestDir, "services")
}

// StacksDir returns the path to the stacks directory.
func (c *Config) StacksDir() string {
	return filepath.Join(c.ManifestDir, "stacks")
}

// OutputDir returns the path to the output directory.
func (c *Config) OutputDir() string {
	return filepath.Join(c.ManifestDir, "output")
}
