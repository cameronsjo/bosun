# ADR-0003: Dagger for Bosun Pipelines

## Status

Deferred

## Context

The bosun component orchestrates deployments via a reconciliation loop:

```
git pull → sops decrypt → Go template render → docker compose up
```

Currently implemented in Go as part of the bosun binary. Question: should we use [Dagger](https://dagger.io) for type-safe, containerized pipelines?

## Decision

**Defer Dagger adoption.** Keep shell scripts for v1. Revisit if complexity grows.

## Rationale

### Current State Works

The bosun binary handles the entire pipeline natively in Go:
- Git operations (clone/pull)
- SOPS decryption
- Go template rendering with Sprig functions
- Docker Compose orchestration

Benefits:
- Single binary, no external dependencies
- Type-safe Go code
- Easy to debug and test
- Sufficient for linear pipelines

### When Dagger Makes Sense

- Complex dependency graphs between steps
- Parallel execution with caching
- Need local/CI parity
- Type-safe pipeline logic (Go/Python/TS SDK)
- Multiple output targets with shared intermediate steps

### Current Complexity Doesn't Justify It

| Factor | Current | Dagger Threshold |
|--------|---------|------------------|
| Steps | 4 linear | 10+ with branches |
| Parallelism | None needed | Multiple independent paths |
| Caching | Git handles it | Heavy build artifacts |
| Contributors | 1-2 | Team needing guardrails |

### Costs of Adoption

- Dagger engine dependency (~100MB)
- Container image bloat
- Learning curve
- More failure modes
- "Resume-driven development" smell

## Future Trigger

Revisit this decision if:

1. Manifest renderer generates multiple output targets needing parallel rendering
2. Pipeline exceeds 10 steps with branching logic
3. Caching becomes critical (large template sets)
4. Team grows and needs type-safe guardrails

## Alternative Path

If adopted later, maintain interface compatibility:

```bash
# Shell (default)
./bosun.sh reconcile

# Dagger (opt-in)
dagger call reconcile --source=.
```

## References

- [Dagger Documentation](https://docs.dagger.io)
- [Dagger vs Shell Scripts](https://docs.dagger.io/zenith/faq#why-not-just-use-shell-scripts)
