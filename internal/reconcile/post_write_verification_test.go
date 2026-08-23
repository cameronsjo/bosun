package reconcile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/fileutil"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func postWriteVerificationError() error {
	return fmt.Errorf("%w: injected readback failure", fileutil.ErrPostWriteVerification)
}

func TestDeployOps_DeployLocalTracksWriteOnPostWriteVerificationFailure(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourceDir := filepath.Join(tmpDir, "source")
	targetDir := filepath.Join(tmpDir, "target")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "config.yml"), []byte("new"), 0o644))

	deploy := &DeployOps{
		ContentHashSync: true,
		copyDirIfChangedFn: func(_, dst string) ([]string, error) {
			require.NoError(t, os.MkdirAll(dst, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dst, "config.yml"), []byte("new"), 0o644))
			return []string{"config.yml"}, postWriteVerificationError()
		},
	}
	result := &DeployResult{}

	err := deploy.DeployLocal(context.Background(), sourceDir, targetDir, result, nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, fileutil.ErrPostWriteVerification)
	assert.Equal(t, []string{"config.yml"}, result.WrittenFiles)
}

func TestDeployOps_DeployLocalFileTracksWriteOnPostWriteVerificationFailure(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	sourceFile := filepath.Join(tmpDir, "source.yml")
	targetFile := filepath.Join(tmpDir, "target", "config.yml")
	require.NoError(t, os.WriteFile(sourceFile, []byte("new"), 0o644))

	deploy := &DeployOps{
		ContentHashSync: true,
		copyFileIfChangedFn: func(_, dst string) (bool, error) {
			require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
			require.NoError(t, os.WriteFile(dst, []byte("new"), 0o644))
			return true, postWriteVerificationError()
		},
	}
	result := &DeployResult{}

	err := deploy.DeployLocalFile(context.Background(), sourceFile, targetFile, result)

	require.Error(t, err)
	assert.ErrorIs(t, err, fileutil.ErrPostWriteVerification)
	assert.Equal(t, []string{"config.yml"}, result.WrittenFiles)
}

func TestRun_OnlyPostWriteVerificationFailureFiresFailedDeployHook(t *testing.T) {
	tests := []struct {
		name        string
		copyErr     error
		wantRestart bool
	}{
		{name: "post-write verification failure", copyErr: postWriteVerificationError(), wantRestart: true},
		{name: "ordinary copy failure", copyErr: errors.New("injected copy failure"), wantRestart: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _, stateFile, _ := gateScopeReconciler(
				t,
				HealthGateScopeOff,
				nil,
				dockerListHealthyStub,
			)
			r.config.PostSyncHooks = NewConfigField([]PostSyncHook{
				{Container: "downstream", Paths: []string{"compose/**"}, Action: "restart"},
				{Container: "unrelated", Paths: []string{"appdata/**"}, Action: "restart"},
			})
			var restarted []string
			mockAPI := newReconcileMockDockerAPI()
			mockAPI.containerListFunc = dockerListHealthyStub
			mockAPI.containerRestartFunc = func(_ context.Context, container string, _ client.ContainerRestartOptions) (client.ContainerRestartResult, error) {
				restarted = append(restarted, container)
				return client.ContainerRestartResult{}, nil
			}
			dockerClient := docker.NewClientWithAPI(mockAPI)
			r.dockerClientFn = func() *docker.Client { return dockerClient }
			r.deploy.copyDirIfChangedFn = func(src, dst string) ([]string, error) {
				require.NoError(t, fileutil.CopyDir(src, dst))
				return []string{"stub.yml"}, tt.copyErr
			}

			err := r.Run(context.Background())

			require.Error(t, err)
			if tt.wantRestart {
				assert.ErrorIs(t, err, fileutil.ErrPostWriteVerification)
				assert.Equal(t, []string{"downstream"}, restarted,
					"the typed post-rename failure must run only hooks matching the recorded write")
			} else {
				assert.NotErrorIs(t, err, fileutil.ErrPostWriteVerification)
				assert.Empty(t, restarted, "ordinary deployment failures must not run hooks")
			}
			saved := LoadState(stateFile)
			assert.True(t, saved.NeedsRedeploy, "copy failure must not be recorded as a successful deploy")
			assert.Equal(t, "prevcommit", saved.LastDeployedCommit)
			assert.Equal(t, 1, saved.AttemptCount)
			assert.Zero(t, saved.DeployCount)
		})
	}
}

func TestDeployLocal_PostWriteVerificationResultUsesHookPath(t *testing.T) {
	tmpDir := t.TempDir()
	stagingDir := filepath.Join(tmpDir, "staging")
	appdataDir := filepath.Join(tmpDir, "appdata")
	sourceDir := filepath.Join(stagingDir, "unraid", "appdata", "authelia")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.MkdirAll(appdataDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "configuration.yml"), []byte("new"), 0o644))

	deploy := &DeployOps{
		ContentHashSync: true,
		copyDirIfChangedFn: func(_, dst string) ([]string, error) {
			require.NoError(t, os.MkdirAll(dst, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dst, "configuration.yml"), []byte("new"), 0o644))
			return []string{"configuration.yml"}, postWriteVerificationError()
		},
	}
	r := NewReconciler(&Config{
		DryRun:                  true,
		AllowEmptyDeclaredState: true,
		StagingDir:              stagingDir,
		InfraSubDir:             "unraid",
		LocalAppdataPath:        appdataDir,
	}, WithDeployOps(deploy))

	result, err := r.deployLocal(context.Background(), nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, fileutil.ErrPostWriteVerification)
	require.NotNil(t, result)
	assert.Equal(t, []string{filepath.Join("appdata", "authelia", "configuration.yml")}, result.WrittenFiles)
	matched := EvaluatePostSyncHooks(result.WrittenFiles, []PostSyncHook{{
		Paths:     []string{"appdata/authelia/**"},
		Action:    "restart",
		Container: "authelia",
	}})
	assert.Len(t, matched, 1)

	// The typed failure is the only one eligible for the failed-deploy hook path.
	assert.True(t, errors.Is(err, fileutil.ErrPostWriteVerification))
}

func TestDeployLocalFile_PostWriteVerificationResultUsesHookPath(t *testing.T) {
	tmpDir := t.TempDir()
	stagingDir := filepath.Join(tmpDir, "staging")
	appdataDir := filepath.Join(tmpDir, "appdata")
	sourceFile := filepath.Join(stagingDir, "unraid", "appdata", "service.env")
	require.NoError(t, os.MkdirAll(filepath.Dir(sourceFile), 0o755))
	require.NoError(t, os.MkdirAll(appdataDir, 0o755))
	require.NoError(t, os.WriteFile(sourceFile, []byte("new"), 0o644))

	deploy := &DeployOps{
		ContentHashSync: true,
		copyFileIfChangedFn: func(_, dst string) (bool, error) {
			require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
			require.NoError(t, os.WriteFile(dst, []byte("new"), 0o644))
			return true, postWriteVerificationError()
		},
	}
	r := NewReconciler(&Config{
		DryRun:                  true,
		AllowEmptyDeclaredState: true,
		StagingDir:              stagingDir,
		InfraSubDir:             "unraid",
		LocalAppdataPath:        appdataDir,
	}, WithDeployOps(deploy))

	result, err := r.deployLocal(context.Background(), nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, fileutil.ErrPostWriteVerification)
	require.NotNil(t, result)
	assert.Equal(t, []string{filepath.Join("appdata", "service.env")}, result.WrittenFiles)
	matched := EvaluatePostSyncHooks(result.WrittenFiles, []PostSyncHook{
		{Paths: []string{"appdata/*.env"}, Action: "restart", Container: "service"},
		{Paths: []string{"compose/**"}, Action: "restart", Container: "unrelated"},
	})
	require.Len(t, matched, 1)
	assert.Equal(t, "service", matched[0].Container)
}
