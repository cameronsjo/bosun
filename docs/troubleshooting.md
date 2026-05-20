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

- Verify SOPS is installed: `sops --version`
- Check age key exists: `ls ~/.config/sops/age/keys.txt`
- Set key path: `export SOPS_AGE_KEY_FILE=/path/to/key`

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

  ```
  declared-state invariant: staging compose directory does not exist:
  /app/staging/compose (compose/ found under sibling dir(s): unraid)
  — did you mean BOSUN_INFRA_DIR=unraid?
  ```

  This is the GH#214 root cause: `BOSUN_INFRA_DIR="."` while `compose/` and `appdata/` live under `unraid/`. Set `BOSUN_INFRA_DIR` to the named directory so render, discovery, and deploy all resolve the same infra root.

**Invariant 2 — `deploy invariant: source has files but no writes recorded` / `destination file has stale mtime`**

The deploy sync step claimed success but either wrote nothing against a non-empty source or the destination's mtime is older than the reconcile start. This is the GH#214 silent-success signature — content-hash sync may have matched against stale destination content, or the write path silently failed.

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

## Debug Mode

Set verbose output:

```bash
bosun --verbose provision mystack
```

## Getting Help

- GitHub Issues: https://github.com/cameronsjo/bosun/issues
- Run diagnostics: `bosun doctor`
