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

# Deliberately no -e: this script runs every check and reports a verdict at the
# end, so a single non-matching grep must not abort the run. Failures are
# counted explicitly and the exit status is derived from that count.
set -uo pipefail

HOST="${1:-unraid}"
CONTAINER="${2:-bosun}"
WINDOW="${VERIFY_WINDOW:-2h}"

if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
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
# shellcheck disable=SC2029  # WINDOW and CONTAINER are local script inputs and
# are meant to expand here, not on the remote host.
if ! ssh "$HOST" "docker logs --since ${WINDOW} --timestamps ${CONTAINER}" > "$LOGFILE" 2>&1; then
  abort "could not read logs from ${HOST}. Nothing was verified."
fi
if [[ ! -s "$LOGFILE" ]]; then
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
if [[ "${req_total:-0}" -eq 0 ]]; then
  warn "no HTTP requests in this window; cannot verify attribution"
else
  req_attributed=$(grep 'HTTP request completed' "$LOGFILE" | grep -c 'remote_addr' || true)
  if [[ "$req_attributed" -eq "$req_total" ]]; then
    pass "all ${req_total} completed requests carry remote_addr"
  else
    fail "${req_attributed}/${req_total} requests carry remote_addr — the field is not unconditional"
  fi
fi

# 4. The dead webhook path is gone. Grep the PATH, not the field name: once
#    remote_addr exists it matches every request line and proves nothing.
step "Check 4: the retired /webhook/github-push path, if hit, 404s and names its sender"
gp_lines=$(grep 'HTTP request completed' "$LOGFILE" | grep 'github-push' || true)
if [[ -z "$gp_lines" ]]; then
  pass "no github-push requests in this window (the webhook was deleted)"
else
  # Present is not automatically a failure -- what matters is that it still 404s
  # (the daemon never registered it) and that the sender is attributable.
  gp_total=$(printf '%s\n' "$gp_lines" | wc -l | tr -d " ")
  gp_404=$(printf '%s\n' "$gp_lines" | grep -c '"status":404' || true)
  gp_attributed=$(printf '%s\n' "$gp_lines" | grep -c 'remote_addr' || true)
  warn "${gp_total} github-push request(s) seen — something is still pointed at the retired path:"
  printf '%s\n' "$gp_lines" | tail -5 >&2
  if [[ "$gp_404" -eq "$gp_total" && "$gp_attributed" -eq "$gp_total" ]]; then
    pass "all ${gp_total} 404'd and carry remote_addr, so the sender is identifiable"
  else
    fail "${gp_404}/${gp_total} returned 404 and ${gp_attributed}/${gp_total} carry remote_addr"
  fi
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
if [[ "$fail_count" -gt 0 ]]; then
  echo "VERDICT: FAIL"
  exit 1
fi
echo "VERDICT: PASS"
