## 1. Foundation — Target type and config parsing

- [ ] 1.1 Define `Target` struct in `internal/reconcile/reconcile.go` with all per-target fields
- [ ] 1.2 Add `Targets []Target` to `reconcile.Config`
- [ ] 1.3 Add `targets:` section to `bosun.yaml` schema in `internal/config/config.go`
- [ ] 1.4 Implement `extractTargets()` to parse YAML targets into `[]Target`
- [ ] 1.5 Implement implicit default target: when `Targets` is empty, synthesize one from flat config fields
- [ ] 1.6 Add `BOSUN_TARGETS` env var parsing in `internal/daemon/daemon.go:ConfigFromEnv()`
- [ ] 1.7 Add deprecation warning when both `targets:` and flat target fields are present
- [ ] 1.8 Write tests for config parsing: multi-target YAML, single-target backwards compat, env var override

## 2. Per-target isolation — staging, state, locking

- [ ] 2.1 Derive per-target staging directory: `<StagingDir>/<target.Name>/`
- [ ] 2.2 Derive per-target state file: `<StateDir>/deploy-state-<target.Name>.json` (default target uses `deploy-state.json`)
- [ ] 2.3 Derive per-target lock file: `<LockDir>/reconcile-<target.Name>.lock`
- [ ] 2.4 Update `New()` to accept a `Target` and configure paths accordingly
- [ ] 2.5 Write tests for path derivation: default target, named target, custom overrides

## 3. Daemon multi-target loop

- [ ] 3.1 Refactor daemon to hold `[]Target` instead of one `Reconciler`
- [ ] 3.2 On each reconciliation cycle, iterate targets and create/run a `Reconciler` per target
- [ ] 3.3 Failure on target N logs error + sends alert, then target N+1 proceeds
- [ ] 3.4 Add target name to logger context for all reconciliation log messages
- [ ] 3.5 Preserve single-flight reconciliation semantics: process-wide gate serializes all entry points, dirty-flag coalesces concurrent triggers into follow-up cycle
- [ ] 3.6 Write integration test: two targets, one fails, second still runs
- [ ] 3.7 Write test: concurrent trigger during active cycle sets dirty flag and coalesces

## 4. Secrets scoping

- [ ] 4.1 Implement `mergeTargetSecrets()`: merge `targets.<name>.*` over top-level keys
- [ ] 4.2 Pass scoped secrets to template rendering per target
- [ ] 4.3 Log overridden keys at debug level
- [ ] 4.4 Write tests: shared-only, target-scoped override, no target scope (passthrough)

## 5. Per-target alerting

- [ ] 5.1 Add `TargetName string` field to alert method signatures
- [ ] 5.2 Include target name in deploy success/failure/recovery alert messages
- [ ] 5.3 Include target name in drift alert messages
- [ ] 5.4 Update alert tests to verify target context is present

## 6. CLI multi-target support

- [ ] 6.1 Add `--target` flag to `bosun reconcile` to reconcile a single target
- [ ] 6.2 Add `--target` flag to `bosun drift` to check drift for a specific target
- [ ] 6.3 Update `bosun status` to show per-target state (last deploy, drift, circuit breaker)
- [ ] 6.4 Add `--target` flag to `bosun trigger` for daemon-mode single-target trigger
- [ ] 6.5 Write tests for CLI target filtering

## 7. Config reload for multi-target

- [ ] 7.1 Update `ConfigReloaderFunc` to return per-target reloadable config
- [ ] 7.2 Ensure env var overrides (`*FromEnv` flags) work per-target
- [ ] 7.3 Write tests for config reload with multiple targets

## 8. Documentation and skill updates

- [ ] 8.1 Update `docs/commands.md` with `--target` flag documentation
- [ ] 8.2 Update `skills/onboard/resources/gitops.md` with multi-target workflow
- [ ] 8.3 Update `skills/onboard/resources/configuration.md` with `targets:` schema
- [ ] 8.4 Add multi-target example to `docs/` or README

## 9. Target validation and safety (folded from Cluster C bug hunt)
- [ ] 9.1 `ResolveTargets` validates target-derived paths (state_file, lock_file, staging_dir, appdata) for traversal/root-confinement — same checks for YAML and `BOSUN_TARGETS` (GHSA-57r2)
- [ ] 9.2 Reserved-name `default` check is case-insensitive, matching dedup (#228)
- [ ] 9.3 Reject colliding state_file / lock_file / staging_dir across targets (#260)
- [ ] 9.4 Reject/fail-fast on colliding Docker namespace (host+project_name) or deploy path across targets (#287)
- [ ] 9.5 YAML and `BOSUN_TARGETS` expose identical per-target field sets; `BOSUN_TARGETS=[]` == absent targets (#272, #273)
- [ ] 9.6 `ConfigForTarget` deep-copies ALL slice/map fields incl. SecretsFiles, DeployPaths, DriftIgnore (#270)
- [ ] 9.7 `applyTargetOverrides` deep-copies caller-owned slices on hot-reload (#271)
- [ ] 9.8 Explicit, documented empty-field inheritance; empty override never silently inherits a foreign host/path (GHSA-jxw8)
- [ ] 9.9 Tests: path-traversal rejection, collision rejection, slice independence after derive + hot-reload, field parity
