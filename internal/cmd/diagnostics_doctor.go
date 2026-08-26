// Package cmd provides the CLI commands for bosun.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/cameronsjo/bosun/internal/config"
	"github.com/cameronsjo/bosun/internal/daemon"
	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/preflight"
	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/cameronsjo/bosun/internal/tunnel"
	"github.com/cameronsjo/bosun/internal/ui"
)

// doctorCmd runs pre-flight checks.
var doctorCmd = &cobra.Command{
	Use:     "doctor",
	Aliases: []string{"checkup"},
	Short:   "Pre-flight checks - is the ship seaworthy?",
	Long:    "Run diagnostic checks for Docker, Git, SOPS, and other dependencies.",
	RunE:    runDoctor,
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
func checkDockerCompose(ctx context.Context) (CheckResult, error) {
	if err := ctx.Err(); err != nil {
		return CheckResult{}, err
	}

	composeCmd := exec.CommandContext(ctx, "docker", "compose", "version", "--short")
	if output, err := composeCmd.Output(); err == nil {
		version := strings.TrimSpace(string(output))
		_, _ = ui.Green.Printf("  * Docker Compose v2 (%s)\n", version)
		return CheckResult{Passed: 1}, nil
	} else if ctxErr := ctx.Err(); ctxErr != nil {
		return CheckResult{}, ctxErr
	}
	_, _ = ui.Red.Println("  x Docker Compose v2 not found")
	_, _ = ui.Blue.Println("      To fix this:")
	_, _ = ui.Blue.Println("      - Install Docker Desktop (includes Compose v2)")
	_, _ = ui.Blue.Println("      - Or: https://docs.docker.com/compose/install/")
	return CheckResult{Failed: 1}, nil
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
// loadErr is the error returned by config.Load(); when non-nil it is inspected
// to distinguish a YAML parse failure from a plain "not found" situation.
func checkProjectRoot(cfg *config.Config, loadErr error) CheckResult {
	if cfg != nil {
		_, _ = ui.Green.Printf("  * Project root found: %s\n", cfg.Root)
		return CheckResult{Passed: 1}
	}

	// Distinguish specific load failures from a missing-project situation so
	// operators see the actual root cause rather than the generic "not found".

	// File-read failure (e.g. permission denied on an existing config file).
	// loadConfigFile embeds the sentinel "failed to read config file" — the
	// project root was found but the file is unreadable.
	if loadErr != nil && strings.Contains(loadErr.Error(), "failed to read config file") {
		_, _ = ui.Red.Printf("  x Project config file unreadable: %s\n", loadErr)
		_, _ = ui.Blue.Println("      To fix this:")
		_, _ = ui.Blue.Println("      - Check file permissions: ls -l bosun.yaml")
		_, _ = ui.Blue.Println("      - Fix permissions: chmod 644 bosun.yaml")
		return CheckResult{Failed: 1}
	}

	// YAML parse/decode error. yaml.v3 surfaces syntax errors as plain
	// *errors.errorString, not *yaml.TypeError. *yaml.TypeError is reserved
	// for type-mismatch failures (e.g. a field declared as int receives a
	// string), which can occur during struct decode after successful parsing.
	// We keep both checks: the sentinel string for the common syntax case and
	// errors.As for future type-mismatch paths.
	var yamlTypeErr *yaml.TypeError
	if loadErr != nil && (errors.As(loadErr, &yamlTypeErr) || strings.Contains(loadErr.Error(), "failed to parse config file")) {
		_, _ = ui.Red.Printf("  x Project config invalid YAML: %s\n", loadErr)
		_, _ = ui.Blue.Println("      To fix this:")
		_, _ = ui.Blue.Println("      - Check bosun.yaml for syntax errors (tabs vs spaces, missing quotes, etc.)")
		_, _ = ui.Blue.Println("      - Validate with: python3 -c \"import yaml,sys; yaml.safe_load(open('bosun.yaml'))\"")
		return CheckResult{Failed: 1}
	}

	_, _ = ui.Yellow.Println("  ! Project root not found (run from project directory)")
	_, _ = ui.Blue.Println("      To fix this:")
	_, _ = ui.Blue.Println("      - Ensure bosun.yaml or manifest/ directory exists")
	_, _ = ui.Blue.Println("      - Run bosun from the root of your project")
	_, _ = ui.Blue.Println("      - Create bosun.yaml in project root if missing")
	return CheckResult{Warned: 1}
}

// checkAgeKey verifies the Age key exists for SOPS decryption.
func checkAgeKey() CheckResult {
	if err := reconcile.NewSOPSOps().CheckAgeKey(); err != nil {
		_, _ = ui.Yellow.Printf("  ! Age key unavailable: %v\n", err)
		return CheckResult{Warned: 1}
	}

	if os.Getenv("SOPS_AGE_KEY") != "" {
		_, _ = ui.Green.Println("  * Age key found via SOPS_AGE_KEY")
		return CheckResult{Passed: 1}
	}

	ageKeyFile := os.Getenv("SOPS_AGE_KEY_FILE")
	if ageKeyFile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			// CheckAgeKey already resolved the home directory successfully.
			return CheckResult{Passed: 1}
		}
		ageKeyFile = filepath.Join(home, ".config", "sops", "age", "keys.txt")
	}
	_, _ = ui.Green.Printf("  * Age key found: %s\n", ageKeyFile)
	return CheckResult{Passed: 1}
}

// checkSOPS verifies SOPS is installed.
func checkSOPS(ctx context.Context) (CheckResult, error) {
	if err := ctx.Err(); err != nil {
		return CheckResult{}, err
	}

	if sopsPath, err := exec.LookPath("sops"); err == nil {
		versionCmd := exec.CommandContext(ctx, sopsPath, "--version")
		if output, err := versionCmd.Output(); err == nil {
			version := strings.TrimSpace(string(output))
			_, _ = ui.Green.Printf("  * SOPS is installed (%s)\n", version)
		} else if ctxErr := ctx.Err(); ctxErr != nil {
			return CheckResult{}, ctxErr
		} else {
			_, _ = ui.Green.Println("  * SOPS is installed")
		}
		return CheckResult{Passed: 1}, nil
	}
	_, _ = ui.Yellow.Println("  ! SOPS not found (needed for secrets)")
	_, _ = ui.Blue.Println("      To fix this:")
	_, _ = ui.Blue.Println("      - macOS: brew install sops")
	_, _ = ui.Blue.Println("      - Ubuntu/Debian: apt-get install sops")
	_, _ = ui.Blue.Println("      - Fedora/RHEL: dnf install sops")
	_, _ = ui.Blue.Println("      - Or: https://github.com/getsops/sops/releases")
	return CheckResult{Warned: 1}, nil
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

// deployTargetsUnderFUSE reports whether any configured (or defaulted)
// deploy path lives under Unraid's /mnt/user FUSE mount (the same check
// reconcile.resolveHookSettleDelay applies at deploy time).
// When targets: is configured, every target's appdata paths are checked.
// Otherwise this mirrors the CLI/daemon's flat single-target resolution
// order (REMOTE_APPDATA/LOCAL_APPDATA env override, falling back to
// reconcile.DefaultConfig()'s hardcoded /mnt/user/appdata default).
func deployTargetsUnderFUSE(cfg *config.Config) bool {
	if targets := cfg.Targets(); len(targets) > 0 {
		for _, t := range targets {
			if reconcile.IsUnderFUSEDeployPath(t.RemoteAppdataPath) || reconcile.IsUnderFUSEDeployPath(t.LocalAppdataPath) {
				return true
			}
		}
		return false
	}

	if p := os.Getenv("REMOTE_APPDATA"); p != "" {
		return reconcile.IsUnderFUSEDeployPath(p)
	}
	if p := os.Getenv("LOCAL_APPDATA"); p != "" {
		return reconcile.IsUnderFUSEDeployPath(p)
	}
	return reconcile.IsUnderFUSEDeployPath(reconcile.DefaultConfig().RemoteAppdataPath)
}

// checkHookSettleDelayFUSE warns when hook_settle_delay is unconfigured
// (zero) while the deploy target lives under Unraid's /mnt/user FUSE (shfs)
// mount, where writes need extra time to settle before post-sync hooks read
// them back. reconcile.resolveHookSettleDelay already applies a runtime
// default in this situation, so a missed warning here isn't a silent
// failure — but explicit config is easier to reason about than a heuristic
// default, hence the nudge.
func checkHookSettleDelayFUSE(cfg *config.Config) CheckResult {
	if cfg == nil {
		return CheckResult{}
	}
	if !deployTargetsUnderFUSE(cfg) {
		return CheckResult{}
	}

	if cfg.HookSettleDelay() > 0 {
		_, _ = ui.Green.Printf("  * hook_settle_delay is set (%s) for a /mnt/user (FUSE) deploy target\n", cfg.HookSettleDelay())
		return CheckResult{Passed: 1}
	}

	_, _ = ui.Yellow.Println("  ! hook_settle_delay is unset for a /mnt/user (FUSE) deploy target")
	_, _ = ui.Blue.Println("      To fix this:")
	_, _ = ui.Blue.Println("      - Set hook_settle_delay: 2s (or higher) in bosun.yaml")
	_, _ = ui.Blue.Println("      - Unraid's shfs FUSE layer can lag writes; post-sync hooks may otherwise fire before the write settles")
	return CheckResult{Warned: 1}
}

func doctorDurationOrSeconds(value string) (time.Duration, bool) {
	if d, err := time.ParseDuration(value); err == nil {
		return d, true
	}
	if d, err := time.ParseDuration(value + "s"); err == nil {
		return d, true
	}
	return 0, false
}

// checkRestartBreakerSampling mirrors the daemon's effective env parsing for
// the two sampling durations and surfaces a cadence that is coarser than the
// restart window. Runtime preserves an accumulating baseline in this case, but
// the mismatch is surprising enough to deserve an operator-visible warning.
func checkRestartBreakerSampling() CheckResult {
	driftInterval := daemon.DefaultDriftInterval
	if value := os.Getenv("BOSUN_DRIFT_INTERVAL"); value != "" {
		if parsed, ok := doctorDurationOrSeconds(value); ok {
			driftInterval = parsed
		}
	}

	restartWindow := reconcile.DefaultConfig().RestartWindow
	if value := os.Getenv("BOSUN_RESTART_WINDOW"); value != "" {
		if parsed, ok := doctorDurationOrSeconds(value); ok && parsed > 0 {
			restartWindow = parsed
		}
	}

	if reconcile.RestartBreakerSamplingMismatch(driftInterval, restartWindow) {
		_, _ = ui.Yellow.Printf("  ! Restart breaker sampling interval (%s) exceeds its window (%s)\n", driftInterval, restartWindow)
		_, _ = ui.Blue.Println("      To fix this:")
		_, _ = ui.Blue.Println("      - Set BOSUN_DRIFT_INTERVAL less than or equal to BOSUN_RESTART_WINDOW")
		_, _ = ui.Blue.Println("      - Slow loops still accumulate, but sampling more frequently makes the configured window meaningful")
		return CheckResult{Warned: 1}
	}

	_, _ = ui.Green.Printf("  * Restart breaker sampling interval (%s) fits its window (%s)\n", driftInterval, restartWindow)
	return CheckResult{Passed: 1}
}

// checkStateDir verifies the deploy-state directory is writable.
// The state dir defaults to reconcile.DefaultStateDir but is overridden by
// the BOSUN_STATE_DIR environment variable (same logic as the daemon).
// On Windows the default path (/var/lib/bosun) is meaningless — the check
// is skipped unless BOSUN_STATE_DIR overrides it with a valid path.
func checkStateDir() CheckResult {
	if runtime.GOOS == "windows" && os.Getenv("BOSUN_STATE_DIR") == "" {
		_, _ = ui.Blue.Println("  - State directory check N/A on Windows (set BOSUN_STATE_DIR to enable)")
		return CheckResult{}
	}
	stateDir := reconcile.DefaultStateDir
	if dir := os.Getenv("BOSUN_STATE_DIR"); dir != "" {
		stateDir = dir
	}

	// Attempt to create the directory if it does not yet exist.
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		_, _ = ui.Red.Printf("  x State directory not writable: %s (%v)\n", stateDir, err)
		_, _ = ui.Blue.Println("      To fix this:")
		_, _ = ui.Blue.Printf("      - Create the directory: mkdir -p %s\n", stateDir)
		_, _ = ui.Blue.Printf("      - Fix permissions: chmod 755 %s\n", stateDir)
		_, _ = ui.Blue.Println("      - Or set BOSUN_STATE_DIR to a writable path")
		return CheckResult{Failed: 1}
	}

	// Probe write access with a temp file.
	probe, err := os.CreateTemp(stateDir, ".bosun-doctor-probe-*")
	if err != nil {
		_, _ = ui.Red.Printf("  x State directory not writable: %s (%v)\n", stateDir, err)
		_, _ = ui.Blue.Println("      To fix this:")
		_, _ = ui.Blue.Printf("      - Fix permissions: chmod 755 %s\n", stateDir)
		_, _ = ui.Blue.Println("      - Or set BOSUN_STATE_DIR to a writable path")
		return CheckResult{Failed: 1}
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())

	_, _ = ui.Green.Printf("  * State directory writable: %s\n", stateDir)
	return CheckResult{Passed: 1}
}

// checkSocketDir verifies the directory that will hold the daemon Unix socket
// is writable. The socket path defaults to daemon.DefaultSocketPath but is
// overridden by BOSUN_SOCKET_PATH.
// On Windows the daemon does not use Unix sockets at /var/run — the check
// is skipped unless BOSUN_SOCKET_PATH overrides it with a valid path.
func checkSocketDir() CheckResult {
	if runtime.GOOS == "windows" && os.Getenv("BOSUN_SOCKET_PATH") == "" {
		_, _ = ui.Blue.Println("  - Socket directory check N/A on Windows (set BOSUN_SOCKET_PATH to enable)")
		return CheckResult{}
	}
	socketPath := "/var/run/bosun.sock"
	if p := os.Getenv("BOSUN_SOCKET_PATH"); p != "" {
		socketPath = p
	}
	socketDir := filepath.Dir(socketPath)

	if err := os.MkdirAll(socketDir, 0755); err != nil {
		_, _ = ui.Red.Printf("  x Socket directory not writable: %s (%v)\n", socketDir, err)
		_, _ = ui.Blue.Println("      To fix this:")
		_, _ = ui.Blue.Printf("      - Create the directory: sudo mkdir -p %s\n", socketDir)
		_, _ = ui.Blue.Printf("      - Fix permissions: sudo chmod 755 %s\n", socketDir)
		_, _ = ui.Blue.Println("      - Or set BOSUN_SOCKET_PATH to a path in a writable directory")
		return CheckResult{Failed: 1}
	}

	// Probe write access — only test if the socket doesn't already exist
	// (an existing socket means the daemon is live, which is fine).
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		probe, err := os.CreateTemp(socketDir, ".bosun-doctor-probe-*")
		if err != nil {
			_, _ = ui.Red.Printf("  x Socket directory not writable: %s (%v)\n", socketDir, err)
			_, _ = ui.Blue.Println("      To fix this:")
			_, _ = ui.Blue.Printf("      - Fix permissions: sudo chmod 755 %s\n", socketDir)
			_, _ = ui.Blue.Println("      - Or set BOSUN_SOCKET_PATH to a path in a writable directory")
			return CheckResult{Failed: 1}
		}
		_ = probe.Close()
		_ = os.Remove(probe.Name())
	}

	_, _ = ui.Green.Printf("  * Socket directory writable: %s\n", socketDir)
	return CheckResult{Passed: 1}
}

// webhookAddr returns the HTTP webhook host:port to probe.
//
// The HTTP webhook server (always-on, serves /health and /webhook) reads its
// port from PORT first, then WEBHOOK_PORT as a legacy alias. This mirrors the
// precedence chain in daemon.ConfigFromEnv. BOSUN_TCP_ADDR is unrelated — it
// configures the opt-in TCP API server, which is a distinct service.
func webhookAddr() string {
	if port := os.Getenv("PORT"); port != "" {
		return "localhost:" + port
	}
	if port := os.Getenv("WEBHOOK_PORT"); port != "" {
		return "localhost:" + port
	}
	return "localhost:8080"
}

// checkWebhook verifies the webhook endpoint is responding.
func checkWebhook() CheckResult {
	addr := webhookAddr()
	httpClient := &http.Client{Timeout: httpClientTimeout}
	resp, err := httpClient.Get("http://" + addr + "/health")
	if err == nil {
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			_, _ = ui.Green.Printf("  * Webhook endpoint responding (%s)\n", addr)
			return CheckResult{Passed: 1}
		}
	}
	_, _ = ui.Yellow.Printf("  ! Webhook not responding at %s (bosun container not running?)\n", addr)
	_, _ = ui.Blue.Println("      To fix this:")
	_, _ = ui.Blue.Println("      - Start bosun container: docker compose up -d bosun")
	_, _ = ui.Blue.Println("      - Check logs: docker logs bosun")
	_, _ = ui.Blue.Printf("      - Verify %s is available and not in use\n", addr)
	_, _ = ui.Blue.Println("      - Set PORT (or WEBHOOK_PORT) to override the default port")
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
		var notInstalled tunnel.ErrNotInstalled
		if errors.As(err, &notInstalled) {
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
		if status.Hostname != "" {
			_, _ = ui.Green.Printf("  * %s is connected (%s)\n", capitalizeProviderName(providerName), status.Hostname)
		} else {
			_, _ = ui.Green.Printf("  * %s is connected\n", capitalizeProviderName(providerName))
		}
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

// checkDeployKeyPermissions validates SSH deploy key file permissions using the
// preflight package's resolution order (BOSUN_SSH_KEY → /config/deploy-key →
// /config/ssh-key → ~/.ssh/id_ed25519 → ~/.ssh/id_rsa).
func checkDeployKeyPermissions() CheckResult {
	return checkDeployKeyPermissionsResult(preflight.CheckSSHKeyPermissions())
}

func checkDeployKeyPermissionsResult(res preflight.SSHKeyPermResult) CheckResult {
	switch {
	case res.Path == "":
		// No key file found — not an error; SSH agent or HTTPS may be used.
		_, _ = ui.Yellow.Println("  ! SSH deploy key not found (using agent or HTTPS?)")
		return CheckResult{Warned: 1}
	case res.Err != nil:
		// The preflight error includes any applicable remediation, so print it
		// directly rather than guessing whether chmod is the right fix.
		_, _ = ui.Red.Printf("  x %s\n", res.Err)
		return CheckResult{Failed: 1}
	case !res.PermissionsChecked:
		_, _ = ui.Yellow.Printf("  ! SSH deploy key file found; Windows ACLs not inspected: %s\n", res.Path)
		return CheckResult{Warned: 1}
	default:
		_, _ = ui.Green.Printf("  * SSH deploy key permissions OK (%04o): %s\n", res.Mode, res.Path)
		return CheckResult{Passed: 1}
	}
}

func runDoctor(cmd *cobra.Command, args []string) error {
	if err := cmd.Context().Err(); err != nil {
		return err
	}

	_, _ = ui.Blue.Println("Running pre-flight checks...")
	fmt.Println()

	var result CheckResult

	// Load config once for checks that need it; preserve the error so
	// checkProjectRoot can distinguish a YAML parse failure from "not found".
	cfg, cfgErr := config.Load()

	// Run all checks with timeout context for Docker
	dockerCtx, cancel := context.WithTimeout(cmd.Context(), dockerPingTimeout)
	result.Add(checkDocker(dockerCtx))
	cancel()
	if ctxErr := cmd.Context().Err(); ctxErr != nil {
		return ctxErr
	}

	composeResult, err := checkDockerCompose(cmd.Context())
	if err != nil {
		return err
	}
	result.Add(composeResult)
	result.Add(checkGit())
	result.Add(checkDeployKeyPermissions())
	result.Add(checkProjectRoot(cfg, cfgErr))
	result.Add(checkAgeKey())
	sopsResult, err := checkSOPS(cmd.Context())
	if err != nil {
		return err
	}
	result.Add(sopsResult)
	result.Add(checkManifestDirectory(cfg))
	result.Add(checkHookSettleDelayFUSE(cfg))
	result.Add(checkRestartBreakerSampling())
	result.Add(checkStateDir())
	result.Add(checkSocketDir())
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
		return fmt.Errorf("%d check(s) failed", result.Failed)
	} else if result.Warned > 0 {
		fmt.Println()
		_, _ = ui.Yellow.Println("Ship can sail, but check warnings.")
	} else {
		fmt.Println()
		_, _ = ui.Green.Println("All systems go! Ready to sail.")
	}
	return nil
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
