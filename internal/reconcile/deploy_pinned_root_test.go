package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeployLocal_DirectoryTargetPinsAboveContainerWritableDir asserts the
// directory deploy pins above appdata/<service>.
//
// Type-transition discovery rejects a service directory that is already a
// symlink, so the exposure is the window between that check and the copy: the
// copy re-resolves its root by path, and os.OpenRoot follows a symlink the
// service's own container puts there in between, redirecting the whole
// rendered tree. The mkdirAll seam stands in for the container winning that
// race.
func TestDeployLocal_DirectoryTargetPinsAboveContainerWritableDir(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	stagingDir := filepath.Join(base, "staging")
	stagingService := filepath.Join(stagingDir, "unraid", "appdata", "svc")
	require.NoError(t, os.MkdirAll(stagingService, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stagingService, "secret.conf"), []byte("token: rendered"), 0o644))

	appdata := filepath.Join(base, "appdata")
	require.NoError(t, os.MkdirAll(appdata, 0o755))
	outside := filepath.Join(base, "outside")
	require.NoError(t, os.MkdirAll(outside, 0o755))
	serviceDir := filepath.Join(appdata, "svc")

	swapped := false
	ops := &DeployOps{ContentHashSync: true}
	ops.localFS = &localDeployFS{
		mkdirAll: func(ctx context.Context, path string, mode os.FileMode) error {
			if err := mkdirAllContext(ctx, path, mode); err != nil {
				return err
			}
			if path == serviceDir && !swapped {
				swapped = true
				if err := os.RemoveAll(path); err != nil {
					return err
				}
				return os.Symlink(outside, path)
			}
			return nil
		},
	}

	r := NewReconciler(&Config{
		StagingDir:       stagingDir,
		InfraSubDir:      "unraid",
		LocalAppdataPath: appdata,
	}, WithDeployOps(ops))

	_, err = r.deployLocal(context.Background(), nil)
	require.True(t, swapped, "the test must exercise the directory deploy path")
	require.Error(t, err, "a swapped service directory must fail the deploy, not be written through")

	_, statErr := os.Stat(filepath.Join(outside, "secret.conf"))
	assert.ErrorIs(t, statErr, os.ErrNotExist, "the rendered tree must not land outside appdata")
}

// TestDeployLocal_SingleFileTargetMakesNoUnpinnedDestinationMutation asserts the
// single-file deploy branch mutates the destination only through the pinned
// copy. A path-resolved os.MkdirAll ahead of that copy created directories as
// host root before the pinned copy could refuse anything; here the copy fails
// on its source, so a destination that appears at all is one that call created.
func TestDeployLocal_SingleFileTargetMakesNoUnpinnedDestinationMutation(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	stagingDir := filepath.Join(base, "staging")
	stagingRoot := filepath.Join(stagingDir, "unraid")
	require.NoError(t, os.MkdirAll(stagingRoot, 0o755))
	// A source the copy refuses before it touches the destination.
	require.NoError(t, os.Symlink(filepath.Join(base, "absent"), filepath.Join(stagingRoot, "service.yml")))

	appdata := filepath.Join(base, "appdata")
	r := NewReconciler(&Config{
		StagingDir:       stagingDir,
		InfraSubDir:      "unraid",
		LocalAppdataPath: appdata,
	}, WithDeployOps(&DeployOps{ContentHashSync: true}))

	_, err = r.deployLocal(context.Background(), nil)
	require.Error(t, err, "an unreadable source must fail the deploy")

	_, statErr := os.Stat(appdata)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "a refused copy must leave no destination directory behind")
}
