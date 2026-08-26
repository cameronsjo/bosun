#!/bin/sh

set -u

readonly DEFAULT_MIN_FREE_GIB=20
readonly DEFAULT_MAX_DELTA_GIB=8
readonly DEFAULT_WAIT_SECONDS=60
readonly SIGNAL_GRACE_SECONDS=1
readonly KIB_PER_GIB=1048576

fail() {
	printf 'agent-go-gate: ERROR: %s\n' "$*" >&2
	exit 64
}

temporary_fail() {
	printf 'agent-go-gate: ERROR: %s\n' "$*" >&2
	exit 75
}

usage() {
	printf 'usage: %s COMMAND [ARG ...]\n' "$0" >&2
	exit 64
}

require_uint() {
	case "$2" in
		''|*[!0-9]*) fail "$1 must be a non-negative integer, got '$2'" ;;
	esac
}

free_kib() {
	LC_ALL=C command df -Pk "$1" | awk 'NR == 2 { print $4 }'
}

format_gib() {
	awk -v kib="$1" 'BEGIN { printf "%.1f", kib / 1048576 }'
}

canonical_dir() {
	(
		cd "$1" 2>/dev/null || exit 1
		pwd -P
	)
}

path_identity() {
	LC_ALL=C command ls -di -- "$1" 2>/dev/null | awk 'NR == 1 { print $1 }'
}

read_record_from() {
	record_value=''
	if [ -r "$1" ]; then
		IFS= read -r record_value < "$1" || [ -n "$record_value" ] || record_value=''
	fi
	printf '%s' "$record_value"
}

parse_record() {
	parsed_pid=''
	parsed_token=''
	case "$1" in
		*' '*)
			parsed_pid=${1%% *}
			parsed_token=${1#* }
			;;
		*) return 1 ;;
	esac
	case "$parsed_pid" in
		''|*[!0-9]*) return 1 ;;
	esac
	case "$parsed_token" in
		"$lock_basename".owner.*) ;;
		*) return 1 ;;
	esac
	case "$parsed_token" in
		*/*|*' '*) return 1 ;;
	esac
}

remove_verified_link() {
	remove_path=$1
	remove_identity=$2
	[ -n "$remove_identity" ] || return 0
	if [ "$(path_identity "$remove_path")" = "$remove_identity" ]; then
		command rm -f -- "$remove_path"
	fi
}

cleanup_links_for_identity() {
	cleanup_identity=$1
	for cleanup_path in "$lock_file".owner.* "$lock_file".capture.*; do
		[ -e "$cleanup_path" ] || continue
		remove_verified_link "$cleanup_path" "$cleanup_identity"
	done
}

cleanup_orphan_links() {
	for orphan_path in "$lock_file".owner.* "$lock_file".capture.*; do
		[ -e "$orphan_path" ] || continue
		orphan_identity=$(path_identity "$orphan_path")
		orphan_record=$(read_record_from "$orphan_path")
		if parse_record "$orphan_record" && ! command kill -0 "$parsed_pid" 2>/dev/null; then
			remove_verified_link "$orphan_path" "$orphan_identity"
		fi
	done
}

create_owner_file() {
	owner_file=$(umask 077 && command mktemp "${lock_file}.owner.$$.XXXXXX") || fail 'cannot create private gate owner file'
	owner_token=${owner_file##*/}
	owner_record="$$ $owner_token"
	if ! (umask 077 && printf '%s\n' "$owner_record" > "$owner_file"); then
		command rm -f -- "$owner_file"
		fail 'cannot publish private gate owner record'
	fi
	owner_identity=$(path_identity "$owner_file")
	[ -n "$owner_identity" ] || fail 'cannot identify private gate owner file'
}

# shellcheck disable=SC2329 # Invoked by the EXIT trap.
release_lock() {
	if [ "${lock_owned:-false}" = true ]; then
		capture_file="${lock_file}.capture.release.${owner_token}"
		if [ ! -e "$capture_file" ] && command ln -P "$lock_file" "$capture_file" 2>/dev/null; then
			capture_identity=$(path_identity "$capture_file")
			capture_record=$(read_record_from "$capture_file")
			current_identity=$(path_identity "$lock_file")
			if [ "$capture_identity" = "$owned_identity" ] &&
				[ "$capture_record" = "$owner_record" ] &&
				[ "$current_identity" = "$owned_identity" ]; then
				command rm -f -- "$lock_file"
			fi
			remove_verified_link "$capture_file" "$capture_identity"
		fi
		lock_owned=false
		owned_identity=''
	fi
	if [ -n "${owner_file:-}" ]; then
		remove_verified_link "$owner_file" "${owner_identity:-}"
	fi
}

reclaim_stale_lock() {
	expected_identity=$1
	expected_record=$2
	capture_file="${lock_file}.capture.reclaim.${owner_token}.${expected_identity}"
	[ ! -e "$capture_file" ] || return 0
	if command ln -P "$lock_file" "$capture_file" 2>/dev/null; then
		capture_identity=$(path_identity "$capture_file")
		capture_record=$(read_record_from "$capture_file")
		current_identity=$(path_identity "$lock_file")
		current_record=$(read_record_from "$lock_file")
		if [ "$capture_identity" = "$expected_identity" ] &&
			[ "$capture_record" = "$expected_record" ] &&
			[ "$current_identity" = "$expected_identity" ] &&
			[ "$current_record" = "$expected_record" ]; then
			command rm -f -- "$lock_file"
			if [ "$(path_identity "$lock_file")" != "$expected_identity" ]; then
				cleanup_links_for_identity "$expected_identity"
			fi
		fi
		remove_verified_link "$capture_file" "$capture_identity"
	fi
}

acquire_lock() {
	waited=0
	reported_wait=false
	while ! command ln -P "$owner_file" "$lock_file" 2>/dev/null; do
		current_identity=$(path_identity "$lock_file")
		current_record=$(read_record_from "$lock_file")
		if parse_record "$current_record"; then
			owner_pid=$parsed_pid
			if ! command kill -0 "$owner_pid" 2>/dev/null; then
				reclaim_stale_lock "$current_identity" "$current_record"
			elif [ "$reported_wait" = false ]; then
				printf 'agent-go-gate: waiting for shared gate held by PID %s\n' "$owner_pid" >&2
				reported_wait=true
			fi
		else
			owner_pid=''
			reclaim_stale_lock "$current_identity" "$current_record"
		fi

		if [ "$waited" -ge "$wait_seconds" ] && [ -e "$lock_file" ]; then
			case "$owner_pid" in
				''|*[!0-9]*)
					temporary_fail "timed out waiting ${wait_seconds}s for gate without a valid owner PID"
					;;
				*)
					temporary_fail "timed out waiting ${wait_seconds}s for gate held by PID $owner_pid"
					;;
			esac
		fi

		command sleep 1
		waited=$((waited + 1))
	done

	owned_identity=$(path_identity "$lock_file")
	current_record=$(read_record_from "$lock_file")
	if [ "$owned_identity" != "$owner_identity" ] || [ "$current_record" != "$owner_record" ]; then
		owned_identity=''
		temporary_fail 'shared gate ownership changed during atomic acquisition; retry later'
	fi
	lock_owned=true
}

# shellcheck disable=SC2329 # Invoked by the signal traps.
forward_signal() {
	signal_name=$1
	signal_code=$2
	case "${command_pid:-}" in
		''|*[!0-9]*) exit "$signal_code" ;;
		*)
			[ -n "$signal_status" ] || signal_status=$signal_code
			command kill "-$signal_name" "$command_pid" 2>/dev/null || true
			;;
	esac
}

child_is_alive() {
	case "${command_pid:-}" in
		''|*[!0-9]*) return 1 ;;
		*) command kill -0 "$command_pid" 2>/dev/null ;;
	esac
}

wait_for_child_exit() {
	waited_for_child=0
	while child_is_alive && [ "$waited_for_child" -lt "$SIGNAL_GRACE_SECONDS" ]; do
		command sleep 1
		waited_for_child=$((waited_for_child + 1))
	done
	! child_is_alive
}

terminate_signaled_child() {
	if ! wait_for_child_exit; then
		command kill -TERM "$command_pid" 2>/dev/null || true
		if ! wait_for_child_exit; then
			command kill -KILL "$command_pid" 2>/dev/null || true
		fi
	fi
	wait "$command_pid" 2>/dev/null || true
}

[ "$#" -gt 0 ] || usage

min_free_gib=${BOSUN_AGENT_MIN_FREE_GIB:-$DEFAULT_MIN_FREE_GIB}
max_delta_gib=${BOSUN_AGENT_MAX_DISK_DELTA_GIB:-$DEFAULT_MAX_DELTA_GIB}
wait_seconds=${BOSUN_AGENT_GATE_WAIT_SECONDS:-$DEFAULT_WAIT_SECONDS}
require_uint BOSUN_AGENT_MIN_FREE_GIB "$min_free_gib"
require_uint BOSUN_AGENT_MAX_DISK_DELTA_GIB "$max_delta_gib"
require_uint BOSUN_AGENT_GATE_WAIT_SECONDS "$wait_seconds"

repo_root=$(command git rev-parse --show-toplevel 2>/dev/null) || fail 'must run inside a Git worktree'
repo_root=$(canonical_dir "$repo_root") || fail 'cannot resolve repository root'
invocation_dir=$(canonical_dir .) || fail 'cannot resolve current directory'
common_dir=$(command git rev-parse --git-common-dir 2>/dev/null) || fail 'cannot resolve Git common directory'
case "$common_dir" in
	/*) ;;
	*) common_dir="$invocation_dir/$common_dir" ;;
esac
common_dir=$(canonical_dir "$common_dir") || fail 'cannot resolve Git common directory'

default_gocache=$(env -u GOCACHE -u GOMODCACHE -u GOTMPDIR go env GOCACHE) || fail 'cannot resolve default GOCACHE'
default_gomodcache=$(env -u GOCACHE -u GOMODCACHE -u GOTMPDIR go env GOMODCACHE) || fail 'cannot resolve default GOMODCACHE'

if [ -n "${GOCACHE:-}" ] && [ "$GOCACHE" != "$default_gocache" ]; then
	fail "private GOCACHE is forbidden; unset it to share $default_gocache"
fi
if [ -n "${GOMODCACHE:-}" ] && [ "$GOMODCACHE" != "$default_gomodcache" ]; then
	fail "private GOMODCACHE is forbidden; unset it to share $default_gomodcache"
fi
if [ -n "${GOTMPDIR:-}" ]; then
	fail 'GOTMPDIR must be unset so Go can clean its standard temporary build directories'
fi
if [ -n "${GOLANGCI_LINT_CACHE:-}" ]; then
	fail 'GOLANGCI_LINT_CACHE must be unset so lint runs share the default cache'
fi

export GOCACHE="$default_gocache"
export GOMODCACHE="$default_gomodcache"
unset GOTMPDIR
unset GOLANGCI_LINT_CACHE

lock_key=$(printf '%s' "$common_dir" | command cksum | awk '{ print $1 "-" $2 }')
[ -n "$lock_key" ] || fail 'cannot derive shared gate key'
lock_file="/tmp/bosun-agent-go-gate-${lock_key}.lock"
lock_basename=${lock_file##*/}
lock_owned=false
owned_identity=''
owner_file=''
owner_identity=''
command_pid=''
signal_status=''
trap release_lock EXIT
trap 'forward_signal HUP 129' HUP
trap 'forward_signal INT 130' INT
trap 'forward_signal TERM 143' TERM
cleanup_orphan_links
create_owner_file
acquire_lock

before_kib=$(free_kib "$repo_root")
require_uint 'available disk space' "$before_kib"
minimum_kib=$((min_free_gib * KIB_PER_GIB))
if [ "$before_kib" -lt "$minimum_kib" ]; then
	temporary_fail "only $(format_gib "$before_kib") GiB free; ${min_free_gib} GiB is required"
fi

printf 'agent-go-gate: start free=%s GiB min=%s GiB lock=%s\n' \
	"$(format_gib "$before_kib")" "$min_free_gib" "$lock_file" >&2

"$@" <&0 &
command_pid=$!
while :; do
	if [ -n "$signal_status" ]; then
		terminate_signaled_child
		command_status=$signal_status
		break
	fi
	wait "$command_pid"
	command_status=$?
	if [ -n "$signal_status" ]; then
		terminate_signaled_child
		command_status=$signal_status
	fi
	break
done
command_pid=''

after_kib=$(free_kib "$repo_root")
require_uint 'available disk space after command' "$after_kib"
delta_kib=$((before_kib - after_kib))
printf 'agent-go-gate: finish free=%s GiB delta=%s GiB status=%s\n' \
	"$(format_gib "$after_kib")" "$(format_gib "$delta_kib")" "$command_status" >&2

if [ "$after_kib" -lt "$minimum_kib" ]; then
	printf 'agent-go-gate: ERROR: free space fell below the %s GiB floor\n' "$min_free_gib" >&2
	[ "$command_status" -ne 0 ] && exit "$command_status"
	exit 75
fi

maximum_delta_kib=$((max_delta_gib * KIB_PER_GIB))
if [ "$delta_kib" -gt "$maximum_delta_kib" ]; then
	printf 'agent-go-gate: ERROR: command consumed %s GiB; limit is %s GiB\n' \
		"$(format_gib "$delta_kib")" "$max_delta_gib" >&2
	[ "$command_status" -ne 0 ] && exit "$command_status"
	exit 75
fi

exit "$command_status"
