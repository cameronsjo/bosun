package manifest

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// RenderService renders a service manifest into compose/traefik/gatus outputs.
func RenderService(manifest *ServiceManifest, provisionsDir string) (*RenderOutput, error) {
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
			output.Compose["services"] = manifest.Compose
		}
		return output, nil
	}

	// Load and merge provisions
	for _, provisionName := range manifest.Provisions {
		provision, err := LoadProvision(provisionName, variables, provisionsDir)
		if err != nil {
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

	return output, nil
}

// mergeProvision merges a provision's outputs into the render output.
func mergeProvision(output *RenderOutput, provision *Provision) {
	if provision.Compose != nil {
		output.Compose = DeepMerge(output.Compose, provision.Compose)
	}
	if provision.Traefik != nil {
		output.Traefik = DeepMerge(output.Traefik, provision.Traefik)
	}
	if provision.Gatus != nil {
		output.Gatus = DeepMerge(output.Gatus, provision.Gatus)
	}
}

// RenderStack renders a stack file into compose/traefik/gatus outputs.
func RenderStack(stackPath, provisionsDir, servicesDir string, valuesOverlay map[string]any) (*RenderOutput, error) {
	stackContent, err := os.ReadFile(stackPath)
	if err != nil {
		return nil, fmt.Errorf("read stack file: %w", err)
	}

	var stack Stack
	if err := yaml.Unmarshal(stackContent, &stack); err != nil {
		return nil, fmt.Errorf("parse stack file: %w", err)
	}

	output := NewRenderOutput()

	for _, serviceFile := range stack.Include {
		servicePath := filepath.Join(servicesDir, serviceFile)
		serviceContent, err := os.ReadFile(servicePath)
		if err != nil {
			return nil, fmt.Errorf("read service %s: %w", serviceFile, err)
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
			return nil, fmt.Errorf("render service %s: %w", manifest.Name, err)
		}

		output.Compose = DeepMerge(output.Compose, serviceOutput.Compose)
		output.Traefik = DeepMerge(output.Traefik, serviceOutput.Traefik)
		output.Gatus = DeepMerge(output.Gatus, serviceOutput.Gatus)
	}

	// Add network definitions from stack
	if stack.Networks != nil {
		output.Compose["networks"] = stack.Networks
	}

	return output, nil
}

// WriteOutputs writes rendered outputs to files in the output directory.
func WriteOutputs(output *RenderOutput, outputDir, stackName string) error {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	targets := map[string]struct {
		content  map[string]any
		filename string
	}{
		"compose": {output.Compose, stackName + ".yml"},
		"traefik": {output.Traefik, "dynamic.yml"},
		"gatus":   {output.Gatus, "endpoints.yml"},
	}

	for target, cfg := range targets {
		if len(cfg.content) == 0 {
			continue
		}

		targetDir := filepath.Join(outputDir, target)
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return fmt.Errorf("create %s directory: %w", target, err)
		}

		outputPath := filepath.Join(targetDir, cfg.filename)
		data, err := yaml.Marshal(cfg.content)
		if err != nil {
			return fmt.Errorf("marshal %s output: %w", target, err)
		}

		if err := os.WriteFile(outputPath, data, 0644); err != nil {
			return fmt.Errorf("write %s output: %w", target, err)
		}

		fmt.Printf("Wrote: %s\n", outputPath)
	}

	return nil
}

// RenderToYAML renders an output to YAML string for dry-run display.
func RenderToYAML(output *RenderOutput) (string, error) {
	combined := map[string]any{
		"compose": output.Compose,
		"traefik": output.Traefik,
		"gatus":   output.Gatus,
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
