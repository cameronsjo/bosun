package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComposeUpRemote_DoesNotRetryApplicationStderr(t *testing.T) {
	for _, stderr := range []string{
		"service startup failed: connection refused",
		"database probe failed: i/o timeout",
	} {
		t.Run(stderr, func(t *testing.T) {
			t.Setenv("BOSUN_TEST_APP_STDERR", stderr)
			capture := installComposeRetrySSHShim(t, `
case "$*" in
  *" ps "*)
    echo ps >> "$BOSUN_TEST_SSH_CAPTURE"
    echo '[]'
    exit 0
    ;;
  *" up "*)
    echo up >> "$BOSUN_TEST_SSH_CAPTURE"
    echo "$BOSUN_TEST_APP_STDERR" >&2
    exit 1
    ;;
esac
exit 1`)

			err := (&DeployOps{}).ComposeUpRemote(context.Background(), "user@testhost", "/srv/compose")
			require.Error(t, err)
			assert.Contains(t, err.Error(), stderr)
			assert.Equal(t, 1, countCapturedSSHCalls(t, capture, "up"),
				"remote command stderr must not make an application failure retryable")
		})
	}
}

func TestComposeUpRemote_RetriesSSHTransportFailure(t *testing.T) {
	capture := installComposeRetrySSHShim(t, `
case "$*" in
  *" up "*)
    echo up >> "$BOSUN_TEST_SSH_CAPTURE"
    if [ ! -e "$BOSUN_TEST_SSH_MARKER" ]; then
      : > "$BOSUN_TEST_SSH_MARKER"
      echo "ssh: connect to host: Connection refused" >&2
      exit 255
    fi
    exit 0
    ;;
esac
exit 1`)

	err := (&DeployOps{}).ComposeUpRemote(context.Background(), "user@testhost", "/srv/compose")
	require.NoError(t, err)
	assert.Equal(t, 2, countCapturedSSHCalls(t, capture, "up"),
		"a genuine SSH transport failure must still retry")
}

func installComposeRetrySSHShim(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	capture := filepath.Join(dir, "calls.log")
	shim := `#!/bin/sh
while [ $# -gt 0 ]; do
  case "$1" in
    -o) shift 2 ;;
    -*) shift ;;
    *) break ;;
  esac
done
shift
` + body + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ssh"), []byte(shim), 0o755))
	t.Setenv("BOSUN_TEST_SSH_CAPTURE", capture)
	t.Setenv("BOSUN_TEST_SSH_MARKER", filepath.Join(dir, "transport-failed-once"))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return capture
}

func countCapturedSSHCalls(t *testing.T, path, call string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return strings.Count(string(data), call+"\n")
}
