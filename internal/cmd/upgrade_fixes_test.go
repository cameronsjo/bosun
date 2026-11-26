package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// composeCommand parses a marshalled compose document and returns the command
// list of one service, so a test can assert the exact flags.
func composeCommand(t *testing.T, doc *yaml.Node, service string) []string {
	t.Helper()

	out, err := yaml.Marshal(doc)
	require.NoError(t, err)

	var compose struct {
		Services map[string]struct {
			Command []string `yaml:"command"`
		} `yaml:"services"`
	}
	require.NoError(t, yaml.Unmarshal(out, &compose))
	require.Contains(t, compose.Services, service)
	return compose.Services[service].Command
}

// resetUpgradeTraefikFlags restores the package-level flag variables, which
// cobra leaves at whatever the previous Execute call set.
func resetUpgradeTraefikFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		upgradeDryRun = false
		upgradeYes = false
		upgradeComposePath = ""
		upgradeDynamicDir = ""
	})
}

// TestAddCommandFlagsToNode_TraefikServiceNamedOtherThanTraefik pins that the
// service is found the same way parseTraefikService finds it: by image or by
// name. Before the fix the lookup was hardcoded to a service named traefik.
func TestAddCommandFlagsToNode_TraefikServiceNamedOtherThanTraefik(t *testing.T) {
	tests := []struct {
		name    string
		compose string
		service string
	}{
		{
			name: "service named proxy with a traefik image",
			compose: `services:
  proxy:
    image: traefik:v3.2
    command:
      - "--api.dashboard=true"
`,
			service: "proxy",
		},
		{
			name: "service named edge with a traefik/traefik image",
			compose: `services:
  web:
    image: nginx:latest
  edge:
    image: traefik/traefik:v3
`,
			service: "edge",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var doc yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(tt.compose), &doc))

			require.NoError(t, addCommandFlagsToNode(&doc, []string{"--new.flag=value"}))

			assert.Contains(t, composeCommand(t, &doc, tt.service), "--new.flag=value")
		})
	}
}

// TestAddCommandFlagsToNode_ReplacesExistingFlag pins that a fix replaces the
// flag it supersedes. Before the fix it was appended, leaving both
// --providers.docker.exposedbydefault=true and =false in the command.
func TestAddCommandFlagsToNode_ReplacesExistingFlag(t *testing.T) {
	tests := []struct {
		name    string
		compose string
		flags   []string
		want    []string
	}{
		{
			name: "sequence command, flag set to true",
			compose: `services:
  traefik:
    image: traefik:v3.2
    command:
      - "--api.dashboard=true"
      - "--providers.docker.exposedbydefault=true"
      - "--log.level=INFO"
`,
			flags: []string{"--providers.docker.exposedbydefault=false"},
			want: []string{
				"--api.dashboard=true",
				"--providers.docker.exposedbydefault=false",
				"--log.level=INFO",
			},
		},
		{
			name: "string command, flag set to true",
			compose: `services:
  traefik:
    image: traefik:v3.2
    command: "--api.dashboard=true --providers.docker.exposedbydefault=true"
`,
			flags: []string{"--providers.docker.exposedbydefault=false"},
			want: []string{
				"--api.dashboard=true",
				"--providers.docker.exposedbydefault=false",
			},
		},
		{
			name: "flag name differs only in case",
			compose: `services:
  traefik:
    image: traefik:v3.2
    command:
      - "--providers.docker.exposedByDefault=true"
`,
			flags: []string{"--providers.docker.exposedbydefault=false"},
			want:  []string{"--providers.docker.exposedbydefault=false"},
		},
		{
			name: "absent flag is appended and others are kept in order",
			compose: `services:
  traefik:
    image: traefik:v3.2
    command:
      - "--api.dashboard=true"
`,
			flags: []string{
				"--entrypoints.web.http.redirections.entrypoint.to=websecure",
				"--entrypoints.web.http.redirections.entrypoint.scheme=https",
			},
			want: []string{
				"--api.dashboard=true",
				"--entrypoints.web.http.redirections.entrypoint.to=websecure",
				"--entrypoints.web.http.redirections.entrypoint.scheme=https",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var doc yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(tt.compose), &doc))

			require.NoError(t, addCommandFlagsToNode(&doc, tt.flags))

			assert.Equal(t, tt.want, composeCommand(t, &doc, "traefik"))
		})
	}
}

// TestApplyDynamicFixes_KeepsExistingConfig pins that adding a middleware does
// not rewrite the rest of the dynamic config. Before the fix the file was
// re-marshalled from a struct holding only http.middlewares, which dropped
// routers, services, tls and comments, and overwrote a file with no
// middlewares key or one that did not parse.
func TestApplyDynamicFixes_KeepsExistingConfig(t *testing.T) {
	fixes := []traefikCheck{{Name: "Compression", FixTarget: "dynamic", Fix: compressionMiddleware()}}

	t.Run("keeps routers, services, tls and comments next to middlewares", func(t *testing.T) {
		dir := t.TempDir()
		existing := `# managed by hand
http:
  routers:
    dashboard:
      rule: Host("traefik.example.com")
      service: api@internal
  services:
    backend:
      loadBalancer:
        servers:
          - url: http://10.0.0.5:8080
  middlewares:
    existing-mw:
      rateLimit:
        average: 100
tls:
  options:
    default:
      minVersion: VersionTLS12
`
		require.NoError(t, os.WriteFile(filepath.Join(dir, "middlewares.yml"), []byte(existing), 0o644))

		require.NoError(t, applyDynamicFixes(dir, fixes))

		data, err := os.ReadFile(filepath.Join(dir, "middlewares.yml"))
		require.NoError(t, err)
		assert.Contains(t, string(data), "# managed by hand")

		var got map[string]any
		require.NoError(t, yaml.Unmarshal(data, &got))
		httpSection, ok := got["http"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, httpSection, "routers")
		assert.Contains(t, httpSection, "services")
		assert.Contains(t, got, "tls")
		middlewares, ok := httpSection["middlewares"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, middlewares, "existing-mw")
		assert.Contains(t, middlewares, "default-compress")
	})

	t.Run("keeps a file that has routers but no middlewares key", func(t *testing.T) {
		dir := t.TempDir()
		existing := `http:
  routers:
    dashboard:
      rule: Host("traefik.example.com")
      service: api@internal
`
		require.NoError(t, os.WriteFile(filepath.Join(dir, "middlewares.yml"), []byte(existing), 0o644))

		require.NoError(t, applyDynamicFixes(dir, fixes))

		data, err := os.ReadFile(filepath.Join(dir, "middlewares.yml"))
		require.NoError(t, err)
		var got map[string]any
		require.NoError(t, yaml.Unmarshal(data, &got))
		httpSection, ok := got["http"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, httpSection, "routers")
		assert.Contains(t, httpSection, "middlewares")
	})

	t.Run("refuses to overwrite a file that does not parse", func(t *testing.T) {
		dir := t.TempDir()
		existing := "http:\n  middlewares: [unclosed\n"
		path := filepath.Join(dir, "middlewares.yml")
		require.NoError(t, os.WriteFile(path, []byte(existing), 0o644))

		err := applyDynamicFixes(dir, fixes)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "middlewares.yml")
		data, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		assert.Equal(t, existing, string(data), "the unparseable file must be left byte for byte")
	})

	t.Run("adds to the file that already defines middlewares", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a-routers.yml"), []byte("http:\n  routers:\n    r:\n      rule: Host(`a.example.com`)\n      service: s\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "b-security.yml"), []byte("http:\n  middlewares:\n    secure-defaults:\n      headers:\n        frameDeny: true\n"), 0o644))

		require.NoError(t, applyDynamicFixes(dir, fixes))

		data, err := os.ReadFile(filepath.Join(dir, "b-security.yml"))
		require.NoError(t, err)
		assert.Contains(t, string(data), "default-compress")
		assert.Contains(t, string(data), "secure-defaults")
		_, statErr := os.Stat(filepath.Join(dir, "middlewares.yml"))
		assert.True(t, os.IsNotExist(statErr), "no second middlewares file should be created")
	})
}

// TestUpgradeTraefikCmd_ExitStatus pins that a failure is an error return, so
// the process exits non-zero. Before the fix the command used Run and always
// exited 0.
func TestUpgradeTraefikCmd_ExitStatus(t *testing.T) {
	const traefikCompose = `services:
  traefik:
    image: traefik:v3.2
    command:
      - "--api.dashboard=true"
`

	t.Run("missing compose file fails", func(t *testing.T) {
		resetUpgradeTraefikFlags(t)
		missing := filepath.Join(t.TempDir(), "does-not-exist.yml")

		_, err := executeCmd(t, "upgrade", "traefik", "--compose", missing)

		assert.Error(t, err)
	})

	t.Run("compose file without a traefik service fails", func(t *testing.T) {
		resetUpgradeTraefikFlags(t)
		compose := filepath.Join(t.TempDir(), "compose.yml")
		require.NoError(t, os.WriteFile(compose, []byte("services:\n  web:\n    image: nginx:latest\n"), 0o644))

		_, err := executeCmd(t, "upgrade", "traefik", "--compose", compose)

		assert.Error(t, err)
	})

	t.Run("a fix that cannot be applied fails", func(t *testing.T) {
		resetUpgradeTraefikFlags(t)
		dir := t.TempDir()
		compose := filepath.Join(dir, "compose.yml")
		require.NoError(t, os.WriteFile(compose, []byte(traefikCompose), 0o644))
		dynamic := filepath.Join(dir, "dynamic")
		require.NoError(t, os.Mkdir(dynamic, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dynamic, "middlewares.yml"), []byte("http:\n  middlewares: [unclosed\n"), 0o644))

		_, err := executeCmd(t, "upgrade", "traefik", "--compose", compose, "--dynamic", dynamic, "--yes")

		assert.Error(t, err)
	})

	t.Run("dry run with findings still succeeds", func(t *testing.T) {
		resetUpgradeTraefikFlags(t)
		compose := filepath.Join(t.TempDir(), "compose.yml")
		require.NoError(t, os.WriteFile(compose, []byte(traefikCompose), 0o644))

		_, err := executeCmd(t, "upgrade", "traefik", "--compose", compose, "--dry-run")

		assert.NoError(t, err)
	})
}
