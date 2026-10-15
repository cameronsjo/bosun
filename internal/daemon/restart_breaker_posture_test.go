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
			name: "root project name resolves implicit default posture",
			cfg: &Config{
				ReconcileConfig:     &reconcile.Config{RestartBreakerEnabled: true},
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
