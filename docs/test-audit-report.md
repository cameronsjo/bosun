# Test Audit Report — Deploy Bug Fixes (Apr 2026)

## Summary

| Metric | Count |
|--------|------:|
| Changed files (impl + test) | 12 |
| New/modified test cases | 15 |
| Coverage gaps found | 4 |
| Coverage gaps resolved | 3 |
| Intentionally untested gaps | 1 |
| Quality issues found & fixed | 2 |
| Narrative logging arcs added | 3 |

**Scope:** GH#221 (FUSE stale read), GH#215 (deploy mode warning), GH#218 (local appdata priority), GH#220 (HTTP force-trigger), GH#219 (container name conflict warning).

---

## Coverage Gaps (by risk)

### Resolved

| # | Package | Function | Gap | Test Added | Risk |
|---|---------|----------|-----|------------|------|
| 1 | `reconcile/compose` | `detectNameConflicts` | Zero coverage — regex strips leading `/` from Docker's quoted name format | `TestDetectNameConflicts` (7 cases: no conflict, with/without slash, multiple, embedded, hyphens/underscores) | **High** — silent regex failure means no remediation guidance |
| 2 | `daemon/server` | `handleManualTrigger` | New JSON body parsing path (~80% uncovered) | 5 new subcases: force=true, empty body compat, malformed JSON degrade, force+HMAC, malformed+HMAC | **High** — HTTP-facing handler with auth gate |
| 3 | `fileutil` | `CopyFileIfChanged` | Symlink source path unreached | `CopyFileIfChanged/symlink_source_skips_gracefully` | **Medium** — symlinks in deploy dirs are uncommon but possible |

### Intentionally Untested

| # | Package | Function | Gap | Rationale |
|---|---------|----------|-----|-----------|
| 1 | `fileutil` | `CopyFileIfChanged` | FUSE stale-read paths (`sizesDiffer` after hash match, byte-comparison divergence) | Structurally untestable without OS-level mock injection. These branches defend against kernel cache bugs that can't be synthetically reproduced. Adding a `statFn` injection point would pollute the function signature for a scenario that's a kernel bug, not application logic. The coverage number reflects defensive code, not a testing deficiency. |

---

## Quality Issues

### Found & Fixed

| # | Package | Issue | Fix |
|---|---------|-------|-----|
| 1 | `daemon/server` | `handleManualTrigger` returned HTTP 400 on malformed JSON body, but `force` is optional metadata — rejecting the entire trigger for a parse error is too strict | Changed to graceful degradation: log at Debug level, proceed with `force=false` |
| 2 | `reconcile/compose` | Per-container conflict messages logged at `Info` level despite being actionable remediation guidance | Elevated to `Warn` level |

---

## Narrative Logging Added

### 1. `internal/fileutil/fileutil.go` — FUSE Guard Arc

- **Before:** `Debug` — `"verifying content before skip"` (signals FUSE guard sequence entry)
- **Success:** `Debug` — `"skip confirmed"` (greppable proof the fast path was taken)
- **Failure:** `Warn` — `"FUSE staleness detected"` (reworded from vague "stale read?" to a named event class)
- **Post-write:** `Debug` — hash verification success

### 2. `internal/reconcile/reconcile.go` — Secrets Fallback Arc

- **Decision point:** `Warn` with structured fields `resolved_via=secrets`, `secrets_key=network.unraid_ip` — makes the implicit-remote decision observable in structured log pipelines (Loki, Datadog) without parsing human-readable strings

### 3. `internal/reconcile/compose.go` — Name Conflict Detection Arc

- **Start:** `Info` — `"checking for container name conflicts"` with `file_count` (failed stacks context)
- **Per-item:** `Warn` — per-container conflict with remediation command
- **Summary:** `Info` — `"container name conflicts detected, remediation commands logged"` with `conflict_count` (machine-parseable for alerting)

---

## Additional Tests Added (not gap-driven)

| Package | Test | Purpose |
|---------|------|---------|
| `fileutil` | `ContentEqual/returns_error_when_path_is_unreadable` | Covers non-`IsNotExist` error branch (chmod 0000, skips under root) |
| `reconcile` | `TestResolveDeployMode/implicit_secrets-only_remote_with_no_appdata_configured` | Isolates exact condition triggering the new structured Warn log |

---

## Recommended Next Steps

1. **Run full test suite** — Verify no regressions from the 15 new/modified test cases across all 4 packages
2. **Merge deploy bug PRs** — Coverage gaps are now addressed; the one intentional gap is documented
3. **Monitor FUSE staleness logs** — The new `"FUSE staleness detected"` Warn messages will confirm whether the defensive code paths are actually reached in production on Unraid
4. **Consider integration test for force-trigger** — The unit tests cover `handleManualTrigger` directly, but an end-to-end test through the daemon's TCP/socket interface with `force=true` would catch serialization issues
5. **Add `detectNameConflicts` to regression suite** — Docker's container name format (`"/name"` with leading slash) has changed across API versions; a regression test pinned to the current format prevents silent breakage
