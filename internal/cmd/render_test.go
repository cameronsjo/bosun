package cmd

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderCmd_Registration(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"render"})
	require.NoError(t, err)
	assert.Equal(t, "render", cmd.Name())
}

func TestRenderCmd_Help(t *testing.T) {
	t.Run("render --help shows description", func(t *testing.T) {
		output, err := executeCmd(t, "render", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "render")
		assert.Contains(t, output, "templates")
		assert.Contains(t, output, "SOPS")
	})

	t.Run("render --help shows examples", func(t *testing.T) {
		output, err := executeCmd(t, "render", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "bosun render")
		assert.Contains(t, output, "config.yml.tmpl")
	})

	t.Run("render --help shows template functions", func(t *testing.T) {
		output, err := executeCmd(t, "render", "--help")
		require.NoError(t, err)
		assert.Contains(t, output, "sprig")
		assert.Contains(t, output, "include")
		assert.Contains(t, output, "fromJsonFile")
	})
}

func TestRenderCmd_NoAliases(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"render"})
	require.NoError(t, err)
	assert.Empty(t, cmd.Aliases, "render command should have no aliases")
}

func TestBosunRenderFuncs(t *testing.T) {
	funcs := bosunRenderFuncs()

	t.Run("has include function", func(t *testing.T) {
		_, ok := funcs["include"]
		assert.True(t, ok, "bosunRenderFuncs should contain 'include'")
	})

	t.Run("has fromJsonFile function", func(t *testing.T) {
		_, ok := funcs["fromJsonFile"]
		assert.True(t, ok, "bosunRenderFuncs should contain 'fromJsonFile'")
	})

	t.Run("include reads file", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := tmpDir + "/test.txt"
		require.NoError(t, os.WriteFile(testFile, []byte("hello world"), 0644))

		includeFn := funcs["include"].(func(string) (string, error))
		result, err := includeFn(testFile)
		require.NoError(t, err)
		assert.Equal(t, "hello world", result)
	})

	t.Run("include returns error for missing file", func(t *testing.T) {
		includeFn := funcs["include"].(func(string) (string, error))
		_, err := includeFn("/nonexistent/file.txt")
		assert.Error(t, err)
	})

	t.Run("fromJsonFile parses JSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := tmpDir + "/data.json"
		require.NoError(t, os.WriteFile(testFile, []byte(`{"key": "value"}`), 0644))

		fromJsonFn := funcs["fromJsonFile"].(func(string) (any, error))
		result, err := fromJsonFn(testFile)
		require.NoError(t, err)

		data, ok := result.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "value", data["key"])
	})

	t.Run("fromJsonFile returns error for invalid JSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := tmpDir + "/bad.json"
		require.NoError(t, os.WriteFile(testFile, []byte("not json"), 0644))

		fromJsonFn := funcs["fromJsonFile"].(func(string) (any, error))
		_, err := fromJsonFn(testFile)
		assert.Error(t, err)
	})

	t.Run("fromJsonFile returns error for missing file", func(t *testing.T) {
		fromJsonFn := funcs["fromJsonFile"].(func(string) (any, error))
		_, err := fromJsonFn("/nonexistent/data.json")
		assert.Error(t, err)
	})
}
