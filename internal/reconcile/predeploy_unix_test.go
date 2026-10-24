//go:build !windows

package reconcile

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// descendantExitWatch makes a FIFO that a hook's background child holds open
// for writing, and returns its path and a channel that receives once every
// writer has closed it: EOF on the read end means the child has exited (its
// descriptors close when it dies, zombie or not). This avoids PID checks,
// which cannot tell a zombie from a live process and can hit a reused PID.
func descendantExitWatch(t *testing.T, dir string) (fifo string, exited <-chan error) {
	t.Helper()
	fifo = filepath.Join(dir, "alive")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	ch := make(chan error, 1)
	go func() {
		// Blocks until the child opens the write end.
		f, err := os.Open(fifo)
		if err != nil {
			ch <- err
			return
		}
		_, err = io.Copy(io.Discard, f)
		_ = f.Close()
		ch <- err
	}()
	return fifo, ch
}

func requireDescendantExited(t *testing.T, exited <-chan error, msg string) {
	t.Helper()
	select {
	case err := <-exited:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal(msg)
	}
}

// TestRunPreDeployHooks_TimeoutKillsProcessGroup pins that a timeout reaches
// the hook's children, not only the direct process.
func TestRunPreDeployHooks_TimeoutKillsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	fifo, exited := descendantExitWatch(t, dir)
	hooks := []PreDeployHook{{
		Name:    "spawner",
		Command: []string{"sh", "-c", "sleep 60 3>'" + fifo + "' & sleep 60"},
		Timeout: Duration{Duration: 500 * time.Millisecond},
	}}

	err := runPreDeployHooks(context.Background(), hooks, dir, "t", "c")

	require.ErrorIs(t, err, context.DeadlineExceeded)
	requireDescendantExited(t, exited, "a child of a timed-out hook must not survive")
}

// TestRunPreDeployHooks_PassingHookLeavesNoBackgroundChild pins that the
// group is killed after a hook exits 0, so a background child cannot keep
// running while the deploy proceeds.
func TestRunPreDeployHooks_PassingHookLeavesNoBackgroundChild(t *testing.T) {
	dir := t.TempDir()
	fifo, exited := descendantExitWatch(t, dir)
	hooks := []PreDeployHook{{
		Name: "detacher",
		// Wait until the child has the FIFO open, so the hook cannot exit
		// before there is a child to kill.
		Command: []string{"sh", "-c", "sleep 60 3>'" + fifo + "' >/dev/null 2>&1 & sleep 0.5; exit 0"},
	}}

	require.NoError(t, runPreDeployHooks(context.Background(), hooks, dir, "t", "c"))
	requireDescendantExited(t, exited, "a background child of a passing hook must be killed")
}

// TestDocumentedNoValueHookIgnoresItsOwnConfig runs the `<no value>` example
// from the onboard GitOps guide against a staging tree that holds a copy of
// bosun.yaml, as staging does when the config sits in the render source. The
// example must pass on a clean tree and block on a rendered sentinel, not
// match the pattern written in its own config.
func TestDocumentedNoValueHookIgnoresItsOwnConfig(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "skills", "onboard", "resources", "gitops.md"))
	require.NoError(t, err)
	m := regexp.MustCompile(`(?m)^    command: (\[.*no value.*\])$`).FindSubmatch(doc)
	require.NotNil(t, m, "the GitOps guide must keep its <no value> example")
	var command []string
	require.NoError(t, json.Unmarshal(m[1], &command))

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "compose"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".bosun"), 0o755))
	config := "pre_deploy_hooks:\n  - command: " + string(m[1]) + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bosun.yaml"), []byte(config), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".bosun", "config.yml"), []byte(config), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose", "a.yml"), []byte("services: {}\n"), 0o644))
	hooks := []PreDeployHook{{Name: "doc-example", Command: command}}

	require.NoError(t, runPreDeployHooks(context.Background(), hooks, dir, "t", "c"),
		"the example must not match the pattern in its own config")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose", "b.yml"), []byte("password: <no value>\n"), 0o644))
	err = runPreDeployHooks(context.Background(), hooks, dir, "t", "c")
	assert.ErrorIs(t, err, ErrPreDeployHookFailed, "the example must block a rendered sentinel")
}
