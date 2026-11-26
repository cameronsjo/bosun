package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestGenerateServiceTemplate_ExactOutput pins the full generated manifest for
// every template. The older tests use Contains, which passes when a template
// carries a stray fmt verb error such as `%!(EXTRA string=name)` after the
// YAML. Only webapp has two name placeholders; api, worker and static have one.
func TestGenerateServiceTemplate_ExactOutput(t *testing.T) {
	tests := []struct {
		template string
		name     string
		want     string
	}{
		{
			template: "webapp",
			name:     "myapp",
			want:     "name: myapp\nprovisions:\n  - webapp\nconfig:\n  port: 8080\n  domain: myapp.example.com\n",
		},
		{
			template: "api",
			name:     "myapi",
			want:     "name: myapi\nprovisions:\n  - api\nconfig:\n  port: 8080\n  health_path: /health\n",
		},
		{
			template: "worker",
			name:     "myworker",
			want:     "name: myworker\nprovisions:\n  - worker\nconfig:\n  replicas: 1\n",
		},
		{
			template: "static",
			name:     "mystatic",
			want:     "name: mystatic\nprovisions:\n  - static\nconfig:\n  root: /var/www/html\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.template, func(t *testing.T) {
			got := generateServiceTemplate(tt.template, tt.name)

			assert.Equal(t, tt.want, got)
			assert.NotContains(t, got, "%!")

			var manifest struct {
				Name       string         `yaml:"name"`
				Provisions []string       `yaml:"provisions"`
				Config     map[string]any `yaml:"config"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(got), &manifest))
			assert.Equal(t, tt.name, manifest.Name)
			assert.Equal(t, []string{tt.template}, manifest.Provisions)
			assert.NotEmpty(t, manifest.Config)
		})
	}
}
