## 1. File Utilities

- [x] 1.1 Add `fileutil.FileHash` and `fileutil.ContentEqual` — SHA-256 hash comparison
- [x] 1.2 Add `fileutil.CopyFileIfChanged(src, dst string) (changed bool, err error)` — wraps CopyFile with content-hash check
- [x] 1.3 Add `fileutil.CopyDirIfChanged(src, dst string) (changedFiles []string, err error)` — returns relative paths of files actually written
- [x] 1.4 Write tests for FileHash, ContentEqual, CopyFileIfChanged, CopyDirIfChanged

## 2. Deploy Layer

- [x] 2.1 Add `ContentHashSync bool` field to `DeployOps`
- [x] 2.2 Update `DeployLocal` to use `CopyDirIfChanged` when `ContentHashSync` is true, plus `removeStaleFiles` for --delete semantics
- [x] 2.3 Update `DeployLocalFile` to use `CopyFileIfChanged` when `ContentHashSync` is true
- [x] 2.4 Add `DeployResult` struct to capture written files across multiple deploy calls
- [x] 2.5 Write tests for content-hash-aware deploy (6 new test cases)

## 3. Reconciler Wiring

- [x] 3.1 Add `ContentHashSync` to reconcile `Config`, daemon `Config`, and `DefaultConfig()` (default: `true`)
- [x] 3.2 Parse `BOSUN_CONTENT_HASH_SYNC` env var in `ConfigFromEnv()`
- [x] 3.3 Pass `ContentHashSync` to `DeployOps` during reconciler setup
- [x] 3.4 Collect written files from deploy calls in `deployLocal()` (returns `*DeployResult`)
- [x] 3.5 Pass written files to `executePostSyncHooks()` as replacement for git diff paths
- [x] 3.6 Write tests for config defaults and env var parsing (5 new test cases)

## 4. Post-Sync Hook Enhancement

- [x] 4.1 Update `executePostSyncHooks` to accept `*DeployResult` parameter
- [x] 4.2 When written-files provided and non-empty, use those for hook matching instead of git diff
- [x] 4.3 When written-files empty (remote mode or opt-out), fall back to git diff behavior
- [x] 4.4 Behavior tested via integration with existing hook tests (hook matching unchanged)

## 5. Documentation

- [x] 5.1 Add `BOSUN_CONTENT_HASH_SYNC` to AGENTS.md env var table
- [x] 5.2 Update onboard skill resources (`skills/onboard/resources/gitops.md`) with content-hash hook behavior
