package daemon

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// #345: BOSUN_LISTEN_ADDR narrows the HTTP bind; the default (unset) must stay
// empty so Server.Start's listenAddr() composes an all-interfaces bind.
func TestConfigFromEnv_ListenAddr(t *testing.T) {
	tests := []struct {
		name string
		env  string // "" = leave unset
		want string
	}{
		{name: "unset leaves ListenAddr empty (all interfaces)", env: "", want: ""},
		{name: "IPv4 host narrows the bind", env: "127.0.0.1", want: "127.0.0.1"},
		{name: "IPv6 host accepted", env: "::1", want: "::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv("BOSUN_LISTEN_ADDR", tt.env)
			}

			cfg := ConfigFromEnv()

			assert.Equal(t, tt.want, cfg.ListenAddr,
				"default bind MUST stay all-interfaces; container callers reach bosun over the docker bridge")
		})
	}
}

// #345: fail-closed is the default; the opt-out is a strict lowercase "true"
// match, mirroring BOSUN_ALLOW_EMPTY_DECLARED_STATE / BOSUN_SKIP_DEPLOY_INVARIANT.
func TestConfigFromEnv_AllowUnauthenticatedWebhook(t *testing.T) {
	tests := []struct {
		name string
		env  string // "" = leave unset
		want bool
	}{
		{name: "unset defaults to fail-closed", env: "", want: false},
		{name: "strict lowercase true opts out", env: "true", want: true},
		{name: "TRUE rejected by strict match", env: "TRUE", want: false},
		{name: "True rejected by strict match", env: "True", want: false},
		{name: "1 rejected by strict match", env: "1", want: false},
		{name: "yes rejected by strict match", env: "yes", want: false},
		{name: "on rejected by strict match", env: "on", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv("BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK", tt.env)
			}

			cfg := ConfigFromEnv()

			assert.Equal(t, tt.want, cfg.AllowUnauthenticatedWebhook,
				"value %q must satisfy only the strict lowercase 'true' match", tt.env)
		})
	}
}

// #390: TargetsFromEnv records that BOSUN_TARGETS supplied the target list, so
// config_reload.go's applyTargetOverrides/setProjectName never let a hot-reload
// of the repo's bosun.yaml override an env-provided value.
func TestConfigFromEnv_TargetsFromEnvField(t *testing.T) {
	tests := []struct {
		name        string
		env         string // "" = leave unset
		wantFromEnv bool
		wantTargets int
	}{
		{name: "unset leaves TargetsFromEnv false", env: "", wantFromEnv: false, wantTargets: 0},
		{name: "valid targets record env provenance", env: `[{"name":"unraid","target_host":"user@unraid"}]`, wantFromEnv: true, wantTargets: 1},
		{name: "explicit empty array is still an intentional env override", env: `[]`, wantFromEnv: true, wantTargets: 0},
		{name: "malformed JSON is not recorded as an intentional override", env: `not-valid-json`, wantFromEnv: false, wantTargets: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv("BOSUN_TARGETS", tt.env)
			}

			cfg := ConfigFromEnv()

			assert.Equal(t, tt.wantFromEnv, cfg.ReconcileConfig.TargetsFromEnv)
			assert.Len(t, cfg.ReconcileConfig.Targets, tt.wantTargets)
		})
	}
}
