package daemon

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type lifecycleTriggerHandler func(http.ResponseWriter, *http.Request)

func lifecycleSocketHandler(d *Daemon) lifecycleTriggerHandler {
	d.socketServer.allowedUIDs[testSocketUID] = struct{}{}
	return func(w http.ResponseWriter, r *http.Request) {
		d.socketServer.handleTrigger(w, withTestSocketPeer(r))
	}
}

func lifecycleTriggerHandlers(t *testing.T, d *Daemon) map[string]lifecycleTriggerHandler {
	t.Helper()
	tcp, err := NewTCPServer(d, "127.0.0.1:0", "test-token")
	require.NoError(t, err)
	d.config.AllowUnauthenticatedWebhook = true
	webhook := NewServer(d)
	return map[string]lifecycleTriggerHandler{
		"socket":  lifecycleSocketHandler(d),
		"tcp":     tcp.handleTrigger,
		"api":     d.handleAPITrigger,
		"webhook": webhook.handleWebhook,
		"manual":  webhook.handleManualTrigger,
		"github": func(w http.ResponseWriter, r *http.Request) {
			req := httptest.NewRequest(
				http.MethodPost,
				"/webhook/github",
				strings.NewReader(`{"ref":"refs/heads/main","pusher":{"name":"lifecycle-test"}}`),
			).WithContext(r.Context())
			req.Header.Set("X-GitHub-Event", "push")
			webhook.handleGitHubWebhook(w, req)
		},
	}
}

func waitForDaemonTasks(t *testing.T, d *Daemon, timeout time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatal("daemon reconcile goroutines did not finish")
	}
}

func waitForTaskContext(t *testing.T, started <-chan context.Context) context.Context {
	t.Helper()
	select {
	case ctx := <-started:
		return ctx
	case <-time.After(time.Second):
		t.Fatal("trigger handler did not start its reconcile goroutine")
		return nil
	}
}

func TestTriggerHandlersUseDaemonLifecycleAndWaitGroup(t *testing.T) {
	for name, makeHandler := range map[string]func(*testing.T, *Daemon) lifecycleTriggerHandler{
		"socket": func(_ *testing.T, d *Daemon) lifecycleTriggerHandler {
			return lifecycleSocketHandler(d)
		},
		"tcp": func(t *testing.T, d *Daemon) lifecycleTriggerHandler {
			tcp, err := NewTCPServer(d, "127.0.0.1:0", "test-token")
			require.NoError(t, err)
			return tcp.handleTrigger
		},
		"api": func(_ *testing.T, d *Daemon) lifecycleTriggerHandler { return d.handleAPITrigger },
		"webhook": func(_ *testing.T, d *Daemon) lifecycleTriggerHandler {
			d.config.AllowUnauthenticatedWebhook = true
			return NewServer(d).handleWebhook
		},
		"manual": func(_ *testing.T, d *Daemon) lifecycleTriggerHandler {
			d.config.AllowUnauthenticatedWebhook = true
			return NewServer(d).handleManualTrigger
		},
		"github": func(_ *testing.T, d *Daemon) lifecycleTriggerHandler {
			d.config.AllowUnauthenticatedWebhook = true
			webhook := NewServer(d)
			return func(w http.ResponseWriter, r *http.Request) {
				req := httptest.NewRequest(
					http.MethodPost,
					"/webhook/github",
					strings.NewReader(`{"ref":"refs/heads/main","pusher":{"name":"lifecycle-test"}}`),
				).WithContext(r.Context())
				req.Header.Set("X-GitHub-Event", "push")
				webhook.handleGitHubWebhook(w, req)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := newConcurrencyDaemon(t)
			d.config.ShutdownTimeout = 2 * time.Second
			started := make(chan context.Context, 1)
			release := make(chan struct{})
			d.triggerReconcileFn = func(ctx context.Context, _ string, _ bool) error {
				started <- ctx
				<-ctx.Done()
				<-release
				return ctx.Err()
			}

			requestCtx := log.WithRequestID(context.Background(), "lifecycle-request-id")
			requestCtx, cancelRequest := context.WithCancel(requestCtx)
			req := httptest.NewRequest(http.MethodPost, "/trigger", strings.NewReader(`{"source":"lifecycle-test"}`)).WithContext(requestCtx)
			recorder := httptest.NewRecorder()
			makeHandler(t, d)(recorder, req)
			require.Equal(t, http.StatusAccepted, recorder.Code)

			taskCtx := waitForTaskContext(t, started)
			assert.Equal(t, "lifecycle-request-id", log.RequestIDFromContext(taskCtx))
			cancelRequest()
			select {
			case <-taskCtx.Done():
				t.Fatal("reconcile inherited request cancellation instead of daemon lifecycle")
			case <-time.After(20 * time.Millisecond):
			}

			shutdownDone := make(chan error, 1)
			go func() { shutdownDone <- d.shutdown() }()
			require.Eventually(t, func() bool { return taskCtx.Err() != nil }, time.Second, 5*time.Millisecond)

			select {
			case <-shutdownDone:
				t.Fatal("shutdown returned before the tracked reconcile goroutine unwound")
			case <-time.After(20 * time.Millisecond):
			}

			close(release)
			require.NoError(t, <-shutdownDone)
			waitForDaemonTasks(t, d, time.Second)
		})
	}
}

func TestTriggerReconcilePanicStillReleasesDaemonWaitGroup(t *testing.T) {
	d := newConcurrencyDaemon(t)
	started := make(chan struct{})
	d.triggerReconcileFn = func(context.Context, string, bool) error {
		close(started)
		panic("simulated async trigger panic")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/trigger", strings.NewReader(`{"source":"panic-test"}`))
	recorder := httptest.NewRecorder()
	d.handleAPITrigger(recorder, req)
	require.Equal(t, http.StatusAccepted, recorder.Code)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("trigger handler did not start its reconcile goroutine")
	}
	waitForDaemonTasks(t, d, time.Second)
}

func TestShutdownTimeoutCancelsButDoesNotLeakTempDirWork(t *testing.T) {
	d := newConcurrencyDaemon(t)
	d.config.ShutdownTimeout = 40 * time.Millisecond
	started := make(chan context.Context, 1)
	cancelled := make(chan struct{})
	release := make(chan struct{})
	lateWrite := filepath.Join(t.TempDir(), "after-cancel.txt")
	d.triggerReconcileFn = func(ctx context.Context, _ string, _ bool) error {
		started <- ctx
		<-ctx.Done()
		close(cancelled)
		<-release // Simulate cleanup that does not honor cancellation promptly.
		return os.WriteFile(lateWrite, []byte("finished"), 0o600)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/trigger", nil)
	recorder := httptest.NewRecorder()
	d.handleAPITrigger(recorder, req)
	require.Equal(t, http.StatusAccepted, recorder.Code)
	waitForTaskContext(t, started)

	start := time.Now()
	require.NoError(t, d.shutdown())
	elapsed := time.Since(start)
	assert.GreaterOrEqual(t, elapsed, d.config.ShutdownTimeout)
	assert.Less(t, elapsed, time.Second)
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel the daemon lifecycle context")
	}

	close(release)
	waitForDaemonTasks(t, d, time.Second)
	content, err := os.ReadFile(lateWrite)
	require.NoError(t, err)
	assert.Equal(t, "finished", string(content))
}

func TestTriggerRejectedAfterShutdownBegins(t *testing.T) {
	d := newConcurrencyDaemon(t)
	var called atomic.Bool
	d.triggerReconcileFn = func(context.Context, string, bool) error {
		called.Store(true)
		return errors.New("must not run")
	}
	d.cancelLifecycle()

	for name, handler := range lifecycleTriggerHandlers(t, d) {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/trigger", nil)
			recorder := httptest.NewRecorder()
			handler(recorder, req)
			assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
		})
	}
	assert.False(t, called.Load())
}

func TestDaemonShutdownIsIdempotentAndKeepsLifecycleClosed(t *testing.T) {
	d := newConcurrencyDaemon(t)
	d.config.ShutdownTimeout = time.Second

	require.NoError(t, d.shutdown())
	require.NoError(t, d.shutdown())
	assert.False(t, d.startReconcileGoroutine(context.Background(), func(context.Context) {
		t.Error("reconcile task started after shutdown")
	}))
}
