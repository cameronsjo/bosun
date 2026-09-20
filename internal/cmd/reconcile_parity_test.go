package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/daemon"
	"github.com/cameronsjo/bosun/internal/reconcile"
)

// The one-shot CLI and the daemon build reconcile.Config separately. This test
// is the only thing keeping them from drifting: every field of the struct is
// classified exactly once below, and an unclassified field fails.
//
// comparedFields decide what a reconciliation renders and where it deploys.
// Both paths must produce the same value for each, under both precedence
// cases (environment over project config, and project config when unset).
var comparedFields = []string{
	"RepoURL",
	"RepoBranch",
	"TargetHost",
	"SecretsFiles",
	"InfraSubDir",
	"TemplateIncludeDir",
	"Targets",
	"TargetsFromEnv",
	"StateFile",
	"PostSyncHooks",
	"HookSettleDelay",
	"DeployPaths",
	"DryRun",
}

// allowedDifferences may differ between the two paths. Each reason is either a
// design choice or a known gap; the gaps are tracked in bosun#676.
var allowedDifferences = map[string]string{
	"RepoDir":           "design: the one-shot points at its own directories; the daemon's are fixed by its image",
	"StagingDir":        "design: same as RepoDir",
	"BackupDir":         "design: same as RepoDir",
	"LogDir":            "design: same as RepoDir",
	"LocalAppdataPath":  "design: same as RepoDir",
	"RemoteAppdataPath": "design: same as RepoDir",
	"Source":            "design: the CLI records \"cli\"; the daemon records its trigger",
	"Force":             "design: FORCE/--force is per-invocation for the one-shot",
	"ConfigReloader":    "design: both assign the same function; compared by pointer below",

	"DeployMode":              "gap (bosun#676): daemon reads BOSUN_DEPLOY_MODE, CLI has --local only",
	"DeploySyncPaths":         "gap (bosun#676): daemon reads BOSUN_DEPLOY_SYNC_PATHS",
	"DeploySyncExclude":       "gap (bosun#676): daemon reads BOSUN_DEPLOY_SYNC_EXCLUDE",
	"CriticalContainers":      "gap (bosun#676): daemon reads BOSUN_CRITICAL_CONTAINERS",
	"DriftIgnore":             "gap (bosun#676): daemon reads BOSUN_DRIFT_IGNORE",
	"HealthGateTimeout":       "gap (bosun#676): daemon reads BOSUN_HEALTH_GATE_TIMEOUT",
	"HealthGateScope":         "gap (bosun#676): daemon reads BOSUN_HEALTH_GATE_SCOPE and the project config",
	"HealthCheckTimeout":      "gap (bosun#676): daemon reads BOSUN_HEALTH_CHECK_TIMEOUT",
	"HealthCheckInterval":     "gap (bosun#676): daemon reads BOSUN_HEALTH_CHECK_INTERVAL",
	"ComposeUpTimeout":        "gap (bosun#676): daemon reads BOSUN_COMPOSE_UP_TIMEOUT",
	"BackupTimeout":           "gap (bosun#676): daemon reads BOSUN_BACKUP_TIMEOUT",
	"RestartBreakerEnabled":   "gap (bosun#676): daemon reads BOSUN_RESTART_BREAKER",
	"RestartThreshold":        "gap (bosun#676): daemon reads BOSUN_RESTART_THRESHOLD",
	"RestartWindow":           "gap (bosun#676): daemon reads BOSUN_RESTART_WINDOW",
	"ContentHashSync":         "gap (bosun#676): daemon reads BOSUN_CONTENT_HASH_SYNC",
	"RemoveOrphans":           "gap (bosun#676): daemon reads BOSUN_REMOVE_ORPHANS and the project config",
	"AllowEmptyDeclaredState": "gap (bosun#676): daemon reads BOSUN_ALLOW_EMPTY_DECLARED_STATE",
	"SkipDeployInvariant":     "gap (bosun#676): daemon reads BOSUN_SKIP_DEPLOY_INVARIANT",
	"OnFailure":               "gap (bosun#676): daemon copies the project alert gates",
	"OnSuccess":               "gap (bosun#676): daemon copies the project alert gates",
	"OnRecovery":              "gap (bosun#676): daemon copies the project alert gates",
}

// unsetByBoth are fields neither path assigns, so comparing them proves
// nothing. They are listed so a field that later becomes live cannot hide in
// the compared set: the test asserts both paths still leave them at the
// package default.
var unsetByBoth = []string{
	"SecretsScope",
	"TargetName",
	"LockFile",
	"BackupsToKeep",
	"ProjectName",
	"ForceRedeployUnchanged",
}

const parityProjectConfig = `
template_include_dir: from-file/templates
deploy_paths:
  - "from-file/**"
hook_settle_delay: 7s
post_sync_hooks:
  - paths: ["from-file/**"]
    action: restart
    container: from-file
targets:
  - name: unraid
    project_name: homelab
`

// parityEnv is every variable either builder reads that also has a project
// config counterpart, set to values that differ from the file's.
func parityEnv(t *testing.T, override bool) {
	t.Helper()
	set := func(k, v string) {
		if !override {
			v = ""
		}
		t.Setenv(k, v)
	}
	// Always set: these have no file counterpart in this fixture.
	t.Setenv("BOSUN_REPO_URL", "git@github.com:cameronsjo/homelab.git")
	t.Setenv("BOSUN_REPO_BRANCH", "main")
	t.Setenv("BOSUN_INFRA_DIR", "unraid")
	t.Setenv("BOSUN_SECRETS_FILE", " secrets.sops.yaml , ,other.sops.yaml ")
	t.Setenv("DEPLOY_TARGET", "root@192.168.1.8")
	t.Setenv("DRY_RUN", "yes")
	t.Setenv("BOSUN_STATE_DIR", t.TempDir())
	// Environment-vs-file precedence pairs.
	set("BOSUN_TEMPLATE_INCLUDE_DIR", "from-env/templates")
	set("BOSUN_DEPLOY_PATHS", `["from-env/**"]`)
	set("BOSUN_HOOK_SETTLE_DELAY", "11s")
	set("BOSUN_POST_SYNC_HOOKS", `[{"paths":["from-env/**"],"action":"restart","container":"from-env"}]`)
	set("BOSUN_TARGETS", `[{"name":"unraid","project_name":"homelab-from-env"}]`)
}

func buildBothConfigs(t *testing.T, override bool) (cli, daemonCfg *reconcile.Config) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bosun.yaml"), []byte(parityProjectConfig), 0o600))
	// Both builders read the project config from the working directory, so the
	// test cannot run in parallel.
	t.Chdir(dir)
	parityEnv(t, override)

	cli, err := buildReconcileConfigFromEnv()
	require.NoError(t, err)
	daemonCfg = daemon.ConfigFromEnv().ReconcileConfig
	require.NotNil(t, daemonCfg)
	return cli, daemonCfg
}

func TestReconcileConfigParityWithDaemon(t *testing.T) {
	for _, tc := range []struct {
		name     string
		override bool
	}{
		{"environment overrides project config", true},
		{"project config applies when the environment is unset", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cli, daemonCfg := buildBothConfigs(t, tc.override)
			cliV := reflect.ValueOf(*cli)
			daemonV := reflect.ValueOf(*daemonCfg)
			defV := reflect.ValueOf(*reconcile.DefaultConfig())

			classified := make(map[string]bool, cliV.NumField())
			for _, name := range comparedFields {
				classified[name] = true
				require.Equalf(t, cliV.FieldByName(name).Interface(), daemonV.FieldByName(name).Interface(),
					"field %s: the CLI and the daemon must agree", name)
			}
			for name := range allowedDifferences {
				classified[name] = true
			}
			for _, name := range unsetByBoth {
				classified[name] = true
				require.Equalf(t, defV.FieldByName(name).Interface(), cliV.FieldByName(name).Interface(),
					"field %s is listed as unset by both, but the CLI set it: classify it instead", name)
				require.Equalf(t, defV.FieldByName(name).Interface(), daemonV.FieldByName(name).Interface(),
					"field %s is listed as unset by both, but the daemon set it: classify it instead", name)
			}

			// ConfigReloader is a func: DeepEqual on two non-nil funcs is always
			// false and comparing them with == panics, so compare identity.
			require.Equal(t,
				reflect.ValueOf(cli.ConfigReloader).Pointer(),
				reflect.ValueOf(daemonCfg.ConfigReloader).Pointer(),
				"both paths must wire the same config reloader")

			for i := 0; i < cliV.NumField(); i++ {
				name := cliV.Type().Field(i).Name
				require.Truef(t, classified[name],
					"reconcile.Config field %s is not classified: add it to comparedFields, allowedDifferences or unsetByBoth in this file", name)
			}
		})
	}
}

// The precedence assertions are separate from the walk above: two paths that
// both pick the wrong source are equal to each other and would pass it.
func TestReconcileConfigParityPrecedence(t *testing.T) {
	t.Run("environment wins", func(t *testing.T) {
		cli, daemonCfg := buildBothConfigs(t, true)
		for name, cfg := range map[string]*reconcile.Config{"cli": cli, "daemon": daemonCfg} {
			require.Equalf(t, "from-env/templates", cfg.TemplateIncludeDir, "%s: template include dir", name)
			require.Equalf(t, []string{"from-env/**"}, cfg.DeployPaths.Value, "%s: deploy paths", name)
			require.Lenf(t, cfg.Targets, 1, "%s: targets", name)
			require.Equalf(t, "homelab-from-env", cfg.Targets[0].ProjectName, "%s: target project name", name)
		}
	})

	t.Run("project config wins when the environment is unset", func(t *testing.T) {
		cli, daemonCfg := buildBothConfigs(t, false)
		for name, cfg := range map[string]*reconcile.Config{"cli": cli, "daemon": daemonCfg} {
			require.Equalf(t, "from-file/templates", cfg.TemplateIncludeDir, "%s: template include dir", name)
			require.Equalf(t, []string{"from-file/**"}, cfg.DeployPaths.Value, "%s: deploy paths", name)
			require.Lenf(t, cfg.Targets, 1, "%s: targets", name)
			require.Equalf(t, "homelab", cfg.Targets[0].ProjectName, "%s: target project name", name)
		}
	})
}

// The two parsers the CLI used to get wrong on its own.
func TestReconcileConfigSharedParsers(t *testing.T) {
	cli, daemonCfg := buildBothConfigs(t, true)

	// Empty entries dropped, and the singular form split like the plural one.
	require.Equal(t, []string{"secrets.sops.yaml", "other.sops.yaml"}, cli.SecretsFiles)
	require.Equal(t, cli.SecretsFiles, daemonCfg.SecretsFiles)

	// DRY_RUN=yes is a dry run on both paths; the CLI used to require "true"
	// exactly, which meant it deployed for real in that environment.
	require.True(t, cli.DryRun, "DRY_RUN=yes must be a dry run for the CLI")
	require.Equal(t, daemonCfg.DryRun, cli.DryRun)
}
