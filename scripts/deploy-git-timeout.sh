#!/usr/bin/env bash
#
# Deploy bosun 0.42.2 to a host and verify it landed.
#
#   bash scripts/deploy-git-timeout.sh [ssh-host] [--yes] [--dry-run]
#
# Why this is manual: bosun runs ghcr.io/cameronsjo/bosun:latest, a moving tag.
# An unchanged compose file means bosun's own reconcile never recreates the
# container, so a released image does not deploy itself. The pull is the step
# nothing else performs.
#
# Exit 0 = deployed (or already current) and verified.
#      1 = a check failed.
#     64 = could not run the checks. NOT a pass.
#
# set -e is deliberately off: the script runs every check and derives its
# verdict from a failure count. Each command that feeds a decision therefore
# checks its own status -- an unchecked capture is how a broken probe reports
# a healthy result.

set -uo pipefail

HOST="unraid"
CONTAINER="bosun"
WANT_VERSION="0.42.2"
ASSUME_YES=0
DRY_RUN=0

usage() {
  sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
  exit 0
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help|help) usage ;;
    --yes|-y) ASSUME_YES=1; shift ;;
    --dry-run|-n) DRY_RUN=1; shift ;;
    -*) printf 'unknown option: %s\n' "$1" >&2; exit 64 ;;
    *) HOST="$1"; shift ;;
  esac
done

# An ssh host beginning with "-" would be read as a flag.
if [[ ! "$HOST" =~ ^[A-Za-z0-9._@-]+$ || "$HOST" == -* ]]; then
  printf 'refusing hostname %q: expected an alphanumeric ssh host\n' "$HOST" >&2
  exit 64
fi

if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
  RED=$'\033[31m'; GREEN=$'\033[32m'; YELLOW=$'\033[33m'; BOLD=$'\033[1m'; OFF=$'\033[0m'
else
  RED=''; GREEN=''; YELLOW=''; BOLD=''; OFF=''
fi

fail_count=0
step() { printf '\n%s==>%s %s\n' "$BOLD" "$OFF" "$1"; }
pass() { printf '%sPASS%s %s\n' "$GREEN" "$OFF" "$1"; }
fail() { printf '%sFAIL%s %s\n' "$RED" "$OFF" "$1"; fail_count=$((fail_count + 1)); }
warn() { printf '%sNOTE%s %s\n' "$YELLOW" "$OFF" "$1"; }

LOGFILE="$(mktemp -t bosun-deploy)"
trap 'rm -f "$LOGFILE"' EXIT

# shellcheck disable=SC2029  # HOST, CONTAINER and COMPOSE_* are local script
# state and are meant to expand here, before the command reaches the remote.
remote() { ssh "$HOST" "$@"; }

# inspect_field returns one docker-inspect field, or empty on any failure. The
# caller MUST treat empty as "did not measure", never as a value.
inspect_field() {
  remote "docker inspect -f '$1' $CONTAINER"
}

print_rollback() {
  printf '\n%sRollback%s (run on %s):\n' "$BOLD" "$OFF" "$HOST"
  if [[ -n "${BEFORE_DIGEST:-}" ]]; then
    printf '  1. Pin the previous image in the compose file for service %s:\n' "${COMPOSE_SERVICE:-bosun}"
    printf '       image: %s\n' "$BEFORE_DIGEST"
    printf '  2. cd %s && docker compose up -d %s\n' "${COMPOSE_DIR:-<compose dir>}" "${COMPOSE_SERVICE:-bosun}"
    printf '  Pinning the digest and going through compose is the whole rollback:\n'
    printf '  a bare "docker run" would drop the ports, volumes, network and labels.\n'
  else
    printf '  The previous image digest was not captured, so there is no anchor to\n'
    printf '  pin. Recover from the compose file in git instead.\n'
  fi
}

abort() {
  printf '%sABORT%s %s\n' "$RED" "$OFF" "$1"
  [[ "${2:-}" == "changed" ]] && print_rollback
  exit 64
}

step "Preflight: reaching $HOST"
if ! remote true; then
  abort "cannot ssh to $HOST. Nothing was changed."
fi
pass "ssh works"

step "Baseline"
BEFORE_STARTED=$(inspect_field '{{.State.StartedAt}}')
BEFORE_IMAGE=$(inspect_field '{{.Image}}')
BEFORE_DIGEST=$(inspect_field '{{if .Image}}{{index .Config.Image}}{{end}}')
BEFORE_REPO_DIGEST=$(remote "docker image inspect -f '{{if .RepoDigests}}{{index .RepoDigests 0}}{{end}}' \$(docker inspect -f '{{.Image}}' $CONTAINER)" 2>&1)
if [[ -n "$BEFORE_REPO_DIGEST" && "$BEFORE_REPO_DIGEST" == *"@sha256:"* ]]; then
  BEFORE_DIGEST="$BEFORE_REPO_DIGEST"
fi

# Both baselines gate the abort. A captured-but-empty BEFORE_IMAGE would later
# read as "the digest changed" and print an empty rollback anchor.
if [[ -z "$BEFORE_STARTED" || -z "$BEFORE_IMAGE" ]]; then
  abort "could not inspect $CONTAINER on $HOST (no such container, or docker is down). Nothing was changed."
fi
printf '  StartedAt: %s\n  Image:     %s\n  Digest:    %s\n' \
  "$BEFORE_STARTED" "$BEFORE_IMAGE" "${BEFORE_DIGEST:-<none>}"

# Compose addresses SERVICES; the container name can differ via container_name:.
COMPOSE_DIR=$(remote "docker inspect -f '{{index .Config.Labels \"com.docker.compose.project.working_dir\"}}' $CONTAINER")
COMPOSE_SERVICE=$(remote "docker inspect -f '{{index .Config.Labels \"com.docker.compose.service\"}}' $CONTAINER")
[[ -n "$COMPOSE_DIR" ]] || abort "could not determine the compose working dir for $CONTAINER. Nothing was changed."
[[ -n "$COMPOSE_SERVICE" ]] || abort "could not determine the compose service name for $CONTAINER. Nothing was changed."
printf '  Compose:   %s (service %s)\n' "$COMPOSE_DIR" "$COMPOSE_SERVICE"

if [[ "$DRY_RUN" -eq 1 ]]; then
  step "Dry run — would execute"
  printf '  cd %s && docker compose pull %s\n' "$COMPOSE_DIR" "$COMPOSE_SERVICE"
  printf '  cd %s && docker compose up -d %s\n' "$COMPOSE_DIR" "$COMPOSE_SERVICE"
  echo "VERDICT: DRY RUN — nothing was changed"
  exit 0
fi

if [[ "$ASSUME_YES" -eq 0 ]]; then
  printf '\nThis recreates the %s container on %s. Continue? [y/N] ' "$CONTAINER" "$HOST"
  read -r reply
  [[ "$reply" == [yY]* ]] || { echo "Aborted by operator; nothing was changed."; exit 0; }
fi

step "Pulling ghcr.io/cameronsjo/bosun:latest"
if ! remote "cd '$COMPOSE_DIR' && docker compose pull $COMPOSE_SERVICE"; then
  abort "pull failed; the old container is still running and unharmed."
fi
if ! remote "cd '$COMPOSE_DIR' && docker compose up -d $COMPOSE_SERVICE"; then
  abort "compose up failed. The host is HALF-CHANGED: the new image is pulled and the container may be stopped or removed. Check 'docker compose ps' on $HOST." changed
fi

step "Waiting for the container to come up"
settled=0
for _ in $(seq 1 30); do
  running=$(inspect_field '{{.State.Running}}')
  if [[ "$running" == "true" ]]; then settled=1; break; fi
  sleep 2
done
if [[ "$settled" -eq 1 ]]; then
  pass "container is running"
else
  fail "container is not running after 60s; 'docker compose ps' on $HOST will say why"
fi

step "Check 1: the running image is the new one"
AFTER_STARTED=$(inspect_field '{{.State.StartedAt}}')
AFTER_IMAGE=$(inspect_field '{{.Image}}')
if [[ -z "$AFTER_STARTED" || -z "$AFTER_IMAGE" ]]; then
  # Empty means the inspect did not run. Comparing it would report "changed".
  fail "could not inspect $CONTAINER after the deploy — nothing was verified"
else
  printf '  StartedAt: %s\n  Image:     %s\n' "$AFTER_STARTED" "$AFTER_IMAGE"
  if [[ "$AFTER_IMAGE" != "$BEFORE_IMAGE" ]]; then
    if [[ "$AFTER_STARTED" != "$BEFORE_STARTED" ]]; then
      pass "image digest changed and StartedAt moved — the new build is running"
    else
      fail "image changed but StartedAt did not; the container was not recreated"
    fi
  elif [[ "$AFTER_STARTED" == "$BEFORE_STARTED" ]]; then
    # Re-running the script on an already-current host is the correct-state
    # case, not a failure. The version check below settles which it is.
    warn "nothing changed — already on this image? The version check decides."
  else
    warn "StartedAt moved but the image is identical (a restart, not an upgrade)"
  fi
fi

step "Check 2: the running binary reports $WANT_VERSION"
VERSION_OUT=$(remote "docker exec $CONTAINER bosun --version" 2>&1)
if grep -qF -- "$WANT_VERSION" <<<"$VERSION_OUT"; then
  pass "bosun --version reports $WANT_VERSION"
else
  fail "expected $WANT_VERSION, got: $VERSION_OUT"
fi

step "Check 3: the daemon is healthy"
# Capture to a file and check the transport. An unchecked capture puts the ssh
# error text into the variable, and every later grep then reads THAT -- which
# reports "no panics" over a log the script never saw.
if ! remote "docker logs --since 10m $CONTAINER" > "$LOGFILE" 2>&1; then
  fail "could not read logs from $HOST — the health check did not run"
elif [[ ! -s "$LOGFILE" ]]; then
  fail "log output was empty; a zero-line log cannot distinguish a healthy daemon from an unreachable one"
else
  # grep the FILE, not a pipeline. `printf | grep -q` SIGPIPEs the producer,
  # and under pipefail the pipeline then exits 141 on a SUCCESSFUL match --
  # inverting the panic check exactly when a panic appears early in a big log.
  if grep -q 'panic:' "$LOGFILE"; then
    fail "the daemon panicked:"
    grep -A15 'panic:' "$LOGFILE" | head -20
  else
    pass "no panics in the last 10 minutes"
  fi

  if grep -q 'remote_addr' "$LOGFILE"; then
    pass "request log carries remote_addr"
  else
    warn "no HTTP requests in this window yet"
  fi
fi

step "Deferred verification"
# on_recovery appears in the config-reload line, which runs on the next
# reconcile -- BOSUN_POLL_INTERVAL defaults to 3600s. Checking for it here
# would be a permanent NOTE, so it is deferred rather than faked.
warn "the on_recovery reload line appears on the next reconcile cycle (poll interval, up to 1h)"
warn "confirm it later with: bash scripts/verify-git-timeout.sh $HOST"

printf '\n%s%d check(s) failed%s\n' "$BOLD" "$fail_count" "$OFF"
if [[ "$fail_count" -gt 0 ]]; then
  print_rollback
  echo "VERDICT: FAIL"
  exit 1
fi
echo "VERDICT: PASS — bosun $WANT_VERSION is live on $HOST"
