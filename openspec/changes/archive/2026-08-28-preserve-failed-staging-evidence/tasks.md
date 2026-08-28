## 1. Staging lifecycle

- [x] 1.1 Move normal staging cleanup after the configured health gate, post-sync hooks, and
  post-deploy verification, while keeping it before successful completion is
  reported
- [x] 1.2 Preserve staging after every failure that occurs after rendering begins,
  including partial render, backup/rollback-anchor failure, state-persistence
  failure, deploy/invariant/compose failure, health failure with rollback success
  or failure, and local post-deploy verification failure
- [x] 1.3 Install one per-target lifecycle finalizer when render preparation begins
  so every later error and panic hardens-or-deletes staging; re-raise panics after
  recording the security outcome
- [x] 1.4 Keep the clear-before-render replacement point so each effective target
  has at most one staging evidence slot; if replacement cannot securely clear and
  recreate the slot, abort before writing decrypted output

## 2. Secret-safe retention

- [x] 2.1 Create the effective staging root with mode `0700` before decrypted output
  is written, keep payload temp files private until atomic rename, and preserve
  existing descendant and destination file-mode semantics while the pipeline runs
- [x] 2.2 Add a confined, no-follow staging verifier/hardener that requires the root
  and descendants to be real directories or regular files, detects symlinks,
  irregular entries, and inspection-to-mutation replacement races, hardens retained
  regular files to `0600`, and never logs contents or link targets
- [x] 2.3 On preparation, verification, or hardening failure, delete only the
  effective staging tree without following links; if deletion also fails,
  join the security failure into the reconcile error and log it without sensitive
  material
- [x] 2.4 Apply the same harden-or-delete fallback when normal cleanup fails after
  successful verification

## 3. Target isolation and repeated runs

- [x] 3.1 Canonicalize all effective staging paths and reject the entire target set
  before execution when paths are equal or have an ancestor/descendant overlap;
  cover implicit-default roots, named-derived children, explicit overrides, and
  symlink-resolved existing ancestors
- [x] 3.2 Exercise valid implicit-default, named-derived, and explicit staging paths
  so cleanup and retention affect only the current target's effective `StagingDir`
- [x] 3.3 Before the first post-upgrade reconciliation and idempotently before later
  cycles, harden-or-delete pre-existing slots before Git sync or secret decryption;
  abort the whole cycle if a slot can be neither protected nor deleted
- [x] 3.4 Verify a later render replaces prior evidence without accumulating
  timestamped directories or modifying sibling target evidence

## 4. Tests

- [x] 4.1 Add full-pipeline tests proving staging is removed only after a verified
  success and retained after configured-health-gate failure
- [x] 4.2 Cover successful rollback, failed/partial rollback, post-deploy verification
  failure, backup/rollback-anchor failure, pre-deploy state-write failure, compose
  failure, cleanup failure, permission-hardening failure, and harden-plus-delete
  failure
- [x] 4.3 Verify the staging root stays `0700` from creation through deploy
  verification, retained descendants become `0700`/`0600`, destination modes do
  not regress, and payload temp files are never broadly readable; cover partial
  render, dry run, panic finalization, root and descendant symlinks, irregular
  entries, traversal attempts, entry-replacement races, and deterministic
  replacement on the next render
- [x] 4.4 Add multi-target CLI/daemon coverage showing one target may retain evidence
  while a successful sibling cleans only its own staging directory when the shared
  cycle context remains live, and that cancellation or deadline expiry leaves later
  siblings untouched; verify equal and nested slots reject the whole target set
  before any target runs
- [x] 4.5 Verify structured logs distinguish retained, discarded, replaced, and
  cleanup-fallback outcomes with target identity and path, but contain no rendered
  secret content or symlink target
- [x] 4.6 Cover successful verification followed by cleanup failure: hardening
  success records deployment success with a warning, while harden-plus-delete
  failure does not record success; also verify non-fatal hook errors still clean up
- [x] 4.7 Preseed secured evidence, inject sync, config-reload, SOPS-decryption, and
  deploy-mode-resolution failures, and prove content and modes of that slot and all
  sibling slots remain unchanged
- [x] 4.8 Run focused repeated and race tests, relevant full package tests, `go vet
  ./...`, `golangci-lint run --new-from-rev=origin/main`, `go build ./...`, strict
  OpenSpec validation, and `git diff --check`

## 5. Documentation

- [x] 5.1 Update `docs/gitops.md` with the configured-health-gate and
  post-verification cleanup order, evidence
  path, private active-staging and retained `0700`/`0600` security boundaries,
  single-slot replacement behavior, dry-run behavior, and cleanup-fallback warning
- [x] 5.2 Update `skills/onboard/resources/gitops.md` with the same lifecycle and
  cleanup order, evidence path, access boundary, replacement, cleanup-fallback,
  and failure/rollback semantics
- [x] 5.3 Update staging-path descriptions in
  `skills/onboard/resources/configuration.md` to document disjoint effective paths
  and warn that failed renders can contain plaintext secrets and are retained only
  in the effective per-target slot
