package config

import (
	"encoding/json"
	"os"
	"strconv"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
	"github.com/cameronsjo/bosun/internal/reconcile"
)

// ApplyProjectOperationalConfig copies the operational fields that bosun.yaml
// can set into a reconcile.Config. The daemon and the one-shot CLI both call
// it, then call ApplyReconcileEnv, so the two paths resolve the same value
// from the same inputs (bosun#676). A nil projectCfg is a no-op.
func ApplyProjectOperationalConfig(projectCfg *Config, rcfg *reconcile.Config) {
	if projectCfg == nil || rcfg == nil {
		return
	}
	rcfg.CriticalContainers.SetFromFile(projectCfg.CriticalContainers())
	rcfg.DeploySyncPaths.SetFromFile(projectCfg.DeploySyncPaths())
	rcfg.DeploySyncExclude.SetFromFile(projectCfg.DeploySyncExclude())
	rcfg.ComposeIgnore.SetFromFile(projectCfg.ComposeIgnore())
	rcfg.HealthGateScope = projectCfg.HealthGateScope()
	rcfg.RemoveOrphans.SetFromFile(projectCfg.RemoveOrphans())
}

// durationEnv reads a duration variable (Go duration or bare seconds).
// A value that does not parse, or is below the allowed floor, is reported and
// ignored so the caller keeps its current value.
func durationEnv(name string, allowZero bool) (time.Duration, bool) {
	v := os.Getenv(name)
	if v == "" {
		return 0, false
	}
	d, ok := ParseDurationValue(v)
	if !ok {
		log.Warn().Str("env", name).Str("value", v).Msg("Skipping env var. Reason: invalid duration format")
		return 0, false
	}
	if d < 0 || (d == 0 && !allowZero) {
		reason := "duration must be positive"
		if allowZero {
			reason = "duration must not be negative"
		}
		log.Warn().Str("env", name).Str("value", v).Msg("Skipping env var. Reason: " + reason)
		return 0, false
	}
	return d, true
}

// jsonEnv decodes a JSON-valued variable into out. It reports whether the
// variable was set and valid; a malformed value is logged and ignored.
func jsonEnv(name string, out any) bool {
	v := os.Getenv(name)
	if v == "" {
		return false
	}
	if err := json.Unmarshal([]byte(v), out); err != nil {
		log.Warn().Err(err).Msg("Failed to parse " + name + ", ignoring")
		return false
	}
	return true
}

// ApplyReconcileEnv applies the environment overrides for the operational
// reconcile.Config fields that the daemon and the one-shot CLI share. Call it
// after ApplyProjectOperationalConfig: a variable wins over bosun.yaml.
//
// Fields that differ by design (directories, Source, Force, hook and target
// handling) are not touched here.
func ApplyReconcileEnv(rcfg *reconcile.Config) {
	if mode := os.Getenv("BOSUN_DEPLOY_MODE"); mode != "" {
		rcfg.DeployMode = mode
	}

	if d, ok := durationEnv("BOSUN_COMPOSE_UP_TIMEOUT", false); ok {
		rcfg.ComposeUpTimeout = d
	}
	if d, ok := durationEnv("BOSUN_BACKUP_TIMEOUT", false); ok {
		rcfg.BackupTimeout = d
	}
	if d, ok := durationEnv("BOSUN_HEALTH_CHECK_TIMEOUT", true); ok {
		rcfg.HealthCheckTimeout = d
	}
	if d, ok := durationEnv("BOSUN_HEALTH_CHECK_INTERVAL", false); ok {
		rcfg.HealthCheckInterval = d
	}
	if d, ok := durationEnv("BOSUN_HEALTH_GATE_TIMEOUT", true); ok {
		rcfg.HealthGateTimeout = d
	}
	if v := os.Getenv("BOSUN_HEALTH_GATE_SCOPE"); v != "" {
		if scope, err := reconcile.ResolveHealthGateScope(v); err == nil {
			rcfg.HealthGateScope = scope
		} else {
			log.Warn().Err(err).Str("value", v).Msg("Invalid BOSUN_HEALTH_GATE_SCOPE, ignoring")
		}
	}

	if v := os.Getenv("BOSUN_RESTART_BREAKER"); v != "" {
		rcfg.RestartBreakerEnabled = ParseBoolValue(v, true)
	}
	if v := os.Getenv("BOSUN_RESTART_THRESHOLD"); v != "" {
		n, err := strconv.Atoi(v)
		switch {
		case err != nil:
			log.Warn().Str("env", "BOSUN_RESTART_THRESHOLD").Str("value", v).Msg("Skipping env var. Reason: invalid integer")
		case n <= 0:
			log.Warn().Str("env", "BOSUN_RESTART_THRESHOLD").Str("value", v).Msg("Skipping env var. Reason: threshold must be positive")
		default:
			rcfg.RestartThreshold = n
		}
	}
	if d, ok := durationEnv("BOSUN_RESTART_WINDOW", false); ok {
		rcfg.RestartWindow = d
	}

	var paths []string
	if jsonEnv("BOSUN_DEPLOY_SYNC_PATHS", &paths) {
		rcfg.DeploySyncPaths.SetFromEnv(paths)
	}
	var exclude []string
	if jsonEnv("BOSUN_DEPLOY_SYNC_EXCLUDE", &exclude) {
		rcfg.DeploySyncExclude.SetFromEnv(exclude)
	}
	var containers []string
	if jsonEnv("BOSUN_CRITICAL_CONTAINERS", &containers) {
		rcfg.CriticalContainers.SetFromEnv(containers)
	}
	var rules []reconcile.DriftIgnoreRule
	if jsonEnv("BOSUN_DRIFT_IGNORE", &rules) {
		rcfg.DriftIgnore.SetFromEnv(rules)
	}

	// Content-hash sync defaults to on for both paths (the daemon's default);
	// reconcile.DefaultConfig leaves it off for embedded callers.
	rcfg.ContentHashSync = ParseBoolValue(os.Getenv("BOSUN_CONTENT_HASH_SYNC"), true)
	if v := os.Getenv("BOSUN_REMOVE_ORPHANS"); v != "" {
		rcfg.RemoveOrphans.SetFromEnv(ParseBoolValue(v, true))
	}

	// Safety overrides use a strict lowercase "true" match on purpose: these
	// gates guard against accidental activation of dangerous behavior, so
	// "yes", "1" and "TRUE" are rejected by design.
	rcfg.AllowEmptyDeclaredState = os.Getenv("BOSUN_ALLOW_EMPTY_DECLARED_STATE") == "true"
	rcfg.SkipDeployInvariant = os.Getenv("BOSUN_SKIP_DEPLOY_INVARIANT") == "true"
}
