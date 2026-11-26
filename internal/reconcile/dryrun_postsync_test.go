package reconcile

import (
	"context"
	"testing"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRun_DryRun_RunsNoPostSyncHooks pins that a dry run restarts no container
// for a post-sync hook, even when a docker client is available and a hook
// matches. `bosun validate --full` relies on it: it runs the reconciler as a dry
// run with the project's post-sync hooks left in place, and promises that no hook
// runs. The CLI attaches no docker client today, so the other guard (a nil client
// function) would hide a missing dry-run guard; this test hands the reconciler a
// client so only the dry-run guard stands between the hook and the restart.
//
// The deploy is remote so that every hook is eligible whatever the deploy
// result holds ("remote deploys make every hook eligible"), and a remote dry run
// returns before any SSH contact.
func TestRun_DryRun_RunsNoPostSyncHooks(t *testing.T) {
	h := newRunHarness(t, true)
	h.cfg.DeployMode = "remote"
	h.cfg.TargetHost = "root@192.0.2.1"
	h.cfg.RemoteAppdataPath = "/mnt/user/appdata"
	h.cfg.PostSyncHooks = NewConfigField([]PostSyncHook{
		{Container: "web", Paths: []string{"**"}, Action: "restart"},
	})

	var restarted []string
	mockAPI := newReconcileMockDockerAPI()
	mockAPI.containerListFunc = dockerListHealthyStub
	mockAPI.containerRestartFunc = func(_ context.Context, container string, _ client.ContainerRestartOptions) (client.ContainerRestartResult, error) {
		restarted = append(restarted, container)
		return client.ContainerRestartResult{}, nil
	}

	r := NewReconciler(h.cfg,
		WithGitOperations(gitSyncing("aaa111", "bbb222")),
		WithDockerClient(docker.NewClientWithAPI(mockAPI)),
	)

	require.NoError(t, r.Run(context.Background()))

	assert.Empty(t, restarted, "a dry run must not run post-sync hooks")
}
