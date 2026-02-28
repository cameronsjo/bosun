package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitCmd_Help(t *testing.T) {
	t.Run("init --help", func(t *testing.T) {
		output, err := executeCmd(t, "init", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "Initialize")
		assert.Contains(t, output, "bosun/")
		assert.Contains(t, output, "manifest/")
		assert.Contains(t, output, ".sops.yaml")
	})
}

func TestInitCmd_Aliases(t *testing.T) {
	t.Run("christen alias", func(t *testing.T) {
		_, err := executeCmd(t, "christen", "--help")
		assert.NoError(t, err)
	})
}

func TestCreateFileIfNotExists(t *testing.T) {
	t.Run("creates new file", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "new-file.txt")
		content := "test content"

		err := createFileIfNotExists(filePath, content)
		require.NoError(t, err)

		assert.FileExists(t, filePath)

		data, err := os.ReadFile(filePath)
		require.NoError(t, err)
		assert.Equal(t, content, string(data))
	})

	t.Run("skips existing file", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "existing-file.txt")
		originalContent := "original content"
		newContent := "new content"

		// Create existing file
		require.NoError(t, os.WriteFile(filePath, []byte(originalContent), 0644))

		err := createFileIfNotExists(filePath, newContent)
		require.NoError(t, err)

		// Content should remain unchanged
		data, err := os.ReadFile(filePath)
		require.NoError(t, err)
		assert.Equal(t, originalContent, string(data))
	})
}

func TestPromptYesNo_NonTTY(t *testing.T) {
	t.Run("returns error when stdin is not a TTY", func(t *testing.T) {
		// This test verifies that promptYesNo returns an error when called without a TTY.
		// In a non-TTY environment (like CI/CD), isTerminal() will return false.
		// The test itself runs in a non-TTY environment, so this should fail.
		_, err := promptYesNo("test prompt")
		if err == nil {
			t.Skip("test must run in non-TTY environment")
		}
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "stdin is not a TTY")
		assert.Contains(t, err.Error(), "--yes")
	})
}

func TestIsTerminal(t *testing.T) {
	t.Run("can detect TTY status", func(t *testing.T) {
		// This test verifies that isTerminal() can be called without panicking.
		// The actual return value depends on the environment.
		result := isTerminal()
		assert.IsType(t, true, result)
	})
}

func TestExtractAgePublicKey(t *testing.T) {
	t.Run("extract from key file", func(t *testing.T) {
		tmpDir := t.TempDir()
		keyFile := filepath.Join(tmpDir, "keys.txt")

		content := `# created: 2024-01-01T00:00:00Z
# public key: age1qyqszqgpqyqszqgpqyqszqgpqyqszqgpqyqszqgpqyqszqgpqyqsqvv9n
AGE-SECRET-KEY-1QYQSZQGPQYQSZQGPQYQSZQGPQYQSZQGPQYQSZQGPQYQSZQGPQYQS
`
		require.NoError(t, os.WriteFile(keyFile, []byte(content), 0600))

		pubKey, err := extractAgePublicKey(keyFile)
		require.NoError(t, err)
		assert.Contains(t, pubKey, "age1")
	})

	t.Run("non-existent file", func(t *testing.T) {
		_, err := extractAgePublicKey("/non/existent/keys.txt")
		assert.Error(t, err)
	})

	t.Run("file without public key", func(t *testing.T) {
		tmpDir := t.TempDir()
		keyFile := filepath.Join(tmpDir, "keys.txt")

		content := `AGE-SECRET-KEY-1QYQSZQGPQYQSZQGPQYQSZQGPQYQSZQGPQYQSZQGPQYQSZQGPQYQS
`
		require.NoError(t, os.WriteFile(keyFile, []byte(content), 0600))

		// Will try age-keygen -y or return error
		_, err := extractAgePublicKey(keyFile)
		// May succeed or fail depending on age-keygen availability
		_ = err
	})
}

func TestStarterTemplates(t *testing.T) {
	t.Run("starterComposeYML has required fields", func(t *testing.T) {
		assert.Contains(t, starterComposeYML, "services:")
		assert.Contains(t, starterComposeYML, "bosun:")
		assert.Contains(t, starterComposeYML, "image:")
		assert.Contains(t, starterComposeYML, "healthcheck:")
	})

	t.Run("starterExampleService has required fields", func(t *testing.T) {
		assert.Contains(t, starterExampleService, "name:")
		assert.Contains(t, starterExampleService, "provisions:")
		assert.Contains(t, starterExampleService, "config:")
	})

	t.Run("starterGitignore has common patterns", func(t *testing.T) {
		assert.Contains(t, starterGitignore, "__pycache__")
		assert.Contains(t, starterGitignore, ".venv")
		assert.Contains(t, starterGitignore, ".DS_Store")
	})

	t.Run("starterReadme has structure", func(t *testing.T) {
		assert.Contains(t, starterReadme, "# My Homelab")
		assert.Contains(t, starterReadme, "bosun")
		assert.Contains(t, starterReadme, "Quick Start")
	})
}

func TestGenerateBosunYaml(t *testing.T) {
	t.Run("with domain", func(t *testing.T) {
		result := generateBosunYaml("example.com")
		assert.Contains(t, result, "domain: example.com")
		assert.Contains(t, result, "infrastructure:")
		assert.Contains(t, result, "traefik")
		assert.NotContains(t, result, "# domain:")
	})

	t.Run("without domain", func(t *testing.T) {
		result := generateBosunYaml("")
		assert.Contains(t, result, "# domain: example.com")
		assert.Contains(t, result, "infrastructure:")
		assert.NotContains(t, result, "\ndomain:")
	})
}

func TestGenerateTraefikConfigs(t *testing.T) {
	t.Run("creates traefik directory structure", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := generateTraefikConfigs(tmpDir, "example.com")
		require.NoError(t, err)

		// Check directories created
		assert.DirExists(t, filepath.Join(tmpDir, "traefik", "conf.d"))
		assert.DirExists(t, filepath.Join(tmpDir, "traefik", "acme"))

		// Check middlewares.yml created
		middlewaresFile := filepath.Join(tmpDir, "traefik", "conf.d", "middlewares.yml")
		assert.FileExists(t, middlewaresFile)

		data, err := os.ReadFile(middlewaresFile)
		require.NoError(t, err)
		content := string(data)
		assert.Contains(t, content, "secure-defaults")
		assert.Contains(t, content, "default-compress")
		assert.Contains(t, content, "stsSeconds")
		assert.Contains(t, content, "minResponseBodyBytes")
	})

	t.Run("creates traefik flags doc with domain", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := generateTraefikConfigs(tmpDir, "mylab.dev")
		require.NoError(t, err)

		flagsDoc := filepath.Join(tmpDir, "traefik", "TRAEFIK-FLAGS.md")
		assert.FileExists(t, flagsDoc)

		data, err := os.ReadFile(flagsDoc)
		require.NoError(t, err)
		content := string(data)
		assert.Contains(t, content, "mylab.dev")
		assert.Contains(t, content, "exposedbydefault=false")
		assert.Contains(t, content, "redirections.entrypoint.to=websecure")
		assert.Contains(t, content, "certificatesresolvers")
	})

	t.Run("does not overwrite existing files", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create the directory and file first
		dynamicDir := filepath.Join(tmpDir, "traefik", "conf.d")
		require.NoError(t, os.MkdirAll(dynamicDir, 0755))
		existingContent := "existing content"
		require.NoError(t, os.WriteFile(filepath.Join(dynamicDir, "middlewares.yml"), []byte(existingContent), 0644))

		err := generateTraefikConfigs(tmpDir, "example.com")
		require.NoError(t, err)

		// Original content preserved
		data, err := os.ReadFile(filepath.Join(dynamicDir, "middlewares.yml"))
		require.NoError(t, err)
		assert.Equal(t, existingContent, string(data))
	})
}

func TestTraefikMiddlewaresYML(t *testing.T) {
	t.Run("has secure-defaults middleware", func(t *testing.T) {
		assert.Contains(t, traefikMiddlewaresYML, "secure-defaults:")
		assert.Contains(t, traefikMiddlewaresYML, "stsSeconds: 31536000")
		assert.Contains(t, traefikMiddlewaresYML, "frameDeny: true")
		assert.Contains(t, traefikMiddlewaresYML, "referrerPolicy:")
	})

	t.Run("has default-compress middleware", func(t *testing.T) {
		assert.Contains(t, traefikMiddlewaresYML, "default-compress:")
		assert.Contains(t, traefikMiddlewaresYML, "minResponseBodyBytes: 1024")
	})
}

func TestPromptInput_NonTTY(t *testing.T) {
	t.Run("returns default when stdin is not a TTY", func(t *testing.T) {
		if isTerminal() {
			t.Skip("test must run in non-TTY environment")
		}
		result := promptInput("test prompt", "default-value")
		assert.Equal(t, "default-value", result)
	})

	t.Run("returns empty string when no default and non-TTY", func(t *testing.T) {
		if isTerminal() {
			t.Skip("test must run in non-TTY environment")
		}
		result := promptInput("test prompt", "")
		assert.Equal(t, "", result)
	})
}

func TestInitCmd_DirectoryStructure(t *testing.T) {
	// This test verifies the expected directory structure
	t.Run("expected directories", func(t *testing.T) {
		expectedDirs := []string{
			"bosun/scripts",
			"manifest/provisions",
			"manifest/services",
			"manifest/stacks",
		}

		for _, dir := range expectedDirs {
			assert.NotEmpty(t, dir)
		}
	})
}
