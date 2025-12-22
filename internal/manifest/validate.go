package manifest

import (
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Validation errors for manifest versioning.
var (
	// ErrUnsupportedAPIVersion indicates an unknown or unsupported API version.
	ErrUnsupportedAPIVersion = errors.New("unsupported API version")

	// ErrInvalidKind indicates an unknown manifest kind.
	ErrInvalidKind = errors.New("invalid manifest kind")

	// ErrKindMismatch indicates the kind doesn't match what was expected.
	ErrKindMismatch = errors.New("kind mismatch")

	// ErrMissingAPIVersion indicates a manifest is missing the apiVersion field.
	ErrMissingAPIVersion = errors.New("missing apiVersion field")

	// ErrMissingKind indicates a manifest is missing the kind field.
	ErrMissingKind = errors.New("missing kind field")
)

// ManifestMeta contains the common metadata fields from a manifest.
type ManifestMeta struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
}

// ValidateAPIVersion checks if the provided version is supported.
// Returns nil if the version is valid or empty (for backwards compatibility).
func ValidateAPIVersion(version string) error {
	if version == "" {
		return nil // Allow empty for backwards compatibility
	}

	for _, supported := range SupportedAPIVersions {
		if version == supported {
			return nil
		}
	}

	return fmt.Errorf("%w: %s (supported: %v)", ErrUnsupportedAPIVersion, version, SupportedAPIVersions)
}

// ValidateKind checks if the provided kind is valid and matches the expected kind.
// Returns nil if the kind is valid or empty (for backwards compatibility).
func ValidateKind(kind, expected string) error {
	if kind == "" {
		return nil // Allow empty for backwards compatibility
	}

	// Check if kind is in the supported list
	valid := false
	for _, supported := range SupportedKinds {
		if kind == supported {
			valid = true
			break
		}
	}

	if !valid {
		return fmt.Errorf("%w: %s (supported: %v)", ErrInvalidKind, kind, SupportedKinds)
	}

	// Check if kind matches expected
	if kind != expected {
		return fmt.Errorf("%w: got %s, expected %s", ErrKindMismatch, kind, expected)
	}

	return nil
}

// ValidateManifest extracts and validates the apiVersion and kind from raw YAML data.
// Returns the manifest metadata and any validation errors.
// For backwards compatibility, missing apiVersion/kind fields are allowed but logged as warnings.
func ValidateManifest(data []byte) (*ManifestMeta, error) {
	var meta ManifestMeta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse manifest metadata: %w", err)
	}

	if err := ValidateAPIVersion(meta.APIVersion); err != nil {
		return &meta, err
	}

	return &meta, nil
}

// ValidateManifestStrict validates a manifest and requires apiVersion and kind fields.
// Use this for strict validation where missing fields should be errors.
func ValidateManifestStrict(data []byte, expectedKind string) (*ManifestMeta, error) {
	var meta ManifestMeta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse manifest metadata: %w", err)
	}

	if meta.APIVersion == "" {
		return &meta, ErrMissingAPIVersion
	}

	if meta.Kind == "" {
		return &meta, ErrMissingKind
	}

	if err := ValidateAPIVersion(meta.APIVersion); err != nil {
		return &meta, err
	}

	if err := ValidateKind(meta.Kind, expectedKind); err != nil {
		return &meta, err
	}

	return &meta, nil
}

// IsVersioned checks if a manifest has apiVersion and kind fields set.
func IsVersioned(data []byte) (bool, error) {
	var meta ManifestMeta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return false, fmt.Errorf("parse manifest metadata: %w", err)
	}

	return meta.APIVersion != "" && meta.Kind != "", nil
}

// GetManifestKind extracts the kind field from raw YAML data.
// Returns empty string if kind is not present.
func GetManifestKind(data []byte) (string, error) {
	var meta ManifestMeta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return "", fmt.Errorf("parse manifest metadata: %w", err)
	}

	return meta.Kind, nil
}
