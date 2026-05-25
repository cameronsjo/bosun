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

When post-sync hooks are configured and the reconciler evaluates a non-empty set of changed files but no file matches any hook pattern, the reconciler SHALL emit a warning that surfaces the misconfiguration. The warning SHALL name the configured patterns and a sample of the evaluated changed files, and SHALL distinguish "no files changed" from "files changed but no pattern matched", so a typo'd glob produces a discoverable signal rather than a silent no-op.

#### Scenario: Typo'd pattern surfaces a warning

- **WHEN** a hook is configured with paths `["traefik/**"]`
- **AND** the deploy change set contains `appdata/traefik/dynamic.yml` (prefixed differently)
- **THEN** the reconciler logs a warning naming the configured patterns and sample changed files
- **AND** the warning indicates files changed but none matched

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

### Requirement: Hook Config Hot-Reload Removal Semantics

On `bosun.yaml` hot-reload, the reconciler SHALL distinguish a `post_sync_hooks` or `hook_settle_delay` key that is absent from one that is explicitly set. An absent key SHALL retain the previously loaded in-memory value; it SHALL NOT silently reset the value to its zero. The reload DTO fields SHALL be pointer-typed so that an absent YAML key decodes to nil and is skipped by the reloader.

To clear hooks, an operator SHALL set `post_sync_hooks: []`. To reset the settle delay to zero, an operator SHALL set `hook_settle_delay: 0s` explicitly.

#### Scenario: Absent settle-delay key retains prior value

- **WHEN** the daemon loaded `hook_settle_delay: 5s`
- **AND** a later commit drops the `hook_settle_delay` line
- **THEN** a hot-reload leaves the in-memory delay at `5s` (unchanged)

#### Scenario: Explicit zero delay clears the value

- **WHEN** a later commit sets `hook_settle_delay: 0s` explicitly
- **THEN** a hot-reload sets the in-memory delay to zero

#### Scenario: Absent hooks key retains prior hooks

- **WHEN** the daemon loaded one or more `post_sync_hooks`
- **AND** a later commit removes the `post_sync_hooks:` section entirely
- **THEN** a hot-reload retains the previously loaded hooks (no silent clear)

#### Scenario: Empty hooks list clears hooks

- **WHEN** a later commit sets `post_sync_hooks: []`
- **THEN** a hot-reload clears the in-memory hooks
