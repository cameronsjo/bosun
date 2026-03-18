package manifest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/cameronsjo/bosun/internal/log"
)

// ErrPathTraversal indicates an attempted path traversal attack.
var ErrPathTraversal = errors.New("path traversal detected")

// ErrCircularInclude indicates a circular include was detected.
var ErrCircularInclude = errors.New("circular include detected")

// validatePathWithinDir checks that a joined path stays within the base directory.
// Returns the cleaned absolute path or an error if path traversal is detected.
func validatePathWithinDir(baseDir, relativePath string) (string, error) {
	// Clean and resolve the base directory to absolute path
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("resolve base directory: %w", err)
	}
	absBase = filepath.Clean(absBase)

	// Join and clean the full path
	fullPath := filepath.Join(absBase, relativePath)
	fullPath = filepath.Clean(fullPath)

	// Verify the path is within the base directory
	// Add separator to prevent prefix matching issues (e.g., /foo vs /foobar)
	if !strings.HasPrefix(fullPath+string(filepath.Separator), absBase+string(filepath.Separator)) &&
		fullPath != absBase {
		return "", fmt.Errorf("%s: %w", relativePath, ErrPathTraversal)
	}

	return fullPath, nil
}

// RenderService renders a service manifest into compose/traefik/gatus outputs.
func RenderService(manifest *ServiceManifest, provisionsDir string) (*RenderOutput, error) {
	logger := log.Component(log.ComponentManifest)
	logger.Debug().
		Str(log.FieldOperation, "render_service").
		Str("service", manifest.Name).
		Int("provision_count", len(manifest.Provisions)).
		Msg("Rendering service manifest")

	output := NewRenderOutput()

	// Build variables from config + name
	variables := make(map[string]any)
	for k, v := range manifest.Config {
		variables[k] = v
	}
	variables["name"] = manifest.Name

	// Handle raw passthrough mode
	if manifest.Type == "raw" {
		if manifest.Compose != nil {
			output.Target(TargetCompose)["services"] = manifest.Compose
		}
		logger.Debug().Str("service", manifest.Name).Msg("Service rendered as raw passthrough")
		return output, nil
	}

	// Load and merge provisions
	for _, provisionName := range manifest.Provisions {
		logger.Debug().Str("provision", provisionName).Str("service", manifest.Name).Msg("Loading provision")
		provision, err := LoadProvision(provisionName, variables, provisionsDir)
		if err != nil {
			logger.Error().Err(err).Str("provision", provisionName).Str("service", manifest.Name).Msg("Failed to load provision")
			return nil, fmt.Errorf("load provision %s: %w", provisionName, err)
		}
		mergeProvision(output, provision)
	}

	// Handle 'needs' shorthand for common dependencies
	for _, need := range manifest.Needs {
		defaults, hasDefaults := SidecarDefaults[need]
		if !hasDefaults {
			continue
		}

		if !ProvisionExists(need, provisionsDir) {
			continue
		}

		// Build sidecar variables: defaults + config + overrides
		sidecarVars := make(map[string]any)
		sidecarVars["name"] = manifest.Name
		sidecarVars["sidecar"] = need

		// Apply defaults (may contain ${name} references that need re-interpolation)
		for k, v := range defaults {
			if s, ok := v.(string); ok {
				// Re-interpolate default values with current variables
				interpolated, err := Interpolate(s, variables)
				if err == nil {
					sidecarVars[k] = interpolated
				} else {
					sidecarVars[k] = v
				}
			} else {
				sidecarVars[k] = v
			}
		}

		// Apply config overrides
		for k, v := range manifest.Config {
			sidecarVars[k] = v
		}

		provision, err := LoadProvision(need, sidecarVars, provisionsDir)
		if err != nil {
			return nil, fmt.Errorf("load need %s: %w", need, err)
		}
		mergeProvision(output, provision)
	}

	// Handle sidecar services with explicit config
	for sidecarType, sidecarConfig := range manifest.Services {
		sidecarVars := make(map[string]any)
		sidecarVars["name"] = manifest.Name
		sidecarVars["sidecar"] = sidecarType

		// Apply sidecar-specific config
		for k, v := range sidecarConfig {
			sidecarVars[k] = v
		}

		// Apply manifest config overrides
		for k, v := range manifest.Config {
			sidecarVars[k] = v
		}

		provision, err := LoadProvision(sidecarType, sidecarVars, provisionsDir)
		if err != nil {
			return nil, fmt.Errorf("load sidecar %s: %w", sidecarType, err)
		}
		mergeProvision(output, provision)
	}

	// Apply compose overrides from manifest (allows app-specific customization)
	if manifest.Compose != nil {
		logger.Debug().Str("service", manifest.Name).Msg("Applying compose overrides")
		// Interpolate variables in the compose override
		composeYAML, err := yaml.Marshal(manifest.Compose)
		if err != nil {
			return nil, fmt.Errorf("marshal compose override: %w", err)
		}

		interpolated, err := Interpolate(string(composeYAML), variables)
		if err != nil {
			logger.Error().Err(err).Str("service", manifest.Name).Msg("Failed to interpolate compose override")
			return nil, fmt.Errorf("interpolate compose override: %w", err)
		}

		var composeOverride map[string]any
		if err := yaml.Unmarshal([]byte(interpolated), &composeOverride); err != nil {
			return nil, fmt.Errorf("parse compose override: %w", err)
		}

		output.Targets[TargetCompose] = DeepMerge(output.Target(TargetCompose), composeOverride)
	}

	logger.Debug().Str("service", manifest.Name).Msg("Service rendering completed")
	return output, nil
}

// mergeProvision merges a provision's outputs into the render output.
func mergeProvision(output *RenderOutput, provision *Provision) {
	for name, content := range provision.Targets {
		if content != nil {
			output.Targets[name] = DeepMerge(output.Target(name), content)
		}
	}
}

// RenderStack renders a stack file into compose/traefik/gatus outputs.
func RenderStack(stackPath, provisionsDir, servicesDir string, valuesOverlay map[string]any) (*RenderOutput, error) {
	logger := log.Component(log.ComponentManifest)
	logger.Info().
		Str(log.FieldOperation, "render_stack").
		Str(log.FieldPath, stackPath).
		Msg("Rendering stack")

	stackContent, err := os.ReadFile(stackPath)
	if err != nil {
		logger.Error().Err(err).Str(log.FieldPath, stackPath).Msg("Failed to read stack file")
		return nil, fmt.Errorf("read stack file: %w", err)
	}

	// Validate apiVersion if present (soft validation for backwards compatibility)
	meta, err := ValidateManifest(stackContent)
	if err != nil {
		return nil, fmt.Errorf("validate stack: %w", err)
	}

	// Warn if manifest is unversioned
	if meta.APIVersion == "" {
		log.Warn().
			Str(log.FieldComponent, log.ComponentManifest).
			Str("stack", stackPath).
			Msg("Stack is missing apiVersion field (run 'bosun migrate' to update)")
	} else if meta.Kind != "" && meta.Kind != KindStack {
		log.Warn().
			Str(log.FieldComponent, log.ComponentManifest).
			Str("stack", stackPath).
			Str("kind", meta.Kind).
			Str("expected", KindStack).
			Msg("Stack has unexpected kind")
	}

	var stack Stack
	if err := yaml.Unmarshal(stackContent, &stack); err != nil {
		return nil, fmt.Errorf("parse stack file: %w", err)
	}

	output := NewRenderOutput()

	for _, serviceFile := range stack.Include {
		// Validate path to prevent path traversal attacks
		servicePath, err := validatePathWithinDir(servicesDir, serviceFile)
		if err != nil {
			logger.Error().Err(err).Str("service_file", serviceFile).Msg("Path validation failed")
			return nil, fmt.Errorf("validate service path %s: %w", serviceFile, err)
		}

		logger.Debug().Str("service_file", serviceFile).Msg("Processing service include")

		serviceContent, err := os.ReadFile(servicePath)
		if err != nil {
			logger.Error().Err(err).Str(log.FieldPath, servicePath).Msg("Failed to read service file")
			return nil, fmt.Errorf("read service %s: %w", serviceFile, err)
		}

		// Validate apiVersion if present (soft validation for backwards compatibility)
		serviceMeta, err := ValidateManifest(serviceContent)
		if err != nil {
			return nil, fmt.Errorf("validate service %s: %w", serviceFile, err)
		}

		// Warn if manifest is unversioned
		if serviceMeta.APIVersion == "" {
			log.Warn().
				Str(log.FieldComponent, log.ComponentManifest).
				Str("service", serviceFile).
				Msg("Service is missing apiVersion field (run 'bosun migrate' to update)")
		} else if serviceMeta.Kind != "" && serviceMeta.Kind != KindService {
			log.Warn().
				Str(log.FieldComponent, log.ComponentManifest).
				Str("service", serviceFile).
				Str("kind", serviceMeta.Kind).
				Str("expected", KindService).
				Msg("Service has unexpected kind")
		}

		var manifest ServiceManifest
		if err := yaml.Unmarshal(serviceContent, &manifest); err != nil {
			return nil, fmt.Errorf("parse service %s: %w", serviceFile, err)
		}

		// Apply values overlay to service config
		if len(valuesOverlay) > 0 {
			if manifest.Config == nil {
				manifest.Config = make(map[string]any)
			}
			manifest.Config = DeepMerge(manifest.Config, valuesOverlay)
		}

		serviceOutput, err := RenderService(&manifest, provisionsDir)
		if err != nil {
			logger.Error().Err(err).Str("service", manifest.Name).Msg("Failed to render service")
			return nil, fmt.Errorf("render service %s: %w", manifest.Name, err)
		}

		for name, content := range serviceOutput.Targets {
			if content != nil {
				output.Targets[name] = DeepMerge(output.Target(name), content)
			}
		}
	}

	// Merge network definitions from stack (don't overwrite service networks)
	if stack.Networks != nil {
		compose := output.Target(TargetCompose)
		if existing, ok := compose["networks"].(map[string]any); ok {
			compose["networks"] = DeepMerge(existing, stack.Networks)
		} else {
			compose["networks"] = stack.Networks
		}
	}

	logger.Info().
		Str(log.FieldOperation, "render_stack").
		Int("service_count", len(stack.Include)).
		Msg("Stack rendering completed")

	return output, nil
}

// WriteOutputs writes rendered outputs to files in the output directory.
// Iterates registered targets in sorted order for reproducible output.
// Unregistered targets are logged as warnings and skipped.
func WriteOutputs(output *RenderOutput, outputDir, stackName string) error {
	logger := log.Component(log.ComponentManifest)

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	// Write registered targets in sorted order
	for _, name := range TargetNames() {
		cfg, registered := TargetRegistry[name]
		if !registered {
			continue
		}

		// Validate target directory stays within outputDir to prevent path traversal
		targetDir, err := validatePathWithinDir(outputDir, cfg.Dir)
		if err != nil {
			return fmt.Errorf("resolve %s directory: %w", name, err)
		}

		outputPath := filepath.Join(targetDir, cfg.Filename(stackName))

		content := output.Targets[name]
		if len(content) == 0 {
			// Clean up stale file if the target is now empty
			if err := os.Remove(outputPath); err == nil {
				logger.Info().Str("target", name).Str(log.FieldPath, outputPath).Msg("Removed stale target file")
			}
			continue
		}

		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return fmt.Errorf("create %s directory: %w", name, err)
		}

		data, err := yaml.Marshal(content)
		if err != nil {
			return fmt.Errorf("marshal %s output: %w", name, err)
		}

		if err := os.WriteFile(outputPath, data, 0644); err != nil {
			return fmt.Errorf("write %s output: %w", name, err)
		}

		fmt.Printf("Wrote: %s\n", outputPath)
	}

	// Warn about unregistered targets
	for name := range output.Targets {
		if _, registered := TargetRegistry[name]; !registered {
			logger.Warn().
				Str("target", name).
				Msg("Skipping unregistered target in WriteOutputs")
		}
	}

	return nil
}

// RenderToYAML renders an output to YAML string for dry-run display.
// Includes all targets (registered and unregistered) in sorted order
// for diagnostic visibility.
func RenderToYAML(output *RenderOutput) (string, error) {
	// Collect all target names and sort for deterministic output
	names := make([]string, 0, len(output.Targets))
	for name := range output.Targets {
		names = append(names, name)
	}
	sort.Strings(names)

	combined := make(map[string]any, len(names))
	for _, name := range names {
		combined[name] = output.Targets[name]
	}

	data, err := yaml.Marshal(combined)
	if err != nil {
		return "", fmt.Errorf("marshal output: %w", err)
	}

	return string(data), nil
}

// LoadServiceManifest loads a service manifest from a file.
func LoadServiceManifest(path string) (*ServiceManifest, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	// Validate apiVersion if present (soft validation for backwards compatibility)
	meta, err := ValidateManifest(content)
	if err != nil {
		return nil, fmt.Errorf("validate manifest: %w", err)
	}

	// Warn if manifest is unversioned
	if meta.APIVersion == "" {
		log.Warn().
			Str(log.FieldComponent, log.ComponentManifest).
			Str("service", path).
			Msg("Service is missing apiVersion field (run 'bosun migrate' to update)")
	} else if meta.Kind != "" && meta.Kind != KindService {
		log.Warn().
			Str(log.FieldComponent, log.ComponentManifest).
			Str("service", path).
			Str("kind", meta.Kind).
			Str("expected", KindService).
			Msg("Service has unexpected kind")
	}

	var manifest ServiceManifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	return &manifest, nil
}

// LoadValuesOverlay loads a values overlay file.
func LoadValuesOverlay(path string) (map[string]any, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read values file: %w", err)
	}

	var values map[string]any
	if err := yaml.Unmarshal(content, &values); err != nil {
		return nil, fmt.Errorf("parse values file: %w", err)
	}

	return values, nil
}
