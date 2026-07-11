package daemon

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Test Plan for daemon.go (resilience-slate additions)
//
// ConfigFromEnv (Classification: configuration)
//   [x] Happy: BOSUN_LISTEN_ADDR sets Config.ListenAddr
//   [x] Boundary: BOSUN_LISTEN_ADDR unset leaves ListenAddr empty (binds all interfaces)
//   [x] Happy: BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true sets the opt-out
//   [x] Boundary: BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK unset defaults false (fail-closed)
//   [x] Unhappy/boundary: BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK uses strict lowercase "true" match
//   [x] Happy: BOSUN_TARGETS sets ReconcileConfig.TargetsFromEnv, gating hot-reload overrides
//   [x] Boundary: BOSUN_TARGETS="[]" (explicit empty) still sets TargetsFromEnv true
//   [x] Unhappy: malformed BOSUN_TARGETS leaves TargetsFromEnv false (parse failed, no override recorded)

// #345: BOSUN_LISTEN_ADDR narrows the HTTP bind; the default (unset) must stay
// empty so Server.Start's listenAddr() composes an all-interfaces bind.
func TestConfigFromEnv_ListenAddr(t *testing.T) {
	t.Run("unset leaves ListenAddr empty (all interfaces)", func(t *testing.T) {
		cfg := ConfigFromEnv()
		assert.Empty(t, cfg.ListenAddr, "default bind MUST stay all-interfaces; container callers reach bosun over the docker bridge")
	})

	t.Run("BOSUN_LISTEN_ADDR narrows the bind", func(t *testing.T) {
		t.Setenv("BOSUN_LISTEN_ADDR", "127.0.0.1")

		cfg := ConfigFromEnv()

		assert.Equal(t, "127.0.0.1", cfg.ListenAddr)
	})

	t.Run("BOSUN_LISTEN_ADDR accepts an IPv6 host", func(t *testing.T) {
		t.Setenv("BOSUN_LISTEN_ADDR", "::1")

		cfg := ConfigFromEnv()

		assert.Equal(t, "::1", cfg.ListenAddr)
	})
}

// #345: fail-closed is the default; the opt-out is a strict lowercase "true"
// match, mirroring BOSUN_ALLOW_EMPTY_DECLARED_STATE / BOSUN_SKIP_DEPLOY_INVARIANT.
func TestConfigFromEnv_AllowUnauthenticatedWebhook(t *testing.T) {
	t.Run("unset defaults to fail-closed (false)", func(t *testing.T) {
		cfg := ConfigFromEnv()
		assert.False(t, cfg.AllowUnauthenticatedWebhook)
	})

	t.Run("BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true opts out of fail-closed auth", func(t *testing.T) {
		t.Setenv("BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK", "true")

		cfg := ConfigFromEnv()

		assert.True(t, cfg.AllowUnauthenticatedWebhook)
	})

	t.Run("non-lowercase-true values do not opt out", func(t *testing.T) {
		for _, v := range []string{"TRUE", "True", "1", "yes", "on"} {
			v := v
			t.Run(v, func(t *testing.T) {
				t.Setenv("BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK", v)

				cfg := ConfigFromEnv()

				assert.False(t, cfg.AllowUnauthenticatedWebhook, "value %q must not satisfy the strict 'true' match", v)
			})
		}
	})
}

// #390: TargetsFromEnv records that BOSUN_TARGETS supplied the target list, so
// config_reload.go's applyTargetOverrides/setProjectName never let a hot-reload
// of the repo's bosun.yaml override an env-provided value.
func TestConfigFromEnv_TargetsFromEnvField(t *testing.T) {
	t.Run("unset BOSUN_TARGETS leaves TargetsFromEnv false", func(t *testing.T) {
		cfg := ConfigFromEnv()
		assert.False(t, cfg.ReconcileConfig.TargetsFromEnv)
	})

	t.Run("valid BOSUN_TARGETS sets TargetsFromEnv true", func(t *testing.T) {
		t.Setenv("BOSUN_TARGETS", `[{"name":"unraid","target_host":"user@unraid"}]`)

		cfg := ConfigFromEnv()

		assert.True(t, cfg.ReconcileConfig.TargetsFromEnv)
	})

	t.Run("explicit empty array still records env provenance", func(t *testing.T) {
		t.Setenv("BOSUN_TARGETS", `[]`)

		cfg := ConfigFromEnv()

		assert.True(t, cfg.ReconcileConfig.TargetsFromEnv, "an explicit []  is still an intentional env override")
		assert.Empty(t, cfg.ReconcileConfig.Targets)
	})

	t.Run("malformed BOSUN_TARGETS JSON leaves TargetsFromEnv false", func(t *testing.T) {
		t.Setenv("BOSUN_TARGETS", `not-valid-json`)

		cfg := ConfigFromEnv()

		assert.False(t, cfg.ReconcileConfig.TargetsFromEnv, "a parse failure must not be recorded as an intentional env override")
	})
}
