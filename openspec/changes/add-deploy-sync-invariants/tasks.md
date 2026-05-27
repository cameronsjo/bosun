# Tasks

Implementation runs in three layers. Layer 1 ships the invariants and observability. Layer 2 is a diagnostic deploy that uses Layer 1 to confirm the root cause. Layer 3 lands the targeted root-cause fix (separate change proposal — out of scope here).

## Layer 1 — Defensive Invariants

### 1.1 Per-file write observability

- [x] `internal/fileutil/fileutil.go` — emit `Debug` log on file write in `CopyFileIfChanged` (src, dst, bytes)
- [x] `internal/fileutil/fileutil.go` — emit `Debug` log on skip in `CopyFileIfChanged` (reason=hash_match)
- [x] `internal/fileutil/fileutil.go` — emit `Debug` log on each call from `CopyDirIfChanged` per-file decision
- [x] Reuse `log.Component("fileutil")` — do not create a new component
- [x] `internal/fileutil/fileutil_test.go` — `TestCopyDirIfChanged_EmitsPerFileLogs`
- [x] `internal/fileutil/fileutil_test.go` — `TestCopyFileIfChanged_EmitsSkipReason`

### 1.2 Declared-state hard error

- [x] `internal/reconcile/drift.go` — split `ExtractDeclaredState` to return `ErrComposeDirMissing` vs `ErrNoDeclaredServices` (sentinel errors)
- [x] `internal/reconcile/reconcile.go` — fail the pipeline unconditionally when `ExtractDeclaredState` returns `ErrComposeDirMissing` (misconfigured staging path, not overridable)
- [x] `internal/reconcile/reconcile.go` — fail on `ErrNoDeclaredServices` unless `BOSUN_ALLOW_EMPTY_DECLARED_STATE=true`; when the override is set, log at `Warn` level (not `Info`) and continue
- [x] `internal/daemon/daemon.go` — parse `BOSUN_ALLOW_EMPTY_DECLARED_STATE` in `ConfigFromEnv()`
- [x] `internal/reconcile/drift_test.go` — `TestExtractDeclaredState_MissingComposeDir_ReturnsErrComposeDirMissing`
- [x] `internal/reconcile/drift_test.go` — `TestExtractDeclaredState_EmptyComposeDir_ReturnsErrNoDeclaredServices`
- [x] `internal/reconcile/reconcile_test.go` — `TestReconcile_DeclaredStateZero_Errors_Default`
- [x] `internal/reconcile/reconcile_test.go` — `TestReconcile_DeclaredStateZero_AllowedViaEnv` (implemented as `TestReconcile_DeclaredStateZero_AllowedViaConfig` in `state_integration_test.go`; env-var wiring tested in `internal/daemon/daemon_test.go`)

### 1.3 Post-deploy mtime invariant

- [x] `internal/reconcile/verify.go` (new file) — `verifyDeployTarget(src, dst, writtenRel, startTime) error` helper
- [x] `internal/reconcile/verify.go` — invariant: each `WrittenFiles` entry must exist at destination with `mtime >= startTime`
- [x] `internal/reconcile/verify.go` — invariant: empty `WrittenFiles` against non-empty source inspects the destination — passes on a content-matched no-op, errors (naming the missing file) when a source file is absent (refined per GH#330; `firstMissingSourceFile` helper)
- [x] `internal/reconcile/reconcile.go` — call `verifyDeployTarget` per-target inside `deployLocal` before `PrefixLatest`
- [x] `internal/daemon/daemon.go` — parse `BOSUN_SKIP_DEPLOY_INVARIANT` in `ConfigFromEnv()` (landed in Layer 1.2 commit alongside `BOSUN_ALLOW_EMPTY_DECLARED_STATE`)
- [x] `internal/reconcile/verify_test.go` — `TestVerifyDeployTarget_StaleDestination_Errors`
- [x] `internal/reconcile/verify_test.go` — `TestVerifyDeployTarget_ZeroWriteScenarios` (table-driven: no-op passes, missing-file errors; refined per GH#330)
- [x] `internal/reconcile/verify_test.go` — `TestDeployLocal_SkipDeployInvariant_BypassesCheck` (env wiring through `deployLocal`)
- [x] `internal/reconcile/verify_test.go` — `TestVerifyDeployTarget_HealthyDeploy_Passes` + `TestDeployLocal_HealthyDeploy_PassesInvariant`

### 1.4 Regression guard

- [x] `internal/reconcile/discovery_test.go` — `TestDiscoverDeployTargets_TopLevelRepoDirs_DiscoveredCorrectly` (locks the `Syncing .beads/.claude/unraid` shape so refactors can't silently regress)

### 1.5 Documentation

- [x] `AGENTS.md` — append `BOSUN_ALLOW_EMPTY_DECLARED_STATE` and `BOSUN_SKIP_DEPLOY_INVARIANT` to the env var table
- [x] `docs/troubleshooting.md` — add "Deploy reports success but files unchanged" section pointing operators at the new errors
- [x] `skills/onboard/resources/gitops.md` — document the invariant and the env-var escape hatches
- [x] `CHANGELOG.md` — release-please generates; verified all three commits use `feat(...)` conventional prefixes so the next release will list them as features under #214

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

- [x] `go test ./internal/reconcile/ ./internal/daemon/ ./internal/fileutil/` passes (full suite — 22.6s reconcile, 3.2s daemon, 0.3s fileutil)
- [x] `openspec validate add-deploy-sync-invariants --strict` passes
- [ ] Manual smoke: `BOSUN_LOG_LEVEL=debug bosun reconcile` against a known-good change shows per-file `wrote` lines (Layer 2, post-merge)
- [ ] Manual smoke: `BOSUN_LOG_LEVEL=debug bosun reconcile` against a no-op change shows per-file skip lines with `reason=hash_match` (Layer 2, post-merge)
- [ ] Manual smoke: `BOSUN_ALLOW_EMPTY_DECLARED_STATE=true bosun reconcile` on an empty-template repo shows warning, not error (Layer 2, post-merge)
- [x] CodeRabbit converged on the spec PR (4 rounds); `ready-to-build` label applied before implementation
