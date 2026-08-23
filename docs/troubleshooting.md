# Troubleshooting Guide

## Common Issues

### "project root not found"

Bosun searches upward for `bosun/` or `manifest/` directory.

- Ensure you're inside a bosun project
- Or specify path: `bosun --root /path/to/project`

### "connect to docker: ..."

- Check Docker is running: `docker ps`
- Check Docker socket permissions
- On Linux: `sudo usermod -aG docker $USER`

### "sops decrypt failed"

Bosun classifies failures without printing raw SOPS errors, key identifiers,
encrypted values, or decrypted MACs:

- **SOPS integrity verification failed** — the file or its MAC may have been
  modified. Restore the encrypted file from a trusted source or re-encrypt it;
  rotating the Age key will not repair corrupted ciphertext.
- **SOPS decryption key unavailable** — verify that `SOPS_AGE_KEY` or
  `SOPS_AGE_KEY_FILE` contains an identity matching the file recipients and
  that the key file is readable.
- **Malformed SOPS encrypted data** — validate or re-encrypt the file with
  SOPS; an encrypted value or metadata field is not decodable.
- **SOPS decryption failed** — validate the file with SOPS and verify the Age
  key when the failure cannot be safely classified further.

Set `BOSUN_LOG_LEVEL=debug` for the sanitized failure category and file context.
Bosun never logs the raw upstream decryption error, even at debug level.

### "docker compose: command not found"

Bosun requires Docker Compose v2:

- Install: https://docs.docker.com/compose/install/
- Verify: `docker compose version`

### SSH connection failures

- Test manually: `ssh user@host exit`
- Check SSH key is loaded: `ssh-add -l`
- Verify host is reachable: `ping host`

### Deploy reports success but files unchanged

If `bosun reconcile` returns `success: true` and `docker compose up` exits 0, but the destination files at `/mnt/user/appdata/<path>` haven't been updated (compare mtimes, or `grep` for an expected token from the new template), one of two invariant errors will now surface the cause instead of letting the deploy claim success silently. Both were added in response to GH#214.

**Invariant 1 — `declared-state invariant: no declared services in staging compose directory`**

The render step produced no parseable services in `<staging>/compose/`. Either templates failed to write to the expected location, the compose dir is genuinely empty, or all files in it are unparseable YAML.

- For genuinely empty repos: set `BOSUN_ALLOW_EMPTY_DECLARED_STATE=true` to opt out — the reconciler will log at `Warn` level (with `override=true`) and continue.
- For misconfigured staging paths (compose dir missing entirely): the error is unconditionally fatal — no override applies. Check `BOSUN_INFRA_DIR` and the rendered staging tree. When the configured infra dir has no `compose/` but a sibling directory does, the error now names the candidate and suggests the fix, e.g.:

  ```text
  declared-state invariant: staging compose directory does not exist:
  /app/staging/compose (compose/ found under sibling dir(s): unraid)
  — did you mean BOSUN_INFRA_DIR="unraid"?
  ```

  This is the GH#214 root cause: `BOSUN_INFRA_DIR="."` while `compose/` and `appdata/` live under `unraid/`. Set `BOSUN_INFRA_DIR` to the named directory so render, discovery, and deploy all resolve the same infra root.

**Invariant 2 — `deploy invariant: source has files but no writes recorded` / `destination file has stale mtime`**

The deploy sync step claimed success but the destination doesn't reflect it. Two shapes trip this: (1) a written file's mtime is older than the reconcile start, or (2) zero files were written against a non-empty source **and a source file is missing from — or holds stale bytes at — the destination**. Both are the GH#214 silent-sync signature — the write path silently failed. The zero-write check is content-equality (SHA-256), not existence: a destination that already byte-matches the source is a legitimate no-op and does **not** fail (fixed in GH#330), but a file that exists at the right path with outdated content **does** fail (a stale write the sync silently failed to replace). The `mismatch=…` field in the error names the first absent-or-differing destination path. Symlinks in the source are skipped — they are never deployed, so they impose no requirement.

To debug:

```bash
BOSUN_LOG_LEVEL=debug bosun reconcile
```

The per-file logs from `internal/fileutil` will show `wrote src=… dst=… bytes=N` for every actual write and `skipped src=… dst=… reason=hash_match` for every skip. Compare against the destination's mtime on disk.

Emergency escape hatch (do NOT leave on):

```bash
BOSUN_SKIP_DEPLOY_INVARIANT=true bosun reconcile
```

The reconciler will log a `Warn` with `override=true` so the override is visible in monitoring. File a bug if you needed this — it indicates the invariant is misfiring or the underlying sync bug is reproducing.

### Removed post-sync hooks still run

On current versions, removing the root `post_sync_hooks` key, setting it to `[]`, or committing a valid empty `bosun.yaml` clears file-sourced hooks on the next successful reload. A missing or malformed config intentionally retains the last effective hooks because it is not a valid snapshot. Check the reload log for `hooks_outcome`, `hooks_source`, and `target`; command arguments are intentionally redacted.

For target hooks, an omitted key inherits root and explicit `post_sync_hooks: []` disables inheritance. Removing a target descriptor drops its operational hook override immediately, but restart the daemon to remove the target from the running topology. `BOSUN_POST_SYNC_HOOKS` and target hooks supplied by `BOSUN_TARGETS` are environment-owned; remove or change those environment values and restart Bosun rather than editing `bosun.yaml`.

## Debug Mode

Set verbose output:

```bash
bosun --verbose provision mystack
```

## Getting Help

- GitHub Issues: https://github.com/cameronsjo/bosun/issues
- Run diagnostics: `bosun doctor`
