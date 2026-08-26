#!/bin/sh

set -u

readonly DEFAULT_MIN_FREE_GIB=100
readonly DEFAULT_MAX_DELTA_GIB=4
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

acquire_lock() {
	case $(command uname -s) in
		Darwin)
			command -v lockf >/dev/null 2>&1 || fail 'lockf is required on Darwin'
			command lockf -s -t "$wait_seconds" 9
			lock_status=$?
			;;
		*)
			command -v flock >/dev/null 2>&1 || fail 'flock is required on this platform'
			command flock -E 75 -w "$wait_seconds" 9
			lock_status=$?
			;;
	esac
	case "$lock_status" in
		0) ;;
		75) temporary_fail "timed out waiting ${wait_seconds}s for shared gate" ;;
		*) fail "cannot acquire shared gate (lock backend exited $lock_status)" ;;
	esac
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

if [ "$min_free_gib" -lt "$DEFAULT_MIN_FREE_GIB" ]; then
	fail "BOSUN_AGENT_MIN_FREE_GIB must be at least ${DEFAULT_MIN_FREE_GIB} GiB, got '$min_free_gib'"
fi
if [ "$max_delta_gib" -gt "$DEFAULT_MAX_DELTA_GIB" ]; then
	fail "BOSUN_AGENT_MAX_DISK_DELTA_GIB must be at most ${DEFAULT_MAX_DELTA_GIB} GiB, got '$max_delta_gib'"
fi
repo_root=$(command git rev-parse --show-toplevel 2>/dev/null) || fail 'must run inside a Git worktree'
repo_root=$(canonical_dir "$repo_root") || fail 'cannot resolve repository root'
invocation_dir=$(canonical_dir .) || fail 'cannot resolve current directory'
common_dir=$(command git rev-parse --git-common-dir 2>/dev/null) || fail 'cannot resolve Git common directory'
case "$common_dir" in
	/*) ;;
	*) common_dir="$invocation_dir/$common_dir" ;;
esac
common_dir=$(canonical_dir "$common_dir") || fail 'cannot resolve Git common directory'

default_gocache=$(env -u GOCACHE -u GOMODCACHE -u GOTMPDIR GOENV=off go env GOCACHE) || fail 'cannot resolve default GOCACHE'
default_gomodcache=$(env -u GOCACHE -u GOMODCACHE -u GOTMPDIR GOENV=off go env GOMODCACHE) || fail 'cannot resolve default GOMODCACHE'
default_gotmpdir=$(env -u GOCACHE -u GOMODCACHE -u GOTMPDIR GOENV=off go env GOTMPDIR) || fail 'cannot resolve default GOTMPDIR'
effective_gocache=$(env -u GOCACHE -u GOMODCACHE -u GOTMPDIR go env GOCACHE) || fail 'cannot resolve GOENV-derived GOCACHE'
effective_gomodcache=$(env -u GOCACHE -u GOMODCACHE -u GOTMPDIR go env GOMODCACHE) || fail 'cannot resolve GOENV-derived GOMODCACHE'
effective_gotmpdir=$(env -u GOCACHE -u GOMODCACHE -u GOTMPDIR go env GOTMPDIR) || fail 'cannot resolve GOENV-derived GOTMPDIR'

if [ "$effective_gocache" != "$default_gocache" ]; then
	fail "GOENV config sets private GOCACHE to $effective_gocache; remove it to share $default_gocache"
fi
if [ "$effective_gomodcache" != "$default_gomodcache" ]; then
	fail "GOENV config sets private GOMODCACHE to $effective_gomodcache; remove it to share $default_gomodcache"
fi
if [ "$effective_gotmpdir" != "$default_gotmpdir" ] || [ -n "$effective_gotmpdir" ]; then
	fail "GOENV config sets private GOTMPDIR to $effective_gotmpdir; remove it so Go can clean standard temporary build directories"
fi

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

lock_file="$common_dir/bosun-agent-go-gate.lock"
command_pid=''
signal_status=''
trap 'forward_signal HUP 129' HUP
trap 'forward_signal INT 130' INT
trap 'forward_signal TERM 143' TERM
(umask 077 && : >> "$lock_file") || fail 'cannot create shared gate file'
exec 9>> "$lock_file" || fail 'cannot open shared gate file'
acquire_lock

before_kib=$(free_kib "$repo_root")
require_uint 'available disk space' "$before_kib"
minimum_kib=$((min_free_gib * KIB_PER_GIB))
if [ "$before_kib" -lt "$minimum_kib" ]; then
	temporary_fail "only $(format_gib "$before_kib") GiB free; ${min_free_gib} GiB is required"
fi

printf 'agent-go-gate: start free=%s GiB min=%s GiB max-delta=%s GiB wait=%ss lock=%s\n' \
	"$(format_gib "$before_kib")" "$min_free_gib" "$max_delta_gib" "$wait_seconds" "$lock_file" >&2

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
