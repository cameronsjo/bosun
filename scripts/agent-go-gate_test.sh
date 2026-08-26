#!/bin/sh

set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd -P)
gate="$script_dir/agent-go-gate.sh"
test_root=$(mktemp -d "${TMPDIR:-/tmp}/bosun-agent-go-gate-test.XXXXXX")
first_pid=''
second_pid=''
gate_pid=''
watchdog_pid=''
orphan_child_pid=''
lock_files=''

cleanup() {
	for cleanup_pid in "$first_pid" "$second_pid" "$gate_pid" "$watchdog_pid" "$orphan_child_pid"; do
		case "$cleanup_pid" in
			''|*[!0-9]*) ;;
			*) command kill "$cleanup_pid" 2>/dev/null || true ;;
		esac
	done
	for cleanup_pid in "$first_pid" "$second_pid" "$gate_pid" "$watchdog_pid" "$orphan_child_pid"; do
		case "$cleanup_pid" in
			''|*[!0-9]*) ;;
			*) wait "$cleanup_pid" 2>/dev/null || true ;;
		esac
	done
	for cleanup_lock in $lock_files; do
		command rm -f -- "$cleanup_lock"
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
	printf '%s/bosun-agent-go-gate.lock\n' "$resolved_common"
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
if [ "${FAKE_USE_REAL_GO:-false}" = true ]; then
	exec "$FAKE_REAL_GO" "$@"
fi
case "$1 $2" in
	'env GOCACHE') printf '%s\n' "$FAKE_DEFAULT_GOCACHE" ;;
	'env GOMODCACHE') printf '%s\n' "$FAKE_DEFAULT_GOMODCACHE" ;;
	'env GOTMPDIR') printf '\n' ;;
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

	command cat > "$fake_bin/record-env" <<'EOF'
#!/bin/sh
printf '%s|%s|%s|%s\n' "$GOCACHE" "$GOMODCACHE" "${GOTMPDIR+x}" "${GOLANGCI_LINT_CACHE+x}" > "$1"
EOF

	command cat > "$fake_bin/record-goenv" <<'EOF'
#!/bin/sh
printf '%s|%s\n' "$GOENV" "$("$FAKE_REAL_GO" env GOPRIVATE)" > "$1"
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

	command cat > "$fake_bin/inherited-lock-child" <<'EOF'
#!/bin/sh
printf '%s\n' "$$" > "$1"
touch "$2"
while [ ! -e "$3" ]; do
	sleep 1
done
EOF

	command cat > "$fake_bin/signal-child" <<'EOF'
#!/bin/sh
printf '%s\n' "$$" > "$1"
printf '%s\n' "$PPID" > "$5"
touch "$2"
trap 'touch "$3"; exit 0' "$4"
while :; do
	sleep 1
done
EOF

	command cat > "$fake_bin/stubborn-child" <<'EOF'
#!/bin/sh
printf '%s\n' "$$" > "$1"
printf '%s\n' "$PPID" > "$4"
touch "$2"
trap 'touch "$3"' TERM
while :; do
	sleep 1
done
EOF

	command chmod +x "$fake_bin/git" "$fake_bin/go" "$fake_bin/df" "$fake_bin/record-env" "$fake_bin/record-goenv" "$fake_bin/touch-path" "$fake_bin/hold-gate" "$fake_bin/inherited-lock-child" "$fake_bin/signal-child" "$fake_bin/stubborn-child"
	export PATH="$fake_bin:$original_path"
	export TMPDIR="$case_dir/tmp"
	export FAKE_REPO_ROOT="$fake_repo"
	export FAKE_COMMON_DIR="$fake_common"
	export FAKE_DEFAULT_GOCACHE="$case_dir/shared-go-build"
	export FAKE_DEFAULT_GOMODCACHE="$case_dir/shared-go-mod"
	export FAKE_REAL_GO="$real_go"
	export FAKE_DF_STATE="$case_dir/df-state"
	export FAKE_DF_FIRST_KIB=125829120
	export FAKE_DF_SECOND_KIB=125829120
	command mkdir -p -- "$TMPDIR"
	case_lock_file=$(lock_file_for_common "$fake_common")
	lock_files="$lock_files $case_lock_file"
	unset GOCACHE GOMODCACHE GOTMPDIR GOLANGCI_LINT_CACHE
	unset BOSUN_AGENT_MIN_FREE_GIB BOSUN_AGENT_MAX_DISK_DELTA_GIB BOSUN_AGENT_GATE_WAIT_SECONDS
	unset FAKE_USE_REAL_GO
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

original_path=$PATH
real_go=$(command -v go)

new_case success
env_file="$case_dir/env"
run_gate record-env "$env_file"
[ "$gate_status" -eq 0 ] || fail "success case exited $gate_status: $gate_output"
expected_env="$FAKE_DEFAULT_GOCACHE|$FAKE_DEFAULT_GOMODCACHE|"
expected_env="$expected_env|"
[ "$(command cat "$env_file")" = "$expected_env" ] || fail 'gate did not enforce shared default Go caches'
assert_contains "$gate_output" 'agent-go-gate: start free=120.0 GiB min=100 GiB max-delta=4 GiB wait=60s'
assert_contains "$gate_output" 'agent-go-gate: finish free=120.0 GiB delta=0.0 GiB status=0'

new_case safe-resource-overrides
marker="$case_dir/ran"
BOSUN_AGENT_MIN_FREE_GIB=110
BOSUN_AGENT_MAX_DISK_DELTA_GIB=2
BOSUN_AGENT_GATE_WAIT_SECONDS=300
export BOSUN_AGENT_MIN_FREE_GIB BOSUN_AGENT_MAX_DISK_DELTA_GIB BOSUN_AGENT_GATE_WAIT_SECONDS
run_gate touch-path "$marker"
[ "$gate_status" -eq 0 ] || fail "safe resource overrides case exited $gate_status: $gate_output"
[ -e "$marker" ] || fail 'safe resource overrides command did not run'
assert_contains "$gate_output" 'min=110 GiB max-delta=2 GiB wait=300s'
unset BOSUN_AGENT_MIN_FREE_GIB BOSUN_AGENT_MAX_DISK_DELTA_GIB BOSUN_AGENT_GATE_WAIT_SECONDS

new_case low-floor-override
marker="$case_dir/must-not-run"
BOSUN_AGENT_MIN_FREE_GIB=99
export BOSUN_AGENT_MIN_FREE_GIB
run_gate touch-path "$marker"
[ "$gate_status" -eq 64 ] || fail "low floor override case exited $gate_status"
assert_contains "$gate_output" "BOSUN_AGENT_MIN_FREE_GIB must be at least 100 GiB, got '99'"
assert_not_exists "$marker"
unset BOSUN_AGENT_MIN_FREE_GIB

new_case high-delta-override
marker="$case_dir/must-not-run"
BOSUN_AGENT_MAX_DISK_DELTA_GIB=5
export BOSUN_AGENT_MAX_DISK_DELTA_GIB
run_gate touch-path "$marker"
[ "$gate_status" -eq 64 ] || fail "high delta override case exited $gate_status"
assert_contains "$gate_output" "BOSUN_AGENT_MAX_DISK_DELTA_GIB must be at most 4 GiB, got '5'"
assert_not_exists "$marker"
unset BOSUN_AGENT_MAX_DISK_DELTA_GIB

new_case invalid-floor-override
marker="$case_dir/must-not-run"
BOSUN_AGENT_MIN_FREE_GIB=invalid
export BOSUN_AGENT_MIN_FREE_GIB
run_gate touch-path "$marker"
[ "$gate_status" -eq 64 ] || fail "invalid floor override case exited $gate_status"
assert_contains "$gate_output" "BOSUN_AGENT_MIN_FREE_GIB must be a non-negative integer, got 'invalid'"
assert_not_exists "$marker"
unset BOSUN_AGENT_MIN_FREE_GIB

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

for goenv_key in GOCACHE GOMODCACHE GOTMPDIR; do
	new_case "goenv-$goenv_key"
	marker="$case_dir/must-not-run"
	goenv_file="$case_dir/go.env"
	private_value="$case_dir/private-${goenv_key}"
	printf '%s=%s\n' "$goenv_key" "$private_value" > "$goenv_file"
	GOENV="$goenv_file"
	FAKE_USE_REAL_GO=true
	export GOENV FAKE_USE_REAL_GO
	run_gate touch-path "$marker"
	[ "$gate_status" -eq 64 ] || fail "GOENV $goenv_key case exited $gate_status: $gate_output"
	assert_contains "$gate_output" "GOENV config sets private $goenv_key to $private_value"
	assert_not_exists "$marker"
	unset GOENV FAKE_USE_REAL_GO
done

new_case goenv-unrelated-setting
goenv_file="$case_dir/go.env"
goenv_result="$case_dir/goenv-result"
printf '%s\n' 'GOPRIVATE=example.test' > "$goenv_file"
GOENV="$goenv_file"
FAKE_USE_REAL_GO=true
export GOENV FAKE_USE_REAL_GO
run_gate record-goenv "$goenv_result"
[ "$gate_status" -eq 0 ] || fail "unrelated GOENV setting case exited $gate_status: $gate_output"
[ "$(command cat "$goenv_result")" = "$goenv_file|example.test" ] || fail 'gate did not preserve unrelated GOENV settings for the child'
unset GOENV FAKE_USE_REAL_GO

new_case low-disk
marker="$case_dir/ran"
FAKE_DF_FIRST_KIB=103809024
FAKE_DF_SECOND_KIB=103809024
export FAKE_DF_FIRST_KIB FAKE_DF_SECOND_KIB
run_gate touch-path "$marker"
[ "$gate_status" -eq 75 ] || fail "low disk case exited $gate_status"
assert_contains "$gate_output" 'only 99.0 GiB free; 100 GiB is required'
assert_not_exists "$marker"

new_case excessive-delta
marker="$case_dir/ran"
FAKE_DF_FIRST_KIB=125829120
FAKE_DF_SECOND_KIB=120586240
export FAKE_DF_FIRST_KIB FAKE_DF_SECOND_KIB
run_gate touch-path "$marker"
[ "$gate_status" -eq 75 ] || fail "excessive delta case exited $gate_status"
[ -e "$marker" ] || fail 'excessive delta command did not run'
assert_contains "$gate_output" 'command consumed 5.0 GiB; limit is 4 GiB'

new_case post-floor
marker="$case_dir/ran"
FAKE_DF_FIRST_KIB=125829120
FAKE_DF_SECOND_KIB=103809024
export FAKE_DF_FIRST_KIB FAKE_DF_SECOND_KIB
run_gate touch-path "$marker"
[ "$gate_status" -eq 75 ] || fail "post-floor case exited $gate_status"
[ -e "$marker" ] || fail 'post-floor command did not run'
assert_contains "$gate_output" 'free space fell below the 100 GiB floor'

new_case command-status
FAKE_DF_FIRST_KIB=125829120
FAKE_DF_SECOND_KIB=120586240
export FAKE_DF_FIRST_KIB FAKE_DF_SECOND_KIB
run_gate sh -c 'exit 7'
[ "$gate_status" -eq 7 ] || fail "command status was not preserved: $gate_status"
assert_contains "$gate_output" 'status=7'
assert_contains "$gate_output" 'command consumed 5.0 GiB; limit is 4 GiB'

new_case serialization
first_acquired="$case_dir/first-acquired"
release_first="$case_dir/release-first"
second_acquired="$case_dir/second-acquired"
fake_repo_two="$case_dir/repo-two"
command mkdir -p -- "$fake_repo_two"
(
	cd "$fake_repo"
	exec sh "$gate" hold-gate "$first_acquired" "$release_first"
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
[ -f "$case_lock_file" ] || fail 'kernel lock file did not persist after release'

new_case wait-timeout
first_acquired="$case_dir/first-acquired"
release_first="$case_dir/release-first"
(
	cd "$fake_repo"
	exec sh "$gate" hold-gate "$first_acquired" "$release_first"
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
assert_contains "$gate_output" 'timed out waiting 0s for shared gate'
assert_not_exists "$case_dir/must-not-run"
BOSUN_AGENT_GATE_WAIT_SECONDS=1
export BOSUN_AGENT_GATE_WAIT_SECONDS
run_gate touch-path "$case_dir/still-must-not-run"
[ "$gate_status" -eq 75 ] || fail "short wait timeout case exited $gate_status"
assert_contains "$gate_output" 'timed out waiting 1s for shared gate'
assert_not_exists "$case_dir/still-must-not-run"
command touch "$release_first"
wait "$first_pid" || fail 'timeout owner failed'
first_pid=''
unset BOSUN_AGENT_GATE_WAIT_SECONDS

new_case wrapper-crash
child_pid_file="$case_dir/child-pid"
child_started="$case_dir/child-started"
release_child="$case_dir/release-child"
after_child_death="$case_dir/after-child-death"
(
	cd "$fake_repo"
	exec sh "$gate" inherited-lock-child "$child_pid_file" "$child_started" "$release_child"
) > "$case_dir/wrapper.log" 2>&1 &
gate_pid=$!
wait_for_path "$child_started" 'inherited-lock child never started'
orphan_child_pid=$(command cat "$child_pid_file")
command kill -KILL "$gate_pid"
set +e
wait "$gate_pid" 2>/dev/null
crash_status=$?
set -e
gate_pid=''
[ "$crash_status" -ne 0 ] || fail 'wrapper crash did not terminate the wrapper'
BOSUN_AGENT_GATE_WAIT_SECONDS=0
export BOSUN_AGENT_GATE_WAIT_SECONDS
run_gate touch-path "$case_dir/must-not-run"
[ "$gate_status" -eq 75 ] || fail "child-inherited lock was released with crashed wrapper: $gate_status"
assert_not_exists "$case_dir/must-not-run"
command touch "$release_child"
BOSUN_AGENT_GATE_WAIT_SECONDS=10
export BOSUN_AGENT_GATE_WAIT_SECONDS
run_gate touch-path "$after_child_death"
[ "$gate_status" -eq 0 ] || fail "lock was not released after inherited-lock child death: $gate_status: $gate_output"
[ -e "$after_child_death" ] || fail 'command did not run after inherited-lock child death'
orphan_child_pid=''
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
	wrapper_pid_file="$case_dir/wrapper-pid"
	timeout_fired="$case_dir/timeout-fired"
	(
		wait_for_path "$wrapper_pid_file" "$wrapper_signal wrapper PID was not recorded"
		wrapper_pid=$(command cat "$wrapper_pid_file")
		command kill "-$wrapper_signal" "$wrapper_pid"
		command sleep 6
		if command kill -0 "$wrapper_pid" 2>/dev/null; then
			command touch "$timeout_fired"
			command kill -TERM "$wrapper_pid" 2>/dev/null || true
		fi
	) &
	watchdog_pid=$!
	run_gate signal-child "$child_pid_file" "$child_started" "$child_signaled" "$child_signal" "$wrapper_pid_file"
	command kill "$watchdog_pid" 2>/dev/null || true
	wait "$watchdog_pid" 2>/dev/null || true
	watchdog_pid=''
	assert_not_exists "$timeout_fired"
	[ "$gate_status" -eq "$want_status" ] || fail "$wrapper_signal gate exited $gate_status"
	[ -e "$child_signaled" ] || fail "gate did not deliver $child_signal while handling $wrapper_signal: $gate_output"
	child_pid=$(command cat "$child_pid_file")
	if command kill -0 "$child_pid" 2>/dev/null; then
		fail "$wrapper_signal gate released while its compiler child was still alive"
	fi
	[ -f "$case_lock_file" ] || fail "$wrapper_signal removed the persistent lock file"
	run_gate touch-path "$case_dir/after-signal"
	[ "$gate_status" -eq 0 ] || fail "gate was not reusable after $wrapper_signal cleanup"
done

new_case signal-kill-escalation
child_pid_file="$case_dir/child-pid"
child_started="$case_dir/child-started"
term_seen="$case_dir/term-seen"
wrapper_pid_file="$case_dir/wrapper-pid"
timeout_fired="$case_dir/timeout-fired"
(
	wait_for_path "$wrapper_pid_file" 'stubborn wrapper PID was not recorded'
	wrapper_pid=$(command cat "$wrapper_pid_file")
	command kill -INT "$wrapper_pid"
	command sleep 6
	if command kill -0 "$wrapper_pid" 2>/dev/null; then
		command touch "$timeout_fired"
		command kill -TERM "$wrapper_pid" 2>/dev/null || true
	fi
) &
watchdog_pid=$!
run_gate stubborn-child "$child_pid_file" "$child_started" "$term_seen" "$wrapper_pid_file"
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
[ -f "$case_lock_file" ] || fail 'KILL escalation removed the persistent lock file'
run_gate touch-path "$case_dir/after-kill-escalation"
[ "$gate_status" -eq 0 ] || fail 'gate was not reusable after KILL escalation'

printf 'agent-go-gate tests: PASS\n'
