---
body_sha256: "a17096b03f8e743a04c622c94a4781b3a00bb4356d55940d8fd3d3c2a40b65e6"
session_id: "8aed3f87-6d8a-49da-8606-52cf876339b2"
model: "claude-opus-5"
harness: "claude-code 2.1.277"
machine: "cf6e768835c7"
approved_session_id: "a9d4a39f-36de-4a08-82d7-a71eddc33092"
status: in-progress
next: "Merge homelab#770 and run its host checks; drive spec PR #673 to ready-to-build, then implement Task 2"
branch: plan/bosun-upgrade-canary
pr: 672
updated: 2026-09-19
date: 2026-09-19
---

# Bosun upgrade canary: shadow render, pinned cutover, watched rollback

## Context

A Bosun upgrade is a blind swap today, and it is more automatic than the docs say.

- **Watchtower upgrades Bosun every night.** Watchtower runs at 04:00 with no label filter and `WATCHTOWER_CLEANUP=true` (`homelab` `unraid/compose/core.yml.tmpl:866-886`). Bosun runs `ghcr.io/cameronsjo/bosun:latest` with no opt-out label (`homelab` `unraid/appdata/bosun/docker-compose.yml:18,121-125`). So each release goes live at the next 04:00 with no check and no rollback anchor, because cleanup deletes the old image. (Found in config; not yet seen on the host.)
- **The manual path checks the wrong things.** `scripts/deploy-git-timeout.sh` checks version, restart and panics. It never confirms that the new version completed a reconcile, and it only prints rollback steps.
- **The CLI and the daemon build their config differently.** `bosun reconcile` does not read `BOSUN_INFRA_DIR`; only the daemon (`internal/daemon/daemon.go:2154`) and `render` (`internal/cmd/render.go:239`) read it. So no CLI dry run shows what the daemon would do.

There is one host and one daemon, so traffic-splitting doesn't apply. Here "canary" means the new version proves itself on the live commit before cutover. If it then fails its first real reconcile, it falls back to the old version automatically.

Scope chosen by Cameron: pin the image, add a shadow render check, and watch after cutover with auto-rollback. A long-running shadow daemon is declined.

## Goal

1. Bosun changes only when a homelab commit changes the digest pin. Watchtower no longer touches it.
2. `bash scripts/upgrade-bosun.sh`, run from the Mac, does the following:
   - Checks the candidate's release provenance.
   - Dry-runs incumbent and candidate on the NAS against the same commit and compares their renders.
   - Cuts over, then watches the first real reconcile.
   - Rolls back to a tagged incumbent image if the watch fails.
   - Every outcome gets a distinct exit code and leaves a durable history line on the NAS.

## Alternatives declined

- **Long-running shadow daemon.** Declined by Cameron, as more machinery than one user needs.
- **`bosun validate --full` as the shadow command.** It reads four env vars and no project config (`internal/cmd/validate.go:284-320`). It also writes the live state file (Task 3).
- **Unifying the CLI and daemon env→config builders into one function.** Correct long-term, but a larger refactor. A parity test (Task 2) catches the same drift at a fraction of the change.
- **Blanking alert env vars in the override.** Refuted by the panel. `getEnvOrDefault`/`BosunEnv` treat `""` as unset and fall back to the legacy `DISCORD_WEBHOOK_URL`, which is what the live compose sets (`internal/config/config.go:1140`, `internal/config/envutil.go:22-28`). An allowlist replaces it.
- **An `:rc` or canary image channel.** The digest pin makes it unnecessary.
- **Canary for the services Bosun deploys.** A different feature; `docs/gitops-comparison.md:44` keeps it out of scope.

## Architecture

```
homelab PR: pin tag@digest ──merge──▶ incumbent bosun syncs appdata/bosun/docker-compose.yml to NAS
                                         (file changes; container untouched; Watchtower opted out)
Mac  scripts/upgrade-bosun.sh  (caffeinate -i; tee ~/Library/Logs/bosun-upgrade/<UTC>.log)
  0 provenance  gh attestation verify oci://ghcr.io/cameronsjo/bosun@<candidate digest> -R cameronsjo/bosun
  1-5           scp scripts/upgrade-bosun-remote.sh → ssh nas 'bash …' (one session; all logic NAS-side)
NAS  upgrade-bosun-remote.sh   (mkdir lock + state file in /mnt/user/appdata/bosun-upgrade/)
  1 preflight   candidate = image: in on-disk compose (must carry @sha256:); incumbent = running RepoDigests[0]
                resume if state file says phase=cutover|watching; sweep stale /tmp/bosun-canary.*
                docker tag <incumbent> bosun:rollback-<ver>
  2 shadow      compose -f live.yml -f canary.override.yml run --rm --name bosun-canary-<run>-<role>
                  environment: !override <allowlist>  volumes: !override <isolated + appdata:ro>
                  bosun reconcile --dry-run --no-alerts   × {incumbent, candidate}
                diff -rq <run>/incumbent/staging/unraid <run>/candidate/staging/unraid → names only
  3 cutover     y/N → compose up -d --pull never; image/StartedAt/version checks (failure → stage 5)
  4 watch       docker exec bosun bosun daemon-status --json: last_reconcile non-null ∧ last_error empty
  5 rollback    persistent rollback.override.yml (image: bosun:rollback-<ver>) → up -d --pull never
                → one bosun trigger → watch again → verdict
```

## Global Constraints

- **Decrypted secrets never leave the NAS.** Dry-run staging holds rendered secrets. Shadow output goes to `mktemp -d /tmp/bosun-canary.XXXXXX` (mode `0700`, RAM-backed on Unraid, bind-mounted per role). The trap that deletes it runs on the NAS, on `EXIT HUP INT TERM`, and it also `docker rm -f`s the `bosun-canary-<run>-*` containers. Output carries file names, counts and verdicts only. Failure log lines stay in the NAS history, not the SSH stream.
- **A shadow container gets only what a render needs.** `environment: !override` allowlists these and nothing else: `BOSUN_REPO_URL`/`REPO_URL`, branch, `BOSUN_INFRA_DIR`, `BOSUN_TARGETS`, `BOSUN_SECRETS_FILE`, `SOPS_AGE_KEY_FILE`, `BOSUN_SSH_KEY`, deploy-path vars, `REPO_DIR`, `STAGING_DIR`, `LOG_DIR`, `BACKUP_DIR`, `DRY_RUN=true`, `TZ`. Every alert, token, webhook, Sentry and OTel variable is left out. `volumes: !override` gives it only the age key and deploy key (both `:ro`), `/mnt/user/appdata:/mnt/appdata:ro` (deploy-mode resolution stats it; `pure.go:101-117`) and its own tmp dirs. There is no `docker.sock` and no compose-manager mount. The lock path is fixed (`target.go:32`) but container-local, so it is isolated anyway.
- **Every image reference is a digest.** A candidate without `@sha256:` is refused. Provenance is verified against the digest, never the tag. Rollback uses the local tag `bosun:rollback-<ver>` with `--pull never`.
- **The script can resume.** Before cutover it writes the state file (`phase`, incumbent digest, rollback tag, candidate digest, cutover `StartedAt`). A re-run with `phase=cutover|watching` resumes the watch; it never takes "incumbent == candidate" as success.
- **Bash waiver, declared:** the remote script, the Mac wrapper and their test harness will all run past the 100-line rule (the waiver originally named only the remote script; polish flagged the other two). Why: the logic is Docker/compose orchestration on an Unraid host with no Python, and it replaces a 250-line predecessor of the same kind. `set -euo pipefail` and `shellcheck` still apply.

## Orchestrator

**Driver:** opus — trigger: secrets-bearing shadow runs and an automatic rollback on the production deployer; the security seat's findings feed back, and Task 6 is an Opus security review.

## Tasks

- [ ] Task 1 — homelab Watchtower opt-out + digest pin
- [ ] Task 2 — OpenSpec change, then reconcile CLI parity + `--no-alerts`
- [x] Task 3 — file the `validate --full` state-write issue (#674)
- [x] Task 4 — upgrade scripts
- [x] Task 5 — script tests + docs
- [ ] Task 6 — Opus security review

### Task 1 — Stop the nightly blind upgrade, pin the image (homelab PR; ships alone)

- `unraid/appdata/bosun/docker-compose.yml`: add the label `com.centurylinklabs.watchtower.enable=false`. Change `image:` to `ghcr.io/cameronsjo/bosun:<X.Y.Z>@sha256:<digest>`, where the digest is today's running `RepoDigests[0]`. The change takes effect at the next recreate, which is Task 4's first run or a manual `up -d`.
- `docs/bosun-maintenance.md`: rewrite § update. An upgrade becomes: PR bumps the pin → sync → `upgrade-bosun.sh`. Resolve the "auto-deploys itself" contradiction (`:16` vs `:40`) and the Watchtower note.
- **Verify on the host after merge**, with the checks in a scratch script Cameron runs:
  - (a) The NAS compose file's sha256 equals the repo blob.
  - (b) The bosun container's `com.docker.compose.project.working_dir` is `/mnt/user/appdata/bosun`.
  - (c) After a manual `up -d`, `docker inspect bosun` shows the label.
  - (d) Watchtower's next run log skips bosun.
  - If (a) fails, Task 4 reads the candidate from `homelab` `origin/main` instead. Record it in `## Deviations`.

### Task 2 — CLI/daemon config parity + `--no-alerts` (bosun; OpenSpec change first)

- Open an OpenSpec change `add-reconcile-cli-daemon-parity` under `openspec/changes/`, with spec deltas for the reconcile CLI. It follows the spec review workflow up to `ready-to-build`.
- `internal/cmd/reconcile.go`: read `BOSUN_INFRA_DIR` the way the daemon does. Add `--no-alerts`, which builds the reconciler with no alert providers. It is explicit; a dry run alone does not imply it.
- Parity test: set one env map, then compare the `reconcile.Config` built by the CLI with the one built by `daemon.ConfigFromEnv` → rcfg, on every field both paths read. Stage the break: remove the new `BOSUN_INFRA_DIR` read and confirm the test goes red.
- `--no-alerts` test: a failing dry run with `DISCORD_WEBHOOK_URL` set sends nothing to a mock provider.
- Update `docs/commands.md` and `skills/onboard/resources/commands.md`.
- Ship as a normal `feat:` release. **Transition:** until the incumbent is a parity-capable version, stage 2 runs in candidate-only mode (Task 4).

### Task 3 — File the dry-run state-write bug (bosun issue)

`Reconciler.Run` records `LastDeployedCommit`, resets `AttemptCount` and bumps `DeployCount` on a dry run (`internal/reconcile/reconcile.go:979-993`). `reconcile --dry-run` is protected by a scratch copy (`internal/cmd/reconcile.go:126-165`). **`validate --full` is not** (`internal/cmd/validate.go:284-320`). Run in the live container, it can mark a pending commit as deployed, which makes the daemon skip it (`:580`) and resets the breaker. File with `cadence:creating-issue`, and give a repro using a throwaway state dir, never the live container. Not a prerequisite here: the shadow uses `reconcile`, not `validate`.

### Task 4 — `scripts/upgrade-bosun.sh` + `scripts/upgrade-bosun-remote.sh` (bosun PR)

The Mac wrapper does four things: preflight SSH, stage 0 provenance (`gh attestation verify`; stop on failure), `scp` of the remote script, and one `ssh` call. It also tees the log and wraps the run in `caffeinate -i`. The remote script does stages 1–5 as drawn in Architecture, plus:

- **Stage 1:**
  - Take a `mkdir` lock next to the state file.
  - Exit 0 `ALREADY-CURRENT` only when the digests match *and* no state file is in progress.
  - Warn if the history log's last entry for this candidate says `ROLLED-BACK`.
- **Stage 2 verdicts.** Each role renders into its own `<tmp>/<role>/staging`. The diff compares `TargetStagingDir(…, unraid)`. The rendered commit is read from each run's log line, and if the two commits differ the incumbent is re-run once.
  - Incumbent fails → `HARNESS-INVALID` (exit 4). The repo or the harness is broken, not the upgrade.
  - Candidate fails → `CANDIDATE-FAILED` (exit 5). No cutover.
  - Diff empty → `RENDER-IDENTICAL`.
  - Diff not empty → print the paths, then y/N. `--yes` never skips this prompt.
  - Incumbent version is older than the Task 2 release → run the candidate only, then `RENDER-OK-NO-BASELINE` and y/N.
- **Stage 3:**
  - `up -d --pull never`. The image was pulled in stage 2, and the compose file names the pinned digest.
  - Checks: image changed, `StartedAt` moved, `bosun --version` equals the candidate. A failed check goes to stage 5.
- **Stage 4:**
  - The reference time is the container's `StartedAt`, read on the NAS. The daemon reconciles about 10s after start (`daemon.go:150,528`), and `last_reconcile` is set only when a cycle ends (`daemon.go:1145`).
  - Pass: a non-null `last_reconcile` with an empty `last_error`.
  - Fail, naming which one tripped and with expected vs seen values: `last_error`, container not running or restarting, `panic:` in logs, or no reconcile within `--watch-timeout` (default 15m).
  - Before rollback, capture the last `daemon-status` JSON, the restart count and 40 log lines to the NAS history.
- **Stage 5:**
  - Write `/mnt/user/appdata/bosun-upgrade/rollback.override.yml`, which persists. Its header gives the date, the reason, and "delete after reverting the pin PR". It is outside Bosun's sync path; confirm that against `deploy_sync_paths`.
  - Run `up -d -f live.yml -f rollback.override.yml --pull never`, check the version, send one `bosun trigger`, and watch again.
  - Rollback works and the reconcile passes → `ROLLED-BACK` (exit 1).
  - Rollback works but the reconcile also fails → `FAULT-NOT-UPGRADE` (exit 2). See `homelab` `docs/runbooks/bosun-deploys-blocked.md`.
  - Rollback itself fails → `HALF-CHANGED` (exit 3). Print the manual steps with the rollback tag.
- **Exit codes:**

  | Code | Meaning |
  |---|---|
  | 0 | Upgraded, or `ALREADY-CURRENT` |
  | 1 | `ROLLED-BACK` |
  | 2 | `FAULT-NOT-UPGRADE` |
  | 3 | `HALF-CHANGED` |
  | 4 | `HARNESS-INVALID` |
  | 5 | `CANDIDATE-FAILED` |
  | 64 | Usage or config error |
  | 75 | Transient failure, **before cutover only**. SSH lost after cutover: re-run, which resumes |

  The script header documents these and flags that exit 1 no longer means "check failed".
- **History:** each run appends one line to `/mnt/user/appdata/bosun-upgrade/history.log`: UTC time, `$USER@host`, both digests, verdict, exit code.
- Remove `deploy-git-timeout.sh` and `verify-git-timeout.sh`. Their mentions in `openspec/changes/update-git-timeouts-and-alert-recovery/` and `docs/plans/2026-09-08-…` are historical and stay unchanged.

### Task 5 — Script tests + docs (same PR as Task 4)

- `scripts/upgrade-bosun_test.sh`, in the style of `agent-go-gate_test.sh`, with `ssh`/`docker`/`gh` stubbed on `PATH`. Cases:
  - One case per exit code.
  - Resume from `phase=watching`.
  - Commit mismatch → retry.
  - A tag-only candidate is refused.
  - The generated override contains no key outside the allowlist, and in particular no `DISCORD_WEBHOOK_URL`, `WEBHOOK_SECRET` or `*_TOKEN`, and no `docker.sock`.
  - Stage the break by adding `DISCORD_WEBHOOK_URL` back to the allowlist; confirm the test goes red.
- Run `shellcheck` on both scripts.
- `skills/onboard/resources/gitops.md`: add an "Upgrading Bosun itself" section. `docs/troubleshooting.md`: one entry per verdict, with the next step for each.

### Task 6 — Opus security review before the first live run

Run `cadence-forge:security-reviewer` on Opus over both scripts, the generated overrides, `internal/config/config.go:1140-1253`, the `--no-alerts` wiring and `internal/reconcile/alerts.go`. This happens after Task 4/5 merge and before Verification step 3.

## Verification

1. `scripts/agent-go-gate.sh go test ./internal/cmd/... ./internal/daemon/...` passes, and the parity test goes red on the staged break.
2. `bash scripts/upgrade-bosun_test.sh` passes, and goes red on the staged allowlist break. `shellcheck` is clean.
3. Task 1's host checks (a)–(d) pass.
4. The first live run is `bash scripts/upgrade-bosun.sh --dry-run`, which runs stages 0–2 only. Expect `RENDER-OK-NO-BASELINE` (transition) or `RENDER-IDENTICAL`. Then check that `ssh nas ls -d /tmp/bosun-canary.*` shows nothing, and that Discord got nothing.
5. The full run on the next real release exits 0. `daemon-status --json` shows a post-cutover `last_reconcile` with no error.
6. Rollback drill, once: build a local image whose entrypoint exits 1, `docker save | ssh nas docker load` it, point the on-disk compose at its digest, and run. Skip provenance with a `--skip-provenance-for-drill` flag that is documented as drill-only. Expect exit 1 `ROLLED-BACK` and the incumbent running from the `bosun:rollback-<ver>` tag. Restore the compose file afterwards.

## Panel

Panel: plan-reviewer (both lenses), red-team-reviewer, operability-reviewer, security-posture-reviewer ran — 42 findings, 39 folded in, 3 declined (see Panel review — findings declined)

## Panel review — findings declined

- Plan reviewer: "Use `bosun render` for the shadow instead of fixing the CLI." Declined. `render` renders templates only; it skips declared-state extraction and deploy-mode resolution, which a canary should exercise. The parity fix plus its test is the lasting cure.
- Operability: "Discord post on `ROLLED-BACK`/`HALF-CHANGED`." Declined for now. The Mac log and NAS history are enough for one operator who is watching the run. Add it if unattended runs ever happen.
- Plan reviewer: "Expected-diff allowlist beyond the y/N prompt." Declined. Renders are deterministic by grep (red team), and one operator reviews each diff.

## Deviations

- **SSH host is `unraid`, not `nas`.** `~/.ssh/config` names the NAS `unraid` (user `root`); `nas` is refused. The wrapper takes `--host` (default `unraid`, or `$BOSUN_UPGRADE_HOST`).
- **Parity detected by capability, not version.** Stage 2 probes each image with `bosun reconcile --help` for `--no-alerts` instead of comparing against the Task 2 release number, which does not exist yet and could drift. A candidate without the flag is refused (exit 64); an incumbent without it gives `RENDER-OK-NO-BASELINE`.
- **Rollback watches the daemon's own start-up reconcile; no `bosun trigger`.** The daemon reconciles about 10s after start (`daemon.go:150`), the same event stage 4 watches. A trigger would queue a second cycle and add a socket-readiness race.
- **Provenance is pinned to the release workflow** (`--signer-workflow …/release-please.yml`). Checked in both directions: the running `0.42.3` digest passes, and the same digest fails against `webui.yml`. A provenance failure exits 5 (`CANDIDATE-FAILED-PROVENANCE`) and is written to the NAS history through `--record-provenance-failure`.
- **The shadow diff covers the whole staging root**, not only `TargetStagingDir(…, unraid)`. It is a superset that covers every target.
- **Task 1 also sets `platform: linux/amd64`** on the bosun service (homelab rule, flagged by CodeRabbit on homelab#770).
- **The incumbent renders first** (pre-PR code review). The first draft rendered the candidate first. That made the "re-run the incumbent" retry unable to converge on a forward push. It also blamed a broken harness on the candidate.
- **The shadow gets an empty `/mnt/appdata`, not `appdata:ro`** (pre-PR security review). Deploy-mode detection only stats the path. The real appdata holds `bosun/.env`, with the secrets the env allowlist drops. The override also resets every inherited risky key (`privileged`, `cap_add`, `devices`, `secrets`, `pid`, …).
- **Cutover pins the verified digest in its own override** (security review). The live file is re-synced from git during the run, so a re-read could start a different image.
- **The drill flag is fenced.** It refuses `--yes`, and the NAS history marks the run `[provenance skipped: drill]`. Provenance also pins `--source-ref refs/heads/main --deny-self-hosted-runners`, checked in both directions.
- **Verification step 6 needs a different drill image.** A `docker load`ed image has no registry digest, and an entrypoint that exits 1 fails the shadow render (exit 5) before cutover. The drill image must be pushed to a registry under a digest. It must pass `reconcile --dry-run`, then fail only as a daemon, for example with a daemon that sets `last_error`.
- **Polish pass** (fresh 4-finder review of the fixed branch) moved more behavior:
  - The lock records its owner's pid and boot id. A dead owner's lock is reclaimed; a live owner is reported, never overridden. This is what makes "re-run to resume" true after a reboot or a silent disconnect.
  - A pin whose tag and digest disagree now fails before cutover (exit 5).
  - A pin reverted during an interrupted upgrade rolls back instead of exiting 64.
  - The watch compares image IDs, not RepoDigests.
  - A failed log read fails the watch.
  - Every state-dir write reports a verdict instead of a bare exit 1.
  - CI's shellcheck (0.9 on Ubuntu) needed SC2317 beside SC2329 on the trap functions; both versions are now clean.
- **Task 2 scope.** The CLI misses about 20 daemon env reads, not just `BOSUN_INFRA_DIR`. As planned, it fixes only `BOSUN_INFRA_DIR`. The parity test classifies every `reconcile.Config` field, and the remaining gaps go to one follow-up issue (spec task 1.5).

## Learnings

- **The Task 3 bug reproduces on `0.42.3`**: one `validate --full` moved `last_deployed_commit` to HEAD and reset `attempt_count` from 2 to 0. The state dir is `/var/lib/bosun` (`state.go:17`), and the live container mounts no volume there. So a `docker exec bosun bosun validate --full` writes the daemon's own state.
- **`rollback.override.yml` is outside Bosun's reach.** It sits in `/mnt/user/appdata/bosun-upgrade/`, which is not in the homelab repo. Sync writes only repo files, and the prune step removes only files bosun itself wrote.
- The image entrypoint is `tini --`, so a one-off command needs the `bosun` prefix (`… bosun reconcile --dry-run`).
- `daemon-status` reports `last_reconcile` in local time (`-05:00`), while `StartedAt` is UTC. The watch compares epoch seconds.
- The NAS has `jq`, GNU `date` and `flock`, but no `python3`. Compose is `v2.40.3`, which supports `!override`/`!reset`.
- Watchtower's `Scanned=` count equals the running containers without the opt-out label (79 == 79 on 2026-09-19). Task 1 check (d) asserts that instead of grepping for "bosun", which Watchtower never logs at info level.
