package reconcile

import (
	"context"
	"testing"
	"time"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runHealthGateWithGuard runs the gate in a goroutine so a gate that never
// honors its timeout fails the test instead of hanging the package.
func runHealthGateWithGuard(t *testing.T, ctx context.Context, dockerClient *docker.Client, timeout, interval time.Duration) error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- CheckCriticalContainerHealth(ctx, dockerClient, []string{"authelia"}, timeout, interval)
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("health gate did not return within 3s; the timeout does not bound a hung Inspect")
		return nil
	}
}

// A Docker Inspect that never returns must not outlive HealthGateTimeout: the
// gate's timeout has to reach the Inspect call, not only the wait between polls.
func TestCheckCriticalContainerHealth_HungInspectHonorsTimeout(t *testing.T) {
	mockAPI := newReconcileMockDockerAPI()
	mockAPI.containerInspectFunc = func(ctx context.Context, _ string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
		<-ctx.Done()
		return client.ContainerInspectResult{}, ctx.Err()
	}

	err := runHealthGateWithGuard(t, context.Background(), docker.NewClientWithAPI(mockAPI), 100*time.Millisecond, 20*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "critical container health gate failed")
	assert.Contains(t, err.Error(), "missing")
}

// When the caller cancels while the gate's own deadline is also due, the gate
// must report the cancellation, not a "missing" gate failure built from
// inspections that only failed because the context was canceled. The original
// select picks one ready case at random, so 20 iterations catch it with
// probability 1 - 2^-20; this test is probabilistic on the original code and
// deterministic on the fixed code.
func TestCheckCriticalContainerHealth_CancelOnDeadlineTieReturnsCtxErr(t *testing.T) {
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		mockAPI := newReconcileMockDockerAPI()
		mockAPI.containerInspectFunc = func(inspectCtx context.Context, _ string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
			time.Sleep(5 * time.Millisecond)
			cancel()
			return client.ContainerInspectResult{}, inspectCtx.Err()
		}

		err := runHealthGateWithGuard(t, ctx, docker.NewClientWithAPI(mockAPI), time.Millisecond, time.Millisecond)
		cancel()
		require.Error(t, err, "iteration %d", i)
		require.ErrorIs(t, err, context.Canceled, "iteration %d: got %v", i, err)
		assert.NotContains(t, err.Error(), "missing", "iteration %d", i)
	}
}

// A caller cancel that lands while the gate waits between polls must return
// context.Canceled, never a gate error assembled from a final inspection run on
// the canceled context. The cancel lands at 20ms, well before the 50ms poll
// tick and the 1s gate timeout, so on the fixed code only the gate context's
// Done channel is ready and the final-path re-checks decide the result.
func TestCheckCriticalContainerHealth_CancelDuringWaitReturnsCanceledNotGateError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockAPI := newReconcileMockDockerAPI()
	mockAPI.containerInspectFunc = func(inspectCtx context.Context, name string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
		if err := inspectCtx.Err(); err != nil {
			return client.ContainerInspectResult{}, err
		}
		return makeInspectResponse(name, "running", &container.Health{Status: "starting"}), nil
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := runHealthGateWithGuard(t, ctx, docker.NewClientWithAPI(mockAPI), time.Second, 50*time.Millisecond)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	assert.NotContains(t, err.Error(), "missing")
	assert.NotContains(t, err.Error(), "health gate failed")
}
