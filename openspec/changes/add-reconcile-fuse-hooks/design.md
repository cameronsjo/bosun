## Context

Post-sync hooks are bosun's documented answer to Unraid's FUSE (shfs) config
propagation problem: after a config file syncs, a service like Traefik watching a
different FUSE handle won't see the write for several seconds, so bosun restarts
the container to force a re-read. The pipeline that drives this — glob matching,
the change set the matcher evaluates, the timing of the restart relative to the
write becoming durably visible, and the hot-reload of hook config — has several
silent-failure modes discovered in the Cluster D bug hunt. Each makes a hook
report success while doing nothing the operator can observe.

Two properties make these bugs pernicious: they are silent (success logs fire
regardless), and they manifest precisely on the FUSE target bosun was built for,
so an operator running on a non-FUSE local disk during testing never reproduces
them.

## Goals / Non-Goals

- Goals:
  - Glob semantics that are correct and identical everywhere `matchGlob` is used,
    including the `deploy_paths` family — one matcher, one behavior.
  - Hooks fire only after written files are durably visible on the target
    filesystem, with a safe-by-default delay rather than a footgun `0`.
  - Every "hook did nothing" path is observable: typo'd glob, empty command,
    deletion-only commit, verification failure.
  - Hot-reload distinguishes "key absent" from "key explicitly set", so an
    operator's FUSE workaround is never silently dropped.
  - Root and target hook state changes as one validated, owned snapshot; failed
    loads and concurrent per-target reconciliation cannot observe partial or
    aliased hook configuration.
- Non-Goals:
  - A general file-watch / inotify replacement for restart hooks.
  - Cross-platform FUSE detection beyond a best-effort heuristic for the doctor
    warning (the safe non-zero default protects operators who aren't detected).
  - New hook actions beyond the existing `restart` / `exec`.

## Decisions

- **Decision: Replace the hand-rolled `**` prefix check with full doublestar
  semantics.** The current matcher (`strings.SplitN(pattern, "**", 2)[0]`) throws
  away the suffix and short-circuits `**/foo` to match-everything. Adopt a real
  doublestar implementation (`github.com/bmatcuk/doublestar/v4`) or a vetted
  glob→regex translation so `appdata/**/dynamic.yml`, `**/dynamic.yml`, and
  `traefik/**/*.yml` all evaluate correctly. Because `matchAnyPath` shares this
  matcher, the fix is spec'd as a single glob-correctness requirement covering
  hooks AND the deploy-path family.
  - Alternatives considered: keep prefix-only and validate patterns at load to
    reject `**`-with-suffix (rejected — silently weakens the documented feature).

- **Decision: Two-part FUSE safety — directory fsync + non-zero settle default.**
  fsync of the destination directory after rename makes the rename durable in the
  underlying filesystem; the settle delay covers the *propagation* window where
  FUSE caches still serve stale entries to other handles. Both are needed: fsync
  alone doesn't defeat client-side FUSE caching, and a delay alone doesn't
  guarantee the rename hit disk. Default `HookSettleDelay` to a safe non-zero
  value (e.g. `2s`); `doctor` warns when a FUSE-like target path runs with `0s`.
  - Alternatives considered: poll the target for the new content hash before
    firing (more robust but requires reading back through the same FUSE handle the
    service uses, which bosun doesn't have; keep as a future enhancement).

- **Decision: Track deletions in the change set the hook matcher consults.**
  `removeStaleFiles` deletions are appended to `WrittenFiles` (or a parallel
  `DeletedFiles` the matcher unions), tagged with an op (`add` | `remove`) so a
  future hook can filter. The minimum bar: a deletion-only commit that matches a
  hook pattern fires the hook. This is also why the empty-match early return
  (#269) must move *below* the deletion merge — otherwise deletions are merged but
  the function still returns before evaluating them.

- **Decision: Verification failure is a recorded write, not a void.** A
  `CopyFileIfChanged` that renames successfully but fails post-write hash readback
  (a FUSE-staleness symptom) still records the path as written so the hook fires;
  the alternative is to propagate the failure as a hard error. Silent omission —
  the current behavior — is the one outcome the spec forbids.

- **Decision: Field-specific presence semantics.** `hook_settle_delay` carries
  raw YAML presence through config loading and the reload DTO so an absent key
  retains the effective value and an explicit `0s` clears it. `post_sync_hooks`
  is different: after a config file loads successfully, its complete hook slice
  is authoritative, so both an absent key and `post_sync_hooks: []` clear stale
  file-sourced hooks. A missing config file or parse/load error is not a
  successful snapshot and retains both prior values. Environment overrides stay
  authoritative replacements and are never changed by file reload.
  - Alternatives considered: require `post_sync_hooks: []` to clear (rejected
    because deleting the section is the natural declarative removal and otherwise
    leaves stale executable behavior); treat an absent delay as zero (rejected
    because it silently removes the FUSE workaround in #267).

- **Decision: Config-file presence defines snapshot success.** A supported
  config file that exists and parses successfully is an authoritative snapshot
  even when it is empty: it clears root file-sourced hooks but retains the prior
  effective delay because that key is absent. A missing file, read failure,
  malformed YAML, unknown field, or failed hook validation produces no snapshot
  and retains every prior hook-related value. The loader/reload DTO must carry
  enough metadata to distinguish a present empty file from no file; a zero-value
  `Config` alone is not sufficient.
  - Alternatives considered: treat missing and empty identically (rejected — it
    either makes deleting the config execute stale hooks or makes temporary file
    loss destructively clear live behavior).

- **Decision: Validate, clone, then commit hook state.** The reloader validates
  root hooks and every target hook override before mutating the effective root,
  delay, or target state. After validation, it deep-clones nested hook slices and
  applies root state before the current target override. A target whose hook key
  disappears, or whose descriptor is removed while the daemon still uses its
  startup target topology, immediately loses stale operational hooks and falls
  back to current root hooks; structural target removal still requires the
  existing daemon restart. Logs describe applied, cleared, retained, or rejected
  state by source and target using counts only — never command arguments.
  - Alternatives considered: mutate fields as they are decoded (rejected — a
    later invalid target can leave a partially applied executable snapshot);
    reuse decoded slices (rejected — concurrent target reconcilers can alias and
    race on nested `Paths` / `Command` slices).

## Risks / Trade-offs

- **Glob correctness is a behavior change** → patterns relying on the old
  prefix-only luck may stop matching. Mitigated by treating it as a documented
  breaking fix and by the new no-match warning making regressions discoverable.
- **Non-zero settle-delay default slows every deploy slightly** → mitigated by a
  small default (~2s) and operator override to `0s` on non-FUSE targets.
- **New dependency (doublestar)** → small, widely used, MIT; vetted as part of
  implementation.
- **Deletion tracking could over-fire hooks** on large prune commits → mitigated
  by per-container dedup (already "restart at most once per deploy").
- **A removed target remains in the daemon's startup topology until restart** →
  mitigated by dropping its stale operational hook override immediately, logging
  the fallback to root, and retaining the existing restart requirement for
  structural target changes.
- **Snapshot logging could expose executable arguments** → mitigated by logging
  source, presence, target, and counts only; hook commands remain redacted.

## Migration Plan

1. Operators relying on prefix-only glob behavior: review `post_sync_hooks` and
   `deploy_paths` patterns against the new no-match warning after first deploy.
2. Operators on non-FUSE local disk who want the old instant behavior: set
   `BOSUN_HOOK_SETTLE_DELAY=0s` (or `hook_settle_delay: 0s`) explicitly.
3. Operators removing file-sourced hooks may delete the section, empty the
   config file, or set `post_sync_hooks: []`; each successful snapshot clears
   them. Deleting the config file itself retains the last effective state.
4. Operators removing a target hook override fall back to root hooks immediately;
   removing the target structurally still requires a daemon restart.
5. Rollback: revert the matcher and default; the additive presence/snapshot
   representation is backwards-compatible to read.

## Open Questions

- Exact safe default for `HookSettleDelay` (`2s` proposed) — settle during
  implementation against real Unraid propagation measurements.
- FUSE detection heuristic for the doctor warning: statfs magic vs. mount-table
  inspection vs. path prefix (`/mnt/user`). Lean: statfs `f_type` where available,
  fall back to warn-always-on-zero.
- Whether deletion entries should default to firing `restart` hooks only, or also
  `exec` hooks (lean: both; dedup protects against storms).
