#!/usr/bin/env bash
# file_bug_hunt_deploy_chain_issues.sh
#
# Files the triaged findings from the May 2026 deploy + daemon chain bug hunt
# (sibling sweep around GH#330) as GitHub issues, grouped by severity.
#
# Usage:
#   DRY_RUN=1 scripts/file_bug_hunt_deploy_chain_issues.sh   # preview titles only
#   scripts/file_bug_hunt_deploy_chain_issues.sh             # file for real
#
# Idempotency: this script CREATES issues; re-running it duplicates. Run once.
set -euo pipefail

REPO="cameronsjo/bosun"
DRY_RUN="${DRY_RUN:-0}"
COUNT=0

# file_issue <priority-label> <extra-labels-csv> <title> <body>
file_issue() {
  local prio="$1" extra="$2" title="$3" body="$4"
  local labels="${prio},type-bug"
  [[ -n "$extra" ]] && labels="${labels},${extra}"
  COUNT=$((COUNT + 1))
  if [[ "$DRY_RUN" == "1" ]]; then
    printf '[DRY-RUN %02d] (%s) %s\n' "$COUNT" "$labels" "$title"
    return 0
  fi
  gh issue create --repo "$REPO" --title "$title" --label "$labels" --body "$body" \
    && sleep 1  # be gentle on the API
}

SIB="> Sibling of #330 (deploy invariant false-fail). Found in the May 2026 deploy+daemon chain bug-hunt sweep."

# ───────────────────────────── P0 — outage / data loss ─────────────────────────────

file_issue priority-critical "" \
"reconcile: removeStaleFiles silently deletes live data (symlink Stat-follow + no protect-list + empty-source wipe)" \
"## Summary
Content-hash sync's \`removeStaleFiles\` deletes destination files that are not present in the source, but its notion of \"present in source\" is broken three ways, all of which destroy live data on the critical reconcile path.

## Evidence
- \`internal/reconcile/deploy.go:257-301\` (removeStaleFiles), \`internal/fileutil/fileutil.go:266-269\` (CopyDirIfChanged symlink skip)

## Three failure modes
1. **Symlink Stat/Lstat disagreement** — \`CopyDirIfChanged\` skips symlinks via \`d.Type()&os.ModeSymlink\` (Lstat), but \`removeStaleFiles\` does \`os.Stat(srcPath)\` which *follows* the link. A broken/relative symlink in the source makes the matching live target file look \"absent in source\" → it is deleted.
2. **Empty/symlink-only source wipes the target** — if the rendered source dir is empty (render glitch), \`CopyDirIfChanged\` writes nothing and \`removeStaleFiles\` deletes the entire live target. The deploy invariant then *passes* (it keys off source-emptiness), so compose-up runs against wiped config.
3. **No protect-list for runtime data** — a git-managed appdata dir also receives container runtime files (\`*.sqlite3\`, session files, \`*.log\`). Every reconcile deletes everything not in source, destroying live runtime state.

## Expected vs actual
Stale-removal should key off \"declared in source\" using consistent (Lstat) semantics, refuse to wipe a populated target from an empty source, and respect a protected-path set. Instead it deletes live data on every reconcile.

$SIB"

file_issue priority-critical "" \
"reconcile: rollback can never consume the backup (Backup writes a tarball, rollback expects loose files)" \
"## Summary
The pre-deploy backup is produced as a single \`configs.tar.gz\`, but the rollback path looks for *loose* compose files and nothing ever extracts the tarball. Rollback is a no-op on every real failure despite \"Backup saved\" success logs.

## Evidence
- \`internal/reconcile/backup.go:81,159\` — \`Backup()\` writes \`configs.tar.gz\`.
- \`internal/reconcile/compose.go:207-223\` — rollback stats \`filepath.Join(backupPath, filepath.Base(f))\` (loose files); no extraction exists anywhere.

## Repro
Deploy fails after a successful pre-deploy backup → rollback runs → \`os.Stat\` misses every loose path → \"No backup files found for rollback\" → broken deploy stays live.

## Expected vs actual
A failed deploy should restore the prior working compose files from the backup. Instead rollback silently does nothing — the backup format and the restore format were never reconciled.

$SIB"

file_issue priority-critical "" \
"reconcile: partial compose failure (1-of-N services) is reported as success and never retried" \
"## Summary
\`ComposeUpIsolated\` returns an error only when *all* compose files fail. A partial failure (one service down) returns \`nil\`, so the reconcile records success, clears the redeploy marker, sends a success alert, and never retries.

## Evidence
- \`internal/reconcile/compose.go:376-380\` — error only when \`summary.Failed == len(composeFiles)\`.
- \`internal/reconcile/reconcile.go:628-630,670\` — \`LastDeployedCommit\` set, \`NeedsRedeploy=false\`, success alert sent.

## Repro
30 compose files, 1 fails to start (missing backup so per-file rollback is skipped, or a genuine start failure). Deploy returns \`nil\`; failed containers stay down forever.

## Expected vs actual
A partial failure should keep \`NeedsRedeploy=true\`, not mark the commit clean, and retry next cycle. Instead it is indistinguishable from full success.

$SIB"

file_issue priority-critical "" \
"reconcile/ssh: remote deploy promotes unverified/partial tar-over-SSH transfers to live" \
"## Summary
Remote deploy infers success from two independent process exit codes and never runs the WrittenFiles/verify invariant the local path has. A truncated or zero-file transfer is promoted to the live target and \`compose up\` runs against it.

## Evidence
- \`internal/reconcile/ssh.go:225-263\` — \`tar -cf - | ssh 'tar -xf -'\`; success = both \`Wait()\`s nil, no \`pipefail\`, no post-transfer integrity check (no file count / checksum / byte count).
- \`internal/reconcile/reconcile.go:1409,1437-1482\` — \`deployRemote\` returns only \`error\`, never builds a \`*DeployResult\` or calls \`verifyTarget\`, so the invariant is effectively always skipped on remote targets.

## Repro
Local \`tar\` killed by SIGPIPE / network blip mid-stream → remote \`tar\` reaches a valid record boundary on a truncated archive and exits 0 → partial content promoted via the atomic \`mv\` → compose up against wrong files.

## Expected vs actual
Remote deploy should enforce the same \"files actually landed\" invariant as local. It enforces nothing. (Decrypted secrets ride this unverified stream — see also the host-key finding.)

$SIB"

file_issue priority-critical "" \
"daemon: a panic in the reconcile pipeline wedges single-flight forever (pushes silently stop deploying)" \
"## Summary
\`d.reconciling\` is set true at the start of the single-flight critical section and reset only on the *normal* return. Any panic in the pipeline unwinds without resetting it, so every subsequent trigger queues a \`pendingTrigger\` that never drains — the daemon silently stops deploying for its lifetime and \`/status\` is stuck \"reconciling\".

## Evidence
- \`internal/daemon/daemon.go:481-522\` — no \`defer func(){ recover; d.reconciling=false }()\` around the critical section.
- \`internal/sentry/sentry.go:146\` — \`Recover()\` is a no-op when \`BOSUN_SENTRY_DSN\` is empty (the default) and never resets daemon state.
- Trigger goroutines at \`socket.go:176\`, \`tcp.go:170\`, \`api.go:312\` have no \`defer Recover()\` at all.

## Repro
Trigger any panic in \`executeReconcile\` (nil-deref in a target reconciler, Docker SDK panic, template panic). The reconcile flag stays \`true\`; all future webhook/poll/manual triggers return \"queued\" and nothing drains them.

## Expected vs actual
A panic should be recovered (or at least reset the flag) so the daemon keeps deploying. Instead it wedges permanently.

$SIB"

file_issue priority-critical "" \
"reconcile: success state saved before post-deploy health verification — unhealthy deploy recorded as success, never retried, breaker disarmed" \
"## Summary
The success state (\`LastDeployedCommit=after\`, \`AttemptCount=0\`, \`NeedsRedeploy=false\`) is saved *before* \`verifyPostDeploy\` runs. When health verification fails, on-disk state already says success, so the next reconcile skips it, the circuit breaker is disarmed, and the failure alert is suppressed.

## Evidence
- \`internal/reconcile/reconcile.go:623-654\` — \`SaveState(...)\` at :632 precedes \`verifyPostDeploy\` at :648; on failure the success fields are never rolled back.
- \`internal/reconcile/state.go:112-113\` — \`ShouldAlert(0,0)\` returns false, so \`sendThrottledFailureAlert\` at :652 emits nothing.

## Repro
compose-up + health gate pass, then a declared container fails its HEALTHCHECK within \`HealthCheckTimeout\`. \`verifyPostDeploy\` errors, but next reconcile sees \`shouldSkipDeploy(after,after,...)==true\` and skips. Unhealthy containers are never re-addressed.

## Expected vs actual
An unhealthy deploy should be recorded as not-yet-successful (retry + breaker eligible + alert). Instead it is committed as success before the check that would catch it.

$SIB"

# ───────────────────────────── P1 — degraded / recovery / security ─────────────────────────────

file_issue priority-high "" \
"reconcile: expandAppdata misclassifies a symlinked service dir (Lstat IsDir=false) → wrong verify path → reconcile aborts" \
"## Summary
A service entry that is a symlink-to-directory is classified \`IsDir:false\` (non-following \`entry.IsDir()\`), routed through the single-file deploy branch (no writes), then the invariant's following \`os.Stat\` walks the dir, finds files, and aborts.

## Evidence
- \`internal/reconcile/discovery.go:94-99\` (expandAppdata, non-following \`entry.IsDir()\`) vs the verify path's following stat.
- Call site: \`internal/reconcile/reconcile.go:1314\`.

## Expected vs actual
Symlinked service dirs should be followed-and-synced or cleanly skipped. Instead shape misclassification feeds the wrong verify path and aborts the reconcile.

$SIB"

file_issue priority-high "" \
"fileutil: CopyDir/CopyDirIfChanged/CopyFileIfChanged take no context — cancellation not honored mid-sync" \
"## Summary
The content-hash copy loop runs the full \`filepath.WalkDir\` regardless of context cancellation; the \`ctx.Err()\` check happens only *after* the entire walk (and \`removeStaleFiles\`) complete.

## Evidence
- \`internal/fileutil/fileutil.go:259\` (CopyDirIfChanged — no ctx), \`internal/reconcile/deploy.go:152-164\` (ctx checked only after the walk).

## Repro
SIGTERM / deadline during a large sync → files keep being written for the full walk; the partial write set is already on disk and \`removeStaleFiles\` may have already deleted live files, yet the run reports cancellation.

## Expected vs actual
Cancellation should interrupt the per-file loop. Instead a cancelled reconcile keeps mutating the live target.

$SIB"

file_issue priority-high "" \
"reconcile: health-gate failure skips rollback entirely (ErrComposeUnhealthy short-circuit)" \
"## Summary
\`ComposeUpMultipleWithRollback\` re-runs compose-up on the *new* files first; when they come up unhealthy it returns \`ErrComposeUnhealthy\` and takes the early-return \"skip rollback\" branch before any backup file is touched. The broken deploy stays live.

## Evidence
- \`internal/reconcile/compose.go:181-192\` — \`errors.Is(deployErr, ErrComposeUnhealthy)\` returns before the rollback block at 194+.
- Called from \`internal/reconcile/reconcile.go:913\`.

## Expected vs actual
A failed health gate should restore the prior working state. Instead rollback is silently skipped for the unhealthy case.

$SIB"

file_issue priority-high "" \
"reconcile/ssh: remote deploy has no rollback wiring and treats ErrComposeUnhealthy as fatal (asymmetric with local)" \
"## Summary
The remote path never threads \`lastBackupPath\`/rollback, and a remote \`ErrComposeUnhealthy\` is treated as a hard fatal error rather than the recoverable warning the local path uses. Synced-but-broken compose files are left in place with no restore.

## Evidence
- \`internal/reconcile/reconcile.go:1470-1474\` (remote) vs the local \`ComposeUpIsolated\` flow.

## Expected vs actual
Remote and local failure handling should match (unhealthy = recoverable warning; real failure = rollback). Instead remote has no rollback and over-fails on unhealthy.

$SIB"

file_issue priority-high "" \
"reconcile/ssh: container/app stderr leaks into the SSH-transient classifier → docker compose up retried 3x" \
"## Summary
\`ComposeUpRemote\` wraps a container-start failure as an error string that includes docker compose stderr, then \`retryWithBackoff\` substring-matches the whole string via \`isTransientSSHError\`. App stderr like \"connection refused\" / \"i/o timeout\" (common while a service waits on its DB) is misread as a transient SSH error and the up is retried 3x against an already-partially-started stack.

## Evidence
- \`internal/reconcile/ssh.go:31-53\` (isTransientSSHError), \`internal/reconcile/compose.go:443-452\` (error wrapping incl. stderr).

## Expected vs actual
A genuine container-start failure should fail fast. Instead it is retried as if the SSH transport blipped.

$SIB"

file_issue priority-high "" \
"reconcile/ssh: non-idempotent retry reuses a fixed temp dir and swallows cleanup errors → partial files overlaid" \
"## Summary
\`tmpDir\` is computed once before \`retryWithBackoff\`; on a transient failure the cleanup \`ssh rm -rf tmpDir\` error is discarded, and since the host is the reason for the retry the cleanup often fails too. The retry then \`mkdir -p\` (no-op) and \`tar -xf\` overlays the new archive onto the partial leftovers; files removed between attempts persist and get \`mv\`'d to the live target.

## Evidence
- \`internal/reconcile/ssh.go:211-262\`.

## Expected vs actual
Each retry should extract into a pristine temp dir. Instead retries are non-idempotent.

$SIB"

file_issue priority-high "" \
"reconcile/ssh: non-atomic 'rm -rf <target> && mv <tmp> <target>' has a destructive window on FUSE/cross-device (Unraid)" \
"## Summary
The \"atomic\" remote replace deletes the live config dir first, then \`mv\`. On a different filesystem or a FUSE/bind mount (Unraid \`/mnt/user\`), the \`mv\` may fail or be non-atomic, leaving the target deleted/empty with no rollback. A connection drop between the two commands leaves the target gone.

## Evidence
- \`internal/reconcile/ssh.go:267-276\`.

## Expected vs actual
A failed/interrupted replace should never destroy the live target. Combined with the no-verify finding, a failed \`mv\` after a successful \`rm\` points containers at a vanished config dir.

$SIB"

file_issue priority-high "area-security" \
"reconcile/ssh: deploy SSH ignores BOSUN_SSH_KNOWN_HOSTS / BOSUN_SSH_INSECURE_HOST_KEY (only go-git honors them)" \
"## Summary
Host-key config is wired only into the go-git in-process clone/pull callback. Every exec'd \`ssh\`/\`scp\` deploy/sync/mkdir runs with no \`-o StrictHostKeyChecking\`, no \`-o UserKnownHostsFile\`, no \`IdentityFile\` — so it uses the *system* known_hosts and OS defaults. In a daemon container with empty system known_hosts, the secret-bearing tar stream is sent with no key pinning (MITM exposure).

## Evidence
- \`internal/reconcile/ssh.go\` (exec path: 130-143, 216, 226, 254, 268, 334, 348, 394) vs \`internal/reconcile/git.go:103-128\` (go-git callback only).

## Expected vs actual
One consistent host-key policy across git and deploy SSH. Instead deploy SSH silently diverges from configured policy.

$SIB"

file_issue priority-high "area-security" \
"daemon: HTTP webhook signature is skipped when no secret is set (fail-open) and the server binds all interfaces" \
"## Summary
When \`WebhookSecret == \"\"\` the entire signature-validation block is skipped, so any caller who can reach the HTTP port can trigger a full reconcile/deploy. The HTTP server binds \`:%d\` on all interfaces, not localhost.

## Evidence
- \`internal/daemon/server.go:198-221\` (handleWebhook), \`273-286\` (handleGitHubWebhook), \`369\` (handleManualTrigger), bind at \`server.go:82\`.

## Expected vs actual
Missing secret should fail closed (reject), and the bind should default to localhost. Instead it is an unauthenticated deploy trigger by default. (Distinct from #295/#296/#297 — this is the auth-bypass-by-default trigger surface.)

$SIB"

file_issue priority-high "" \
"daemon: /ready reports ready after a failed initial reconcile" \
"## Summary
The initial reconcile goroutine logs the error from \`TriggerReconcile\` and then unconditionally calls \`d.setReady(true)\`, so \`/ready\` returns 200 even though no reconcile has ever succeeded.

## Evidence
- \`internal/daemon/daemon.go:318-323\` — \`setReady(true)\` is outside any success check.

## Expected vs actual
\`/ready\` should reflect whether the daemon has successfully reconciled. Instead an orchestrator/LB sees it as healthy after a hard initial failure.

$SIB"

file_issue priority-high "" \
"daemon: drift detection and restart breaker are not multi-target fan-out aware (only the base state file/project)" \
"## Summary
Periodic \`runDriftCheck\` and \`runRestartBreaker\` read only \`d.config.ReconcileConfig.StateFile\` and \`.ProjectName\` (the base/default target). Named targets each write their own \`deploy-state-<name>.json\`, so their containers are never drift-checked or breaker-evaluated, and \`/health\` lies about them.

## Evidence
- \`internal/daemon/daemon.go:728-734,978,984-987,1187-1196\`; gap self-documented at \`reconcile.go:572-576\`.

## Expected vs actual
Drift/breaker/health should fan out per target. Instead they assume a single flat base config (distinct from #285's state-dir pre-creation).

$SIB"

file_issue priority-high "" \
"daemon: restart breaker loses its tripped entry when the container stops, and never resolves across 'docker start'" \
"## Summary
Two coupled defects in breaker resolution: (1) tripping the breaker stops the container, which removes it from \`collectRestartCounts\` (running/restarting only), so the \`Tripped:true\` entry is dropped on the next evaluation — no \"resolved\" alert, and a restart is treated as a brand-new baseline that can immediately re-trip. (2) Resolution requires \`count <= prev.RestartCount\`, but Docker's \`RestartCount\` is monotonic across \`docker start\`, so any post-trip restart keeps it \`Tripped\` permanently.

## Evidence
- \`internal/reconcile/restart_breaker.go:52-68,111-113,160-162\`; call site \`internal/daemon/daemon.go:993\`.

## Expected vs actual
A tripped breaker should persist until resolved, then emit a resolved alert. Instead the record is lost and/or never resolves.

$SIB"

file_issue priority-high "" \
"daemon: sendDriftAlert swallows the provider error but stamps DriftAlertedItems → critical drift alert lost for the cooldown" \
"## Summary
\`sendDriftAlert\` logs and discards a provider send failure, then the caller unconditionally records \`DriftAlertedItems[key]=now\`. \`ShouldAlertDrift\` then suppresses re-alert for the full \`DriftAlertCooldown\` (default 1h), so a failed critical (missing/unhealthy) drift alert is silently lost.

## Evidence
- \`internal/daemon/daemon.go:842-852\` (swallow) + \`1013-1032\` (unconditional stamp).

## Expected vs actual
A failed critical-drift alert should be retried next tick. Instead delivery failure is recorded as success.

$SIB"

file_issue priority-high "" \
"daemon: self-heal triggers with force=false, so image_mismatch/unhealthy drift is skipped → no-op self-heal loop" \
"## Summary
\`BOSUN_DRIFT_SELF_HEAL\` triggers \`TriggerReconcile(ctx, \"drift-self-heal\", false)\`. With no new commit, \`shouldSkipDeploy\` returns true and the skip-confirm only re-runs when \`hasMissingDeclaredServices\` (running-state only). For \`image_mismatch\`/\`unhealthy\` drift (container is running), the reconcile skips the deploy; drift persists and self-heal re-fires every \`DriftSelfHealCooldown\` (default 15m) doing nothing.

## Evidence
- \`internal/daemon/daemon.go:967\`, \`internal/reconcile/pure.go:14-46\`.

## Expected vs actual
Self-heal should re-apply declared state (force=true) to fix the drift. Instead it loops uselessly.

$SIB"

# ───────────────────────────── P2 — recoverable / rough ─────────────────────────────

file_issue priority-medium "" \
"fileutil: CopyFile chmod failure aborts the write after content is copied, leaving stale dst with a misleading error" \
"## Summary
\`CopyFile\` applies permissions after writing the temp file; if \`os.Chmod\` fails (FS without mode bits, race), the call returns a \"set permissions\" error, the temp is cleaned up, and the original \`dst\` is left stale — the error obscures that the content copy succeeded into the temp.

## Evidence
- \`internal/fileutil/fileutil.go:99-105\`.

$SIB"

file_issue priority-medium "" \
"reconcile: local backup failure leaves a partial/corrupt archive on disk (asymmetric with remote cleanup)" \
"## Summary
On a local \`Backup\` timeout / corrupt-archive failure, the partial \`backup-TS/\` + truncated \`configs.tar.gz\` are left on disk; the remote path removes them (\`backup.go:210-211\`) but the local path does not.

## Evidence
- \`internal/reconcile/backup.go:118-121\` — missing \`os.RemoveAll(backupPath)\` before the local-failure return.

$SIB"

file_issue priority-medium "" \
"reconcile: CleanupBackups counts corrupt/partial backup dirs toward retention and can evict a good backup" \
"## Summary
Retention cleanup filters purely on \`IsDir()\` + \`backup-\` prefix without validating \`configs.tar.gz\`, so a corrupt dir occupies a \`keep\` slot and can evict an older known-good backup.

## Evidence
- \`internal/reconcile/backup.go:236-262\`.

$SIB"

file_issue priority-medium "" \
"reconcile/ssh: sshCmd.Start() failure path never calls tarCmd.Wait() → zombie process + FD leak" \
"## Summary
When \`sshCmd.Start()\` fails after \`tarCmd.Start()\` succeeded, the code kills \`tar\` but never \`Wait()\`s it, leaking a zombie and its stdout pipe FD. Over a long-lived daemon with repeated start failures these accumulate.

## Evidence
- \`internal/reconcile/ssh.go:243-250\`.

$SIB"

file_issue priority-medium "" \
"reconcile/ssh: temp-dir cleanup reuses the cancelled context → orphaned remote temp dir" \
"## Summary
On cancellation, the \`rm -rf tmpDir\` cleanup is issued with the already-cancelled \`ctx\` (\`exec.CommandContext\`), so it returns immediately and the remote temp dir is orphaned. Cancellation classification is also incomplete (only \`DeadlineExceeded\` is special-cased).

## Evidence
- \`internal/reconcile/ssh.go:259\` (and 341, 399).

$SIB"

file_issue priority-medium "" \
"daemon: socket/TCP/API trigger goroutines are untracked by the WaitGroup and lack recover → leak + not awaited on shutdown" \
"## Summary
The socket/TCP/API trigger handlers spawn bare \`go func()\` calls (no \`wg.Add\`, no \`defer Recover()\`), unlike the HTTP webhook handlers which use the server WaitGroup. A burst leaks goroutines, and graceful shutdown never waits on them.

## Evidence
- \`internal/daemon/socket.go:176\`, \`internal/daemon/tcp.go:170\`, \`internal/daemon/api.go:312\` vs \`server.go\` (\`s.wg.Add(1)\`).

$SIB"

file_issue priority-medium "" \
"daemon: a drift type transition (unhealthy→missing) emits a false 'Drift Resolved' alert" \
"## Summary
Drift dedup keys are \`service:type\`. When a service transitions from \`unhealthy\` to (more severe) \`missing\`, \`ShouldAlertDrift\` sees \`svc:unhealthy\` absent → emits a \"Drift Resolved: svc:unhealthy\" alert in the same cycle as \"Drift Detected: svc:missing\", falsely signaling recovery for a service that is actually worse.

## Evidence
- \`internal/daemon/daemon.go:792-797,838-852\`, \`internal/reconcile/state.go:240-263\`.

$SIB"

# ───────────────────────────── P3 — latent / polish ─────────────────────────────

file_issue priority-low "" \
"fileutil: directory-only deploys are untracked in WrittenFiles → invariant can't verify created dirs" \
"## Summary
\`CopyDirIfChanged\` \`MkdirAll\`s directories but never adds them to \`written\`. A source with only (required) empty directories produces an empty write list; the invariant can't confirm the dirs landed and hook globs keyed off \`WrittenFiles\` see nothing changed.

## Evidence
- \`internal/fileutil/fileutil.go:277-279\` + \`internal/reconcile/verify.go\` zero-write branch.

$SIB"

file_issue priority-low "" \
"reconcile: BOSUN_HEALTH_GATE_TIMEOUT=0 cannot disable the gate (overloaded 0 vs default)" \
"## Summary
\`0\` is overloaded as both \"use default\" and an intended \"disable\". \`reconcile.go:901\` rewrites \`0→60s\` so the gate can't be disabled, and \`CheckCriticalContainerHealth\` with \`timeout=0\` does \`time.After(0)\` (immediate single check) — inconsistent with \`HealthCheckTimeout<=0\` which disables verification.

## Evidence
- \`internal/reconcile/healthgate.go:33-49\`, \`internal/reconcile/reconcile.go:899-902\`, \`internal/daemon/daemon.go:1701\`.

$SIB"

file_issue priority-low "" \
"reconcile: empty backup paths return 'Backup saved' success with an empty dir and skip verification" \
"## Summary
When none of the derived backup paths exist (fresh host), \`Backup\` early-returns success, skips \`VerifyBackup\`, sets \`lastBackupPath\` to an empty dir, and logs \"Backup saved\" — a content-free backup reported as real, so a later rollback silently finds nothing.

## Evidence
- \`internal/reconcile/backup.go:91-94\`.

$SIB"

file_issue priority-low "" \
"reconcile: path-aware skip with a pending NeedsRedeploy muddies deploy state" \
"## Summary
When a prior partial failure left \`NeedsRedeploy=true\` and a new commit touches no deploy-relevant paths, the path-aware skip block writes \`LastDeployedCommit=after\` + SaveState without clearing \`NeedsRedeploy\` or resetting attempt counters, leaving stale redeploy intent alongside a new \"deployed\" commit.

## Evidence
- \`internal/reconcile/reconcile.go:439-459\`.

$SIB"

echo ""
echo "Total issues processed: $COUNT (DRY_RUN=$DRY_RUN)"
