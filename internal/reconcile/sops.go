package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
	sopslib "github.com/getsops/sops/v3"
	sopsage "github.com/getsops/sops/v3/age"
	"github.com/getsops/sops/v3/config"
	"github.com/getsops/sops/v3/decrypt"
	sopsdotenv "github.com/getsops/sops/v3/stores/dotenv"
	sopsini "github.com/getsops/sops/v3/stores/ini"
	"gopkg.in/yaml.v3"
)

// ErrAgeKeyNotFound is returned when no age key is found for SOPS decryption.
var ErrAgeKeyNotFound = errors.New("age key not found")

// ErrNotSOPSFile is returned when a file is not a valid SOPS-encrypted file.
var ErrNotSOPSFile = errors.New("file is not SOPS-encrypted")

// ErrUnsupportedSOPSFormat is returned when a secrets file cannot be decoded
// into the key/value map Bosun uses for template data.
var ErrUnsupportedSOPSFormat = errors.New("unsupported SOPS secrets format")

// ErrSOPSIntegrity is returned when SOPS cannot authenticate the encrypted
// document or its MAC. The underlying library error is deliberately not
// wrapped because it can contain decrypted MACs or encrypted values.
var ErrSOPSIntegrity = errors.New("sops integrity verification failed")

// ErrSOPSKeyUnavailable is returned when no configured key can recover the
// SOPS data key. The underlying library error is deliberately not wrapped
// because it can contain key identifiers, key material, or local paths.
var ErrSOPSKeyUnavailable = errors.New("sops decryption key unavailable")

// ErrMalformedSOPSData is returned when encrypted values or SOPS metadata
// cannot be decoded after the file passes Bosun's structural validation.
var ErrMalformedSOPSData = errors.New("malformed SOPS encrypted data")

// ErrSOPSDecryption is returned for failures that cannot be safely classified.
var ErrSOPSDecryption = errors.New("sops decryption failed")

type sopsFileFormat string

const (
	sopsFormatYAML   sopsFileFormat = "yaml"
	sopsFormatJSON   sopsFileFormat = "json"
	sopsFormatDotenv sopsFileFormat = "dotenv"
	sopsFormatINI    sopsFileFormat = "ini"
)

// SOPSOps provides SOPS decryption operations.
type SOPSOps struct {
	decryptFile        func(path, format string) ([]byte, error)
	identityFileReader regularNonEmptyFileReader
}

// NewSOPSOps creates a new SOPSOps instance.
func NewSOPSOps() *SOPSOps {
	return &SOPSOps{
		decryptFile:        decrypt.File,
		identityFileReader: readRegularNonEmptyFile,
	}
}

// ValidateAgeIdentityForSecrets performs the startup-only Age preflight when
// this reconciliation is configured to decrypt at least one secrets file.
func ValidateAgeIdentityForSecrets(secretFiles []string) error {
	for _, secretFile := range secretFiles {
		if strings.TrimSpace(secretFile) != "" {
			return NewSOPSOps().CheckAgeKey()
		}
	}
	return nil
}

func inferSOPSFormat(path string) (sopsFileFormat, error) {
	originalPath := path
	extension := filepath.Ext(path)
	if strings.EqualFold(extension, ".sops") {
		path = strings.TrimSuffix(path, extension)
		extension = filepath.Ext(path)
	}

	switch strings.ToLower(extension) {
	case ".yaml", ".yml":
		return sopsFormatYAML, nil
	case ".json":
		return sopsFormatJSON, nil
	case ".env":
		return sopsFormatDotenv, nil
	case ".ini":
		return sopsFormatINI, nil
	default:
		if extension == "" {
			extension = "<none>"
		}
		return "", fmt.Errorf("%w: %q has extension %q; supported extensions are .yaml, .yml, .json, .env, and .ini (optionally followed by .sops); binary SOPS files cannot be merged into Bosun template secrets", ErrUnsupportedSOPSFormat, originalPath, extension)
	}
}

// CheckAgeKey verifies that an age key is available for SOPS decryption.
// It checks in order:
//  1. SOPS_AGE_KEY environment variable
//  2. SOPS_AGE_KEY_FILE environment variable
//  3. Default location: ~/.config/sops/age/keys.txt
//
// Returns nil if a key is found, or an error with setup instructions if not.
func (s *SOPSOps) CheckAgeKey() error {
	logger := log.Component(log.ComponentSOPS)
	readIdentityFile := s.identityFileReader
	if readIdentityFile == nil {
		readIdentityFile = readRegularNonEmptyFile
	}

	// Check SOPS_AGE_KEY environment variable
	if key := os.Getenv("SOPS_AGE_KEY"); key != "" {
		logger.Debug().Str("source", "SOPS_AGE_KEY").Msg("Age key found via environment variable")
		return nil
	}

	// Check SOPS_AGE_KEY_FILE environment variable
	if keyFile := os.Getenv("SOPS_AGE_KEY_FILE"); keyFile != "" {
		if err := validateAgeIdentityFile(keyFile, readIdentityFile); err != nil {
			return ageIdentityFileError("SOPS_AGE_KEY_FILE", keyFile, err)
		}
		logger.Debug().Str("source", "SOPS_AGE_KEY_FILE").Str(log.FieldPath, keyFile).Msg("Age key found via key file")
		return nil
	}

	// Check default location
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("%w: unable to determine home directory: %v\n\nTo fix:\n  1. Set SOPS_AGE_KEY environment variable with the key content\n  2. Or set SOPS_AGE_KEY_FILE=/path/to/key", ErrAgeKeyNotFound, err)
	}

	defaultKeyPath := filepath.Join(homeDir, ".config", "sops", "age", "keys.txt")
	if err := validateAgeIdentityFile(defaultKeyPath, readIdentityFile); err == nil {
		logger.Debug().Str("source", "default").Str(log.FieldPath, defaultKeyPath).Msg("Age key found at default location")
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return ageIdentityFileError("default Age identity file", defaultKeyPath, err)
	}

	return fmt.Errorf(`%w

To fix:
  1. Generate key: age-keygen -o ~/.config/sops/age/keys.txt
  2. Or set SOPS_AGE_KEY_FILE=/path/to/key
  3. Or set SOPS_AGE_KEY environment variable with the key content`, ErrAgeKeyNotFound)
}

func validateAgeIdentityFile(path string, readFile regularNonEmptyFileReader) error {
	contents, err := readFile(path)
	if err != nil {
		return err
	}
	var identities sopsage.ParsedIdentities
	if err := identities.Import(string(contents)); err != nil || len(identities) == 0 {
		return errors.New("does not contain a parseable Age identity")
	}
	return nil
}

func ageIdentityFileError(source, path string, cause error) error {
	return fmt.Errorf(`%w: %s %q %v.

To fix:
  1. Pre-create the host path as a regular, non-empty Age identity file before starting Bosun
  2. Generate a key with: age-keygen -o %q
  3. If using Docker, verify the bind-mount source is a file; Docker may create a directory when a missing host source is mounted`, ErrAgeKeyNotFound, source, path, cause, path)
}

// ValidateSOPSFile checks if a file is a valid SOPS-encrypted file.
// Returns nil if valid, or an actionable error describing the problem.
func ValidateSOPSFile(path string) error {
	format, err := inferSOPSFormat(path)
	if err != nil {
		return err
	}
	return validateSOPSFile(path, format)
}

func validateSOPSFile(path string, format sopsFileFormat) error {
	// Check file exists
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("SOPS file not found: %s", path)
		}
		return fmt.Errorf("failed to read SOPS file: %w", err)
	}

	if format == sopsFormatDotenv || format == sopsFormatINI {
		return validateFlatSOPSFile(path, format, data)
	}

	var content map[string]any
	if format == sopsFormatJSON {
		if err := json.Unmarshal(data, &content); err != nil {
			return fmt.Errorf("invalid JSON syntax in %s: %w", path, err)
		}
	} else if err := yaml.Unmarshal(data, &content); err != nil {
		return fmt.Errorf("invalid YAML syntax in %s: %w", path, err)
	}
	return validateSOPSMetadataMap(path, content)
}

func validateSOPSMetadataMap(path string, content map[string]any) error {
	// Check for SOPS metadata marker and the fields SOPS needs before attempting
	// key discovery. This keeps malformed files from being reported as key errors.
	metadataValue, hasSOPS := content["sops"]
	if !hasSOPS {
		return fmt.Errorf("%w: %s does not contain 'sops' metadata key. Encrypt it with: sops --encrypt --in-place %s", ErrNotSOPSFile, path, path)
	}
	metadata, ok := metadataValue.(map[string]any)
	if !ok {
		return fmt.Errorf("%w: %s has invalid 'sops' metadata: expected a mapping", ErrNotSOPSFile, path)
	}

	mac, ok := metadata["mac"].(string)
	if !ok || strings.TrimSpace(mac) == "" {
		return fmt.Errorf("%w: %s has incomplete 'sops' metadata: missing non-empty 'mac'", ErrNotSOPSFile, path)
	}

	lastModifiedValue, hasLastModified := metadata["lastmodified"]
	if !hasLastModified {
		return fmt.Errorf("%w: %s has incomplete 'sops' metadata: missing non-empty 'lastmodified'", ErrNotSOPSFile, path)
	}
	validLastModified := false
	switch lastModified := lastModifiedValue.(type) {
	case string:
		if strings.TrimSpace(lastModified) == "" {
			return fmt.Errorf("%w: %s has incomplete 'sops' metadata: missing non-empty 'lastmodified'", ErrNotSOPSFile, path)
		}
		_, err := time.Parse(time.RFC3339, lastModified)
		validLastModified = err == nil
	case time.Time:
		validLastModified = !lastModified.IsZero()
	}
	if !validLastModified {
		return fmt.Errorf("%w: %s has invalid 'sops.lastmodified': expected an RFC3339 timestamp", ErrNotSOPSFile, path)
	}

	if !hasSOPSRecipients(metadata) {
		return fmt.Errorf("%w: %s has incomplete 'sops' metadata: no key recipient with a non-empty encrypted data key in age, pgp, kms, gcp_kms, azure_kv, hc_vault, or key_groups", ErrNotSOPSFile, path)
	}

	return nil
}

func flatSOPSStore(format sopsFileFormat) sopslib.Store {
	storesConfig := config.NewStoresConfig()
	if format == sopsFormatDotenv {
		return sopsdotenv.NewStore(&storesConfig.Dotenv)
	}
	return sopsini.NewStore(&storesConfig.INI)
}

func validateFlatSOPSFile(path string, format sopsFileFormat, data []byte) error {
	store := flatSOPSStore(format)
	branches, err := store.LoadPlainFile(data)
	if err != nil {
		return fmt.Errorf("invalid %s syntax in %s", format, path)
	}
	if len(branches) == 0 || !store.HasSopsTopLevelKey(branches[0]) {
		return fmt.Errorf("%w: %s does not contain 'sops' metadata. Encrypt it with: sops --encrypt --in-place %s", ErrNotSOPSFile, path, path)
	}

	tree, err := store.LoadEncryptedFile(data)
	if err != nil {
		return fmt.Errorf("%w: %s has invalid %s SOPS metadata: %v", ErrNotSOPSFile, path, format, err)
	}
	if strings.TrimSpace(tree.Metadata.MessageAuthenticationCode) == "" {
		return fmt.Errorf("%w: %s has incomplete 'sops' metadata: missing non-empty 'mac'", ErrNotSOPSFile, path)
	}
	if tree.Metadata.LastModified.IsZero() {
		return fmt.Errorf("%w: %s has incomplete 'sops' metadata: missing valid 'lastmodified'", ErrNotSOPSFile, path)
	}
	if !hasEncryptedMasterKey(tree.Metadata) {
		return fmt.Errorf("%w: %s has incomplete 'sops' metadata: no key recipient with a non-empty encrypted data key", ErrNotSOPSFile, path)
	}
	return nil
}

func hasEncryptedMasterKey(metadata sopslib.Metadata) bool {
	for _, group := range metadata.KeyGroups {
		for _, key := range group {
			if len(key.EncryptedDataKey()) > 0 {
				return true
			}
		}
	}
	return false
}

func hasSOPSRecipients(metadata map[string]any) bool {
	if hasDirectSOPSRecipients(metadata) {
		return true
	}

	groups, ok := metadata["key_groups"].([]any)
	if !ok {
		return false
	}
	for _, groupValue := range groups {
		group, ok := groupValue.(map[string]any)
		if ok && hasDirectSOPSRecipients(group) {
			return true
		}
	}
	return false
}

func hasDirectSOPSRecipients(metadata map[string]any) bool {
	for _, key := range []string{"age", "pgp", "kms", "gcp_kms", "azure_kv", "hc_vault"} {
		recipients, ok := metadata[key].([]any)
		if !ok {
			continue
		}
		for _, recipientValue := range recipients {
			recipient, ok := recipientValue.(map[string]any)
			if !ok {
				continue
			}
			encryptedKey, hasEncryptedKey := recipient["enc"].(string)
			if hasEncryptedKey && strings.TrimSpace(encryptedKey) != "" {
				return true
			}
		}
	}
	return false
}

// Decrypt decrypts a SOPS-encrypted file and returns the plaintext bytes as JSON.
// It first validates the file is SOPS-encrypted and checks that an age key is available.
// Uses go-sops library for in-process decryption - no external binary required.
func (s *SOPSOps) Decrypt(ctx context.Context, file string) ([]byte, error) {
	logger := log.Component(log.ComponentSOPS)
	format, err := inferSOPSFormat(file)
	if err != nil {
		return nil, err
	}

	// Validate SOPS file before attempting decryption
	if err := validateSOPSFile(file, format); err != nil {
		return nil, err
	}

	if err := s.CheckAgeKey(); err != nil {
		return nil, err
	}

	logger.Debug().
		Str(log.FieldOperation, "decrypt").
		Str(log.FieldPath, file).
		Msg("Decrypting SOPS file")

	// Use go-sops library for in-process decryption
	// The decrypt.File function reads the age key from SOPS_AGE_KEY or SOPS_AGE_KEY_FILE
	// or the default location ~/.config/sops/age/keys.txt
	decryptFile := s.decryptFile
	if decryptFile == nil {
		decryptFile = decrypt.File
	}
	plaintext, err := decryptFile(file, string(format))
	if err != nil {
		safeErr := sanitizeDecryptError(err)
		logger.Debug().
			Err(safeErr).
			Str(log.FieldPath, file).
			Msg("SOPS decryption failed")
		return nil, fmt.Errorf("sops decrypt failed for %s: %w", file, safeErr)
	}

	logger.Debug().
		Str(log.FieldOperation, "decrypt").
		Str(log.FieldPath, file).
		Msg("Successfully decrypted SOPS file")

	data, err := decodeSOPSPlaintext(plaintext, format)
	if err != nil {
		// Parser errors for flat formats can include the complete plaintext line.
		// Keep decrypted values out of errors returned to CLI and daemon callers.
		return nil, fmt.Errorf("failed to parse decrypted %s from %s", format, file)
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to convert decrypted data to JSON from %s: %w", file, err)
	}

	return jsonBytes, nil
}

func decodeSOPSPlaintext(plaintext []byte, format sopsFileFormat) (map[string]any, error) {
	var data map[string]any
	switch format {
	case sopsFormatYAML:
		if err := yaml.Unmarshal(plaintext, &data); err != nil {
			return nil, err
		}
	case sopsFormatJSON:
		if err := json.Unmarshal(plaintext, &data); err != nil {
			return nil, err
		}
	case sopsFormatDotenv, sopsFormatINI:
		branches, err := flatSOPSStore(format).LoadPlainFile(plaintext)
		if err != nil {
			return nil, err
		}
		if len(branches) != 1 {
			return nil, fmt.Errorf("expected one document, got %d", len(branches))
		}
		return sopsBranchToMap(branches[0]), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedSOPSFormat, format)
	}
	return data, nil
}

func sopsBranchToMap(branch sopslib.TreeBranch) map[string]any {
	result := make(map[string]any)
	for _, item := range branch {
		key, ok := item.Key.(string)
		if !ok {
			continue
		}
		if nested, ok := item.Value.(sopslib.TreeBranch); ok {
			value := sopsBranchToMap(nested)
			if key == "DEFAULT" && len(value) == 0 {
				continue
			}
			result[key] = value
			continue
		}
		result[key] = item.Value
	}
	return result
}

// DecryptToMap decrypts a SOPS-encrypted file and returns the data as a map.
func (s *SOPSOps) DecryptToMap(ctx context.Context, file string) (map[string]any, error) {
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

// sanitizeDecryptError classifies errors from the decrypt library without
// returning or wrapping the upstream error. SOPS errors can contain decrypted
// MACs, encrypted values, key identifiers, key material, and local paths, so
// even strings that appear actionable must be replaced with static guidance.
func sanitizeDecryptError(err error) error {
	if err == nil {
		return nil
	}

	errLower := strings.ToLower(err.Error())

	if errors.Is(err, sopslib.MacMismatch) || containsAny(errLower,
		"failed to verify data integrity",
		"mac mismatch",
		"failed to decrypt original mac",
		"cannot decrypt mac",
		"message authentication failed",
		"could not decrypt with aes_gcm",
	) {
		return fmt.Errorf("%w: the encrypted file may be corrupted or modified; restore it from a trusted source or re-encrypt it", ErrSOPSIntegrity)
	}

	if containsAny(errLower,
		"error getting data key",
		"failed to get the data key",
		"no identity matched",
		"no master key",
		"could not decrypt group",
		"error decrypting key",
		"failed to decrypt data key",
		"failed to create reader for decrypting sops data key",
		"failed to load age identities",
		"incorrect passphrase",
		"could not find",
		"no key found",
		"cannot find",
		"key not found",
		"permission denied",
		"no such file",
	) {
		return fmt.Errorf("%w: verify that the configured Age identity matches the file recipients and that its key file is readable", ErrSOPSKeyUnavailable)
	}

	if errors.Is(err, sopslib.MetadataNotFound) || containsAny(errLower,
		"error unmarshaling input",
		"sops metadata not found",
		"does not match sops' data format",
		"error base64-decoding",
		"unknown datatype",
	) {
		return fmt.Errorf("%w: encrypted values or metadata are invalid; validate or re-encrypt the file with SOPS", ErrMalformedSOPSData)
	}

	return fmt.Errorf("%w: verify the Age key and validate the encrypted file with SOPS", ErrSOPSDecryption)
}

func containsAny(value string, patterns ...string) bool {
	for _, pattern := range patterns {
		if strings.Contains(value, pattern) {
			return true
		}
	}
	return false
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
