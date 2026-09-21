# Rebuilding the rollback drill image

`scripts/rollback-drill.sh` proves the canary's automatic rollback works. It needs an image that **renders correctly and fails only as a daemon**. This is how to rebuild that image and point the drill at it.

Until now the recipe existed only in a commit message. That is why this file exists: the drill is rare, so the recipe is always cold when you need it.

## When

The drill image is built **FROM a specific bosun release**, pinned by digest. It does not track releases. Rebuild it when:

- the drill fails at the shadow render (`CANDIDATE-FAILED`, exit 5) because its base has drifted too far from the running version to render the same tree, or
- you want the drill to exercise a newer release's cutover path.

A stale drill image is not dangerous — it is pinned, private, and only ever runs during a drill. Do not schedule a rebuild. See [Why this is not automated](#why-this-is-not-automated).

## The two properties that matter

Both were learned by getting them wrong, and both are easy to break while "improving" the image:

- **It must pass the shadow render.** An image whose entrypoint exits immediately fails stage 2 and returns `CANDIDATE-FAILED` without ever reaching the watch — it tests the wrong stage. `bosun --version` and `bosun reconcile` must work normally.
- **The fault must live in the image.** Breaking the candidate through the shared compose file — a bad env var, a tiny `--watch-timeout` — breaks the rollback target too, so stage 5's own watch fails and the verdict becomes `FAULT-NOT-UPGRADE` instead of `ROLLED-BACK`.

The shim below satisfies both: everything passes through to the real binary except `bosun daemon`, which stays up and never reconciles.

## Build it

Pick the base digest — normally whatever is pinned in homelab now:

```bash
docker buildx imagetools inspect ghcr.io/cameronsjo/bosun:<X.Y.Z>   # take the Digest: line
```

Two files in an empty directory. `drill`:

```sh
#!/bin/sh
# Rollback-drill image. Everything works except the daemon, which stays up and
# never reconciles -- the exact shape that exercises the canary watch and its
# automatic rollback. NOT A RELEASE.
if [ "$1" = bosun ] && [ "$2" = daemon ]; then
  echo "drill image: daemon intentionally inert, no reconcile will complete" >&2
  exec sleep infinity
fi
exec "$@"
```

`Dockerfile` — keep the base pinned by digest, never by tag:

```dockerfile
FROM ghcr.io/cameronsjo/bosun:<X.Y.Z>@sha256:<base digest>

USER root
COPY drill /usr/local/bin/drill
RUN chmod 0755 /usr/local/bin/drill
USER bosun

ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/drill"]
CMD ["bosun", "daemon"]
```

`ENTRYPOINT` and `CMD` must match the base's own (`/sbin/tini --` plus `bosun daemon`) with the shim spliced in. Check with `docker image inspect` if the base ever changes them.

Build for the deploy host's architecture — the NAS is amd64, so this matters when building from an ARM Mac:

```bash
gh auth token | docker login ghcr.io -u <user> --password-stdin
docker buildx build --platform linux/amd64 -t ghcr.io/cameronsjo/bosun-drill:watch-fail --push .
```

Pipe the token; never put it in `argv`.

## Confirm it before trusting it

The package must be **private**. A new GHCR package defaults to private — confirm rather than assume:

```bash
gh api user/packages/container/bosun-drill --jq .visibility   # must print: private
```

It is a separate package from `bosun` on purpose, so nothing deliberately broken can ever be pulled by something expecting a release. A public drill package also fails the canary's provenance check on its repo, so making it public breaks the drill as well as being wrong.

Then check the three behaviours the drill depends on, on the host that will run it:

```bash
IMG=ghcr.io/cameronsjo/bosun-drill@sha256:<new digest>
ssh unraid "docker run --rm --network none $IMG bosun --version"                  # real version
ssh unraid "docker run --rm --network none $IMG bosun reconcile --help | grep -c -- --no-alerts"   # 1
ssh unraid "timeout 8 docker run --rm --network none $IMG"                        # notice, then hangs
```

The second is not cosmetic: the canary refuses a candidate without `reconcile --no-alerts`.

## Point the drill at it

`DRILL_REF` near the top of `scripts/rollback-drill.sh` is a hardcoded digest. Update it, and update the base version named in that script's header and in `docs/plans/2026-09-19-bosun-upgrade-canary.md`.

Then run the drill (it needs a terminal — an unverified image always prompts):

```bash
bash scripts/rollback-drill.sh
```

Expect exit 1, `ROLLED-BACK [provenance skipped: drill]`.

## Why this is not automated

A scheduled job that rebuilds and publishes a deliberately broken bosun image is a worse failure mode than a stale one. It puts a broken artifact on a registry on a timer, with no one watching, for a procedure that runs a few times a year and needs an operator at a prompt anyway.

The drill is deliberate by design. Its image should be too.
