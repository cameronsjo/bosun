## 1. Specification and inventory

- [x] 1.1 Record all affected consumers and exclusions.
- [x] 1.2 Define trusted-root, standard-swap, IncludeDir, extraction, and lock
      migration semantics.

## 2. Filesystem implementation

- [ ] 2.1 Add root-bound fileutil destination operations and source-open helpers.
- [ ] 2.2 Wire local directory and single-file deploys to pinned roots.
- [ ] 2.3 Bind content-hash skip and byte comparison to the destination root.
- [ ] 2.4 Confine stale-file pruning to the trusted destination root.
- [ ] 2.5 Pin template source/include reads while preserving IncludeDir semantics.
- [ ] 2.6 Pin rollback extraction and reject realized link escapes.

## 3. Lock implementation

- [ ] 3.1 Enforce owner-only Unix lock parent and file permissions.
- [ ] 3.2 Fail closed with actionable path/fix errors; document Windows residual.

## 4. Evidence and consumer docs

- [ ] 4.1 Add deterministic tests and staged-break mutation recipe.
- [ ] 4.2 Update `docs/security.md` and onboard GitOps/configuration resources.
- [ ] 4.3 Run formatting, OpenSpec strict validation, and gated checks where the
      resource floor permits; record blocked exit codes precisely.
- [ ] 4.4 Commit explicit task paths with the required session trailers.
