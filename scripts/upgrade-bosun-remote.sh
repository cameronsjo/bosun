#!/usr/bin/env bash
#
# NAS side of the bosun upgrade canary. scripts/upgrade-bosun.sh copies this
# file to the host, runs it over one ssh session, and deletes it afterwards.
# To resume after a lost connection, re-run the wrapper: it stages a fresh
# copy, and this script resumes from its state file.
#
#   bash upgrade-bosun-remote.sh [--dry-run] [--yes] [--watch-timeout SECONDS]
#                                [--expect-candidate REF] [--operator NAME]
#                                [--provenance-skipped]
#   bash upgrade-bosun-remote.sh --print-candidate
#   bash upgrade-bosun-remote.sh --record-provenance-failure [--operator NAME]
#
# Stages: 1 preflight, 2 shadow render (incumbent, then candidate, each a
# dry run of the same commit in a throwaway container), 3 cutover, 4 watch the
# first real reconcile, 5 roll back to the tagged incumbent if the watch fails.
#
# Exit codes. NOTE: exit 1 is a rollback, not "a check failed".
#   0  UPGRADED, or ALREADY-CURRENT, or a --dry-run that rendered cleanly
#   1  ROLLED-BACK        the candidate failed its watch; incumbent restored and healthy
#   2  FAULT-NOT-UPGRADE  the incumbent also fails after rollback; the fault is elsewhere
#   3  HALF-CHANGED       the rollback itself failed, or cannot be trusted; manual steps printed
#   4  HARNESS-INVALID    the incumbent failed its own shadow render
#   5  CANDIDATE-FAILED   the candidate failed before cutover; nothing changed
#  64  usage or configuration error; nothing changed
#  75  transient failure before cutover, a live run holding the lock, or an
#      interruption; retry. A re-run resumes a recorded cutover or rollback.
#  129/130/143  interrupted by HUP/INT/TERM; re-run to resume
#
# Secrets: shadow runs render decrypted secrets into a 0700 dir under /tmp
# (RAM on Unraid), removed on exit. Only file names, counts and verdicts reach
# stdout; failure detail goes to the NAS state directory.

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
CUTOVER_OVERRIDE="$STATE_DIR/cutover.override.yml"
LOCK="$STATE_DIR/lock"
# Lowercase: it becomes part of a compose project name.
RUN_ID="$(date -u +%Y%m%dt%H%M%Sz)-$$"

# Environment a shadow render may see. Everything else in the live service's
# environment -- alert webhooks, tokens, Sentry, OTel, the webhook secret -- is
# dropped. The container also gets no docker socket and no view of appdata
# (whose bosun/.env holds those secrets); see write_override.
ENV_ALLOWLIST='["TZ","BOSUN_REPO_URL","REPO_URL","BOSUN_REPO_BRANCH","REPO_BRANCH","BOSUN_INFRA_DIR","BOSUN_TARGETS","BOSUN_SECRETS_FILE","SECRETS_FILES","SOPS_AGE_KEY_FILE","BOSUN_SSH_KEY","BOSUN_SSH_KNOWN_HOSTS","BOSUN_GIT_FETCH_DEPTH","BOSUN_DEPLOY_PATHS","BOSUN_DEPLOY_SYNC_PATHS","BOSUN_DEPLOY_SYNC_EXCLUDE","BOSUN_TEMPLATE_INCLUDE_DIR"]'
# Kept byte-identical with upgrade-bosun.sh; the test suite checks it.
REF_RE='^[a-z0-9][a-z0-9./_-]*(:[A-Za-z0-9._-]+)?@sha256:[0-9a-f]{64}$'
ROLLBACK_TAG_RE='^bosun:rollback-[0-9A-Za-z._-]+$'
IMAGE_ID_RE='^sha256:[0-9a-f]{64}$'
PATH_RE='^/[A-Za-z0-9._/-]+$'

DRY_RUN=0 ASSUME_YES=0 WATCH_TIMEOUT=900 EXPECT_CANDIDATE="" PROVENANCE_SKIPPED=0
OPERATOR="${USER:-unknown}@$(hostname -s 2>/dev/null || echo nas)"
RUN_DIR="" LOCKED=0 FINISHING=0 CANDIDATE="" MUTATING=0
# The upgrade record: set at preflight, or loaded from the state file on resume.
PROJECT="" INCUMBENT="" INCUMBENT_IMAGE="" INCUMBENT_VERSION="" ROLLBACK_TAG="" CANDIDATE_IMAGE=""
AGE_SRC="" DEPLOY_SRC=""

say() { printf '%s\n' "$*"; }
step() { printf '\n==> %s\n' "$*"; }
die() { printf 'ERROR %s\n' "$2" >&2 || true; finish "$3" "$1"; }

usage() { awk 'NR == 1 { next } /^#/ { sub(/^# ?/, ""); print; next } { exit }' "$0"; exit 0; }

# cleanup stops shadow containers before deleting their output, so nothing is
# still writing rendered secrets while the directory goes. Every step
# tolerates failure: set -e is live inside the trap, and one failed step must
# not leave the lock or the secrets behind.
# shellcheck disable=SC2317,SC2329  # invoked by the EXIT trap
cleanup() {
  local ids
  ids="$(docker ps -aq --filter "name=^bosun-canary-$RUN_ID-" 2>/dev/null || true)"
  if [[ -n "$ids" ]]; then
    # shellcheck disable=SC2086  # ids is a newline list of hex container ids
    docker rm -f $ids >/dev/null 2>&1 || true
  fi
  if [[ -n "$RUN_DIR" && -d "$RUN_DIR" ]]; then rm -rf -- "$RUN_DIR" || true; fi
  if [[ "$LOCKED" -eq 1 ]]; then rm -f -- "$CUTOVER_OVERRIDE" || true; rm -rf -- "$LOCK" || true; fi
  return 0
}
trap cleanup EXIT
# A signal still gets a history line and, if the terminal is alive, a verdict.
# State is kept, so a re-run resumes an interrupted cutover.
# shellcheck disable=SC2317,SC2329  # invoked by the signal traps
on_signal() { [[ "$FINISHING" -eq 1 ]] && exit "$1"; finish "$1" "INTERRUPTED by $2 (re-run to resume)"; }
trap 'on_signal 129 HUP' HUP
trap 'on_signal 130 INT' INT
trap 'on_signal 143 TERM' TERM

# finish records one history line, prints the verdict and exits. Every verdict
# goes through here. Both writes are best-effort and the history line goes
# first: a full or read-only share, or a hung-up terminal, must not turn a
# verdict into a bare `set -e` exit 1, which the wrapper reads as ROLLED-BACK.
finish() {
  local code="$1" verdict="$2"
  FINISHING=1
  if [[ "$PROVENANCE_SKIPPED" -eq 1 ]]; then verdict="$verdict [provenance skipped: drill]"
  elif [[ -z "$EXPECT_CANDIDATE" && "$MUTATING" -eq 1 ]]; then verdict="$verdict [provenance: not checked by wrapper]"; fi
  if [[ -d "$STATE_DIR" && ! -L "$STATE_DIR" ]]; then
    # Each field is made printable before the line is assembled: a crafted pin
    # must not forge a line (newline) or a field (tab).
    { printf '%s\t%s\tincumbent=%s\tcandidate=%s\t%s\texit=%s\n' \
        "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$OPERATOR" "$(printable "${INCUMBENT:-?}")" \
        "$(printable "${CANDIDATE:-?}")" "$(printable "$verdict")" "$code" >> "$HISTORY"; } 2>/dev/null ||
      { printf 'WARNING could not append to %s\n' "$HISTORY" >&2 || true; }
  fi
  printf '\nVERDICT: %s (exit %s)\n' "$verdict" "$code" 2>/dev/null || true
  exit "$code"
}

# printable replaces every control character (tab and newline included) with
# "?", for values that reach the history file or the terminal unvalidated.
printable() { printf '%s' "$1" | LC_ALL=C tr '\000-\037\177' '?'; }

# forget_file removes a state artifact; failing to is worth a warning, never a
# different verdict.
forget_file() { rm -f -- "$@" 2>/dev/null || say "  WARNING could not remove $*"; }

digest_of() { printf '%s' "${1##*@}"; }
# same_image compares by digest: a RepoDigest carries no tag, the pin does.
same_image() { [[ -n "$1" && -n "$2" && "$(digest_of "$1")" == "$(digest_of "$2")" ]]; }
# release_version prints the X.Y.Z of a pin tagged X.Y.Z or vX.Y.Z, and
# nothing for any other tag (latest, 0.43, none): only a full release tag can
# be compared with `bosun --version`.
release_version() {
  local name="${1%@*}" tag=""
  if [[ "${name##*/}" == *:* ]]; then tag="${name##*:}"; fi
  tag="${tag#v}"
  if [[ "$tag" =~ ^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$ ]]; then printf '%s' "$tag"; fi
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

image_id_of() { docker image inspect -f '{{.Id}}' "$1"; }
version_of_image() { docker run --rm --entrypoint bosun "$1" --version 2>/dev/null | head -n1; }

# save_phase writes the upgrade record with its phase; any extra key=value
# arguments are appended. Returns non-zero on any write failure; every caller
# decides what that means.
save_phase() {
  local phase="$1" tmp="$STATE_FILE.tmp"; shift
  { printf '%s\n' "phase=$phase" "project=$PROJECT" "incumbent=$INCUMBENT" "incumbent_image=$INCUMBENT_IMAGE" \
      "incumbent_version=$INCUMBENT_VERSION" "rollback_tag=$ROLLBACK_TAG" "candidate=$CANDIDATE" \
      "candidate_image=$CANDIDATE_IMAGE" "$@" > "$tmp"; } 2>/dev/null || return 1
  mv -f "$tmp" "$STATE_FILE" 2>/dev/null
}

# load_state reads the state file once into STATE_PHASE and the record
# globals, and fails unless every value passes its allowlist: these values
# reach compose files and docker arguments.
STATE_PHASE="" STATE_CANDIDATE="" STATE_REASON=""
load_state() {
  local k v
  [[ -f "$STATE_FILE" ]] || return 0
  while IFS='=' read -r k v; do
    case "$k" in
      phase) STATE_PHASE="$v" ;;
      project) PROJECT="$v" ;;
      incumbent) INCUMBENT="$v" ;;
      incumbent_image) INCUMBENT_IMAGE="$v" ;;
      incumbent_version) INCUMBENT_VERSION="$v" ;;
      rollback_tag) ROLLBACK_TAG="$v" ;;
      candidate) STATE_CANDIDATE="$v" ;;
      candidate_image) CANDIDATE_IMAGE="$v" ;;
      reason) STATE_REASON="$v" ;;
    esac
  done < "$STATE_FILE"
  [[ "$STATE_PHASE" =~ ^(cutover|watching|rollback)$ ]] &&
    [[ "$PROJECT" =~ ^[a-z0-9][a-z0-9_-]*$ ]] &&
    [[ "$ROLLBACK_TAG" =~ $ROLLBACK_TAG_RE ]] &&
    [[ "$INCUMBENT_IMAGE" =~ $IMAGE_ID_RE ]] &&
    [[ "$CANDIDATE_IMAGE" =~ $IMAGE_ID_RE ]] &&
    [[ "$INCUMBENT_VERSION" =~ ^[0-9A-Za-z._-]+$ ]] &&
    [[ "$INCUMBENT" =~ $REF_RE ]] &&
    [[ "$STATE_CANDIDATE" =~ $REF_RE ]]
}

# record_failure is best-effort: losing the detail must never block a rollback.
record_failure() {
  local label="$1"
  local out="$STATE_DIR/failures/$RUN_ID-$label.log"
  if mkdir -p "$STATE_DIR/failures" 2>/dev/null && {
    printf '== %s\n' "$label"
    docker exec "$CONTAINER" bosun daemon-status --json 2>&1 || true
    docker inspect -f 'restarts={{.RestartCount}} status={{.State.Status}}' "$CONTAINER" 2>&1 || true
    docker logs --tail 40 "$CONTAINER" 2>&1 || true
  } > "$out" 2>/dev/null; then
    chmod 600 "$out" 2>/dev/null || true
    say "  failure detail kept on the NAS: $out"
  else
    say "  WARNING could not write failure detail to $out"
  fi
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

# mount_source prints the host path mounted at container path $2, and fails
# unless there is exactly one and it is a plain absolute path: the value goes
# into YAML verbatim.
mount_source() {
  local src
  src="$(jq -r --arg t "$2" '[.services.bosun.volumes[]? | select(.target == $t) | .source] | if length == 1 then .[0] else empty end' <<<"$1")" || return 1
  [[ "$src" =~ $PATH_RE ]] || return 1
  printf '%s' "$src"
}

# acquire_lock takes the state-dir lock and records its owner. A lock whose
# owner is gone (killed, or the NAS rebooted) is reclaimed; a live owner --
# for example a run that kept going after the ssh session dropped -- is
# reported with its pid, never overridden.
acquire_lock() {
  local boot owner="" owner_pid="" owner_boot="" moved=""
  boot="$(cat /proc/sys/kernel/random/boot_id 2>/dev/null || echo unknown)"
  if ! mkdir "$LOCK" 2>/dev/null; then
    owner="$(cat "$LOCK/owner" 2>/dev/null || true)"
    read -r owner_pid owner_boot <<<"$owner" || true
    # Stale means the owner is known and provably gone: either it belongs to
    # an earlier boot (its pid cannot still be that run) or it is this boot and
    # the pid is gone. A lock with no owner file yet is a run between its mkdir
    # and its owner write, so it counts as live.
    if ! [[ "$owner_pid" =~ ^[0-9]+$ ]] ||
      { [[ "$owner_boot" == "$boot" ]] && kill -0 "$owner_pid" 2>/dev/null; }; then
      say "Another upgrade run (pid ${owner_pid:-unknown}) holds $LOCK."
      say "A live run finishes on its own; follow it in $HISTORY. Re-run after it exits to resume anything it left."
      say "If no run is active and the owner is unknown, remove $LOCK by hand."
      printf '\nVERDICT: LOCKED (exit 75)\n'; exit 75
    fi
    # Rename the stale lock away: the rename is atomic, so of two racing runs
    # only one moves it. Then confirm we moved the dead owner's lock and not a
    # fresh one another run published in between; if not, put it back.
    if [[ "$owner_boot" == "$boot" ]]; then
      say "  reclaiming a stale lock (owner pid $owner_pid is gone)"
    else
      say "  reclaiming a stale lock (owner pid $owner_pid belongs to an earlier boot)"
    fi
    rm -rf -- "$LOCK.stale.$$" 2>/dev/null || true
    mv "$LOCK" "$LOCK.stale.$$" 2>/dev/null || { printf '\nVERDICT: LOCKED (exit 75)\n'; exit 75; }
    moved="$(cat "$LOCK.stale.$$/owner" 2>/dev/null || true)"
    if [[ "$moved" != "$owner" ]]; then
      if [[ ! -e "$LOCK" ]]; then mv "$LOCK.stale.$$" "$LOCK" 2>/dev/null || true; fi
      printf '\nVERDICT: LOCKED (exit 75)\n'; exit 75
    fi
    rm -rf -- "$LOCK.stale.$$" 2>/dev/null || true
    mkdir "$LOCK" 2>/dev/null || { printf '\nVERDICT: LOCKED (exit 75)\n'; exit 75; }
  fi
  LOCKED=1 MUTATING=1
  printf '%s %s\n' "$$" "$boot" > "$LOCK/owner" 2>/dev/null || true
}

# write_shadow emits a standalone compose file for one shadow role. It is an
# allowlist: the service is built from scratch, never merged with the live
# one, so nothing the live service carries (the docker socket, appdata,
# capabilities, devices, cgroup rules, labels) can reach the shadow. Only
# allowlisted env values and the two key paths are read from the live config.
# /mnt/appdata is an empty directory: deploy-mode detection only stats it, and
# the real appdata holds bosun/.env with the secrets the env allowlist drops.
write_shadow() {
  local role="$1" image="$2" dir="$3" cfg="$4" env_yaml
  env_yaml="$(jq -r --argjson allow "$ENV_ALLOWLIST" '
      .services.bosun.environment // {} | to_entries
      | map(select(.key as $k | $allow | index($k)))
      | .[] | "      \(.key): \(.value | tostring | gsub("\\$"; "$$") | @json)"' <<<"$cfg")" || return 1
  mkdir -p "$dir/appdata-empty" || return 1
  cat > "$dir/shadow.yml" <<EOF
# Generated by upgrade-bosun-remote.sh for the $role shadow render. Throwaway.
services:
  $SERVICE:
    image: "$image"
    # root, to read the root-owned 0600 key files; no capabilities beyond that.
    user: "0:0"
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    network_mode: bridge
    environment:
$env_yaml
      DRY_RUN: "true"
      BOSUN_LOG_FORMAT: "json"
      REPO_DIR: "/work/repo"
      STAGING_DIR: "/work/staging"
      LOG_DIR: "/work/logs"
      BACKUP_DIR: "/work/backups"
      BOSUN_STATE_DIR: "/work/state"
    volumes:
      - "$AGE_SRC:/config/age-key.txt:ro"
      - "$DEPLOY_SRC:/config/deploy-key:ro"
      - "$dir/appdata-empty:/mnt/appdata:ro"
      - "$dir:/work"
    labels:
      - com.centurylinklabs.watchtower.enable=false
EOF
}

# shadow_run renders one role and prints the commit it rendered. It fails if
# the render fails, logs no commit, or leaves an empty staging tree (two empty
# trees would otherwise compare as RENDER-IDENTICAL).
shadow_run() {
  local role="$1" image="$2" cfg="$3" commit first
  local dir="$RUN_DIR/$role"
  rm -rf -- "$dir"
  # repo/ and staging/ are left for bosun to create: it checks the staging
  # root's mode, and a pre-created 0755 dir would fail that check.
  if ! mkdir -p "$dir/logs" "$dir/backups" "$dir/state" || ! chmod 700 "$dir"; then return 1; fi
  write_shadow "$role" "$image" "$dir" "$cfg" || return 1
  if ! docker compose -p "bosun-canary-$RUN_ID" -f "$dir/shadow.yml" run --rm --no-deps -T \
      --name "bosun-canary-$RUN_ID-$role" "$SERVICE" bosun reconcile --dry-run --no-alerts > "$dir/run.log" 2>&1; then
    return 1
  fi
  commit="$(grep '"Reconcile pipeline completed"' "$dir/run.log" | grep -o '"commit":"[0-9a-f]\{40\}"' | head -n1 | cut -d'"' -f4)" || true
  [[ -n "$commit" ]] || { printf '  %s logged no completed render\n' "$role" >&2; return 1; }
  first="$(find "$dir/staging" -type f -print -quit 2>/dev/null)" || first=""
  [[ -n "$first" ]] || { printf '  %s rendered an empty staging tree\n' "$role" >&2; return 1; }
  printf '%s' "$commit"
}

# keep_shadow_log keeps log lines only; the rendered tree stays in RAM and is
# deleted on exit. A template error can quote a rendered line, so the copy is
# 0600 in the root-only state dir. That exposes nothing new: the same rendered
# secrets already sit in plaintext under appdata once bosun deploys them.
keep_shadow_log() {
  local out="$STATE_DIR/failures/$RUN_ID-shadow-$1.log"
  if mkdir -p "$STATE_DIR/failures" 2>/dev/null && tail -n 60 "$RUN_DIR/$1/run.log" > "$out" 2>/dev/null; then
    chmod 600 "$out" 2>/dev/null || true
    say "  last 60 log lines kept on the NAS: $out"
  else
    say "  WARNING could not keep the shadow log in $STATE_DIR/failures"
  fi
}

# watch_reconcile waits for the first reconcile cycle to finish after the
# container's StartedAt, and succeeds only if it finished without an error,
# while the container kept running image $1 with no restart and no panic.
watch_reconcile() {
  local expect_image="$1" label="$2" started started_epoch deadline line image status restarts lr le lr_epoch ds logs
  line="$(inspect_live)" || { say "  FAIL container $CONTAINER is gone"; return 1; }
  read -r _ _ started _ _ <<<"$line"
  started_epoch="$(to_epoch "$started")" || { say "  FAIL could not read StartedAt"; return 1; }
  deadline=$(( $(date +%s) + WATCH_TIMEOUT ))
  say "  watching for a reconcile after $started (timeout ${WATCH_TIMEOUT}s)"
  while :; do
    line="$(inspect_live)" || { say "  FAIL container $CONTAINER disappeared (expected: running)"; return 1; }
    read -r image status _ restarts _ <<<"$line"
    if [[ "$status" != running || "$restarts" != 0 ]]; then
      say "  FAIL container status=$status restarts=$restarts (expected: running, 0 restarts)"; return 1
    fi
    if [[ "$image" != "$expect_image" ]]; then
      say "  FAIL running image $image changed under the watch (expected: $expect_image)"; return 1
    fi
    # Captured, not piped: `docker logs | grep -q` under pipefail reports
    # failure exactly when grep finds a match early. A failed read is a
    # failure, not a clean log.
    logs="$(docker logs --since "$started" "$CONTAINER" 2>&1)" || { say "  FAIL could not read the daemon log"; return 1; }
    if [[ "$logs" == *'panic:'* ]]; then
      say "  FAIL the daemon panicked"; return 1
    fi
    ds="$(docker exec "$CONTAINER" bosun daemon-status --json 2>/dev/null)" || ds=""
    # One jq pass. Split on the first "|": a timestamp never contains one, and
    # an empty field survives (a tab-IFS read would collapse it).
    ds="$(jq -r '"\(.last_reconcile // "")|\(.last_error // "")"' <<<"$ds" 2>/dev/null)" || ds="|"
    lr="${ds%%|*}"; le="${ds#*|}"
    if [[ -n "$lr" ]] && lr_epoch="$(to_epoch "$lr")" && (( lr_epoch >= started_epoch )); then
      if [[ -n "$le" ]]; then
        say "  FAIL first reconcile ended with an error (expected: last_error empty; error text is in the NAS failure log)"
        return 1
      fi
      say "  PASS $label: reconcile finished at $lr with no error"
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
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

# stage_rollback restores the incumbent from the record in the globals, which
# preflight set or load_state validated.
stage_rollback() {
  local reason="${1//[^A-Za-z0-9 ._:()\'-]/}"
  local manual="cd $COMPOSE_DIR && docker compose -p $PROJECT -f docker-compose.yml -f $ROLLBACK_OVERRIDE up -d --pull never $SERVICE"
  step "Stage 5: rolling back to $ROLLBACK_TAG ($reason)"
  if ! save_phase rollback "reason=$reason" ||
    ! { cat > "$ROLLBACK_OVERRIDE" <<EOF
# Written $(date -u +%Y-%m-%dT%H:%M:%SZ) by upgrade-bosun-remote.sh.
# Reason: $reason
# Keeps bosun on the pre-upgrade image while the pin names the failed candidate
# ($CANDIDATE). Revert the pin PR in homelab, then delete this file.
# The image is the incumbent's immutable digest; $ROLLBACK_TAG is kept
# only so the image cannot be pruned.
services:
  $SERVICE:
    image: "$INCUMBENT"
EOF
    } 2>/dev/null; then
    say "  FAIL could not write to $STATE_DIR, so the rollback override does not exist."
    say "  Manual recovery, on the NAS: write $ROLLBACK_OVERRIDE with image \"$INCUMBENT\" for service $SERVICE, then:"
    say "    $manual"
    finish 3 HALF-CHANGED
  fi
  if ! compose_up "$PROJECT" -f "$LIVE" -f "$ROLLBACK_OVERRIDE" || ! wait_running; then
    say "  FAIL the rollback did not start. Manual recovery, on the NAS:"
    say "    $manual"
    say "    docker image inspect $ROLLBACK_TAG   # the anchor; do not prune it"
    finish 3 HALF-CHANGED
  fi
  # The tag is mutable; the image ID recorded at preflight is not.
  local line running_image=""
  if line="$(inspect_live)"; then read -r running_image _ _ _ _ <<<"$line"; fi
  if [[ "$running_image" != "$INCUMBENT_IMAGE" ]]; then
    say "  FAIL rolled-back container runs image '$running_image' (expected: $INCUMBENT_IMAGE, the incumbent recorded at preflight)"
    finish 3 HALF-CHANGED
  fi
  if watch_reconcile "$INCUMBENT_IMAGE" "incumbent after rollback"; then
    forget_file "$STATE_FILE"
    say "  The pin still names the failed candidate. Revert it in homelab, then delete $ROLLBACK_OVERRIDE."
    finish 1 ROLLED-BACK
  fi
  record_failure incumbent-after-rollback
  forget_file "$STATE_FILE"
  say "  The incumbent fails too, so the upgrade is not the cause. See homelab docs/runbooks/bosun-deploys-blocked.md."
  finish 2 FAULT-NOT-UPGRADE
}

stage_watch() {
  step "Stage 4: watching the first real reconcile"
  if watch_reconcile "$CANDIDATE_IMAGE" "candidate"; then
    forget_file "$STATE_FILE" "$ROLLBACK_OVERRIDE"
    finish 0 UPGRADED
  fi
  record_failure candidate
  stage_rollback "candidate failed its first reconcile"
}

stage_cutover() {
  local before_started="$1" line image started version tag up_ok=1
  step "Stage 3: cutover"
  # Nothing has changed yet, so a failed write here aborts cleanly.
  save_phase cutover || die TRANSIENT "could not write $STATE_FILE; nothing changed" 75
  # Pin the exact digest that was verified and shadow-rendered. The live file
  # is re-synced from git while this runs, so re-reading it could start a
  # different image. The rollback override is dropped on purpose.
  { printf 'services:\n  %s:\n    image: "%s"\n' "$SERVICE" "$CANDIDATE" > "$CUTOVER_OVERRIDE"; } 2>/dev/null || {
    forget_file "$STATE_FILE"
    die TRANSIENT "could not write $CUTOVER_OVERRIDE; nothing changed" 75
  }
  if ! compose_up "$PROJECT" -f "$LIVE" -f "$CUTOVER_OVERRIDE" || ! wait_running; then up_ok=0; fi
  forget_file "$CUTOVER_OVERRIDE"
  if [[ "$up_ok" -eq 0 ]]; then
    record_failure cutover
    stage_rollback "compose up of the candidate failed"
  fi
  line="$(inspect_live)" || stage_rollback "container missing after cutover"
  read -r image _ started _ _ <<<"$line"
  if [[ "$image" != "$CANDIDATE_IMAGE" ]]; then stage_rollback "running image is not the candidate"; fi
  if [[ "$started" == "$before_started" ]]; then stage_rollback "container was not recreated (StartedAt unchanged)"; fi
  version="$(docker exec "$CONTAINER" bosun --version 2>/dev/null | head -n1)" || version=""
  [[ -n "$version" ]] || stage_rollback "the candidate does not answer bosun --version"
  tag="$(release_version "$CANDIDATE")"
  if [[ -n "$tag" && "$version" != "bosun version $tag" ]]; then
    stage_rollback "candidate reports '$version' (expected: bosun version $tag)"
  fi
  say "  PASS candidate is running ($version), started $started"
  # phase=cutover already makes a re-run resume, so this write is advisory.
  save_phase watching "started=$started" || say "  WARNING could not record phase=watching; a re-run still resumes from phase=cutover"
  stage_watch
}

# resume finishes an upgrade the state file records. It never returns.
resume() {
  local line image=""
  say "  an interrupted upgrade is recorded (phase=$STATE_PHASE)"
  if line="$(inspect_live)"; then read -r image _ _ _ _ <<<"$line"; fi
  if [[ "$STATE_CANDIDATE" != "$CANDIDATE" ]]; then
    if same_image "$CANDIDATE" "$INCUMBENT"; then
      # The pin was reverted while a candidate may be live: restore what it names.
      CANDIDATE="$STATE_CANDIDATE"
      if [[ "$image" == "$INCUMBENT_IMAGE" ]]; then
        forget_file "$STATE_FILE" "$ROLLBACK_OVERRIDE"
        finish 0 "ALREADY-CURRENT (pin reverted; the incumbent is running)"
      fi
      stage_rollback "the pin was reverted to the incumbent"
    fi
    say "  FAIL the pin moved to a third image ($CANDIDATE) during an upgrade of $STATE_CANDIDATE."
    say "  Running image: ${image:-none}. Decide by hand, then remove $STATE_FILE."
    CANDIDATE="$STATE_CANDIDATE"
    finish 3 HALF-CHANGED
  fi
  case "$STATE_PHASE" in
    cutover|watching)
      if [[ "$image" == "$CANDIDATE_IMAGE" ]]; then stage_watch; fi
      if [[ "$STATE_PHASE" == cutover && "$image" == "$INCUMBENT_IMAGE" ]]; then
        # compose up never replaced the incumbent: nothing changed.
        forget_file "$STATE_FILE"
        finish 75 "INTERRUPTED-BEFORE-CUTOVER (nothing changed; re-run)"
      fi
      stage_rollback "resumed: candidate not running" ;;
    rollback) stage_rollback "${STATE_REASON:-resumed rollback}" ;;
  esac
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
      --provenance-skipped) PROVENANCE_SKIPPED=1; shift ;;
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
  if [[ "$PROVENANCE_SKIPPED" -eq 1 && "$ASSUME_YES" -eq 1 ]]; then
    die USAGE "--yes is refused when provenance was skipped; an unverified image always gets the prompt" 64
  fi
  [[ -f "$LIVE" ]] || die CONFIG "no compose file at $LIVE" 64
  [[ ! -L "$STATE_DIR" ]] || die CONFIG "$STATE_DIR is a symlink; refusing it" 64

  CANDIDATE="$(read_candidate)" || die CONFIG "could not read the $SERVICE image from $LIVE" 64
  if [[ "$print_candidate" -eq 1 ]]; then printf '%s\n' "$CANDIDATE"; exit 0; fi
  { mkdir -p "$STATE_DIR" && chmod 700 "$STATE_DIR"; } 2>/dev/null || die CONFIG "cannot create or restrict $STATE_DIR" 64
  if [[ "$provenance_failed" -eq 1 ]]; then
    # The Mac side found no valid provenance; record it here so the NAS
    # history holds every outcome.
    finish 5 CANDIDATE-FAILED-PROVENANCE
  fi
  [[ "$CANDIDATE" =~ $REF_RE ]] || die CONFIG "the pinned image '$(printable "$CANDIDATE")' carries no @sha256: digest; refusing an unpinned candidate" 64
  if [[ -n "$EXPECT_CANDIDATE" && "$EXPECT_CANDIDATE" != "$CANDIDATE" ]]; then
    die CONFIG "the pin changed since provenance was checked (checked $(printable "$EXPECT_CANDIDATE"), now $CANDIDATE)" 64
  fi

  acquire_lock

  step "Stage 1: preflight"
  # The state file is read before the container: an interrupted cutover can
  # leave no container at all, and that is exactly when a resume must reach
  # the rollback.
  if [[ -f "$STATE_FILE" ]]; then
    if [[ "$DRY_RUN" -eq 1 ]]; then
      die CONFIG "--dry-run never resumes a cutover or rollback; re-run without --dry-run to finish it" 64
    fi
    load_state || die CONFIG "$STATE_FILE is malformed; inspect it and resolve by hand" 64
    resume
  fi

  local line image started
  line="$(inspect_live)" || die CONFIG "container $CONTAINER not found" 64
  read -r image _ started _ PROJECT <<<"$line"
  [[ "$PROJECT" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || die CONFIG "container $CONTAINER carries no usable compose project label" 64
  INCUMBENT="$(running_ref_for "$image")" || INCUMBENT=""
  [[ "$INCUMBENT" =~ $REF_RE ]] || die CONFIG "the running image has no registry digest to anchor a rollback" 64
  say "  incumbent: $INCUMBENT"
  say "  candidate: $CANDIDATE"

  if same_image "$INCUMBENT" "$CANDIDATE"; then
    finish 0 ALREADY-CURRENT
  fi
  local last_run=""
  if [[ -f "$HISTORY" ]]; then last_run="$(grep -F "candidate=$CANDIDATE" "$HISTORY" 2>/dev/null | tail -n1 || true)"; fi
  if [[ "$last_run" == *ROLLED-BACK* ]]; then
    say "  WARNING this candidate was rolled back before; see $HISTORY"
  fi
  if [[ -f "$ROLLBACK_OVERRIDE" ]]; then
    say "  WARNING $ROLLBACK_OVERRIDE is in effect; a successful upgrade removes it"
  fi

  # Leftovers of an earlier run. We hold the lock, so none of these are live.
  local stale
  stale="$(docker ps -aq --filter 'name=^bosun-canary-' 2>/dev/null || true)"
  if [[ -n "$stale" ]]; then
    # shellcheck disable=SC2086  # newline list of hex container ids
    docker rm -f $stale >/dev/null 2>&1 || true
  fi
  for stale in "$TMP_ROOT"/bosun-canary.*; do
    if [[ -e "$stale" ]]; then rm -rf -- "$stale" || true; fi
  done

  INCUMBENT_VERSION="$(docker exec "$CONTAINER" bosun --version 2>/dev/null | head -n1 | sed -n 's/^bosun version //p')" || INCUMBENT_VERSION=""
  [[ "$INCUMBENT_VERSION" =~ ^[0-9A-Za-z._-]+$ ]] || die CONFIG "could not read the incumbent version" 64
  INCUMBENT_IMAGE="$image"
  [[ "$INCUMBENT_IMAGE" =~ $IMAGE_ID_RE ]] || die CONFIG "unexpected image id '$INCUMBENT_IMAGE' for $CONTAINER" 64
  ROLLBACK_TAG="bosun:rollback-$INCUMBENT_VERSION"
  docker tag "$image" "$ROLLBACK_TAG" || die CONFIG "could not tag the rollback anchor" 64
  say "  rollback anchor: $ROLLBACK_TAG"

  step "Stage 2: shadow render"
  docker pull -q "$CANDIDATE" >/dev/null || die TRANSIENT "could not pull $CANDIDATE" 75
  CANDIDATE_IMAGE="$(image_id_of "$CANDIDATE")" || CANDIDATE_IMAGE=""
  [[ "$CANDIDATE_IMAGE" =~ $IMAGE_ID_RE ]] || die TRANSIENT "could not resolve the pulled candidate's image id" 75
  # A pin whose tag and digest disagree is caught here, before cutover.
  local want_version cand_version
  want_version="$(release_version "$CANDIDATE")"
  cand_version="$(version_of_image "$CANDIDATE")" || cand_version=""
  [[ -n "$cand_version" ]] || die CANDIDATE-FAILED "the candidate does not answer bosun --version" 5
  if [[ -n "$want_version" && "$cand_version" != "bosun version $want_version" ]]; then
    die CANDIDATE-FAILED "the pin's tag says $want_version but the image reports '$cand_version'" 5
  fi
  local cfg inc_flag inc_commit="" cand_commit
  cfg="$(live_config)" || die CONFIG "docker compose config failed for $LIVE" 64
  AGE_SRC="$(mount_source "$cfg" /config/age-key.txt)" || AGE_SRC=""
  DEPLOY_SRC="$(mount_source "$cfg" /config/deploy-key)" || DEPLOY_SRC=""
  if [[ -z "$AGE_SRC" || -z "$DEPLOY_SRC" ]]; then
    die CONFIG "$LIVE needs exactly one plain-path mount each for /config/age-key.txt and /config/deploy-key" 64
  fi
  RUN_DIR="$(mktemp -d "$TMP_ROOT/bosun-canary.XXXXXX")" || die TRANSIENT "could not create a shadow directory under $TMP_ROOT" 75
  chmod 700 "$RUN_DIR" || die TRANSIENT "could not restrict $RUN_DIR" 75
  has_no_alerts_flag "$CANDIDATE" || die CONFIG "the candidate has no 'reconcile --no-alerts'; it predates the canary and cannot be shadow-rendered" 64
  inc_flag=0; if has_no_alerts_flag "$INCUMBENT"; then inc_flag=1; fi

  # The incumbent renders first. Its failure means the harness or the repo is
  # broken (HARNESS-INVALID), so a later candidate failure is the candidate's.
  # If a push lands between the two renders, the candidate saw the newer
  # commit; re-running the incumbent (the older snapshot) converges on it.
  if [[ "$inc_flag" -eq 1 ]]; then
    inc_commit="$(shadow_run incumbent "$INCUMBENT" "$cfg")" || { keep_shadow_log incumbent; finish 4 HARNESS-INVALID; }
    say "  incumbent rendered commit $inc_commit"
  fi
  if ! cand_commit="$(shadow_run candidate "$CANDIDATE" "$cfg")"; then
    keep_shadow_log candidate
    finish 5 CANDIDATE-FAILED
  fi
  say "  candidate rendered commit $cand_commit"

  local verdict
  if [[ "$inc_flag" -eq 0 ]]; then
    say "  RENDER-OK-NO-BASELINE: the incumbent predates reconcile CLI parity, so there is no baseline to compare"
    verdict=RENDER-OK-NO-BASELINE
  else
    if [[ "$inc_commit" != "$cand_commit" ]]; then
      say "  commits differ (incumbent $inc_commit, candidate $cand_commit); re-running the incumbent once"
      inc_commit="$(shadow_run incumbent "$INCUMBENT" "$cfg")" || { keep_shadow_log incumbent; finish 4 HARNESS-INVALID; }
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
      # File names come from the rendered repo: made printable, one per line.
      while IFS= read -r line; do
        line="${line//"$RUN_DIR/incumbent/staging"/incumbent}"
        say "    $(printable "${line//"$RUN_DIR/candidate/staging"/candidate}")"
      done <<<"$diff_out"
    fi
  fi

  if [[ "$DRY_RUN" -eq 1 ]]; then finish 0 "$verdict (dry run, nothing changed)"; fi
  if [[ "$verdict" != RENDER-IDENTICAL || "$ASSUME_YES" -eq 0 ]]; then
    prompt_yes "Cut over bosun to $CANDIDATE?" || finish 0 "DECLINED at $verdict (nothing changed)"
  fi
  stage_cutover "$started"
}

main "$@"
