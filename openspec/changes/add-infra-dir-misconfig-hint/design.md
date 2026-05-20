# Design: Infra Directory Misconfiguration Hint

## Context

`ExtractDeclaredState(stagingDir)` (`internal/reconcile/drift.go`) stats
`filepath.Join(stagingDir, "compose")` and returns `ErrComposeDirMissing` when
it does not exist. `stagingDir` here is `StagingDir/InfraSubDir` — the rendered
mirror of the repo's infra directory. The function does not know `InfraSubDir`
itself; it only receives the already-joined path.

The diagnostic value we want — "did you mean `BOSUN_INFRA_DIR=unraid`?" — needs
two pieces of information that live in different places:

1. **Which sibling contains `compose/`** — discoverable by scanning the children
   of `stagingDir`. `ExtractDeclaredState` has `stagingDir`, so it can scan.
2. **The `BOSUN_INFRA_DIR` value to suggest** — `filepath.Join(InfraSubDir,
   candidate)`. Only the stage-6 call site in `reconcile.go` knows the current
   `InfraSubDir`.

## Decision

**Split the responsibility along the information boundary.**

- A small helper in `drift.go` — `findComposeCandidates(dir string) []string` —
  reads the immediate children of `dir` and returns the names of those that
  contain a `compose/` subdirectory. Pure modulo filesystem; trivially testable.
- `ExtractDeclaredState` calls the helper when `composeDir` is absent and wraps
  the candidate names into the `ErrComposeDirMissing` error message (relative to
  the infra dir — e.g. `unraid`). This keeps the function self-contained and its
  error useful even for callers that don't post-process it.
- The stage-6 call site in `reconcile.go` — which holds `InfraSubDir` — is
  responsible for the fully-qualified suggestion (`BOSUN_INFRA_DIR=<join>`) when
  it surfaces/logs the error. The base error names the candidate; the call site
  upgrades it to a copy-pasteable env assignment.

### Why scan in `ExtractDeclaredState` rather than only at the call site

The error originates in `drift.go`. Tests for `ExtractDeclaredState` already
assert on the error; adding the candidate name there keeps the contract local
and the unit test simple (no need to stand up a `Reconciler`). The call site
adds the `InfraSubDir` prefix for the operator-facing form but does not need to
re-scan.

### Suggestion format

- **Zero candidates** (`compose/` truly absent everywhere): keep the existing
  bare `ErrComposeDirMissing: <path>` message. No misleading suggestion.
- **One candidate** (`unraid`): `... did you mean BOSUN_INFRA_DIR=unraid?`
- **Multiple candidates** (`unraid`, `staging`): list them —
  `... candidate infra dirs with compose/: [unraid staging]; set BOSUN_INFRA_DIR
  to one of them`.

## Edge Cases

| Case | Behavior |
|---|---|
| `stagingDir` itself unreadable | Existing `os.Stat`/`ReadDir` error path; no scan. Don't mask I/O errors with a hint. |
| Candidate dir has `compose/` but it's a file, not a dir | Not a candidate — require `compose/` to be a directory (matches what `ExtractDeclaredState` would glob). |
| Nested deeper than one level (`a/b/compose`) | Out of scope. One-level scan covers the overwhelming common case (the GH#214 shape). A deeper scan risks O(tree) cost on a failing path and ambiguous suggestions. |
| Hidden dirs (`.beads`, `.git`) contain `compose/` | Skip dot-prefixed children — they are never legitimate infra roots and would produce noisy suggestions. |
| Current `InfraSubDir="."`, candidate `unraid` | Suggested value is `unraid` (`filepath.Join(".", "unraid")` → `unraid`). |
| Current `InfraSubDir="foo"`, candidate `bar` | Suggested value is `foo/bar`. |

## Performance

The scan runs only on the already-failing `ErrComposeDirMissing` path — never on
the happy path. It reads one directory level and stats one subdirectory per
child. Cost is O(children of infra dir), bounded and off the hot path.

## Testing Strategy

- `findComposeCandidates` unit tests: zero / one / multiple candidates, dot-dir
  exclusion, `compose`-is-a-file rejection, unreadable dir.
- `ExtractDeclaredState` test: missing `compose/` with a sibling holding
  `compose/` → error names the candidate.
- Call-site test (or table extension): the surfaced error contains
  `BOSUN_INFRA_DIR=<join>` for representative `InfraSubDir` values.
