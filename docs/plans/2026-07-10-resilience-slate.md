# bosun Resilience Slate — Triaged Fix Plan (2026-07-10)

> Pickup-ready contract for a zero-context session. Read the anchor issues + this doc; you do
> not need the conversation that produced it.

## Context

bosun is a GitOps reconcile daemon (Go) that clones a config repo, renders/deploys Docker
Compose stacks to local or remote (SSH) targets, and watches drift. **94 open issues** (as of
2026-07-10). This slate triages three failure themes, each a real incident or live hole:

- **A — config-loading correctness**: the daemon's per-target config (`project_name` above
  all) is not fully honored from the repo `bosun.yaml`, so deploys collide.
- **B — security fail-open**: the webhook/trigger handlers deploy on an *unauthenticated*
  request when no secret is configured. The fix is **fail-closed auth**, not a bind change —
  see ruling A below on why the default bind stays `:%d`.
- **C — destructive-window atomicity**: the remote deploy's "atomic" replace deletes the
  live config dir *before* the move, with no rollback.

**Repository**: the `bosun` repo (local checkout), `main` **43 commits behind
`origin/main`**. All file/line refs are anchored to `origin/main` (`git -C … show
origin/main:<path>`) — don't trust the working tree, don't pull it; branch fresh worktrees
from `origin/main`. bosun ships **one-issue-one-PR from isolated worktrees**; this plan groups
by file-conflict, not mega-branch, and prescribes ordering.

## Triage method & scope

Method: read the three anchor issues (`#390/#391`, `#345`, `#343`), locate the defective
code at `origin/main`, and **re-check each anchor's status against those 43 commits** — some
have moved (the #390 row below is *partially addressed*, not unfixed), then sweep all 94 open
issues for the tightest title/label siblings of A/B/C.

**Anchor re-verification against `origin/main`:**

| Issue | Status on origin/main | Evidence |
|---|---|---|
| #345 (fail-open) | **UNFIXED** | All three handlers gate validation behind `WebhookSecret != ""` (server.go:223/:317/:435, explicit `else` "no secret → decode body directly" :465). Bind is `":%d"` in `Start` (:82); no bind-addr field on `Config`. |
| #343 (non-atomic replace) | **UNFIXED** | `ssh.go:267` — `rm -rf <target> && mv <tmp> <target>`. Deletes target before the move. |
| #391 (`default` discarded) | **UNFIXED** | `target.go:178-179` still `continue`s on `EqualFold(t.Name, DefaultTargetName)` — project_name dropped. #407 casefolded the check but didn't honor the target. |
| #390 (targets never read) | **PARTIALLY ADDRESSED** | `ConfigFromEnv` now adopts `projectCfg.Targets()` behind a three-conjunct guard (`!targetsFromEnv && len(rcfg.Targets)==0 && len(projectCfg.Targets())>0`, daemon.go:1763-64), so "env-only" is **stale**. BUT the collision root persists: `applyTargetOverrides` (config_reload.go:114) overlays only 4 operational fields and only for **named** targets — **never `ProjectName`, never the default**. |

**In scope**: sub-clusters A, B, C (anchors + ≤8 honorable mentions below). **Deferred (out of
scope, ~82 issues)**: the rollback-FAMILY siblings C deliberately leaves for later
(#331, #332, #335, #336 — see sub-cluster C), drift/breaker (#347-#350, #357-#358), backup
(#243-#244, #352-#353), sops/template (#245-#247, #278, #292-#294), fileutil
(#281-#282, #338, #351). Do not fix them here.

---

## Sub-cluster A — config-loading correctness

**Anchors**: #390 (partially addressed), #391 (unfixed).
**Files**: `internal/reconcile/target.go`, `internal/reconcile/config_reload.go`.

### Evidence

`target.go:177-180` — `ResolveTargets()` **skips-with-warn** (`EqualFold`→`continue`) any
target casefolding to `default`; #407 added the casefold, preserved the discard. Zero valid
targets → implicit default (target.go:200-214) takes `ProjectName` from flat `c.ProjectName`
(empty in the file-driven case → project-less `docker compose up` → container collisions). So
"fail loud" on a multi-target collision is NEW behavior, not a preserved default.
`applyTargetOverrides` (config_reload.go:114-131) overlays 4 operational fields, **not
ProjectName**, and only for named targets.

### Fix spec

1. **#391 — honor a lone `default` target (Option 1, ratified)** (`ResolveTargets`,
   target.go): when the config declares exactly one target and it casefolds to `default`,
   treat it as the implicit default's *configuration* (adopt its `ProjectName`, paths,
   StateFile, StagingDir) rather than discarding it. When there is >1 target and one is
   `default`, **fail loud** (a hard error, not the current skip-with-warn) — this is the new
   behavior. Net: a single `name: default` target's `project_name` reaches the deploy;
   multi-target-with-`default` errors instead of silently dropping a target.
2. **#390 — hot-reload ProjectName for the default/single-target case** (`config_reload.go`
   ONLY — see the daemon.go-frozen constraint below): extend the reload path so the cloned
   repo's single-target `project_name` is adopted before the first deploy even when
   `TargetName == DefaultTargetName`. Add `ProjectName` handling to `applyTargetOverrides`
   (guarded by `FromEnv` like the other fields — env `BOSUN_TARGETS`/flat overrides must
   still win). Push the resolved value onto `r.deploy.ProjectName` the same way
   `RemoveOrphans` is pushed (config_reload.go:81-83). **daemon.go is FROZEN to Group B**
   (ruling C): if honoring a *root-level* `project_name` needs a `ConfigFromEnv` edit in
   daemon.go, **drop root-level from #390's scope or sequence #390 after #345** — do not edit
   daemon.go from Group A (see Sequencing).

### Tests & acceptance

- `internal/reconcile/target_test.go` (NEW — no `target_test.go` exists; nearest
  `validation_test.go`): table test on `ResolveTargets` — (a) single `name: default` +
  `project_name: homelab` → target carries `homelab` (#391 regression); (b) multi-target incl.
  `default` → **fail loud** (hard error, not skip-with-warn); (c) no targets → implicit default
  from flat fields (unchanged).
- `config_reload.go` coverage: extend a reload test (grep `reloadProjectConfig` callers) →
  `ProjectName` adopted from a reloaded single-target config; env-set value **not** overwritten.
- Acceptance: cloned single `name: default` (or root-level) `project_name: X` → `docker compose
  -p X up` (no `"project":""`); env/flat overrides still win; `go test ./internal/reconcile/...`
  green.

---

## Sub-cluster B — security fail-open

**Anchor**: #345 (unfixed). **File**: `internal/daemon/server.go` (+ `daemon.go` config).
**Evidence** (see #345 anchor row): all three handlers skip validation on empty secret; `Start` binds `:%d` — the bind is NOT the security fix (ruling A).

### Fix spec

1. **Fail closed on missing secret (the security fix)**: when `WebhookSecret == ""`, reject
   trigger requests in all three handlers unless an **explicit opt-out** is set. Mirror the
   existing strict `== "true"` opt-in idiom (`BOSUN_ALLOW_EMPTY_DECLARED_STATE`,
   `BOSUN_SKIP_DEPLOY_INVARIANT`, daemon.go:1751-1752): add
   `BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK == "true"`; store on `Config`; gate the skip on it.
   Route all three through one shared helper — **`authorizeTrigger` contract** (pin this; the
   panel flagged double-write + scheme ambiguity):
   - Signature: `authorizeTrigger(w http.ResponseWriter, r *http.Request, body []byte, scheme triggerScheme) bool`.
   - The helper **writes the `401`/`403` itself and returns `false`**; caller does
     `if !s.authorizeTrigger(...) { return }` — **no second write**. Success → `true`, writes
     nothing. Missing-secret fail-closed lives here → one code path, no per-handler drift.
   - **Per-scheme** discriminator: GitHub → HMAC over `X-Hub-Signature-256`; generic
     `/webhook` → its current signature scheme; **manual (`handleManualTrigger`)** → served
     over the **Unix socket** (local trust): **exempt from HMAC** but **still governed by the
     allow-unauthenticated flag** (fail-closed default the opt-out relaxes) — not a free pass.
2. **LOUD startup warning + CHANGELOG (mandatory)**: this **403s secret-less deploys on
   upgrade** (README lists `WEBHOOK_SECRET` as "Optional"). Emit a loud `log.Warn` at start
   when no secret and no opt-out are set; call it out in CHANGELOG + PR body. See docs task.
3. **`BOSUN_LISTEN_ADDR` knob — DEFAULT UNCHANGED (ruling A)**: add `ListenAddr string` to
   `Config`, have `Start` use `net.JoinHostPort(s.cfg.ListenAddr, strconv.Itoa(port))` — **but
   keep the default bind at `:%d` (all interfaces); do NOT flip to `127.0.0.1`.** bosun runs
   bare-metal while Traefik (`/webhook/github`), Prometheus (`/metrics`), and Homepage
   (`/api/widget`) reach it from **containers over the docker bridge, not loopback** — a
   loopback default silently **bricks the live homelab deploy**. Fail-closed auth already
   closes the hole; the flip is redundant and the *sole* brick vector. Knob ships opt-in.
4. **Bind does NOT close #253 / #296**: it authenticates neither the Unix-socket trigger
   (#253) nor `/metrics` + `/api/widget` (#296) — scoped as **B follow-ups**; don't sell it so.

**Docs/spec task (ruling G — REQUIRED, or spec/CI reds):** fail-closed changes the documented
posture, so the #345 PR MUST also update `docs/security.md`,
`openspec/changes/add-daemon-security/`, and the README (`WEBHOOK_SECRET` is **no longer
"Optional"** without the opt-out) — in the same PR.

### Tests & acceptance

- `internal/daemon/server_test.go` (exists): no-secret/no-opt-out POST to `/webhook`,
  `/webhook/github` (push), `/webhook/manual` → reject + **no reconcile** (spy daemon);
  opt-out set → accepted; with a secret: valid sig → accepted, invalid/absent → `401`. Assert
  the helper writes **exactly one** response (no double-write).
- Bind test: `Start` composes the **all-interfaces** bind by default (unchanged), narrows on
  explicit `BOSUN_LISTEN_ADDR`.
- Acceptance: fail-closed default + opt-out restores old behavior; **default bind unchanged**
  (containers still reach bosun); docs/spec/README updated (ruling G); `go test
  ./internal/daemon/...` green.

---

## Sub-cluster C — destructive-window atomicity

**Anchor**: #343 (unfixed). **File**: `internal/reconcile/ssh.go`.
**Evidence**: `ssh.go:267` — `rm -rf <target> && mv <tmp> <target>`. `rm -rf` runs first; on
Unraid `/mnt/user` (FUSE) the cross-device `mv` may be non-atomic/fail, and a connection drop
between the two commands leaves the target **gone with no rollback**.

### Fix spec (function-level)

Rework the remote replace so **the live target is never deleted before the replacement is in
place**:

1. **Sibling-staging is ALREADY in place** — `tmpDir` is created under
   `targetParent = filepath.Dir(targetDir)` (ssh.go:204-212), already sharing the target's
   parent, so the "move staging to a sibling first" step is a **NO-OP**; drop it. What makes
   the swap safe is **retain-old-until-new**, not co-location — on Unraid's `/mnt/user` shfs
   (FUSE) a cross-device `rename` isn't kernel-atomic even for same-parent paths (EXDEV
   persists on shfs), so "safe, not atomic" is the honest framing.
2. Replace `rm -rf <target> && mv <tmp> <target>` with a **retain-old rename-swap**:
   `mv <target> <target>.bosun-old.<ts>` → `mv <tmp> <target>` → `rm -rf <target>.bosun-old.<ts>`,
   as one `ssh` invocation with `set -e` + rollback trap. Keep `shellquote.Join` on every path.
3. **Fix the rollback-nesting defect (panel finding)**: a naive rollback `mv <target>.old
   <target>` **nests-into** a partial `<target>` that survived a failed cross-device move
   instead of restoring it. Fix: in the rollback branch, **clear a partial `<target>` before
   restoring** (`[ -e <target> ] && rm -rf <target>; mv <target>.old <target>`).
4. **Crash-recovery path (panel finding)**: a mid-swap SSH drop currently **wedges the next
   reconcile** and **leaks `.bosun-old.<ts>`**. At the next deploy's start: (a) orphan-clean
   stale `.bosun-old.<ts>` siblings, (b) target-missing → **promote the newest
   `.bosun-old.<ts>`** back to `<target>` (self-heal). Preserve existing cleanup-on-error paths
   (ssh.go:270-273).
5. **Adopt the #402 FUSE settle idiom — CITE, don't reinvent**: #402 shipped Unraid
   `/mnt/user` handling for hooks — `IsUnderFUSEDeployPath` + `resolveHookSettleDelay`
   (`FUSEDeployPathPrefix = "/mnt/user"`, `defaultFUSESettleDelay = 2s`) in `hooks.go`. Reuse
   that detection + settle discipline (fsync the dest after rename + the FUSE settle delay) —
   same shfs/EXDEV problem, same solution.

### Tests & acceptance

- Unit-test the **command construction** (ssh.go SSH-shells): a pure helper returns the swap
  shell string; table-test it (paths quoted; order move-aside → move-in → cleanup; rollback
  branch present and **clears a partial `<target>` before restoring** so it doesn't nest-into).
  Nearest tests: `deploy_pure_test.go` / `pure_test.go`.
- Failure case: second `mv` fails → rollback restores `<target>` from `<target>.bosun-old.<ts>`
  incl. the nest-into guard. Crash-recovery: `<target>` missing + stray `.bosun-old.<ts>` →
  next deploy promotes the newest and cleans orphans.
- Acceptance: no sequence deletes `<target>` before the replacement is durably in place; an
  interrupted replace leaves the old OR the new tree, never empty; `go test
  ./internal/reconcile/...` green.

---

## Honorable mentions (≤8)

| Issue | Sub-cluster | Disposition |
|---|---|---|
| #272 `BOSUN_TARGETS=[]` handled inconsistently (drift vs daemon) | A | Same resolver surface as #390/#391; fix alongside if the resolver is open, else fast-follow. |
| #273 YAML `targets:` can't set state_file/staging_dir but `BOSUN_TARGETS` can | A | Parity gap in the same config-load path A touches; strong fast-follow. |
| #253 Unix socket peer creds extracted but never enforced | B | Sibling fail-open (local UID can force-trigger); same authorize-the-trigger theme — bundle into B's `authorizeTrigger` thinking. |
| #296 `/metrics` + `/api/widget` unauthenticated info-disclosure | B | Same surface family; needs its own auth — the bind knob does NOT close it (default bind stays all-interfaces). |
| #255 no `ReadHeaderTimeout` — Slowloris | B | Set `ReadHeaderTimeout` on the `http.Server` **construction in `NewServer`** (server.go:70), NOT `Start`. One-line-ish. |
| #334 promotes unverified/partial tar-over-SSH transfers to live (critical) | C | **IN-SCOPE** — swap-WINDOW hardening: atomicity is meaningless without integrity-verifying `tmp` before the swap. After #343. |
| #252 tar partial-extraction (local exit 0) replaces target with garbage | C | Same destructive-promotion window; verify-before-swap covers both. |
| #342 non-idempotent retry reuses fixed temp dir, swallows cleanup errors | C | Same `ssh.go` path; conflicts with #343 → sequence, don't parallelize. |

---

## Sequencing & PR strategy

One-issue-one-PR from isolated worktrees, branched from `origin/main`. File-conflict groups,
each strictly sequential internally:

- **Group A (target.go / config_reload.go)** — **#391 first** (makes a `default` target's
  config reachable) → **#390** (hot-reloads `project_name`). Honorable A (#272, #273) follow.
- **Group B (server.go / daemon.go)** — **#345** as one PR (fail-closed + `authorizeTrigger`
  helper + `BOSUN_LISTEN_ADDR` knob, default unchanged) establishes the auth helper the
  follow-ups extend → honorable B (#253, #296, #255).
- **Group C (ssh.go, all one file)** — **#343** (retain-old swap + rollback-nesting fix + crash
  recovery) → **#334/#252** (verify tmp before swap) → **#342** (retry idempotency). In #334's
  PR body, **name the deliberate deferral** of the rollback-FAMILY siblings
  (#331/#332/#335/#336) so the scope reads intentional.

**daemon.go is FROZEN to Group B (ruling C)** — A/B/C parallel worktrees are valid **only**
under that constraint. Group A's #390 stays in `config_reload.go`; if root-level `project_name`
needs a daemon.go `ConfigFromEnv` edit, #390 drops root-level scope or sequences after #345.
With that held, the three anchor PRs (#391→#390, #345, #343) touch disjoint files and run in
parallel. Serialized order if needed: **#345** (live hole, smallest blast) → **#391/#390**
(unblocks homelab deploy) → **#343** (largest, careful rollback testing).

## Risks

- **#391**: Option 1 *adds* both honor-lone-default and fail-loud-on-multi over today's
  skip-with-warn; guard with tests so a genuine multi-target collision errors (not drops).
- **#345 fail-closed** is breaking for secret-less-on-trusted-network users (403s deploys on
  upgrade); the opt-out flag + loud warn + docs/README update are mandatory.
- **#345 bind NOT flipped** (ruling A): default stays all-interfaces; flipping to loopback
  bricks the container-side callers. Fail-closed auth is the fix; the flip is declined.
- **#343 cross-device**: never kernel-atomic on `/mnt/user` (shfs EXDEV); retain-old keeps it
  *safe* not *atomic*. Test on the real mount; reuse #402's FUSE idiom.
- **Env naming**: legacy `WEBHOOK_SECRET` unprefixed, new `BOSUN_`-prefixed — keep new vars
  prefixed, document `WEBHOOK_SECRET` as a legacy exception (don't rename).
- **Stale checkout**: local `main` is 43 behind — branch from `origin/main`, not the working
  tree, or you re-introduce fixed code / mis-anchor lines.

## Verification

- Per PR: `go build ./...` + `go test` on the touched package green (new failure-case test
  present); whole slate `go test ./...` green before the final merge.
- #345 manual smoke: no-secret `curl -X POST localhost:8080/webhook` → **reject**;
  `BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true` → accept. Confirm default bind is **still
  all-interfaces** (`ss -ltnp` shows `0.0.0.0`/`*`, not loopback) so container callers reach
  it; set `BOSUN_LISTEN_ADDR` → confirm the bind narrows.

## Sources

- Issues: `gh issue view {390,391,345,343} -R cameronsjo/bosun`; `gh issue list --state open
  --limit 200` (94 open). Recent merges checked for overlap (#407 casefold, #410/#409/#408/#316
  /#298/#300/#222) — none close the four anchors.
- Code @ `origin/main` (checkout 43 behind): `server.go` (handlers 210-470, bind 71-82,
  NewServer 43-80, validateSignature 561-590), `daemon.go` (Config 40-60, ConfigFromEnv
  1488-1790, targets 1763-1764), `target.go` (ResolveTargets 168-215), `config_reload.go`
  (reload 11-109, applyTargetOverrides 114-131), `ssh.go` (staging 204-212, replace 265-276),
  `hooks.go` (FUSE idiom 129-170).

## Open decisions (Cameron)

Both pre-ruled here, Cameron-vetoable at PR review:

1. **Bind default**: knob-only default-unchanged (all interfaces) vs flip to loopback →
   **RULED knob-only** — the flip was the sole brick vector (container callers over the docker
   bridge); fail-closed auth closes the hole without it.
2. **#391**: honor-lone-default vs fail-loud-on-all → **RULED Option 1** (honor lone; fail loud
   only on multi-target-with-`default`).

## Panel review — findings declined

None — all actionable findings folded; the many red-team live-state confirmations required no
change.

## Panel review

Panel: 2× adversarial plan-reviewer lenses (conflict/drift, underspecification) + a live-state
red-team seat + owner-lens review — ~40 findings (majority live-state confirmations), all actionable folded, 0
declined. Verdicts: SOUND-WITH-FIXES ×3 / APPROVE-WITH-CHANGES. Load-bearing rulings folded:
bind default NOT flipped (knob-only — the flip was the sole brick vector; fail-closed auth
closes the hole); daemon.go frozen to Group B (preserves parallel worktrees); the #343
rollback-nesting defect + crash recovery + #402 FUSE idiom; #391 ratified
honor-lone-default; and #334 reframed as swap-window hardening.
