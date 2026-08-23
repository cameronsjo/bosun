//go:build !windows

package reconcile

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDockerComposeCommandContext_CancellationSendsSIGTERM(t *testing.T) {
	shim, events := installComposeCancellationHelper(t, "exit-on-term")
	ctx, cancel := context.WithCancel(context.Background())
	d := &DeployOps{ComposeUpTimeout: time.Minute}

	done := make(chan error, 1)
	go func() {
		done <- d.ComposeUpMultiple(ctx, []string{"compose.yml"})
	}()

	waitForComposeCancellationEvent(t, events, "started")
	waitForComposeCancellationEvent(t, events, "daemon-started")
	started := time.Now()
	cancel()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
		assert.Less(t, time.Since(started), dockerComposeCancelGrace,
			"a cooperative Docker CLI should exit during the grace period")
	case <-time.After(dockerComposeCancelGrace + time.Second):
		t.Fatal("compose command did not stop after cancellation")
	}

	waitForComposeCancellationEvent(t, events, "term")
	waitForComposeCancellationEvent(t, events, "daemon-term")
	time.Sleep(600 * time.Millisecond)
	contents, err := os.ReadFile(events)
	require.NoError(t, err)
	assert.NotContains(t, string(contents), "daemon-completed",
		"graceful CLI cancellation must stop the independent daemon-side operation")
	assert.FileExists(t, shim)
}

func TestDockerComposeCommandContext_DeadlinePreservesContextError(t *testing.T) {
	_, events := installComposeCancellationHelper(t, "exit-on-term")
	ctx := &controlledDeadlineContext{
		Context:  context.Background(),
		done:     make(chan struct{}),
		deadline: time.Now().Add(time.Minute),
	}
	d := &DeployOps{ComposeUpTimeout: time.Minute}

	done := make(chan error, 1)
	go func() {
		done <- d.ComposeUpMultiple(ctx, []string{"compose.yml"})
	}()

	waitForComposeCancellationEvent(t, events, "started")
	waitForComposeCancellationEvent(t, events, "daemon-started")
	close(ctx.done)

	select {
	case err := <-done:
		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(dockerComposeCancelGrace + time.Second):
		t.Fatal("compose command did not stop after its deadline")
	}
	waitForComposeCancellationEvent(t, events, "term")
	waitForComposeCancellationEvent(t, events, "daemon-term")
}

func TestConfigureComposeCancellation_EscalatesAfterGrace(t *testing.T) {
	shim, events := installComposeCancellationHelper(t, "ignore-term")
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, shim)
	const grace = 100 * time.Millisecond
	configureComposeCancellation(cmd, grace)

	require.NoError(t, cmd.Start())
	waitForComposeCancellationEvent(t, events, "started")
	waitForComposeCancellationEvent(t, events, "daemon-started")
	started := time.Now()
	cancel()
	err := cmd.Wait()
	elapsed := time.Since(started)

	require.Error(t, err)
	assert.GreaterOrEqual(t, elapsed, grace,
		"the command must receive its graceful cancellation window before escalation")
	assert.Less(t, elapsed, time.Second,
		"an uncooperative Docker CLI must be force-killed after the grace period")
	waitForComposeCancellationEvent(t, events, "term")
	waitForComposeCancellationEvent(t, events, "daemon-term")
	time.Sleep(500 * time.Millisecond)
	contents, readErr := os.ReadFile(events)
	require.NoError(t, readErr)
	assert.NotContains(t, string(contents), "completed")
}

func TestConfigureComposeCancellation_ProcessNotStarted(t *testing.T) {
	cmd := exec.Command("docker", "compose", "ps")
	configureComposeCancellation(cmd, time.Second)

	require.NotNil(t, cmd.Cancel)
	assert.ErrorIs(t, cmd.Cancel(), os.ErrProcessDone)
	assert.Equal(t, time.Second, cmd.WaitDelay)
}

func TestConfigureComposeCancellation_ProcessAlreadyExited(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "true")
	configureComposeCancellation(cmd, time.Second)
	require.NoError(t, cmd.Run())

	assert.ErrorIs(t, cmd.Cancel(), os.ErrProcessDone)
}

func TestCancelComposeProcess_SyscallFailures(t *testing.T) {
	tests := []struct {
		name       string
		getpgidErr error
		termErr    error
		wantErr    error
		wantKills  []syscall.Signal
	}{
		{
			name:       "process disappeared before group lookup",
			getpgidErr: syscall.ESRCH,
			wantErr:    os.ErrProcessDone,
		},
		{
			name:       "group lookup permission failure",
			getpgidErr: syscall.EPERM,
			wantErr:    syscall.EPERM,
		},
		{
			name:      "process disappeared before graceful signal",
			termErr:   syscall.ESRCH,
			wantErr:   os.ErrProcessDone,
			wantKills: []syscall.Signal{syscall.SIGTERM},
		},
		{
			name:      "graceful signal permission failure",
			termErr:   syscall.EPERM,
			wantErr:   syscall.EPERM,
			wantKills: []syscall.Signal{syscall.SIGTERM},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotKills []syscall.Signal
			err := cancelComposeProcessWithOps(
				&os.Process{Pid: 1234},
				time.Second,
				composeCancelPollInterval,
				func(pid int) (int, error) {
					assert.Equal(t, 1234, pid)
					return 1234, tt.getpgidErr
				},
				func(pid int, signal syscall.Signal) error {
					assert.Equal(t, -1234, pid)
					gotKills = append(gotKills, signal)
					return tt.termErr
				},
			)

			assert.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, tt.wantKills, gotKills)
		})
	}
}

func TestCancelComposeProcess_PollingHandlesPermissionRace(t *testing.T) {
	pollCalls := 0
	err := cancelComposeProcessWithOps(
		&os.Process{Pid: 4321},
		time.Hour,
		time.Millisecond,
		func(int) (int, error) { return 4321, nil },
		func(pid int, signal syscall.Signal) error {
			assert.Equal(t, -4321, pid)
			switch signal {
			case syscall.SIGTERM:
				return nil
			case 0:
				pollCalls++
				if pollCalls == 1 {
					return syscall.EPERM
				}
				return syscall.ESRCH
			default:
				t.Fatalf("unexpected signal %v", signal)
				return nil
			}
		},
	)

	require.NoError(t, err)
	assert.Equal(t, 2, pollCalls)
}

func TestCancelComposeProcess_PollingFailure(t *testing.T) {
	err := cancelComposeProcessWithOps(
		&os.Process{Pid: 4321},
		time.Hour,
		time.Millisecond,
		func(int) (int, error) { return 4321, nil },
		func(_ int, signal syscall.Signal) error {
			if signal == syscall.SIGTERM {
				return nil
			}
			return syscall.EINVAL
		},
	)

	assert.ErrorIs(t, err, syscall.EINVAL)
}

func TestCancelComposeProcess_EscalationResults(t *testing.T) {
	tests := []struct {
		name    string
		killErr error
		wantErr error
	}{
		{name: "group already exited", killErr: syscall.ESRCH},
		{name: "force kill denied", killErr: syscall.EPERM, wantErr: syscall.EPERM},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotSignals []syscall.Signal
			err := cancelComposeProcessWithOps(
				&os.Process{Pid: 9876},
				0,
				time.Hour,
				func(int) (int, error) { return 9876, nil },
				func(pid int, signal syscall.Signal) error {
					assert.Equal(t, -9876, pid)
					gotSignals = append(gotSignals, signal)
					if signal == syscall.SIGKILL {
						return tt.killErr
					}
					return nil
				},
			)

			if tt.wantErr == nil {
				require.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tt.wantErr)
			}
			assert.Equal(t, []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL}, gotSignals)
		})
	}
}

func TestDockerComposeCancellationHelper(t *testing.T) {
	mode := os.Getenv("BOSUN_TEST_COMPOSE_CANCEL_MODE")
	if mode == "" {
		return
	}

	events := os.Getenv("BOSUN_TEST_COMPOSE_CANCEL_EVENTS")
	signals := make(chan os.Signal, 1)
	signalNotify(signals)
	defer signalStop(signals)

	if mode == "daemon-work" || mode == "ignore-term-daemon" {
		appendComposeCancellationEvent(events, "daemon-started")
		select {
		case <-signals:
			appendComposeCancellationEvent(events, "daemon-term")
			if mode == "ignore-term-daemon" {
				time.Sleep(400 * time.Millisecond)
				appendComposeCancellationEvent(events, "daemon-completed")
			}
		case <-time.After(30 * time.Second):
			appendComposeCancellationEvent(events, "daemon-completed")
		}
		return
	}

	var daemonWork *exec.Cmd
	if mode == "exit-on-term" || mode == "ignore-term" {
		daemonWork = exec.Command(os.Args[0], "-test.run=^TestDockerComposeCancellationHelper$")
		daemonMode := "daemon-work"
		if mode == "ignore-term" {
			daemonMode = "ignore-term-daemon"
		}
		daemonWork.Env = composeCancellationHelperEnv(daemonMode)
		if err := daemonWork.Start(); err != nil {
			os.Exit(2)
		}
	}
	appendComposeCancellationEvent(events, "started")

	select {
	case <-signals:
		appendComposeCancellationEvent(events, "term")
		if mode == "exit-on-term" {
			return
		}
		select {}
	case <-time.After(30 * time.Second):
		appendComposeCancellationEvent(events, "completed")
	}
}

func composeCancellationHelperEnv(mode string) []string {
	const key = "BOSUN_TEST_COMPOSE_CANCEL_MODE="
	env := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, key) {
			env = append(env, entry)
		}
	}
	return append(env, key+mode)
}

func installComposeCancellationHelper(t *testing.T, mode string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	events := filepath.Join(dir, "events.log")
	shim := filepath.Join(dir, "docker")
	script := "#!/bin/sh\nexec \"$BOSUN_TEST_COMPOSE_CANCEL_BINARY\" -test.run=^TestDockerComposeCancellationHelper$ -- \"$@\"\n"
	require.NoError(t, os.WriteFile(shim, []byte(script), 0o755))
	t.Setenv("BOSUN_TEST_COMPOSE_CANCEL_BINARY", os.Args[0])
	t.Setenv("BOSUN_TEST_COMPOSE_CANCEL_MODE", mode)
	t.Setenv("BOSUN_TEST_COMPOSE_CANCEL_EVENTS", events)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return shim, events
}

func waitForComposeCancellationEvent(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(path)
		if err == nil {
			for _, event := range strings.Split(string(contents), "\n") {
				if event == want {
					return
				}
			}
		}
		if err != nil && !os.IsNotExist(err) {
			require.NoError(t, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("event %q was not recorded in %s", want, path)
}

func appendComposeCancellationEvent(path, event string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		os.Exit(2)
	}
	_, writeErr := f.WriteString(event + "\n")
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		os.Exit(2)
	}
}

// Wrappers keep all direct os/signal use in the helper process. Calling
// signal.Notify in the parent test would consume the signal under test.
func signalNotify(ch chan<- os.Signal) {
	signal.Notify(ch, syscall.SIGTERM)
}

func signalStop(ch chan<- os.Signal) {
	signal.Stop(ch)
}

type controlledDeadlineContext struct {
	context.Context
	done     chan struct{}
	deadline time.Time
}

func (c *controlledDeadlineContext) Deadline() (time.Time, bool) {
	return c.deadline, true
}

func (c *controlledDeadlineContext) Done() <-chan struct{} {
	return c.done
}

func (c *controlledDeadlineContext) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}
