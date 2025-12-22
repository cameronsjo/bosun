package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getsops/sops/v3/decrypt"
	"gopkg.in/yaml.v3"
)

// ErrAgeKeyNotFound is returned when no age key is found for SOPS decryption.
var ErrAgeKeyNotFound = errors.New("age key not found")

// ErrNotSOPSFile is returned when a file is not a valid SOPS-encrypted file.
var ErrNotSOPSFile = errors.New("file is not SOPS-encrypted")

// SOPSOps provides SOPS decryption operations.
type SOPSOps struct{}

// NewSOPSOps creates a new SOPSOps instance.
func NewSOPSOps() *SOPSOps {
	return &SOPSOps{}
}

// CheckAgeKey verifies that an age key is available for SOPS decryption.
// It checks in order:
//  1. SOPS_AGE_KEY environment variable
//  2. SOPS_AGE_KEY_FILE environment variable
//  3. Default location: ~/.config/sops/age/keys.txt
//
// Returns nil if a key is found, or an error with setup instructions if not.
func (s *SOPSOps) CheckAgeKey() error {
	// Check SOPS_AGE_KEY environment variable
	if key := os.Getenv("SOPS_AGE_KEY"); key != "" {
		return nil
	}

	// Check SOPS_AGE_KEY_FILE environment variable
	if keyFile := os.Getenv("SOPS_AGE_KEY_FILE"); keyFile != "" {
		if _, err := os.Stat(keyFile); err == nil {
			return nil
		}
		return fmt.Errorf("%w: SOPS_AGE_KEY_FILE is set to %q but file does not exist.\n\nTo fix:\n  1. Create the key file at the specified path\n  2. Or set SOPS_AGE_KEY_FILE to an existing key file\n  3. Or run: age-keygen -o ~/.config/sops/age/keys.txt", ErrAgeKeyNotFound, keyFile)
	}

	// Check default location
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("%w: unable to determine home directory: %v\n\nTo fix:\n  1. Set SOPS_AGE_KEY environment variable with the key content\n  2. Or set SOPS_AGE_KEY_FILE=/path/to/key", ErrAgeKeyNotFound, err)
	}

	defaultKeyPath := filepath.Join(homeDir, ".config", "sops", "age", "keys.txt")
	if _, err := os.Stat(defaultKeyPath); err == nil {
		return nil
	}

	return fmt.Errorf(`%w

To fix:
  1. Generate key: age-keygen -o ~/.config/sops/age/keys.txt
  2. Or set SOPS_AGE_KEY_FILE=/path/to/key
  3. Or set SOPS_AGE_KEY environment variable with the key content`, ErrAgeKeyNotFound)
}

// ValidateSOPSFile checks if a file is a valid SOPS-encrypted file.
// Returns nil if valid, or an actionable error describing the problem.
func ValidateSOPSFile(path string) error {
	// Check file exists
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("SOPS file not found: %s", path)
		}
		return fmt.Errorf("failed to read SOPS file: %w", err)
	}

	// Parse as YAML
	var content map[string]any
	if err := yaml.Unmarshal(data, &content); err != nil {
		return fmt.Errorf("invalid YAML syntax in %s: %w", path, err)
	}

	// Check for SOPS metadata marker
	if _, hasSOPS := content["sops"]; !hasSOPS {
		return fmt.Errorf("%w: %s does not contain 'sops' metadata key. Encrypt it with: sops --encrypt --in-place %s", ErrNotSOPSFile, path, path)
	}

	return nil
}

// Decrypt decrypts a SOPS-encrypted file and returns the plaintext bytes as JSON.
// It first validates the file is SOPS-encrypted and checks that an age key is available.
// Uses go-sops library for in-process decryption - no external binary required.
func (s *SOPSOps) Decrypt(ctx context.Context, file string) ([]byte, error) {
	// Validate SOPS file before attempting decryption
	if err := ValidateSOPSFile(file); err != nil {
		return nil, err
	}

	if err := s.CheckAgeKey(); err != nil {
		return nil, err
	}

	// Use go-sops library for in-process decryption
	// The decrypt.File function reads the age key from SOPS_AGE_KEY or SOPS_AGE_KEY_FILE
	// or the default location ~/.config/sops/age/keys.txt
	plaintext, err := decrypt.File(file, "yaml")
	if err != nil {
		return nil, fmt.Errorf("sops decrypt failed for %s: %w", file, sanitizeDecryptError(err))
	}

	// Parse the YAML and convert to JSON for consistent output
	var data map[string]any
	if err := yaml.Unmarshal(plaintext, &data); err != nil {
		return nil, fmt.Errorf("failed to parse decrypted YAML from %s: %w", file, err)
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to convert decrypted data to JSON from %s: %w", file, err)
	}

	return jsonBytes, nil
}

// DecryptToMap decrypts a SOPS-encrypted file and returns the data as a map.
// It first checks that an age key is available.
func (s *SOPSOps) DecryptToMap(ctx context.Context, file string) (map[string]any, error) {
	if err := s.CheckAgeKey(); err != nil {
		return nil, err
	}

	data, err := s.Decrypt(ctx, file)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse decrypted JSON from %s: %w", file, err)
	}
	return result, nil
}

// DecryptFiles decrypts multiple SOPS files and merges them into a single map.
// Later files override earlier ones for duplicate keys.
// This method implements the SecretsDecryptor interface.
func (s *SOPSOps) DecryptFiles(ctx context.Context, files []string) (map[string]any, error) {
	merged := make(map[string]any)

	for _, file := range files {
		data, err := s.DecryptToMap(ctx, file)
		if err != nil {
			return nil, err
		}
		mergeMap(merged, data)
	}

	return merged, nil
}

// DecryptToJSON decrypts files and returns merged JSON bytes.
func (s *SOPSOps) DecryptToJSON(ctx context.Context, files []string) ([]byte, error) {
	merged, err := s.DecryptFiles(ctx, files)
	if err != nil {
		return nil, err
	}
	return json.Marshal(merged)
}

// sanitizeDecryptError wraps and sanitizes errors from the decrypt library.
// This ensures no sensitive information (like partial keys or decrypted content)
// is exposed in error messages.
func sanitizeDecryptError(err error) error {
	if err == nil {
		return nil
	}

	errStr := err.Error()
	errLower := strings.ToLower(errStr)

	// Check for common, safe error patterns and return them directly
	safePatterns := []string{
		"could not find",
		"no key found",
		"failed to get",
		"cannot find",
		"key not found",
		"permission denied",
		"no such file",
	}

	for _, pattern := range safePatterns {
		if strings.Contains(errLower, pattern) {
			// Limit length for safety
			if len(errStr) > 200 {
				return errors.New(errStr[:200] + "... (truncated)")
			}
			return err
		}
	}

	// For unknown errors, provide a generic message
	return errors.New("decryption failed - check age key configuration")
}

// mergeMap recursively merges src into dst.
func mergeMap(dst, src map[string]any) {
	for key, srcVal := range src {
		if dstVal, exists := dst[key]; exists {
			// If both are maps, merge recursively.
			if srcMap, srcOk := srcVal.(map[string]any); srcOk {
				if dstMap, dstOk := dstVal.(map[string]any); dstOk {
					mergeMap(dstMap, srcMap)
					continue
				}
			}
		}
		dst[key] = srcVal
	}
}
