package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/cameronsjo/bosun/internal/log"
)

// provisionExtensions are the file extensions a provision may use, in lookup
// order: a provisions directory holding both foo.yml and foo.yaml loads foo.yml.
var provisionExtensions = []string{".yml", ".yaml"}

// loadState tracks one LoadProvision call across its include graph.
type loadState struct {
	// stack holds the provisions on the current include path. A name found
	// here is a real cycle.
	stack map[string]bool
	// done holds the provisions already loaded and merged. A name found here
	// is a diamond (the same base reached by two paths): not an error, and it
	// is not merged a second time.
	done map[string]bool
}

// provisionPath resolves a provision name to a file inside provisionsDir.
// It tries each of provisionExtensions in order and returns the first regular
// file that exists (a directory named foo.yml is skipped); when none exists it
// returns the first candidate so the caller's read reports "not found" for it.
// A name that resolves outside provisionsDir returns ErrPathTraversal and
// nothing is read. The check is lexical: it rejects ".." that climbs out of the
// directory, and does not resolve symlinks.
func provisionPath(provisionsDir, provisionName string) (string, error) {
	var first string
	for _, ext := range provisionExtensions {
		candidate, err := validatePathWithinDir(provisionsDir, provisionName+ext)
		if err != nil {
			return "", err
		}
		if first == "" {
			first = candidate
		}
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return first, nil
}

// LoadProvision loads a provision file, interpolates variables, and parses YAML.
// Supports inheritance via 'includes' key with circular include protection.
// A provision reachable by more than one include path (a diamond) is merged
// once, at its first occurrence. A name that resolves outside provisionsDir is
// rejected with ErrPathTraversal.
func LoadProvision(provisionName string, variables map[string]any, provisionsDir string) (*Provision, error) {
	state := &loadState{stack: make(map[string]bool), done: make(map[string]bool)}
	return loadProvisionInternal(provisionName, variables, provisionsDir, state)
}

func loadProvisionInternal(provisionName string, variables map[string]any, provisionsDir string, state *loadState) (*Provision, error) {
	logger := log.Component(log.ComponentManifest)

	// Prevent circular includes - return error instead of silently ignoring
	if state.stack[provisionName] {
		logger.Error().Str("provision", provisionName).Msg("Circular include detected")
		return nil, fmt.Errorf("provision %s: %w", provisionName, ErrCircularInclude)
	}
	// A diamond include was already merged at its first occurrence
	if state.done[provisionName] {
		logger.Debug().Str("provision", provisionName).Msg("Provision already loaded through another include, skipping")
		return &Provision{Targets: make(map[string]map[string]any)}, nil
	}
	state.stack[provisionName] = true
	defer delete(state.stack, provisionName)
	logger.Debug().Str("provision", provisionName).Msg("Loading provision")

	filePath, err := provisionPath(provisionsDir, provisionName)
	if err != nil {
		logger.Error().Str("provision", provisionName).Err(err).Msg("Provision name resolves outside the provisions directory")
		return nil, fmt.Errorf("provision %s: %w", provisionName, err)
	}
	rawContent, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Error().Str("provision", provisionName).Str(log.FieldPath, filePath).Msg("Provision file not found")
			return nil, fmt.Errorf("provision not found: %s", filePath)
		}
		logger.Error().Str("provision", provisionName).Str(log.FieldPath, filePath).Err(err).Msg("Failed to read provision file")
		return nil, fmt.Errorf("read provision %s: %w", filePath, err)
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
		logger.Error().Str("provision", provisionName).Err(err).Msg("Failed to interpolate provision variables")
		return nil, fmt.Errorf("interpolate provision %s: %w", provisionName, err)
	}

	// Parse YAML into raw map to handle includes
	var rawProvision map[string]any
	if err := yaml.Unmarshal([]byte(interpolated), &rawProvision); err != nil {
		logger.Error().Str("provision", provisionName).Err(err).Msg("Failed to parse provision YAML")
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
		logger.Debug().
			Str("provision", provisionName).
			Int("include_count", len(includes)).
			Msg("Processing provision includes")
		result := make(map[string]map[string]any)

		for _, included := range includes {
			logger.Debug().
				Str("provision", provisionName).
				Str("included_provision", included).
				Msg("Loading included provision")
			includedProvision, err := loadProvisionInternal(included, variables, provisionsDir, state)
			if err != nil {
				logger.Error().
					Str("provision", provisionName).
					Str("included_provision", included).
					Err(err).
					Msg("Failed to load included provision")
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

		state.done[provisionName] = true
		return &Provision{Targets: result}, nil
	}

	// No includes - extract target maps from raw provision
	targets := make(map[string]map[string]any)
	for key, val := range rawProvision {
		if m, ok := val.(map[string]any); ok {
			targets[key] = m
		}
	}

	state.done[provisionName] = true
	return &Provision{Targets: targets}, nil
}

// ListProvisions returns the names of all available provisions.
func ListProvisions(provisionsDir string) ([]string, error) {
	logger := log.Component(log.ComponentManifest)
	logger.Debug().Str(log.FieldPath, provisionsDir).Msg("Listing available provisions")

	entries, err := os.ReadDir(provisionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Warn().Str(log.FieldPath, provisionsDir).Msg("Provisions directory not found")
			return nil, fmt.Errorf("provisions directory not found: %s", provisionsDir)
		}
		logger.Error().Str(log.FieldPath, provisionsDir).Err(err).Msg("Failed to read provisions directory")
		return nil, fmt.Errorf("read provisions directory: %w", err)
	}

	var provisions []string
	seen := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := filepath.Ext(name)
		if !slices.Contains(provisionExtensions, ext) {
			continue
		}
		// foo.yml and foo.yaml name the same provision; list it once
		provision := strings.TrimSuffix(name, ext)
		if seen[provision] {
			continue
		}
		seen[provision] = true
		provisions = append(provisions, provision)
	}

	logger.Debug().
		Str(log.FieldPath, provisionsDir).
		Int("provision_count", len(provisions)).
		Msg("Listed available provisions")
	return provisions, nil
}

// ProvisionExists checks if a provision file (.yml or .yaml) exists. A name
// that resolves outside provisionsDir does not exist.
func ProvisionExists(provisionName, provisionsDir string) bool {
	filePath, err := provisionPath(provisionsDir, provisionName)
	if err != nil {
		return false
	}
	info, err := os.Stat(filePath)
	return err == nil && !info.IsDir()
}
