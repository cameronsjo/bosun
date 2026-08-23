## 1. Staging lifecycle

- [ ] 1.1 Move normal staging cleanup after the health gate, post-sync hooks, and
  post-deploy verification, while keeping it before successful completion is
  reported
- [ ] 1.2 Preserve staging after every failure that occurs after rendering begins,
  including health failure with rollback success, rollback failure, and local
  post-deploy verification failure
- [ ] 1.3 Keep the existing clear-before-render replacement point so each effective
  target has at most one staging evidence slot

## 2. Secret-safe retention

- [ ] 2.1 Add a symlink-safe retained-staging hardening helper that uses `Lstat`,
  restricts directories to owner-only and regular files to owner read/write, and
  never logs contents
- [ ] 2.2 On hardening failure, delete the staging tree; if deletion also fails,
  join the security failure into the reconcile error and log it without sensitive
  material
- [ ] 2.3 Apply the same harden-or-delete fallback when normal cleanup fails after
  successful verification

## 3. Target isolation and repeated runs

- [ ] 3.1 Exercise implicit-default, named-derived, and explicit staging paths so
  cleanup and retention affect only the current target's effective `StagingDir`
- [ ] 3.2 Verify a later render replaces prior evidence without accumulating
  timestamped directories or modifying sibling target evidence

## 4. Tests

- [ ] 4.1 Add full-pipeline tests proving staging is removed only after a verified
  success and retained after health-gate failure
- [ ] 4.2 Cover successful rollback, failed/partial rollback, post-deploy verification
  failure, cleanup failure, permission-hardening failure, and harden-plus-delete
  failure
- [ ] 4.3 Verify retained file and directory modes, symlink non-traversal, no secret
  content in logs, and deterministic replacement on the next render
- [ ] 4.4 Add multi-target CLI/daemon coverage showing one target may retain evidence
  while a successful sibling cleans only its own staging directory
- [ ] 4.5 Run focused repeated and race tests, relevant full package tests, `go vet
  ./...`, `golangci-lint run --new-from-rev=origin/main`, `go build ./...`, strict
  OpenSpec validation, and `git diff --check`

## 5. Documentation

- [ ] 5.1 Update `docs/gitops.md` with the post-verification cleanup order, evidence
  path, owner-only security boundary, and single-slot replacement behavior
- [ ] 5.2 Update `skills/onboard/resources/gitops.md` with the same lifecycle and
  failure/rollback semantics
- [ ] 5.3 Update staging-path descriptions in
  `skills/onboard/resources/configuration.md` to warn that failed renders can contain
  plaintext secrets and are retained only in the effective per-target slot
