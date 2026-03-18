package manifest

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/cameronsjo/bosun/internal/log"
)

// LoadProvision loads a provision file, interpolates variables, and parses YAML.
// Supports inheritance via 'includes' key with circular include protection.
func LoadProvision(provisionName string, variables map[string]any, provisionsDir string) (*Provision, error) {
	loaded := make(map[string]bool)
	return loadProvisionInternal(provisionName, variables, provisionsDir, loaded)
}

func loadProvisionInternal(provisionName string, variables map[string]any, provisionsDir string, loaded map[string]bool) (*Provision, error) {
	// Prevent circular includes - return error instead of silently ignoring
	if loaded[provisionName] {
		return nil, fmt.Errorf("provision %s: %w", provisionName, ErrCircularInclude)
	}
	loaded[provisionName] = true

	provisionPath := filepath.Join(provisionsDir, provisionName+".yml")
	rawContent, err := os.ReadFile(provisionPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("provision not found: %s", provisionPath)
		}
		return nil, fmt.Errorf("read provision %s: %w", provisionPath, err)
	}

	// Validate apiVersion if present (soft validation for backwards compatibility)
	meta, err := ValidateManifest(rawContent)
	if err != nil {
		return nil, fmt.Errorf("validate provision %s: %w", provisionName, err)
	}

	// Warn if manifest is unversioned
	if meta.APIVersion == "" {
		log.Warn().
			Str(log.FieldComponent, log.ComponentManifest).
			Str("provision", provisionName).
			Msg("Provision is missing apiVersion field (run 'bosun migrate' to update)")
	} else if meta.Kind != "" && meta.Kind != KindProvision {
		log.Warn().
			Str(log.FieldComponent, log.ComponentManifest).
			Str("provision", provisionName).
			Str("kind", meta.Kind).
			Str("expected", KindProvision).
			Msg("Provision has unexpected kind")
	}

	// Interpolate BEFORE YAML parsing
	interpolated, err := Interpolate(string(rawContent), variables)
	if err != nil {
		return nil, fmt.Errorf("interpolate provision %s: %w", provisionName, err)
	}

	// Parse YAML into raw map to handle includes
	var rawProvision map[string]any
	if err := yaml.Unmarshal([]byte(interpolated), &rawProvision); err != nil {
		return nil, fmt.Errorf("parse provision %s: %w", provisionName, err)
	}

	if rawProvision == nil {
		rawProvision = make(map[string]any)
	}

	// Remove apiVersion and kind from raw provision (they're metadata, not output)
	delete(rawProvision, "apiVersion")
	delete(rawProvision, "kind")

	// Extract includes before processing
	var includes []string
	if includesRaw, ok := rawProvision["includes"]; ok {
		delete(rawProvision, "includes")
		switch v := includesRaw.(type) {
		case []any:
			for _, item := range v {
				includes = append(includes, fmt.Sprintf("%v", item))
			}
		case []string:
			includes = v
		}
	}

	// Handle inheritance - load included provisions first, then merge this on top
	if len(includes) > 0 {
		result := make(map[string]map[string]any)

		for _, included := range includes {
			includedProvision, err := loadProvisionInternal(included, variables, provisionsDir, loaded)
			if err != nil {
				return nil, fmt.Errorf("include %s in %s: %w", included, provisionName, err)
			}

			// Merge included provision's targets
			for name, content := range includedProvision.Targets {
				if content != nil {
					existing := result[name]
					if existing == nil {
						existing = make(map[string]any)
					}
					result[name] = DeepMerge(existing, content)
				}
			}
		}

		// Merge this provision's targets on top of included ones
		for key, val := range rawProvision {
			if targetData, ok := val.(map[string]any); ok {
				existing := result[key]
				if existing == nil {
					existing = make(map[string]any)
				}
				result[key] = DeepMerge(existing, targetData)
			}
		}

		return &Provision{Targets: result}, nil
	}

	// No includes - extract target maps from raw provision
	targets := make(map[string]map[string]any)
	for key, val := range rawProvision {
		if m, ok := val.(map[string]any); ok {
			targets[key] = m
		}
	}

	return &Provision{Targets: targets}, nil
}

// ListProvisions returns the names of all available provisions.
func ListProvisions(provisionsDir string) ([]string, error) {
	entries, err := os.ReadDir(provisionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("provisions directory not found: %s", provisionsDir)
		}
		return nil, fmt.Errorf("read provisions directory: %w", err)
	}

	var provisions []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) == ".yml" || filepath.Ext(name) == ".yaml" {
			provisions = append(provisions, name[:len(name)-len(filepath.Ext(name))])
		}
	}

	return provisions, nil
}

// ProvisionExists checks if a provision file exists.
func ProvisionExists(provisionName, provisionsDir string) bool {
	provisionPath := filepath.Join(provisionsDir, provisionName+".yml")
	_, err := os.Stat(provisionPath)
	return err == nil
}
