#!/usr/bin/env bash
# Hermetic tests for rollback-drill.sh. The full drill runs in a real PTY so
# its terminal guard remains exercised; ssh, git, and the canary are stubs.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DRILL="$SCRIPT_DIR/rollback-drill.sh"
ROOT="$(mktemp -d "${TMPDIR:-/tmp}/bosun-rollback-drill-test.XXXXXX")"
trap 'rm -rf -- "$ROOT"' EXIT

passed=0
CASE=""
F=""
OUT=""
RC=0

fail() {
  printf 'FAIL [%s]: %s\n' "$CASE" "$*" >&2
  printf -- '--- output\n%s\n' "$(cat "$OUT" 2>/dev/null)" >&2
  printf -- '--- calls\n%s\n' "$(cat "$F/calls" 2>/dev/null)" >&2
  exit 1
}

ok() {
  passed=$((passed + 1))
  printf 'ok   %s\n' "$CASE"
}

assert_rc() { [[ "$RC" -eq "$1" ]] || fail "exit $RC, expected $1"; }
assert_out() { grep -qF -- "$1" "$OUT" || fail "output lacks: $1"; }
assert_no_out() { ! grep -qF -- "$1" "$OUT" || fail "output contains: $1"; }
assert_call() { grep -qF -- "$1" "$F/calls" || fail "calls lack: $1"; }
assert_no_call() { ! grep -qF -- "$1" "$F/calls" || fail "calls contain: $1"; }

new_case() {
  CASE="$1"
  F="$ROOT/$CASE"
  unset FAKE_DRILL_FAIL FAKE_INCUMBENT_FAIL FAKE_DRILL_OUTPUT FAKE_INCUMBENT_OUTPUT \
    FAKE_DRILL_STDERR FAKE_INCUMBENT_STDERR
  mkdir -p "$F/bin" "$F/repo/scripts"
  printf 'image: incumbent\n' > "$F/compose.yml"
  cp "$F/compose.yml" "$F/original-compose.yml"
  : > "$F/calls"

  cat > "$F/bin/git" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF

  cat > "$F/bin/ssh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
F="$FAKE_ROOT"
cmd="${@: -1}"
printf '%s\n' "$cmd" >> "$F/calls"
case "$cmd" in
  *"docker exec bosun bosun daemon-status --json")
    printf '%s\n' '{"state":"idle"}'
    ;;
  *"[ -e /mnt/user/appdata/bosun-upgrade/state ]")
    exit 1
    ;;
  *"[ -e /mnt/user/appdata/bosun/docker-compose.yml.predrill ]")
    [[ -e "$F/backup" ]]
    ;;
  *"[ -e /mnt/user/appdata/bosun-upgrade/rollback.override.yml ]")
    exit 1
    ;;
  *"docker run --rm --network none --cap-drop ALL --security-opt no-new-privileges "*)
    [[ "$cmd" == "docker run --rm --network none --cap-drop ALL --security-opt no-new-privileges $FAKE_DRILL_REF bosun --version" ]] || {
      printf 'unexpected drill probe: %s\n' "$cmd" >&2
      exit 64
    }
    if [[ "${FAKE_DRILL_FAIL:-0}" == 1 ]]; then
      printf '%s\n' 'docker: unable to start drill image' >&2
      exit 42
    fi
    if [[ -n "${FAKE_DRILL_STDERR:-}" ]]; then
      printf '%s\n' "$FAKE_DRILL_STDERR" >&2
    fi
    printf '%s\n' "${FAKE_DRILL_OUTPUT-bosun version 0.43.1}"
    ;;
  *"docker exec bosun bosun --version")
    if [[ "${FAKE_INCUMBENT_FAIL:-0}" == 1 ]]; then
      printf '%s\n' 'docker: incumbent unavailable' >&2
      exit 43
    fi
    if [[ -n "${FAKE_INCUMBENT_STDERR:-}" ]]; then
      printf '%s\n' "$FAKE_INCUMBENT_STDERR" >&2
    fi
    printf '%s\n' "${FAKE_INCUMBENT_OUTPUT-bosun version 0.43.1}"
    ;;
  *"-f /mnt/user/appdata/bosun/docker-compose.yml.predrill"*)
    mv -f "$F/backup" "$F/compose.yml"
    sha256sum "$F/compose.yml"
    ;;
  *"sha256sum /mnt/user/appdata/bosun/docker-compose.yml")
    sha256sum "$F/compose.yml"
    ;;
  *"cp -p /mnt/user/appdata/bosun/docker-compose.yml /mnt/user/appdata/bosun/docker-compose.yml.predrill")
    cp -p "$F/compose.yml" "$F/backup"
    ;;
  *"sed -i"*)
    printf 'image: %s\n' "$FAKE_DRILL_REF" > "$F/compose.yml"
    ;;
  *"grep -Fq"*)
    grep -Fq -- "$FAKE_DRILL_REF" "$F/compose.yml"
    ;;
  *"grep -n 'image: ghcr'"*)
    printf '1:image: %s\n' "$FAKE_DRILL_REF"
    ;;
  *"docker inspect bosun --format"*)
    printf '%s\n' 'restarts=0 status=running'
    ;;
  *)
    printf 'unexpected ssh command: %s\n' "$cmd" >&2
    exit 64
    ;;
esac
EOF

  cat > "$F/repo/scripts/upgrade-bosun.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s' 'canary prompt [y/N] '
reply=''
IFS= read -r reply || :
printf '%s\n' 'VERDICT: ROLLED-BACK (exit 1)'
exit "${FAKE_CANARY_RC:-1}"
EOF
  chmod +x "$F/bin/git" "$F/bin/ssh" "$F/repo/scripts/upgrade-bosun.sh"
}

run_case() {
  OUT="$F/output"
  RC=0
  TEST_SCRIPT="$DRILL" PATH="$F/bin:$PATH" FAKE_ROOT="$F" FAKE_DRILL_REF="$DRILL_REF" \
    BOSUN_DIR="$F/repo" BOSUN_UPGRADE_HOST=nas BOSUN_DRILL_WATCH_TIMEOUT=1 \
    python3 - <<'PY' > "$OUT" 2>&1 || RC=$?
import errno
import os
import pty
import select
import signal
import sys
import time

pid, fd = pty.fork()
if pid == 0:
    env = os.environ.copy()
    os.execvpe("bash", ["bash", env["TEST_SCRIPT"]], env)

captured = bytearray()
os.write(fd, b"y\n")
deadline = time.monotonic() + 10
status = None
while True:
    ready, _, _ = select.select([fd], [], [], 0.2)
    if ready:
        try:
            chunk = os.read(fd, 4096)
            if chunk:
                captured.extend(chunk)
        except OSError as error:
            if error.errno == errno.EIO:
                pass
            else:
                raise
    waited, status = os.waitpid(pid, os.WNOHANG)
    if waited == pid:
        break
    if time.monotonic() >= deadline:
        os.kill(pid, signal.SIGKILL)
        _, status = os.waitpid(pid, 0)
        captured.extend(b"\nPTY child timed out\n")
        break

# A PTY can report EIO before the child status is observable. Always reap the
# known child before interpreting its exit code, and drain bytes that arrived
# between the final read and waitpid.
if status is None:
    _, status = os.waitpid(pid, 0)
while True:
    ready, _, _ = select.select([fd], [], [], 0)
    if not ready:
        break
    try:
        chunk = os.read(fd, 4096)
        if not chunk:
            break
        captured.extend(chunk)
    except OSError as error:
        if error.errno == errno.EIO:
            break
        raise
sys.stdout.buffer.write(captured)
if os.WIFEXITED(status):
    raise SystemExit(os.WEXITSTATUS(status))
if os.WIFSIGNALED(status):
    raise SystemExit(128 + os.WTERMSIG(status))
raise SystemExit(1)
PY
}

run_without_tty() {
  OUT="$F/output"
  RC=0
  TEST_SCRIPT="$DRILL" PATH="$F/bin:$PATH" FAKE_ROOT="$F" FAKE_DRILL_REF="$DRILL_REF" \
    BOSUN_DIR="$F/repo" BOSUN_UPGRADE_HOST=nas BOSUN_DRILL_WATCH_TIMEOUT=1 \
    bash "$DRILL" > "$OUT" 2>&1 || RC=$?
}

DRILL_REF='ghcr.io/cameronsjo/bosun-drill@sha256:4419ea44951940705f76d1d1e32a17a0725bfcc3b592c6173c43f0be114e02a0'

new_case same-version
export FAKE_DRILL_OUTPUT='bosun version 0.43.1' FAKE_INCUMBENT_OUTPUT='bosun version 0.43.1'
run_case
assert_rc 1; assert_out 'version comparison: SAME'; assert_out 'VERDICT: ROLLED-BACK'; ok

new_case drifted-version
export FAKE_DRILL_OUTPUT='bosun version 0.43.1' FAKE_INCUMBENT_OUTPUT='bosun version 0.45.0'
run_case
assert_rc 1; assert_out 'version comparison: DRIFT'; assert_out 'RENDER-DIFFERS may be version-related'; assert_out 'review the diff'; ok

new_case multiline-version
export FAKE_DRILL_OUTPUT=$'bosun version 0.43.1\nbuild metadata: test' FAKE_INCUMBENT_OUTPUT='bosun version 0.43.1'
run_case
assert_rc 1; assert_out 'version comparison: SAME'; assert_no_out 'build metadata'; ok

new_case drill-unsafe-token
unsafe=$'bosun version 0.43.1\033[31m'
export FAKE_DRILL_OUTPUT="$unsafe" FAKE_INCUMBENT_OUTPUT='bosun version 0.43.1'
run_case
assert_rc 75; assert_out 'version output was unreadable'; assert_no_out "$unsafe"
assert_no_call 'cp -p'; assert_no_call 'sed -i'; ok

new_case cold-image-stderr-preamble
preamble="Unable to find image '$DRILL_REF' locally"
export FAKE_DRILL_STDERR="$preamble" FAKE_INCUMBENT_OUTPUT='bosun version 0.43.1'
run_case
assert_rc 1; assert_out 'version comparison: SAME'; assert_out 'VERDICT: ROLLED-BACK'; assert_no_out "$preamble"
assert_call 'docker run --rm --network none --cap-drop ALL --security-opt no-new-privileges'; assert_no_call 'bosun --version 2>/dev/null'; ok

new_case drill-unsafe-stderr
unsafe=$'\033]0;drill-version\007'
export FAKE_DRILL_STDERR="$unsafe" FAKE_INCUMBENT_OUTPUT='bosun version 0.43.1'
run_case
assert_rc 1; assert_out 'version comparison: SAME'; assert_out 'VERDICT: ROLLED-BACK'; assert_no_out "$unsafe"; ok

new_case incumbent-unreadable
export FAKE_DRILL_OUTPUT='bosun version 0.43.1' FAKE_INCUMBENT_FAIL=1
run_case
assert_rc 1; assert_out 'version comparison: UNKNOWN'; assert_out 'incumbent version: unknown'; assert_out 'review any RENDER-DIFFERS output'; ok

new_case incumbent-unreadable-output
export FAKE_DRILL_OUTPUT='bosun version 0.43.1' FAKE_INCUMBENT_OUTPUT='not a bosun version'
run_case
assert_rc 1; assert_out 'version comparison: UNKNOWN'; assert_out 'unreadable --version output'; ok

new_case incumbent-unsafe-token
unsafe=$'bosun version 0.43.1\033]0;incumbent-version\007'
export FAKE_DRILL_OUTPUT='bosun version 0.43.1' FAKE_INCUMBENT_OUTPUT="$unsafe"
run_case
assert_rc 1; assert_out 'version comparison: UNKNOWN'; assert_out 'unreadable --version output'
assert_no_out "$unsafe"; assert_call 'cp -p'; ok

new_case incumbent-unsafe-stderr
unsafe=$'\033[31mincumbent-version\033[0m'
export FAKE_DRILL_OUTPUT='bosun version 0.43.1' FAKE_INCUMBENT_STDERR="$unsafe"
run_case
assert_rc 1; assert_out 'version comparison: SAME'; assert_out 'VERDICT: ROLLED-BACK'
assert_no_out "$unsafe"; ok

new_case drill-unreadable
export FAKE_DRILL_OUTPUT='not a bosun version' FAKE_INCUMBENT_OUTPUT='bosun version 0.43.1'
run_case
assert_rc 75; assert_out 'version output was unreadable'; assert_no_call 'cp -p'; assert_no_call 'sed -i'; ok

new_case drill-failure
export FAKE_DRILL_FAIL=1 FAKE_INCUMBENT_OUTPUT='bosun version 0.43.1'
run_case
assert_rc 75; assert_out 'probe exit 42'; assert_no_call 'docker exec bosun bosun --version'; assert_no_call 'cp -p'; ok

new_case no-tty-guard
export FAKE_DRILL_OUTPUT='bosun version 0.43.1' FAKE_INCUMBENT_OUTPUT='bosun version 0.43.1'
run_without_tty
assert_rc 64; assert_out 'Run this from a terminal'; [[ ! -s "$F/calls" ]] || fail 'no-TTY guard contacted the NAS'; ok

new_case probe-order-and-restoration
export FAKE_DRILL_OUTPUT='bosun version 0.43.1' FAKE_INCUMBENT_OUTPUT='bosun version 0.43.1'
run_case
assert_rc 1
drill_line="$(grep -n 'docker run --rm --network none' "$F/calls" | head -n1 | cut -d: -f1)"
incumbent_line="$(grep -n 'docker exec bosun bosun --version' "$F/calls" | head -n1 | cut -d: -f1)"
backup_line="$(grep -n 'cp -p /mnt/user/appdata/bosun/docker-compose.yml' "$F/calls" | head -n1 | cut -d: -f1)"
[[ "$drill_line" -lt "$backup_line" && "$incumbent_line" -lt "$backup_line" ]] || fail 'version probes ran after backup'
assert_call '--network none --cap-drop ALL --security-opt no-new-privileges'
assert_call "$DRILL_REF bosun --version"
assert_no_call '--entrypoint'
assert_no_call '--mount'; assert_no_call '--volume'; assert_no_call 'age-key'
cmp -s "$F/compose.yml" "$F/original-compose.yml" || fail 'compose file was not restored'
[[ ! -e "$F/backup" ]] || fail 'backup was not consumed during restoration'
ok

printf '\n%d passed\n' "$passed"
