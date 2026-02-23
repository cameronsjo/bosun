# Change: Content-Hash File Sync

## Why

Bosun rewrites every rendered config file on every deploy, even when content hasn't changed. On Unraid's FUSE filesystem, unnecessary writes invalidate file handles, causing services like Traefik to read stale configs until their containers are restarted. Additionally, post-sync hooks currently use `git diff` to determine "changed files," which measures git-level changes — not actual on-disk changes. This means hooks can fire for files whose rendered output hasn't changed (e.g., a git change touches a template variable that doesn't affect a particular output file).

## What Changes

- Add `fileutil.CopyFileIfChanged()` — compares SHA-256 hash of source content against existing destination before writing. Skips write when content matches.
- Add `fileutil.CopyDirIfChanged()` — directory variant that returns the list of files actually written.
- Wire `DeployLocal` and `DeployLocalFile` to use content-hash-aware variants via a config flag.
- Track which files were actually written to disk during deploy.
- Pass actually-written file list (instead of git diff) to post-sync hook evaluation.
- Add `BOSUN_CONTENT_HASH_SYNC` env var to opt into behavior (default: `true`).

## Impact

- Affected specs: `reconcile`
- Affected code: `internal/fileutil/fileutil.go`, `internal/reconcile/deploy.go`, `internal/reconcile/reconcile.go`, `internal/reconcile/hooks.go`
- All consumers:
  - `fileutil.CopyFile` — called by `CopyDir`, `DeployLocalFile`, and tests
  - `fileutil.CopyDir` — called by `DeployLocal`
  - `DeployLocal` — called by `deployLocal()` in reconcile.go (6 call sites)
  - `DeployLocalFile` — called by `deployLocal()` in reconcile.go (4 call sites)
  - `EvaluatePostSyncHooks` — called by `executePostSyncHooks()` in reconcile.go
  - `executePostSyncHooks` — called from `reconcile()` pipeline
  - Remote deploy functions (`DeployRemote`, `DeployRemoteFile`) — NOT affected (SSH+tar has different atomicity model; content-hash requires reading remote files which adds latency and complexity)
