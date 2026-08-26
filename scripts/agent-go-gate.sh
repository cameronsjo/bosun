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

lock_identity() {
	LC_ALL=C command ls -di -- "$lock_dir" 2>/dev/null | awk 'NR == 1 { print $1 }'
}

read_lock_pid() {
	lock_pid=''
	if [ -r "$lock_dir/pid" ]; then
		IFS= read -r lock_pid < "$lock_dir/pid" || lock_pid=''
	fi
	printf '%s' "$lock_pid"
}

# shellcheck disable=SC2329 # Invoked by the EXIT trap.
release_lock() {
	if [ "${lock_owned:-false}" = true ]; then
		owner_pid=$(read_lock_pid)
		current_identity=$(lock_identity)
		if [ "$owner_pid" = "$$" ] && [ "$current_identity" = "${owned_identity:-}" ]; then
			command rm -f -- "$lock_dir/pid"
			command rmdir -- "$lock_dir" 2>/dev/null || true
		fi
		lock_owned=false
		owned_identity=''
	fi
}

reclaim_stale_lock() {
	expected_identity=$1
	expected_pid=$2
	current_identity=$(lock_identity)
	current_pid=$(read_lock_pid)
	[ -n "$current_identity" ] || return 0
	[ "$current_identity" = "$expected_identity" ] || return 0
	[ "$current_pid" = "$expected_pid" ] || return 0

	stale_dir="${lock_dir}.stale.$$"
	if command mv -- "$lock_dir" "$stale_dir" 2>/dev/null; then
		moved_identity=$(LC_ALL=C command ls -di -- "$stale_dir" 2>/dev/null | awk 'NR == 1 { print $1 }')
		if [ "$moved_identity" = "$expected_identity" ]; then
			command rm -f -- "$stale_dir/pid"
			command rm -f -- "$stale_dir"/.pid.pending.*
			command rmdir -- "$stale_dir" 2>/dev/null || true
		elif [ ! -e "$lock_dir" ]; then
			# The lock changed between validation and rename. Restore it rather
			# than deleting another gate owner's lock.
			command mv -- "$stale_dir" "$lock_dir" 2>/dev/null || true
		fi
	fi
}

acquire_lock() {
	waited=0
	reported_wait=false
	observed_identity=''
	unowned_waited=0
	while ! command mkdir -- "$lock_dir" 2>/dev/null; do
		current_identity=$(lock_identity)
		owner_pid=$(read_lock_pid)
		if [ "$current_identity" != "$observed_identity" ]; then
			observed_identity=$current_identity
			unowned_waited=0
		fi

		case "$owner_pid" in
			''|*[!0-9]*)
				# mkdir and the PID write are separate operations. Give a new
				# lock identity two seconds to publish its PID before treating
				# that specific lock as stale.
				if [ "$unowned_waited" -ge 2 ]; then
					reclaim_stale_lock "$current_identity" "$owner_pid"
				fi
				unowned_waited=$((unowned_waited + 1))
				;;
			*)
				if ! command kill -0 "$owner_pid" 2>/dev/null; then
					reclaim_stale_lock "$current_identity" "$owner_pid"
				elif [ "$reported_wait" = false ]; then
					printf 'agent-go-gate: waiting for shared gate held by PID %s\n' "$owner_pid" >&2
					reported_wait=true
				fi
				;;
		esac

		if [ "$waited" -ge "$wait_seconds" ] && [ -e "$lock_dir" ]; then
			owner_pid=$(read_lock_pid)
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

	lock_owned=true
	owned_identity=$(lock_identity)
	if [ -z "$owned_identity" ]; then
		lock_owned=false
		command rmdir -- "$lock_dir" 2>/dev/null || true
		fail 'cannot identify shared gate ownership'
	fi
	pending_pid="$lock_dir/.pid.pending.$$"
	if ! (umask 077 && printf '%s\n' "$$" > "$pending_pid"); then
		release_lock
		fail 'cannot publish shared gate ownership'
	fi
	if [ "$(lock_identity)" != "$owned_identity" ]; then
		lock_owned=false
		owned_identity=''
		temporary_fail 'shared gate ownership changed during PID publication; retry later'
	fi
	if ! command mv -- "$pending_pid" "$lock_dir/pid" 2>/dev/null; then
		lock_owned=false
		owned_identity=''
		temporary_fail 'shared gate ownership changed during PID publication; retry later'
	fi
	if [ "$(lock_identity)" != "$owned_identity" ] || [ "$(read_lock_pid)" != "$$" ]; then
		lock_owned=false
		owned_identity=''
		temporary_fail 'shared gate ownership changed during PID publication; retry later'
	fi
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
lock_dir="/tmp/bosun-agent-go-gate-${lock_key}.lock"
lock_owned=false
owned_identity=''
command_pid=''
signal_status=''
trap release_lock EXIT
trap 'forward_signal HUP 129' HUP
trap 'forward_signal INT 130' INT
trap 'forward_signal TERM 143' TERM
acquire_lock

before_kib=$(free_kib "$repo_root")
require_uint 'available disk space' "$before_kib"
minimum_kib=$((min_free_gib * KIB_PER_GIB))
if [ "$before_kib" -lt "$minimum_kib" ]; then
	temporary_fail "only $(format_gib "$before_kib") GiB free; ${min_free_gib} GiB is required"
fi

printf 'agent-go-gate: start free=%s GiB min=%s GiB lock=%s\n' \
	"$(format_gib "$before_kib")" "$min_free_gib" "$lock_dir" >&2

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
