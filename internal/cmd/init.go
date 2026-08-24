package cmd

import (
	"bufio"
	"context"
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

var (
	initYes     bool
	initSystemd bool
	initDomain  string
)

func runInit(cmd *cobra.Command, args []string) error {
	if err := cmd.Context().Err(); err != nil {
		return err
	}

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
	agePubKey, err := setupAgeKey(cmd.Context())
	if err != nil {
		if ctxErr := cmd.Context().Err(); ctxErr != nil {
			return ctxErr
		}
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
	if ctxErr := cmd.Context().Err(); ctxErr != nil {
		return ctxErr
	}
	gitDir := filepath.Join(targetDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		if _, err := exec.LookPath("git"); err == nil {
			gitInit := exec.CommandContext(cmd.Context(), "git", "init", targetDir)
			gitInit.Stdout = os.Stdout
			gitInit.Stderr = os.Stderr
			if err := gitInit.Run(); err != nil {
				if ctxErr := cmd.Context().Err(); ctxErr != nil {
					return ctxErr
				}
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

	// Step 5: Ask for domain (used for Traefik routing)
	domain := initDomain
	if domain == "" && !initYes {
		ui.Info("Configuring Traefik defaults...")
		fmt.Println("  Your base domain is used for automatic subdomain routing.")
		fmt.Println("  A service named 'myapp' becomes myapp.<domain>.")
		fmt.Println("  Leave blank to skip (you can set this later in bosun.yaml).")
		fmt.Println()
		domain = promptInput("  Base domain:", "")
	}

	// Step 6: Create starter files
	ui.Info("Creating starter files...")

	// bosun docker-compose.yml
	if err := createFileIfNotExists(composeFile, starterComposeYML); err != nil {
		return fmt.Errorf("create compose file: %w", err)
	}

	// Example service manifest
	exampleService := filepath.Join(targetDir, "manifest", "services", "example.yml")
	if err := createFileIfNotExists(exampleService, starterExampleService); err != nil {
		return fmt.Errorf("create example service: %w", err)
	}

	// bosun.yaml
	bosunYamlFile := filepath.Join(targetDir, "bosun.yaml")
	bosunYamlContent := generateBosunYaml(domain)
	if err := createFileIfNotExists(bosunYamlFile, bosunYamlContent); err != nil {
		return fmt.Errorf("create bosun.yaml: %w", err)
	}

	// Traefik configs (only if domain was provided)
	if domain != "" {
		if err := generateTraefikConfigs(targetDir, domain); err != nil {
			ui.Warning("Traefik config generation: %v", err)
		}
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

	// Step 7: Generate systemd unit files if requested
	if initSystemd {
		ui.Info("Generating systemd unit files...")
		if err := generateSystemdUnits(targetDir); err != nil {
			return fmt.Errorf("generate systemd units: %w", err)
		}
	}

	// Summary
	fmt.Println()
	ui.Anchor("Yacht christened! Here's your checklist:")
	fmt.Println()
	fmt.Println("  1. Review .sops.yaml and update the age public key if needed")
	if domain != "" {
		fmt.Println("  2. Review traefik/ configs and update your ACME email")
		fmt.Println("  3. Edit manifest/services/example.yml or create your own")
		fmt.Println("  4. Run 'bosun doctor' to verify your setup")
		fmt.Println("  5. Run 'bosun upgrade traefik' to check Traefik configuration")
		fmt.Println("  6. Run 'bosun yacht up' to start services")
		fmt.Println("  7. Push to git to deploy!")
	} else {
		fmt.Println("  2. Set your domain in bosun.yaml")
		fmt.Println("  3. Edit manifest/services/example.yml or create your own")
		fmt.Println("  4. Run 'bosun doctor' to verify your setup")
		fmt.Println("  5. Run 'bosun yacht up' to start the webhook receiver")
		fmt.Println("  6. Push to git to deploy!")
	}
	fmt.Println()
	ui.Info("Run 'bosun --help' for all commands.")

	return nil
}

// setupAgeKey checks for an existing age key or generates a new one.
func setupAgeKey(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

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
		return extractAgePublicKey(ctx, ageKeyFile)
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
	keygen := exec.CommandContext(ctx, "age-keygen", "-o", ageKeyFile)
	output, err := keygen.CombinedOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
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
	return extractAgePublicKey(ctx, ageKeyFile)
}

// extractAgePublicKey reads the public key from an age key file.
func extractAgePublicKey(ctx context.Context, keyFile string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	file, err := os.Open(keyFile)
	if err != nil {
		return "", fmt.Errorf("open key file: %w", err)
	}
	defer func() { _ = file.Close() }()

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
		deriveCmd := exec.CommandContext(ctx, "age-keygen", "-y", keyFile)
		output, err := deriveCmd.Output()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
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

// promptInput asks the user for text input with a default value.
// Returns the default if stdin is not a TTY.
func promptInput(question, defaultVal string) string {
	if !isTerminal() {
		return defaultVal
	}

	if defaultVal != "" {
		fmt.Printf("%s [%s] ", question, defaultVal)
	} else {
		fmt.Printf("%s ", question)
	}

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return defaultVal
	}

	response = strings.TrimSpace(response)
	if response == "" {
		return defaultVal
	}
	return response
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

// generateBosunYaml creates the bosun.yaml content with optional domain.
func generateBosunYaml(domain string) string {
	var b strings.Builder
	b.WriteString("# bosun.yaml - Project configuration\n")
	b.WriteString("# See: bosun doctor (pre-flight checks)\n\n")

	if domain != "" {
		b.WriteString("# Base domain for Traefik routing\n")
		_, _ = fmt.Fprintf(&b, "domain: %s\n\n", domain)
	} else {
		b.WriteString("# Base domain for Traefik routing (uncomment and set your domain)\n")
		b.WriteString("# domain: example.com\n\n")
	}

	b.WriteString("# Infrastructure containers (shown separately in bosun status)\n")
	b.WriteString("infrastructure:\n")
	b.WriteString("  containers:\n")
	b.WriteString("    - traefik\n")
	b.WriteString("    - authelia\n")
	b.WriteString("    - gatus\n")

	return b.String()
}

// generateTraefikConfigs creates Traefik static and dynamic config files
// with Bosun's recommended security defaults.
func generateTraefikConfigs(targetDir, domain string) error {
	// Create traefik config directories
	dynamicDir := filepath.Join(targetDir, "traefik", "conf.d")
	if err := os.MkdirAll(dynamicDir, 0755); err != nil {
		return fmt.Errorf("create traefik config directory: %w", err)
	}

	// Create traefik/acme directory for certificate storage
	acmeDir := filepath.Join(targetDir, "traefik", "acme")
	if err := os.MkdirAll(acmeDir, 0755); err != nil {
		return fmt.Errorf("create acme directory: %w", err)
	}

	// Dynamic config: security headers + compression middleware
	middlewaresFile := filepath.Join(dynamicDir, "middlewares.yml")
	if err := createFileIfNotExists(middlewaresFile, traefikMiddlewaresYML); err != nil {
		return fmt.Errorf("create middlewares.yml: %w", err)
	}

	// Static config hint file (command flags reference)
	staticHintFile := filepath.Join(targetDir, "traefik", "TRAEFIK-FLAGS.md")
	staticContent := generateTraefikFlagsDoc(domain)
	if err := createFileIfNotExists(staticHintFile, staticContent); err != nil {
		return fmt.Errorf("create traefik flags doc: %w", err)
	}

	ui.Success("Created Traefik configs with security defaults")
	return nil
}

// generateTraefikFlagsDoc creates a reference doc for Traefik command flags.
func generateTraefikFlagsDoc(domain string) string {
	return fmt.Sprintf(`# Traefik Command Flags

Add these flags to your Traefik service's command section in your compose file.

`+"```yaml"+`
services:
  traefik:
    image: traefik:v3
    command:
      # API and dashboard
      - "--api.dashboard=true"
      # Docker provider
      - "--providers.docker=true"
      - "--providers.docker.exposedbydefault=false"
      - "--providers.docker.defaultRule=Host(`+"`"+`{{ index .Labels \"bosun.subdomain\" }}.%s`+"`"+`)"
      # Dynamic config from file
      - "--providers.file.directory=/etc/traefik/conf.d"
      - "--providers.file.watch=true"
      # Entrypoints
      - "--entrypoints.web.address=:80"
      - "--entrypoints.websecure.address=:443"
      # HTTP to HTTPS redirect
      - "--entrypoints.web.http.redirections.entrypoint.to=websecure"
      - "--entrypoints.web.http.redirections.entrypoint.scheme=https"
      # Let's Encrypt
      - "--certificatesresolvers.letsencrypt.acme.email=you@example.com"
      - "--certificatesresolvers.letsencrypt.acme.storage=/letsencrypt/acme.json"
      - "--certificatesresolvers.letsencrypt.acme.httpchallenge.entrypoint=web"
`+"```"+`

These flags implement Bosun's recommended security baseline.
Run `+"`bosun upgrade traefik`"+` to check your configuration at any time.
`, domain)
}

const traefikMiddlewaresYML = `# Traefik Dynamic Config - Security Defaults
# Generated by bosun init
# See: bosun upgrade traefik (check configuration)
# See: docs/traefik-defaults.md (what each header does)

http:
  middlewares:
    # Security headers - apply to all public-facing routers
    secure-defaults:
      headers:
        stsSeconds: 31536000
        stsIncludeSubdomains: true
        stsPreload: true
        contentTypeNosniff: true
        frameDeny: true
        browserXssFilter: true
        referrerPolicy: strict-origin-when-cross-origin
        permissionsPolicy: camera=(), microphone=(), geolocation=()

    # Compression - gzip/brotli for responses over 1KB
    default-compress:
      compress:
        minResponseBodyBytes: 1024
`

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().BoolVarP(&initYes, "yes", "y", false, "Skip all interactive prompts (assume yes for all questions)")
	initCmd.Flags().BoolVar(&initSystemd, "systemd", false, "Generate systemd unit files for daemon mode")
	initCmd.Flags().StringVar(&initDomain, "domain", "", "Base domain for Traefik routing (e.g., example.com)")
}

// generateSystemdUnits creates systemd service and socket unit files.
func generateSystemdUnits(targetDir string) error {
	systemdDir := filepath.Join(targetDir, "systemd")
	if err := os.MkdirAll(systemdDir, 0755); err != nil {
		return fmt.Errorf("create systemd directory: %w", err)
	}

	// Generate bosund.service
	serviceFile := filepath.Join(systemdDir, "bosund.service")
	if err := createFileIfNotExists(serviceFile, systemdServiceUnit); err != nil {
		return fmt.Errorf("create service unit: %w", err)
	}

	// Generate bosund.socket (for socket activation)
	socketFile := filepath.Join(systemdDir, "bosund.socket")
	if err := createFileIfNotExists(socketFile, systemdSocketUnit); err != nil {
		return fmt.Errorf("create socket unit: %w", err)
	}

	// Generate environment file template
	envFile := filepath.Join(systemdDir, "bosund.env.example")
	if err := createFileIfNotExists(envFile, systemdEnvFile); err != nil {
		return fmt.Errorf("create env file: %w", err)
	}

	// Generate installation script
	installScript := filepath.Join(systemdDir, "install.sh")
	if err := createFileIfNotExists(installScript, systemdInstallScript); err != nil {
		return fmt.Errorf("create install script: %w", err)
	}
	// Make install script executable
	if err := os.Chmod(installScript, 0755); err != nil {
		return fmt.Errorf("chmod install script: %w", err)
	}

	ui.Success("Generated systemd unit files in systemd/")
	fmt.Println()
	ui.Info("To install the systemd service:")
	fmt.Println("    cd systemd && sudo ./install.sh")
	fmt.Println()
	ui.Info("Or manually:")
	fmt.Println("    sudo cp bosund.service bosund.socket /etc/systemd/system/")
	fmt.Println("    sudo cp bosund.env.example /etc/bosun/bosund.env")
	fmt.Println("    sudo systemctl daemon-reload")
	fmt.Println("    sudo systemctl enable --now bosund.socket")

	return nil
}

// Systemd unit file templates

const systemdServiceUnit = `[Unit]
Description=Bosun GitOps Daemon
Documentation=https://github.com/cameronsjo/bosun
After=network-online.target docker.service
Wants=network-online.target
Requires=docker.service

[Service]
Type=simple
User=bosun
Group=bosun

# Environment file with secrets
EnvironmentFile=/etc/bosun/bosund.env

# Main daemon process
ExecStart=/usr/local/bin/bosun daemon
ExecReload=/bin/kill -HUP $MAINPID

# Restart configuration
Restart=on-failure
RestartSec=10
StartLimitIntervalSec=60
StartLimitBurst=3

# Security hardening
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/var/run/bosun.sock /var/lib/bosun /var/log/bosun

# Resource limits
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`

const systemdSocketUnit = `[Unit]
Description=Bosun GitOps Socket
Documentation=https://github.com/cameronsjo/bosun
PartOf=bosund.service

[Socket]
ListenStream=/var/run/bosun.sock
SocketMode=0660
SocketUser=bosun
SocketGroup=bosun

# Remove stale socket on start
RemoveOnStop=yes

[Install]
WantedBy=sockets.target
`

const systemdEnvFile = `# Bosun Daemon Environment Configuration
# Copy this to /etc/bosun/bosund.env and edit

# Required: Git repository URL
BOSUN_REPO_URL=https://github.com/your-org/your-infra-repo.git

# Optional: private HTTPS Git authentication. Set both values together.
# Credentials require an HTTPS URL without embedded userinfo. Restart Bosun
# after rotating either value; project configuration reload does not read them.
# BOSUN_GIT_USERNAME=your-username
# BOSUN_GIT_TOKEN=your-personal-access-token

# Optional: Git branch (default: main)
# BOSUN_REPO_BRANCH=main

# Optional: Poll interval in seconds (default: 3600)
# BOSUN_POLL_INTERVAL=3600

# Optional: Deploy target for remote deployments
# DEPLOY_TARGET=root@192.168.1.8

# Optional: Secrets files (comma-separated, relative to repo)
# BOSUN_SECRETS_FILE=secrets/prod.sops.yaml

# Optional: Webhook secret for GitHub webhooks
# GITHUB_WEBHOOK_SECRET=your-webhook-secret

# Optional: Discord webhook for notifications
# DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/...

# Optional: Disable HTTP server (socket-only mode)
# BOSUN_DISABLE_HTTP=true
`

const systemdInstallScript = `#!/bin/bash
# Bosun systemd installation script
# Run as root: sudo ./install.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "Installing Bosun systemd service..."

# Create bosun user if it doesn't exist
if ! id -u bosun &>/dev/null; then
    echo "Creating bosun user..."
    useradd -r -s /sbin/nologin -d /var/lib/bosun -m bosun
    usermod -aG docker bosun
fi

# Create directories
echo "Creating directories..."
mkdir -p /etc/bosun
mkdir -p /var/lib/bosun
mkdir -p /var/log/bosun
mkdir -p /var/run

# Set permissions
chown bosun:bosun /var/lib/bosun
chown bosun:bosun /var/log/bosun

# Copy unit files
echo "Installing unit files..."
cp "$SCRIPT_DIR/bosund.service" /etc/systemd/system/
cp "$SCRIPT_DIR/bosund.socket" /etc/systemd/system/

# Copy environment file if it doesn't exist
if [ ! -f /etc/bosun/bosund.env ]; then
    echo "Installing environment file template..."
    cp "$SCRIPT_DIR/bosund.env.example" /etc/bosun/bosund.env
    chmod 600 /etc/bosun/bosund.env
    echo ""
    echo "IMPORTANT: Edit /etc/bosun/bosund.env with your configuration!"
    echo ""
fi

# Reload systemd
echo "Reloading systemd..."
systemctl daemon-reload

echo ""
echo "Installation complete!"
echo ""
echo "Next steps:"
echo "  1. Edit /etc/bosun/bosund.env with your configuration"
echo "  2. Enable and start the service:"
echo "     sudo systemctl enable --now bosund.socket"
echo "     sudo systemctl enable --now bosund.service"
echo ""
echo "Commands:"
echo "  sudo systemctl status bosund        # Check status"
echo "  sudo journalctl -u bosund -f        # Follow logs"
echo "  bosun trigger                       # Trigger reconciliation"
echo "  bosun daemon-status                 # Check daemon status"
`
