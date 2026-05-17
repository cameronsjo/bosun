# Tasks

Implementation runs in three layers. Layer 1 ships the invariants and observability. Layer 2 is a diagnostic deploy that uses Layer 1 to confirm the root cause. Layer 3 lands the targeted root-cause fix (separate change proposal — out of scope here).

## Layer 1 — Defensive Invariants

### 1.1 Per-file write observability

- [ ] `internal/fileutil/fileutil.go` — emit `Debug` log on file write in `CopyFileIfChanged` (src, dst, bytes)
- [ ] `internal/fileutil/fileutil.go` — emit `Debug` log on skip in `CopyFileIfChanged` (reason=hash_match)
- [ ] `internal/fileutil/fileutil.go` — emit `Debug` log on each call from `CopyDirIfChanged` per-file decision
- [ ] Reuse `log.Component("fileutil")` — do not create a new component
- [ ] `internal/fileutil/fileutil_test.go` — `TestCopyDirIfChanged_EmitsPerFileLogs`
- [ ] `internal/fileutil/fileutil_test.go` — `TestCopyFileIfChanged_EmitsSkipReason`

### 1.2 Declared-state hard error

- [ ] `internal/reconcile/drift.go` — split `ExtractDeclaredState` to return `ErrComposeDirMissing` vs `ErrNoDeclaredServices` (sentinel errors)
- [ ] `internal/reconcile/reconcile.go` — fail the pipeline when `ExtractDeclaredState` returns either sentinel, unless `BOSUN_ALLOW_EMPTY_DECLARED_STATE=true`
- [ ] `internal/reconcile/reconcile.go` — log the empty-dir case as a clearly-formatted warning (not `info`) when override is set
- [ ] `internal/daemon/daemon.go` — parse `BOSUN_ALLOW_EMPTY_DECLARED_STATE` in `ConfigFromEnv()`
- [ ] `internal/reconcile/drift_test.go` — `TestExtractDeclaredState_MissingComposeDir_ReturnsErrComposeDirMissing`
- [ ] `internal/reconcile/drift_test.go` — `TestExtractDeclaredState_EmptyComposeDir_ReturnsErrNoDeclaredServices`
- [ ] `internal/reconcile/reconcile_test.go` — `TestReconcile_DeclaredStateZero_Errors_Default`
- [ ] `internal/reconcile/reconcile_test.go` — `TestReconcile_DeclaredStateZero_AllowedViaEnv`

### 1.3 Post-deploy mtime invariant

- [ ] `internal/reconcile/deploy.go` — new `verifyDeploy(targets, startTime) error` helper
- [ ] `internal/reconcile/deploy.go` — invariant: each `WrittenFiles` entry must exist at destination with `mtime >= startTime`
- [ ] `internal/reconcile/deploy.go` — invariant: empty `WrittenFiles` against non-empty source is an error
- [ ] `internal/reconcile/reconcile.go` — call `verifyDeploy` between deploy sync and compose-up
- [ ] `internal/daemon/daemon.go` — parse `BOSUN_SKIP_DEPLOY_INVARIANT` in `ConfigFromEnv()`
- [ ] `internal/reconcile/deploy_test.go` — `TestVerifyDeploy_StaleDestination_Errors`
- [ ] `internal/reconcile/deploy_test.go` — `TestVerifyDeploy_EmptyWrittenFiles_Against_NonEmptySource_Errors`
- [ ] `internal/reconcile/deploy_test.go` — `TestVerifyDeploy_SkippedViaEnv_NoError`
- [ ] `internal/reconcile/deploy_test.go` — `TestVerifyDeploy_HealthyDeploy_Passes`

### 1.4 Regression guard

- [ ] `internal/reconcile/deploy_test.go` — `TestDiscoverDeployTargets_TopLevelRepoDirs_DiscoveredCorrectly` (locks the `Syncing .beads/.claude/unraid` shape so refactors can't silently regress)

### 1.5 Documentation

- [ ] `AGENTS.md` — append `BOSUN_ALLOW_EMPTY_DECLARED_STATE` and `BOSUN_SKIP_DEPLOY_INVARIANT` to the env var table
- [ ] `docs/troubleshooting.md` — add "Deploy reports success but files unchanged" section pointing operators at the new errors
- [ ] `skills/onboard/resources/gitops.md` — document the invariant and the env-var escape hatches
- [ ] `CHANGELOG.md` — release-please generates; verify the conventional commit message will produce the right entry

## Layer 2 — Diagnostic Run (post-merge)

- [ ] Tag a release with Layer 1 changes; deploy to homelab
- [ ] Push the same FreshRSS-shape `.tmpl` change that originally reproduced the bug
- [ ] Capture `BOSUN_LOG_LEVEL=debug` logs from the daemon
- [ ] Confirm which of the three Layer-3 hypothesis branches the bug falls into:
  - (a) Source staging dir empty for the target
  - (b) Source has files, hashes match destination
  - (c) Source path mismatch between render output and `discoverDeployTargets` walk
- [ ] Open separate spec proposal for Layer 3 once root cause is identified

## Verification

- [ ] `make test` passes
- [ ] `openspec validate add-deploy-sync-invariants --strict` passes
- [ ] Manual smoke: `BOSUN_LOG_LEVEL=debug bosun reconcile` against a known-good change shows per-file `wrote` lines
- [ ] Manual smoke: `BOSUN_LOG_LEVEL=debug bosun reconcile` against a no-op change shows per-file `skipped (unchanged)` lines
- [ ] Manual smoke: `BOSUN_ALLOW_EMPTY_DECLARED_STATE=true bosun reconcile` on an empty-template repo shows warning, not error
- [ ] CodeRabbit converges on the spec PR; `ready-to-build` label applied before any implementation begins
