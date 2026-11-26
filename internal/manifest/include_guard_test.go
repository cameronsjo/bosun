package manifest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// includeRecursionChildEnv selects the mode of TestIncludeRecursionChild. It
// is unset in a normal run, which skips the child.
const includeRecursionChildEnv = "BOSUN_TEST_INCLUDE_RECURSION_CHILD"

func writeTemplateFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

// writeIncludeChain writes chain0..chainN where each file includes the next
// and the last one is plain text. Rendering {{ include "chain0" . }} then
// reaches the last file at include depth N+1.
func writeIncludeChain(t *testing.T, dir string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		writeTemplateFile(t, dir, fmt.Sprintf("chain%d.yaml", i), fmt.Sprintf("{{ include \"chain%d\" . }}", i+1))
	}
	writeTemplateFile(t, dir, fmt.Sprintf("chain%d.yaml", n), "leaf")
}

func TestIncludeFunc_NestingDepth(t *testing.T) {
	tests := []struct {
		name    string
		n       int // chain0..chainN: the last file is included at depth n+1
		wantErr bool
	}{
		{name: "shallow chain renders", n: 9},
		{name: "chain exactly at the limit renders", n: MaxIncludeDepth - 1},
		// The original has no guard, so this chain renders there; at the limit
		// plus one the guard refuses it.
		{name: "chain one past the limit fails", n: MaxIncludeDepth, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeIncludeChain(t, dir, tt.n)
			engine, err := NewTemplateEngine(dir)
			require.NoError(t, err)

			got, err := engine.RenderTemplateString(`{{ include "chain0" . }}`, &TemplateContext{})

			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrIncludeDepthExceeded)
			} else {
				require.NoError(t, err)
				assert.Equal(t, "leaf", got)
			}
			// Every include, failed or not, gives its slot back.
			assert.Zero(t, engine.includeDepth.Load())
		})
	}
}

// TestIncludeFunc_SelfRecursion renders a template that includes itself, in
// a child process. On the original code that overflows the Go stack, which is
// a fatal error that no recover catches and that would kill this whole test
// binary; the child process keeps the failure to one test.
func TestIncludeFunc_SelfRecursion(t *testing.T) {
	for _, mode := range []string{"helper", "file-cycle"} {
		t.Run(mode, func(t *testing.T) {
			exe, err := os.Executable()
			require.NoError(t, err)
			cmd := exec.Command(exe, "-test.run=^TestIncludeRecursionChild$", "-test.v")
			cmd.Env = append(os.Environ(), includeRecursionChildEnv+"="+mode)

			out, err := cmd.CombinedOutput()

			text := string(out)
			if len(text) > 3000 {
				text = text[:3000]
			}
			require.NoError(t, err, "child did not survive the recursion; output:\n%s", text)
			assert.Contains(t, string(out), "--- PASS: TestIncludeRecursionChild")
		})
	}
}

// TestIncludeRecursionChild is the child half of TestIncludeFunc_SelfRecursion.
func TestIncludeRecursionChild(t *testing.T) {
	mode := os.Getenv(includeRecursionChildEnv)
	if mode == "" {
		t.Skip("child of TestIncludeFunc_SelfRecursion")
	}

	dir := t.TempDir()
	switch mode {
	case "helper":
		writeTemplateFile(t, dir, "_helpers.tpl", `{{- define "loop" -}}{{ include "loop" . }}{{- end -}}`)
	case "file-cycle":
		writeTemplateFile(t, dir, "ping.yaml", `{{ include "pong" . }}`)
		writeTemplateFile(t, dir, "pong.yaml", `{{ include "ping" . }}`)
	default:
		t.Fatalf("unknown mode %q", mode)
	}
	engine, err := NewTemplateEngine(dir)
	require.NoError(t, err)

	entry := map[string]string{"helper": "loop", "file-cycle": "ping"}[mode]
	_, err = engine.RenderTemplateString(fmt.Sprintf(`{{ include %q . }}`, entry), &TemplateContext{})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrIncludeDepthExceeded)
	assert.Zero(t, engine.includeDepth.Load())
}

// templateTraversalFixture lays out a templates directory with a valid template
// one level above it. A name that climbs out by ".." reaches that file, so an
// unguarded loader renders it without error.
func templateTraversalFixture(t *testing.T) (engine *TemplateEngine, outsideAbs string) {
	t.Helper()
	base := t.TempDir()
	templatesDir := filepath.Join(base, "templates")
	const body = "kind: Template\ncompose:\n  services:\n    leak:\n      image: x\n"
	writeTemplateFile(t, base, "outside.yaml", body)
	writeTemplateFile(t, templatesDir, "inside.yaml", body)
	writeTemplateFile(t, templatesDir, "sub/inner.yaml", body)
	engine, err := NewTemplateEngine(templatesDir)
	require.NoError(t, err)
	return engine, filepath.Join(base, "outside")
}

func templateTestContext() *TemplateContext {
	return &TemplateContext{
		Chart:  ChartMeta{Name: "app"},
		Values: map[string]any{},
		Deps:   map[string]DependencyInfo{},
	}
}

// TestTemplateEngine_PathTraversal drives each template-name entry point into
// the containment check: a chart's templates list, a dependency name, a name in
// an {{ include }} call, and the loader itself.
func TestTemplateEngine_PathTraversal(t *testing.T) {
	engine, outsideAbs := templateTraversalFixture(t)
	ctx := templateTestContext()

	refused := map[string]func(name string) error{
		"RenderTemplate": func(name string) error {
			_, err := engine.RenderTemplate(name, ctx)
			return err
		},
		"loadTemplate": func(name string) error {
			_, err := engine.loadTemplate(name)
			return err
		},
		"include call": func(name string) error {
			_, err := engine.RenderTemplateString(fmt.Sprintf(`{{ include %q . }}`, name), ctx)
			return err
		},
		"chart templates entry": func(name string) error {
			_, err := engine.RenderChart(&Chart{Name: "app", Templates: []string{name}}, map[string]any{})
			return err
		},
		"chart dependency": func(name string) error {
			_, err := engine.RenderChart(&Chart{Name: "app", Dependencies: []ChartDependency{{Name: name}}}, map[string]any{})
			return err
		},
		"renderDependency": func(name string) error {
			_, err := engine.renderDependency(&Chart{Name: "app"}, &ChartDependency{Name: name}, ctx)
			return err
		},
	}

	for entry, call := range refused {
		for _, name := range []string{"../outside", "sub/../../outside"} {
			t.Run(entry+"/refuses "+name, func(t *testing.T) {
				err := call(name)

				require.Error(t, err)
				assert.ErrorIs(t, err, ErrPathTraversal)
			})
		}
	}

	t.Run("names inside the directory still render", func(t *testing.T) {
		for _, name := range []string{"inside", "sub/inner", "sub/../inside", "./inside"} {
			out, err := engine.RenderTemplate(name, ctx)
			require.NoError(t, err, name)
			assert.Contains(t, out.Targets, TargetCompose, name)

			_, err = engine.renderDependency(&Chart{Name: "app"}, &ChartDependency{Name: name}, ctx)
			require.NoError(t, err, name)
		}
	})

	t.Run("absolute name stays inside the directory", func(t *testing.T) {
		// filepath.Join places an absolute name under the templates dir, so it is
		// not found rather than read. Control: passes on the original too.
		_, err := engine.RenderTemplate(outsideAbs, ctx)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "template not found")
		assert.NotContains(t, err.Error(), strings.ToLower(ErrPathTraversal.Error()))
	})

	t.Run("a missing dependency template is still not an error", func(t *testing.T) {
		out, err := engine.renderDependency(&Chart{Name: "app"}, &ChartDependency{Name: "no-such-template"}, ctx)

		require.NoError(t, err)
		assert.Empty(t, out.Targets[TargetCompose])
	})
}
