## ADDED Requirements

### Requirement: Recursive Glob Matching Correctness

The glob matcher SHALL honor the full pattern including any literal suffix that follows a `**` segment, and SHALL NOT collapse a `**` pattern to a prefix-only check. A pattern beginning with `**/` SHALL match a file only when the remainder of the pattern matches the file's trailing path segments, and SHALL NOT match every changed file unconditionally.

The same matcher backs post-sync hook path matching and the `deploy_paths`, `deploy_sync_paths`, and `deploy_sync_exclude` evaluation used for path-aware deploy skipping, so the corrected semantics SHALL apply identically to all of them.

#### Scenario: Suffix after recursive segment is honored

- **WHEN** a hook pattern is `appdata/**/dynamic.yml`
- **AND** the changed file is `appdata/traefik/dynamic.yml`
- **THEN** the pattern matches the file
- **AND** the changed file `appdata/traefik/other.yml` does NOT match

#### Scenario: Leading recursive segment does not match everything

- **WHEN** a hook pattern is `**/foo.yml`
- **AND** the changed file is `unrelated/bar.yml`
- **THEN** the pattern does NOT match
- **AND** the changed file `nested/dir/foo.yml` DOES match

#### Scenario: Deploy-path family uses the corrected matcher

- **WHEN** `deploy_paths` contains `traefik/**/*.yml`
- **AND** a commit changes only `traefik/conf.d/dynamic.yml`
- **THEN** path-aware skip treats the deploy as relevant (the pattern matches the suffix, not just the `traefik/` prefix)
- **AND** a commit changing only `docker-compose.yml` is NOT matched by that pattern

### Requirement: FUSE-Safe Hook Timing

The reconciler SHALL ensure written files are durably visible on the target filesystem before running post-sync hooks. It SHALL fsync each destination directory after renaming written files into place, and it SHALL apply a settle delay before executing hooks whose default value is a safe non-zero duration rather than zero.

The settle delay SHALL be configurable via `hook_settle_delay` in `bosun.yaml` and the `BOSUN_HOOK_SETTLE_DELAY` environment variable. The `bosun doctor` preflight SHALL warn when a FUSE-like deploy target is configured with a zero settle delay, because hooks may then fire before the rename has propagated through FUSE caches.

#### Scenario: Destination directory fsynced after write

- **WHEN** the reconciler writes a config file to a deploy target and renames it into place
- **THEN** the destination directory is fsynced before any post-sync hook runs

#### Scenario: Default settle delay is non-zero

- **WHEN** neither `hook_settle_delay` nor `BOSUN_HOOK_SETTLE_DELAY` is set
- **THEN** the reconciler applies a safe non-zero settle delay before running hooks
- **AND** the deploy does not fire hooks with a zero delay by default

#### Scenario: Doctor warns on zero delay for FUSE target

- **WHEN** `bosun doctor` runs against a FUSE-like deploy target (e.g. an Unraid `/mnt/user` path)
- **AND** the effective `hook_settle_delay` is `0s`
- **THEN** doctor emits a warning that hooks may fire before FUSE propagation completes

#### Scenario: Explicit zero delay honored on non-FUSE target

- **WHEN** an operator sets `BOSUN_HOOK_SETTLE_DELAY=0s`
- **THEN** the reconciler applies no settle delay
- **AND** no startup error is raised

### Requirement: Hook Match Observability

When post-sync hooks are configured and the reconciler evaluates a non-empty set of changed files but no file matches any hook pattern, the reconciler SHALL emit a warning that surfaces the misconfiguration. The warning SHALL include complete distinct, duplicate, empty, and missing pattern counts plus evaluated and matched-file counts; SHALL include at most five bounded pattern samples and five bounded staging-relative changed-file samples; SHALL redact absolute or traversal paths; and SHALL distinguish "no files changed" from "files changed but no pattern matched", so a typo'd glob produces a discoverable signal rather than a silent no-op. It SHALL NOT include file contents or hook command arguments.

#### Scenario: Typo'd pattern surfaces a warning

- **WHEN** a hook is configured with paths `["traefik/**"]`
- **AND** the deploy change set contains `appdata/traefik/dynamic.yml` (prefixed differently)
- **THEN** the reconciler logs a warning with bounded configured-pattern and staging-relative changed-file samples plus complete counts
- **AND** the warning indicates files changed but none matched
- **AND** the warning does not include file contents or hook command arguments

#### Scenario: No-change deploy is not flagged as a mismatch

- **WHEN** hooks are configured
- **AND** the deploy change set is empty
- **THEN** the reconciler does not emit a no-match warning (the "no files changed" case is logged distinctly, not as a misconfiguration)

### Requirement: Post-Write Verification Propagation

A successful file write whose post-write content verification fails SHALL NOT be silently omitted from the deploy change set. When `CopyFileIfChanged` renames a file into place successfully but the post-write hash readback fails — a known FUSE-staleness symptom — the reconciler SHALL either record the path in the change set so matching hooks still fire, or surface the verification failure as an error. It SHALL NOT return as if the file were unchanged, which would also cause the file to be skipped (and the hook to miss) on a later retry.

#### Scenario: Verification failure still records the written path

- **WHEN** a file is renamed into place successfully
- **AND** the post-write content verification fails
- **THEN** the path is recorded in the change set (hook-eligible) or the deploy surfaces an error
- **AND** the file is NOT silently treated as unchanged

#### Scenario: Retry after verification failure does not orphan the hook

- **WHEN** a prior deploy left a file whose verification failed and which was not recorded
- **AND** a subsequent deploy finds the on-disk content already matches the source hash
- **THEN** the reconciler does not skip the file in a way that permanently prevents the hook from firing

### Requirement: Hook Config Presence Semantics

On a successful `bosun.yaml` hot-reload, the reconciler SHALL apply field-specific absence semantics. An absent `hook_settle_delay` key SHALL retain the previously effective in-memory value, while an explicit `hook_settle_delay: 0s` SHALL set it to zero. An absent `post_sync_hooks` key SHALL clear stale file-sourced hooks, as SHALL an explicit `post_sync_hooks: []`.

The daemon and one-shot CLI initial-load and reload paths SHALL implement the same semantics. A supported config file that is present and successfully parsed is a successful snapshot even when the file is empty. A missing config file or a config read, parse, unknown-field, or non-hook validation error SHALL retain the existing effective values under the established graceful-degradation behavior; only a successfully loaded config snapshot can clear file-sourced hooks. The config loader and reload DTO SHALL preserve enough file-presence and key-presence metadata to distinguish a present empty file from no file and an absent delay from explicit zero.

`BOSUN_POST_SYNC_HOOKS` and `BOSUN_HOOK_SETTLE_DELAY` SHALL remain authoritative replacement overrides: a successful file reload SHALL NOT change either environment-sourced value. Hook overrides supplied through an authoritative `BOSUN_TARGETS` value SHALL likewise remain environment-owned and SHALL NOT be replaced by repo target hooks.

Root hook state SHALL be applied before per-target overrides. A target with no `post_sync_hooks` key SHALL inherit the successfully reloaded root hook state, an explicit target `post_sync_hooks: []` SHALL clear inherited hooks for that target, and removing a prior target-specific hook key SHALL discard the stale target hooks and fall back to the current root state. If a target descriptor is removed from the repo while the daemon continues using its startup target topology, its stale operational hook override SHALL be discarded and its existing reconciler SHALL use current root hooks; structural target removal continues to require a daemon restart.

The reconciler SHALL validate root hooks and every target hook override before applying any hook-related field. Invalid executable hooks SHALL abort the current reconciliation before deployment and SHALL leave the prior hooks and delay unchanged. A successful snapshot SHALL be deep-cloned before application so root and target `PostSyncHook`, `Paths`, and `Command` slices are isolated from the loader and from other targets under concurrent reconciliation. Reload logs SHALL identify whether hook state was applied, cleared, retained, or rejected, plus the source and target where relevant, without logging hook command arguments.

#### Scenario: Absent settle-delay key retains prior value

- **WHEN** the daemon loaded `hook_settle_delay: 5s`
- **AND** a later commit drops the `hook_settle_delay` line
- **THEN** a hot-reload leaves the in-memory delay at `5s` (unchanged)

#### Scenario: Explicit zero delay clears the value

- **WHEN** a later commit sets `hook_settle_delay: 0s` explicitly
- **THEN** a hot-reload sets the in-memory delay to zero

#### Scenario: Absent hooks key clears file-sourced hooks

- **WHEN** the daemon loaded one or more `post_sync_hooks`
- **AND** a later commit removes the `post_sync_hooks:` section entirely
- **THEN** a successful hot-reload clears the previously file-sourced hooks

#### Scenario: Empty hooks list clears hooks

- **WHEN** a later commit sets `post_sync_hooks: []`
- **THEN** a hot-reload clears the in-memory hooks

#### Scenario: Present empty config is a successful snapshot

- **WHEN** a supported project config file exists and parses successfully but is empty
- **THEN** hot-reload clears prior file-sourced root hooks
- **AND** retains the prior effective settle delay because `hook_settle_delay` is absent

#### Scenario: Environment hook override survives file removal

- **WHEN** `BOSUN_POST_SYNC_HOOKS` supplies the effective hooks
- **AND** a successfully reloaded `bosun.yaml` omits `post_sync_hooks`
- **THEN** the environment-sourced hooks remain unchanged

#### Scenario: Environment delay override survives file reload

- **WHEN** `BOSUN_HOOK_SETTLE_DELAY` supplies the effective delay
- **AND** a successfully reloaded `bosun.yaml` omits or changes `hook_settle_delay`
- **THEN** the environment-sourced delay remains unchanged

#### Scenario: Environment target hook override survives repo reload

- **WHEN** authoritative `BOSUN_TARGETS` supplies target-specific hooks
- **AND** a successfully reloaded repo config changes, clears, or removes hooks for that target
- **THEN** the environment-sourced target hooks remain unchanged

#### Scenario: Missing config file retains effective values

- **WHEN** the repo no longer contains a supported project config file
- **THEN** hot-reload retains the existing hooks and settle delay
- **AND** reconciliation continues under graceful degradation

#### Scenario: Unreadable or malformed config retains effective values

- **WHEN** the repo's project config cannot be read or parsed
- **THEN** hot-reload retains the existing hooks and settle delay
- **AND** reconciliation continues under graceful degradation
- **AND** no partial root or target hook state is applied

#### Scenario: Invalid executable hooks fail closed

- **WHEN** a successfully decoded project config contains an invalid executable hook
- **THEN** hot-reload rejects the entire hook-related snapshot and aborts the current reconciliation before deployment
- **AND** prior root hooks, target hooks, and settle delay remain unchanged
- **AND** the failure is logged without exposing command arguments

#### Scenario: Target without hook key inherits current root

- **WHEN** root hooks are configured
- **AND** a target is present but omits its `post_sync_hooks` key
- **THEN** that target receives a deep-cloned copy of the current root hooks

#### Scenario: Target hook removal falls back to root

- **WHEN** a target previously supplied target-specific hooks
- **AND** a successfully reloaded config keeps the target but removes its `post_sync_hooks` key
- **THEN** stale target-specific hooks are discarded
- **AND** that target inherits the current root hook state

#### Scenario: Explicit empty target hooks clear inheritance

- **WHEN** root hooks are configured
- **AND** a target explicitly sets `post_sync_hooks: []`
- **THEN** that target has no hooks after reload

#### Scenario: Removed target descriptor drops stale operational hooks

- **WHEN** a target previously supplied target-specific hooks
- **AND** a successful repo snapshot removes that target descriptor while the daemon still has the target in its startup topology
- **THEN** the existing target reconciler discards the stale target-specific hooks and falls back to current root hooks
- **AND** the daemon logs that structural target removal still requires restart

#### Scenario: Reloaded hook slices are isolated under concurrency

- **WHEN** root hooks are inherited by multiple targets and reconciliations run concurrently
- **THEN** each target owns independent hook, path, and command slices
- **AND** mutation in the loader snapshot or one target cannot alter another target's effective hooks
- **AND** race-detector tests report no concurrent access violation

#### Scenario: Reload logging is source-aware and redacted

- **WHEN** a reload applies, clears, retains, or rejects hook-related state
- **THEN** logs identify the outcome, source, hook count, and target when applicable
- **AND** logs do NOT include hook command arguments

#### Scenario: Initial load uses the same presence distinction

- **WHEN** a valid project config initially omits both hook keys
- **THEN** file-sourced hooks are empty
- **AND** `hook_settle_delay` remains default-sourced (or environment-sourced) rather than being marked as an explicit file zero
- **AND** the effective delay is the safe non-zero default unless an environment override is present
