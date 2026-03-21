// Package cmd provides the CLI commands for bosun.
package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/config"
	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/tunnel"
	"github.com/cameronsjo/bosun/internal/ui"
)

// doctorCmd runs pre-flight checks.
var doctorCmd = &cobra.Command{
	Use:     "doctor",
	Aliases: []string{"checkup"},
	Short:   "Pre-flight checks - is the ship seaworthy?",
	Long:    "Run diagnostic checks for Docker, Git, SOPS, and other dependencies.",
	Run:     runDoctor,
}

// checkDocker verifies Docker is running and accessible.
// NOTE: This function uses explicit Docker client handling because it needs
// a caller-provided context with timeout for the ping operation.
func checkDocker(ctx context.Context) CheckResult {
	var result CheckResult
	err := withDockerClientContext(ctx, func(client *docker.Client) error {
		if err := client.Ping(ctx); err == nil {
			_, _ = ui.Green.Println("  * Docker is running")
			result = CheckResult{Passed: 1}
			return nil
		}
		_, _ = ui.Red.Println("  x Docker is not running")
		_, _ = ui.Blue.Println("      To fix this:")
		_, _ = ui.Blue.Println("      - Start Docker: systemctl start docker")
		_, _ = ui.Blue.Println("      - Or use Docker Desktop on macOS/Windows")
		result = CheckResult{Failed: 1}
		return nil
	})

	if err != nil {
		_, _ = ui.Red.Println("  x Docker is not running")
		_, _ = ui.Blue.Println("      To fix this:")
		_, _ = ui.Blue.Println("      - Install Docker from https://docs.docker.com/get-docker/")
		_, _ = ui.Blue.Println("      - Ensure Docker daemon is running: systemctl start docker")
		_, _ = ui.Blue.Println("      - Check permissions: docker ps (should not require sudo)")
		return CheckResult{Failed: 1}
	}

	return result
}

// checkDockerCompose verifies Docker Compose v2 is installed.
func checkDockerCompose() CheckResult {
	composeCmd := exec.Command("docker", "compose", "version", "--short")
	if output, err := composeCmd.Output(); err == nil {
		version := strings.TrimSpace(string(output))
		_, _ = ui.Green.Printf("  * Docker Compose v2 (%s)\n", version)
		return CheckResult{Passed: 1}
	}
	_, _ = ui.Red.Println("  x Docker Compose v2 not found")
	_, _ = ui.Blue.Println("      To fix this:")
	_, _ = ui.Blue.Println("      - Install Docker Desktop (includes Compose v2)")
	_, _ = ui.Blue.Println("      - Or: https://docs.docker.com/compose/install/")
	return CheckResult{Failed: 1}
}

// checkGit verifies Git is installed.
func checkGit() CheckResult {
	if _, err := exec.LookPath("git"); err == nil {
		_, _ = ui.Green.Println("  * Git is installed")
		return CheckResult{Passed: 1}
	}
	_, _ = ui.Red.Println("  x Git not found")
	_, _ = ui.Blue.Println("      To fix this:")
	_, _ = ui.Blue.Println("      - macOS: brew install git")
	_, _ = ui.Blue.Println("      - Ubuntu/Debian: apt-get install git")
	_, _ = ui.Blue.Println("      - Fedora/RHEL: dnf install git")
	_, _ = ui.Blue.Println("      - Windows: https://git-scm.com/download/win")
	return CheckResult{Failed: 1}
}

// checkProjectRoot verifies the project root is accessible.
func checkProjectRoot(cfg *config.Config) CheckResult {
	if cfg != nil {
		_, _ = ui.Green.Printf("  * Project root found: %s\n", cfg.Root)
		return CheckResult{Passed: 1}
	}
	_, _ = ui.Yellow.Println("  ! Project root not found (run from project directory)")
	_, _ = ui.Blue.Println("      To fix this:")
	_, _ = ui.Blue.Println("      - Ensure config.yaml or manifest/ directory exists")
	_, _ = ui.Blue.Println("      - Run bosun from the root of your project")
	_, _ = ui.Blue.Println("      - Create config.yaml in project root if missing")
	return CheckResult{Warned: 1}
}

// checkAgeKey verifies the Age key exists for SOPS decryption.
func checkAgeKey() CheckResult {
	ageKeyFile := os.Getenv("SOPS_AGE_KEY_FILE")
	if ageKeyFile == "" {
		home, _ := os.UserHomeDir()
		ageKeyFile = filepath.Join(home, ".config", "sops", "age", "keys.txt")
	}
	if _, err := os.Stat(ageKeyFile); err == nil {
		_, _ = ui.Green.Printf("  * Age key found: %s\n", ageKeyFile)
		return CheckResult{Passed: 1}
	}
	_, _ = ui.Yellow.Printf("  ! Age key not found at %s\n", ageKeyFile)
	_, _ = ui.Blue.Println("      To fix this:")
	_, _ = ui.Blue.Printf("      - Run: age-keygen -o %s\n", ageKeyFile)
	_, _ = ui.Blue.Println("      - Or set SOPS_AGE_KEY_FILE env var to existing key")
	_, _ = ui.Blue.Println("      - Install age: https://github.com/FiloSottile/age#installation")
	return CheckResult{Warned: 1}
}

// checkSOPS verifies SOPS is installed.
func checkSOPS() CheckResult {
	if sopsPath, err := exec.LookPath("sops"); err == nil {
		versionCmd := exec.Command(sopsPath, "--version")
		if output, err := versionCmd.Output(); err == nil {
			version := strings.TrimSpace(string(output))
			_, _ = ui.Green.Printf("  * SOPS is installed (%s)\n", version)
		} else {
			_, _ = ui.Green.Println("  * SOPS is installed")
		}
		return CheckResult{Passed: 1}
	}
	_, _ = ui.Yellow.Println("  ! SOPS not found (needed for secrets)")
	_, _ = ui.Blue.Println("      To fix this:")
	_, _ = ui.Blue.Println("      - macOS: brew install sops")
	_, _ = ui.Blue.Println("      - Ubuntu/Debian: apt-get install sops")
	_, _ = ui.Blue.Println("      - Fedora/RHEL: dnf install sops")
	_, _ = ui.Blue.Println("      - Or: https://github.com/getsops/sops/releases")
	return CheckResult{Warned: 1}
}

// checkManifestDirectory verifies the manifest directory exists.
func checkManifestDirectory(cfg *config.Config) CheckResult {
	if cfg == nil {
		return CheckResult{} // Skip if no config
	}
	if _, err := os.Stat(cfg.ManifestDir); err == nil {
		_, _ = ui.Green.Println("  * Manifest directory found")
		return CheckResult{Passed: 1}
	}
	_, _ = ui.Yellow.Println("  ! Manifest directory not found")
	_, _ = ui.Blue.Println("      To fix this:")
	_, _ = ui.Blue.Printf("      - Create manifest directory at: %s\n", cfg.ManifestDir)
	_, _ = ui.Blue.Println("      - Or update manifest_dir in config.yaml")
	_, _ = ui.Blue.Println("      - See: https://github.com/cameronsjo/bosun/docs/")
	return CheckResult{Warned: 1}
}

// checkWebhook verifies the webhook endpoint is responding.
func checkWebhook() CheckResult {
	httpClient := &http.Client{Timeout: httpClientTimeout}
	resp, err := httpClient.Get("http://localhost:8080/health")
	if err == nil {
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			_, _ = ui.Green.Println("  * Webhook endpoint responding")
			return CheckResult{Passed: 1}
		}
	}
	_, _ = ui.Yellow.Println("  ! Webhook not responding (bosun container not running?)")
	_, _ = ui.Blue.Println("      To fix this:")
	_, _ = ui.Blue.Println("      - Start bosun container: docker-compose up -d bosun")
	_, _ = ui.Blue.Println("      - Check logs: docker logs bosun")
	_, _ = ui.Blue.Println("      - Verify port 8080 is available and not in use")
	return CheckResult{Warned: 1}
}

// checkTunnel verifies the configured tunnel provider is installed and connected.
func checkTunnel(ctx context.Context, cfg *config.Config) CheckResult {
	providerName := "tailscale" // default
	if cfg != nil {
		providerName = cfg.TunnelProvider()
	}

	provider, err := tunnel.NewProvider(providerName)
	if err != nil {
		if _, ok := err.(tunnel.ErrNotInstalled); ok {
			_, _ = ui.Yellow.Printf("  ! %s not installed\n", capitalizeProviderName(providerName))
			_, _ = ui.Blue.Println("      To fix this:")
			switch providerName {
			case "tailscale":
				_, _ = ui.Blue.Println("      - Install from: https://tailscale.com/download")
			case "cloudflare":
				_, _ = ui.Blue.Println("      - Install from: https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/installation/")
			default:
				_, _ = ui.Blue.Printf("      - Install %s\n", providerName)
			}
			return CheckResult{Warned: 1}
		}
		_, _ = ui.Yellow.Printf("  ! Tunnel provider error: %v\n", err)
		return CheckResult{Warned: 1}
	}

	status, err := provider.Status(ctx)
	if err != nil {
		_, _ = ui.Yellow.Printf("  ! Failed to get %s status: %v\n", providerName, err)
		return CheckResult{Warned: 1}
	}

	if status.Connected {
		_, _ = ui.Green.Printf("  * %s is connected", capitalizeProviderName(providerName))
		if status.Hostname != "" {
			fmt.Printf(" (%s)", status.Hostname)
		}
		fmt.Println()
		return CheckResult{Passed: 1}
	}

	_, _ = ui.Yellow.Printf("  ! %s is not connected (state: %s)\n", capitalizeProviderName(providerName), status.BackendState)
	_, _ = ui.Blue.Println("      To fix this:")
	switch providerName {
	case "tailscale":
		_, _ = ui.Blue.Println("      - Run: tailscale up")
	case "cloudflare":
		_, _ = ui.Blue.Println("      - Run: cloudflared tunnel run <tunnel-name>")
	}
	return CheckResult{Warned: 1}
}

// capitalizeProviderName capitalizes the first letter of a provider name.
func capitalizeProviderName(name string) string {
	if name == "" {
		return name
	}
	// Handle special cases
	switch name {
	case "tailscale":
		return "Tailscale"
	case "cloudflare":
		return "Cloudflare"
	default:
		return strings.ToUpper(name[:1]) + name[1:]
	}
}

// checkTraefikConfig validates Traefik configuration against recommended defaults.
// Returns an empty CheckResult if no Traefik service is found (not all projects use Traefik).
func checkTraefikConfig(cfg *config.Config) CheckResult {
	var result CheckResult

	composePath, err := findTraefikComposeFile(cfg, "")
	if err != nil {
		// No Traefik service found — skip silently
		return result
	}

	svc, err := parseTraefikService(composePath)
	if err != nil || svc == nil {
		return result
	}

	fmt.Println()
	_, _ = ui.Blue.Println("--- Traefik Configuration ---")

	// Check 1: HTTPS redirect
	httpsCheck := checkHTTPSRedirect(svc)
	switch httpsCheck.Status {
	case "pass":
		_, _ = ui.Green.Println("  * Traefik: HTTPS redirect configured")
		result.Passed++
	default:
		_, _ = ui.Yellow.Println("  ! Traefik: HTTPS redirect not configured")
		_, _ = ui.Blue.Println("      To fix this:")
		_, _ = ui.Blue.Println("      - Add to Traefik command: --entrypoints.web.http.redirections.entrypoint.to=websecure")
		_, _ = ui.Blue.Println("      - Or run: bosun upgrade traefik")
		result.Warned++
	}

	// Check 2: exposedByDefault
	exposedCheck := checkExposedByDefault(svc)
	switch exposedCheck.Status {
	case "pass":
		_, _ = ui.Green.Println("  * Traefik: exposedByDefault is false")
		result.Passed++
	default:
		_, _ = ui.Yellow.Println("  ! Traefik: exposedByDefault not set to false")
		_, _ = ui.Blue.Println("      To fix this:")
		_, _ = ui.Blue.Println("      - Add to Traefik command: --providers.docker.exposedbydefault=false")
		_, _ = ui.Blue.Println("      - Or run: bosun upgrade traefik")
		result.Warned++
	}

	// Check 3: Security headers middleware
	dynamicDir := findTraefikDynamicDir(svc, composePath)
	headersCheck := checkSecurityHeaders(dynamicDir)
	switch headersCheck.Status {
	case "pass":
		_, _ = ui.Green.Println("  * Traefik: Security headers middleware configured")
		result.Passed++
	default:
		_, _ = ui.Yellow.Println("  ! Traefik: No secure-defaults middleware found")
		_, _ = ui.Blue.Println("      To fix this:")
		_, _ = ui.Blue.Println("      - Add secure-defaults middleware to Traefik dynamic config")
		_, _ = ui.Blue.Println("      - Or run: bosun upgrade traefik")
		result.Warned++
	}

	// Check 4: Docker socket exposure
	socketCheck := checkDockerSocket(svc)
	switch socketCheck.Status {
	case "pass":
		_, _ = ui.Green.Printf("  * Traefik: %s\n", socketCheck.Description)
		result.Passed++
	case "warn":
		_, _ = ui.Yellow.Printf("  ! Traefik: %s\n", socketCheck.Description)
		_, _ = ui.Blue.Println("      To fix this:")
		_, _ = ui.Blue.Println("      - Use docker-socket-proxy instead of mounting /var/run/docker.sock directly")
		_, _ = ui.Blue.Println("      - See: https://github.com/Tecnativa/docker-socket-proxy")
		result.Warned++
	}

	return result
}

// checkDockerSocket verifies whether the Docker socket is mounted directly.
func checkDockerSocket(svc *traefikComposeService) traefikCheck {
	check := traefikCheck{
		Name: "Docker Socket",
	}

	for _, vol := range svc.Volumes {
		if strings.Contains(vol, "/var/run/docker.sock") {
			check.Status = "warn"
			check.Description = "Docker socket mounted directly (consider docker-socket-proxy for security)"
			return check
		}
	}

	check.Status = "pass"
	check.Description = "Docker socket not mounted directly"
	return check
}

func runDoctor(cmd *cobra.Command, args []string) {
	_, _ = ui.Blue.Println("Running pre-flight checks...")
	fmt.Println()

	var result CheckResult

	// Load config once for checks that need it
	cfg, _ := config.Load()

	// Run all checks with timeout context for Docker
	ctx, cancel := context.WithTimeout(context.Background(), dockerPingTimeout)
	result.Add(checkDocker(ctx))
	cancel()

	result.Add(checkDockerCompose())
	result.Add(checkGit())
	result.Add(checkProjectRoot(cfg))
	result.Add(checkAgeKey())
	result.Add(checkSOPS())
	result.Add(checkManifestDirectory(cfg))
	result.Add(checkWebhook())

	// Check tunnel provider with timeout
	tunnelCtx, tunnelCancel := context.WithTimeout(context.Background(), doctorCheckTimeout)
	result.Add(checkTunnel(tunnelCtx, cfg))
	tunnelCancel()

	// Check Traefik configuration (only if a Traefik service is found)
	result.Add(checkTraefikConfig(cfg))

	// Summary
	fmt.Println()
	fmt.Printf("Summary: ")
	_, _ = ui.Green.Printf("%d passed", result.Passed)
	fmt.Printf(", ")
	_, _ = ui.Yellow.Printf("%d warnings", result.Warned)
	fmt.Printf(", ")
	_, _ = ui.Red.Printf("%d failed\n", result.Failed)

	if result.Failed > 0 {
		fmt.Println()
		_, _ = ui.Red.Println("Ship not seaworthy! Fix errors above.")
		os.Exit(1)
	} else if result.Warned > 0 {
		fmt.Println()
		_, _ = ui.Yellow.Println("Ship can sail, but check warnings.")
	} else {
		fmt.Println()
		_, _ = ui.Green.Println("All systems go! Ready to sail.")
	}
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
