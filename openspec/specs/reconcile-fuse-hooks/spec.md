# reconcile-fuse-hooks Specification

## Purpose

Define the filesystem durability, recursive path matching, change-set,
diagnostic, and configuration-reload contracts that keep post-sync hooks
reliable, including on Unraid FUSE deploy targets.

## Requirements
### Requirement: Recursive Glob Matching Correctness

The glob matcher SHALL honor the full pattern including any literal suffix that follows a `**` segment, and SHALL NOT collapse a `**` pattern to a prefix-only check. A pattern beginning with `**/` SHALL match a file only when the remainder of the pattern matches the file's trailing path segments, and SHALL NOT match every changed file unconditionally.

The same matcher SHALL back post-sync hook path matching, `deploy_paths`
relevance checks, the `deploy_sync_paths` target allowlist, and the
`deploy_sync_exclude` target blocklist, so recursive semantics apply identically
at every consumer.

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

#### Scenario: Deploy paths use the corrected matcher

- **WHEN** `deploy_paths` contains `traefik/**/*.yml`
- **AND** a commit changes only `traefik/conf.d/dynamic.yml`
- **THEN** path-aware skip treats the deploy as relevant (the pattern matches the suffix, not just the `traefik/` prefix)
- **AND** a commit changing only `docker-compose.yml` is NOT matched by that pattern

#### Scenario: Deploy-sync allowlist uses the corrected matcher

- **WHEN** `deploy_sync_paths` contains `appdata/**/traefik`
- **AND** one-level deploy-target discovery evaluates `appdata/traefik`
- **THEN** that target is included
- **AND** `appdata/authelia` is NOT included by that pattern

#### Scenario: Deploy-sync blocklist uses the corrected matcher

- **WHEN** `deploy_sync_exclude` contains `appdata/**/retired`
- **AND** one-level deploy-target discovery evaluates `appdata/retired`
- **THEN** that target is excluded
- **AND** `appdata/active` is NOT excluded by that pattern

### Requirement: FUSE-Safe Hook Timing

For local file replacement, the reconciler SHALL sync each changed destination
parent directory after atomic rename and before post-write verification or
post-sync hooks. Directory-copy paths MAY batch these operations, but SHALL sync
each unique changed parent deterministically before returning. Platform-specific
directory-sync limitations SHALL retain the repository's portable file-copy
contract rather than preventing supported deployments.

The settle delay SHALL be configurable via `hook_settle_delay` in `bosun.yaml`
and `BOSUN_HOOK_SETTLE_DELAY`. When no file value or valid environment duration
is configured and the effective local deploy path is exactly `/mnt/user` or is
beneath that path-segment boundary, the reconciler SHALL apply a 2-second
fallback before hooks. A valid environment duration SHALL override the file
value, including `0s`; invalid environment input SHALL be ignored in favor of
the file value or applicable omitted-value default. An explicit file value,
including `0s`, SHALL override the fallback, and an unconfigured non-`/mnt/user`
path SHALL retain zero delay. `bosun doctor` SHALL use the same presence/source
distinction and warn only when a `/mnt/user` deploy path has an effective zero
delay; it SHALL NOT warn for an omitted value whose effective runtime delay is
the 2-second fallback.

#### Scenario: Destination directory fsynced after write

- **WHEN** the reconciler writes a config file to a deploy target and renames it into place
- **THEN** the changed destination parent is synced before post-write verification and before any post-sync hook runs

#### Scenario: Unconfigured Unraid target receives safe fallback

- **WHEN** neither `hook_settle_delay` nor `BOSUN_HOOK_SETTLE_DELAY` is set
- **AND** the local deploy path is `/mnt/user/appdata`
- **THEN** the reconciler waits 2 seconds before running hooks

#### Scenario: Doctor warns on zero delay for FUSE target

- **WHEN** `bosun doctor` runs against a FUSE-like deploy target (e.g. an Unraid `/mnt/user` path)
- **AND** the effective `hook_settle_delay` is `0s`
- **THEN** doctor emits a warning that hooks may fire before FUSE propagation completes

#### Scenario: Doctor does not warn for safe fallback

- **WHEN** `bosun doctor` runs against a `/mnt/user` deploy target
- **AND** neither settle-delay source is configured
- **THEN** doctor does NOT emit a zero-delay warning
- **AND** reconcile's effective settle delay remains the 2-second fallback

#### Scenario: Explicit zero disables fallback

- **WHEN** an operator sets `BOSUN_HOOK_SETTLE_DELAY=0s`
- **AND** the deploy path is under `/mnt/user`
- **THEN** the reconciler applies no settle delay
- **AND** no startup error is raised

#### Scenario: Unconfigured non-Unraid target retains zero delay

- **WHEN** neither settle-delay source is configured
- **AND** the deploy path is `/srv/appdata`
- **THEN** the reconciler applies no settle delay

#### Scenario: Lookalike path does not receive fallback

- **WHEN** neither settle-delay source is configured
- **AND** the deploy path is `/mnt/userdata/appdata`
- **THEN** the reconciler applies no settle delay

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

A file whose atomic rename succeeds SHALL remain in the deploy result when its
post-write content verification fails, and the copy SHALL return an error that
wraps `fileutil.ErrPostWriteVerification`. The reconciliation SHALL remain
failed, retain its redeploy marker, send the normal failure alert, and SHALL NOT
advance the successful deployment diff base. When Docker is available, dry run
is false, hooks are configured, and a previous successful commit exists, the
reconciler SHALL still evaluate matching hooks for the preserved path as
best-effort remediation. Hook execution SHALL NOT convert the deployment to
success.

#### Scenario: Verification failure still records the written path

- **WHEN** a file is renamed into place successfully
- **AND** the post-write content verification fails
- **THEN** the path is recorded in the deploy result and remains hook-eligible
- **AND** the deploy surfaces `ErrPostWriteVerification`
- **AND** the file is NOT silently treated as unchanged

#### Scenario: Verification remediation does not erase failure

- **WHEN** a preserved path matches a configured hook after post-write verification fails
- **THEN** the reconciler may execute that hook as best-effort remediation
- **AND** the reconciliation still fails with its redeploy marker and successful diff base unchanged

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
- **AND** the effective delay receives the 2-second fallback only when the deploy path is `/mnt/user` or beneath it

### Requirement: Directory-Aware Deploy Change Tracking

Content-hash sync SHALL include each descendant directory it actually creates
in the canonical staging-relative deploy change set, including empty
directories. It SHALL exclude an ordinarily created deploy target root and
directories that already existed, so plumbing and no-op reconciles do not
trigger post-sync hooks. A managed file-to-directory transition SHALL include
the transitioned path, including a top-level target path, and every descendant
directory created from its source subtree.

Each recorded directory SHALL be subject to the post-deploy existence, type,
and fresh-mtime invariant. A change set containing directories but no regular
file writes SHALL NOT suppress the existing content invariant: every regular
source file must already be present and byte-identical at the destination.

Hook change-source selection SHALL preserve this priority. A remote deploy SHALL
make every configured hook eligible regardless of its result slices. A
successful content-hash local deploy SHALL use `WrittenFiles` and `DeletedFiles`
as its complete path-level change set even when both are empty. A standard-copy
local deploy SHALL use non-empty `WrittenFiles` or `DeletedFiles` as direct path
evidence; only when both are empty SHALL it use normalized git diff. The
implementation SHALL NOT infer a different selector or introduce a parallel
authority marker.

#### Scenario: Newly created empty directory triggers a matching hook

- **WHEN** content-hash sync creates a previously absent descendant directory containing no files
- **THEN** its staging-relative path is included in the deploy change set
- **AND** a matching post-sync hook is eligible to run

#### Scenario: Existing directory remains a no-op

- **WHEN** a source directory already exists at the destination
- **THEN** that directory is not included in the deploy change set
- **AND** an otherwise unchanged reconcile does not trigger a hook

#### Scenario: Content-hash no-op remains authoritative

- **WHEN** a successful content-hash local deploy reports empty `WrittenFiles` and `DeletedFiles`
- **THEN** the selected content-hash mode makes the result authoritative
- **AND** hooks treat the result as an authoritative no-change result without invoking git-diff fallback

#### Scenario: Content-hash deletion-only result remains authoritative

- **WHEN** a successful content-hash local deploy reports only `DeletedFiles`
- **THEN** the selected content-hash mode makes the result authoritative
- **AND** hooks evaluate the reported deletions without invoking git-diff fallback

#### Scenario: Deploy target root is plumbing

- **WHEN** content-hash sync creates the deploy target root or a missing ancestor needed to reach it
- **THEN** neither the root marker nor its ancestors are included in the deploy change set

#### Scenario: Descendant directory keeps its canonical target prefix

- **WHEN** content-hash sync creates descendant directory `cache` for deploy target `appdata/service`
- **THEN** local deploy aggregation records `appdata/service/cache` in the canonical staging-relative change set
- **AND** post-sync hook matching evaluates that prefixed path rather than the producer-local `cache` path

#### Scenario: Compose directory keeps its canonical target prefix

- **WHEN** content-hash sync creates descendant directory `fragments` in the separately deployed `compose` target
- **THEN** compose aggregation records `compose/fragments` in the canonical staging-relative change set
- **AND** post-sync hook matching evaluates that prefixed path rather than the producer-local `fragments` path

#### Scenario: Directory-only change set does not mask a missing file

- **WHEN** the deploy change set contains newly created directories but no regular file writes
- **AND** a regular source file is missing or byte-different at the destination
- **THEN** the post-deploy invariant fails before compose-up

#### Scenario: Recorded directory must be fresh and remain a directory

- **WHEN** a recorded directory is missing, has an mtime older than the reconcile start, or is a non-directory destination entry (including a symlink)
- **THEN** the post-deploy invariant fails before compose-up

#### Scenario: Nested managed file becomes an empty directory tree

- **WHEN** a previously managed regular file below a deploy target is replaced by a source directory containing empty descendants
- **THEN** the transitioned path and each created descendant directory are included in the deploy change set
- **AND** matching hooks can observe the type transition

#### Scenario: Top-level managed file becomes a directory

- **WHEN** a deploy target previously recorded as a managed regular file is replaced by a source directory
- **THEN** the top-level target path is included in the deploy change set despite ordinary target-root creation being plumbing
- **AND** each created descendant directory is included under that target path

#### Scenario: Standard-copy local deploy retains normalized git-diff fallback

- **WHEN** a standard-copy local deploy reports empty `WrittenFiles` and `DeletedFiles`
- **THEN** the result provides no direct path evidence
- **AND** hooks evaluate the existing git-diff fallback after normalization to canonical staging-relative paths

#### Scenario: Standard-copy local direct evidence remains authoritative

- **WHEN** a standard-copy local deploy reports non-empty `WrittenFiles` or `DeletedFiles`
- **THEN** hooks evaluate those reported paths directly
- **AND** git-diff fallback is not invoked

#### Scenario: Remote deploy retains unconditional all-hooks fallback

- **WHEN** a remote deploy completes
- **THEN** its result slices are not used for path filtering, regardless of either slice's length
- **AND** every configured post-sync hook remains eligible to run without path filtering
