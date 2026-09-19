#!/usr/bin/env bash
#
# Upgrade the bosun daemon on the NAS to the image pinned in homelab.
#
#   bash scripts/upgrade-bosun.sh [--host HOST] [--dry-run] [--yes]
#                                 [--watch-timeout SECONDS] [--skip-provenance-for-drill]
#
# Run it after a homelab PR has moved the pin in
# unraid/appdata/bosun/docker-compose.yml and bosun has synced that file to the
# NAS. It verifies the candidate's build provenance, then runs
# scripts/upgrade-bosun-remote.sh on the NAS over one ssh session: shadow
# render, cutover, watch, and automatic rollback. Read that script's header for
# the stages.
#
#   --host HOST        ssh host for the NAS (default: unraid, or $BOSUN_UPGRADE_HOST)
#   --dry-run          stages 0-2 only: provenance and shadow render; changes nothing
#   --yes              skip the cutover prompt when both renders are identical.
#                      Never skips the prompt when they differ or have no baseline.
#   --watch-timeout S  seconds to wait for the first reconcile (default 900)
#   --skip-provenance-for-drill
#                      rollback drill ONLY: accept an image that has no release
#                      provenance. Never use it for a real upgrade. It refuses
#                      --yes, and the NAS history marks the run as unverified.
#                      The shadow render still hands the image the age key.
#
# Exit codes (the remote script's, passed through). NOTE: 1 means ROLLED-BACK.
#   0 UPGRADED / ALREADY-CURRENT / clean dry run   1 ROLLED-BACK
#   2 FAULT-NOT-UPGRADE   3 HALF-CHANGED   4 HARNESS-INVALID
#   5 CANDIDATE-FAILED (includes failed provenance)
#  64 usage or configuration error   75 transient; retry. A connection lost
#     after cutover also exits 75: re-run, and the remote side resumes the watch.
#
# The full log goes to ~/Library/Logs/bosun-upgrade/<UTC time>.log.

set -euo pipefail

HOST="${BOSUN_UPGRADE_HOST:-unraid}"
REPO="cameronsjo/bosun"
IMAGE_REPO="ghcr.io/cameronsjo/bosun"
SIGNER_WORKFLOW="cameronsjo/bosun/.github/workflows/release-please.yml"
REF_RE='^[a-z0-9][a-z0-9./_-]*(:[A-Za-z0-9._-]+)?@sha256:[0-9a-f]{64}$'
LOG_DIR="${BOSUN_UPGRADE_LOG_DIR:-$HOME/Library/Logs/bosun-upgrade}"
SCRIPT_DIR="$(cd "$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")" && pwd)"
REMOTE_SCRIPT="$SCRIPT_DIR/upgrade-bosun-remote.sh"

DRY_RUN=0 ASSUME_YES=0 WATCH_TIMEOUT=900 SKIP_PROVENANCE=0

usage() { awk 'NR == 1 { next } /^#/ { sub(/^# ?/, ""); print; next } { exit }' "$0"; exit 0; }
fail() { printf 'ERROR %s\n' "$1" >&2; printf '\nVERDICT: %s (exit %s)\n' "$2" "$3"; exit "$3"; }

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -h|--help|help) usage ;;
      --dry-run) DRY_RUN=1; shift ;;
      --yes) ASSUME_YES=1; shift ;;
      --skip-provenance-for-drill) SKIP_PROVENANCE=1; shift ;;
      --host|--watch-timeout)
        [[ $# -ge 2 ]] || fail "$1 needs a value" USAGE 64
        if [[ "$1" == --host ]]; then HOST="$2"; else WATCH_TIMEOUT="$2"; fi
        shift 2 ;;
      *) fail "unknown argument: $1" USAGE 64 ;;
    esac
  done
  # A host starting with "-" would reach ssh as an option.
  [[ "$HOST" =~ ^[A-Za-z0-9._@][A-Za-z0-9._@-]*$ ]] || fail "refusing ssh host '$HOST'" USAGE 64
  [[ "$WATCH_TIMEOUT" =~ ^[0-9]+$ && "$WATCH_TIMEOUT" -gt 0 ]] || fail "--watch-timeout must be a positive number of seconds" USAGE 64
  if [[ "$SKIP_PROVENANCE" -eq 1 && "$ASSUME_YES" -eq 1 ]]; then
    fail "--yes is refused with --skip-provenance-for-drill; an unverified image always gets the prompt" USAGE 64
  fi
}

verify_provenance() {
  local candidate="$1"
  if [[ "$SKIP_PROVENANCE" -eq 1 ]]; then
    printf 'WARNING provenance NOT checked (--skip-provenance-for-drill). Drill use only.\n'
    return 0
  fi
  [[ "${candidate%%[:@]*}" == "$IMAGE_REPO" ]] || fail "candidate $candidate is not from $IMAGE_REPO" CANDIDATE-FAILED 5
  command -v gh >/dev/null || fail "gh is not installed; it verifies release provenance" CONFIG 64
  # Pinned to the release workflow, run from main, on a GitHub-hosted runner:
  # a copy of the workflow dispatched from another branch does not pass.
  if ! gh attestation verify "oci://$IMAGE_REPO@${candidate##*@}" -R "$REPO" --signer-workflow "$SIGNER_WORKFLOW" \
      --source-ref refs/heads/main --deny-self-hosted-runners > /dev/null; then
    remote_run --record-provenance-failure || true
    fail "no valid build provenance from $SIGNER_WORKFLOW for ${candidate##*@}" CANDIDATE-FAILED 5
  fi
  printf 'PASS provenance: built and attested by %s\n' "$SIGNER_WORKFLOW"
}

REMOTE_PATH=""

# remote_run runs the staged remote script without a TTY. Every argument is
# %q-quoted: the remote shell parses this string before the script validates it.
remote_run() {
  local quoted
  quoted="$(printf ' %q' "$@")"
  ssh -o BatchMode=yes "$HOST" "bash $REMOTE_PATH$quoted"
}

# shellcheck disable=SC2329  # invoked by the EXIT trap
remove_remote_script() {
  if [[ -n "$REMOTE_PATH" ]]; then
    ssh -o BatchMode=yes -o ConnectTimeout=10 "$HOST" "rm -f $REMOTE_PATH" 2>/dev/null || true
  fi
  return 0
}

main() {
  parse_args "$@"
  [[ -f "$REMOTE_SCRIPT" ]] || fail "missing $REMOTE_SCRIPT" CONFIG 64

  printf '==> Stage 0: preflight and provenance (host %s)\n' "$HOST"
  ssh -o BatchMode=yes -o ConnectTimeout=10 "$HOST" true || fail "cannot ssh to $HOST; nothing changed" TRANSIENT 75

  local candidate rc operator
  REMOTE_PATH="$(ssh -o BatchMode=yes "$HOST" 'mktemp /tmp/bosun-upgrade-remote.XXXXXX')" || fail "could not stage the remote script" TRANSIENT 75
  [[ "$REMOTE_PATH" =~ ^/tmp/bosun-upgrade-remote\.[A-Za-z0-9]+$ ]] || fail "unexpected remote temp path '$REMOTE_PATH'" CONFIG 64
  trap remove_remote_script EXIT
  scp -q "$REMOTE_SCRIPT" "$HOST:$REMOTE_PATH" || fail "could not copy the remote script" TRANSIENT 75

  candidate="$(remote_run --print-candidate)" || fail "could not read the pinned image on $HOST" TRANSIENT 75
  [[ "$candidate" =~ $REF_RE ]] || fail "pinned image '$candidate' carries no @sha256: digest; refusing an unpinned candidate" CONFIG 64
  printf '  candidate: %s\n' "$candidate"
  verify_provenance "$candidate"

  operator="${USER:-unknown}@$(hostname -s)"
  [[ "$operator" =~ ^[A-Za-z0-9._@-]{1,64}$ ]] || operator="unknown@mac"
  local args=(--expect-candidate "$candidate" --watch-timeout "$WATCH_TIMEOUT" --operator "$operator")
  [[ "$DRY_RUN" -eq 1 ]] && args+=(--dry-run)
  [[ "$ASSUME_YES" -eq 1 ]] && args+=(--yes)
  # The NAS history must show that this run was not verified.
  [[ "$SKIP_PROVENANCE" -eq 1 ]] && args+=(--provenance-skipped)

  # One session with a TTY for the cutover prompt.
  rc=0
  ssh -t "$HOST" "bash $REMOTE_PATH$(printf ' %q' "${args[@]}")" || rc=$?
  if [[ "$rc" -eq 255 ]]; then
    printf '\nThe ssh connection was lost. Re-run this script: if the cutover had started, it resumes the watch.\n'
    printf '\nVERDICT: CONNECTION-LOST (exit 75)\n'
    exit 75
  fi
  exit "$rc"
}

# Keep the Mac awake and keep a log, then run main once.
if [[ -z "${BOSUN_UPGRADE_WRAPPED:-}" ]]; then
  export BOSUN_UPGRADE_WRAPPED=1
  mkdir -p "$LOG_DIR"
  log="$LOG_DIR/$(date -u +%Y%m%dT%H%M%SZ).log"
  set +e
  caffeinate -i bash "$0" "$@" 2>&1 | tee "$log"
  rc="${PIPESTATUS[0]}"
  printf 'Log: %s\n' "$log"
  exit "$rc"
fi
main "$@"
