package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestUpgradeCmd_Help(t *testing.T) {
	t.Run("upgrade --help", func(t *testing.T) {
		output, err := executeCmd(t, "upgrade", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "upgrade")
		assert.Contains(t, output, "traefik")
	})
}

func TestUpgradeTraefikCmd_Help(t *testing.T) {
	t.Run("upgrade traefik --help", func(t *testing.T) {
		output, err := executeCmd(t, "upgrade", "traefik", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "HTTPS")
		assert.Contains(t, output, "security headers")
		assert.Contains(t, output, "dry-run")
	})
}

// --- Check function unit tests ---

func TestCheckHTTPSRedirect(t *testing.T) {
	testCases := []struct {
		name       string
		command    any
		wantStatus string
	}{
		{
			name:       "redirect configured (list)",
			command:    []any{"--entrypoints.web.http.redirections.entrypoint.to=websecure", "--api.dashboard=true"},
			wantStatus: "pass",
		},
		{
			name:       "redirect configured (string)",
			command:    "--entrypoints.web.http.redirections.entrypoint.to=websecure --api.dashboard=true",
			wantStatus: "pass",
		},
		{
			name:       "redirect missing",
			command:    []any{"--api.dashboard=true"},
			wantStatus: "missing",
		},
		{
			name:       "no command",
			command:    nil,
			wantStatus: "missing",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &traefikComposeService{Command: tc.command}
			check := checkHTTPSRedirect(svc)
			assert.Equal(t, tc.wantStatus, check.Status)
			assert.Equal(t, "HTTPS Redirect", check.Name)
			if tc.wantStatus == "missing" {
				assert.NotEmpty(t, check.Fix)
			}
		})
	}
}

func TestCheckExposedByDefault(t *testing.T) {
	testCases := []struct {
		name       string
		command    any
		wantStatus string
	}{
		{
			name:       "set to false",
			command:    []any{"--providers.docker.exposedbydefault=false"},
			wantStatus: "pass",
		},
		{
			name:       "set to true",
			command:    []any{"--providers.docker.exposedbydefault=true"},
			wantStatus: "warn",
		},
		{
			name:       "not set",
			command:    []any{"--api.dashboard=true"},
			wantStatus: "warn",
		},
		{
			name:       "no command",
			command:    nil,
			wantStatus: "warn",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &traefikComposeService{Command: tc.command}
			check := checkExposedByDefault(svc)
			assert.Equal(t, tc.wantStatus, check.Status)
			assert.Equal(t, "Exposed By Default", check.Name)
			if tc.wantStatus != "pass" {
				assert.NotEmpty(t, check.Fix)
			}
		})
	}
}

func TestCheckDefaultRule(t *testing.T) {
	testCases := []struct {
		name       string
		command    any
		wantStatus string
	}{
		{
			name:       "default rule configured",
			command:    []any{"--providers.docker.defaultRule=Host(`{{ .Name }}.example.com`)"},
			wantStatus: "pass",
		},
		{
			name:       "default rule missing",
			command:    []any{"--api.dashboard=true"},
			wantStatus: "missing",
		},
		{
			name:       "no command",
			command:    nil,
			wantStatus: "missing",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &traefikComposeService{Command: tc.command}
			check := checkDefaultRule(svc, "")
			assert.Equal(t, tc.wantStatus, check.Status)
			assert.Equal(t, "Default Rule", check.Name)
			if tc.wantStatus == "missing" {
				assert.NotEmpty(t, check.Fix)
			}
		})
	}
}

func TestCheckDefaultRule_DomainAware(t *testing.T) {
	t.Run("uses configured domain in fix", func(t *testing.T) {
		svc := &traefikComposeService{Command: []any{"--api.dashboard=true"}}
		check := checkDefaultRule(svc, "mylab.dev")
		assert.Equal(t, "missing", check.Status)
		assert.Contains(t, check.Fix, "mylab.dev")
		assert.NotContains(t, check.Fix, "example.com")
	})

	t.Run("falls back to example.com when no domain", func(t *testing.T) {
		svc := &traefikComposeService{Command: []any{"--api.dashboard=true"}}
		check := checkDefaultRule(svc, "")
		assert.Equal(t, "missing", check.Status)
		assert.Contains(t, check.Fix, "example.com")
	})
}

func TestCheckSecurityHeaders(t *testing.T) {
	t.Run("middleware exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		middleware := `http:
  middlewares:
    secure-defaults:
      headers:
        stsSeconds: 31536000
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "middlewares.yml"), []byte(middleware), 0644))

		check := checkSecurityHeaders(tmpDir)
		assert.Equal(t, "pass", check.Status)
	})

	t.Run("middleware missing", func(t *testing.T) {
		tmpDir := t.TempDir()
		otherMiddleware := `http:
  middlewares:
    other-middleware:
      rateLimit:
        average: 100
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "middlewares.yml"), []byte(otherMiddleware), 0644))

		check := checkSecurityHeaders(tmpDir)
		assert.Equal(t, "missing", check.Status)
		assert.NotEmpty(t, check.Fix)
	})

	t.Run("empty directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		check := checkSecurityHeaders(tmpDir)
		assert.Equal(t, "missing", check.Status)
	})

	t.Run("no directory", func(t *testing.T) {
		check := checkSecurityHeaders("")
		assert.Equal(t, "missing", check.Status)
	})

	t.Run("middleware in template file", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Template files don't parse as valid YAML, but should still be detected via string search
		tmpl := `{{ if .Values.secure }}
http:
  middlewares:
    secure-defaults:
      headers:
        stsSeconds: 31536000
{{ end }}`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "dynamic.yml"), []byte(tmpl), 0644))

		check := checkSecurityHeaders(tmpDir)
		assert.Equal(t, "pass", check.Status)
	})
}

func TestCheckCompression(t *testing.T) {
	t.Run("compression configured", func(t *testing.T) {
		tmpDir := t.TempDir()
		middleware := `http:
  middlewares:
    default-compress:
      compress:
        minResponseBodyBytes: 1024
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "compress.yml"), []byte(middleware), 0644))

		check := checkCompression(tmpDir)
		assert.Equal(t, "pass", check.Status)
	})

	t.Run("compression missing", func(t *testing.T) {
		tmpDir := t.TempDir()
		check := checkCompression(tmpDir)
		assert.Equal(t, "missing", check.Status)
		assert.NotEmpty(t, check.Fix)
	})

	t.Run("no directory", func(t *testing.T) {
		check := checkCompression("")
		assert.Equal(t, "missing", check.Status)
	})
}

func TestCheckACMEResolver(t *testing.T) {
	testCases := []struct {
		name       string
		command    any
		wantStatus string
	}{
		{
			name:       "ACME configured",
			command:    []any{"--certificatesresolvers.letsencrypt.acme.email=admin@example.com"},
			wantStatus: "pass",
		},
		{
			name:       "ACME missing",
			command:    []any{"--api.dashboard=true"},
			wantStatus: "missing",
		},
		{
			name:       "no command",
			command:    nil,
			wantStatus: "missing",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &traefikComposeService{Command: tc.command}
			check := checkACMEResolver(svc)
			assert.Equal(t, tc.wantStatus, check.Status)
			assert.Equal(t, "ACME Resolver", check.Name)
			if tc.wantStatus == "missing" {
				assert.NotEmpty(t, check.Fix)
			}
		})
	}
}

func TestCheckDockerSocket(t *testing.T) {
	testCases := []struct {
		name       string
		volumes    []string
		wantStatus string
	}{
		{
			name:       "socket mounted",
			volumes:    []string{"/var/run/docker.sock:/var/run/docker.sock"},
			wantStatus: "warn",
		},
		{
			name:       "socket mounted read-only",
			volumes:    []string{"/var/run/docker.sock:/var/run/docker.sock:ro"},
			wantStatus: "warn",
		},
		{
			name:       "no socket mount",
			volumes:    []string{"/data/traefik:/etc/traefik"},
			wantStatus: "pass",
		},
		{
			name:       "no volumes",
			volumes:    nil,
			wantStatus: "pass",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &traefikComposeService{Volumes: tc.volumes}
			check := checkDockerSocket(svc)
			assert.Equal(t, tc.wantStatus, check.Status)
		})
	}
}

// --- Detection tests ---

func TestParseTraefikService(t *testing.T) {
	t.Run("traefik by image", func(t *testing.T) {
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "compose.yml")
		content := `services:
  reverse-proxy:
    image: traefik:v3.2
    command:
      - "--api.dashboard=true"
      - "--providers.docker.exposedbydefault=false"
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./conf.d:/etc/traefik/conf.d
`
		require.NoError(t, os.WriteFile(composePath, []byte(content), 0644))

		svc, err := parseTraefikService(composePath)
		require.NoError(t, err)
		require.NotNil(t, svc)

		assert.Equal(t, "traefik:v3.2", svc.Image)
		assert.True(t, svc.hasCommandFlag("--api.dashboard"))
		assert.True(t, svc.hasCommandFlag("--providers.docker.exposedbydefault"))
		assert.Len(t, svc.Volumes, 2)
	})

	t.Run("traefik by name", func(t *testing.T) {
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "compose.yml")
		content := `services:
  traefik:
    image: custom-traefik:latest
    command: "--api.dashboard=true"
`
		require.NoError(t, os.WriteFile(composePath, []byte(content), 0644))

		svc, err := parseTraefikService(composePath)
		require.NoError(t, err)
		require.NotNil(t, svc)
	})

	t.Run("no traefik service", func(t *testing.T) {
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "compose.yml")
		content := `services:
  web:
    image: nginx:latest
`
		require.NoError(t, os.WriteFile(composePath, []byte(content), 0644))

		svc, err := parseTraefikService(composePath)
		require.NoError(t, err)
		assert.Nil(t, svc)
	})

	t.Run("non-existent file", func(t *testing.T) {
		svc, err := parseTraefikService("/non/existent/file.yml")
		assert.Error(t, err)
		assert.Nil(t, svc)
	})
}

func TestFindTraefikDynamicDir(t *testing.T) {
	t.Run("conf.d volume", func(t *testing.T) {
		tmpDir := t.TempDir()
		confDir := filepath.Join(tmpDir, "conf.d")
		require.NoError(t, os.MkdirAll(confDir, 0755))

		composePath := filepath.Join(tmpDir, "compose.yml")
		svc := &traefikComposeService{
			Volumes: []string{
				"/var/run/docker.sock:/var/run/docker.sock",
				"./conf.d:/etc/traefik/conf.d",
			},
		}

		result := findTraefikDynamicDir(svc, composePath)
		assert.Equal(t, evalSymlinks(t, confDir), evalSymlinks(t, result))
	})

	t.Run("dynamic volume", func(t *testing.T) {
		tmpDir := t.TempDir()
		dynamicDir := filepath.Join(tmpDir, "dynamic")
		require.NoError(t, os.MkdirAll(dynamicDir, 0755))

		composePath := filepath.Join(tmpDir, "compose.yml")
		svc := &traefikComposeService{
			Volumes: []string{
				"./dynamic:/etc/traefik/dynamic",
			},
		}

		result := findTraefikDynamicDir(svc, composePath)
		assert.Equal(t, evalSymlinks(t, dynamicDir), evalSymlinks(t, result))
	})

	t.Run("no dynamic dir", func(t *testing.T) {
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "compose.yml")
		svc := &traefikComposeService{
			Volumes: []string{
				"/var/run/docker.sock:/var/run/docker.sock",
			},
		}

		result := findTraefikDynamicDir(svc, composePath)
		assert.Empty(t, result)
	})

	t.Run("rules volume", func(t *testing.T) {
		tmpDir := t.TempDir()
		rulesDir := filepath.Join(tmpDir, "rules")
		require.NoError(t, os.MkdirAll(rulesDir, 0755))

		composePath := filepath.Join(tmpDir, "compose.yml")
		svc := &traefikComposeService{
			Volumes: []string{
				"./rules:/etc/traefik/rules",
			},
		}

		result := findTraefikDynamicDir(svc, composePath)
		expected, _ := filepath.EvalSymlinks(rulesDir)
		actual, _ := filepath.EvalSymlinks(result)
		assert.Equal(t, expected, actual)
	})
}

func TestFindTraefikComposeFile(t *testing.T) {
	t.Run("explicit path", func(t *testing.T) {
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "my-compose.yml")
		content := `services:
  traefik:
    image: traefik:v3.2
`
		require.NoError(t, os.WriteFile(composePath, []byte(content), 0644))

		result, err := findTraefikComposeFile(nil, composePath)
		assert.NoError(t, err)
		assert.Equal(t, composePath, result)
	})

	t.Run("explicit path not found", func(t *testing.T) {
		_, err := findTraefikComposeFile(nil, "/non/existent/compose.yml")
		assert.Error(t, err)
	})
}

func TestCommandList(t *testing.T) {
	testCases := []struct {
		name     string
		command  any
		expected []string
	}{
		{
			name:     "string command",
			command:  "--flag1 --flag2",
			expected: []string{"--flag1", "--flag2"},
		},
		{
			name:     "list command",
			command:  []any{"--flag1", "--flag2"},
			expected: []string{"--flag1", "--flag2"},
		},
		{
			name:     "nil command",
			command:  nil,
			expected: nil,
		},
		{
			name:     "empty list",
			command:  []any{},
			expected: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &traefikComposeService{Command: tc.command}
			result := svc.commandList()
			if tc.expected == nil {
				assert.Nil(t, result)
			} else {
				assert.Equal(t, tc.expected, result)
			}
		})
	}
}

func TestHasCommandFlag(t *testing.T) {
	svc := &traefikComposeService{
		Command: []any{
			"--api.dashboard=true",
			"--providers.docker.exposedbydefault=false",
			"--entrypoints.web.address=:80",
		},
	}

	assert.True(t, svc.hasCommandFlag("--api.dashboard"))
	assert.True(t, svc.hasCommandFlag("--providers.docker.exposedbydefault"))
	assert.True(t, svc.hasCommandFlag("--entrypoints.web"))
	assert.False(t, svc.hasCommandFlag("--certificatesresolvers"))
	assert.False(t, svc.hasCommandFlag("--nonexistent"))
}

func TestMiddlewareExistsInDir(t *testing.T) {
	t.Run("middleware found", func(t *testing.T) {
		tmpDir := t.TempDir()
		content := `http:
  middlewares:
    secure-defaults:
      headers:
        stsSeconds: 31536000
    default-compress:
      compress:
        minResponseBodyBytes: 1024
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "middlewares.yml"), []byte(content), 0644))

		assert.True(t, middlewareExistsInDir(tmpDir, "secure-defaults"))
		assert.True(t, middlewareExistsInDir(tmpDir, "default-compress"))
		assert.False(t, middlewareExistsInDir(tmpDir, "nonexistent"))
	})

	t.Run("non-yaml files ignored", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "readme.txt"), []byte("secure-defaults"), 0644))
		assert.False(t, middlewareExistsInDir(tmpDir, "secure-defaults"))
	})

	t.Run("empty directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		assert.False(t, middlewareExistsInDir(tmpDir, "secure-defaults"))
	})

	t.Run("non-existent directory", func(t *testing.T) {
		assert.False(t, middlewareExistsInDir("/non/existent/dir", "secure-defaults"))
	})
}

func TestCollectFixes(t *testing.T) {
	checks := []traefikCheck{
		{Name: "A", Status: "pass"},
		{Name: "B", Status: "warn", Fix: "fix-b"},
		{Name: "C", Status: "missing", Fix: "fix-c"},
		{Name: "D", Status: "pass"},
	}

	fixes := collectFixes(checks)
	assert.Len(t, fixes, 2)
	assert.Equal(t, "B", fixes[0].Name)
	assert.Equal(t, "C", fixes[1].Name)
}

func TestCollectFixes_AllPassing(t *testing.T) {
	checks := []traefikCheck{
		{Name: "A", Status: "pass"},
		{Name: "B", Status: "pass"},
	}

	fixes := collectFixes(checks)
	assert.Empty(t, fixes)
}

func TestFileContainsGoTemplate(t *testing.T) {
	t.Run("template file", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.yml.tmpl")
		require.NoError(t, os.WriteFile(path, []byte("value: {{ .Values.domain }}"), 0644))
		assert.True(t, fileContainsGoTemplate(path))
	})

	t.Run("plain file", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.yml")
		require.NoError(t, os.WriteFile(path, []byte("value: example.com"), 0644))
		assert.False(t, fileContainsGoTemplate(path))
	})

	t.Run("non-existent file", func(t *testing.T) {
		assert.False(t, fileContainsGoTemplate("/non/existent/file"))
	})
}

func TestFindMapValue(t *testing.T) {
	t.Run("finds key in mapping", func(t *testing.T) {
		var doc yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte("key1: value1\nkey2: value2"), &doc))
		root := doc.Content[0]

		node := findMapValue(root, "key1")
		require.NotNil(t, node)
		assert.Equal(t, "value1", node.Value)
	})

	t.Run("returns nil for missing key", func(t *testing.T) {
		var doc yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte("key1: value1"), &doc))
		root := doc.Content[0]

		node := findMapValue(root, "nonexistent")
		assert.Nil(t, node)
	})

	t.Run("returns nil for non-mapping node", func(t *testing.T) {
		node := &yaml.Node{Kind: yaml.SequenceNode}
		assert.Nil(t, findMapValue(node, "key"))
	})
}

func TestWriteFilePreservePerms(t *testing.T) {
	t.Run("creates new file with default permissions", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "new-file.txt")

		err := writeFilePreservePerms(path, []byte("hello"))
		require.NoError(t, err)

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "hello", string(data))
	})

	t.Run("preserves existing file permissions", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "existing.txt")

		require.NoError(t, os.WriteFile(path, []byte("old"), 0600))
		err := writeFilePreservePerms(path, []byte("new"))
		require.NoError(t, err)

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "new", string(data))

		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	})
}

func TestAddCommandFlagsToNode(t *testing.T) {
	t.Run("adds flags to sequence command", func(t *testing.T) {
		composeYAML := `services:
  traefik:
    image: traefik:v3.2
    command:
      - "--api.dashboard=true"
`
		var doc yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(composeYAML), &doc))

		err := addCommandFlagsToNode(&doc, []string{"--new.flag=value"})
		require.NoError(t, err)

		// Re-marshal and verify the flag was added
		out, err := yaml.Marshal(&doc)
		require.NoError(t, err)
		assert.Contains(t, string(out), "--new.flag=value")
	})

	t.Run("adds flags to string command", func(t *testing.T) {
		composeYAML := `services:
  traefik:
    image: traefik:v3.2
    command: "--api.dashboard=true"
`
		var doc yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(composeYAML), &doc))

		err := addCommandFlagsToNode(&doc, []string{"--new.flag=value"})
		require.NoError(t, err)

		out, err := yaml.Marshal(&doc)
		require.NoError(t, err)
		assert.Contains(t, string(out), "--new.flag=value")
		assert.Contains(t, string(out), "--api.dashboard=true")
	})

	t.Run("creates command when missing", func(t *testing.T) {
		composeYAML := `services:
  traefik:
    image: traefik:v3.2
`
		var doc yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(composeYAML), &doc))

		err := addCommandFlagsToNode(&doc, []string{"--new.flag=value"})
		require.NoError(t, err)

		out, err := yaml.Marshal(&doc)
		require.NoError(t, err)
		assert.Contains(t, string(out), "--new.flag=value")
	})

	t.Run("rejects invalid document", func(t *testing.T) {
		node := &yaml.Node{Kind: yaml.ScalarNode, Value: "not a document"}
		err := addCommandFlagsToNode(node, []string{"--flag"})
		assert.Error(t, err)
	})

	t.Run("rejects missing services key", func(t *testing.T) {
		composeYAML := `version: "3"`
		var doc yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(composeYAML), &doc))

		err := addCommandFlagsToNode(&doc, []string{"--flag"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "services")
	})

	t.Run("rejects missing traefik service", func(t *testing.T) {
		composeYAML := `services:
  web:
    image: nginx:latest
`
		var doc yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(composeYAML), &doc))

		err := addCommandFlagsToNode(&doc, []string{"--flag"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "traefik")
	})
}

func TestApplyDynamicFixes(t *testing.T) {
	t.Run("creates middlewares file", func(t *testing.T) {
		tmpDir := t.TempDir()

		fixes := []traefikCheck{
			{
				Name:      "Security Headers",
				FixTarget: "dynamic",
				Fix:       securityHeadersMiddleware(),
			},
		}

		err := applyDynamicFixes(tmpDir, fixes)
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(tmpDir, "middlewares.yml"))
		require.NoError(t, err)
		content := string(data)
		assert.Contains(t, content, "secure-defaults")
		assert.Contains(t, content, "stsSeconds")
	})

	t.Run("merges with existing file", func(t *testing.T) {
		tmpDir := t.TempDir()

		existing := `http:
  middlewares:
    existing-mw:
      rateLimit:
        average: 100
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "middlewares.yml"), []byte(existing), 0644))

		fixes := []traefikCheck{
			{
				Name:      "Compression",
				FixTarget: "dynamic",
				Fix:       compressionMiddleware(),
			},
		}

		err := applyDynamicFixes(tmpDir, fixes)
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(tmpDir, "middlewares.yml"))
		require.NoError(t, err)
		content := string(data)
		assert.Contains(t, content, "existing-mw")
		assert.Contains(t, content, "default-compress")
	})

	t.Run("no-op with empty fixes", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := applyDynamicFixes(tmpDir, nil)
		require.NoError(t, err)

		// No file should be created
		_, err = os.Stat(filepath.Join(tmpDir, "middlewares.yml"))
		assert.True(t, os.IsNotExist(err))
	})
}

func TestShowFixes(t *testing.T) {
	t.Run("does not panic with command fixes", func(t *testing.T) {
		fixes := []traefikCheck{
			{
				Name:      "HTTPS Redirect",
				FixTarget: "command",
				Fix:       "--entrypoints.web.http.redirections.entrypoint.to=websecure",
			},
		}
		// showFixes writes to stdout; verify it doesn't panic
		showFixes(fixes, false)
	})

	t.Run("does not panic with dynamic fixes", func(t *testing.T) {
		fixes := []traefikCheck{
			{
				Name:      "Security Headers",
				FixTarget: "dynamic",
				Fix:       securityHeadersMiddleware(),
			},
		}
		showFixes(fixes, false)
	})

	t.Run("does not panic with template mode", func(t *testing.T) {
		fixes := []traefikCheck{
			{
				Name:      "Test",
				FixTarget: "command",
				Fix:       "--flag=value",
			},
		}
		showFixes(fixes, true)
	})

	t.Run("does not panic with mixed fixes", func(t *testing.T) {
		fixes := []traefikCheck{
			{Name: "A", FixTarget: "command", Fix: "--flag-a"},
			{Name: "B", FixTarget: "dynamic", Fix: compressionMiddleware()},
			{Name: "C", FixTarget: "command", Fix: "--flag-c"},
		}
		showFixes(fixes, false)
	})
}

func TestShowGenericRecommendations(t *testing.T) {
	t.Run("does not panic", func(t *testing.T) {
		// showGenericRecommendations writes to stdout; verify it doesn't panic
		showGenericRecommendations()
	})
}

func TestSecurityHeadersMiddleware(t *testing.T) {
	result := securityHeadersMiddleware()
	assert.Contains(t, result, "secure-defaults")
	assert.Contains(t, result, "stsSeconds: 31536000")
	assert.Contains(t, result, "frameDeny: true")
	assert.Contains(t, result, "contentTypeNosniff: true")
	assert.Contains(t, result, "referrerPolicy")
}

func TestCompressionMiddleware(t *testing.T) {
	result := compressionMiddleware()
	assert.Contains(t, result, "default-compress")
	assert.Contains(t, result, "minResponseBodyBytes: 1024")
}

func TestContainsTraefikService(t *testing.T) {
	t.Run("traefik present", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "compose.yml")
		content := `services:
  traefik:
    image: traefik:v3.2
`
		require.NoError(t, os.WriteFile(path, []byte(content), 0644))
		assert.True(t, containsTraefikService(path))
	})

	t.Run("traefik absent", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "compose.yml")
		content := `services:
  web:
    image: nginx:latest
`
		require.NoError(t, os.WriteFile(path, []byte(content), 0644))
		assert.False(t, containsTraefikService(path))
	})

	t.Run("non-existent file", func(t *testing.T) {
		assert.False(t, containsTraefikService("/non/existent.yml"))
	})
}

func TestFindTraefikInDir(t *testing.T) {
	t.Run("finds traefik compose in directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		content := `services:
  traefik:
    image: traefik:v3.2
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "traefik.yml"), []byte(content), 0644))

		result := findTraefikInDir(tmpDir)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "traefik.yml")
	})

	t.Run("skips non-yaml files", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.txt"), []byte("traefik"), 0644))

		result := findTraefikInDir(tmpDir)
		assert.Empty(t, result)
	})

	t.Run("skips directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "subdir.yml"), 0755))

		result := findTraefikInDir(tmpDir)
		assert.Empty(t, result)
	})

	t.Run("non-existent directory", func(t *testing.T) {
		result := findTraefikInDir("/non/existent/dir")
		assert.Empty(t, result)
	})

	t.Run("empty directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		result := findTraefikInDir(tmpDir)
		assert.Empty(t, result)
	})
}

func TestApplyCommandFixes(t *testing.T) {
	t.Run("adds flags to compose file", func(t *testing.T) {
		tmpDir := t.TempDir()
		composePath := filepath.Join(tmpDir, "compose.yml")
		content := `services:
  traefik:
    image: traefik:v3.2
    command:
      - "--api.dashboard=true"
`
		require.NoError(t, os.WriteFile(composePath, []byte(content), 0644))

		svc := &traefikComposeService{
			Command: []any{"--api.dashboard=true"},
		}

		err := applyCommandFixes(composePath, svc, []string{"--new.flag=value"})
		require.NoError(t, err)

		data, err := os.ReadFile(composePath)
		require.NoError(t, err)
		assert.Contains(t, string(data), "--new.flag=value")
	})

	t.Run("non-existent file returns error", func(t *testing.T) {
		err := applyCommandFixes("/non/existent.yml", nil, []string{"--flag"})
		assert.Error(t, err)
	})
}
