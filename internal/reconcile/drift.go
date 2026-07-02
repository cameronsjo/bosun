package reconcile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/log"

	"gopkg.in/yaml.v3"
)

// ComposeProjectLabel is the label Docker Compose v2 sets on managed containers.
const ComposeProjectLabel = "com.docker.compose.project"

// ComposeServiceLabel is the label Docker Compose v2 sets for the service name.
const ComposeServiceLabel = "com.docker.compose.service"

// ActualService represents the observed state of a running container.
type ActualService struct {
	Name          string
	ContainerName string // Full Docker container name (for inspect calls)
	Image         string
	State         string
	Health        string
}

// DriftIgnoreRule defines a pattern for suppressing known drift noise.
// Service supports glob patterns (filepath.Match) and Type can be a specific
// drift type or "*" to match all types.
type DriftIgnoreRule struct {
	Service string `yaml:"service" json:"service"` // Glob pattern matching service name (e.g., "traefik", "*.monitoring")
	Type    string `yaml:"type" json:"type"`       // Drift type to ignore: "missing", "image_mismatch", "unhealthy", or "*"
}

// DriftReport is the result of comparing declared vs actual state.
type DriftReport struct {
	CheckedAt    time.Time
	Items        []DriftItem
	IgnoredCount int // Number of items filtered out by ignore rules
}

// HasDrift returns true if any drift items were detected.
func (r *DriftReport) HasDrift() bool {
	return len(r.Items) > 0
}

// DriftSummaries returns a slice of "service:type" strings for each drift item.
// Suitable for structured log fields where the full list of drifting containers
// needs to be queryable (not just the count).
func (r *DriftReport) DriftSummaries() []string {
	if len(r.Items) == 0 {
		return nil
	}
	summaries := make([]string, len(r.Items))
	for i, item := range r.Items {
		summaries[i] = item.Service + ":" + string(item.Type)
	}
	return summaries
}

// HasCriticalDrift returns true if drift includes missing or unhealthy services.
func (r *DriftReport) HasCriticalDrift() bool {
	for _, item := range r.Items {
		if item.Type == DriftMissing || item.Type == DriftUnhealthy {
			return true
		}
	}
	return false
}

// ErrComposeDirMissing is returned by ExtractDeclaredState when the staging
// compose directory does not exist on disk. This indicates a misconfigured
// staging path and is always treated as a fatal reconcile error — operators
// cannot opt out, because there is no way to distinguish "intentionally empty
// repo" from "wrong path."
var ErrComposeDirMissing = errors.New("staging compose directory does not exist")

// ErrNoDeclaredServices is returned by ExtractDeclaredState when the staging
// compose directory exists but contains no parseable services. This is
// overridable via BOSUN_ALLOW_EMPTY_DECLARED_STATE=true for genuinely empty
// repos (early scaffolding, archive branches).
var ErrNoDeclaredServices = errors.New("no declared services in staging compose directory")

// findComposeCandidates returns the names of immediate child directories of dir
// that themselves contain a compose/ subdirectory. Dot-prefixed children are
// skipped (never legitimate infra roots), and a "compose" entry that is a file
// rather than a directory does not count. Used to suggest a likely
// BOSUN_INFRA_DIR when the configured compose directory is missing (GH#214).
// Results are sorted for deterministic suggestions.
func findComposeCandidates(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var candidates []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		composeInfo, statErr := os.Stat(filepath.Join(dir, entry.Name(), "compose"))
		if statErr == nil && composeInfo.IsDir() {
			candidates = append(candidates, entry.Name())
		}
	}
	sort.Strings(candidates)
	return candidates
}

// ExtractDeclaredState parses rendered compose files from a staging directory
// and returns the list of declared services with their images.
//
// Returns ErrComposeDirMissing if the compose directory does not exist (always
// fatal). Returns ErrNoDeclaredServices if the directory exists but no services
// are declared (overridable). Other errors indicate I/O failures.
func ExtractDeclaredState(stagingDir string) ([]DeclaredService, error) {
	logger := log.Component(log.ComponentReconcile)
	composeDir := filepath.Join(stagingDir, "compose")

	logger.Debug().
		Str(log.FieldPath, composeDir).
		Msg("Preparing to extract declared services from compose files")

	// Distinguish "compose dir missing" (misconfigured) from "compose dir
	// exists but empty" (genuinely empty, possibly intentional). The two
	// failure modes have different remediation paths.
	if _, statErr := os.Stat(composeDir); statErr != nil {
		if os.IsNotExist(statErr) {
			candidates := findComposeCandidates(stagingDir)
			logger.Error().
				Str(log.FieldPath, composeDir).
				Strs("candidate_infra_dirs", candidates).
				Msg("Failed to extract declared state. Reason: compose directory does not exist")
			if len(candidates) > 0 {
				return nil, fmt.Errorf("%w: %s (compose/ found under sibling dir(s): %s)",
					ErrComposeDirMissing, composeDir, strings.Join(candidates, ", "))
			}
			return nil, fmt.Errorf("%w: %s", ErrComposeDirMissing, composeDir)
		}
		logger.Error().Err(statErr).Str(log.FieldPath, composeDir).Msg("Failed to extract declared state. Reason: cannot stat compose directory")
		return nil, fmt.Errorf("stat compose dir: %w", statErr)
	}

	files, err := filepath.Glob(filepath.Join(composeDir, "*.yml"))
	if err != nil {
		logger.Error().Err(err).Str(log.FieldPath, composeDir).Msg("Failed to extract declared state. Reason: cannot glob compose files")
		return nil, fmt.Errorf("glob compose files: %w", err)
	}

	if len(files) == 0 {
		logger.Warn().
			Str(log.FieldPath, composeDir).
			Msg("Failed to extract declared state. Reason: no compose files found")
		return nil, fmt.Errorf("%w: %s", ErrNoDeclaredServices, composeDir)
	}

	logger.Debug().Int("file_count", len(files)).Msg("Found compose files, parsing declared services")

	var declared []DeclaredService
	seen := make(map[string]bool)

	for _, f := range files {
		services, err := extractServicesFromCompose(f)
		if err != nil {
			logger.Warn().
				Str(log.FieldPath, f).
				Err(err).
				Msg("Failed to parse compose file, skipping")
			continue
		}

		for _, svc := range services {
			if !seen[svc.Name] {
				declared = append(declared, svc)
				seen[svc.Name] = true
			}
		}
	}

	if len(declared) == 0 {
		logger.Warn().
			Str(log.FieldPath, composeDir).
			Msg("Failed to extract declared state. Reason: no services declared in any compose files")
		return nil, fmt.Errorf("%w: %s", ErrNoDeclaredServices, composeDir)
	}

	// Sort for deterministic output.
	sort.Slice(declared, func(i, j int) bool {
		return declared[i].Name < declared[j].Name
	})

	logger.Info().Int("service_count", len(declared)).Msg("Successfully extracted declared state from compose files")
	return declared, nil
}

// extractServicesFromCompose parses a single compose YAML file and extracts service names and images.
func extractServicesFromCompose(path string) ([]DeclaredService, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	var compose struct {
		Services map[string]struct {
			Image string `yaml:"image"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}

	var services []DeclaredService
	for name, svc := range compose.Services {
		services = append(services, DeclaredService{
			Name:  name,
			Image: svc.Image,
		})
	}
	return services, nil
}

// CollectActualState queries Docker for running containers filtered by the
// compose project label to scope to bosun-managed services.
func CollectActualState(ctx context.Context, client *docker.Client, projectName string) ([]ActualService, error) {
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)

	logger.Debug().
		Str("project_name", projectName).
		Msg("Preparing to collect actual container state from Docker")

	containers, err := client.ListContainers(ctx, false)
	if err != nil {
		logger.Error().
			Err(err).
			Str("project_name", projectName).
			Msg("Failed to collect actual state. Reason: cannot list containers from Docker")
		return nil, fmt.Errorf("list containers: %w", err)
	}

	logger.Debug().Int("total_containers", len(containers)).Msg("Listed containers, filtering by project")

	var actual []ActualService
	seen := make(map[string]bool)

	for _, c := range containers {
		serviceName, matched := matchContainer(c, projectName)
		if !matched {
			continue
		}

		// Deduplicate by service name (replicas produce multiple containers).
		if seen[serviceName] {
			continue
		}
		seen[serviceName] = true

		actual = append(actual, ActualService{
			Name:          serviceName,
			ContainerName: c.Name,
			Image:         c.Image,
			State:         c.State,
			Health:        c.Health,
		})
	}

	sort.Slice(actual, func(i, j int) bool {
		return actual[i].Name < actual[j].Name
	})

	logger.Info().
		Int("matched_services", len(actual)).
		Int("total_containers", len(containers)).
		Msg("Successfully collected actual container state")

	return actual, nil
}

// matchContainer determines if a container belongs to the given compose project
// and extracts the service name. Uses compose labels (preferred) with name-based
// fallback for containers that lack labels.
func matchContainer(c docker.ContainerInfo, projectName string) (serviceName string, matched bool) {
	// Prefer label-based matching — authoritative and unambiguous.
	if c.Labels != nil {
		labelProject := c.Labels[ComposeProjectLabel]
		labelService := c.Labels[ComposeServiceLabel]

		if labelProject != "" && labelService != "" {
			if projectName == "" || labelProject == projectName {
				return labelService, true
			}
			return "", false
		}
	}

	// Fallback: name-based matching for containers without compose labels.
	if projectName == "" {
		return c.Name, true
	}

	if !strings.HasPrefix(c.Name, projectName+"-") {
		return "", false
	}

	return serviceNameFromContainer(c.Name, projectName), true
}

// serviceNameFromContainer extracts the service name from a container name.
// Docker Compose v2 format: <project>-<service>-<replica>
// This is a fallback for containers without compose labels.
func serviceNameFromContainer(containerName, projectName string) string {
	trimmed := strings.TrimPrefix(containerName, projectName+"-")
	// Remove the trailing replica number (e.g., "-1")
	if idx := strings.LastIndex(trimmed, "-"); idx > 0 {
		suffix := trimmed[idx+1:]
		isNumeric := true
		for _, c := range suffix {
			if c < '0' || c > '9' {
				isNumeric = false
				break
			}
		}
		if isNumeric {
			return trimmed[:idx]
		}
	}
	return trimmed
}

// CompareDrift compares declared services against actual running services
// and returns a drift report identifying any discrepancies.
func CompareDrift(declared []DeclaredService, actual []ActualService) *DriftReport {
	report := &DriftReport{
		CheckedAt: time.Now(),
	}

	// Build lookup of actual services by name.
	actualByName := make(map[string]*ActualService, len(actual))
	for i := range actual {
		actualByName[actual[i].Name] = &actual[i]
	}

	for _, decl := range declared {
		act, exists := actualByName[decl.Name]

		if !exists {
			report.Items = append(report.Items, DriftItem{
				Service:  decl.Name,
				Type:     DriftMissing,
				Declared: decl.Image,
			})
			continue
		}

		// Check if service is running.
		if act.State != "running" {
			report.Items = append(report.Items, DriftItem{
				Service:  decl.Name,
				Type:     DriftMissing,
				Declared: decl.Image,
				Actual:   "state=" + act.State,
			})
			continue
		}

		// Check image mismatch.
		if decl.Image != "" && act.Image != "" && !imagesMatch(decl.Image, act.Image) {
			report.Items = append(report.Items, DriftItem{
				Service:  decl.Name,
				Type:     DriftImageMismatch,
				Declared: decl.Image,
				Actual:   act.Image,
			})
		}

		// Check health status.
		if act.Health == "unhealthy" {
			report.Items = append(report.Items, DriftItem{
				Service: decl.Name,
				Type:    DriftUnhealthy,
			})
		}
	}

	return report
}

// maxHealthOutput is the maximum length of health check output to include in drift items.
// Keeps log lines reasonable while providing enough diagnostic context.
const maxHealthOutput = 200

// EnrichUnhealthyItems inspects unhealthy containers to populate DriftItem.Actual
// with the last health check result (exit code + output). Only calls Docker Inspect
// for containers with DriftUnhealthy type, so the cost scales with the number of
// unhealthy containers, not total containers.
func EnrichUnhealthyItems(ctx context.Context, client *docker.Client, report *DriftReport, actual []ActualService) {
	if !report.HasDrift() {
		return
	}

	logger := log.ComponentCtx(ctx, log.ComponentReconcile)

	// Build service→container name lookup from actual services.
	containerNames := make(map[string]string, len(actual))
	for _, svc := range actual {
		containerNames[svc.Name] = svc.ContainerName
	}

	for i := range report.Items {
		item := &report.Items[i]
		if item.Type != DriftUnhealthy {
			continue
		}

		containerName, ok := containerNames[item.Service]
		if !ok {
			continue
		}

		details, err := client.Inspect(ctx, containerName)
		if err != nil {
			logger.Debug().
				Err(err).
				Str(log.FieldContainer, containerName).
				Msg("Skipping health enrichment. Reason: inspect failed")
			continue
		}

		item.Actual = formatHealthDetail(details)
	}
}

// formatHealthDetail extracts a diagnostic string from container inspect results.
// Format: "failing_streak=N, last_exit=X, output=<truncated output>"
func formatHealthDetail(details *docker.ContainerDetails) string {
	if details.HealthLog == nil {
		return "unhealthy (no health log)"
	}

	result := fmt.Sprintf("failing_streak=%d, last_exit=%d", details.HealthFailingStreak, details.HealthLog.ExitCode)

	if details.HealthLog.Output != "" {
		output := strings.TrimSpace(details.HealthLog.Output)
		if len(output) > maxHealthOutput {
			output = output[:maxHealthOutput-3] + "..."
		}
		result += fmt.Sprintf(", output=%s", output)
	}

	return result
}

// imagesMatch compares two image references, handling tag normalization.
// "nginx" matches "nginx:latest", "nginx:latest" matches "nginx:latest".
func imagesMatch(declared, actual string) bool {
	return normalizeImage(declared) == normalizeImage(actual)
}

// normalizeImage normalizes an image reference by appending :latest if no tag is present.
func normalizeImage(image string) string {
	// Handle digest references (image@sha256:...)
	if strings.Contains(image, "@") {
		return image
	}
	// If no tag, append :latest
	if !strings.Contains(image, ":") {
		return image + ":latest"
	}
	return image
}

// RunDriftCheck performs a full drift check: loads declared state from the
// state file and compares against actual Docker state. Ignore rules filter
// out known drift noise before reporting.
func RunDriftCheck(ctx context.Context, client *docker.Client, stateFile, projectName string, ignoreRules []DriftIgnoreRule) (*DriftReport, error) {
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)
	start := time.Now()

	state := LoadState(stateFile)
	if len(state.DeclaredServices) == 0 {
		return nil, fmt.Errorf("no declared services in state file (has a deployment been completed?)")
	}

	logger.Debug().
		Int("declared_services", len(state.DeclaredServices)).
		Str("project_name", projectName).
		Msg("Starting drift check")

	actual, err := CollectActualState(ctx, client, projectName)
	if err != nil {
		return nil, fmt.Errorf("collect actual state: %w", err)
	}

	report := CompareDrift(state.DeclaredServices, actual)

	// Enrich unhealthy items with last health check output (inspects only drifting containers).
	EnrichUnhealthyItems(ctx, client, report, actual)

	// Filter out items matching ignore rules.
	if len(ignoreRules) > 0 {
		preFilterCount := len(report.Items)
		report.Items = filterIgnoredDrift(report.Items, ignoreRules)
		report.IgnoredCount = preFilterCount - len(report.Items)
		if report.IgnoredCount > 0 {
			logger.Debug().
				Int("ignored_count", report.IgnoredCount).
				Int("remaining_count", len(report.Items)).
				Msg("Filtered drift items by ignore rules")
		}
	}

	logEvent := logger.Info().
		Int("declared_services", len(state.DeclaredServices)).
		Int("actual_services", len(actual)).
		Int("drift_items", len(report.Items)).
		Bool("has_drift", report.HasDrift()).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds())
	if report.HasDrift() {
		logEvent = logEvent.Strs("drift_containers", report.DriftSummaries())
	}
	logEvent.Msg("Drift check completed")

	return report, nil
}
