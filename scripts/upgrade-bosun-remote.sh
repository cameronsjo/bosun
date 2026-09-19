#!/usr/bin/env bash
#
# NAS side of the bosun upgrade canary. scripts/upgrade-bosun.sh copies this
# file to the host and runs it over one ssh session; run it directly only for
# --print-candidate or to resume after a lost connection.
#
#   bash upgrade-bosun-remote.sh [--dry-run] [--yes] [--watch-timeout SECONDS]
#                                [--expect-candidate REF] [--operator NAME]
#   bash upgrade-bosun-remote.sh --print-candidate
#
# Stages: 1 preflight, 2 shadow render (incumbent and candidate dry-run the
# same commit in throwaway containers), 3 cutover, 4 watch the first real
# reconcile, 5 roll back to the tagged incumbent if the watch fails.
#
# Exit codes. NOTE: exit 1 is a rollback, not "a check failed".
#   0  UPGRADED, or ALREADY-CURRENT, or a --dry-run that rendered cleanly
#   1  ROLLED-BACK        the candidate failed its watch; incumbent restored and healthy
#   2  FAULT-NOT-UPGRADE  the incumbent also fails after rollback; the fault is elsewhere
#   3  HALF-CHANGED       the rollback itself failed; manual steps printed
#   4  HARNESS-INVALID    the incumbent failed its own shadow render
#   5  CANDIDATE-FAILED   the candidate failed its shadow render; nothing changed
#  64  usage or configuration error; nothing changed
#  75  transient failure before cutover; retry later. After cutover a lost
#      connection is recovered by re-running, which resumes the watch.
#
# Secrets: shadow runs render decrypted secrets into a 0700 dir under /tmp
# (RAM on Unraid), removed on exit. Only file names, counts and verdicts reach
# stdout; failure detail goes to the NAS history directory.

set -euo pipefail

COMPOSE_DIR="${BOSUN_UPGRADE_COMPOSE_DIR:-/mnt/user/appdata/bosun}"
STATE_DIR="${BOSUN_UPGRADE_STATE_DIR:-/mnt/user/appdata/bosun-upgrade}"
TMP_ROOT="${BOSUN_UPGRADE_TMP_ROOT:-/tmp}"
TTY_IN="${BOSUN_UPGRADE_TTY:-/dev/tty}"
POLL_SECONDS="${BOSUN_UPGRADE_POLL_SECONDS:-15}"
UP_WAIT_SECONDS="${BOSUN_UPGRADE_UP_WAIT_SECONDS:-60}"
CONTAINER=bosun
SERVICE=bosun
LIVE="$COMPOSE_DIR/docker-compose.yml"
STATE_FILE="$STATE_DIR/state"
HISTORY="$STATE_DIR/history.log"
ROLLBACK_OVERRIDE="$STATE_DIR/rollback.override.yml"
LOCK="$STATE_DIR/lock"
# Lowercase: it becomes part of a compose project name.
RUN_ID="$(date -u +%Y%m%dt%H%M%Sz)-$$"

# Environment a shadow render may see. Everything else in the live service --
# alert webhooks, tokens, Sentry, OTel, the webhook secret -- is dropped.
ENV_ALLOWLIST='["TZ","BOSUN_REPO_URL","REPO_URL","BOSUN_REPO_BRANCH","REPO_BRANCH","BOSUN_INFRA_DIR","BOSUN_TARGETS","BOSUN_SECRETS_FILE","SECRETS_FILES","SOPS_AGE_KEY_FILE","BOSUN_SSH_KEY","BOSUN_SSH_KNOWN_HOSTS","BOSUN_GIT_FETCH_DEPTH","BOSUN_DEPLOY_PATHS","BOSUN_DEPLOY_SYNC_PATHS","BOSUN_DEPLOY_SYNC_EXCLUDE","BOSUN_TEMPLATE_INCLUDE_DIR"]'
REF_RE='^[a-z0-9][a-z0-9./_-]*(:[A-Za-z0-9._-]+)?@sha256:[0-9a-f]{64}$'

DRY_RUN=0 ASSUME_YES=0 WATCH_TIMEOUT=900 EXPECT_CANDIDATE="" OPERATOR="${USER:-unknown}@$(hostname -s 2>/dev/null || echo nas)"
RUN_DIR="" LOCKED=0 INCUMBENT="" CANDIDATE=""

say() { printf '%s\n' "$*"; }
step() { printf '\n==> %s\n' "$*"; }
die() { printf 'ERROR %s\n' "$2" >&2; finish "$3" "$1"; }

usage() { sed -n '2,32p' "$0" | sed 's/^# \{0,1\}//'; exit 0; }

# shellcheck disable=SC2329  # invoked by the EXIT trap
cleanup() {
  if [[ -n "$RUN_DIR" && -d "$RUN_DIR" ]]; then rm -rf -- "$RUN_DIR"; fi
  local ids
  ids="$(docker ps -aq --filter "name=^bosun-canary-$RUN_ID-" 2>/dev/null || true)"
  if [[ -n "$ids" ]]; then
    # shellcheck disable=SC2086  # ids is a newline list of hex container ids
    docker rm -f $ids >/dev/null 2>&1 || true
  fi
  if [[ "$LOCKED" -eq 1 ]]; then rm -rf -- "$LOCK"; fi
  return 0
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

# finish records one history line and exits. Every verdict goes through here.
finish() {
  local code="$1" verdict="$2"
  if [[ -d "$STATE_DIR" ]]; then
    printf '%s\t%s\tincumbent=%s\tcandidate=%s\t%s\texit=%s\n' \
      "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$OPERATOR" "${INCUMBENT:-?}" "${CANDIDATE:-?}" "$verdict" "$code" >> "$HISTORY"
  fi
  printf '\nVERDICT: %s (exit %s)\n' "$verdict" "$code"
  exit "$code"
}

digest_of() { printf '%s' "${1##*@}"; }
# same_image compares by digest: a RepoDigest carries no tag, the pin does.
same_image() { [[ -n "$1" && -n "$2" && "$(digest_of "$1")" == "$(digest_of "$2")" ]]; }
tag_of() {
  local name="${1%@*}"
  if [[ "${name##*/}" == *:* ]]; then printf '%s' "${name##*:}"; fi
}
to_epoch() {
  [[ -n "$1" ]] || return 1
  date -u -d "$1" +%s
}

live_config() { docker compose -f "$LIVE" config --format json; }

read_candidate() {
  local ref
  ref="$(live_config | jq -r ".services.$SERVICE.image // empty")" || return 1
  printf '%s' "$ref"
}

# inspect_live prints "image-id status started restarts project" for the live
# container, or fails when it does not exist.
inspect_live() {
  docker inspect -f '{{.Image}} {{.State.Status}} {{.State.StartedAt}} {{.RestartCount}} {{index .Config.Labels "com.docker.compose.project"}}' "$CONTAINER"
}

running_ref_for() {
  # The RepoDigest of image id $1, preferring the candidate's repository.
  local want_repo="${CANDIDATE%%[:@]*}" digests
  digests="$(docker image inspect -f '{{range .RepoDigests}}{{println .}}{{end}}' "$1")" || return 1
  grep -m1 "^${want_repo}@sha256:" <<<"$digests" || head -n1 <<<"$digests"
}

save_state() {
  local tmp="$STATE_FILE.tmp"
  printf '%s\n' "$@" > "$tmp"
  mv -f "$tmp" "$STATE_FILE"
}
# state_get prints one key from the state file; no file means no value.
state_get() {
  [[ -f "$STATE_FILE" ]] || return 0
  sed -n "s/^$1=//p" "$STATE_FILE" | head -n1
}

record_failure() {
  local label="$1"
  local out="$STATE_DIR/failures/$RUN_ID-$label.log"
  mkdir -p "$STATE_DIR/failures"
  {
    printf '== %s\n' "$label"
    docker exec "$CONTAINER" bosun daemon-status --json 2>&1 || true
    docker inspect -f 'restarts={{.RestartCount}} status={{.State.Status}}' "$CONTAINER" 2>&1 || true
    docker logs --tail 40 "$CONTAINER" 2>&1 || true
  } > "$out"
  chmod 600 "$out"
  say "  failure detail kept on the NAS: $out"
}

prompt_yes() {
  local reply=""
  printf '%s [y/N] ' "$1"
  read -r reply < "$TTY_IN" || true
  [[ "$reply" == [yY] || "$reply" == [yY][eE][sS] ]]
}

has_no_alerts_flag() {
  local help
  help="$(docker run --rm --entrypoint bosun "$1" reconcile --help 2>&1)" || return 1
  [[ "$help" == *--no-alerts* ]]
}

# write_override emits the compose override for one shadow role.
write_override() {
  local role="$1" image="$2" dir="$3" cfg="$4" env_yaml age deploy appdata
  env_yaml="$(jq -r --argjson allow "$ENV_ALLOWLIST" '
      .services.bosun.environment // {} | to_entries
      | map(select(.key as $k | $allow | index($k)))
      | .[] | "      \(.key): \(.value | tostring | gsub("\\$"; "$$") | @json)"' <<<"$cfg")"
  age="$(jq -r '.services.bosun.volumes[]? | select(.target == "/config/age-key.txt") | .source' <<<"$cfg")"
  deploy="$(jq -r '.services.bosun.volumes[]? | select(.target == "/config/deploy-key") | .source' <<<"$cfg")"
  appdata="$(jq -r '.services.bosun.volumes[]? | select(.target == "/mnt/appdata") | .source' <<<"$cfg")"
  [[ -n "$age" && -n "$deploy" && -n "$appdata" ]] || return 1
  cat > "$dir/override.yml" <<EOF
# Generated by upgrade-bosun-remote.sh for the $role shadow render. Throwaway.
services:
  $SERVICE:
    image: "$image"
    container_name: !reset null
    env_file: !reset []
    environment: !override
$env_yaml
      DRY_RUN: "true"
      BOSUN_LOG_FORMAT: "json"
      REPO_DIR: "/work/repo"
      STAGING_DIR: "/work/staging"
      LOG_DIR: "/work/logs"
      BACKUP_DIR: "/work/backups"
      BOSUN_STATE_DIR: "/work/state"
    volumes: !override
      - "$age:/config/age-key.txt:ro"
      - "$deploy:/config/deploy-key:ro"
      - "$appdata:/mnt/appdata:ro"
      - "$dir:/work"
    networks: !reset []
    network_mode: bridge
    labels: !override
      - com.centurylinklabs.watchtower.enable=false
    healthcheck: !override
      disable: true
EOF
}

# shadow_run renders one role and prints the commit it rendered, or fails.
shadow_run() {
  local role="$1" image="$2" cfg="$3" no_alerts="$4" commit
  local dir="$RUN_DIR/$role"
  rm -rf -- "$dir"
  # repo/ and staging/ are left for bosun to create: it checks the staging
  # root's mode, and a pre-created 0755 dir would fail that check.
  mkdir -p "$dir/logs" "$dir/backups" "$dir/state"
  chmod 700 "$dir"
  write_override "$role" "$image" "$dir" "$cfg" || { say "  could not read the key and appdata mounts from $LIVE" >&2; return 1; }
  local args=(reconcile --dry-run)
  [[ "$no_alerts" -eq 1 ]] && args+=(--no-alerts)
  if ! docker compose -p "bosun-canary-$RUN_ID" -f "$LIVE" -f "$dir/override.yml" run --rm --no-deps -T \
      --name "bosun-canary-$RUN_ID-$role" "$SERVICE" bosun "${args[@]}" > "$dir/run.log" 2>&1; then
    return 1
  fi
  commit="$(grep '"Reconcile pipeline completed"' "$dir/run.log" | grep -o '"commit":"[0-9a-f]\{40\}"' | head -n1 | cut -d'"' -f4)" || true
  [[ -n "$commit" ]] || return 1
  printf '%s' "$commit"
}

keep_shadow_log() {
  mkdir -p "$STATE_DIR/failures"
  local out="$STATE_DIR/failures/$RUN_ID-shadow-$1.log"
  # Log lines only: the render itself stays in RAM and is deleted on exit.
  tail -n 60 "$RUN_DIR/$1/run.log" > "$out" 2>/dev/null || true
  chmod 600 "$out"
  say "  last 60 log lines kept on the NAS: $out"
}

# watch_reconcile waits for the first reconcile cycle to finish after the
# container's StartedAt, and succeeds only if it finished without an error.
watch_reconcile() {
  local expect_ref="$1" label="$2" started started_epoch deadline now status line image restarts lr le lr_epoch ds logs
  line="$(inspect_live)" || { say "  FAIL container $CONTAINER is gone"; return 1; }
  read -r image status started restarts _ <<<"$line"
  started_epoch="$(to_epoch "$started")" || { say "  FAIL could not read StartedAt"; return 1; }
  deadline=$(( $(date +%s) + WATCH_TIMEOUT ))
  say "  watching for a reconcile after $started (timeout ${WATCH_TIMEOUT}s)"
  while :; do
    line="$(inspect_live)" || { say "  FAIL container $CONTAINER disappeared (expected: running)"; return 1; }
    read -r image status _ restarts _ <<<"$line"
    if [[ "$status" != running || "$restarts" != 0 ]]; then
      say "  FAIL container status=$status restarts=$restarts (expected: running, 0 restarts)"; return 1
    fi
    if ! same_image "$(running_ref_for "$image")" "$expect_ref"; then
      say "  FAIL running image changed under the watch (expected: $expect_ref)"; return 1
    fi
    # Captured, not piped: `docker logs | grep -q` under pipefail reports
    # failure exactly when grep finds a match early.
    logs="$(docker logs --since "$started" "$CONTAINER" 2>&1)" || logs=""
    if [[ "$logs" == *'panic:'* ]]; then
      say "  FAIL the daemon panicked"; return 1
    fi
    ds="$(docker exec "$CONTAINER" bosun daemon-status --json 2>/dev/null)" || ds=""
    lr="$(jq -r '.last_reconcile // empty' <<<"$ds" 2>/dev/null)" || lr=""
    le="$(jq -r '.last_error // empty' <<<"$ds" 2>/dev/null)" || le=""
    if [[ -n "$lr" ]] && lr_epoch="$(to_epoch "$lr")" && (( lr_epoch >= started_epoch )); then
      if [[ -n "$le" ]]; then
        say "  FAIL first reconcile ended with an error (expected: last_error empty; error text is in the NAS failure log)"
        return 1
      fi
      say "  PASS $label: reconcile finished at $lr with no error"
      return 0
    fi
    now="$(date +%s)"
    if (( now >= deadline )); then
      say "  FAIL no reconcile finished within ${WATCH_TIMEOUT}s (last_reconcile=${lr:-null})"; return 1
    fi
    sleep "$POLL_SECONDS"
  done
}

compose_up() {
  local project="$1"; shift
  docker compose -p "$project" "$@" up -d --pull never "$SERVICE"
}

wait_running() {
  local deadline=$(( $(date +%s) + UP_WAIT_SECONDS )) line status
  while :; do
    if line="$(inspect_live)"; then
      read -r _ status _ _ _ <<<"$line"
      [[ "$status" == running ]] && return 0
    fi
    (( $(date +%s) >= deadline )) && return 1
    sleep 2
  done
}

stage_rollback() {
  local reason="$1" project rollback_tag inc_version
  project="$(state_get project)"; rollback_tag="$(state_get rollback_tag)"; inc_version="$(state_get incumbent_version)"
  INCUMBENT="$(state_get incumbent)"; CANDIDATE="$(state_get candidate)"
  if [[ -z "$project" || -z "$rollback_tag" || -z "$inc_version" ]]; then
    say "  FAIL $STATE_FILE lacks the rollback anchor; roll back by hand to the previous pin"
    finish 3 HALF-CHANGED
  fi
  step "Stage 5: rolling back to $rollback_tag ($reason)"
  save_state "phase=rollback" "project=$project" "incumbent=$INCUMBENT" "incumbent_version=$inc_version" \
    "rollback_tag=$rollback_tag" "candidate=$CANDIDATE" "reason=$reason"
  cat > "$ROLLBACK_OVERRIDE" <<EOF
# Written $(date -u +%Y-%m-%dT%H:%M:%SZ) by upgrade-bosun-remote.sh.
# Reason: $reason
# Keeps bosun on the pre-upgrade image while the pin names the failed candidate
# ($CANDIDATE). Revert the pin PR in homelab, then delete this file.
services:
  $SERVICE:
    image: "$rollback_tag"
EOF
  if ! compose_up "$project" -f "$LIVE" -f "$ROLLBACK_OVERRIDE" || ! wait_running; then
    say "  FAIL the rollback did not start. Manual recovery, on the NAS:"
    say "    cd $COMPOSE_DIR && docker compose -p $project -f docker-compose.yml -f $ROLLBACK_OVERRIDE up -d --pull never $SERVICE"
    say "    docker image inspect $rollback_tag   # the anchor; do not prune it"
    finish 3 HALF-CHANGED
  fi
  local version
  version="$(docker exec "$CONTAINER" bosun --version 2>/dev/null | head -n1)" || version=""
  if [[ "$version" != "bosun version $inc_version" ]]; then
    say "  FAIL rolled-back container reports '$version' (expected: bosun version $inc_version)"
    finish 3 HALF-CHANGED
  fi
  local inc_ref
  inc_ref="$(running_ref_for "$(inspect_live | cut -d' ' -f1)")" || inc_ref=""
  if watch_reconcile "$inc_ref" "incumbent after rollback"; then
    rm -f -- "$STATE_FILE"
    say "  The pin still names the failed candidate. Revert it in homelab, then delete $ROLLBACK_OVERRIDE."
    finish 1 ROLLED-BACK
  fi
  record_failure incumbent-after-rollback
  rm -f -- "$STATE_FILE"
  say "  The incumbent fails too, so the upgrade is not the cause. See homelab docs/runbooks/bosun-deploys-blocked.md."
  finish 2 FAULT-NOT-UPGRADE
}

stage_watch() {
  step "Stage 4: watching the first real reconcile"
  if watch_reconcile "$CANDIDATE" "candidate"; then
    rm -f -- "$STATE_FILE" "$ROLLBACK_OVERRIDE"
    finish 0 UPGRADED
  fi
  record_failure candidate
  stage_rollback "candidate failed its first reconcile"
}

stage_cutover() {
  local project="$1" before_started="$2" line image started version tag
  step "Stage 3: cutover"
  save_state "phase=cutover" "project=$project" "incumbent=$INCUMBENT" "incumbent_version=$INCUMBENT_VERSION" \
    "rollback_tag=$ROLLBACK_TAG" "candidate=$CANDIDATE"
  # The rollback override is dropped here on purpose: the live file names the candidate.
  if ! compose_up "$project" -f "$LIVE" || ! wait_running; then
    record_failure cutover
    stage_rollback "compose up of the candidate failed"
  fi
  line="$(inspect_live)" || stage_rollback "container missing after cutover"
  read -r image _ started _ _ <<<"$line"
  if ! same_image "$(running_ref_for "$image")" "$CANDIDATE"; then stage_rollback "running image is not the candidate"; fi
  if [[ "$started" == "$before_started" ]]; then stage_rollback "container was not recreated (StartedAt unchanged)"; fi
  tag="$(tag_of "$CANDIDATE")"
  version="$(docker exec "$CONTAINER" bosun --version 2>/dev/null | head -n1)" || version=""
  if [[ -n "$tag" && "$version" != "bosun version $tag" ]]; then
    stage_rollback "candidate reports '$version' (expected: bosun version $tag)"
  fi
  say "  PASS candidate is running ($version), started $started"
  save_state "phase=watching" "project=$project" "incumbent=$INCUMBENT" "incumbent_version=$INCUMBENT_VERSION" \
    "rollback_tag=$ROLLBACK_TAG" "candidate=$CANDIDATE"
  stage_watch
}

main() {
  local print_candidate=0 provenance_failed=0
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -h|--help|help) usage ;;
      --dry-run) DRY_RUN=1; shift ;;
      --yes) ASSUME_YES=1; shift ;;
      --print-candidate) print_candidate=1; shift ;;
      --record-provenance-failure) provenance_failed=1; shift ;;
      --watch-timeout|--expect-candidate|--operator)
        [[ $# -ge 2 ]] || die USAGE "$1 needs a value" 64
        case "$1" in
          --watch-timeout) WATCH_TIMEOUT="$2" ;;
          --expect-candidate) EXPECT_CANDIDATE="$2" ;;
          --operator) OPERATOR="$2" ;;
        esac
        shift 2 ;;
      *) die USAGE "unknown argument: $1" 64 ;;
    esac
  done
  [[ "$WATCH_TIMEOUT" =~ ^[0-9]+$ && "$WATCH_TIMEOUT" -gt 0 ]] || die USAGE "--watch-timeout must be a positive number of seconds" 64
  [[ "$OPERATOR" =~ ^[A-Za-z0-9._@-]{1,64}$ ]] || die USAGE "--operator must match [A-Za-z0-9._@-]" 64
  [[ -f "$LIVE" ]] || die CONFIG "no compose file at $LIVE" 64

  CANDIDATE="$(read_candidate)" || die CONFIG "could not read the $SERVICE image from $LIVE" 64
  if [[ "$print_candidate" -eq 1 ]]; then printf '%s\n' "$CANDIDATE"; exit 0; fi
  if [[ "$provenance_failed" -eq 1 ]]; then
    # The Mac side found no valid provenance; record it here so the NAS
    # history holds every outcome.
    mkdir -p "$STATE_DIR"; chmod 700 "$STATE_DIR"
    finish 5 CANDIDATE-FAILED-PROVENANCE
  fi
  [[ "$CANDIDATE" =~ $REF_RE ]] || die CONFIG "the pinned image '$CANDIDATE' carries no @sha256: digest; refusing an unpinned candidate" 64
  if [[ -n "$EXPECT_CANDIDATE" && "$EXPECT_CANDIDATE" != "$CANDIDATE" ]]; then
    die CONFIG "the pin changed since provenance was checked (checked $EXPECT_CANDIDATE, now $CANDIDATE)" 64
  fi

  mkdir -p "$STATE_DIR"; chmod 700 "$STATE_DIR"
  if ! mkdir "$LOCK" 2>/dev/null; then
    say "Another upgrade holds $LOCK. If none is running, remove that directory and re-run."
    printf '\nVERDICT: LOCKED (exit 75)\n'; exit 75
  fi
  LOCKED=1

  step "Stage 1: preflight"
  local line image started project phase
  line="$(inspect_live)" || die CONFIG "container $CONTAINER not found" 64
  read -r image _ started _ project <<<"$line"
  [[ -n "$project" ]] || die CONFIG "container $CONTAINER carries no compose project label" 64
  INCUMBENT="$(running_ref_for "$image")" || INCUMBENT=""
  [[ "$INCUMBENT" =~ $REF_RE ]] || die CONFIG "the running image has no registry digest to anchor a rollback" 64
  say "  incumbent: $INCUMBENT"
  say "  candidate: $CANDIDATE"

  phase="$(state_get phase)"
  if [[ -n "$phase" ]]; then
    CANDIDATE_STATE="$(state_get candidate)"
    say "  resuming an interrupted upgrade (phase=$phase)"
    [[ "$CANDIDATE_STATE" == "$CANDIDATE" ]] || die CONFIG "state file names candidate $CANDIDATE_STATE but the pin names $CANDIDATE; resolve by hand ($STATE_FILE)" 64
    INCUMBENT="$(state_get incumbent)"; INCUMBENT_VERSION="$(state_get incumbent_version)"; ROLLBACK_TAG="$(state_get rollback_tag)"
    case "$phase" in
      cutover|watching)
        if same_image "$(running_ref_for "$image")" "$CANDIDATE"; then stage_watch; fi
        stage_rollback "resumed: candidate not running" ;;
      rollback) stage_rollback "$(state_get reason)" ;;
      *) die CONFIG "unknown phase '$phase' in $STATE_FILE" 64 ;;
    esac
  fi

  if same_image "$INCUMBENT" "$CANDIDATE"; then
    finish 0 ALREADY-CURRENT
  fi
  local last_run=""
  if [[ -f "$HISTORY" ]]; then last_run="$(grep -F "candidate=$CANDIDATE" "$HISTORY" | tail -n1 || true)"; fi
  if [[ "$last_run" == *ROLLED-BACK* ]]; then
    say "  WARNING this candidate was rolled back before; see $HISTORY"
  fi
  if [[ -f "$ROLLBACK_OVERRIDE" ]]; then
    say "  WARNING $ROLLBACK_OVERRIDE is in effect; a successful upgrade removes it"
  fi

  local stale
  for stale in "$TMP_ROOT"/bosun-canary.*; do
    [[ -e "$stale" ]] && rm -rf -- "$stale"
  done
  stale="$(docker ps -aq --filter 'name=^bosun-canary-' 2>/dev/null || true)"
  # shellcheck disable=SC2086  # newline list of hex container ids
  [[ -n "$stale" ]] && docker rm -f $stale >/dev/null 2>&1

  INCUMBENT_VERSION="$(docker exec "$CONTAINER" bosun --version 2>/dev/null | head -n1 | sed -n 's/^bosun version //p')" || INCUMBENT_VERSION=""
  [[ "$INCUMBENT_VERSION" =~ ^[0-9A-Za-z._-]+$ ]] || die CONFIG "could not read the incumbent version" 64
  ROLLBACK_TAG="bosun:rollback-$INCUMBENT_VERSION"
  docker tag "$image" "$ROLLBACK_TAG" || die CONFIG "could not tag the rollback anchor" 64
  say "  rollback anchor: $ROLLBACK_TAG"

  step "Stage 2: shadow render"
  docker pull -q "$CANDIDATE" >/dev/null || die TRANSIENT "could not pull $CANDIDATE" 75
  local cfg cand_flag inc_flag cand_commit inc_commit
  cfg="$(live_config)" || die CONFIG "docker compose config failed for $LIVE" 64
  RUN_DIR="$(mktemp -d "$TMP_ROOT/bosun-canary.XXXXXX")"; chmod 700 "$RUN_DIR"
  cand_flag=0; has_no_alerts_flag "$CANDIDATE" && cand_flag=1
  inc_flag=0; has_no_alerts_flag "$INCUMBENT" && inc_flag=1
  [[ "$cand_flag" -eq 1 ]] || die CONFIG "the candidate has no 'reconcile --no-alerts'; it predates the canary and cannot be shadow-rendered" 64

  if ! cand_commit="$(shadow_run candidate "$CANDIDATE" "$cfg" 1)"; then
    keep_shadow_log candidate
    finish 5 CANDIDATE-FAILED
  fi
  say "  candidate rendered commit $cand_commit"

  local verdict
  if [[ "$inc_flag" -eq 0 ]]; then
    say "  RENDER-OK-NO-BASELINE: the incumbent predates reconcile CLI parity, so there is no baseline to compare"
    verdict=RENDER-OK-NO-BASELINE
  else
    inc_commit="$(shadow_run incumbent "$INCUMBENT" "$cfg" 1)" || { keep_shadow_log incumbent; finish 4 HARNESS-INVALID; }
    if [[ "$inc_commit" != "$cand_commit" ]]; then
      say "  commits differ (incumbent $inc_commit, candidate $cand_commit); re-running the incumbent once"
      inc_commit="$(shadow_run incumbent "$INCUMBENT" "$cfg" 1)" || { keep_shadow_log incumbent; finish 4 HARNESS-INVALID; }
      [[ "$inc_commit" == "$cand_commit" ]] || die TRANSIENT "the repo moved during the shadow render; retry" 75
    fi
    local diff_out
    diff_out="$(diff -rq "$RUN_DIR/incumbent/staging" "$RUN_DIR/candidate/staging" 2>&1)" || true
    if [[ -z "$diff_out" ]]; then
      verdict=RENDER-IDENTICAL
      say "  RENDER-IDENTICAL: both versions render the same staging tree"
    else
      verdict=RENDER-DIFFERS
      say "  RENDER-DIFFERS: the renders differ (names only):"
      sed -e "s#$RUN_DIR/incumbent/staging#incumbent#g" -e "s#$RUN_DIR/candidate/staging#candidate#g" <<<"$diff_out" | sed 's/^/    /'
    fi
  fi

  if [[ "$DRY_RUN" -eq 1 ]]; then finish 0 "$verdict (dry run, nothing changed)"; fi
  if [[ "$verdict" != RENDER-IDENTICAL || "$ASSUME_YES" -eq 0 ]]; then
    prompt_yes "Cut over bosun to $CANDIDATE?" || finish 0 "DECLINED at $verdict (nothing changed)"
  fi
  stage_cutover "$project" "$started"
}

main "$@"
