# ADR-0013: Fail-Loud Invariant Gates in the Reconcile Pipeline

## Status

Proposed

## Context

The original GitOps reconcile pipeline followed an optimistic-success model: each stage logged its own failures, but pipeline-level outcomes were determined by whether commands returned zero. This produced the failure mode documented in [GH#214](https://github.com/cameronsjo/bosun/issues/214):

1. Render produced no parseable services (mis-pathed staging directory)
2. `ExtractDeclaredState` returned `nil, nil` and the pipeline logged `declared_services: 0` at `Info` level
3. `CopyDirIfChanged` hashed staging files against stale destination content and recorded zero writes
4. `docker compose up` exited 0 because the on-disk compose files hadn't changed
5. Every diagnostic surface reported success; only host-side `mtime` inspection revealed the deploy was a no-op

The class of bug is "the failure is the absence of work" — there is no error to surface because every component independently succeeded. Existing observability didn't help because the observable signals were normal-looking.

A general pattern exists for this: **place explicit invariant checks between pipeline stages, with default-strict behavior and named escape hatches**. The pattern is well-established in formal methods (Hoare logic, Eiffel contracts) and in defense-in-depth systems (Kubernetes admission webhooks, AWS Config Rules). We are formalizing it for Bosun's reconcile pipeline so future "absence of work" failures can be caught the same way.

## Decision

The reconcile pipeline SHALL support **named invariant gates** between stages that perform substantive work. Each gate is a function that:

1. **Has a fixed contract** — pre-conditions on what previous stages must have produced, post-conditions the gate enforces before allowing the next stage
2. **Fails loud by default** — returns a sentinel error (`Err<Invariant><FailureMode>`) that the pipeline wraps and surfaces; no silent skips
3. **Documents its escape hatch** — every overridable invariant gets one and only one `BOSUN_<NAME>=true` env var; the override is logged at `Warn` level with `override=true` on every invocation so it shows up in monitoring
4. **Distinguishes "configuration error" from "intentional state"** — gates that catch misconfigurations (e.g., missing staging compose dir) are NOT overridable; gates that catch legitimate-but-rare cases (empty repo, diagnostic deploys) ARE overridable

The first two gates land in PR #227:

| Stage | Gate | Sentinel(s) | Override env var |
|-------|------|-------------|------------------|
| 6 (post-render) | Declared-state must be non-zero | `ErrComposeDirMissing` (fatal) / `ErrNoDeclaredServices` (overridable) | `BOSUN_ALLOW_EMPTY_DECLARED_STATE` |
| 9 (post-sync) | Per-target created/written paths exist with the expected type and fresh mtime; no-file-write targets content-match their source | `ErrDeployInvariantEmptyWrite` / `ErrDeployInvariantStaleMtime` / `ErrDeployInvariantMissingFile` / `ErrDeployInvariantWrongType` | `BOSUN_SKIP_DEPLOY_INVARIANT` |

Future gates SHOULD follow this template:

- Sentinel error names prefixed with `ErrDeployInvariant`, `ErrRenderInvariant`, `ErrBackupInvariant`, etc., scoping to the pipeline stage they protect
- Gate functions live in `internal/reconcile/verify.go` (or a sibling file if the file grows beyond one logical concern)
- Tests cover: each sentinel triggers the right error, the override bypasses (when applicable), boundary conditions where strict equality matters
- Per-file `Debug` log at gate entry so operators tracing a deploy see the gate firing

### Amendment — GH#330 (empty-write gate refinement)

The `ErrDeployInvariantEmptyWrite` gate originally treated **any** zero-write deploy target with a non-empty source as a failure, on the assumption that "source has files but nothing was written" is the silent-sync signature. This was too aggressive: with content-hash sync, a target legitimately records zero writes when the destination already byte-matches the source. [GH#330](https://github.com/cameronsjo/bosun/issues/330) is the resulting outage — a single byte-identical config (`dnscrypt-proxy.toml`) tripped the gate and aborted the entire reconcile before `docker compose up`, taking down 62 containers.

The gate now **inspects the destination** instead of inferring from the write count: on zero writes against a non-empty source it verifies each regular source file exists and is byte-identical at the destination. All equal → legitimate no-op, pass. Any missing or different → genuine silent-sync failure, fire `ErrDeployInvariantEmptyWrite` with the first mismatching path. This preserves the gate's protective value — it still catches "files should be on disk but aren't current" — while removing the false positive. The reframing also strengthens the gate: it asserts the post-condition directly rather than via the `WrittenFiles` proxy.

This is consistent with the original "fail loud, but only on genuine absence of work" intent; the bug was conflating "no write this run" with "no file on disk."

### Amendment — GH#358 (directory-aware change tracking)

`WrittenFiles` now also records descendant directories that content-hash sync
actually creates, including empty directories. The deploy target root and
pre-existing directories remain excluded so plumbing and no-op reconciles do
not trigger hooks. Managed file-to-directory transitions record the transitioned
path and all created descendants as well.

The invariant classifies those entries instead of treating any non-empty slice
as proof that a regular file was written. Every recorded directory must exist as
a real directory with a fresh mtime. When the slice is empty or contains only
directories, the GH#330 content comparison still runs for every regular source
file. This prevents creation of an empty directory from masking a missing or
stale config file.

## Consequences

### Pros

- **Silent-success failure modes become structurally impossible** for the specific failure shape each gate covers. The pipeline cannot reach a downstream stage if the gate fires.
- **Defense in depth** — each gate is independent of upstream observability. Even if a future change breaks logging or alerting, gates still fail-fast.
- **Operators get a clear escape hatch** for edge cases (empty repo, diagnostic deploys) without weakening the strict default for everyone else.
- **Pattern is recognizable** — once a gate lands at one stage, adding another at a different stage is mechanical: pick the invariant, name the sentinel, wire the env var.
- **Tests are local** — a gate function is pure modulo filesystem calls, so unit tests cover the contract directly without standing up the full pipeline.

### Cons

- **Each gate adds latency** to the hot path. Created/written path checks are O(changed paths); a deploy with no regular-file writes compares the regular source tree to distinguish a legitimate content-hash no-op from silent sync failure.
- **Override env vars are a risk surface** (see [security.md → Operator Escape Hatches](../security.md)). Mitigated by `override=true` logging on every invocation so monitoring catches accidental persistence.
- **The pattern does not prove all written file content** — the no-regular-file-write branch checks source/destination equality, while a run that recorded file writes checks their existence and mtime. Post-write hash verification and post-deploy health checks cover different failure modes.
- **Adding a gate is a breaking change to the pipeline contract** — existing test fixtures that don't satisfy the new pre-conditions will fail. Mitigated by giving each gate an override env var.

## Alternatives Considered

| Alternative | Why not |
|-------------|---------|
| **Validate inputs at each stage's entry instead of adding gates between stages** | Spreads invariant logic across many call sites; harder to add new invariants. Gates centralize the contract. |
| **Use a single end-of-pipeline assertion ("deploy must have changed something")** | Hides which stage failed. A multi-stage pipeline benefits from per-stage gates because the diagnosis lands at the failing stage, not at the post-mortem. |
| **Make gates always-on with no escape hatch** | Genuinely empty repos and diagnostic deploys become impossible. Operator escape hatches are a load-bearing feature for staged rollouts. |
| **Use a generic precondition library (e.g., `pkg/errors`-style contracts)** | Adds a dependency and a vocabulary for a small set of gates. Native sentinel errors + `errors.Is` are sufficient and idiomatic Go. |
| **Push verification to an external admission controller (Kubernetes-style)** | Out of scope for a single-binary GitOps tool. The whole point of Bosun is no out-of-process dependencies on the target. |

## References

- [GH#214 — local deploy sync silent no-op](https://github.com/cameronsjo/bosun/issues/214)
- [GH#358 — empty directory creation omitted from deploy change tracking](https://github.com/cameronsjo/bosun/issues/358)
- [PR #227 — spec: add deploy-sync invariants and per-file write observability](https://github.com/cameronsjo/bosun/pull/227)
- `openspec/changes/add-deploy-sync-invariants/specs/reconcile/spec.md` — formal spec deltas
- `docs/troubleshooting.md` → "Deploy reports success but files unchanged" — operator-facing diagnosis
- `docs/gitops.md` → pipeline stage table — current gate placement
- `docs/error-handling.md` → Deploy-Sync Invariant Sentinels — sentinel reference
- ADR-0001 (Manifest System) — the upstream stage these gates protect
