package daemon

import (
	"bytes"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/cameronsjo/bosun/internal/reconcile"
)

func TestWarnRestartBreakerScopePosture(t *testing.T) {
	tests := []struct {
		name               string
		cfg                *Config
		targets            []reconcile.Target
		wantWarning        bool
		wantProjectNameFix bool
		wantText           []string
	}{
		{
			name: "unresolved implicit default warns once at startup",
			cfg: &Config{
				ReconcileConfig: &reconcile.Config{RestartBreakerEnabled: true},
			},
			targets:            []reconcile.Target{{Name: reconcile.DefaultTargetName}},
			wantWarning:        true,
			wantProjectNameFix: true,
		},
		{
			// bosun#719: the root-level name is posture input only; the breaker
			// scope stays empty until a reconcile reloads it from the repo.
			name: "root project name does not silence the unresolved implicit default",
			cfg: &Config{
				ReconcileConfig:     &reconcile.Config{RestartBreakerEnabled: true},
				ProjectNameFromFile: "homelab",
			},
			targets:     []reconcile.Target{{Name: reconcile.DefaultTargetName}},
			wantWarning: true,
			wantText: []string{
				"implicit default target",
				"root-level project_name",
				"will stop nothing until a reconcile loads project_name",
				"set project_name on a target named default",
			},
		},
		{
			name: "env targets ignore the root project name",
			cfg: &Config{
				ReconcileConfig:     &reconcile.Config{RestartBreakerEnabled: true, TargetsFromEnv: true},
				ProjectNameFromFile: "homelab",
			},
			targets:            []reconcile.Target{{Name: reconcile.DefaultTargetName}},
			wantWarning:        true,
			wantProjectNameFix: true,
		},
		{
			name: "target project name resolves implicit default posture",
			cfg: &Config{
				ReconcileConfig:     &reconcile.Config{RestartBreakerEnabled: true, ProjectName: "homelab"},
				ProjectNameFromFile: "homelab",
			},
			targets: []reconcile.Target{{Name: reconcile.DefaultTargetName}},
		},
		{
			name: "named blank project keeps compose fallback posture",
			cfg: &Config{
				ReconcileConfig: &reconcile.Config{RestartBreakerEnabled: true},
			},
			targets: []reconcile.Target{{Name: "nas"}},
		},
		{
			name: "disabled breaker has no posture warning",
			cfg: &Config{
				ReconcileConfig: &reconcile.Config{RestartBreakerEnabled: false},
			},
			targets: []reconcile.Target{{Name: reconcile.DefaultTargetName}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			d := &Daemon{config: tt.cfg}
			d.warnRestartBreakerScopePosture(zerolog.New(&output), tt.targets)

			if tt.wantWarning && len(tt.wantText) > 0 {
				for _, want := range tt.wantText {
					assert.Contains(t, output.String(), want)
				}
				return
			}
			if tt.wantWarning {
				assert.Contains(t, output.String(), "implicit default target")
				assert.Contains(t, output.String(), "no project_name is configured")
				if tt.wantProjectNameFix {
					assert.Contains(t, output.String(), "Set project_name")
				}
				return
			}
			assert.Empty(t, output.String())
		})
	}
}
