#!/usr/bin/env bash
#
# Deploy bosun 0.42.2 to unraid and verify it, in one pass.
#
#   bash scripts/deploy-git-timeout.sh [ssh-host]
#
# Why this is manual: bosun runs ghcr.io/cameronsjo/bosun:latest, a moving tag.
# An unchanged compose file means bosun's own reconcile never recreates the
# container, so a released image does not deploy itself. The pull is the step
# nothing else performs.
#
# Exit 0 = deployed and verified. 1 = a check failed. 64 = could not run.

set -uo pipefail

HOST="${1:-unraid}"
CONTAINER="bosun"
WANT_VERSION="0.42.2"

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
abort() { printf '%sABORT%s %s\n' "$RED" "$OFF" "$1"; exit 64; }

# shellcheck disable=SC2029  # HOST, CONTAINER and COMPOSE_DIR are local script
# state and are meant to expand here, before the command reaches the remote.
remote() { ssh "$HOST" "$@"; }

step "Preflight: reaching $HOST"
remote true || abort "cannot ssh to $HOST. Nothing was changed."
pass "ssh works"

step "Baseline (record this before changing anything)"
BEFORE_STARTED=$(remote "docker inspect -f '{{.State.StartedAt}}' $CONTAINER" 2>/dev/null)
BEFORE_IMAGE=$(remote "docker inspect -f '{{.Image}}' $CONTAINER" 2>/dev/null)
[[ -n "$BEFORE_STARTED" ]] || abort "could not inspect the $CONTAINER container"
printf '  StartedAt: %s\n  Image:     %s\n' "$BEFORE_STARTED" "$BEFORE_IMAGE"

# Find the compose project directory rather than assuming it.
COMPOSE_DIR=$(remote "docker inspect -f '{{index .Config.Labels \"com.docker.compose.project.working_dir\"}}' $CONTAINER" 2>/dev/null)
[[ -n "$COMPOSE_DIR" ]] || abort "could not determine the compose working dir for $CONTAINER"
printf '  Compose:   %s\n' "$COMPOSE_DIR"

step "Pulling ghcr.io/cameronsjo/bosun:latest"
# RestartCount does not move on a recreate, and the compose file is unchanged,
# so StartedAt is the only signal that says the new image is running.
remote "cd '$COMPOSE_DIR' && docker compose pull $CONTAINER" || abort "pull failed; the old container is still running and unharmed"
remote "cd '$COMPOSE_DIR' && docker compose up -d $CONTAINER" || abort "up failed; check 'docker compose logs $CONTAINER' on $HOST"

step "Waiting for the daemon to settle"
sleep 15

step "Check 1: the container actually restarted"
AFTER_STARTED=$(remote "docker inspect -f '{{.State.StartedAt}}' $CONTAINER" 2>/dev/null)
AFTER_IMAGE=$(remote "docker inspect -f '{{.Image}}' $CONTAINER" 2>/dev/null)
printf '  StartedAt: %s\n  Image:     %s\n' "$AFTER_STARTED" "$AFTER_IMAGE"
if [[ "$AFTER_STARTED" != "$BEFORE_STARTED" ]]; then
  pass "StartedAt moved -- the container was recreated"
else
  fail "StartedAt is unchanged; the pull did not take effect"
fi
if [[ "$AFTER_IMAGE" != "$BEFORE_IMAGE" ]]; then
  pass "image digest changed"
else
  warn "image digest unchanged -- already on this build?"
fi

step "Check 2: the running binary reports $WANT_VERSION"
VERSION_OUT=$(remote "docker exec $CONTAINER bosun --version" 2>&1)
if printf '%s' "$VERSION_OUT" | grep -q "$WANT_VERSION"; then
  pass "bosun --version reports $WANT_VERSION"
else
  fail "expected $WANT_VERSION, got: $VERSION_OUT"
fi

step "Check 3: the new code is live in the logs"
sleep 20
LOGS=$(remote "docker logs --since 5m $CONTAINER" 2>&1)
if printf '%s' "$LOGS" | grep -q 'on_recovery'; then
  pass "reload log reports on_recovery -- the retraction gate is wired"
else
  warn "no config reload yet (it runs on the next reconcile cycle); re-run scripts/verify-git-timeout.sh later"
fi

if printf '%s' "$LOGS" | grep -q 'remote_addr'; then
  pass "request log carries remote_addr"
else
  warn "no HTTP requests yet in this window"
fi

step "Check 4: nothing is wedged"
if printf '%s' "$LOGS" | grep -q 'panic:'; then
  fail "the daemon panicked -- see 'docker logs $CONTAINER' on $HOST"
  printf '%s\n' "$LOGS" | grep -A15 'panic:' | head -20
else
  pass "no panics in the last 5 minutes"
fi

printf '\n%s%d check(s) failed%s\n' "$BOLD" "$fail_count" "$OFF"
if [[ "$fail_count" -gt 0 ]]; then
  # shellcheck disable=SC2016  # the backticks are literal, quoting a command
  # for the operator to read -- not a substitution.
  printf 'Rollback: on %s, `cd %s && docker compose down %s && docker run` the previous digest %s\n' \
    "$HOST" "$COMPOSE_DIR" "$CONTAINER" "$BEFORE_IMAGE"
  echo "VERDICT: FAIL"
  exit 1
fi
echo "VERDICT: PASS -- bosun $WANT_VERSION is live"
echo "Follow up later with: bash scripts/verify-git-timeout.sh $HOST"
