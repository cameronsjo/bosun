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
# --skip-provenance-for-drill: an unverified image always gets a human prompt.
# That guard is deliberate and this script does not route around it. You will
# be asked once; answer y to run the drill.
#
# WHAT IT DOES
#   1. Refuses unless the daemon is idle and no upgrade state is recorded.
#   2. Backs up the NAS compose file, then points it at a drill image.
#   3. Runs the canary. Expect: shadow render PASSES (the drill image renders
#      exactly like its base), cutover succeeds, then the watch times out
#      because the drill daemon stays up and never reconciles -- and the canary
#      rolls back to bosun:rollback-0.43.1 on its own.
#   4. Restores the compose file, on every exit path including Ctrl-C.
#
# EXPECTED RESULT: exit 1, VERDICT: ROLLED-BACK, bosun back on 0.43.1 healthy.
#   exit 2 FAULT-NOT-UPGRADE means the rollback ran but its own watch failed --
#     raise --watch-timeout and retry; the incumbent needs ~60s to reconcile.
#   exit 5 CANDIDATE-FAILED means it never reached the watch; the drill image
#     failed the shadow render instead, which is a different bug.
#
# The drill image is ghcr.io/cameronsjo/bosun-drill, a PRIVATE package separate
# from the real one, so nothing deliberately broken can ever be pulled by
# something expecting a bosun release. It is the 0.43.1 image with one shim:
# `bosun daemon` becomes `sleep infinity`; everything else passes through.
#
# AFTERWARDS the canary leaves /mnt/user/appdata/bosun-upgrade/rollback.override.yml
# in place by design, so the rolled-back image keeps running. This script tells
# you the two commands to clear it. It does not clear it for you: that is a
# deliberate decision point, not cleanup.

set -euo pipefail

HOST="${BOSUN_UPGRADE_HOST:-unraid}"
# Derive the repo from this script's own location, resolving symlinks: a
# hardcoded path breaks for anyone whose checkout lives elsewhere, and
# dirname "$BASH_SOURCE" alone yields the symlink's directory when invoked
# through one on PATH.
SELF="$(cd "$(dirname "$(readlink -f "${BASH_SOURCE[0]}" 2>/dev/null || printf '%s' "${BASH_SOURCE[0]}")")" && pwd)"
REPO="${BOSUN_DIR:-$(dirname "$SELF")}"
NAS_FILE=/mnt/user/appdata/bosun/docker-compose.yml
BACKUP="$NAS_FILE.predrill"
DRILL_REF='ghcr.io/cameronsjo/bosun-drill@sha256:4419ea44951940705f76d1d1e32a17a0725bfcc3b592c6173c43f0be114e02a0'
WATCH="${DRILL_WATCH_TIMEOUT:-180}"

on_nas() { ssh -o BatchMode=yes "$HOST" "$1"; }

if ! (: < /dev/tty) 2>/dev/null; then
  echo "Run this from a terminal: the canary prompts for the unverified image." >&2
  exit 64
fi

restored=0
restore() {
  [ "$restored" -eq 1 ] && return 0
  restored=1
  echo
  echo "==> Restoring the NAS compose file"
  on_nas "[ -f $BACKUP ] && mv -f $BACKUP $NAS_FILE && sha256sum $NAS_FILE" || {
    echo "COULD NOT RESTORE. The backup is at $BACKUP on $HOST -- move it back by hand." >&2
    return 0
  }
}
trap restore EXIT INT TERM

cd "$REPO"
echo "==> Updating the canary scripts"
git fetch -q origin
git merge --ff-only origin/main

echo "==> Preflight"
state=$(on_nas "docker exec bosun bosun daemon-status --json" | sed -n 's/.*"state": *"\([a-z]*\)".*/\1/p')
[ "$state" = idle ] || { echo "daemon is '${state:-unknown}', not idle. Wait and retry." >&2; exit 75; }
on_nas "ls /mnt/user/appdata/bosun-upgrade/state" >/dev/null 2>&1 &&
  { echo "an upgrade is already recorded; resolve it before drilling." >&2; exit 64; }
before=$(on_nas "docker exec bosun bosun --version | head -1")
echo "  daemon idle, running: $before"

echo "==> Pointing the NAS compose at the drill image"
on_nas "cp -p $NAS_FILE $BACKUP"
on_nas "sed -i 's|^\( *image: \)ghcr.io/cameronsjo/bosun.*|\1$DRILL_REF|' $NAS_FILE"
on_nas "grep -n 'image: ghcr' $NAS_FILE"

echo
echo "==> Running the canary. Answer y at the prompt to run the drill."
rc=0
bash scripts/upgrade-bosun.sh --skip-provenance-for-drill --watch-timeout "$WATCH" || rc=$?

restore

echo
echo "==> Result: exit $rc"
case "$rc" in
  1) echo "PASS  ROLLED-BACK -- the rollback works." ;;
  0) echo "FAIL  the drill image passed its watch, which it must not. Check the shim." ;;
  2) echo "PARTIAL  rollback ran, its own watch failed. Retry with DRILL_WATCH_TIMEOUT=300." ;;
  *) echo "Unexpected. Read docs/troubleshooting.md for this verdict." ;;
esac

echo
echo "State now:"
on_nas "docker exec bosun bosun --version | head -1; docker inspect bosun --format 'restarts={{.RestartCount}} status={{.State.Status}}'" || true
on_nas "ls /mnt/user/appdata/bosun-upgrade/rollback.override.yml" 2>/dev/null && cat <<NEXT

An override is in place, pinning the rolled-back image. The pin in git is
already correct, so clearing it is two commands -- run them only when the
daemon is idle:

   ssh $HOST 'docker exec bosun bosun daemon-status --json'   # state must be idle
   ssh $HOST 'rm /mnt/user/appdata/bosun-upgrade/rollback.override.yml && cd /mnt/user/appdata/bosun && docker compose up -d'
NEXT
exit "$rc"
