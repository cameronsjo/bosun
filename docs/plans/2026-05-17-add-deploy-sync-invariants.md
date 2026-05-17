# Plan: Bosun #214 — Local Deploy Sync Silent No-Op

## Context

**Bug:** In local-deploy mode (`local_appdata_path` set, no `target_host`), Bosun's reconcile pipeline reports `success: true` while the rendered templates never overwrite the files at `/mnt/user/appdata/<path>`. Compose-up exits 0 because the on-disk file hasn't changed; drift-check warns but nobody sees it; only 1 of 7 expected post-sync hooks fires.

**Reproduction (2026-05-16, commit `ee3b8c2` on `cameronsjo/homelab`):** Pushed 23 new `freshrss` references in `unraid/compose/capture.yml.tmpl`. Bosun logged `Templates rendered successfully`, `declared_services: 0`, `Syncing .beads/.claude/unraid`, `Partial deploy: 10/11 files succeeded`. Host-side: `grep -c freshrss /mnt/user/appdata/compose/capture.yml` returned `0`. File mtime was 13 days stale. Manual `bosun render` inside the container produced 18 freshrss matches against the same template — so render works in isolation but the pipeline write-back is broken.

**Why this is hard to spot:** Every diagnostic surface says success. Webhook → 200. Daemon → `success: true`. Compose-up → exit 0. The only catch is host-side mtime verification, which nobody automates.

**Intended outcome:** Make this failure mode impossible to ship silently — defensive instrumentation + invariants that fail the reconcile when render output doesn't actually land on disk. Then root-cause and fix the underlying sync gap.

## Findings From Phase 1 Exploration

Code paths verified (Explore agent, 2026-05-17):

- `internal/reconcile/deploy.go:115-249` — `deployLocal`. Walks `stagingSubDir = StagingDir/InfraSubDir` via `discoverDeployTargets`. Correctly reads staging, not `/app/repo`.
- `internal/reconcile/deploy.go:42` — `discoverDeployTargets`. Calls `os.ReadDir(stagingSubDir)`; returns top-level entries as deploy targets. **This is why logs show `Syncing .beads/.claude/unraid`** — `InfraSubDir` resolves to staging root for this repo, so top-level repo dirs are the targets.
- `internal/fileutil/fileutil.go:247-279` — `CopyDirIfChanged(src, dst) ([]string, error)`. SHA-256 compares each file; returns `WrittenFiles` as source-relative paths. Correct in isolation.
- `internal/reconcile/drift.go:79-119` — `ExtractDeclaredState`. Globs `filepath.Join(stagingDir, "compose")` for `*.yml`. **Returns `nil, nil` silently when no matches** (line 88). Caller at `reconcile.go:511-519` logs `info` and continues.
- `internal/reconcile/reconcile.go:294-632` — pipeline: pull → decrypt → render (498) → extract declared state (511) → backup (525) → deploy sync (548) → cleanup staging (561) → compose-up (inside `deployLocal`, 1252) → hooks (601).

**Three reinforcing signals from logs:**

1. `declared_services: 0` — drift can't find compose files at `stagingDir/compose`. Either render output didn't land there, or path glob expects a directory the render step doesn't produce.
2. `Syncing .beads/.claude/unraid` — staging contains raw repo top-level dirs, not just rendered output. Plausibly render copies the workspace and writes rendered files into a sub-path that `ExtractDeclaredState` and `discoverDeployTargets` disagree on.
3. `Partial deploy: 10/11 files succeeded` with empty `WrittenFiles` per target — each target's `CopyDirIfChanged` ran but wrote nothing, either because source dir was empty or because hashes happened to match (the latter contradicts the user's 13-day-stale dest content, so source-empty is the leading hypothesis).

## Approach

Three-layer fix. Each layer is independently valuable; together they make this failure mode impossible to repeat.

### Layer 1 — Defensive Invariants (ship even before root cause is found)

Make the silent failure surfaces loud.

1. **Promote `declared_services: 0` to a hard error.**
   - File: `internal/reconcile/reconcile.go:511-519`
   - Change: when `ExtractDeclaredState` returns zero services, return an error from the reconcile run unless the operator opts in via `BOSUN_ALLOW_EMPTY_DECLARED_STATE=true` (escape hatch for genuinely empty repos).
   - Side fix: in `internal/reconcile/drift.go:79-119`, distinguish "no compose dir" from "compose dir exists but is empty." The first should error; the second should be a clearly logged warning.

2. **Per-file write log line in `CopyDirIfChanged`.**
   - File: `internal/fileutil/fileutil.go:247-279`
   - Change: when a file IS written (hash mismatch or new), emit a `Debug` log: `wrote src=<src> dst=<dst> bytes=<n>`. When a file is SKIPPED (hash match), emit `Trace`-or-`Debug` with reason. Today this code path has zero per-file observability — the only way to know what happened is to compare mtimes externally.
   - Use the existing `log.Component("fileutil")` pattern from `internal/log/`.

3. **Post-deploy mtime invariant.**
   - New: a verification step that runs after `deployLocal` completes and before reporting success.
   - For each path in `WrittenFiles` (across all targets), stat the destination and assert `mtime >= reconcileStartTime`. If `WrittenFiles` is empty for a target whose source dir is non-empty, that's an error.
   - Default-on; escape hatch via `BOSUN_SKIP_DEPLOY_INVARIANT=true`.
   - Best home: `internal/reconcile/deploy.go` (new `verifyDeploy` helper) called from the `doDeploy` orchestrator near `reconcile.go:548-561`.

### Layer 2 — Diagnostic Run (uses Layer 1 to confirm root cause)

Once Layer 1 is in place, do a reproduction deploy with `BOSUN_LOG_LEVEL=debug`. The new per-file logs will reveal exactly which of these is happening:

- (a) Source staging dir is empty for the target (render didn't write here)
- (b) Source has files, hashes match destination (shouldn't be possible given stale dest content)
- (c) Source path mismatch between render output and `discoverDeployTargets` walk

Strongest prior hypothesis (from the field report's `Syncing .beads/.claude/unraid` signal): **render writes to `/app/staging/output/<...>` or similar, but `discoverDeployTargets` walks `/app/staging/` directly.** Layer 1 logs will confirm or refute this in one deploy cycle.

### Layer 3 — Targeted Root-Cause Fix

The actual fix depends on Layer 2's findings. Likely candidates:

- **If render-output path mismatch:** align `discoverDeployTargets` source dir to whatever `render` writes to. Probably one-line change: replace `filepath.Join(r.config.StagingDir, r.config.InfraSubDir)` with the render-output dir, which the renderer already exposes.
- **If hash-cache bug:** investigate `CopyFileIfChanged`'s read-back verification (PR #221 added FUSE staleness guards; may have introduced a regression).
- **If `WrittenFiles` plumbing:** check `PrefixLatest` and the `t.RelPath` prefixing — CLAUDE.md flags this as a known footgun.

## Critical Files

| File | Role | Touched in layer |
|---|---|---|
| `internal/reconcile/reconcile.go` | Pipeline orchestrator; declared-state hard error | L1, L2 |
| `internal/reconcile/drift.go` | `ExtractDeclaredState` empty-vs-missing distinction | L1 |
| `internal/reconcile/deploy.go` | `deployLocal`, `discoverDeployTargets`, new `verifyDeploy` | L1, L3 |
| `internal/fileutil/fileutil.go` | Per-file write logging in `CopyDirIfChanged` / `CopyFileIfChanged` | L1 |
| `internal/log/` | Existing component logger (reuse, do not extend) | L1 |
| `internal/reconcile/reconcile_test.go` | Test cases for declared-state hard error, mtime invariant | L1 |
| `internal/fileutil/fileutil_test.go` | Test cases for per-file log emissions | L1 |

## Existing Utilities to Reuse

- `log.Component("fileutil")` — for new write/skip logs. Already standard in `internal/log/`.
- `ui.Warning` / `ui.Error` — already wired up; use for user-facing surfaces of the new hard-error case.
- `CopyDirIfChanged` return values — extend its meaning, not its signature. `WrittenFiles` already carries the data the invariant needs.
- The PR #221 read-back verification in `CopyFileIfChanged` — for the FUSE-staleness lens of root-cause investigation.

## OpenSpec Discipline

Per `openspec/AGENTS.md` and project CLAUDE.md: this is a behavior change in the reconcile pipeline (turning a silent skip into a hard error, adding a deploy invariant). **MUST create a change proposal under `openspec/changes/fix-local-deploy-sync-invariant/` with spec deltas BEFORE writing implementation code.** The proposal documents:

- The new `BOSUN_ALLOW_EMPTY_DECLARED_STATE` and `BOSUN_SKIP_DEPLOY_INVARIANT` escape hatches.
- The contract change: empty `WrittenFiles` against a non-empty source is now an error.
- The error category for declared-state-zero failures (so alert routing classifies them correctly).

Spec PR follows the Spec Review Workflow from CLAUDE.md (CodeRabbit pass, `ready-to-build` label, then implementation).

## Verification

1. **Unit tests** — `make test` in `internal/reconcile/` and `internal/fileutil/`:
   - `TestExtractDeclaredState_EmptyComposeDir_Errors`
   - `TestReconcile_DeclaredStateZero_Errors_Unless_Override`
   - `TestVerifyDeploy_StaleDestination_Errors`
   - `TestVerifyDeploy_EmptyWrittenFiles_Errors`
   - `TestCopyDirIfChanged_EmitsPerFileLogs`

2. **Integration repro** — push a commit changing a `.tmpl` (any non-trivial body change), trigger Bosun, check:
   - Without Layer 1 in place: today's silent success.
   - With Layer 1 in place: reconcile fails loudly with which invariant tripped.
   - With Layer 3 fix applied: deploy succeeds, host-side mtime updates, `grep -c <new-token>` returns nonzero.

3. **Manual smoke** —
   - `BOSUN_LOG_LEVEL=debug bosun reconcile` against a known-good change → expect per-file `wrote` lines for each changed file.
   - `BOSUN_LOG_LEVEL=debug bosun reconcile` against a no-op change → expect per-file `skipped (unchanged)` lines.
   - `BOSUN_ALLOW_EMPTY_DECLARED_STATE=true bosun reconcile` on an empty-template repo → expect warning, not error.

4. **Regression guard** — add a test fixture for the `Syncing .beads/.claude/unraid` shape (top-level repo dirs as targets) so the next refactor of `discoverDeployTargets` can't silently regress this case.

## Out of Scope

- The stale-config-after-daemon-restart issue (hooks not reloading from `bosun.yaml` until restart). That's a separate known gotcha; document it as a follow-up issue, don't bundle it.
- A `bosun adopt` command for project-name relabeling. That belongs in #219 follow-up if at all.
- Migrating to a different content-hash strategy. The hash logic is fine; the bug is upstream of it.

## Open Questions for the User

1. Should the declared-state hard error be opt-out (default on) or opt-in (default warn)? Recommendation: **opt-out**, with `BOSUN_ALLOW_EMPTY_DECLARED_STATE` as the escape hatch. Silent success is the worse default.
2. The mtime invariant: should it block compose-up, or just fail the reconcile after compose-up runs? Recommendation: **block compose-up.** A successful compose-up against stale content is the worst possible outcome.
3. Spec proposal first, or land the diagnostic logging (Layer 1 item 2) as a fast non-spec'd change and spec-proposal the invariants (Layer 1 items 1 and 3)? Recommendation: **spec-proposal everything.** The discipline is worth keeping.
