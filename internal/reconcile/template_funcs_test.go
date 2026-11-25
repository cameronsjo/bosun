package reconcile

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Masterminds/sprig/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// renderOne writes content to a template file, renders it through
// ExecuteTemplate, and returns the output path and any error.
func renderOne(t *testing.T, data map[string]any, content string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	templateFile := filepath.Join(dir, "test.tmpl")
	require.NoError(t, os.WriteFile(templateFile, []byte(content), 0644))
	outputFile := filepath.Join(dir, "output", "test.txt")
	err := NewTemplateOps(data).ExecuteTemplate(context.Background(), templateFile, outputFile)
	return outputFile, err
}

// TestTemplateFunctionAllowlist_DeniedFuncsFailToParse pins the Template
// Function Allowlist scenarios "Environment access refused" and "Network
// access refused": each denied function fails to parse and writes no output.
func TestTemplateFunctionAllowlist_DeniedFuncsFailToParse(t *testing.T) {
	t.Setenv("BOSUN_TEST_SECRET", "s3cr3t")

	tests := []struct {
		name     string
		function string
		content  string
	}{
		{name: "env", function: "env", content: `{{ env "BOSUN_TEST_SECRET" }}`},
		{name: "expandenv", function: "expandenv", content: `{{ expandenv "$BOSUN_TEST_SECRET" }}`},
		{name: "getHostByName", function: "getHostByName", content: `{{ getHostByName "localhost" }}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputFile, err := renderOne(t, map[string]any{}, tt.content)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "failed to parse template")
			assert.Contains(t, err.Error(), `function "`+tt.function+`" not defined`)
			_, statErr := os.Stat(outputFile)
			assert.ErrorIs(t, statErr, fs.ErrNotExist, "no output file may be written")
		})
	}
}

// TestTemplateFunctionAllowlist_AllowedFuncsRender pins the scenario
// "Allowlisted Sprig functions still available" and guards against stripping
// more than the denied functions (a switch to Sprig's hermetic map would drop
// now and date).
func TestTemplateFunctionAllowlist_AllowedFuncsRender(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "default and upper", content: `{{ .name | default "x" | upper }}`, want: "WEB"},
		{name: "quote", content: `{{ quote "a" }}`, want: `"a"`},
		{name: "b64enc", content: `{{ b64enc "hi" }}`, want: "aGk="},
		{name: "indent", content: `{{ indent 2 "x" }}`, want: "  x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputFile, err := renderOne(t, map[string]any{"name": "web"}, tt.content)
			require.NoError(t, err)
			got, err := os.ReadFile(outputFile)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}

	t.Run("now and date", func(t *testing.T) {
		outputFile, err := renderOne(t, map[string]any{}, `{{ now | date "2006" }}`)
		require.NoError(t, err)
		got, err := os.ReadFile(outputFile)
		require.NoError(t, err)
		assert.Regexp(t, `^\d{4}$`, string(got))
	})
}

// TestTemplateFunctionAllowlist_GoldenList pins the allowlist against the
// Sprig version in go.mod. Every Sprig function must be either allowlisted or
// denied, so a Sprig upgrade that adds a function fails here until the new
// function is reviewed into one list or the other ("Unlisted Sprig function
// refused").
func TestTemplateFunctionAllowlist_GoldenList(t *testing.T) {
	all := sprig.TxtFuncMap()

	t.Run("allowlist is sorted with no duplicates", func(t *testing.T) {
		assert.True(t, slices.IsSorted(allowedSprigFuncs), "allowedSprigFuncs must be sorted")
		assert.Len(t, slices.Compact(slices.Clone(allowedSprigFuncs)), len(allowedSprigFuncs), "allowedSprigFuncs has duplicates")
	})

	t.Run("allowlist has the reviewed size", func(t *testing.T) {
		assert.Len(t, allowedSprigFuncs, 208, "Sprig v3.3.0 has 211 functions; 3 are denied")
	})

	t.Run("denied functions are not allowlisted", func(t *testing.T) {
		for _, name := range deniedSprigFuncs {
			assert.NotContains(t, allowedSprigFuncs, name)
			_, offered := SafeSprigFuncMap()[name]
			assert.False(t, offered, "%s must not be offered to templates", name)
		}
	})

	t.Run("every allowlisted name exists in Sprig", func(t *testing.T) {
		for _, name := range allowedSprigFuncs {
			_, ok := all[name]
			assert.True(t, ok, "allowlisted %q is not a Sprig function", name)
		}
	})

	t.Run("every Sprig function is reviewed", func(t *testing.T) {
		for name := range all {
			reviewed := slices.Contains(allowedSprigFuncs, name) || slices.Contains(deniedSprigFuncs, name)
			assert.True(t, reviewed, "Sprig function %q is in neither allowedSprigFuncs nor deniedSprigFuncs; review it into one", name)
		}
	})

	t.Run("SafeSprigFuncMap offers exactly the allowlist", func(t *testing.T) {
		funcs := SafeSprigFuncMap()
		keys := make([]string, 0, len(funcs))
		for name := range funcs {
			keys = append(keys, name)
		}
		slices.Sort(keys)
		assert.Equal(t, allowedSprigFuncs, keys)
	})
}
