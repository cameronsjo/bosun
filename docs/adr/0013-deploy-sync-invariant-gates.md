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
| 9 (post-sync) | Per-target written files exist with fresh mtime | `ErrDeployInvariantEmptyWrite` / `ErrDeployInvariantStaleMtime` / `ErrDeployInvariantMissingFile` | `BOSUN_SKIP_DEPLOY_INVARIANT` |

Future gates SHOULD follow this template:

- Sentinel error names prefixed with `ErrDeployInvariant`, `ErrRenderInvariant`, `ErrBackupInvariant`, etc., scoping to the pipeline stage they protect
- Gate functions live in `internal/reconcile/verify.go` (or a sibling file if the file grows beyond one logical concern)
- Tests cover: each sentinel triggers the right error, the override bypasses (when applicable), boundary conditions where strict equality matters
- Per-file `Debug` log at gate entry so operators tracing a deploy see the gate firing

## Consequences

### Pros

- **Silent-success failure modes become structurally impossible** for the specific failure shape each gate covers. The pipeline cannot reach a downstream stage if the gate fires.
- **Defense in depth** — each gate is independent of upstream observability. Even if a future change breaks logging or alerting, gates still fail-fast.
- **Operators get a clear escape hatch** for edge cases (empty repo, diagnostic deploys) without weakening the strict default for everyone else.
- **Pattern is recognizable** — once a gate lands at one stage, adding another at a different stage is mechanical: pick the invariant, name the sentinel, wire the env var.
- **Tests are local** — a gate function is pure modulo filesystem calls, so unit tests cover the contract directly without standing up the full pipeline.

### Cons

- **Each gate adds latency** to the hot path. Mitigated by keeping gates O(written files) rather than O(staging tree); `dirHasRegularFiles` uses `fs.SkipAll` for early exit.
- **Override env vars are a risk surface** (see [security.md → Operator Escape Hatches](../security.md)). Mitigated by `override=true` logging on every invocation so monitoring catches accidental persistence.
- **The pattern doesn't catch "wrong work" failures** — only "absence of work." A gate that asserts "files were written" won't catch "files were written, but the content is wrong." Different failure modes need different gates or different layers (e.g., post-deploy health checks).
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
- [PR #227 — spec: add deploy-sync invariants and per-file write observability](https://github.com/cameronsjo/bosun/pull/227)
- `openspec/changes/add-deploy-sync-invariants/specs/reconcile/spec.md` — formal spec deltas
- `docs/troubleshooting.md` → "Deploy reports success but files unchanged" — operator-facing diagnosis
- `docs/gitops.md` → pipeline stage table — current gate placement
- `docs/error-handling.md` → Deploy-Sync Invariant Sentinels — sentinel reference
- ADR-0001 (Manifest System) — the upstream stage these gates protect
