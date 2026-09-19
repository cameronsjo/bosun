#!/usr/bin/env bash
#
# Tests for upgrade-bosun.sh and upgrade-bosun-remote.sh. docker, ssh, scp, gh,
# caffeinate and GNU `date -d` are stubbed on PATH; nothing touches a real host.
#
#   bash scripts/upgrade-bosun_test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REMOTE="$SCRIPT_DIR/upgrade-bosun-remote.sh"
WRAPPER="$SCRIPT_DIR/upgrade-bosun.sh"
ROOT="$(mktemp -d "${TMPDIR:-/tmp}/bosun-upgrade-test.XXXXXX")"
trap 'rm -rf -- "$ROOT"' EXIT

INC_DIGEST="sha256:$(printf 'a%.0s' {1..64})"
CAND_DIGEST="sha256:$(printf 'b%.0s' {1..64})"
CANDIDATE="ghcr.io/cameronsjo/bosun:0.43.0@$CAND_DIGEST"
COMMIT_A="$(printf '1%.0s' {1..40})"
COMMIT_B="$(printf '2%.0s' {1..40})"

passed=0
fail() { printf 'FAIL [%s]: %s\n' "$CASE" "$*" >&2; printf -- '--- output\n%s\n' "$(cat "$OUT" 2>/dev/null)" >&2; exit 1; }
ok() { passed=$((passed + 1)); printf 'ok   %s\n' "$CASE"; }
assert_rc() { [[ "$RC" -eq "$1" ]] || fail "exit $RC, expected $1"; }
assert_out() { grep -qF -- "$1" "$OUT" || fail "output lacks: $1"; }
assert_no_out() { if grep -qF -- "$1" "$OUT"; then fail "output contains: $1"; fi; }
assert_calls() { grep -qF -- "$1" "$F/calls" || fail "no docker call matching: $1"; }
assert_no_calls() { if grep -qF -- "$1" "$F/calls"; then fail "unexpected docker call: $1"; fi; }
running_role() { cat "$F/running_role"; }

write_stubs() {
  local bin="$1"
  mkdir -p "$bin"
  # date: translate GNU `-d <RFC3339 Z>` to BSD; pass everything else through.
  cat > "$bin/date" <<'EOF'
#!/usr/bin/env bash
args=("$@") val=""
for ((i = 0; i < ${#args[@]}; i++)); do [[ "${args[$i]}" == -d ]] && val="${args[$((i + 1))]}"; done
if [[ -n "$val" ]]; then
  if [[ "$(uname)" == Darwin ]]; then exec /bin/date -j -u -f '%Y-%m-%dT%H:%M:%SZ' "${val%%.*}" +%s; fi
  exec /bin/date "$@"
fi
exec /bin/date "$@"
EOF
  cat > "$bin/docker" <<'EOF'
#!/usr/bin/env bash
# Fake docker. Scenario lives in $FAKE; every call is appended to $FAKE/calls.
F="$FAKE"; printf '%s\n' "$*" >> "$F/calls"
now() { /bin/date -u +%Y-%m-%dT%H:%M:%SZ; }
role_of_ref() { case "$1" in *"$FAKE_INC_DIGEST"*|inc-id|bosun:rollback-*) echo incumbent ;; *) echo candidate ;; esac; }
id_of_role() { [[ "$1" == incumbent ]] && echo inc-id || echo cand-id; }
case "$1" in
  compose)
    shift; files=() sub="" name=""
    while [[ $# -gt 0 ]]; do
      case "$1" in
        -f) files+=("$2"); shift 2 ;;
        -p) shift 2 ;;
        --name) name="$2"; shift 2 ;;
        config|run|up) [[ -z "$sub" ]] && sub="$1"; shift ;;
        *) shift ;;
      esac
    done
    case "$sub" in
      config) cat "$F/live.json" ;;
      run)
        role="${name##*-}"; override="${files[1]}"
        cp "$override" "$F/override-$role.yml"
        n=$(( $(cat "$F/runs-$role" 2>/dev/null || echo 0) + 1 )); echo "$n" > "$F/runs-$role"
        var="FAKE_RENDER_${role^^}"; [[ "${!var:-ok}" == ok ]] || { echo '{"level":"error","message":"render failed"}'; exit 1; }
        dir="$(sed -n 's#^ *- "\(.*\):/work"$#\1#p' "$override")"
        mkdir -p "$dir/staging/unraid"
        cvar="FAKE_CONTENT_${role^^}"; printf '%s\n' "${!cvar:-same}" > "$dir/staging/unraid/rendered.yml"
        kvar="FAKE_COMMITS_${role^^}"; read -r -a commits <<<"${!kvar:-$FAKE_DEFAULT_COMMIT}"
        commit="${commits[$((n - 1))]:-${commits[-1]}}"
        printf '{"level":"info","component":"reconcile","commit":"%s","message":"Reconcile pipeline completed"}\n' "$commit" ;;
      up)
        target=candidate
        [[ "${#files[@]}" -gt 1 ]] && target=incumbent
        var="FAKE_UP_FAIL_${target^^}"; [[ "${!var:-0}" == 1 ]] && exit 1
        echo "$target" > "$F/running_role"; now > "$F/started"; sleep 1 ;;
    esac ;;
  inspect)
    [[ -f "$F/running_role" ]] || exit 1
    r="$(cat "$F/running_role")"
    if [[ "$3" == *'{{.Image}}'* ]]; then echo "$(id_of_role "$r") running $(cat "$F/started") 0 bosun"
    else echo "restarts=0 status=running"; fi ;;
  image)
    [[ "${*: -1}" == inc-id ]] && echo "ghcr.io/cameronsjo/bosun@$FAKE_INC_DIGEST" || echo "ghcr.io/cameronsjo/bosun@$FAKE_CAND_DIGEST" ;;
  exec)
    r="$(cat "$F/running_role")"
    if [[ "$*" == *--version* ]]; then
      [[ "$r" == incumbent ]] && echo "bosun version 0.42.3" || echo "bosun version ${FAKE_CAND_VERSION:-0.43.0}"
    else
      var="FAKE_STATUS_${r^^}"
      case "${!var:-ok}" in
        ok) printf '{"state":"idle","last_reconcile":"%s","last_error":null}\n' "$(now)" ;;
        error) printf '{"state":"idle","last_reconcile":"%s","last_error":"deploy failed"}\n' "$(now)" ;;
        never) printf '{"state":"reconciling","last_reconcile":null,"last_error":null}\n' ;;
      esac
    fi ;;
  logs)
    r="$(cat "$F/running_role")"; var="FAKE_PANIC_${r^^}"
    [[ "${!var:-0}" == 1 ]] && echo "panic: runtime error" ; true ;;
  run)
    r="$(role_of_ref "${@: -3:1}")"; var="FAKE_NOALERTS_${r^^}"
    echo "Flags:"; [[ "${!var:-1}" == 1 ]] && echo "      --no-alerts   send no alerts"; true ;;
  tag) echo "$3" > "$F/tagged" ;;
  pull|rm) ;;
  ps) ;;
  *) echo "fake docker: unhandled $*" >&2; exit 2 ;;
esac
EOF
  chmod +x "$bin/date" "$bin/docker"
}

# new_case prepares a fake NAS: live compose, running incumbent, pinned candidate.
new_case() {
  CASE="$1"
  F="$ROOT/$CASE"; mkdir -p "$F/bin" "$F/compose" "$F/state" "$F/tmp"
  write_stubs "$F/bin"
  : > "$F/calls"
  echo incumbent > "$F/running_role"
  echo "2026-09-01T00:00:00Z" > "$F/started"
  : > "$F/compose/docker-compose.yml"
  jq -n --arg img "${2:-$CANDIDATE}" '{services: {bosun: {image: $img,
    environment: {TZ: "America/Chicago", BOSUN_REPO_URL: "git@github.com:x/homelab.git", BOSUN_INFRA_DIR: "unraid",
      BOSUN_TARGETS: "[{\"name\":\"unraid\"}]", BOSUN_SECRETS_FILE: "secrets.sops.yaml", SOPS_AGE_KEY_FILE: "/config/age-key.txt",
      WEBHOOK_SECRET: "s3cret", DISCORD_WEBHOOK_URL: "https://discord.example/hook", BOSUN_METRICS_TOKEN: "tok",
      BOSUN_SENTRY_DSN: "https://sentry.example/1", BOSUN_OTEL_ENDPOINT: "http://otel", DRY_RUN: "false"},
    volumes: [{source: "/mnt/user/appdata/bosun/age-key.txt", target: "/config/age-key.txt"},
      {source: "/mnt/user/appdata/bosun/homelab-deploy-key", target: "/config/deploy-key"},
      {source: "/var/run/docker.sock", target: "/var/run/docker.sock"},
      {source: "/mnt/user/appdata", target: "/mnt/appdata"}]}}}' > "$F/live.json"
  echo n > "$F/tty"
  unset "${!FAKE_@}" 2>/dev/null || true
  export FAKE="$F" FAKE_INC_DIGEST="$INC_DIGEST" FAKE_CAND_DIGEST="$CAND_DIGEST" FAKE_DEFAULT_COMMIT="$COMMIT_A"
}

run_remote() {
  RC=0
  OUT="$F/out"
  PATH="$F/bin:$PATH" BOSUN_UPGRADE_COMPOSE_DIR="$F/compose" BOSUN_UPGRADE_STATE_DIR="$F/state" \
    BOSUN_UPGRADE_TMP_ROOT="$F/tmp" BOSUN_UPGRADE_TTY="$F/tty" BOSUN_UPGRADE_POLL_SECONDS=1 \
    BOSUN_UPGRADE_UP_WAIT_SECONDS=2 bash "$REMOTE" --watch-timeout 3 "$@" > "$OUT" 2>&1 || RC=$?
}

history_has() { grep -qF -- "$1" "$F/state/history.log" || fail "history lacks: $1"; }
assert_clean_tmp() {
  local left; left="$(find "$F/tmp" -mindepth 1 -maxdepth 1 | head -n1)"
  [[ -z "$left" ]] || fail "shadow tmp dir left behind: $left"
  [[ ! -d "$F/state/lock" ]] || fail "lock left behind"
}

# ---- remote script ---------------------------------------------------------

new_case already-current "ghcr.io/cameronsjo/bosun:0.42.3@$INC_DIGEST"
run_remote
assert_rc 0; assert_out "VERDICT: ALREADY-CURRENT"; history_has "ALREADY-CURRENT"; assert_no_calls "compose -p"; ok

new_case tag-only-candidate "ghcr.io/cameronsjo/bosun:0.43.0"
run_remote
assert_rc 64; assert_out "carries no @sha256: digest"; assert_no_calls "tag "; ok

new_case expect-candidate-mismatch
run_remote --expect-candidate "ghcr.io/cameronsjo/bosun:0.43.1@$CAND_DIGEST"
assert_rc 64; assert_out "pin changed since provenance was checked"; ok

new_case upgraded
run_remote --yes
assert_rc 0; assert_out "RENDER-IDENTICAL"; assert_out "VERDICT: UPGRADED"
[[ "$(running_role)" == candidate ]] || fail "candidate not running"
[[ "$(cat "$F/tagged")" == "bosun:rollback-0.42.3" ]] || fail "rollback anchor not tagged"
[[ ! -f "$F/state/state" ]] || fail "state file left after success"
history_has "UPGRADED"; history_has "exit=0"; assert_calls "--pull never"; assert_clean_tmp; ok

new_case env-allowlist
run_remote --dry-run
assert_rc 0
for forbidden in DISCORD_WEBHOOK_URL WEBHOOK_SECRET _TOKEN SENTRY OTEL docker.sock s3cret; do
  for role in incumbent candidate; do
    if grep -qF -- "$forbidden" "$F/override-$role.yml"; then fail "$role override carries $forbidden"; fi
  done
done
grep -qF 'BOSUN_INFRA_DIR: "unraid"' "$F/override-candidate.yml" || fail "override lost BOSUN_INFRA_DIR"
grep -qF 'DRY_RUN: "true"' "$F/override-candidate.yml" || fail "override is not a dry run"
grep -qF '/mnt/user/appdata:/mnt/appdata:ro' "$F/override-candidate.yml" || fail "appdata not read-only"
assert_calls "reconcile --dry-run --no-alerts"
[[ "$(running_role)" == incumbent ]] || fail "dry run changed the running container"; assert_clean_tmp; ok

new_case render-differs-declined
export FAKE_CONTENT_CANDIDATE=RENDERED-SECRET-MARKER
run_remote --yes
assert_rc 0; assert_out "the renders differ"; assert_out "unraid/rendered.yml"; assert_no_out "RENDERED-SECRET-MARKER"
assert_out "DECLINED at RENDER-DIFFERS"; [[ "$(running_role)" == incumbent ]] || fail "declined run cut over"; ok

new_case no-baseline
export FAKE_NOALERTS_INCUMBENT=0
run_remote --dry-run
assert_rc 0; assert_out "RENDER-OK-NO-BASELINE"; [[ ! -f "$F/runs-incumbent" ]] || fail "incumbent rendered without a baseline"; ok

new_case candidate-lacks-flag
export FAKE_NOALERTS_CANDIDATE=0
run_remote --dry-run
assert_rc 64; assert_out "predates the canary"; ok

new_case candidate-failed
export FAKE_RENDER_CANDIDATE=fail
run_remote --yes
assert_rc 5; assert_out "VERDICT: CANDIDATE-FAILED"; [[ "$(running_role)" == incumbent ]] || fail "changed on candidate failure"
ls "$F/state/failures/"*shadow-candidate.log >/dev/null || fail "no shadow log kept"; assert_clean_tmp; ok

new_case harness-invalid
export FAKE_RENDER_INCUMBENT=fail
run_remote --yes
assert_rc 4; assert_out "VERDICT: HARNESS-INVALID"; ok

new_case commit-mismatch-retry
export FAKE_COMMITS_INCUMBENT="$COMMIT_B $COMMIT_A"
run_remote --dry-run
assert_rc 0; assert_out "re-running the incumbent once"; [[ "$(cat "$F/runs-incumbent")" == 2 ]] || fail "incumbent not re-run"; ok

new_case commit-mismatch-transient
export FAKE_COMMITS_INCUMBENT="$COMMIT_B $COMMIT_B"
run_remote --dry-run
assert_rc 75; assert_out "repo moved"; ok

new_case rolled-back
export FAKE_STATUS_CANDIDATE=error
run_remote --yes
assert_rc 1; assert_out "VERDICT: ROLLED-BACK"; [[ "$(running_role)" == incumbent ]] || fail "incumbent not restored"
grep -qF 'image: "bosun:rollback-0.42.3"' "$F/state/rollback.override.yml" || fail "rollback override missing"
history_has "ROLLED-BACK"; ls "$F/state/failures/"*-candidate.log >/dev/null || fail "failure detail not kept"; ok

new_case rolled-back-timeout
export FAKE_STATUS_CANDIDATE=never
run_remote --yes
assert_rc 1; assert_out "no reconcile finished within"; ok

new_case rolled-back-panic
export FAKE_PANIC_CANDIDATE=1
run_remote --yes
assert_rc 1; assert_out "the daemon panicked"; ok

new_case fault-not-upgrade
export FAKE_STATUS_CANDIDATE=error FAKE_STATUS_INCUMBENT=error
run_remote --yes
assert_rc 2; assert_out "VERDICT: FAULT-NOT-UPGRADE"; ok

new_case half-changed
export FAKE_STATUS_CANDIDATE=error FAKE_UP_FAIL_INCUMBENT=1
run_remote --yes
assert_rc 3; assert_out "VERDICT: HALF-CHANGED"; assert_out "bosun:rollback-0.42.3"
grep -qF 'phase=rollback' "$F/state/state" || fail "state not kept for a resume"; ok

new_case resume-watching
echo candidate > "$F/running_role"; echo "2026-09-19T00:00:00Z" > "$F/started"
printf '%s\n' "phase=watching" "project=bosun" "incumbent=ghcr.io/cameronsjo/bosun@$INC_DIGEST" \
  "incumbent_version=0.42.3" "rollback_tag=bosun:rollback-0.42.3" "candidate=$CANDIDATE" > "$F/state/state"
run_remote
assert_rc 0; assert_out "resuming an interrupted upgrade (phase=watching)"; assert_out "VERDICT: UPGRADED"
assert_no_calls "compose -p bosun-canary"; ok

new_case lock-held
mkdir -p "$F/state/lock"
run_remote --dry-run
assert_rc 75; assert_out "Another upgrade holds"; [[ -d "$F/state/lock" ]] || fail "removed someone else's lock"; ok

new_case yes-without-identical-still-prompts
export FAKE_NOALERTS_INCUMBENT=0
run_remote --yes
assert_rc 0; assert_out "Cut over bosun"; assert_out "DECLINED at RENDER-OK-NO-BASELINE"; ok

# ---- Mac wrapper -----------------------------------------------------------

new_wrapper_case() {
  new_case "$1"
  cat > "$F/bin/ssh" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$FAKE/ssh-calls"
cmd="${*: -1}"
case "$cmd" in
  true) exit 0 ;;
  mktemp*) echo /tmp/bosun-upgrade-remote.Ab12Cd ;;
  *--print-candidate*) echo "$FAKE_WRAP_CANDIDATE" ;;
  *--record-provenance-failure*) exit 5 ;;
  rm\ -f*) exit 0 ;;
  *) exit "${FAKE_REMOTE_RC:-0}" ;;
esac
EOF
  printf '#!/usr/bin/env bash\nexit 0\n' > "$F/bin/scp"
  cat > "$F/bin/gh" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$FAKE/gh-calls"
exit "${FAKE_GH_RC:-0}"
EOF
  cat > "$F/bin/caffeinate" <<'EOF'
#!/usr/bin/env bash
shift
exec "$@"
EOF
  chmod +x "$F/bin/ssh" "$F/bin/scp" "$F/bin/gh" "$F/bin/caffeinate"
  : > "$F/ssh-calls"; : > "$F/gh-calls"
  export FAKE_WRAP_CANDIDATE="$CANDIDATE"
}
run_wrapper() {
  RC=0; OUT="$F/out"
  PATH="$F/bin:$PATH" BOSUN_UPGRADE_WRAPPED="${WRAPPED-1}" BOSUN_UPGRADE_LOG_DIR="$F/logs" bash "$WRAPPER" "$@" > "$OUT" 2>&1 || RC=$?
}

new_wrapper_case wrapper-pass-through
export FAKE_REMOTE_RC=1
run_wrapper
assert_rc 1; grep -qF -- "--signer-workflow cameronsjo/bosun/.github/workflows/release-please.yml" "$F/gh-calls" || fail "provenance not pinned to the release workflow"
grep -qF -- "--expect-candidate" "$F/ssh-calls" || fail "candidate not pinned for the remote run"; ok

new_wrapper_case wrapper-provenance-failed
export FAKE_GH_RC=1
run_wrapper
assert_rc 5; assert_out "no valid build provenance"; grep -qF -- "--record-provenance-failure" "$F/ssh-calls" || fail "provenance failure not recorded on the NAS"
if grep -qF -- "-t unraid" "$F/ssh-calls"; then fail "ran the upgrade after a provenance failure"; fi; ok

new_wrapper_case wrapper-unpinned
export FAKE_WRAP_CANDIDATE="ghcr.io/cameronsjo/bosun:latest"
run_wrapper
assert_rc 64; assert_out "refusing an unpinned candidate"; [[ ! -s "$F/gh-calls" ]] || fail "verified an unpinned candidate"; ok

new_wrapper_case wrapper-foreign-repo
export FAKE_WRAP_CANDIDATE="ghcr.io/someone/bosun:1@$CAND_DIGEST"
run_wrapper
assert_rc 5; assert_out "is not from ghcr.io/cameronsjo/bosun"; ok

new_wrapper_case wrapper-drill-skips-provenance
export FAKE_GH_RC=1
run_wrapper --skip-provenance-for-drill
assert_rc 0; assert_out "provenance NOT checked"; [[ ! -s "$F/gh-calls" ]] || fail "drill ran gh"; ok

new_wrapper_case wrapper-connection-lost
export FAKE_REMOTE_RC=255
run_wrapper
assert_rc 75; assert_out "CONNECTION-LOST"; ok

new_wrapper_case wrapper-bad-host
run_wrapper --host "-oProxyCommand=evil"
assert_rc 64; assert_out "refusing ssh host"; ok

new_wrapper_case wrapper-logs-and-keeps-exit
export FAKE_REMOTE_RC=2
WRAPPED="" run_wrapper
assert_rc 2; ls "$F/logs/"*.log >/dev/null || fail "no log written"; ok

printf '\n%d passed\n' "$passed"
