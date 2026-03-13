## Context

Bosun's reconcile pipeline writes all rendered configs unconditionally on every deploy cycle. On FUSE-backed filesystems (Unraid's `/mnt/user/appdata`), even identical writes invalidate file handles, causing services to read stale configs. Post-sync hooks exist specifically to restart containers after config changes, but they fire based on git diff — not actual disk changes — leading to unnecessary restarts.

## Goals / Non-Goals

- **Goals:**
  - Skip file writes when content hasn't changed (local deploy only)
  - Make post-sync hooks aware of actually-written files
  - Reduce unnecessary container restarts on FUSE filesystems
  - Maintain atomic write guarantees for files that do change

- **Non-Goals:**
  - Optimize remote (SSH) deploys — reading remote files for comparison adds latency and complexity
  - Change the `--delete` semantics of `DeployLocal` (files removed from source still get removed from target)
  - Make this work for template rendering itself (staging dir is ephemeral and always clean)

## Decisions

- **Hash algorithm: SHA-256** — fast enough for config files (typically <100KB), collision-resistant, and available in Go stdlib. No need for xxhash/crc32 since we're comparing small files and the bottleneck is disk I/O, not hashing.

- **Comparison at fileutil level** — `CopyFileIfChanged` reads the destination file and compares hashes before writing. This keeps the optimization close to the I/O boundary and makes it reusable.

- **`DeployLocal` changes return type** — needs to communicate which files were actually written. Return `[]string` of relative paths. This is a breaking change to the internal API but has no external consumers.

- **Opt-in via env var, default true** — `BOSUN_CONTENT_HASH_SYNC=true` is the default. Users can set `false` to restore unconditional writes if hashing causes issues. Since this is a pure optimization with no behavioral change for changed files, defaulting to `true` is safe.

- **Post-sync hooks: written-files takes priority over git diff** — when content-hash sync is active and deploy produced a written-files list, hooks match against that list. Falls back to git diff when: (a) remote deploy (no written-files), (b) content-hash sync disabled, (c) first deploy.

- **`DeployLocal` `--delete` semantics preserved** — the current remove-then-rename pattern deletes files not in source. Content-hash comparison happens per-file during `CopyDirIfChanged`; files present in source but absent in target still get written. Files present in target but absent in source still get removed (by the directory replacement). The only change is that files present in both with identical content are not re-written.

## Risks / Trade-offs

- **Double read on changed files** — `CopyFileIfChanged` reads the destination for comparison, then writes a new file. For files that actually changed, this adds one extra read. For config files (<100KB), this is negligible compared to the FUSE write cost avoided.

- **Race condition window** — between reading the destination hash and writing the new file, the destination could change. This is acceptable because: (a) bosun holds a flock during reconciliation, (b) no other process should be writing to appdata configs.

- **New files always written** — if the destination doesn't exist, `CopyFileIfChanged` skips comparison and writes directly. The `os.IsNotExist` check handles this cleanly.

## Open Questions

None — this is a focused optimization with clear boundaries.
