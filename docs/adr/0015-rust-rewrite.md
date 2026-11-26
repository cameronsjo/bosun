# ADR-0015: Rewrite bosun in Rust

## Status

Proposed

Revisits the "Rust: overkill for this use case" row in
[ADR-0010](0010-go-rewrite.md). ADR-0010 stays in force until this ADR is
accepted and the cutover in the plan completes.

## Context

bosun is a ~51k-line Go binary (plus ~98k lines of tests). The Go choice in
ADR-0010 rested on Go-native libraries: the Docker SDK, `go-git`, the SOPS
library, and `text/template` + Sprig. Those libraries are still the reason a
Rust port is hard.

A Rust rewrite is being considered. The plan is in
[docs/plans/2026-11-26-rust-rewrite.md](../plans/2026-11-26-rust-rewrite.md).

## Decision

Proposed: rewrite the `bosun` binary in Rust, gated on three spikes.

1. A Go-compatible template engine renders every known template
   byte-identically.
2. A Rust SOPS decryptor matches the real `sops` binary on all four formats
   and fails closed the same way.
3. `gix` plus system `ssh` passes the existing Git trust scenarios.

If any spike fails its bar, this ADR goes back for a new decision. It is not
ported around.

The Go binary keeps shipping until the Rust binary passes all OpenSpec
scenarios and a two-week shadow run. User-facing contracts do not change:
CLI, config, env vars, template syntax, state files, API, and release asset
names.

## Consequences

### Pros

- No GC. Lower, steadier memory for a long-running daemon on a home server.
- The type system can enforce rules that are only comments and tests today,
  such as "no lock held across I/O" and "dry run never persists".
- Static musl binary with no C dependencies, if the spikes go the lean way.

### Cons

- 12–16 weeks of calendar time, by a low-confidence estimate.
- bosun owns a template engine and a SOPS decryptor that Go got for free.
- `main` keeps moving during the port, so parity is a moving target.
- Go's Docker, Git, and SOPS ecosystems are first-party. Rust's are not.

## Alternatives Considered

| Alternative | Why not |
|-------------|---------|
| Stay on Go | Valid. Pick this if the spikes fail or the goal in the plan's open question 1 does not justify the cost |
| Port only the daemon to Rust | Two languages and two binaries. Breaks "single binary" |
| Switch templates to `minijinja` | Breaks every user repo and ADR-0011 (Helm alignment) |

## References

- [Rust rewrite plan](../plans/2026-11-26-rust-rewrite.md)
- [ADR-0010: Go rewrite](0010-go-rewrite.md)
- [ADR-0011: Helm alignment](0011-helm-alignment.md)
