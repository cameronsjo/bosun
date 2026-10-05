package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deployEscapeFixture arranges what a compromised container can do to its own
// appdata volume: replace a directory under the deploy root with a symlink to a
// directory outside it. bosun deploys as host root, so a write that follows
// that symlink lands wherever the container chose.
type deployEscapeFixture struct {
	appdata string
	outside string
	src     string
	target  string
	escaped string
}

func newDeployEscapeFixture(t *testing.T) deployEscapeFixture {
	t.Helper()

	tmpDir := t.TempDir()
	fixture := deployEscapeFixture{
		appdata: filepath.Join(tmpDir, "appdata"),
		outside: filepath.Join(tmpDir, "outside"),
		src:     filepath.Join(tmpDir, "staging", "appdata", "svc", "config.yml"),
	}
	fixture.target = filepath.Join(fixture.appdata, "svc", "config.yml")
	fixture.escaped = filepath.Join(fixture.outside, "config.yml")

	require.NoError(t, os.MkdirAll(filepath.Dir(fixture.src), 0o755))
	require.NoError(t, os.WriteFile(fixture.src, []byte("rendered config"), 0o644))
	require.NoError(t, os.MkdirAll(fixture.appdata, 0o755))
	require.NoError(t, os.MkdirAll(fixture.outside, 0o755))
	require.NoError(t, os.Symlink(fixture.outside, filepath.Join(fixture.appdata, "svc")))
	return fixture
}

// TestDeployLocalFileManaged_PinsWriteToDeployRoot covers the single-file
// deploy path the reconciler dispatches for every non-directory target. Both
// sync modes must refuse a destination that leaves the deploy root.
func TestDeployLocalFileManaged_PinsWriteToDeployRoot(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name            string
		contentHashSync bool
	}{
		{name: "content-hash mode", contentHashSync: true},
		{name: "standard mode", contentHashSync: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fixture := newDeployEscapeFixture(t)
			deploy := &DeployOps{ContentHashSync: tt.contentHashSync}
			result := &DeployResult{}

			err := deploy.deployLocalFileManaged(context.Background(), fixture.src, fixture.target, fixture.appdata, result, nil)

			require.Error(t, err)
			assert.NoFileExists(t, fixture.escaped, "the deploy must not write outside the appdata root")
			assert.Empty(t, result.WrittenFiles, "a refused write must not be reported as deployed")
		})
	}
}

// TestDeployLocalFileManaged_WritesNormalTargetUnderDeployRoot keeps the
// ordinary single-file deploy working once its write is pinned.
func TestDeployLocalFileManaged_WritesNormalTargetUnderDeployRoot(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name            string
		contentHashSync bool
	}{
		{name: "content-hash mode", contentHashSync: true},
		{name: "standard mode", contentHashSync: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			appdata := filepath.Join(tmpDir, "appdata")
			require.NoError(t, os.MkdirAll(appdata, 0o755))
			src := filepath.Join(tmpDir, "staging", "traefik.yml")
			require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
			require.NoError(t, os.WriteFile(src, []byte("rendered config"), 0o644))
			target := filepath.Join(appdata, "traefik.yml")

			deploy := &DeployOps{ContentHashSync: tt.contentHashSync}
			require.NoError(t, deploy.deployLocalFileManaged(context.Background(), src, target, appdata, nil, nil))

			deployed, err := os.ReadFile(target)
			require.NoError(t, err)
			assert.Equal(t, "rendered config", string(deployed))
		})
	}
}
