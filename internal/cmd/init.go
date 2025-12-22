package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/cameronsjo/bosun/internal/ui"
)

// initCmd represents the init command.
var initCmd = &cobra.Command{
	Use:     "init [directory]",
	Aliases: []string{"christen"},
	Short:   "Christen your yacht (interactive setup wizard)",
	Long: `Initialize a new bosun project with the required directory structure,
encryption keys, and starter files.

This creates:
  - bosun/             Webhook receiver compose file
  - manifest/          Service definitions
    - provisions/      Reusable templates
    - services/        Individual services
    - stacks/          Service groups
  - .sops.yaml         SOPS encryption config
  - .gitignore         Git ignore file
  - README.md          Project documentation

If no directory is specified, the current directory is used.

Use --yes to skip all interactive prompts (useful for non-TTY environments).`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInit,
}

var initYes bool

func runInit(cmd *cobra.Command, args []string) error {
	targetDir := "."
	if len(args) > 0 {
		targetDir = args[0]
	}

	// Get absolute path
	absDir, err := filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	targetDir = absDir

	ui.Anchor("Christening your yacht...")
	fmt.Println()

	// Check if already initialized
	bosunDir := filepath.Join(targetDir, "bosun")
	composeFile := filepath.Join(bosunDir, "docker-compose.yml")
	if _, err := os.Stat(bosunDir); err == nil {
		if _, err := os.Stat(composeFile); err == nil {
			ui.Warning("This directory already has a bosun project.")
			if !initYes {
				response, err := promptYesNo("Reinitialize? This won't overwrite existing files.")
				if err != nil {
					return err
				}
				if !response {
					fmt.Println("Aborted.")
					return nil
				}
			}
		}
	}

	// Step 1: Create directory structure
	ui.Info("Creating project structure...")
	dirs := []string{
		filepath.Join(targetDir, "bosun", "scripts"),
		filepath.Join(targetDir, "manifest", "provisions"),
		filepath.Join(targetDir, "manifest", "services"),
		filepath.Join(targetDir, "manifest", "stacks"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	ui.Success("Created directories")

	// Step 2: Check/setup age key
	ui.Info("Setting up encryption...")
	agePubKey, err := setupAgeKey()
	if err != nil {
		ui.Warning("Age setup: %v", err)
		agePubKey = "AGE-PUBLIC-KEY-REPLACE-ME"
	}

	// Step 3: Create .sops.yaml if not exists
	sopsFile := filepath.Join(targetDir, ".sops.yaml")
	if _, err := os.Stat(sopsFile); os.IsNotExist(err) {
		sopsContent := fmt.Sprintf(`creation_rules:
  - path_regex: .*\.sops\.yaml$
    age: %s
`, agePubKey)
		if err := os.WriteFile(sopsFile, []byte(sopsContent), 0644); err != nil {
			return fmt.Errorf("create .sops.yaml: %w", err)
		}
		ui.Success("Created .sops.yaml")
	} else {
		ui.Warning(".sops.yaml already exists, skipping")
	}

	// Step 4: Initialize git if needed
	ui.Info("Setting up version control...")
	gitDir := filepath.Join(targetDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		if _, err := exec.LookPath("git"); err == nil {
			gitInit := exec.Command("git", "init", targetDir)
			gitInit.Stdout = os.Stdout
			gitInit.Stderr = os.Stderr
			if err := gitInit.Run(); err != nil {
				ui.Warning("Git init failed: %v", err)
			} else {
				ui.Success("Initialized git repository")
			}
		} else {
			ui.Warning("Git not found, skipping")
		}
	} else {
		ui.Success("Git repository exists")
	}

	// Step 5: Create starter files
	ui.Info("Creating starter files...")

	// bosun docker-compose.yml
	if err := createFileIfNotExists(composeFile, starterComposeYML); err != nil {
		return fmt.Errorf("create compose file: %w", err)
	}

	// manifest/pyproject.toml
	pyprojectFile := filepath.Join(targetDir, "manifest", "pyproject.toml")
	if err := createFileIfNotExists(pyprojectFile, starterPyprojectTOML); err != nil {
		return fmt.Errorf("create pyproject.toml: %w", err)
	}

	// Example service manifest
	exampleService := filepath.Join(targetDir, "manifest", "services", "example.yml")
	if err := createFileIfNotExists(exampleService, starterExampleService); err != nil {
		return fmt.Errorf("create example service: %w", err)
	}

	// .gitignore
	gitignoreFile := filepath.Join(targetDir, ".gitignore")
	if err := createFileIfNotExists(gitignoreFile, starterGitignore); err != nil {
		return fmt.Errorf("create .gitignore: %w", err)
	}

	// README.md
	readmeFile := filepath.Join(targetDir, "README.md")
	if err := createFileIfNotExists(readmeFile, starterReadme); err != nil {
		return fmt.Errorf("create README.md: %w", err)
	}

	// Summary
	fmt.Println()
	ui.Anchor("Yacht christened! Here's your checklist:")
	fmt.Println()
	fmt.Println("  1. Review .sops.yaml and update the age public key if needed")
	fmt.Println("  2. Edit manifest/services/example.yml or create your own")
	fmt.Println("  3. Run 'bosun doctor' to verify your setup")
	fmt.Println("  4. Run 'bosun yacht up' to start the webhook receiver")
	fmt.Println("  5. Push to git to deploy!")
	fmt.Println()
	ui.Info("Run 'bosun --help' for all commands.")

	return nil
}

// setupAgeKey checks for an existing age key or generates a new one.
func setupAgeKey() (string, error) {
	// Get age key file path
	ageKeyFile := os.Getenv("SOPS_AGE_KEY_FILE")
	if ageKeyFile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("get home directory: %w", err)
		}
		ageKeyFile = filepath.Join(home, ".config", "sops", "age", "keys.txt")
	}

	// Check if key exists
	if _, err := os.Stat(ageKeyFile); err == nil {
		ui.Success("Age key found: %s", ageKeyFile)
		return extractAgePublicKey(ageKeyFile)
	}

	// Check if age-keygen is available
	if _, err := exec.LookPath("age-keygen"); err != nil {
		ui.Error("age-keygen not found. Install age first:")
		fmt.Println("      brew install age  # macOS")
		fmt.Println("      apt install age   # Debian/Ubuntu")
		return "", fmt.Errorf("age-keygen not found")
	}

	// Generate new key
	ui.Warning("No age key found. Generating...")
	keyDir := filepath.Dir(ageKeyFile)
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		return "", fmt.Errorf("create key directory: %w", err)
	}

	// Run age-keygen
	keygen := exec.Command("age-keygen", "-o", ageKeyFile)
	output, err := keygen.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("generate age key: %w", err)
	}

	// Set secure permissions
	if err := os.Chmod(ageKeyFile, 0600); err != nil {
		return "", fmt.Errorf("set key permissions: %w", err)
	}

	ui.Success("Generated age key: %s", ageKeyFile)

	// Extract public key from output
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "Public key:") {
			pubKey := strings.TrimSpace(strings.TrimPrefix(line, "Public key:"))
			return pubKey, nil
		}
	}

	// Fall back to extracting from file
	return extractAgePublicKey(ageKeyFile)
}

// extractAgePublicKey reads the public key from an age key file.
func extractAgePublicKey(keyFile string) (string, error) {
	file, err := os.Open(keyFile)
	if err != nil {
		return "", fmt.Errorf("open key file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		// Look for comment with public key
		if strings.Contains(line, "public key:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1]), nil
			}
		}
	}

	// Try using age-keygen -y to derive public key
	if _, err := exec.LookPath("age-keygen"); err == nil {
		deriveCmd := exec.Command("age-keygen", "-y", keyFile)
		output, err := deriveCmd.Output()
		if err == nil {
			return strings.TrimSpace(string(output)), nil
		}
	}

	return "", fmt.Errorf("could not extract public key from %s", keyFile)
}

// isTerminal checks if stdin is a TTY.
func isTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// promptYesNo asks the user a yes/no question.
// Returns error if stdin is not a TTY and cannot read input.
func promptYesNo(question string) (bool, error) {
	if !isTerminal() {
		return false, fmt.Errorf("cannot prompt for input: stdin is not a TTY. Use --yes flag to skip interactive prompts")
	}

	fmt.Printf("%s [y/N] ", question)

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("read user input: %w", err)
	}

	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes", nil
}

// createFileIfNotExists creates a file with the given content if it doesn't exist.
func createFileIfNotExists(filename, content string) error {
	if _, err := os.Stat(filename); err == nil {
		ui.Warning("%s already exists, skipping", filepath.Base(filename))
		return nil
	}

	if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
		return err
	}

	ui.Success("Created %s", filepath.Base(filename))
	return nil
}

// Starter file templates

const starterComposeYML = `# Bosun - GitOps webhook receiver
# This container receives webhooks and deploys your services

services:
  bosun:
    image: ghcr.io/cameronsjo/bosun:latest
    container_name: bosun
    restart: unless-stopped
    environment:
      TZ: ${TZ:-America/Chicago}
      WEBHOOK_SECRET: ${WEBHOOK_SECRET:-change-me}
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ${APPDATA:-./appdata}/bosun:/app/data
    ports:
      - "8080:8080"
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:8080/health"]
      interval: 30s
      timeout: 5s
      retries: 3

networks:
  default:
    name: bosun-net
`

const starterPyprojectTOML = `[project]
name = "bosun-manifest"
version = "0.1.0"
description = "Service manifests for bosun"
requires-python = ">=3.11"
dependencies = [
    "pyyaml>=6.0",
]

[build-system]
requires = ["hatchling"]
build-backend = "hatchling.build"

[tool.hatch.build.targets.wheel]
packages = ["."]
`

const starterExampleService = `# Example service manifest
# Replace with your actual service configuration
name: example

provisions:
  - container
  - healthcheck
  - reverse-proxy

config:
  image: nginx:alpine
  port: 80
  subdomain: example
  domain: example.com
  description: Example service
`

const starterGitignore = `# Secrets (encrypted files are OK)
*.yaml
!*.sops.yaml
secrets.yaml

# Python
__pycache__/
*.py[cod]
.venv/

# Output
manifest/output/

# OS
.DS_Store
Thumbs.db

# IDE
.idea/
.vscode/
`

const starterReadme = `# My Homelab

Managed by [bosun](https://github.com/cameronsjo/bosun) - Helm for home.

## Quick Start

` + "```bash" + `
# Check system
bosun doctor

# Start bosun webhook receiver
bosun yacht up

# Add services to manifest/services/
# Deploy with git push
` + "```" + `

## Structure

` + "```" + `
├── bosun/           # Webhook receiver
├── manifest/        # Service definitions
│   ├── provisions/  # Reusable templates
│   ├── services/    # Individual services
│   └── stacks/      # Service groups
└── .sops.yaml       # Encryption config
` + "```" + `
`

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().BoolVarP(&initYes, "yes", "y", false, "Skip all interactive prompts (assume yes for all questions)")
}
