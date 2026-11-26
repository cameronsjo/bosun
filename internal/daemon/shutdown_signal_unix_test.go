//go:build !windows

package daemon

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestNotifyShutdown_StopDetaches checks that the stop func returned by
// notifyShutdown really unregisters the channel. SIGUSR1 stands in for the
// shutdown signals so the test cannot terminate the test binary: a sentinel is
// registered first, and every send is confirmed delivered before the next step.
func TestNotifyShutdown_StopDetaches(t *testing.T) {
	sentinel := make(chan os.Signal, 2)
	signal.Notify(sentinel, syscall.SIGUSR1)
	t.Cleanup(func() { signal.Stop(sentinel) })

	ch, stop := notifyShutdown(syscall.SIGUSR1)

	waitFor := func(c <-chan os.Signal, what string) {
		t.Helper()
		select {
		case <-c:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s did not receive SIGUSR1", what)
		}
	}

	// Positive control: before stop, both channels receive the signal. Without
	// this, the empty-channel check below would pass for a channel that was
	// never registered at all.
	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGUSR1))
	waitFor(ch, "the notify channel before stop")
	waitFor(sentinel, "the sentinel before stop")

	stop()

	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGUSR1))
	// The sentinel receiving proves the second signal was delivered. Only then
	// is it meaningful that the stopped channel stayed empty.
	waitFor(sentinel, "the sentinel after stop")

	select {
	case sig := <-ch:
		t.Fatalf("stopped channel still received %v", sig)
	default:
	}
}
