package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/cameronsjo/bosun/internal/config"
	"github.com/cameronsjo/bosun/internal/log"
	"github.com/cameronsjo/bosun/internal/ui"
)

// Flag variables for upgrade traefik command.
var (
	upgradeDryRun      bool
	upgradeYes         bool
	upgradeComposePath string
	upgradeDynamicDir  string
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade project configurations to recommended defaults",
	Long: `Upgrade project configurations to incorporate Bosun's recommended defaults.

Subcommands:
  bosun upgrade traefik   Check and apply Traefik security and performance defaults`,
}

var upgradeTraefikCmd = &cobra.Command{
	Use:   "traefik",
	Short: "Apply recommended Traefik defaults (HTTPS, security headers, compression)",
	Long: `Check your Traefik configuration against Bosun's recommended defaults and
interactively apply improvements.

Checks for: HTTPS redirect, security headers, compression, defaultRule,
ACME certificate resolver, and exposedByDefault setting.

By default shows recommendations without applying. Use --yes to apply all.

Examples:
  bosun upgrade traefik              # Show recommendations (dry-run)
  bosun upgrade traefik --yes        # Apply all recommendations
  bosun upgrade traefik --dry-run    # Explicit dry-run mode

Exits non-zero when the compose file cannot be found or parsed, when no prompt
is possible without --yes, or when a fix cannot be applied.`,
	RunE: runUpgradeTraefik,
}

func init() {
	upgradeTraefikCmd.Flags().BoolVar(&upgradeDryRun, "dry-run", false, "Show recommendations without applying")
	upgradeTraefikCmd.Flags().BoolVarP(&upgradeYes, "yes", "y", false, "Apply all recommendations without prompting")
	upgradeTraefikCmd.Flags().StringVar(&upgradeComposePath, "compose", "", "Path to compose file containing Traefik service")
	upgradeTraefikCmd.Flags().StringVar(&upgradeDynamicDir, "dynamic", "", "Path to Traefik dynamic config directory")

	upgradeCmd.AddCommand(upgradeTraefikCmd)
	rootCmd.AddCommand(upgradeCmd)
}

// traefikCheck holds the result of a single Traefik configuration check.
type traefikCheck struct {
	Name        string // "HTTPS Redirect"
	Status      string // "pass", "warn", "missing"
	Description string // human-readable explanation
	Fix         string // the config line/block to add
	FixTarget   string // "command" or "dynamic" — where the fix goes
}

// traefikComposeService represents the Traefik service parsed from a compose file.
type traefikComposeService struct {
	Image   string            `yaml:"image"`
	Command any               `yaml:"command"` // string or []string
	Volumes []string          `yaml:"volumes"`
	Labels  map[string]string `yaml:"labels"`
	Ports   []any             `yaml:"ports"`
}

// commandList returns the Traefik command flags as a string slice,
// handling both string and list YAML formats.
func (s *traefikComposeService) commandList() []string {
	if s.Command == nil {
		return nil
	}
	switch cmd := s.Command.(type) {
	case string:
		return strings.Fields(cmd)
	case []any:
		var result []string
		for _, item := range cmd {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	}
	return nil
}

// hasCommandFlag checks if the Traefik command contains a flag matching the prefix.
func (s *traefikComposeService) hasCommandFlag(prefix string) bool {
	for _, flag := range s.commandList() {
		if strings.HasPrefix(flag, prefix) {
			return true
		}
	}
	return false
}

// findTraefikComposeFile finds the compose file containing a Traefik service.
// Search order: --compose flag, output dir compose files, project root compose files.
func findTraefikComposeFile(cfg *config.Config, composeFlagPath string) (string, error) {
	if composeFlagPath != "" {
		if _, err := os.Stat(composeFlagPath); err != nil {
			return "", fmt.Errorf("compose file not found: %s", composeFlagPath)
		}
		return composeFlagPath, nil
	}

	// Search output dir compose files
	if cfg != nil {
		composeDir := filepath.Join(cfg.OutputDir(), "compose")
		if match := findTraefikInDir(composeDir); match != "" {
			return match, nil
		}
	}

	// Search project root for standard compose file names
	if cfg != nil {
		for _, name := range []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"} {
			path := filepath.Join(cfg.Root, name)
			if containsTraefikService(path) {
				return path, nil
			}
		}

		// Search bosun/ directory
		bosunDir := filepath.Join(cfg.Root, "bosun")
		if match := findTraefikInDir(bosunDir); match != "" {
			return match, nil
		}
	}

	return "", fmt.Errorf("no compose file with Traefik service found")
}

// findTraefikInDir scans a directory for YAML files containing a Traefik service.
func findTraefikInDir(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		path := filepath.Join(dir, name)
		if containsTraefikService(path) {
			return path
		}
	}
	return ""
}

// containsTraefikService checks if a YAML file contains a Traefik service.
func containsTraefikService(path string) bool {
	svc, err := parseTraefikService(path)
	return err == nil && svc != nil
}

// parseTraefikService extracts the Traefik service config from a compose file.
// Returns nil if no Traefik service is found.
func parseTraefikService(composePath string) (*traefikComposeService, error) {
	data, err := os.ReadFile(composePath)
	if err != nil {
		return nil, err
	}

	var compose struct {
		Services map[string]traefikComposeService `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return nil, err
	}

	// Map iteration order is random; walk the names sorted so a compose file with
	// two matching services always yields the same one, and the same one that
	// addCommandFlagsToNode edits.
	names := make([]string, 0, len(compose.Services))
	for name := range compose.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		svc := compose.Services[name]
		if isTraefikService(name, svc.Image) {
			return &svc, nil
		}
	}

	return nil, nil
}

// isTraefikService reports whether a compose service is Traefik, by image or
// by service name.
func isTraefikService(name, image string) bool {
	return strings.HasPrefix(image, "traefik:") || strings.HasPrefix(image, "traefik/") || name == "traefik"
}

// findTraefikDynamicDir locates the dynamic config directory from Traefik volume mounts.
func findTraefikDynamicDir(svc *traefikComposeService, composePath string) string {
	composeDir := filepath.Dir(composePath)
	for _, vol := range svc.Volumes {
		parts := strings.SplitN(vol, ":", 2)
		if len(parts) < 2 {
			continue
		}
		hostPath := parts[0]
		containerPath := parts[1]

		// Look for typical dynamic config mount points
		if strings.Contains(containerPath, "conf.d") ||
			strings.Contains(containerPath, "dynamic") ||
			strings.Contains(containerPath, "rules") {
			// Resolve relative paths against compose file location
			if !filepath.IsAbs(hostPath) {
				hostPath = filepath.Join(composeDir, hostPath)
			}
			if info, err := os.Stat(hostPath); err == nil && info.IsDir() {
				return hostPath
			}
		}
	}
	return ""
}

// checkHTTPSRedirect verifies that HTTP→HTTPS redirect is configured.
func checkHTTPSRedirect(svc *traefikComposeService) traefikCheck {
	check := traefikCheck{
		Name:      "HTTPS Redirect",
		FixTarget: "command",
	}

	if svc.hasCommandFlag("--entrypoints.web.http.redirections.entrypoint.to") {
		check.Status = "pass"
		check.Description = "HTTP→HTTPS redirect is configured"
		return check
	}

	check.Status = "missing"
	check.Description = "HTTP traffic is not redirected to HTTPS"
	check.Fix = "--entrypoints.web.http.redirections.entrypoint.to=websecure\n--entrypoints.web.http.redirections.entrypoint.scheme=https"
	return check
}

// checkExposedByDefault verifies that exposedByDefault is set to false.
func checkExposedByDefault(svc *traefikComposeService) traefikCheck {
	check := traefikCheck{
		Name:      "Exposed By Default",
		FixTarget: "command",
	}

	for _, flag := range svc.commandList() {
		if strings.HasPrefix(flag, "--providers.docker.exposedbydefault=") {
			val := strings.TrimPrefix(flag, "--providers.docker.exposedbydefault=")
			if strings.EqualFold(val, "false") {
				check.Status = "pass"
				check.Description = "Containers are not exposed by default (opt-in via traefik.enable=true)"
				return check
			}
			// Explicitly set to true
			check.Status = "warn"
			check.Description = "exposedByDefault is true — all containers with ports are auto-exposed to Traefik"
			check.Fix = "--providers.docker.exposedbydefault=false"
			return check
		}
	}

	check.Status = "warn"
	check.Description = "exposedByDefault not set — defaults to true, all containers with ports are auto-exposed"
	check.Fix = "--providers.docker.exposedbydefault=false"
	return check
}

// checkDefaultRule verifies that a defaultRule is configured.
// domain is used to suggest a concrete defaultRule fix; falls back to "example.com".
func checkDefaultRule(svc *traefikComposeService, domain string) traefikCheck {
	check := traefikCheck{
		Name:      "Default Rule",
		FixTarget: "command",
	}

	if svc.hasCommandFlag("--providers.docker.defaultRule") {
		check.Status = "pass"
		check.Description = "Default routing rule is configured"
		return check
	}

	if domain == "" {
		domain = "example.com"
	}
	check.Status = "missing"
	check.Description = "No defaultRule — each service must define its own routing rule"
	check.Fix = fmt.Sprintf("--providers.docker.defaultRule=Host(`{{ index .Labels \"bosun.subdomain\" }}.%s`)", domain)
	return check
}

// checkSecurityHeaders verifies that security headers middleware exists in dynamic config.
func checkSecurityHeaders(dynamicDir string) traefikCheck {
	check := traefikCheck{
		Name:      "Security Headers",
		FixTarget: "dynamic",
	}

	if dynamicDir == "" {
		check.Status = "missing"
		check.Description = "No dynamic config directory found — cannot check for security headers middleware"
		check.Fix = securityHeadersMiddleware()
		return check
	}

	if middlewareExistsInDir(dynamicDir, "secure-defaults") {
		check.Status = "pass"
		check.Description = "Security headers middleware (secure-defaults) is configured"
		return check
	}

	check.Status = "missing"
	check.Description = "No secure-defaults middleware — responses lack security headers (HSTS, nosniff, etc.)"
	check.Fix = securityHeadersMiddleware()
	return check
}

// checkCompression verifies that compression middleware exists in dynamic config.
func checkCompression(dynamicDir string) traefikCheck {
	check := traefikCheck{
		Name:      "Compression",
		FixTarget: "dynamic",
	}

	if dynamicDir == "" {
		check.Status = "missing"
		check.Description = "No dynamic config directory found — cannot check for compression middleware"
		check.Fix = compressionMiddleware()
		return check
	}

	if middlewareExistsInDir(dynamicDir, "default-compress") {
		check.Status = "pass"
		check.Description = "Compression middleware (default-compress) is configured"
		return check
	}

	check.Status = "missing"
	check.Description = "No default-compress middleware — responses are not compressed"
	check.Fix = compressionMiddleware()
	return check
}

// checkACMEResolver verifies that an ACME certificate resolver is configured.
func checkACMEResolver(svc *traefikComposeService) traefikCheck {
	check := traefikCheck{
		Name:      "ACME Resolver",
		FixTarget: "command",
	}

	if svc.hasCommandFlag("--certificatesresolvers.") {
		check.Status = "pass"
		check.Description = "ACME certificate resolver is configured"
		return check
	}

	check.Status = "missing"
	check.Description = "No ACME resolver — TLS certificates must be managed manually"
	check.Fix = "--certificatesresolvers.letsencrypt.acme.email=you@example.com\n--certificatesresolvers.letsencrypt.acme.storage=/letsencrypt/acme.json\n--certificatesresolvers.letsencrypt.acme.httpchallenge.entrypoint=web"
	return check
}

// middlewareExistsInDir scans YAML files in a directory for a middleware name.
func middlewareExistsInDir(dir, middlewareName string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}

		// Parse the YAML and look for the middleware name in http.middlewares
		var dynCfg struct {
			HTTP struct {
				Middlewares map[string]any `yaml:"middlewares"`
			} `yaml:"http"`
		}
		if err := yaml.Unmarshal(data, &dynCfg); err != nil {
			// Fall back to simple string search for template files
			if strings.Contains(string(data), middlewareName) {
				return true
			}
			continue
		}

		if _, exists := dynCfg.HTTP.Middlewares[middlewareName]; exists {
			return true
		}
	}

	return false
}

// securityHeadersMiddleware returns the recommended security headers middleware YAML.
func securityHeadersMiddleware() string {
	return `# Add to Traefik dynamic config (e.g., conf.d/middlewares.yml)
http:
  middlewares:
    secure-defaults:
      headers:
        stsSeconds: 31536000
        stsIncludeSubdomains: true
        stsPreload: true
        contentTypeNosniff: true
        frameDeny: true
        browserXssFilter: true
        referrerPolicy: strict-origin-when-cross-origin
        permissionsPolicy: camera=(), microphone=(), geolocation=()`
}

// compressionMiddleware returns the recommended compression middleware YAML.
func compressionMiddleware() string {
	return `# Add to Traefik dynamic config (e.g., conf.d/middlewares.yml)
http:
  middlewares:
    default-compress:
      compress:
        minResponseBodyBytes: 1024`
}

func runUpgradeTraefik(cmd *cobra.Command, args []string) error {
	// A failure here is not a usage mistake; the usage text would only bury it.
	cmd.SilenceUsage = true

	upgradeLogger := log.Component("upgrade-traefik")

	cfg, err := config.Load()
	if err != nil {
		upgradeLogger.Debug().Err(err).Msg("Could not load bosun config, continuing without project config")
	}

	_, _ = ui.Blue.Println("Checking Traefik configuration...")
	fmt.Println()

	// Find Traefik compose file
	composePath, err := findTraefikComposeFile(cfg, upgradeComposePath)
	if err != nil {
		_, _ = ui.Yellow.Printf("  ! %v\n", err)
		fmt.Println()
		fmt.Println("Specify the compose file with --compose <path>")
		return fmt.Errorf("find Traefik compose file: %w", err)
	}

	_, _ = ui.Green.Printf("  Found Traefik in: %s\n", composePath)

	// Check for template files
	isTemplate := strings.HasSuffix(composePath, ".tmpl") || fileContainsGoTemplate(composePath)
	if isTemplate {
		_, _ = ui.Yellow.Println("  ! Compose file is a template — fixes will be shown but not auto-applied")
	}

	// Parse Traefik service
	svc, err := parseTraefikService(composePath)
	if err != nil || svc == nil {
		if isTemplate {
			_, _ = ui.Yellow.Println("  ! Could not parse template file as plain YAML — showing generic recommendations")
			fmt.Println()
			showGenericRecommendations()
			return nil
		}
		_, _ = ui.Red.Println("  x Failed to parse Traefik service from compose file")
		if err != nil {
			return fmt.Errorf("parse Traefik service from %s: %w", composePath, err)
		}
		return fmt.Errorf("no Traefik service (image traefik or a service named traefik) in %s", composePath)
	}

	// Find dynamic config directory
	dynamicDir := upgradeDynamicDir
	if dynamicDir == "" {
		dynamicDir = findTraefikDynamicDir(svc, composePath)
	}

	if dynamicDir != "" {
		fmt.Printf("  Dynamic config: %s\n", dynamicDir)
	}

	fmt.Println()

	// Run all checks
	checks := []traefikCheck{
		checkHTTPSRedirect(svc),
		checkExposedByDefault(svc),
		checkDefaultRule(svc, configDomain(cfg)),
		checkSecurityHeaders(dynamicDir),
		checkCompression(dynamicDir),
		checkACMEResolver(svc),
	}

	// Display results
	var passed, warned, missing int
	for _, c := range checks {
		switch c.Status {
		case "pass":
			passed++
			_, _ = ui.Green.Printf("  * %s: %s\n", c.Name, c.Description)
		case "warn":
			warned++
			_, _ = ui.Yellow.Printf("  ! %s: %s\n", c.Name, c.Description)
		case "missing":
			missing++
			_, _ = ui.Red.Printf("  x %s: %s\n", c.Name, c.Description)
		}
	}

	// Summary
	fmt.Println()
	fmt.Printf("  Results: ")
	_, _ = ui.Green.Printf("%d passed", passed)
	fmt.Printf(", ")
	_, _ = ui.Yellow.Printf("%d warnings", warned)
	fmt.Printf(", ")
	_, _ = ui.Red.Printf("%d missing\n", missing)

	if warned == 0 && missing == 0 {
		fmt.Println()
		_, _ = ui.Green.Println("All Traefik defaults are in place!")
		return nil
	}

	// Show fixes
	fmt.Println()
	needsFix := collectFixes(checks)
	if len(needsFix) == 0 {
		return nil
	}

	if upgradeDryRun || isTemplate {
		showFixes(needsFix, isTemplate)
		return nil
	}

	if upgradeYes {
		return applyFixes(needsFix, svc, composePath, dynamicDir)
	}

	// Interactive mode
	showFixes(needsFix, false)
	fmt.Println()
	apply, err := promptYesNo("Apply these fixes?")
	if err != nil {
		_, _ = ui.Yellow.Printf("  ! %v\n", err)
		return fmt.Errorf("prompt to apply fixes: %w", err)
	}
	if !apply {
		fmt.Println("No changes made.")
		return nil
	}
	return applyFixes(needsFix, svc, composePath, dynamicDir)
}

// collectFixes gathers checks that need fixing.
func collectFixes(checks []traefikCheck) []traefikCheck {
	var fixes []traefikCheck
	for _, c := range checks {
		if c.Status == "warn" || c.Status == "missing" {
			fixes = append(fixes, c)
		}
	}
	return fixes
}

// showFixes displays the recommended fixes.
func showFixes(fixes []traefikCheck, isTemplate bool) {
	_, _ = ui.Blue.Println("Recommended fixes:")
	fmt.Println()

	// Group by target
	var commandFixes, dynamicFixes []traefikCheck
	for _, f := range fixes {
		if f.FixTarget == "command" {
			commandFixes = append(commandFixes, f)
		} else {
			dynamicFixes = append(dynamicFixes, f)
		}
	}

	if len(commandFixes) > 0 {
		fmt.Println("  Add to Traefik command flags:")
		for _, f := range commandFixes {
			for _, line := range strings.Split(f.Fix, "\n") {
				_, _ = ui.Yellow.Printf("    + %s\n", line)
			}
		}
		fmt.Println()
	}

	if len(dynamicFixes) > 0 {
		fmt.Println("  Add to Traefik dynamic config:")
		for _, f := range dynamicFixes {
			for _, line := range strings.Split(f.Fix, "\n") {
				_, _ = ui.Yellow.Printf("    %s\n", line)
			}
			fmt.Println()
		}
	}

	if isTemplate {
		_, _ = ui.Yellow.Println("  Template file detected — review and apply these changes manually.")
	}
}

// showGenericRecommendations displays Traefik best practices when the compose
// file cannot be parsed (e.g., Go template files with {{ }} syntax).
func showGenericRecommendations() {
	_, _ = ui.Blue.Println("Recommended Traefik configuration:")
	fmt.Println()
	fmt.Println("  Command flags:")
	_, _ = ui.Yellow.Println("    + --entrypoints.web.http.redirections.entrypoint.to=websecure")
	_, _ = ui.Yellow.Println("    + --entrypoints.web.http.redirections.entrypoint.scheme=https")
	_, _ = ui.Yellow.Println("    + --providers.docker.exposedbydefault=false")
	_, _ = ui.Yellow.Println("    + --providers.docker.defaultRule=Host(`{{ normalize .Name }}.<domain>`)")
	_, _ = ui.Yellow.Println("    + --certificatesresolvers.letsencrypt.acme.httpchallenge.entrypoint=web")
	fmt.Println()
	fmt.Println("  Dynamic config (conf.d/middlewares.yml):")
	_, _ = ui.Yellow.Println("    " + strings.ReplaceAll(securityHeadersMiddleware(), "\n", "\n    "))
	fmt.Println()
	_, _ = ui.Yellow.Println("    " + strings.ReplaceAll(compressionMiddleware(), "\n", "\n    "))
	fmt.Println()
	_, _ = ui.Yellow.Println("  Review your template file and apply these changes manually.")
	fmt.Println("  See: bosun upgrade traefik --help")
}

// applyFixes writes the recommended fixes to the compose and dynamic config
// files. It returns every failure joined, after attempting both targets.
func applyFixes(fixes []traefikCheck, svc *traefikComposeService, composePath, dynamicDir string) error {
	var commandFlags []string
	var dynamicFixes []traefikCheck
	var applyErrs []error

	for _, f := range fixes {
		if f.FixTarget == "command" {
			commandFlags = append(commandFlags, strings.Split(f.Fix, "\n")...)
		} else {
			dynamicFixes = append(dynamicFixes, f)
		}
	}

	// Apply command flag fixes to compose file
	if len(commandFlags) > 0 {
		if err := applyCommandFixes(composePath, svc, commandFlags); err != nil {
			_, _ = ui.Red.Printf("  x Failed to update compose file: %v\n", err)
			applyErrs = append(applyErrs, fmt.Errorf("update compose file %s: %w", composePath, err))
		} else {
			_, _ = ui.Green.Printf("  * Updated %s with %d command flags\n", composePath, len(commandFlags))
		}
	}

	// Apply dynamic config fixes
	if len(dynamicFixes) > 0 {
		if dynamicDir == "" {
			_, _ = ui.Yellow.Println("  ! No dynamic config directory — showing fixes for manual application")
			for _, f := range dynamicFixes {
				fmt.Println(f.Fix)
			}
		} else {
			if err := applyDynamicFixes(dynamicDir, dynamicFixes); err != nil {
				_, _ = ui.Red.Printf("  x Failed to update dynamic config: %v\n", err)
				applyErrs = append(applyErrs, fmt.Errorf("update dynamic config in %s: %w", dynamicDir, err))
			} else {
				_, _ = ui.Green.Printf("  * Updated dynamic config in %s\n", dynamicDir)
			}
		}
	}

	fmt.Println()
	if len(applyErrs) > 0 {
		_, _ = ui.Yellow.Println("Some fixes could not be applied. Review the errors above and apply manually.")
		return errors.Join(applyErrs...)
	}
	_, _ = ui.Green.Println("Fixes applied! Restart Traefik to pick up changes.")
	return nil
}

// applyCommandFixes adds missing command flags to the Traefik service in a compose file.
func applyCommandFixes(composePath string, _ *traefikComposeService, flags []string) error {
	data, err := os.ReadFile(composePath)
	if err != nil {
		return err
	}

	// Parse as raw YAML to preserve structure
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse compose file: %w", err)
	}

	// Find the traefik service and its command node
	if err := addCommandFlagsToNode(&doc, flags); err != nil {
		return err
	}

	output, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshal compose file: %w", err)
	}

	return writeFilePreservePerms(composePath, output)
}

// addCommandFlagsToNode adds command flags to the Traefik service's command node.
func addCommandFlagsToNode(doc *yaml.Node, flags []string) error {
	// Navigate: root document → mapping → services → mapping → traefik → mapping → command
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return fmt.Errorf("invalid document structure")
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("root is not a mapping")
	}

	// Find "services" key
	servicesNode := findMapValue(root, "services")
	if servicesNode == nil {
		return fmt.Errorf("no 'services' key found")
	}

	// Find the Traefik service by image or name, as parseTraefikService does.
	traefikNode := findTraefikServiceNode(servicesNode)
	if traefikNode == nil {
		return fmt.Errorf("no traefik service found (image traefik or a service named traefik)")
	}

	// Find or create "command" key
	commandNode := findMapValue(traefikNode, "command")
	if commandNode == nil {
		// Add command as a sequence
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: "command"}
		valueNode := &yaml.Node{Kind: yaml.SequenceNode}
		mergeCommandFlags(valueNode, flags)
		traefikNode.Content = append(traefikNode.Content, keyNode, valueNode)
		return nil
	}

	// Merge flags into the existing command
	if commandNode.Kind == yaml.SequenceNode {
		mergeCommandFlags(commandNode, flags)
		return nil
	}

	// If command is a scalar string, convert to sequence
	if commandNode.Kind == yaml.ScalarNode {
		existing := strings.Fields(commandNode.Value)
		commandNode.Kind = yaml.SequenceNode
		commandNode.Value = ""
		commandNode.Tag = ""
		commandNode.Content = nil
		for _, item := range existing {
			commandNode.Content = append(commandNode.Content, &yaml.Node{
				Kind:  yaml.ScalarNode,
				Value: item,
			})
		}
		mergeCommandFlags(commandNode, flags)
		return nil
	}

	return fmt.Errorf("unsupported command format")
}

// findTraefikServiceNode returns the Traefik service in a compose services
// mapping, matched by image or name like parseTraefikService. With several
// matches it returns the one with the lowest name, so both functions agree.
func findTraefikServiceNode(services *yaml.Node) *yaml.Node {
	if services.Kind != yaml.MappingNode {
		return nil
	}

	var best *yaml.Node
	bestName := ""
	for i := 0; i+1 < len(services.Content); i += 2 {
		name := services.Content[i].Value
		node := services.Content[i+1]

		image := ""
		if imageNode := findMapValue(node, "image"); imageNode != nil {
			image = imageNode.Value
		}
		if !isTraefikService(name, image) {
			continue
		}
		if best == nil || name < bestName {
			best, bestName = node, name
		}
	}
	return best
}

// commandFlagKey returns the part of a command flag before its value, such as
// --providers.docker.exposedbydefault for --providers.docker.exposedbydefault=true.
func commandFlagKey(flag string) string {
	key, _, _ := strings.Cut(flag, "=")
	return key
}

// mergeCommandFlags applies flags to a command sequence. A flag whose name is
// already present (ignoring case) has its value replaced in place, so a fix
// never leaves two settings for one option; a flag that is absent is appended.
func mergeCommandFlags(command *yaml.Node, flags []string) {
	for _, flag := range flags {
		key := commandFlagKey(flag)
		replaced := false
		for _, item := range command.Content {
			if item.Kind == yaml.ScalarNode && strings.EqualFold(commandFlagKey(item.Value), key) {
				item.Value = flag
				replaced = true
			}
		}
		if !replaced {
			command.Content = append(command.Content, &yaml.Node{
				Kind:  yaml.ScalarNode,
				Value: flag,
			})
		}
	}
}

// findMapValue finds a value node in a YAML mapping by key.
func findMapValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// collectFixMiddlewares parses the middleware definitions out of the dynamic
// fixes. A fix whose YAML does not parse is skipped.
func collectFixMiddlewares(fixes []traefikCheck) map[string]any {
	middlewares := make(map[string]any)

	for _, f := range fixes {
		// Parse the fix YAML to extract middleware definitions
		var cfg struct {
			HTTP struct {
				Middlewares map[string]any `yaml:"middlewares"`
			} `yaml:"http"`
		}

		// Strip comment lines before parsing
		var yamlLines []string
		for _, line := range strings.Split(f.Fix, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "#") {
				yamlLines = append(yamlLines, line)
			}
		}

		if err := yaml.Unmarshal([]byte(strings.Join(yamlLines, "\n")), &cfg); err != nil {
			dynLogger := log.Component("upgrade-traefik")
			dynLogger.Debug().Err(err).Str("check", f.Name).Msg("Failed to parse fix YAML for dynamic config")
			continue
		}
		for name, mw := range cfg.HTTP.Middlewares {
			middlewares[name] = mw
		}
	}

	return middlewares
}

// middlewaresTargetFile picks the file that receives new middlewares:
// middlewares.yml when it exists, else the first YAML file in the directory
// that already defines http.middlewares, else a new middlewares.yml.
func middlewaresTargetFile(dynamicDir string) string {
	defaultFile := filepath.Join(dynamicDir, "middlewares.yml")
	if _, err := os.Stat(defaultFile); err == nil {
		return defaultFile
	}

	entries, err := os.ReadDir(dynamicDir)
	if err != nil {
		return defaultFile
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}

		path := filepath.Join(dynamicDir, name)
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		var probe struct {
			HTTP struct {
				Middlewares map[string]any `yaml:"middlewares"`
			} `yaml:"http"`
		}
		if yaml.Unmarshal(data, &probe) == nil && probe.HTTP.Middlewares != nil {
			return path
		}
	}
	return defaultFile
}

// childMapping returns the mapping stored under key in parent, creating it when
// the key is absent or holds null. A key that holds anything else is an error:
// the caller must not replace configuration it does not understand.
func childMapping(parent *yaml.Node, key string) (*yaml.Node, error) {
	value := findMapValue(parent, key)
	if value == nil {
		value = &yaml.Node{Kind: yaml.MappingNode}
		parent.Content = append(parent.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, value)
		return value, nil
	}

	if value.Kind == yaml.ScalarNode && value.Tag == "!!null" {
		value.Kind = yaml.MappingNode
		value.Tag = ""
		value.Value = ""
		value.Style = 0
		return value, nil
	}
	if value.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%q is not a mapping", key)
	}
	return value, nil
}

// dynamicMiddlewaresNode returns the http.middlewares mapping of a parsed
// dynamic config, creating the parts that are missing.
func dynamicMiddlewaresNode(doc *yaml.Node) (*yaml.Node, error) {
	if doc.Kind == 0 {
		doc.Kind = yaml.DocumentNode
	}
	if len(doc.Content) == 0 {
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	}

	root := doc.Content[0]
	if root.Kind == yaml.ScalarNode && root.Tag == "!!null" {
		root.Kind = yaml.MappingNode
		root.Tag = ""
		root.Value = ""
	}
	if root.Kind != yaml.MappingNode {
		return nil, errors.New("top level is not a mapping")
	}

	httpNode, err := childMapping(root, "http")
	if err != nil {
		return nil, err
	}
	return childMapping(httpNode, "middlewares")
}

// applyDynamicFixes adds the recommended middlewares to the dynamic config
// directory. It edits the file as a YAML tree, so routers, services, tls,
// other middlewares and comments are kept, and it refuses to touch a file it
// cannot parse rather than overwrite it.
func applyDynamicFixes(dynamicDir string, fixes []traefikCheck) error {
	middlewares := collectFixMiddlewares(fixes)
	if len(middlewares) == 0 {
		return nil
	}

	targetFile := middlewaresTargetFile(dynamicDir)

	var doc yaml.Node
	existing, err := os.ReadFile(targetFile)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(existing, &doc); err != nil {
			return fmt.Errorf("parse %s: %w", targetFile, err)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("read %s: %w", targetFile, err)
	}

	middlewaresNode, err := dynamicMiddlewaresNode(&doc)
	if err != nil {
		return fmt.Errorf("%s: %w", targetFile, err)
	}

	names := make([]string, 0, len(middlewares))
	for name := range middlewares {
		names = append(names, name)
	}
	sort.Strings(names)

	added := false
	for _, name := range names {
		if findMapValue(middlewaresNode, name) != nil {
			continue
		}
		var value yaml.Node
		if err := value.Encode(middlewares[name]); err != nil {
			return fmt.Errorf("encode middleware %s: %w", name, err)
		}
		middlewaresNode.Content = append(middlewaresNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: name}, &value)
		added = true
	}
	if !added {
		return nil
	}

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(&doc); err != nil {
		return fmt.Errorf("marshal %s: %w", targetFile, err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("marshal %s: %w", targetFile, err)
	}

	return writeFilePreservePerms(targetFile, buf.Bytes())
}

// writeFilePreservePerms writes data to a file, preserving existing permissions.
// Falls back to 0644 if the file doesn't exist or can't be stat'd.
func writeFilePreservePerms(path string, data []byte) error {
	mode := os.FileMode(0644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	return os.WriteFile(path, data, mode)
}

// configDomain returns the domain from config, or empty string if config is nil.
func configDomain(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return cfg.Domain()
}

// fileContainsGoTemplate checks if a file contains Go template syntax.
func fileContainsGoTemplate(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	content := string(data)
	return strings.Contains(content, "{{") && strings.Contains(content, "}}")
}
