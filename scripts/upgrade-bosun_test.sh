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
REAL_DOCKER="$(command -v docker || true)"   # before any stub reaches PATH
trap 'rm -rf -- "$ROOT"' EXIT

INC_DIGEST="sha256:$(printf 'a%.0s' {1..64})"
CAND_DIGEST="sha256:$(printf 'b%.0s' {1..64})"
INC_ID="sha256:$(printf 'c%.0s' {1..64})"
CAND_ID="sha256:$(printf 'd%.0s' {1..64})"
OTHER_ID="sha256:$(printf 'e%.0s' {1..64})"
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
  # date: GNU `-d` on Linux; on macOS, translate RFC 3339 (fraction, Z or
  # +-HH:MM offset) to BSD date. Everything else passes through.
  cat > "$bin/date" <<'EOF'
#!/usr/bin/env bash
args=("$@") val=""
for ((i = 0; i < ${#args[@]}; i++)); do [[ "${args[$i]}" == -d ]] && val="${args[$((i + 1))]}"; done
if [[ -n "$val" && "$(uname)" == Darwin ]]; then
  tz="$(sed -E 's/^\.[0-9]+//' <<<"${val:19}")"   # drop the fraction
  case "$tz" in Z|'') tz=+0000 ;; *) tz="${tz/:/}" ;; esac
  exec /bin/date -j -u -f '%Y-%m-%dT%H:%M:%S%z' "${val:0:19}$tz" +%s
fi
exec /bin/date "$@"
EOF
  cat > "$bin/docker" <<'EOF'
#!/usr/bin/env bash
# Fake docker. Scenario lives in $FAKE; every call is appended to $FAKE/calls.
F="$FAKE"; printf '%s\n' "$*" >> "$F/calls"
fmt_epoch() {  # $1 epoch, $2 offset seconds, $3 suffix
  local e=$(( $1 + $2 ))
  if [[ "$(uname)" == Darwin ]]; then /bin/date -u -r "$e" "+%Y-%m-%dT%H:%M:%S$3"; else /bin/date -u -d "@$e" "+%Y-%m-%dT%H:%M:%S$3"; fi
}
now() { fmt_epoch "$(/bin/date +%s)" 0 Z; }
role_of_ref() { case "$1" in *"$FAKE_INC_DIGEST"*|"$FAKE_INC_ID"|bosun:rollback-*) echo incumbent ;; *) echo candidate ;; esac; }
id_of_role() { case "$1" in incumbent) echo "$FAKE_INC_ID" ;; candidate) echo "$FAKE_CAND_ID" ;; *) echo "$FAKE_OTHER_ID" ;; esac; }
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
        role="${name##*-}"; override="${files[-1]}"
        cp "$override" "$F/override-$role.yml"
        echo "$role" >> "$F/run-order"
        n=$(( $(cat "$F/runs-$role" 2>/dev/null || echo 0) + 1 )); echo "$n" > "$F/runs-$role"
        var="FAKE_RENDER_${role^^}"; [[ "${!var:-ok}" != fail ]] || { echo '{"level":"error","message":"render failed"}'; exit 1; }
        dir="$(sed -n 's#^ *- "\(.*\):/work"$#\1#p' "$override")"
        mkdir -p "$dir/staging/unraid"
        if [[ "${!var:-ok}" != empty ]]; then
          cvar="FAKE_CONTENT_${role^^}"; printf '%s\n' "${!cvar:-same}" > "$dir/staging/unraid/rendered.yml"
        fi
        kvar="FAKE_COMMITS_${role^^}"; read -r -a commits <<<"${!kvar:-$FAKE_DEFAULT_COMMIT}"
        commit="${commits[$((n - 1))]:-${commits[-1]}}"
        printf '{"level":"info","component":"reconcile","commit":"%s","message":"Reconcile pipeline completed"}\n' "$commit" ;;
      up)
        last="${files[-1]}"; target=candidate
        grep -q "$FAKE_INC_DIGEST" "$last" && target=incumbent
        [[ "${#files[@]}" -gt 1 ]] && cp "$last" "$F/up-override-$target.yml"
        var="FAKE_UP_FAIL_${target^^}"; [[ "${!var:-0}" == 1 ]] && exit 1
        if [[ "$target" == incumbent && "${FAKE_ROLLBACK_WRONG_IMAGE:-0}" == 1 ]]; then target=other; fi
        echo "$target" > "$F/running_role"
        printf '%s\n' "$(now | sed 's/Z$/.123456789Z/')" > "$F/started"; sleep 1 ;;
    esac ;;
  inspect)
    r="$(cat "$F/running_role")"; [[ "$r" != none ]] || exit 1
    var="FAKE_RESTARTS_${r^^}"
    if [[ "$3" == *'{{.Image}}'* ]]; then echo "$(id_of_role "$r") running $(cat "$F/started") ${!var:-0} bosun"
    else echo "restarts=${!var:-0} status=running"; fi ;;
  image)
    if [[ "$4" == *'{{.Id}}'* ]]; then id_of_role "$(role_of_ref "${*: -1}")"; exit 0; fi
    case "${*: -1}" in
      "$FAKE_INC_ID") echo "ghcr.io/cameronsjo/bosun@$FAKE_INC_DIGEST" ;;
      "$FAKE_CAND_ID") echo "ghcr.io/cameronsjo/bosun@$FAKE_CAND_DIGEST" ;;
      *) echo "ghcr.io/cameronsjo/bosun@sha256:$(printf 'f%.0s' {1..64})" ;;
    esac ;;
  exec)
    r="$(cat "$F/running_role")"
    if [[ "$*" == *--version* ]]; then
      [[ "$r" == candidate ]] && echo "bosun version ${FAKE_CAND_VERSION:-0.43.0}" || echo "bosun version ${FAKE_INC_VERSION:-0.42.3}"
    else
      var="FAKE_STATUS_${r^^}"; e="$(/bin/date +%s)"
      case "${!var:-ok}" in
        ok) printf '{"state":"idle","last_reconcile":"%s","last_error":null}\n' "$(now)" ;;
        offset) printf '{"state":"idle","last_reconcile":"%s","last_error":null}\n' "$(fmt_epoch "$e" -18000 -05:00)" ;;
        stale) printf '{"state":"idle","last_reconcile":"%s","last_error":null}\n' "$(fmt_epoch "$e" -3600 Z)" ;;
        error) printf '{"state":"idle","last_reconcile":"%s","last_error":"deploy failed"}\n' "$(now)" ;;
        never) printf '{"state":"reconciling","last_reconcile":null,"last_error":null}\n' ;;
      esac
    fi ;;
  logs)
    [[ "${FAKE_LOGS_FAIL:-0}" == 1 ]] && exit 1
    r="$(cat "$F/running_role")"; var="FAKE_PANIC_${r^^}"
    [[ "${!var:-0}" == 1 ]] && echo "panic: runtime error"
    # What the daemon logged for its last cycle. "deployed" is a full deploy;
    # "skipped" is the common case where the commit has not moved, which logs
    # an end-of-cycle line but no completed PIPELINE; "none" is a daemon that
    # reports a reconcile in daemon-status without logging one at all.
    var="FAKE_CYCLE_LOG_${r^^}"
    case "${!var:-deployed}" in
      deployed)
        printf '{"level":"info","component":"reconcile","message":"Reconcile pipeline completed"}\n'
        printf '{"level":"info","success":true,"message":"Reconciliation cycle completed"}\n' ;;
      skipped)
        printf '{"level":"info","component":"reconcile","message":"No deploy-relevant files changed, skipping reconciliation"}\n'
        printf '{"level":"info","success":true,"message":"Reconciliation cycle completed"}\n' ;;
      none) ;;
    esac
    true ;;
  run)
    # docker run [flags...] --entrypoint bosun IMAGE <args...>
    shift; img="" ; while [[ $# -gt 0 ]]; do
      if [[ "$1" == --entrypoint ]]; then img="$3"; shift 3; break; fi
      shift
    done
    r="$(role_of_ref "$img")"
    if [[ "$1" == --version ]]; then
      [[ "$r" == candidate ]] && echo "bosun version ${FAKE_CAND_IMAGE_VERSION:-0.43.0}" || echo "bosun version ${FAKE_INC_VERSION:-0.42.3}"
      exit 0
    fi
    var="FAKE_NOALERTS_${r^^}"
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
  echo "2026-09-01T00:00:00.000000001Z" > "$F/started"
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
  export FAKE="$F" FAKE_INC_DIGEST="$INC_DIGEST" FAKE_CAND_DIGEST="$CAND_DIGEST" FAKE_DEFAULT_COMMIT="$COMMIT_A" \
    FAKE_INC_ID="$INC_ID" FAKE_CAND_ID="$CAND_ID" FAKE_OTHER_ID="$OTHER_ID"
}

run_remote() {
  RC=0
  OUT="$F/out"
  PATH="$F/bin:$PATH" BOSUN_UPGRADE_COMPOSE_DIR="$F/compose" BOSUN_UPGRADE_STATE_DIR="$F/state" \
    BOSUN_UPGRADE_TMP_ROOT="$F/tmp" BOSUN_UPGRADE_TTY="$F/tty" BOSUN_UPGRADE_POLL_SECONDS=1 \
    BOSUN_UPGRADE_UP_WAIT_SECONDS=2 bash "$REMOTE" --watch-timeout 3 "$@" > "$OUT" 2>&1 || RC=$?
}

write_state() {  # $1 phase
  printf '%s\n' "phase=$1" "project=bosun" "incumbent=ghcr.io/cameronsjo/bosun@$INC_DIGEST" "incumbent_image=$INC_ID" \
    "incumbent_version=0.42.3" "rollback_tag=bosun:rollback-0.42.3" "candidate=$CANDIDATE" \
    "candidate_image=$CAND_ID" > "$F/state/state"
}

# A lock owner the remote script sees as alive: this test's pid, on this boot.
write_live_lock() {
  mkdir -p "$F/state/lock"
  printf '%s %s\n' "$$" "$(cat /proc/sys/kernel/random/boot_id 2>/dev/null || echo unknown)" > "$F/state/lock/owner"
}

history_has() { grep -qF -- "$1" "$F/state/history.log" || fail "history lacks: $1"; }
assert_clean_tmp() {
  # The kept-failure dir is deliberate; a rendered tree is not.
  local left; left="$(find "$F/tmp" -mindepth 1 -maxdepth 1 -name 'bosun-canary.*' | head -n1)"
  [[ -z "$left" ]] || fail "shadow tmp dir left behind: $left"
  [[ ! -d "$F/state/lock" ]] || fail "lock left behind"
  [[ ! -f "$F/state/cutover.override.yml" ]] || fail "cutover override left behind"
}

# ---- remote script: preflight ----------------------------------------------

new_case already-current "ghcr.io/cameronsjo/bosun:0.42.3@$INC_DIGEST"
run_remote
assert_rc 0; assert_out "VERDICT: ALREADY-CURRENT"; history_has "ALREADY-CURRENT"; assert_no_calls "compose -p"; ok

new_case tag-only-candidate "ghcr.io/cameronsjo/bosun:0.43.0"
run_remote
assert_rc 64; assert_out "carries no @sha256: digest"; assert_no_calls "tag "; ok

new_case expect-candidate-mismatch
run_remote --expect-candidate "ghcr.io/cameronsjo/bosun:0.43.1@$CAND_DIGEST"
assert_rc 64; assert_out "pin changed since provenance was checked"; ok

new_case lock-held-by-live-run
write_live_lock
run_remote --dry-run
assert_rc 75; assert_out "pid $$"; assert_out "A live run finishes on its own"; [[ -d "$F/state/lock" ]] || fail "removed a live run's lock"; ok

new_case lock-stale-is-reclaimed
mkdir -p "$F/state/lock"; printf '999999 %s\n' "$(cat /proc/sys/kernel/random/boot_id 2>/dev/null || echo unknown)" > "$F/state/lock/owner"
run_remote --dry-run
assert_rc 0; assert_out "reclaiming a stale lock"; [[ ! -d "$F/state/lock" ]] || fail "reclaimed lock not released"; ok

# The reboot case: the pid may well be alive again as some other process, so
# liveness alone must not decide. An owner from an earlier boot is stale.
new_case lock-stale-after-reboot
mkdir -p "$F/state/lock"; printf '%s %s\n' "$$" "boot-from-before-the-reboot" > "$F/state/lock/owner"
run_remote --dry-run
assert_rc 0; assert_out "belongs to an earlier boot"; ok

new_case lock-without-owner-is-live
mkdir -p "$F/state/lock"
run_remote --dry-run
assert_rc 75; assert_out "owner is unknown"; [[ -d "$F/state/lock" ]] || fail "removed an ownerless lock"; ok

new_case history-cannot-be-forged "$(printf 'ghcr.io/cameronsjo/bosun:1\nforged\tline')"
run_remote --dry-run
assert_rc 64; [[ "$(wc -l < "$F/state/history.log")" -eq 1 ]] || fail "a crafted pin wrote more than one history line"
[[ "$(awk -F'\t' '{print NF}' "$F/state/history.log")" -eq 6 ]] || fail "a crafted pin added history fields"; ok

new_case direct-run-is-marked
run_remote --dry-run
history_has "[provenance: not checked by wrapper]"; ok

new_case print-candidate
run_remote --print-candidate
assert_rc 0; [[ "$(cat "$OUT")" == "$CANDIDATE" ]] || fail "--print-candidate printed more than the pin"; ok

new_case record-provenance-failure
run_remote --record-provenance-failure --operator cameron@sjomba
assert_rc 5; history_has "CANDIDATE-FAILED-PROVENANCE"; history_has "cameron@sjomba"; ok

new_case state-dir-unwritable
rm -rf "$F/state"; printf 'not a dir\n' > "$F/state"
run_remote --dry-run
assert_rc 64; assert_out "cannot create or restrict"; ok

CASE=ref-re-parity
[[ "$(grep -m1 '^REF_RE=' "$REMOTE")" == "$(grep -m1 '^REF_RE=' "$WRAPPER")" ]] || fail "REF_RE differs between the two scripts"; ok

new_case state-dir-symlink
rm -rf "$F/state"; mkdir -p "$F/elsewhere"; ln -s "$F/elsewhere" "$F/state"
run_remote --dry-run
assert_rc 64; assert_out "is a symlink"; ok

# ---- remote script: shadow render ------------------------------------------

new_case env-allowlist
run_remote --dry-run
assert_rc 0
for role in incumbent candidate; do
  o="$F/override-$role.yml"
  # cap_add is forbidden unconditionally: the obvious wrong fix for the lock
  # problem below is cap_add: [DAC_OVERRIDE], and the assertion that would
  # catch it sits behind a real-compose check that skips where compose is
  # absent. This one always runs.
  for forbidden in DISCORD_WEBHOOK_URL WEBHOOK_SECRET _TOKEN SENTRY OTEL docker.sock s3cret "/mnt/user/appdata:" cap_add; do
    if grep -qF -- "$forbidden" "$o"; then fail "$role override carries $forbidden"; fi
  done
  # The tmpfs is load-bearing, not hygiene: cap_drop ALL takes CAP_DAC_OVERRIDE,
  # and without it root cannot write the uid-1000-owned lock dir baked into the
  # image, so every render fails to take the reconcile lock.
  for required in 'BOSUN_INFRA_DIR: "unraid"' 'DRY_RUN: "true"' 'appdata-empty:/mnt/appdata:ro' \
      'age-key.txt:/config/age-key.txt:ro' 'cap_drop: [ALL]' 'no-new-privileges:true' 'network_mode: bridge'; do
    grep -qF -- "$required" "$o" || fail "$role shadow file lacks: $required"
  done
  # Placement, not just presence: the same entry under volumes: would be a host
  # bind of /run/bosun into the shadow, and the jq check that tells the two
  # apart sits behind a real-compose guard that skips where compose is absent.
  awk '/^    tmpfs:$/ {under=1; next}
       under && /^      - "\/run\/bosun:mode=0755,size=1m"$/ {found=1}
       under && !/^      - / {under=0}
       END {exit found ? 0 : 1}' "$o" ||
    fail "$role shadow file does not list - \"/run/bosun:mode=0755,size=1m\" under tmpfs:"
done
assert_calls "reconcile --dry-run --no-alerts"
if grep -F 'run --rm' "$F/calls" | grep -qF -- "$F/compose/docker-compose.yml"; then fail "shadow run merged the live compose file"; fi
[[ "$(head -n1 "$F/run-order")" == incumbent ]] || fail "incumbent must render first"
[[ "$(running_role)" == incumbent ]] || fail "dry run changed the running container"; assert_clean_tmp; ok

# The generated shadow file must also be valid to real compose, and carry
# nothing beyond the allowlist. Skipped, and said so, where no compose exists.
CASE=shadow-file-parses-in-real-compose
if [[ -n "$REAL_DOCKER" ]] && "$REAL_DOCKER" compose version >/dev/null 2>&1; then
  merged="$("$REAL_DOCKER" compose -p canarytest -f "$ROOT/env-allowlist/override-candidate.yml" config --format json 2>&1)" ||
    { OUT=/dev/null; fail "real compose rejected the shadow file: $merged"; }
  OUT=/dev/null
  [[ "$(jq -r '.services | keys | join(",")' <<<"$merged")" == bosun ]] || fail "shadow file defines more than one service"
  [[ "$(jq -r '.services.bosun.environment | has("DISCORD_WEBHOOK_URL")' <<<"$merged")" == false ]] || fail "shadow env carries DISCORD_WEBHOOK_URL"
  [[ "$(jq -r '[.services.bosun.volumes[].target] | sort | join(",")' <<<"$merged")" == "/config/age-key.txt,/config/deploy-key,/mnt/appdata,/work" ]] ||
    fail "shadow volumes are not exactly the allowlist"
  [[ "$(jq -r '.services.bosun.cap_drop | join(",")' <<<"$merged")" == ALL ]] || fail "shadow keeps capabilities"
  # The lock dir must be writable through a tmpfs, never by handing back
  # DAC_OVERRIDE -- that one capability defeats every file-permission check in
  # the container, and the cap_add assertion below is what stops that fix.
  [[ "$(jq -r '(.services.bosun.tmpfs // []) | join(",")' <<<"$merged")" == "/run/bosun:mode=0755,size=1m" ]] ||
    fail "shadow does not tmpfs the lock dir with a pinned mode and size"
  [[ "$(jq -r '[.services.bosun | .privileged, .devices, .cap_add, .pid] | map(select(. != null and . != false)) | length' <<<"$merged")" == 0 ]] ||
    fail "shadow carries a privilege-bearing key"
  ok
else
  printf 'skip %s (no docker compose on this machine)\n' "$CASE"
fi

# The two probes run an image the operator has not been asked about yet, and on
# the drill path that image is unverified. They need neither.
new_case probes-are-unprivileged
run_remote --dry-run
assert_rc 0
while IFS= read -r probe; do
  for flag in '--network none' '--cap-drop ALL' '--security-opt no-new-privileges'; do
    case "$probe" in *"$flag"*) ;; *) fail "probe lacks $flag: $probe" ;; esac
  done
done < <(command grep -E '^run .*--entrypoint bosun' "$F/calls")
command grep -qE '^run .*--entrypoint bosun' "$F/calls" || fail "no image probe ran"; ok

# A downgrade is a signed, provenance-passing release. It must be named and
# must always prompt, including for a digest-only pin where the tag says
# nothing: the version the image reports is what decides.
new_case downgrade-forces-prompt "ghcr.io/cameronsjo/bosun:0.41.0@$CAND_DIGEST"
export FAKE_CAND_IMAGE_VERSION=0.41.0 FAKE_CAND_VERSION=0.41.0
run_remote --yes
assert_rc 0; assert_out "WARNING this is a DOWNGRADE: 0.42.3 -> 0.41.0"; assert_out "DECLINED"; assert_no_calls "up -d"; ok

new_case downgrade-detected-without-a-release-tag "ghcr.io/cameronsjo/bosun@$CAND_DIGEST"
export FAKE_CAND_IMAGE_VERSION=0.41.0 FAKE_CAND_VERSION=0.41.0
run_remote --yes
assert_rc 0; assert_out "WARNING this is a DOWNGRADE"; assert_out "DECLINED"; ok

# SemVer puts 1.0.0-rc.1 before 1.0.0, and sort -V puts it after. Getting that
# backwards let --yes cut over to a prerelease with no prompt at all.
new_case downgrade-to-a-prerelease "ghcr.io/cameronsjo/bosun:1.0.0-rc.1@$CAND_DIGEST"
export FAKE_INC_VERSION=1.0.0 FAKE_CAND_IMAGE_VERSION=1.0.0-rc.1 FAKE_CAND_VERSION=1.0.0-rc.1
run_remote --yes
assert_rc 0; assert_out "RENDER-IDENTICAL"; assert_out "WARNING this is a DOWNGRADE: 1.0.0 -> 1.0.0-rc.1"
assert_out "DECLINED"; assert_no_calls "up -d"; [[ "$(running_role)" == incumbent ]] || fail "cut over to a prerelease"; ok

# sort -V puts alpha-1 before alpha.1; SemVer puts alpha.1 first. Rather than
# hand-write the comparator, two different prereleases of one version are
# undecided and always prompt, so --yes cannot carry either direction through.
new_case prereleases-of-one-version-always-prompt "ghcr.io/cameronsjo/bosun:1.0.0-alpha-1@$CAND_DIGEST"
export FAKE_INC_VERSION=1.0.0-alpha.1 FAKE_CAND_IMAGE_VERSION=1.0.0-alpha-1 FAKE_CAND_VERSION=1.0.0-alpha-1
run_remote --yes
assert_rc 0; assert_out "order undecided: 1.0.0-alpha.1 -> 1.0.0-alpha-1"; assert_out "DECLINED"
assert_no_calls "up -d"; [[ "$(running_role)" == incumbent ]] || fail "moved between prereleases unprompted"; ok

new_case candidate-tag-digest-mismatch
export FAKE_CAND_IMAGE_VERSION=0.41.0
run_remote --yes
assert_rc 5; assert_out "tag says 0.43.0"; assert_no_calls "up -d"; [[ ! -f "$F/runs-candidate" ]] || fail "rendered a mislabelled candidate"; ok

new_case incumbent-empty-render
export FAKE_RENDER_INCUMBENT=empty FAKE_RENDER_CANDIDATE=empty
run_remote --yes
assert_rc 4; assert_out "incumbent rendered an empty staging tree"; [[ ! -f "$F/runs-candidate" ]] || fail "blamed the candidate"; ok

new_case ambiguous-key-mount
jq '.services.bosun.volumes += [{source: "/other/age.txt", target: "/config/age-key.txt"}]' "$F/live.json" > "$F/l" && mv "$F/l" "$F/live.json"
run_remote --dry-run
assert_rc 64; assert_out "exactly one plain-path mount"; ok

# The allowlist drops keys, but `docker compose config` has already expanded
# every ${VAR} from the project's .env. A compose file that writes the webhook
# URL into an allowlisted variable carries it into a shadow that has a network,
# and --dry-run alone is enough to send it. Refuse, and do not echo the value.
new_case env-allowlist-leaks-through-a-value
jq '.services.bosun.environment.BOSUN_REPO_URL = "https://attacker.example/https://discord.example/hook.git"' \
  "$F/live.json" > "$F/l" && mv "$F/l" "$F/live.json"
run_remote --dry-run
assert_rc 64; assert_out "DISCORD_WEBHOOK_URL inside BOSUN_REPO_URL"
if command grep -q 'discord.example' "$OUT"; then fail "the refusal printed the secret it was refusing to leak"; fi
[[ ! -f "$F/runs-candidate" && ! -f "$F/runs-incumbent" ]] || fail "rendered anyway"; ok

new_case render-differs-declined
export FAKE_CONTENT_CANDIDATE=RENDERED-SECRET-MARKER
run_remote --yes
assert_rc 0; assert_out "RENDER-DIFFERS"; assert_out "unraid/rendered.yml"; assert_no_out "RENDERED-SECRET-MARKER"
assert_out "DECLINED at RENDER-DIFFERS"; [[ "$(running_role)" == incumbent ]] || fail "declined run cut over"; ok

new_case no-baseline
export FAKE_NOALERTS_INCUMBENT=0
run_remote --dry-run
assert_rc 0; assert_out "RENDER-OK-NO-BASELINE"; [[ ! -f "$F/runs-incumbent" ]] || fail "incumbent rendered without a baseline"; ok

new_case yes-without-identical-still-prompts
export FAKE_NOALERTS_INCUMBENT=0
run_remote --yes
assert_rc 0; assert_out "Cut over bosun"; assert_out "DECLINED at RENDER-OK-NO-BASELINE"; ok

new_case candidate-lacks-flag
export FAKE_NOALERTS_CANDIDATE=0
run_remote --dry-run
assert_rc 64; assert_out "predates the canary"; ok

new_case candidate-failed
export FAKE_RENDER_CANDIDATE=fail
run_remote --yes
assert_rc 5; assert_out "VERDICT: CANDIDATE-FAILED"; [[ "$(running_role)" == incumbent ]] || fail "changed on candidate failure"
ls "$F/tmp/bosun-canary-failures."*/*shadow-candidate.log >/dev/null || fail "no shadow log kept in RAM"
if find "$F/state" -name '*shadow*' | command grep -q .; then fail "a rendered-log copy reached the array-backed state dir"; fi
assert_clean_tmp; ok

# /tmp is shared and this runs as root on the NAS. A fixed directory name lets
# any local user pre-create it, keep ownership, and read failure detail that can
# quote a rendered secret. Each run must make its own instead.
new_case failures-dir-is-not-the-fixed-path
mkdir -p "$F/tmp/bosun-canary-failures"
export FAKE_RENDER_CANDIDATE=fail
run_remote --yes
assert_rc 5
kept="$(find "$F/tmp" -name '*shadow-candidate.log' | head -n1)"
[[ -n "$kept" ]] || fail "no shadow log kept at all"
case "$kept" in "$F/tmp/bosun-canary-failures/"*) fail "wrote into the pre-created directory: $kept" ;; esac
[[ -z "$(find "$F/tmp/bosun-canary-failures" -mindepth 1)" ]] || fail "the pre-created directory was used"
assert_out "$kept"
[[ -n "$(find "$F/tmp" -maxdepth 1 -name 'bosun-canary-failures.*')" ]] || fail "no per-run failures dir"
[[ -z "$(find "$F/tmp" -maxdepth 1 -name 'bosun-canary.*')" ]] || fail "the stale sweep glob now matches the failures dir"
ok

new_case candidate-empty-render
export FAKE_RENDER_CANDIDATE=empty
run_remote --yes
assert_rc 5; assert_out "candidate rendered an empty staging tree"; ok

new_case harness-invalid
export FAKE_RENDER_INCUMBENT=fail
run_remote --yes
assert_rc 4; assert_out "VERDICT: HARNESS-INVALID"; [[ ! -f "$F/runs-candidate" ]] || fail "rendered the candidate on a broken harness"; ok

# A push lands between the renders: the incumbent saw A, the candidate B. Only
# re-running the earlier (incumbent) render can converge on B.
new_case commit-moves-forward
export FAKE_COMMITS_INCUMBENT="$COMMIT_A $COMMIT_B" FAKE_COMMITS_CANDIDATE="$COMMIT_B"
run_remote --dry-run
assert_rc 0; assert_out "re-running the incumbent once"; assert_out "RENDER-IDENTICAL"
[[ "$(cat "$F/runs-incumbent")" == 2 ]] || fail "incumbent not re-run"; ok

new_case commit-keeps-moving
export FAKE_COMMITS_INCUMBENT="$COMMIT_A $COMMIT_A" FAKE_COMMITS_CANDIDATE="$COMMIT_B"
run_remote --dry-run
assert_rc 75; assert_out "repo moved"; ok

# ---- remote script: cutover, watch, rollback --------------------------------

new_case upgraded
run_remote --yes
assert_rc 0; assert_out "RENDER-IDENTICAL"; assert_out "VERDICT: UPGRADED"
[[ "$(running_role)" == candidate ]] || fail "candidate not running"
[[ "$(cat "$F/tagged")" == "bosun:rollback-0.42.3" ]] || fail "rollback anchor not tagged"
grep -qF "image: \"$CANDIDATE\"" "$F/up-override-candidate.yml" || fail "cutover did not pin the verified digest"
[[ ! -f "$F/state/state" ]] || fail "state file left after success"
history_has "UPGRADED"; history_has "exit=0"; assert_calls "--pull never"; assert_clean_tmp; ok

new_case offset-timestamps
export FAKE_STATUS_CANDIDATE=offset
run_remote --yes
assert_rc 0; assert_out "VERDICT: UPGRADED"; ok

new_case non-release-tag "ghcr.io/cameronsjo/bosun:0.43@$CAND_DIGEST"
run_remote --yes
assert_rc 0; assert_out "VERDICT: UPGRADED"; ok

new_case release-tag-version-mismatch
export FAKE_CAND_VERSION=0.44.0
run_remote --yes
assert_rc 1; assert_out "expected: bosun version 0.43.0"; ok

new_case history-unwritable
mkdir -p "$F/state/history.log"
run_remote --yes
assert_rc 0; assert_out "VERDICT: UPGRADED"; assert_out "could not append"; ok

new_case rolled-back
export FAKE_STATUS_CANDIDATE=error
run_remote --yes
assert_rc 1; assert_out "VERDICT: ROLLED-BACK"; [[ "$(running_role)" == incumbent ]] || fail "incumbent not restored"
grep -qF "image: \"ghcr.io/cameronsjo/bosun@$INC_DIGEST\"" "$F/state/rollback.override.yml" || fail "rollback override does not pin the incumbent digest"
history_has "ROLLED-BACK"
ls "$F/tmp/bosun-canary-failures."*/*-candidate.log >/dev/null || fail "failure detail not kept in RAM"
if find "$F/state" -name '*candidate*' | command grep -q .; then fail "daemon failure detail reached the array-backed state dir"; fi; ok

# daemon-status is the candidate's own word. A candidate that reports a fresh
# reconcile with no end-of-cycle line in the log must not pass the watch.
new_case rolled-back-unsupported-status-claim
export FAKE_CYCLE_LOG_CANDIDATE=none
run_remote --yes
assert_rc 1; assert_out "no end-of-cycle line in the log yet"; assert_out "VERDICT: ROLLED-BACK"; ok

# The common cutover: the commit has not moved, so the first cycle skips. It
# logs an end-of-cycle line but never a completed pipeline. Requiring the
# pipeline line would roll back every healthy upgrade.
new_case upgraded-when-first-cycle-skips
export FAKE_CYCLE_LOG_CANDIDATE=skipped
run_remote --yes
assert_rc 0; assert_out "VERDICT: UPGRADED"; [[ "$(running_role)" == candidate ]] || fail "rolled back a healthy candidate"; ok

# The waiting line must not repeat once per poll for the whole timeout.
new_case waiting-line-is-printed-once
export FAKE_CYCLE_LOG_CANDIDATE=none
run_remote --yes
assert_rc 1
[[ "$(command grep -c 'no end-of-cycle line' "$OUT")" -le 2 ]] || fail "waiting line repeated every poll"; ok

new_case rolled-back-stale-reconcile
export FAKE_STATUS_CANDIDATE=stale
run_remote --yes
assert_rc 1; assert_out "no reconcile finished within"; ok

new_case rolled-back-timeout
export FAKE_STATUS_CANDIDATE=never
run_remote --yes
assert_rc 1; assert_out "no reconcile finished within"; ok

new_case rolled-back-panic
export FAKE_PANIC_CANDIDATE=1
run_remote --yes
assert_rc 1; assert_out "the daemon panicked"; ok

new_case rolled-back-crash-loop
export FAKE_RESTARTS_CANDIDATE=2
run_remote --yes
assert_rc 1; assert_out "restarts=2 (expected: running, 0 restarts)"; ok

new_case rolled-back-unreadable-log
export FAKE_LOGS_FAIL=1 FAKE_STATUS_INCUMBENT=never
run_remote --yes
assert_rc 2; assert_out "could not read the daemon log"; ok

new_case fault-not-upgrade
export FAKE_STATUS_CANDIDATE=error FAKE_STATUS_INCUMBENT=error
run_remote --yes
assert_rc 2; assert_out "VERDICT: FAULT-NOT-UPGRADE"; ok

new_case half-changed
export FAKE_STATUS_CANDIDATE=error FAKE_UP_FAIL_INCUMBENT=1
run_remote --yes
assert_rc 3; assert_out "VERDICT: HALF-CHANGED"; assert_out "bosun:rollback-0.42.3"
grep -qF 'phase=rollback' "$F/state/state" || fail "state not kept for a resume"; ok

new_case rollback-wrong-image
export FAKE_STATUS_CANDIDATE=error FAKE_ROLLBACK_WRONG_IMAGE=1
run_remote --yes
assert_rc 3; assert_out "the incumbent recorded at preflight"; ok

# ---- remote script: resume --------------------------------------------------

new_case resume-watching
echo candidate > "$F/running_role"; echo "2026-09-19T00:00:00Z" > "$F/started"; write_state watching
run_remote
assert_rc 0; assert_out "interrupted upgrade is recorded (phase=watching)"; assert_out "VERDICT: UPGRADED"
assert_no_calls "compose -p bosun-canary"; assert_no_calls "up -d"; ok

new_case resume-after-pin-reverted "ghcr.io/cameronsjo/bosun:0.42.3@$INC_DIGEST"
echo candidate > "$F/running_role"; write_state watching
run_remote
assert_rc 1; assert_out "the pin was reverted to the incumbent"; assert_out "VERDICT: ROLLED-BACK"
[[ "$(running_role)" == incumbent ]] || fail "incumbent not restored"; ok

new_case resume-pin-moved-elsewhere "ghcr.io/cameronsjo/bosun:0.44.0@sha256:$(printf '9%.0s' {1..64})"
echo candidate > "$F/running_role"; write_state watching
run_remote
assert_rc 3; assert_out "pin moved to a third image"; assert_no_calls "up -d"; ok

new_case resume-container-missing
echo none > "$F/running_role"; write_state cutover
run_remote
assert_rc 1; assert_out "resumed: candidate not running"; assert_out "VERDICT: ROLLED-BACK"; ok

new_case resume-before-cutover
write_state cutover
run_remote
assert_rc 75; assert_out "INTERRUPTED-BEFORE-CUTOVER"; [[ ! -f "$F/state/state" ]] || fail "state kept"
assert_no_calls "up -d"; ok

new_case dry-run-never-resumes
echo candidate > "$F/running_role"; write_state watching
run_remote --dry-run
assert_rc 64; assert_out "--dry-run never resumes"; assert_no_calls "up -d"; assert_no_calls "exec bosun bosun daemon-status"
grep -qF 'phase=watching' "$F/state/state" || fail "dry run touched the state"; ok

new_case malformed-state
printf 'phase=rollback\nrollback_tag=evil"; rm -rf /\n' > "$F/state/state"
run_remote
assert_rc 64; assert_out "malformed"; assert_no_calls "up -d"; ok

# ---- remote script: drill marker --------------------------------------------

new_case drill-refuses-yes
run_remote --provenance-skipped --yes
assert_rc 64; assert_out "--yes is refused"; ok

new_case drill-marks-history
run_remote --provenance-skipped --dry-run
assert_rc 0; history_has "[provenance skipped: drill]"; ok

# ---- Mac wrapper -----------------------------------------------------------

# Wrapper cases. With FAKE_SSH=real the ssh stub runs the copy scp staged,
# i.e. the real remote script against the fake docker, so the flag and quoting
# contract between the two scripts is exercised end to end. Otherwise it
# answers each call from canned values.
new_wrapper_case() {
  new_case "$1"
  cat > "$F/bin/ssh" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$FAKE/ssh-calls"
cmd="${*: -1}"
case "$cmd" in
  true) exit 0 ;;
  mktemp*) echo /tmp/bosun-upgrade-remote.Ab12Cd; exit 0 ;;
  rm\ -f*) exit 0 ;;
esac
if [[ "${FAKE_SSH:-canned}" == real ]]; then
  exec bash -c "${cmd//\/tmp\/bosun-upgrade-remote.Ab12Cd/$FAKE/remote-copy.sh}"
fi
case "$cmd" in
  *--print-candidate*) echo "$FAKE_WRAP_CANDIDATE" ;;
  *--record-provenance-failure*) exit 5 ;;
  *) exit "${FAKE_REMOTE_RC:-0}" ;;
esac
EOF
  cat > "$F/bin/scp" <<'EOF'
#!/usr/bin/env bash
cp "${@: -2:1}" "$FAKE/remote-copy.sh"
EOF
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
  PATH="$F/bin:$PATH" BOSUN_UPGRADE_WRAPPED="${WRAPPED-1}" BOSUN_UPGRADE_LOG_DIR="$F/logs" \
    BOSUN_UPGRADE_COMPOSE_DIR="$F/compose" BOSUN_UPGRADE_STATE_DIR="$F/state" BOSUN_UPGRADE_TMP_ROOT="$F/tmp" \
    BOSUN_UPGRADE_TTY="$F/tty" BOSUN_UPGRADE_POLL_SECONDS=1 BOSUN_UPGRADE_UP_WAIT_SECONDS=2 \
    bash "$WRAPPER" --watch-timeout 3 "$@" > "$OUT" 2>&1 || RC=$?
}

new_wrapper_case wrapper-end-to-end-upgrade
export FAKE_SSH=real
run_wrapper --yes
assert_rc 0; assert_out "PASS provenance"; assert_out "VERDICT: UPGRADED"; [[ "$(running_role)" == candidate ]] || fail "not upgraded"
history_has "$(printf '%s@%s' "${USER:-unknown}" "$(hostname -s)")"; ok

new_wrapper_case wrapper-end-to-end-provenance-failure
export FAKE_SSH=real FAKE_GH_RC=1
run_wrapper
assert_rc 5; history_has "CANDIDATE-FAILED-PROVENANCE"; history_has "$(printf '%s@%s' "${USER:-unknown}" "$(hostname -s)")"
[[ "$(running_role)" == incumbent ]] || fail "changed after a provenance failure"; ok

new_wrapper_case wrapper-end-to-end-rollback
export FAKE_SSH=real FAKE_STATUS_CANDIDATE=error
run_wrapper --yes
assert_rc 1; assert_out "VERDICT: ROLLED-BACK"; ok

new_wrapper_case wrapper-pass-through
export FAKE_REMOTE_RC=1
run_wrapper
assert_rc 1
for pin in "--signer-workflow cameronsjo/bosun/.github/workflows/release-please.yml" "--source-ref refs/heads/main" "--deny-self-hosted-runners"; do
  grep -qF -- "$pin" "$F/gh-calls" || fail "provenance check lacks: $pin"
done
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
assert_rc 0; assert_out "provenance NOT checked"; [[ ! -s "$F/gh-calls" ]] || fail "drill ran gh"
grep -qF -- "--provenance-skipped" "$F/ssh-calls" || fail "drill not marked for the NAS history"; ok

new_wrapper_case wrapper-drill-refuses-yes
run_wrapper --skip-provenance-for-drill --yes
assert_rc 64; assert_out "--yes is refused"; ok

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
