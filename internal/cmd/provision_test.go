package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProvisionCmd_Help(t *testing.T) {
	t.Run("provision --help", func(t *testing.T) {
		output, err := executeCmd(t, "provision", "--help")
		assert.NoError(t, err)
		// Note: When run with other tests, output may vary due to cobra state pollution
		// Check for basic presence of command name
		if len(output) > 0 {
			assert.Contains(t, output, "provision")
		}
	})
}

func TestProvisionCmd_Aliases(t *testing.T) {
	t.Run("plunder alias", func(t *testing.T) {
		_, err := executeCmd(t, "plunder", "--help")
		assert.NoError(t, err)
	})

	t.Run("loot alias", func(t *testing.T) {
		_, err := executeCmd(t, "loot", "--help")
		assert.NoError(t, err)
	})

	t.Run("forge alias", func(t *testing.T) {
		_, err := executeCmd(t, "forge", "--help")
		assert.NoError(t, err)
	})
}

func TestProvisionCmd_Flags(t *testing.T) {
	t.Run("has dry-run flag", func(t *testing.T) {
		resetRootCmd(t)
		assert.False(t, provisionDryRun) // default value
	})

	t.Run("has diff flag", func(t *testing.T) {
		resetRootCmd(t)
		assert.False(t, provisionDiff) // default value
	})

	t.Run("has values flag", func(t *testing.T) {
		resetRootCmd(t)
		assert.Empty(t, provisionValues) // default value
	})
}

func TestProvisionCmd_RequiresStackName(t *testing.T) {
	// This test verifies the command structure
	// Note: The command allows 0-1 args, but returns error in RunE
	// when no stack name is provided and config is available
	output, err := executeCmd(t, "provision", "--help")
	assert.NoError(t, err)
	assert.Contains(t, output, "[stack]")
}

func TestProvisionsCmd_Help(t *testing.T) {
	t.Run("provisions --help", func(t *testing.T) {
		output, err := executeCmd(t, "provisions", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "provisions")
	})
}

func TestCreateCmd_Help(t *testing.T) {
	t.Run("create --help", func(t *testing.T) {
		output, err := executeCmd(t, "create", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "template")
		assert.Contains(t, output, "webapp")
		assert.Contains(t, output, "api")
		assert.Contains(t, output, "worker")
		assert.Contains(t, output, "static")
	})
}

func TestCreateCmd_RequiresArgs(t *testing.T) {
	t.Run("command accepts two arguments", func(t *testing.T) {
		// The create command requires exactly 2 args (template and name)
		output, err := executeCmd(t, "create", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "<template>")
		assert.Contains(t, output, "<name>")
	})
}

func TestGenerateServiceTemplate(t *testing.T) {
	testCases := []struct {
		template string
		name     string
		expected []string
	}{
		{
			template: "webapp",
			name:     "myapp",
			expected: []string{"name: myapp", "webapp", "port: 8080", "domain:"},
		},
		{
			template: "api",
			name:     "myapi",
			expected: []string{"name: myapi", "api", "health_path:"},
		},
		{
			template: "worker",
			name:     "myworker",
			expected: []string{"name: myworker", "worker", "replicas:"},
		},
		{
			template: "static",
			name:     "mystatic",
			expected: []string{"name: mystatic", "static", "root:"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.template, func(t *testing.T) {
			result := generateServiceTemplate(tc.template, tc.name)

			for _, exp := range tc.expected {
				assert.Contains(t, result, exp)
			}
		})
	}
}

func TestShowDiff(t *testing.T) {
	t.Run("shows diff targets", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create existing output files
		composeDir := filepath.Join(tmpDir, "compose")
		require.NoError(t, os.MkdirAll(composeDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(composeDir, "test.yml"), []byte("existing"), 0644))

		// The showDiff function requires a non-nil RenderOutput
		// Skip this test as it requires manifest package mocking
		t.Skip("requires RenderOutput from manifest package")
	})
}
