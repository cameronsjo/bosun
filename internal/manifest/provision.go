package manifest

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadProvision loads a provision file, interpolates variables, and parses YAML.
// Supports inheritance via 'includes' key with circular include protection.
func LoadProvision(provisionName string, variables map[string]any, provisionsDir string) (*Provision, error) {
	loaded := make(map[string]bool)
	return loadProvisionInternal(provisionName, variables, provisionsDir, loaded)
}

func loadProvisionInternal(provisionName string, variables map[string]any, provisionsDir string, loaded map[string]bool) (*Provision, error) {
	// Prevent circular includes
	if loaded[provisionName] {
		return &Provision{}, nil
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
		for _, target := range TargetNames {
			result[target] = make(map[string]any)
		}

		for _, included := range includes {
			includedProvision, err := loadProvisionInternal(included, variables, provisionsDir, loaded)
			if err != nil {
				return nil, fmt.Errorf("include %s in %s: %w", included, provisionName, err)
			}

			// Merge included provision's targets
			if includedProvision.Compose != nil {
				result["compose"] = DeepMerge(result["compose"], includedProvision.Compose)
			}
			if includedProvision.Traefik != nil {
				result["traefik"] = DeepMerge(result["traefik"], includedProvision.Traefik)
			}
			if includedProvision.Gatus != nil {
				result["gatus"] = DeepMerge(result["gatus"], includedProvision.Gatus)
			}
		}

		// Merge this provision on top of included ones
		for _, target := range TargetNames {
			if targetData, ok := rawProvision[target].(map[string]any); ok {
				result[target] = DeepMerge(result[target], targetData)
			}
		}

		return &Provision{
			Compose: result["compose"],
			Traefik: result["traefik"],
			Gatus:   result["gatus"],
		}, nil
	}

	// No includes - return provision directly
	provision := &Provision{}
	if compose, ok := rawProvision["compose"].(map[string]any); ok {
		provision.Compose = compose
	}
	if traefik, ok := rawProvision["traefik"].(map[string]any); ok {
		provision.Traefik = traefik
	}
	if gatus, ok := rawProvision["gatus"].(map[string]any); ok {
		provision.Gatus = gatus
	}

	return provision, nil
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
