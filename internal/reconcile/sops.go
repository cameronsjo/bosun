package reconcile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// SOPSOps provides SOPS decryption operations.
type SOPSOps struct{}

// NewSOPSOps creates a new SOPSOps instance.
func NewSOPSOps() *SOPSOps {
	return &SOPSOps{}
}

// Decrypt decrypts a SOPS-encrypted file and returns the plaintext bytes.
func (s *SOPSOps) Decrypt(ctx context.Context, file string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "sops", "--input-type", "yaml", "--output-type", "json", "-d", file)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("sops decrypt failed for %s: %w: %s", file, err, stderr.String())
	}
	return stdout.Bytes(), nil
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

// DecryptMultiple decrypts multiple SOPS files and merges them into a single map.
// Later files override earlier ones for duplicate keys.
func (s *SOPSOps) DecryptMultiple(ctx context.Context, files []string) (map[string]any, error) {
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
	merged, err := s.DecryptMultiple(ctx, files)
	if err != nil {
		return nil, err
	}
	return json.Marshal(merged)
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
