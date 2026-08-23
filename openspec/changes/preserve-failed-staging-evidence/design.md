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

## Goals / Non-Goals

- Goals:
  - Keep the exact failed render available long enough for operator diagnosis.
  - Delete staging only after the target's complete verification path succeeds.
  - Bound retained plaintext to one per-target slot and owner-only access.
  - Preserve evidence regardless of rollback success, partial failure, or absence.
  - Keep target outcomes and staging paths independent in multi-target runs.
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

- **Decision: retained evidence is hardened owner-only, fail-closed.** On a
  post-render failure, Bosun recursively restricts directories to owner-only and
  regular files to owner read/write without following symlinks. It logs the
  staging path and retention outcome, never file contents. If hardening fails,
  Bosun removes the staging tree; if removal also fails, the returned failure is
  joined with the security error and logged as critical without secret material.
  - Alternatives considered: keep the existing `0755`/`0644` modes for
    convenience. Rejected because a retained render can contain plaintext secrets.

- **Decision: one evidence slot per effective target.** A later attempt preserves
  old evidence through sync, config reload, decryption, and deploy-mode resolution.
  Only entry into the next render phase clears that target's slot, after which the
  new render (complete or partial) becomes the current evidence. Successful targets
  remove only their own staging directory; a sibling target's failure evidence is
  untouched.

- **Decision: rollback outcome does not control evidence retention.** A successful
  rollback restores the live managed tree but does not explain why the rejected
  render failed. A failed or partial rollback makes that render even more valuable.
  Both outcomes retain the same secured staging slot.

## Risks / Trade-offs

- A failed render can contain secrets on disk until the next render → owner-only
  hardening, no extra copy, no content logging, and fail-closed deletion on a
  hardening error bound the exposure.
- The next automated retry replaces evidence before an operator inspects it → the
  single-slot policy deliberately favors bounded secret retention and disk usage;
  logs identify the path and replacement event so operators know the window.
- Permission hardening may encounter symlinks or unusual files → use `Lstat`, never
  follow symlinks, and delete the tree if the expected protection cannot be proven.
- Cleanup after verification keeps staging alive longer on successful deployments
  → the added lifetime is bounded by existing health timeouts and hook execution.
- Cleanup failure after a successful verification can leave plaintext → apply the
  same owner-only hardening fallback before reporting success; inability to harden
  or delete becomes an error rather than a silent successful leak.

## Migration Plan

No configuration or state-file migration is required. Existing staging paths remain
valid. On upgrade, any pre-existing staging tree is handled by the next render's
normal clear-before-render behavior. Rollback is code-only: restore cleanup to its
prior position and remove the evidence hardening helper.

## Open Questions

- None blocking. Durable export can be designed later as an explicit operator
  action with encryption and its own retention policy; it is intentionally outside
  this change.
