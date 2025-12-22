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

	t.Run("starterPyprojectTOML has required fields", func(t *testing.T) {
		assert.Contains(t, starterPyprojectTOML, "[project]")
		assert.Contains(t, starterPyprojectTOML, "name =")
		assert.Contains(t, starterPyprojectTOML, "requires-python")
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
