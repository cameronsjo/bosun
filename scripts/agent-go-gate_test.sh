#!/bin/sh

set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd -P)
gate="$script_dir/agent-go-gate.sh"
test_root=$(mktemp -d "${TMPDIR:-/tmp}/bosun-agent-go-gate-test.XXXXXX")
first_pid=''
second_pid=''
gate_pid=''
lock_dirs=''

cleanup() {
	for cleanup_pid in "$first_pid" "$second_pid" "$gate_pid"; do
		case "$cleanup_pid" in
			''|*[!0-9]*) ;;
			*) command kill "$cleanup_pid" 2>/dev/null || true ;;
		esac
	done
	for cleanup_lock in $lock_dirs; do
		command rm -rf -- "$cleanup_lock"
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

lock_dir_for_common() {
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
trap 'touch "$3"; exit 0' TERM
while :; do
	sleep 1
done
EOF

	command chmod +x "$fake_bin/git" "$fake_bin/go" "$fake_bin/df" "$fake_bin/record-env" "$fake_bin/touch-path" "$fake_bin/hold-gate" "$fake_bin/signal-child"
	export PATH="$fake_bin:$original_path"
	export TMPDIR="$case_dir/tmp"
	export FAKE_REPO_ROOT="$fake_repo"
	export FAKE_COMMON_DIR="$fake_common"
	export FAKE_DEFAULT_GOCACHE="$case_dir/shared-go-build"
	export FAKE_DEFAULT_GOMODCACHE="$case_dir/shared-go-mod"
	export FAKE_DF_STATE="$case_dir/df-state"
	export FAKE_DF_FIRST_KIB=31457280
	export FAKE_DF_SECOND_KIB=31457280
	command mkdir -p -- "$TMPDIR"
	case_lock_dir=$(lock_dir_for_common "$fake_common")
	lock_dirs="$lock_dirs $case_lock_dir"
	unset GOCACHE GOMODCACHE GOTMPDIR GOLANGCI_LINT_CACHE
	unset BOSUN_AGENT_MIN_FREE_GIB BOSUN_AGENT_MAX_DISK_DELTA_GIB BOSUN_AGENT_GATE_WAIT_SECONDS
}

run_gate() {
	set +e
	gate_output=$(cd "$fake_repo" && sh "$gate" "$@" 2>&1)
	gate_status=$?
	set -e
}

original_path=$PATH

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
command mkdir -- "$case_lock_dir"
stale_pid=999999
while command kill -0 "$stale_pid" 2>/dev/null; do
	stale_pid=$((stale_pid + 1))
done
printf '%s\n' "$stale_pid" > "$case_lock_dir/pid"
run_gate touch-path "$marker"
[ "$gate_status" -eq 0 ] || fail "stale lock case exited $gate_status: $gate_output"
[ -e "$marker" ] || fail 'command did not run after stale lock reclamation'
assert_not_exists "$case_lock_dir"

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

new_case signal-forwarding
child_pid_file="$case_dir/child-pid"
child_started="$case_dir/child-started"
child_signaled="$case_dir/child-signaled"
(
	cd "$fake_repo"
	exec sh "$gate" signal-child "$child_pid_file" "$child_started" "$child_signaled"
) > "$case_dir/gate.log" 2>&1 &
gate_pid=$!
waited=0
while [ ! -e "$child_started" ] && [ "$waited" -lt 10 ]; do
	command sleep 1
	waited=$((waited + 1))
done
[ -e "$child_started" ] || fail 'signal child never started'
command kill -TERM "$gate_pid"
set +e
wait "$gate_pid"
gate_status=$?
set -e
gate_pid=''
[ "$gate_status" -eq 143 ] || fail "signaled gate exited $gate_status"
[ -e "$child_signaled" ] || fail 'gate did not forward TERM to its child'
child_pid=$(command cat "$child_pid_file")
if command kill -0 "$child_pid" 2>/dev/null; then
	fail 'gate released while its compiler child was still alive'
fi
assert_not_exists "$case_lock_dir"
run_gate touch-path "$case_dir/after-signal"
[ "$gate_status" -eq 0 ] || fail 'gate was not reusable after signal cleanup'

printf 'agent-go-gate tests: PASS\n'
