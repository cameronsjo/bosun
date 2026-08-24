## 1. Directory change-set production

- [x] 1.1 Extend `CopyDirIfChanged` to record each descendant directory created by that call, including empty directories, while excluding the deploy root, its missing ancestors, and pre-existing directories.
- [x] 1.2 Preserve collision behavior when a destination path exists as a non-directory, and handle a concurrent directory creator without reporting a false change.
- [x] 1.3 Add table-driven fileutil tests for nested and empty directory creation, pre-existing directories, repeat no-ops, non-directory collisions, and directory inspection failures.

## 2. Local deploy aggregation and hooks

- [x] 2.1 Expand `DeployResult.WrittenFiles`, `AddWritten`, and `PrefixLatest` semantics from regular-file writes to created-or-written paths while retaining canonical staging-relative path safety.
- [x] 2.2 Propagate created descendant directories through every discovered local content-hash directory target and prefix them with the target's canonical staging-relative path.
- [x] 2.3 Propagate created descendant directories through the separately deployed compose target, prefix them with `compose`, and add an explicit compose-directory hook regression test.
- [x] 2.4 Report a top-level managed file-to-directory transition as the target path, report its created descendants, and keep ordinary deploy-root creation excluded as plumbing.
- [x] 2.5 Add discovered-appdata deploy and hook tests for an empty-directory-only change, nested directory creation, target prefixing, top-level and nested file-to-directory transitions, repeat no-ops, and matching/non-matching post-sync hook patterns.
- [x] 2.6 Preserve and test authoritative path evidence for successful local content-hash results, including empty no-op and deletion-only results; consumers must use those complete path sets without fallback.
- [x] 2.7 Preserve and test standard-copy local direct evidence: non-empty `WrittenFiles` or `DeletedFiles` are evaluated without git-diff fallback.
- [x] 2.8 Preserve and test standard-copy local fallback: only when both `WrittenFiles` and `DeletedFiles` are empty, hooks use git diff normalized to canonical staging-relative paths.
- [x] 2.9 Preserve and test remote behavior: every configured hook remains eligible to run unconditionally regardless of either path slice's length.

## 3. Deploy invariant integration

- [x] 3.1 Make stage 9 verify each reported path exists, has the same directory-or-regular-file type as its source entry, and has an mtime no older than the reconcile start time.
- [x] 3.2 When no regular file was written, run the existing source-to-destination content check even if newly created directories are present in `WrittenFiles`.
- [x] 3.3 Add invariant tests for missing directories, wrong regular-file and directory destination types (including symlinks), stale directory mtimes, directory-only sources, and directory-only change sets with missing or byte-different regular files.
- [x] 3.4 Add a regression that keeps `DeployState.DeployedFiles` file-only and documents that persisted empty-directory ownership for a later directory-to-file transition remains separate work.

## 4. Documentation

- [x] 4.1 Update `docs/adr/0013-deploy-sync-invariant-gates.md`, `docs/gitops.md`, `docs/error-handling.md`, and `docs/troubleshooting.md` from file-only `WrittenFiles` language to the created/written path contract.
- [x] 4.2 **Required implementation merge gate:** update `skills/onboard/resources/gitops.md` to describe the created/written deploy tracking contract and updated stage-9 invariant behavior; update any configuration reference that describes `BOSUN_SKIP_DEPLOY_INVARIANT`. Implementation PR #551 MUST NOT merge while this resource remains outdated.
- [x] 4.3 Confirm `llms.txt`, command docs, configuration schema docs, and manifest docs remain accurate; update only those whose capability claims changed.

## 5. Verification

- [x] 5.1 Run focused tests repeatedly and under `-race`, including fileutil, discovered-target and compose aggregation, hooks, managed transitions, deploy invariants, authoritative no-op and deletion-only results, standard-copy direct and normalized-diff branches, and remote all-hooks behavior.
- [x] 5.2 Run repository tests, coverage, vet, lint, supported cross-platform builds, and strict OpenSpec validation; cover new error paths so the patch coverage gate remains green.
