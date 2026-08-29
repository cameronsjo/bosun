# Multi-target R1 validation plan

## Goal

Complete issue #438's R1 slice by rejecting an invalid effective target set before any target can reconcile, and by confining explicit target-owned paths to their permitted roots. Preserve single-default compatibility and apply one validation path to YAML and `BOSUN_TARGETS`.

## Chosen approach

Keep structural validation in `internal/reconcile/target.go`, immediately before effective targets are returned. Validate every configured descriptor first, then derive effective configurations and validate confinement and resource collisions across the complete set. This makes every current consumer fail closed without duplicating admission rules in commands or the daemon.

Path checks use cleaned absolute paths, existing-symlink resolution, and `filepath.Rel` containment rather than string prefixes. Explicit named-target state, staging, and local appdata/deploy overrides are checked against the base state directory, staging root, and local deploy root respectively.

## Boundaries

- This slice does not add scalar presence-aware decoding; omission-versus-explicit-empty semantics remain R2.
- This slice does not change alerts, structured log attribution, daemon status, or drift behavior.
- Existing collision checks and implicit/lone-default compatibility remain authoritative.

## Checklist

- [x] Reconcile the approved OpenSpec requirements, current target resolver, and issue #438 R1 scope.
- [x] Add mutation-sensitive tests for empty, unsafe, and case-insensitive duplicate descriptors that prove the entire set is rejected.
- [x] Add state, staging, and local-path confinement tests for direct, YAML, and `BOSUN_TARGETS` inputs.
- [x] Implement complete-set descriptor validation and root-confinement checks in the shared resolver.
- [x] Update the OpenSpec task ledger and onboard configuration documentation.
- [ ] Run focused tests, full tests, race tests, vet, lint, build, coverage, and strict OpenSpec validation through the resource gate where applicable.
- [ ] Run the polish review, fix findings, open the PR, verify hosted checks, merge, and remove the temporary worktree.
