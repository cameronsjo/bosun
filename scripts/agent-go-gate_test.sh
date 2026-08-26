#!/bin/sh

set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd -P)
gate="$script_dir/agent-go-gate.sh"
test_root=$(mktemp -d "${TMPDIR:-/tmp}/bosun-agent-go-gate-test.XXXXXX")
first_pid=''
second_pid=''
gate_pid=''
watchdog_pid=''
lock_files=''

cleanup() {
	if [ -n "${FAKE_ACQUIRE_LN_RELEASE_FILE:-}" ]; then
		command touch "$FAKE_ACQUIRE_LN_RELEASE_FILE" 2>/dev/null || true
	fi
	if [ -n "${FAKE_CANONICAL_UNLINK_RELEASE_FILE:-}" ]; then
		command touch "$FAKE_CANONICAL_UNLINK_RELEASE_FILE" 2>/dev/null || true
	fi
	for cleanup_pid in "$first_pid" "$second_pid" "$gate_pid" "$watchdog_pid"; do
		case "$cleanup_pid" in
			''|*[!0-9]*) ;;
			*) command kill "$cleanup_pid" 2>/dev/null || true ;;
		esac
	done
	for cleanup_pid in "$first_pid" "$second_pid" "$gate_pid" "$watchdog_pid"; do
		case "$cleanup_pid" in
			''|*[!0-9]*) ;;
			*) wait "$cleanup_pid" 2>/dev/null || true ;;
		esac
	done
	for cleanup_lock in $lock_files; do
		command rm -f -- "$cleanup_lock" "$cleanup_lock".owner.* "$cleanup_lock".capture.*
	done
	command rm -rf -- "$test_root"
}

trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

fail() {
	printf 'FAIL: %s\n' "$*" >&2
	exit 1
}

assert_contains() {
	case "$1" in
		*"$2"*) ;;
		*) fail "expected output to contain: $2" ;;
	esac
}

assert_not_exists() {
	[ ! -e "$1" ] || fail "expected path to be absent: $1"
}

lock_file_for_common() {
	resolved_common=$(CDPATH='' cd -- "$1" && pwd -P)
	lock_key=$(printf '%s' "$resolved_common" | command cksum | awk '{ print $1 "-" $2 }')
	printf '/tmp/bosun-agent-go-gate-%s.lock\n' "$lock_key"
}

new_case() {
	case_name=$1
	case_dir="$test_root/$case_name"
	fake_bin="$case_dir/bin"
	fake_repo="$case_dir/repo"
	fake_common="$case_dir/common.git"
	command mkdir -p -- "$fake_bin" "$fake_repo" "$fake_common"

	command cat > "$fake_bin/git" <<'EOF'
#!/bin/sh
case "$1 $2" in
	'rev-parse --show-toplevel') printf '%s\n' "$FAKE_REPO_ROOT" ;;
	'rev-parse --git-common-dir') printf '%s\n' "$FAKE_COMMON_DIR" ;;
	*) exit 2 ;;
esac
EOF

	command cat > "$fake_bin/go" <<'EOF'
#!/bin/sh
case "$1 $2" in
	'env GOCACHE') printf '%s\n' "$FAKE_DEFAULT_GOCACHE" ;;
	'env GOMODCACHE') printf '%s\n' "$FAKE_DEFAULT_GOMODCACHE" ;;
	*) exit 2 ;;
esac
EOF

	command cat > "$fake_bin/df" <<'EOF'
#!/bin/sh
count=0
if [ -r "$FAKE_DF_STATE" ]; then
	IFS= read -r count < "$FAKE_DF_STATE"
fi
count=$((count + 1))
printf '%s\n' "$count" > "$FAKE_DF_STATE"
if [ "$count" -eq 1 ]; then
	free=$FAKE_DF_FIRST_KIB
else
	free=$FAKE_DF_SECOND_KIB
fi
printf 'Filesystem 1024-blocks Used Available Capacity Mounted on\n'
printf '/dev/fake 999999999 1 %s 1%% /fake\n' "$free"
EOF

	command cat > "$fake_bin/ln" <<'EOF'
#!/bin/sh
if [ "${FAKE_LN_FAIL:-false}" = true ]; then
	exit 1
fi
[ "$1" = -P ] || exit 2
source_path=$2
destination_path=$3
case "$source_path" in
	"$FAKE_CANONICAL_LOCK".owner.*)
		if [ "$destination_path" = "$FAKE_CANONICAL_LOCK" ] && [ -n "${FAKE_ACQUIRE_LN_PAUSED_FILE:-}" ]; then
			touch "$FAKE_ACQUIRE_LN_PAUSED_FILE"
			while [ ! -e "$FAKE_ACQUIRE_LN_RELEASE_FILE" ]; do
				sleep 1
			done
		fi
		;;
esac
exec "$FAKE_REAL_LN" -P "$source_path" "$destination_path"
EOF

	command cat > "$fake_bin/rm" <<'EOF'
#!/bin/sh
for remove_path do
	if [ "$remove_path" = "$FAKE_CANONICAL_LOCK" ] && [ -n "${FAKE_CANONICAL_UNLINK_PAUSED_FILE:-}" ]; then
		touch "$FAKE_CANONICAL_UNLINK_PAUSED_FILE"
		while [ ! -e "$FAKE_CANONICAL_UNLINK_RELEASE_FILE" ]; do
			sleep 1
		done
	fi
done
exec "$FAKE_REAL_RM" "$@"
EOF

	command cat > "$fake_bin/record-env" <<'EOF'
#!/bin/sh
printf '%s|%s|%s|%s\n' "$GOCACHE" "$GOMODCACHE" "${GOTMPDIR+x}" "${GOLANGCI_LINT_CACHE+x}" > "$1"
EOF

	command cat > "$fake_bin/touch-path" <<'EOF'
#!/bin/sh
touch "$1"
EOF

	command cat > "$fake_bin/hold-gate" <<'EOF'
#!/bin/sh
touch "$1"
while [ ! -e "$2" ]; do
	sleep 1
done
EOF

	command cat > "$fake_bin/signal-child" <<'EOF'
#!/bin/sh
printf '%s\n' "$$" > "$1"
touch "$2"
trap 'touch "$3"; exit 0' "$4"
while :; do
	sleep 1
done
EOF

	command cat > "$fake_bin/stubborn-child" <<'EOF'
#!/bin/sh
printf '%s\n' "$$" > "$1"
touch "$2"
trap 'touch "$3"' TERM
while :; do
	sleep 1
done
EOF

	command chmod +x "$fake_bin/git" "$fake_bin/go" "$fake_bin/df" "$fake_bin/ln" "$fake_bin/rm" "$fake_bin/record-env" "$fake_bin/touch-path" "$fake_bin/hold-gate" "$fake_bin/signal-child" "$fake_bin/stubborn-child"
	export PATH="$fake_bin:$original_path"
	export TMPDIR="$case_dir/tmp"
	export FAKE_REPO_ROOT="$fake_repo"
	export FAKE_COMMON_DIR="$fake_common"
	export FAKE_DEFAULT_GOCACHE="$case_dir/shared-go-build"
	export FAKE_DEFAULT_GOMODCACHE="$case_dir/shared-go-mod"
	export FAKE_REAL_LN="$real_ln"
	export FAKE_REAL_RM="$real_rm"
	export FAKE_DF_STATE="$case_dir/df-state"
	export FAKE_DF_FIRST_KIB=31457280
	export FAKE_DF_SECOND_KIB=31457280
	command mkdir -p -- "$TMPDIR"
	case_lock_file=$(lock_file_for_common "$fake_common")
	lock_files="$lock_files $case_lock_file"
	export FAKE_CANONICAL_LOCK="$case_lock_file"
	unset GOCACHE GOMODCACHE GOTMPDIR GOLANGCI_LINT_CACHE
	unset BOSUN_AGENT_MIN_FREE_GIB BOSUN_AGENT_MAX_DISK_DELTA_GIB BOSUN_AGENT_GATE_WAIT_SECONDS
	unset FAKE_LN_FAIL FAKE_ACQUIRE_LN_PAUSED_FILE FAKE_ACQUIRE_LN_RELEASE_FILE
	unset FAKE_CANONICAL_UNLINK_PAUSED_FILE FAKE_CANONICAL_UNLINK_RELEASE_FILE
}

run_gate() {
	set +e
	gate_output=$(cd "$fake_repo" && sh "$gate" "$@" 2>&1)
	gate_status=$?
	set -e
}

wait_for_path() {
	wait_path=$1
	wait_description=$2
	waited=0
	while [ ! -e "$wait_path" ] && [ "$waited" -lt 10 ]; do
		command sleep 1
		waited=$((waited + 1))
	done
	[ -e "$wait_path" ] || fail "$wait_description"
}

path_identity() {
	LC_ALL=C command ls -di -- "$1" 2>/dev/null | awk 'NR == 1 { print $1 }'
}

dead_pid() {
	unused_pid=999999
	while command kill -0 "$unused_pid" 2>/dev/null; do
		unused_pid=$((unused_pid + 1))
	done
	printf '%s\n' "$unused_pid"
}

create_owner_link() {
	record_pid=$1
	created_owner=$(mktemp "${case_lock_file}.owner.${record_pid}.XXXXXX")
	created_token=${created_owner##*/}
	printf '%s %s\n' "$record_pid" "$created_token" > "$created_owner"
	"$real_ln" -P "$created_owner" "$case_lock_file"
}

assert_no_auxiliary_links() {
	for auxiliary_path in "$case_lock_file".owner.* "$case_lock_file".capture.*; do
		[ ! -e "$auxiliary_path" ] || fail "expected auxiliary link to be absent: $auxiliary_path"
	done
}

original_path=$PATH
real_ln=$(command -v ln)
real_rm=$(command -v rm)

new_case success
env_file="$case_dir/env"
run_gate record-env "$env_file"
[ "$gate_status" -eq 0 ] || fail "success case exited $gate_status: $gate_output"
expected_env="$FAKE_DEFAULT_GOCACHE|$FAKE_DEFAULT_GOMODCACHE|"
expected_env="$expected_env|"
[ "$(command cat "$env_file")" = "$expected_env" ] || fail 'gate did not enforce shared default Go caches'
assert_contains "$gate_output" 'agent-go-gate: start free=30.0 GiB min=20 GiB'
assert_contains "$gate_output" 'agent-go-gate: finish free=30.0 GiB delta=0.0 GiB status=0'

new_case private-cache
marker="$case_dir/ran"
GOCACHE="$case_dir/private"
export GOCACHE
run_gate touch-path "$marker"
[ "$gate_status" -eq 64 ] || fail "private cache case exited $gate_status"
assert_contains "$gate_output" 'private GOCACHE is forbidden'
assert_not_exists "$marker"
unset GOCACHE

new_case private-module-cache
marker="$case_dir/ran"
GOMODCACHE="$case_dir/private-mod"
export GOMODCACHE
run_gate touch-path "$marker"
[ "$gate_status" -eq 64 ] || fail "private module cache case exited $gate_status"
assert_contains "$gate_output" 'private GOMODCACHE is forbidden'
assert_not_exists "$marker"
unset GOMODCACHE

new_case gotmpdir
marker="$case_dir/ran"
GOTMPDIR="$case_dir/private-tmp"
export GOTMPDIR
run_gate touch-path "$marker"
[ "$gate_status" -eq 64 ] || fail "GOTMPDIR case exited $gate_status"
assert_contains "$gate_output" 'GOTMPDIR must be unset'
assert_not_exists "$marker"
unset GOTMPDIR

new_case private-lint-cache
marker="$case_dir/ran"
GOLANGCI_LINT_CACHE="$case_dir/private-lint"
export GOLANGCI_LINT_CACHE
run_gate touch-path "$marker"
[ "$gate_status" -eq 64 ] || fail "private lint cache case exited $gate_status"
assert_contains "$gate_output" 'GOLANGCI_LINT_CACHE must be unset'
assert_not_exists "$marker"
unset GOLANGCI_LINT_CACHE

new_case low-disk
marker="$case_dir/ran"
FAKE_DF_FIRST_KIB=10485760
FAKE_DF_SECOND_KIB=10485760
export FAKE_DF_FIRST_KIB FAKE_DF_SECOND_KIB
run_gate touch-path "$marker"
[ "$gate_status" -eq 75 ] || fail "low disk case exited $gate_status"
assert_contains "$gate_output" 'only 10.0 GiB free; 20 GiB is required'
assert_not_exists "$marker"

new_case excessive-delta
marker="$case_dir/ran"
FAKE_DF_FIRST_KIB=31457280
FAKE_DF_SECOND_KIB=22020096
export FAKE_DF_FIRST_KIB FAKE_DF_SECOND_KIB
run_gate touch-path "$marker"
[ "$gate_status" -eq 75 ] || fail "excessive delta case exited $gate_status"
[ -e "$marker" ] || fail 'excessive delta command did not run'
assert_contains "$gate_output" 'command consumed 9.0 GiB; limit is 8 GiB'

new_case post-floor
marker="$case_dir/ran"
FAKE_DF_FIRST_KIB=31457280
FAKE_DF_SECOND_KIB=19922944
export FAKE_DF_FIRST_KIB FAKE_DF_SECOND_KIB
run_gate touch-path "$marker"
[ "$gate_status" -eq 75 ] || fail "post-floor case exited $gate_status"
[ -e "$marker" ] || fail 'post-floor command did not run'
assert_contains "$gate_output" 'free space fell below the 20 GiB floor'

new_case command-status
FAKE_DF_FIRST_KIB=31457280
FAKE_DF_SECOND_KIB=22020096
export FAKE_DF_FIRST_KIB FAKE_DF_SECOND_KIB
run_gate sh -c 'exit 7'
[ "$gate_status" -eq 7 ] || fail "command status was not preserved: $gate_status"
assert_contains "$gate_output" 'status=7'
assert_contains "$gate_output" 'command consumed 9.0 GiB; limit is 8 GiB'

new_case stale-lock
marker="$case_dir/ran"
stale_pid=$(dead_pid)
create_owner_link "$stale_pid"
stale_owner=$created_owner
run_gate touch-path "$marker"
[ "$gate_status" -eq 0 ] || fail "stale lock case exited $gate_status: $gate_output"
[ -e "$marker" ] || fail 'command did not run after stale lock reclamation'
assert_not_exists "$case_lock_file"
assert_not_exists "$stale_owner"
assert_no_auxiliary_links

new_case orphan-links
marker="$case_dir/ran"
stale_pid=$(dead_pid)
orphan_owner=$(mktemp "${case_lock_file}.owner.${stale_pid}.XXXXXX")
orphan_token=${orphan_owner##*/}
printf '%s %s\n' "$stale_pid" "$orphan_token" > "$orphan_owner"
orphan_capture="${case_lock_file}.capture.orphan"
"$real_ln" -P "$orphan_owner" "$orphan_capture"
run_gate touch-path "$marker"
[ "$gate_status" -eq 0 ] || fail "orphan link case exited $gate_status: $gate_output"
[ -e "$marker" ] || fail 'command did not run after orphan cleanup'
assert_not_exists "$orphan_owner"
assert_not_exists "$orphan_capture"
assert_no_auxiliary_links

new_case empty-lock
marker="$case_dir/ran"
: > "$case_lock_file"
run_gate touch-path "$marker"
[ "$gate_status" -eq 0 ] || fail "empty lock case exited $gate_status: $gate_output"
[ -e "$marker" ] || fail 'command did not run after empty lock reclamation'
assert_not_exists "$case_lock_file"
assert_no_auxiliary_links

new_case malformed-lock
marker="$case_dir/ran"
printf '%s\n' 'not-an-owner-record' > "$case_lock_file"
run_gate touch-path "$marker"
[ "$gate_status" -eq 0 ] || fail "malformed lock case exited $gate_status: $gate_output"
[ -e "$marker" ] || fail 'command did not run after malformed lock reclamation'
assert_not_exists "$case_lock_file"
assert_no_auxiliary_links

new_case unreclaimable-lock-timeout
marker="$case_dir/must-not-run"
printf '%s\n' 'not-an-owner-record' > "$case_lock_file"
FAKE_LN_FAIL=true
BOSUN_AGENT_GATE_WAIT_SECONDS=3
export FAKE_LN_FAIL BOSUN_AGENT_GATE_WAIT_SECONDS
run_gate touch-path "$marker"
[ "$gate_status" -eq 75 ] || fail "unreclaimable lock case exited $gate_status"
assert_contains "$gate_output" 'timed out waiting 3s for gate without a valid owner PID'
assert_not_exists "$marker"
unset FAKE_LN_FAIL BOSUN_AGENT_GATE_WAIT_SECONDS
"$real_rm" -f -- "$case_lock_file"

new_case atomic-publication
marker="$case_dir/ran"
acquire_paused="$case_dir/acquire-paused"
acquire_release="$case_dir/acquire-release"
FAKE_ACQUIRE_LN_PAUSED_FILE="$acquire_paused"
FAKE_ACQUIRE_LN_RELEASE_FILE="$acquire_release"
export FAKE_ACQUIRE_LN_PAUSED_FILE FAKE_ACQUIRE_LN_RELEASE_FILE
(
	cd "$fake_repo"
	sh "$gate" touch-path "$marker"
) > "$case_dir/gate.log" 2>&1 &
gate_pid=$!
wait_for_path "$acquire_paused" 'atomic acquisition did not reach the publication seam'
assert_not_exists "$case_lock_file"
assert_not_exists "$marker"
owner_count=0
for published_owner in "$case_lock_file".owner.*; do
	[ -e "$published_owner" ] || continue
	owner_count=$((owner_count + 1))
	published_record=$(command cat "$published_owner")
	case "$published_record" in
		[0-9]*' '"${published_owner##*/}") ;;
		*) fail 'private owner file was not fully written before atomic publication' ;;
	esac
done
[ "$owner_count" -eq 1 ] || fail "expected one private owner file before publication, found $owner_count"
command touch "$acquire_release"
wait "$gate_pid" || fail 'atomically published gate failed'
gate_pid=''
[ -e "$marker" ] || fail 'atomically published command did not run'
assert_not_exists "$case_lock_file"
assert_no_auxiliary_links
unset FAKE_ACQUIRE_LN_PAUSED_FILE FAKE_ACQUIRE_LN_RELEASE_FILE

new_case validated-release-serialization
owner_marker="$case_dir/owner-ran"
third_marker="$case_dir/third-ran"
unlink_paused="$case_dir/unlink-paused"
unlink_resume="$case_dir/unlink-resume"
FAKE_CANONICAL_UNLINK_PAUSED_FILE="$unlink_paused"
FAKE_CANONICAL_UNLINK_RELEASE_FILE="$unlink_resume"
export FAKE_CANONICAL_UNLINK_PAUSED_FILE FAKE_CANONICAL_UNLINK_RELEASE_FILE
(
	cd "$fake_repo"
	sh "$gate" touch-path "$owner_marker"
) > "$case_dir/owner.log" 2>&1 &
gate_pid=$!
wait_for_path "$unlink_paused" 'release did not reach the validated canonical unlink seam'
[ -e "$owner_marker" ] || fail 'owner command did not finish before release'
canonical_identity=$(path_identity "$case_lock_file")
[ -n "$canonical_identity" ] || fail 'canonical lock vanished before validated unlink'
capture_found=false
for release_capture in "$case_lock_file".capture.release.*; do
	[ -e "$release_capture" ] || continue
	capture_found=true
	[ "$(path_identity "$release_capture")" = "$canonical_identity" ] || fail 'release capture did not preserve canonical identity'
done
[ "$capture_found" = true ] || fail 'release did not create an identity-preserving capture link'
(
	cd "$fake_repo"
	BOSUN_AGENT_GATE_WAIT_SECONDS=10 sh "$gate" touch-path "$third_marker"
) > "$case_dir/third.log" 2>&1 &
second_pid=$!
command sleep 1
assert_not_exists "$third_marker"
[ "$(path_identity "$case_lock_file")" = "$canonical_identity" ] || fail 'canonical lock changed while validated release was paused'
command touch "$unlink_resume"
wait "$gate_pid" || fail 'original owner failed during validated release'
gate_pid=''
wait "$second_pid" || fail 'third gate failed after canonical unlink'
second_pid=''
[ -e "$third_marker" ] || fail 'third gate never ran after validated canonical unlink'
assert_contains "$(command cat "$case_dir/third.log")" 'waiting for shared gate held by PID'
assert_not_exists "$case_lock_file"
assert_no_auxiliary_links
unset FAKE_CANONICAL_UNLINK_PAUSED_FILE FAKE_CANONICAL_UNLINK_RELEASE_FILE

new_case validated-stale-reclaim-serialization
stale_pid=$(dead_pid)
create_owner_link "$stale_pid"
stale_owner=$created_owner
stale_identity=$(path_identity "$case_lock_file")
first_marker="$case_dir/first-ran"
second_marker="$case_dir/second-ran"
unlink_paused="$case_dir/unlink-paused"
unlink_resume="$case_dir/unlink-resume"
FAKE_CANONICAL_UNLINK_PAUSED_FILE="$unlink_paused"
FAKE_CANONICAL_UNLINK_RELEASE_FILE="$unlink_resume"
export FAKE_CANONICAL_UNLINK_PAUSED_FILE FAKE_CANONICAL_UNLINK_RELEASE_FILE
(
	cd "$fake_repo"
	BOSUN_AGENT_GATE_WAIT_SECONDS=10 sh "$gate" touch-path "$first_marker"
) > "$case_dir/first.log" 2>&1 &
first_pid=$!
wait_for_path "$unlink_paused" 'stale reclaim did not reach the validated canonical unlink seam'
[ "$(path_identity "$case_lock_file")" = "$stale_identity" ] || fail 'stale canonical lock changed before validated reclaim unlink'
reclaim_capture_found=false
for reclaim_capture in "$case_lock_file".capture.reclaim.*; do
	[ -e "$reclaim_capture" ] || continue
	reclaim_capture_found=true
	[ "$(path_identity "$reclaim_capture")" = "$stale_identity" ] || fail 'stale reclaim capture did not preserve canonical identity'
done
[ "$reclaim_capture_found" = true ] || fail 'stale reclaim did not create an identity-preserving capture link'
(
	cd "$fake_repo"
	BOSUN_AGENT_GATE_WAIT_SECONDS=10 sh "$gate" touch-path "$second_marker"
) > "$case_dir/second.log" 2>&1 &
second_pid=$!
command sleep 1
assert_not_exists "$first_marker"
assert_not_exists "$second_marker"
[ "$(path_identity "$case_lock_file")" = "$stale_identity" ] || fail 'stale canonical lock vanished while validated reclaim was paused'
command touch "$unlink_resume"
wait "$first_pid" || fail 'first gate failed after validated stale reclaim'
first_pid=''
wait "$second_pid" || fail 'second gate failed after validated stale reclaim'
second_pid=''
[ -e "$first_marker" ] || fail 'first gate never ran after stale canonical unlink'
[ -e "$second_marker" ] || fail 'second gate never ran after stale canonical unlink'
assert_not_exists "$case_lock_file"
assert_not_exists "$stale_owner"
assert_no_auxiliary_links
unset FAKE_CANONICAL_UNLINK_PAUSED_FILE FAKE_CANONICAL_UNLINK_RELEASE_FILE

new_case serialization
first_acquired="$case_dir/first-acquired"
release_first="$case_dir/release-first"
second_acquired="$case_dir/second-acquired"
fake_repo_two="$case_dir/repo-two"
command mkdir -p -- "$fake_repo_two"
(
	cd "$fake_repo"
	sh "$gate" hold-gate "$first_acquired" "$release_first"
) > "$case_dir/first.log" 2>&1 &
first_pid=$!
waited=0
while [ ! -e "$first_acquired" ] && [ "$waited" -lt 10 ]; do
	command sleep 1
	waited=$((waited + 1))
done
[ -e "$first_acquired" ] || fail 'first serialized command never acquired the gate'
(
	cd "$fake_repo_two"
	FAKE_REPO_ROOT="$fake_repo_two" sh "$gate" touch-path "$second_acquired"
) > "$case_dir/second.log" 2>&1 &
second_pid=$!
command sleep 2
assert_not_exists "$second_acquired"
command touch "$release_first"
wait "$first_pid" || fail 'first serialized command failed'
wait "$second_pid" || fail 'second serialized command failed'
first_pid=''
second_pid=''
[ -e "$second_acquired" ] || fail 'second serialized command never ran after lock release'
assert_contains "$(command cat "$case_dir/second.log")" 'waiting for shared gate held by PID'

new_case wait-timeout
first_acquired="$case_dir/first-acquired"
release_first="$case_dir/release-first"
(
	cd "$fake_repo"
	sh "$gate" hold-gate "$first_acquired" "$release_first"
) > "$case_dir/first.log" 2>&1 &
first_pid=$!
waited=0
while [ ! -e "$first_acquired" ] && [ "$waited" -lt 10 ]; do
	command sleep 1
	waited=$((waited + 1))
done
[ -e "$first_acquired" ] || fail 'timeout owner never acquired the gate'
BOSUN_AGENT_GATE_WAIT_SECONDS=0
export BOSUN_AGENT_GATE_WAIT_SECONDS
run_gate touch-path "$case_dir/must-not-run"
[ "$gate_status" -eq 75 ] || fail "wait timeout case exited $gate_status"
assert_contains "$gate_output" 'timed out waiting 0s for gate held by PID'
assert_not_exists "$case_dir/must-not-run"
command touch "$release_first"
wait "$first_pid" || fail 'timeout owner failed'
first_pid=''
unset BOSUN_AGENT_GATE_WAIT_SECONDS

for wrapper_signal in HUP INT TERM; do
	case "$wrapper_signal" in
		HUP) want_status=129; child_signal=HUP ;;
		INT) want_status=130; child_signal=TERM ;;
		TERM) want_status=143; child_signal=TERM ;;
	esac
	new_case "signal-$wrapper_signal"
	child_pid_file="$case_dir/child-pid"
	child_started="$case_dir/child-started"
	child_signaled="$case_dir/child-signaled"
	timeout_fired="$case_dir/timeout-fired"
	(
		watch_waited=0
		while [ ! -e "$child_started" ] && [ "$watch_waited" -lt 10 ]; do
			command sleep 1
			watch_waited=$((watch_waited + 1))
		done
		[ -e "$child_started" ] || exit 1
		wrapper_pid=$(awk '{ print $1 }' "$case_lock_file")
		command kill "-$wrapper_signal" "$wrapper_pid"
		command sleep 6
		if command kill -0 "$wrapper_pid" 2>/dev/null; then
			command touch "$timeout_fired"
			command kill -TERM "$wrapper_pid" 2>/dev/null || true
		fi
	) &
	watchdog_pid=$!
	run_gate signal-child "$child_pid_file" "$child_started" "$child_signaled" "$child_signal"
	command kill "$watchdog_pid" 2>/dev/null || true
	wait "$watchdog_pid" 2>/dev/null || true
	watchdog_pid=''
	assert_not_exists "$timeout_fired"
	[ "$gate_status" -eq "$want_status" ] || fail "$wrapper_signal gate exited $gate_status"
	[ -e "$child_signaled" ] || fail "gate did not deliver $child_signal while handling $wrapper_signal"
	child_pid=$(command cat "$child_pid_file")
	if command kill -0 "$child_pid" 2>/dev/null; then
		fail "$wrapper_signal gate released while its compiler child was still alive"
	fi
	assert_not_exists "$case_lock_file"
	assert_no_auxiliary_links
	run_gate touch-path "$case_dir/after-signal"
	[ "$gate_status" -eq 0 ] || fail "gate was not reusable after $wrapper_signal cleanup"
done

new_case signal-kill-escalation
child_pid_file="$case_dir/child-pid"
child_started="$case_dir/child-started"
term_seen="$case_dir/term-seen"
timeout_fired="$case_dir/timeout-fired"
(
	watch_waited=0
	while [ ! -e "$child_started" ] && [ "$watch_waited" -lt 10 ]; do
		command sleep 1
		watch_waited=$((watch_waited + 1))
	done
	[ -e "$child_started" ] || exit 1
	wrapper_pid=$(awk '{ print $1 }' "$case_lock_file")
	command kill -INT "$wrapper_pid"
	command sleep 6
	if command kill -0 "$wrapper_pid" 2>/dev/null; then
		command touch "$timeout_fired"
		command kill -TERM "$wrapper_pid" 2>/dev/null || true
	fi
) &
watchdog_pid=$!
run_gate stubborn-child "$child_pid_file" "$child_started" "$term_seen"
command kill "$watchdog_pid" 2>/dev/null || true
wait "$watchdog_pid" 2>/dev/null || true
watchdog_pid=''
assert_not_exists "$timeout_fired"
[ "$gate_status" -eq 130 ] || fail "kill-escalated gate exited $gate_status"
[ -e "$term_seen" ] || fail 'gate did not escalate ignored INT to TERM'
child_pid=$(command cat "$child_pid_file")
if command kill -0 "$child_pid" 2>/dev/null; then
	fail 'gate released before KILL terminated its stubborn child'
fi
assert_not_exists "$case_lock_file"
assert_no_auxiliary_links
run_gate touch-path "$case_dir/after-kill-escalation"
[ "$gate_status" -eq 0 ] || fail 'gate was not reusable after KILL escalation'

printf 'agent-go-gate tests: PASS\n'
