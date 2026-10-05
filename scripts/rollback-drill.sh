#!/usr/bin/env bash
# rollback-drill.sh — prove the canary's automatic rollback works.
#
#   bash scripts/rollback-drill.sh
#
# Verification step 6 of docs/plans/2026-09-19-bosun-upgrade-canary.md. Rollback
# is the canary's safety net, and a safety net nobody has dropped weight into is
# a guess. Re-run this after any change to stages 3-5.
#
# First run 2026-09-20 against the live NAS: exit 1, ROLLED-BACK, bosun restored
# to 0.43.1 healthy with 0 restarts, compose file byte-identical afterwards.
#
# NEEDS A TERMINAL. The canary refuses --yes together with
# --skip-provenance-for-drill, so the cutover prompt cannot be suppressed: an
# unverified image always gets a human. That guard is deliberate and this script
# does not route around it. You will be asked once; answer y to run the drill.
#
# It takes no arguments. Three environment knobs:
#   BOSUN_UPGRADE_HOST         ssh host for the NAS (default: unraid)
#   BOSUN_DIR                  bosun checkout to run from (default: this
#                              script's own repo). It must be on main.
#   BOSUN_DRILL_WATCH_TIMEOUT  seconds per watch (default: 180). The candidate
#                              watch and the incumbent's watch after rollback
#                              each get it, so a run takes about twice this.
#
# WHAT IT DOES
#   1. Refuses unless the daemon is idle, no upgrade state is recorded, and no
#      leftovers from an earlier drill are on the NAS.
#   2. Backs up the NAS compose file, then points it at a drill image and
#      asserts the edit landed.
#   3. Runs the canary. Expect: shadow render PASSES (the drill image renders
#      exactly like its base), cutover succeeds, then the watch times out
#      because the drill daemon stays up and never reconciles -- and the canary
#      rolls back to the incumbent's digest on its own.
#   4. Restores the compose file, on every exit path including Ctrl-C, and
#      compares its sha256 with the one recorded before the edit.
#
# EXPECTED RESULT: exit 1, VERDICT: ROLLED-BACK [provenance skipped: drill],
# bosun back on 0.43.1 healthy. Every other exit is in docs/troubleshooting.md
# under "Upgrade script verdicts":
#   exit 0  the drill never ran (ALREADY-CURRENT, or DECLINED at the prompt),
#     or the drill image passed its watch, which it must not. The VERDICT line
#     the canary printed says which; exit 0 alone does not.
#   exit 2  FAULT-NOT-UPGRADE: the rollback ran but its own watch failed. Raise
#     BOSUN_DRILL_WATCH_TIMEOUT and retry; the incumbent needs ~60s to reconcile.
#   exit 3  HALF-CHANGED: the rollback itself did not start, so bosun may be
#     down. Act on it now, using the recovery command the canary printed.
#   exit 5  CANDIDATE-FAILED: it never reached the watch; the drill image failed
#     the shadow render instead, which is a different bug.
#   exit 75 transient. A failed pull of the private drill image lands here: the
#     NAS needs a ghcr.io read credential for it.
#
# The drill image is ghcr.io/cameronsjo/bosun-drill, a PRIVATE package separate
# from the real one, so nothing deliberately broken can ever be pulled by
# something expecting a bosun release. It is the 0.43.1 image with one shim:
# `bosun daemon` becomes `sleep infinity`; everything else passes through. The
# shadow render hands that unverified image the age key, exactly as it would a
# real candidate, so only an image you built yourself belongs here.
#
# AFTERWARDS the canary leaves /mnt/user/appdata/bosun-upgrade/rollback.override.yml
# in place by design, so the rolled-back image keeps running. This script tells
# you the two commands to clear it. It does not clear it for you: that is a
# deliberate decision point, not cleanup.

set -euo pipefail

case "${1:-}" in
  -h|--help|help) awk 'NR == 1 { next } /^#/ { sub(/^# ?/, ""); print; next } { exit }' "$0"; exit 0 ;;
  "") : ;;
  *) echo "rollback-drill.sh takes no arguments (got '$1'); --help lists the environment knobs." >&2; exit 64 ;;
esac

HOST="${BOSUN_UPGRADE_HOST:-unraid}"
# A host starting with "-" would reach ssh as an option.
[[ "$HOST" =~ ^[A-Za-z0-9._@][A-Za-z0-9._@-]*$ ]] || { echo "refusing ssh host '$HOST'" >&2; exit 64; }
# Derive the repo from this script's own location, resolving symlinks: a
# hardcoded path breaks for anyone whose checkout lives elsewhere, and
# dirname "$BASH_SOURCE" alone yields the symlink's directory when invoked
# through one on PATH.
SELF="$(cd "$(dirname "$(readlink -f "${BASH_SOURCE[0]}" 2>/dev/null || printf '%s' "${BASH_SOURCE[0]}")")" && pwd -P)"
REPO="${BOSUN_DIR:-$(dirname "$SELF")}"
NAS_FILE=/mnt/user/appdata/bosun/docker-compose.yml
BACKUP="$NAS_FILE.predrill"
OVERRIDE=/mnt/user/appdata/bosun-upgrade/rollback.override.yml
DRILL_REF='ghcr.io/cameronsjo/bosun-drill@sha256:4419ea44951940705f76d1d1e32a17a0725bfcc3b592c6173c43f0be114e02a0'
WATCH="${BOSUN_DRILL_WATCH_TIMEOUT:-180}"
[[ "$WATCH" =~ ^[0-9]+$ && "$WATCH" -gt 0 ]] ||
  { echo "BOSUN_DRILL_WATCH_TIMEOUT must be a positive number of seconds" >&2; exit 64; }

on_nas() { ssh -o BatchMode=yes -o ConnectTimeout=10 "$HOST" "$1"; }

if ! (: < /dev/tty) 2>/dev/null; then
  echo "Run this from a terminal: the canary prompts for the unverified image." >&2
  exit 64
fi

backed_up=0
restored=0
pre_sha=""
# restore does nothing until the backup exists, so an exit during preflight does
# not tell the operator to hand-restore a file that was never touched. It marks
# itself done only on success, so a failed explicit call is retried by the trap.
restore() {
  [[ "$backed_up" -eq 1 && "$restored" -eq 0 ]] || return 0
  local now=""
  echo
  echo "==> Restoring the NAS compose file"
  now="$(on_nas "[ -f $BACKUP ] && mv -f $BACKUP $NAS_FILE && sha256sum $NAS_FILE")" || {
    echo "COULD NOT RESTORE. The backup is at $BACKUP on $HOST -- move it back by hand." >&2
    return 0
  }
  restored=1
  now="${now%% *}"
  if [[ "$now" == "$pre_sha" ]]; then
    echo "  restored, sha256 ${now:0:12} matches the file recorded before the drill."
  else
    echo "  WARNING sha256 is ${now:0:12}, expected ${pre_sha:0:12}. What is on $HOST is not what the drill replaced; check it against git." >&2
  fi
  return 0
}
trap restore EXIT
trap 'restore; exit 130' INT
trap 'restore; exit 143' TERM

cd "$REPO"
echo "==> Updating the canary scripts"
git fetch -q origin
git merge --ff-only origin/main ||
  { echo "$REPO is not a fast-forward of origin/main. Run the drill from a checkout on main, or point BOSUN_DIR at one." >&2; exit 64; }

echo "==> Preflight"
state="$(on_nas "docker exec bosun bosun daemon-status --json" | sed -n 's/.*"state": *"\([a-z]*\)".*/\1/p')" || true
[[ "$state" == idle ]] || { echo "daemon is '${state:-unreadable}', not idle. Wait and retry." >&2; exit 75; }
on_nas "[ -e /mnt/user/appdata/bosun-upgrade/state ]" >/dev/null 2>&1 &&
  { echo "an upgrade is already recorded on $HOST; resolve it before drilling." >&2; exit 64; }
on_nas "[ -e $BACKUP ]" >/dev/null 2>&1 &&
  { echo "$BACKUP already exists on $HOST, so an earlier drill never restored. Move it back by hand first: this run would overwrite it." >&2; exit 64; }
on_nas "[ -e $OVERRIDE ]" >/dev/null 2>&1 &&
  { echo "$OVERRIDE is still in place on $HOST from an earlier drill. Clear it first (the last run printed the two commands)." >&2; exit 64; }
before="$(on_nas "docker exec bosun bosun --version | head -1")" || true
pre_sha="$(on_nas "sha256sum $NAS_FILE")" || true
pre_sha="${pre_sha%% *}"
[[ "$pre_sha" =~ ^[0-9a-f]{64}$ ]] || { echo "could not read the sha256 of $NAS_FILE on $HOST." >&2; exit 75; }
echo "  daemon idle, running: ${before:-unknown}, compose sha256 ${pre_sha:0:12}"

echo "==> Pointing the NAS compose at the drill image"
on_nas "cp -p $NAS_FILE $BACKUP"
backed_up=1
on_nas "sed -i 's|^\( *image: \)ghcr.io/cameronsjo/bosun.*|\1$DRILL_REF|' $NAS_FILE"
# sed -i exits 0 when it matches nothing. Without this the canary would run
# against the real production pin and report ALREADY-CURRENT as a drill failure.
on_nas "grep -Fq '$DRILL_REF' $NAS_FILE" ||
  { echo "the drill image is not in $NAS_FILE after the edit; the sed matched no image line (quoted, interpolated, or indented differently). Nothing was upgraded." >&2; exit 64; }
on_nas "grep -n 'image: ghcr' $NAS_FILE"

echo
echo "==> Running the canary. Answer y at the prompt to run the drill."
rc=0
bash "$REPO/scripts/upgrade-bosun.sh" --skip-provenance-for-drill --watch-timeout "$WATCH" || rc=$?

restore

echo
echo "==> Result: exit $rc"
case "$rc" in
  1) echo "PASS  ROLLED-BACK -- the rollback works." ;;
  0) echo "FAIL  read the VERDICT line above: ALREADY-CURRENT or DECLINED means the drill never ran; UPGRADED means the drill image passed its watch, which it must not." ;;
  2) echo "PARTIAL  rollback ran, its own watch failed. Retry with BOSUN_DRILL_WATCH_TIMEOUT=300." ;;
  3) echo "ACT NOW  HALF-CHANGED -- the rollback did not start and bosun may be down. Run the recovery command the canary printed above." ;;
  5) echo "FAIL  CANDIDATE-FAILED -- the drill image failed the shadow render and never reached the watch. Check the shim." ;;
  75) echo "RETRY  transient. A failed pull of the private drill image lands here: the NAS needs a ghcr.io read credential." ;;
  *) echo "Unexpected exit $rc. Read docs/troubleshooting.md for this verdict." ;;
esac

echo
echo "State now:"
on_nas "docker exec bosun bosun --version | head -1; docker inspect bosun --format 'restarts={{.RestartCount}} status={{.State.Status}}'" || true
on_nas "[ -e $OVERRIDE ]" >/dev/null 2>&1 && cat <<NEXT

An override is in place, pinning the rolled-back image. Ignore the canary's
closing line about reverting the pin in homelab: this was a drill, the pin there
was never moved and must not be touched. The NAS compose file is already back,
so clearing the override is two commands -- run them only when the daemon is
idle:

   ssh $HOST 'docker exec bosun bosun daemon-status --json'   # state must be idle
   ssh $HOST 'rm $OVERRIDE && cd /mnt/user/appdata/bosun && docker compose up -d'
NEXT
exit "$rc"
