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

- Unix lock setup traverses the configured parent through handles, accepts only
  trusted root- or daemon-owned ancestors, requires a daemon-owned `0700`
  final directory, and fails closed with a path-specific remediation when
  those properties cannot be proven.
- Windows retains its existing `LockFileEx` behavior. The residual reparse/ACL
  guarantee is documented for the consumer and PR report.
- Destination mutations and content comparisons use one trusted application-data
  root handle where the operation has a root. Standard atomic-swap mode creates
  its sibling temporary tree in the target's existing parent and preserves its
  same-parent rename semantics; it is not described as universally
  handle-pinned.
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
- Attempt compiler-heavy checks only through `scripts/agent-go-gate.sh`; local
  attempts observed 57 GiB free earlier and 42.4 GiB at the latest attempt,
  both below the mandatory 100 GiB floor. Gate exit 75 is an environment limit
  and must not be bypassed.
- Remove controls in explicit, compile-valid proof commits and record the
  resulting behavioral failures in the PR's linked CI evidence.

## Status

- [x] Proposal, design, spec deltas, and tasks authored and strictly validated.
- [x] Source and tests ported from the approved hardening head.
- [x] Remaining template and destination comparison races closed.
- [x] Stale pruning uses the trusted destination root.
- [x] Consumer documentation updated.
- [x] Local Go execution attempted through the mandatory resource gate; exit 75
  below its 100 GiB free-space floor. CI supplies the compiler/race evidence.
- [x] Six filesystem control families plus the pinned-root directory-entry
  regression were assertion-checked against disposable scratch copies without
  introducing compile-invalid mutations.
- [x] Restored head `d478b98` passed full race/build/lint CI38774204534 and the
  unchanged 80% patch-coverage gate (80.31%).
- [x] Local commit created with explicit paths; root agent independently reviews.

## Review and publication

Independent review cleared the source after descriptor-bound staging/cleanup,
fingerprints, standalone render source reads, lock diagnostics, Darwin/Linux
aliases, native Windows rename and compatibility fixes. CI run 38764744519
provided behavioral red evidence for both pinned-root directory-entry tests,
stale-pruning ancestor replacement, the five earlier filesystem regressions,
and the Linux alias regression before restoration. Restored head `d478b98`
passed CI38774204534 and codecov/patch (80.31%). A deterministic fingerprint namespace swap
test is included; the broad concurrent filesystem racing harness in #671
remains deferred. Windows native tests are present but have not run on
Windows; standard atomic swap retains its documented trusted-parent precondition
and Windows locks retain the approved ACL/reparse residual. #669 remains
separate.

## Fix round 3

The orchestrator approved post-extraction resolution of all created symlinks,
including sibling/forward chains, and actionable refusal of symlinked `.tmpl`
entries instead of silent omission. The Unix lock basename is identity-checked
before permission repair. Unreleased/operator/onboard notes describe physical
source/include paths and the lock/template migration, and the security doc names
the shared-appdata in-root residual. Deterministic tests cover both sibling-chain
orders, allowed dangling links, discovered template links and post-discovery
replacement. Fresh red/restored-green evidence is recorded in the PR and report;
the local resource gate remains mandatory.

Independent security review additionally identified a hardlink alias that keeps
an unsafe symlink inode after the original name is replaced. The final resolver
checks every created link path, including hardlinks, and the regression models
that exact replacement sequence. CI38777697413 at the first deliberate negative
head compiled and failed the sibling-chain, template-refusal and
identity-before-Chmod assertions. The alias-only negative is a separate proof
with the ordinary sibling-chain and other fix3 controls restored. The final
restoration retains all regressions and updates the older template test that
previously expected a silent skip to require the same actionable refusal.
