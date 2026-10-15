# Filesystem confinement and reconcile lock hardening

## Scope

Implement the approved filesystem unit of bosun #668 on a fresh `origin/main`
branch. The unit covers destination writes and comparisons, stale-file pruning,
template source and include reads, local rollback extraction, and reconcile lock
ownership. It excludes #669 emergency/restore sites, ingress authorization, SSH
trust, restart-breaker behavior, and the missing #671 racing/differential
harnesses unless a small deterministic harness can be added without changing
the security boundary.

## Decisions

- Unix lock setup fails closed when the configured lock path or its trusted
  parent cannot be proven owner-only. Errors name the path and the remediation.
- Windows retains its existing `LockFileEx` behavior. The residual reparse/ACL
  guarantee is documented for the consumer and PR report.
- Destination mutations and content comparisons use one trusted application-data
  root handle where the operation has a root. Standard atomic-swap mode keeps
  its trusted sibling temporary directory and trusted parent rename semantics;
  it is not described as universally handle-pinned.
- Template source, `include`, and `fromJsonFile` reads pin the source root and
  use descriptors from that root for validation and content reads. IncludeDir
  remains the configured allowlist and its current relative/absolute resolution.
- Stale-file pruning resolves all managed removals through the same trusted
  destination root used for the copy.
- Rollback extraction writes beneath a fresh pinned extraction root and rejects
  realized escapes through chained links.

## Consumers

- `internal/fileutil`: `CopyFile`, `CopyDir`, `CopyDirIfChanged`, content hash
  comparison, destination root helpers, and source-open helpers.
- `internal/reconcile/deploy.go`: local directory and single-file deployment,
  stale-file pruning, target directory creation, and both content-hash and
  standard copy paths.
- `internal/reconcile/template.go`: template source parsing, non-template copy,
  `include`, `fromJsonFile`, and `RenderDirectory` IncludeDir resolution.
- `internal/reconcile/remote_rollback.go`: local extraction used by remote
  rollback and its tar link handling.
- `internal/reconcile/reconcile.go` and `internal/reconcile/lock_*.go`:
  lock-directory creation, lock-file opening, owner-only Unix permissions, and
  Windows compatibility behavior.
- `skills/onboard/resources/gitops.md`, `configuration.md`, and
  `docs/security.md`: operator-facing confinement and lock behavior.

## Verification

- Run `gofmt`, focused static checks, OpenSpec strict validation, and the
  deterministic unit tests that fit the local environment.
- Attempt compiler-heavy checks only through `scripts/agent-go-gate.sh`; the
  current host has 57 GiB free against the mandatory 100 GiB floor, so a gate
  exit 75 is an environment limit and must not be bypassed.
- Provide a deterministic mutation recipe under
  `/private/tmp/cos-bosun668-assessment` that removes each control and observes
  a behavioral test failure without introducing compile failures.

## Status

- [x] Proposal, design, spec deltas, and tasks authored and strictly validated.
- [x] Source and tests ported from the approved hardening head.
- [x] Remaining template and destination comparison races closed.
- [x] Stale pruning uses the trusted destination root.
- [x] Consumer documentation updated.
- [ ] Focused tests and staged-break evidence recorded.
- [ ] Local commit created with explicit paths; root agent independently reviews.
