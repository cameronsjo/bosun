# Rebuilding the rollback drill image

`scripts/rollback-drill.sh` proves the canary's automatic rollback works. It needs an image that **renders correctly and fails only as a daemon**. This is how to rebuild that image and point the drill at it.

Until now the recipe existed only in a commit message. That is why this file exists: the drill is rare, so the recipe is always cold when you need it.

## When

The drill image is built **FROM a specific bosun release**, pinned by digest. It does not track releases. Rebuild it when:

- the drill reports **`RENDER-DIFFERS`** and lists files, because the base has drifted from the running version, or
- you want the drill to exercise a newer release's cutover path.

Drift is noisy, not fatal. `RENDER-DIFFERS` prompts rather than aborting, and answering `y` still cuts over and still exercises rollback — the renders differing is expected when the base and the incumbent are different releases. Answering `n` ends the run at **exit 0, `DECLINED at RENDER-DIFFERS`**, which is the signal to come here. It is not `CANDIDATE-FAILED`; that verdict means the drill image failed to render at all, which is a broken shim, not drift.

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

`ENTRYPOINT` and `CMD` must match the base's own (`/sbin/tini --` plus `bosun daemon`) with the shim spliced in. The source of truth is `Dockerfile` in this repo; `docker image inspect <base>` confirms what actually shipped.

Build for the deploy host's architecture — the NAS is amd64, so this matters when building from an ARM Mac:

```bash
gh auth token | docker login ghcr.io -u <your github username> --password-stdin
docker buildx build --platform linux/amd64 -t ghcr.io/cameronsjo/bosun-drill:watch-fail --push .
```

Pipe the token; never put it in `argv`. The token needs `write:packages` — check with `gh auth status`, which lists scopes. If the push fails with a permissions error, that is the first thing to look at; the message reads like a bad username.

## Confirm it before trusting it

The package must be **private**. A new GHCR package defaults to private — confirm rather than assume:

```bash
gh api user/packages/container/bosun-drill --jq .visibility   # must print: private
```

It is a separate package from `bosun` on purpose, so nothing deliberately broken can ever be pulled by something expecting a release. Nothing in the canary enforces that: on the drill path `verify_provenance` returns at the skip before it checks anything, including the repo name (`scripts/upgrade-bosun.sh:78-82`). Private is a containment decision you are making, not a rule the tooling applies — so it is on you to check it.

The NAS pulls this image during the drill. Because the package is private, that host needs a `ghcr.io` read credential; the probes below are the first pull and will surface a missing one. In the canary itself a failed pull exits 75, not 5.

Then check the three behaviours the drill depends on, on the host that will run it:

```bash
IMG=ghcr.io/cameronsjo/bosun-drill@sha256:<new digest>

# 1. Real version, exit 0.
ssh unraid "docker run --rm --network none $IMG bosun --version"

# 2. The flag must be present. The canary tests presence, not count.
ssh unraid "docker run --rm --network none $IMG bosun reconcile --help | grep -q -- --no-alerts" && echo PRESENT

# 3. Inert: prints the notice, then hangs until timeout kills it.
#    PASS is exit 124 (timeout fired). Exit 0 means it exited on its own,
#    which is the shim broken in the direction that tests the wrong stage.
ssh unraid "timeout 8 docker run --rm --network none $IMG"; echo "exit=$?"
```

Probe 2 is not cosmetic: the canary refuses a candidate whose `reconcile` has no `--no-alerts`. Probe 3's inverted exit code is the one to read carefully — a passing drill image never exits on its own.

## Point the drill at it

Two places in `scripts/rollback-drill.sh`, and only two:

- `DRILL_REF` (`:85`) — the digest you just pushed.
- The header line describing the base (`:55`, "It is the `X.Y.Z` image with one shim").

Leave the file's other two version mentions alone. `:11` records the first live run and `:39` names the incumbent a passing drill restores; both are history, and editing them to match a new base makes the record false. For the same reason, do **not** touch `docs/plans/2026-09-19-bosun-upgrade-canary.md` — its version numbers are a dated receipt, not configuration.

Keep the GHCR version you are replacing until `DRILL_REF` no longer points at it. Pruning untagged versions is the easy way to leave a committed digest pointing at nothing.

Then run the drill (it needs a terminal — an unverified image always prompts):

```bash
bash scripts/rollback-drill.sh
```

Expect exit 1, `ROLLED-BACK [provenance skipped: drill]`.

## Why this is not automated

A scheduled job that rebuilds and publishes a deliberately broken bosun image is a worse failure mode than a stale one. It puts a broken artifact on a registry on a timer, with no one watching, for a procedure that runs a few times a year and needs an operator at a prompt anyway.

The drill is deliberate by design. Its image should be too.
