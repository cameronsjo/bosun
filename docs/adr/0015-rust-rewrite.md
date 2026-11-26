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

A Rust rewrite is being considered. Stated plainly, the main reason is
that Cameron wants to standardize on Rust as the long-term language. The
other stated reasons are weaker than they sound:

- **Safety:** Go is already memory-safe. Rust adds compile-time data-race
  freedom, which matters for the daemon's concurrency rules, but bosun would
  take on parser code (templates, YAML emitting) that Go got for free.
- **Footprint:** unproven. The Go baseline is a 57.2 MiB binary and a
  58.2 MiB compressed image, most of which is `docker-cli` and friends.
  The plan measures a Rust skeleton against targets in phase 0.
- **Interop:** none for a standalone binary, unless another Rust project
  imports bosun crates. None is named.

The plan is in
[docs/plans/2026-11-26-rust-rewrite.md](../plans/2026-11-26-rust-rewrite.md).

## Decision

Proposed: rewrite the `bosun` binary in Rust, gated on four phase-0
checks.

1. A Rust daemon skeleton with the real dependency set meets the footprint
   targets in the plan.
2. After the template function allowlist is trimmed on Go, a pure-Rust,
   Go-compatible engine renders every known template byte-identically.
3. SOPS decryption, through a pinned `sops` binary or an owned decryptor,
   matches the real `sops` on all four formats for age keys and fails
   closed the same way.
4. `gix` or the `git` CLI, plus system `ssh`, passes the existing Git trust
   and redirect scenarios.

If any check fails its bar, this ADR goes back for a new decision. It is
not ported around.

If committed, the port is all Rust: no Go code bosun owns survives
cutover. External tools (`docker compose`, `ssh`, and possibly `sops` and
`git`) stay allowed. Three changes land on Go first, in phase 0, because
they pay off even if the rewrite stops: age-only SOPS, a trimmed template
function allowlist, and retiring Dagger for plain GitHub Actions (a
separate ADR supersedes ADR-0003). Windows stays out of scope.

The Go binary keeps shipping until the Rust binary passes all OpenSpec
scenarios and a two-week shadow run. User-facing contracts do not change:
CLI, config, env vars, template syntax, state files, API, and release asset
names.

## Consequences

### Pros

- One language across Cameron's stack, which is the main goal.
- Compile-time data-race freedom. With `std::sync::Mutex` (not
  `tokio::sync::Mutex`), the compiler rejects a spawned future that holds
  the reconcile lock across `.await`, so #793's rule becomes a build error.
- No GC. Possibly lower, steadier memory for a long-running daemon, if the
  phase-0 probe confirms it.
- `#![forbid(unsafe_code)]` in bosun's own crates. Dependencies still use
  `unsafe`; this covers bosun's code only.

### Cons

- 26–33 weeks (about 6–8 months) of calendar time, by a low-confidence
  estimate.
- bosun owns a Go-compatible template engine and a YAML emitter that Go got
  for free, and possibly a SOPS decryptor.
- No useful partial result: a port that stalls mid-way is sunk cost, apart
  from the Go-first changes.
- Trimming the template allowlist breaks any user template that uses a
  dropped function.
- `main` keeps moving during the port, so parity is a moving target.
- Go's Docker, Git, and SOPS ecosystems are first-party. Rust's are not.
- Non-age SOPS key groups (PGP, cloud KMS, Vault) stop working.
- Shelling out to `sops`, if chosen, reverses ADR-0010's "no sops CLI"
  choice and adds a runtime dependency.

## Alternatives Considered

| Alternative | Why not |
|-------------|---------|
| Stay on Go | Valid. Pick this if a phase-0 check fails, or if the preference alone does not justify 6–8 months |
| Port only the daemon to Rust | Two languages and two binaries. Breaks "single binary" |
| Switch templates to `minijinja` | Breaks every user repo and ADR-0011 (Helm alignment) |
| Run Go `text/template` as WASM inside the Rust binary | Keeps Go in the build. Ruled out by "all Rust" |

## References

- [Rust rewrite plan](../plans/2026-11-26-rust-rewrite.md)
- [ADR-0010: Go rewrite](0010-go-rewrite.md)
- [ADR-0003: Dagger](0003-dagger-for-bosun.md)
- [ADR-0011: Helm alignment](0011-helm-alignment.md)
