#!/usr/bin/env bash
#
# Post-deploy verification for the git-timeout and alert-retraction change.
#
# Run this on a machine that can reach the bosun host over SSH, after the new
# image is live. It reads only; it triggers nothing and changes no state.
#
#   bash scripts/verify-git-timeout.sh [ssh-host] [container]
#
# Exit 0 = every check passed. Exit 1 = at least one failed. Exit 64 = the
# script could not run its checks at all, which is NOT a pass.

set -uo pipefail

HOST="${1:-unraid}"
CONTAINER="${2:-bosun}"
WINDOW="${VERIFY_WINDOW:-2h}"

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  RED=$'\033[31m'; GREEN=$'\033[32m'; YELLOW=$'\033[33m'; BOLD=$'\033[1m'; OFF=$'\033[0m'
else
  RED=''; GREEN=''; YELLOW=''; BOLD=''; OFF=''
fi

pass_count=0
fail_count=0

step() { printf '%s==>%s %s\n' "$BOLD" "$OFF" "$1" >&2; }
pass() { printf '%sPASS%s %s\n' "$GREEN" "$OFF" "$1" >&2; pass_count=$((pass_count + 1)); }
fail() { printf '%sFAIL%s %s\n' "$RED" "$OFF" "$1" >&2; fail_count=$((fail_count + 1)); }
warn() { printf '%sNOTE%s %s\n' "$YELLOW" "$OFF" "$1" >&2; }
abort() { printf '%sABORT%s %s\n' "$RED" "$OFF" "$1" >&2; exit 64; }

# Capture the log once. Every check reads this file, so a transport failure
# cannot masquerade as "no matches found" in one check and a pass in another.
LOGFILE="$(mktemp -t bosun-verify)"
trap 'rm -f "$LOGFILE"' EXIT

step "Fetching ${CONTAINER} logs from ${HOST} (last ${WINDOW})"
if ! ssh "$HOST" "docker logs --since ${WINDOW} --timestamps ${CONTAINER}" > "$LOGFILE" 2>&1; then
  abort "could not read logs from ${HOST}. Nothing was verified."
fi
if [ ! -s "$LOGFILE" ]; then
  abort "log output was empty. A zero-line log cannot distinguish a healthy daemon from an unreachable one."
fi
printf 'Captured %s log lines.\n' "$(wc -l < "$LOGFILE" | tr -d ' ')" >&2

# 1. The running binary is the new one.
step "Check 1: the deployed image carries this change"
if grep -q 'on_recovery' "$LOGFILE"; then
  pass "reload log reports on_recovery — the new binary is running"
elif grep -q 'Reloaded project config from repo' "$LOGFILE"; then
  fail "config reload logged but without on_recovery — the OLD binary is still running. A moving :latest tag needs an explicit 'docker compose pull && up -d'."
else
  warn "no config reload in this window; cannot confirm the binary version from logs alone. Check StartedAt instead:"
  warn "  ssh ${HOST} \"docker inspect -f '{{.State.StartedAt}}' ${CONTAINER}\""
fi

# 2. Timeout errors carry elapsed time, not just the declared bound.
step "Check 2: timeout errors report measured elapsed time"
if grep -q 'timed out' "$LOGFILE"; then
  if grep 'timed out' "$LOGFILE" | grep -q 'elapsed_ms'; then
    pass "timeout lines carry elapsed_ms alongside timeout_ms"
    grep 'timed out' "$LOGFILE" | tail -3 >&2
  else
    fail "a timeout was logged without elapsed_ms — the old error text is still in play"
  fi
else
  warn "no timeouts in this window (expected: they are rare). Nothing to check."
fi

# 3. Every completed request names its sender.
step "Check 3: HTTP requests carry remote_addr"
req_total=$(grep -c 'HTTP request completed' "$LOGFILE" || true)
if [ "${req_total:-0}" -eq 0 ]; then
  warn "no HTTP requests in this window; cannot verify attribution"
else
  req_attributed=$(grep 'HTTP request completed' "$LOGFILE" | grep -c 'remote_addr' || true)
  if [ "$req_attributed" -eq "$req_total" ]; then
    pass "all ${req_total} completed requests carry remote_addr"
  else
    fail "${req_attributed}/${req_total} requests carry remote_addr — the field is not unconditional"
  fi
fi

# 4. The dead webhook path is gone. Grep the PATH, not the field name: once
#    remote_addr exists it matches every request line and proves nothing.
step "Check 4: no requests to the retired /webhook/github-push path"
if grep 'HTTP request completed' "$LOGFILE" | grep -q 'github-push'; then
  fail "something is still posting to github-push:"
  grep 'HTTP request completed' "$LOGFILE" | grep 'github-push' | tail -5 >&2
else
  pass "no github-push requests in this window"
fi

# 5. Recovery alerts are reachable at all.
step "Check 5: recovery dispatch is not silently disabled"
if grep -q '"on_recovery":false' "$LOGFILE"; then
  fail "on_recovery is false — failure alerts cannot be retracted"
elif grep -q '"on_recovery":true' "$LOGFILE"; then
  pass "on_recovery is true"
else
  warn "no reload line carrying on_recovery in this window"
fi

printf '\n%s%d passed, %d failed%s\n' "$BOLD" "$pass_count" "$fail_count" "$OFF" >&2
if [ "$fail_count" -gt 0 ]; then
  echo "VERDICT: FAIL"
  exit 1
fi
echo "VERDICT: PASS"
