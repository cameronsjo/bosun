package reconcile

import (
	"context"
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
	Name   string
	Image  string
	State  string
	Health string
}

// DriftReport is the result of comparing declared vs actual state.
type DriftReport struct {
	CheckedAt time.Time
	Items     []DriftItem
}

// HasDrift returns true if any drift items were detected.
func (r *DriftReport) HasDrift() bool {
	return len(r.Items) > 0
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

// ExtractDeclaredState parses rendered compose files from a staging directory
// and returns the list of declared services with their images.
func ExtractDeclaredState(stagingDir string) ([]DeclaredService, error) {
	composeDir := filepath.Join(stagingDir, "compose")

	files, err := filepath.Glob(filepath.Join(composeDir, "*.yml"))
	if err != nil {
		return nil, fmt.Errorf("glob compose files: %w", err)
	}

	// Also check for .yml.tmpl files (pre-chezmoi rendering)
	tmplFiles, err := filepath.Glob(filepath.Join(composeDir, "*.yml.tmpl"))
	if err != nil {
		return nil, fmt.Errorf("glob compose template files: %w", err)
	}
	files = append(files, tmplFiles...)

	if len(files) == 0 {
		return nil, nil
	}

	var declared []DeclaredService
	seen := make(map[string]bool)

	for _, f := range files {
		services, err := extractServicesFromCompose(f)
		if err != nil {
			log.Warn().
				Str(log.FieldComponent, log.ComponentReconcile).
				Str(log.FieldPath, f).
				Err(err).
				Msg("Failed to parse compose file for declared state, skipping")
			continue
		}

		for _, svc := range services {
			if !seen[svc.Name] {
				declared = append(declared, svc)
				seen[svc.Name] = true
			}
		}
	}

	// Sort for deterministic output.
	sort.Slice(declared, func(i, j int) bool {
		return declared[i].Name < declared[j].Name
	})

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
	containers, err := client.ListContainers(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	var actual []ActualService
	for _, c := range containers {
		// If a project name is set, filter by compose project label.
		// We use name-based matching since ContainerInfo doesn't expose labels.
		// Docker Compose v2 names containers as: <project>-<service>-<replica>
		if projectName != "" && !isProjectContainer(c.Name, projectName) {
			continue
		}

		actual = append(actual, ActualService{
			Name:   serviceNameFromContainer(c.Name, projectName),
			Image:  c.Image,
			State:  c.State,
			Health: c.Health,
		})
	}

	sort.Slice(actual, func(i, j int) bool {
		return actual[i].Name < actual[j].Name
	})

	return actual, nil
}

// isProjectContainer checks if a container name belongs to a compose project.
// Docker Compose v2 names containers as: <project>-<service>-<replica>
func isProjectContainer(containerName, projectName string) bool {
	return strings.HasPrefix(containerName, projectName+"-")
}

// serviceNameFromContainer extracts the service name from a container name.
// Docker Compose v2 format: <project>-<service>-<replica>
// If no project name, returns the container name as-is.
func serviceNameFromContainer(containerName, projectName string) string {
	if projectName == "" {
		return containerName
	}

	trimmed := strings.TrimPrefix(containerName, projectName+"-")
	// Remove the trailing replica number (e.g., "-1")
	if idx := strings.LastIndex(trimmed, "-"); idx > 0 {
		suffix := trimmed[idx+1:]
		// Only strip if the suffix is purely numeric (replica count).
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
// state file and compares against actual Docker state.
func RunDriftCheck(ctx context.Context, client *docker.Client, stateFile, projectName string) (*DriftReport, error) {
	state := LoadState(stateFile)
	if len(state.DeclaredServices) == 0 {
		return nil, fmt.Errorf("no declared services in state file (has a deployment been completed?)")
	}

	actual, err := CollectActualState(ctx, client, projectName)
	if err != nil {
		return nil, fmt.Errorf("collect actual state: %w", err)
	}

	report := CompareDrift(state.DeclaredServices, actual)
	return report, nil
}
