package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

var compressionFixes = []traefikCheck{{Name: "Compression", FixTarget: "dynamic", Fix: compressionMiddleware()}}

// TestApplyDynamicFixes_RefusesUnexpectedShapes covers the error returns: a
// dynamic config whose structure is not what the edit expects is reported and
// left byte for byte, never rewritten.
func TestApplyDynamicFixes_RefusesUnexpectedShapes(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		wantErr  string
	}{
		{name: "top level is a list", existing: "- a\n- b\n", wantErr: "top level is not a mapping"},
		{name: "http is a list", existing: "http: [a]\n", wantErr: `"http" is not a mapping`},
		{name: "middlewares is a list", existing: "http:\n  middlewares: [a]\n", wantErr: `"middlewares" is not a mapping`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "middlewares.yml")
			require.NoError(t, os.WriteFile(path, []byte(tt.existing), 0o644))

			err := applyDynamicFixes(dir, compressionFixes)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			data, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			assert.Equal(t, tt.existing, string(data))
		})
	}

	t.Run("target is a directory", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, "middlewares.yml"), 0o755))

		err := applyDynamicFixes(dir, compressionFixes)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read")
	})

	t.Run("target file cannot be written", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root ignores file modes")
		}
		dir := t.TempDir()
		path := filepath.Join(dir, "middlewares.yml")
		require.NoError(t, os.WriteFile(path, []byte("http:\n  middlewares: {}\n"), 0o444))

		err := applyDynamicFixes(dir, compressionFixes)

		require.Error(t, err)
	})
}

// TestApplyDynamicFixes_CreatesMissingParts covers a dynamic config that has no
// http or middlewares section yet, including null values and an empty file.
func TestApplyDynamicFixes_CreatesMissingParts(t *testing.T) {
	tests := []struct {
		name     string
		existing string
	}{
		{name: "empty file", existing: ""},
		{name: "empty mapping", existing: "{}\n"},
		{name: "null http", existing: "http:\n"},
		{name: "null middlewares", existing: "http:\n  middlewares:\n"},
		{name: "only tls", existing: "tls:\n  options:\n    default:\n      minVersion: VersionTLS12\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "middlewares.yml")
			require.NoError(t, os.WriteFile(path, []byte(tt.existing), 0o644))

			require.NoError(t, applyDynamicFixes(dir, compressionFixes))

			data, err := os.ReadFile(path)
			require.NoError(t, err)
			var got struct {
				HTTP struct {
					Middlewares map[string]any `yaml:"middlewares"`
				} `yaml:"http"`
			}
			require.NoError(t, yaml.Unmarshal(data, &got))
			assert.Contains(t, got.HTTP.Middlewares, "default-compress")
		})
	}

	t.Run("creates the file in an empty directory", func(t *testing.T) {
		dir := t.TempDir()

		require.NoError(t, applyDynamicFixes(dir, compressionFixes))

		data, err := os.ReadFile(filepath.Join(dir, "middlewares.yml"))
		require.NoError(t, err)
		assert.Contains(t, string(data), "default-compress")
	})
}

// TestApplyDynamicFixes_LeavesFileWhenMiddlewareExists pins that nothing is
// rewritten when every recommended middleware is already defined.
func TestApplyDynamicFixes_LeavesFileWhenMiddlewareExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "middlewares.yml")
	existing := "# hand written\nhttp:\n    middlewares:\n        default-compress:\n            compress: {}\n"
	require.NoError(t, os.WriteFile(path, []byte(existing), 0o644))

	require.NoError(t, applyDynamicFixes(dir, compressionFixes))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, existing, string(data))
}

// TestApplyCommandFixes_RejectsUnparseableCompose covers the parse error.
func TestApplyCommandFixes_RejectsUnparseableCompose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "compose.yml")
	content := "services:\n  traefik: [unclosed\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	err := applyCommandFixes(path, nil, []string{"--new.flag=value"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse compose file")
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, content, string(data))
}

// TestTraefikServicePick_IsDeterministic pins that parseTraefikService and
// addCommandFlagsToNode choose the same service when two match: the lowest name.
func TestTraefikServicePick_IsDeterministic(t *testing.T) {
	const compose = `services:
  zeta:
    image: traefik:v3.2
    command:
      - "--from=zeta"
  alpha:
    image: traefik:v3.2
    command:
      - "--from=alpha"
`
	path := filepath.Join(t.TempDir(), "compose.yml")
	require.NoError(t, os.WriteFile(path, []byte(compose), 0o644))

	for i := 0; i < 20; i++ {
		svc, err := parseTraefikService(path)
		require.NoError(t, err)
		require.NotNil(t, svc)
		assert.Equal(t, []string{"--from=alpha"}, svc.commandList())
	}

	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(compose), &doc))
	require.NoError(t, addCommandFlagsToNode(&doc, []string{"--new.flag=value"}))
	assert.Contains(t, composeCommand(t, &doc, "alpha"), "--new.flag=value")
	assert.NotContains(t, composeCommand(t, &doc, "zeta"), "--new.flag=value")
}

// TestUpgradeTraefikCmd_PromptNeedsTTY pins that declining to prompt is an
// error rather than a silent success: without --yes or --dry-run and without a
// terminal, nothing can confirm the fixes.
func TestUpgradeTraefikCmd_PromptNeedsTTY(t *testing.T) {
	if isTerminal() {
		t.Skip("stdin is a terminal; the prompt would block")
	}
	resetUpgradeTraefikFlags(t)
	compose := filepath.Join(t.TempDir(), "compose.yml")
	require.NoError(t, os.WriteFile(compose, []byte("services:\n  traefik:\n    image: traefik:v3.2\n"), 0o644))

	_, err := executeCmd(t, "upgrade", "traefik", "--compose", compose)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "TTY")
}
