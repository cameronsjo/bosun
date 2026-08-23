## Context

`Reconciler.Run` currently renders into `Config.StagingDir`, deploys that tree,
calls `cleanupStaging`, and only then runs the health gate. The reconciler later
runs post-sync hooks and local post-deploy verification before recording success.
Consequently, a gate or verification failure has no source tree left to inspect,
even when rollback is partial or fails.

Staging is security-sensitive. Template data is the merged decrypted SOPS map, and
rendered files are currently emitted with mode `0644`. Arbitrary transformations
inside Go templates make reliable content redaction impossible: replacing literal
secret values would miss encoded, concatenated, or otherwise transformed values.
The safe design must protect the complete render rather than claim it is redacted.

Named targets already derive isolated staging paths through `ConfigForTarget` and
`TargetStagingDir`. Both the daemon and CLI reconcile targets sequentially and
continue after one target fails, so lifecycle operations must never act on the base
staging root when a named target has an effective child or explicit override.
Configured overrides can nevertheless make two effective paths equal or nested, so
derivation alone is not a sufficient deletion boundary.

## Goals / Non-Goals

- Goals:
  - Keep the exact failed render available long enough for operator diagnosis.
  - Delete staging only after the target's complete verification path succeeds.
  - Bound rendered plaintext to one per-target slot and owner-only access for its
    entire lifetime, not only after a failure is returned.
  - Preserve evidence regardless of rollback success, partial failure, or absence.
  - Keep target outcomes and staging paths independent in multi-target runs.
  - Reject overlapping effective staging paths before any target execution.
- Non-Goals:
  - Long-term or timestamped archival of rendered plaintext.
  - Automated redaction of arbitrary rendered templates.
  - Changing backup contents, rollback scope, health classification, or retry
    policy.
  - Adding a new configuration flag; safe bounded behavior is the default.

## Decisions

- **Decision: retain failures in the effective `StagingDir`, not `BackupDir`.**
  The staging path is already the rendered tree's security and ownership boundary.
  Leaving it in place avoids a second copy, and the next render's existing
  clear-before-render step provides a natural single-slot retention bound.
  - Alternatives considered: timestamped `staging-failed-*` directories under
    `BackupDir`. Rejected because they multiply plaintext secret copies and require
    a second retention policy. A redacted archive was also rejected because
    arbitrary template transformations cannot be redacted soundly.

- **Decision: cleanup follows the complete verification path.** The normal cleanup
  call moves after the health gate, post-sync hook execution, and post-deploy
  verification, but before successful completion is reported. Hook errors keep
  their existing non-fatal warning semantics. Any deployment failure after a
  completed render leaves that target's current render in place for diagnosis.
  - Alternatives considered: cleanup immediately after only the critical health
    gate. Rejected because the later post-deploy verification can independently
    reject the deployment and needs the same evidence.

- **Decision: staging is private before decrypted output is written and remains
  private, fail-closed.** Bosun prepares the effective staging root at `0700`,
  and keeps payload temp files private until atomic rename. Active descendants
  retain their existing intended modes (`0755` directories, `0644` rendered
  templates, and source modes for copied entries), because local copy and remote tar
  currently propagate those modes to the deploy destination; the `0700` root is
  the active tree's confidentiality boundary. On retention, Bosun hardens every
  directory to `0700` and regular file to `0600`. Traversal starts from the
  effective root, stays confined beneath it, uses no-follow operations, and treats
  a symlink, irregular file, or entry replaced between inspection and protection
  as a hardening failure. It logs the target, staging path, and retention outcome,
  never file contents or link targets. If Bosun cannot prove the tree private, it
  removes the staging tree without following links; if removal also fails, the
  returned failure is joined with the security error and logged as critical without
  secret material.
  - Alternatives considered: keep the existing `0755`/`0644` modes for
    convenience. Rejected because a retained render can contain plaintext secrets.

- **Decision: one evidence slot per effective target.** A later attempt preserves
  old evidence through sync, config reload, decryption, and deploy-mode resolution.
  Only entry into the next render phase clears that target's slot. If replacement
  cannot completely clear or privately recreate the slot, Bosun applies the same
  harden-or-delete rule and aborts before writing decrypted output. Otherwise the
  new render (complete or partial) becomes the current evidence. Successful targets
  remove only their own staging directory; a sibling target's failure evidence is
  untouched.

- **Decision: effective staging paths are canonical and pairwise disjoint.** Before
  any target runs, Bosun canonicalizes every effective staging path and rejects the
  target set if any two paths are equal or either is an ancestor of the other.
  Validation covers implicit-default roots, name-derived children, explicit
  overrides, and symlink-resolved existing ancestors, using the host platform's
  path comparison semantics. This prevents one target's recursive cleanup or
  hardening from reaching a sibling slot.

- **Decision: a lifecycle finalizer covers every exit after render preparation.**
  Once replacement begins, normal errors and panics both run a per-target finalizer
  that either proves the remaining tree private or deletes it without following
  links. A panic is re-raised after the retention outcome is logged. This closes
  partial-render, newly added return-path, and panic gaps; owner-only creation modes
  also limit exposure if the process is terminated before cleanup code can run.

- **Decision: rollback outcome does not control evidence retention.** A successful
  rollback restores the live managed tree but does not explain why the rejected
  render failed. A failed or partial rollback makes that render even more valuable.
  Both outcomes retain the same secured staging slot.

## Risks / Trade-offs

- A failed render can contain secrets on disk until the next render → a `0700` root
  from first write, `0700`/`0600` retained entries, re-verification on every exit,
  no extra copy, no content logging, and fail-closed deletion on a protection error
  bound the exposure.
- The next automated retry replaces evidence before an operator inspects it → the
  single-slot policy deliberately favors bounded secret retention and disk usage;
  logs identify the path and replacement event so operators know the window.
- Permission hardening may encounter symlinks, unusual files, or replacement races
  → use no-follow inspection and mutation, require only real directories and regular
  files below the confined effective root, and delete the tree if protection cannot
  be proven.
- Cleanup after verification keeps staging alive longer on successful deployments
  → the added lifetime is bounded by existing health timeouts and hook execution.
- Cleanup failure after a successful verification can leave plaintext → apply the
  same owner-only hardening fallback before reporting success; inability to harden
  or delete becomes an error rather than a silent successful leak.
- A dry run still renders and therefore replaces any prior evidence slot → retain
  the existing one-slot behavior, secure the dry-run render identically, and log
  the replacement/retention outcome so the diagnostic trade-off is visible.

## Migration Plan

No configuration or state-file schema migration is required. Before the first
post-upgrade reconciliation—and idempotently before later cycles—Bosun validates
all effective staging paths and hardens or deletes every pre-existing slot before
Git sync, secret decryption, or target execution. If a slot can be neither proven
private nor deleted, the whole cycle fails closed before any target runs. Rollback
is code-only: restore cleanup to its prior position and remove the secure staging
lifecycle and preflight.

## Open Questions

- None blocking. Durable export can be designed later as an explicit operator
  action with encryption and its own retention policy; it is intentionally outside
  this change.
