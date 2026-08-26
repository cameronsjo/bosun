package update

import (
	"context"
	"errors"
	"io"
	"runtime"
	"testing"
	"time"

	"github.com/creativeprojects/go-selfupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeUpdateClient struct {
	release        *Release
	found          bool
	detectErr      error
	updateErr      error
	detectContext  context.Context
	updateContext  context.Context
	afterDetect    func()
	detectCalls    int
	updateCalls    int
	updatedRelease *Release
	updatedPath    string
}

func (f *fakeUpdateClient) DetectLatest(ctx context.Context) (*Release, bool, error) {
	f.detectCalls++
	f.detectContext = ctx
	if f.afterDetect != nil {
		f.afterDetect()
	}
	return f.release, f.found, f.detectErr
}

func (f *fakeUpdateClient) UpdateTo(ctx context.Context, release *Release, path string) error {
	f.updateCalls++
	f.updateContext = ctx
	f.updatedRelease = release
	f.updatedPath = path
	return f.updateErr
}

func useUpdateDependencies(t *testing.T, client updateClient, clientErr error, path string, pathErr error) {
	t.Helper()

	previousNewUpdateClient := newUpdateClient
	previousExecutablePath := executablePath
	newUpdateClient = func() (updateClient, error) { return client, clientErr }
	executablePath = func() (string, error) { return path, pathErr }
	t.Cleanup(func() {
		newUpdateClient = previousNewUpdateClient
		executablePath = previousExecutablePath
	})
}

func testRelease(version string) *Release {
	return &Release{
		Version:     version,
		ReleaseURL:  "https://example.test/releases/" + version,
		PublishedAt: "2026-08-26",
		Changelog:   "Fixed the thing",
	}
}

type updateTestContextKey struct{}

func TestUpdateAvailable(t *testing.T) {
	tests := []struct {
		name           string
		currentVersion string
		latestVersion  string
		want           bool
		wantErr        string
	}{
		{name: "development build", currentVersion: "dev", latestVersion: "1.2.3", want: true},
		{name: "newer release", currentVersion: "1.2.2", latestVersion: "1.2.3", want: true},
		{name: "leading v", currentVersion: "v1.2.2", latestVersion: "v1.2.3", want: true},
		{name: "same release", currentVersion: "1.2.3", latestVersion: "1.2.3"},
		{name: "older release", currentVersion: "1.2.4", latestVersion: "1.2.3"},
		{name: "invalid current", currentVersion: "not-a-version", latestVersion: "1.2.3", wantErr: `parsing current version "not-a-version"`},
		{name: "invalid latest", currentVersion: "1.2.3", latestVersion: "not-a-version", wantErr: `parsing latest version "not-a-version"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := updateAvailable(tt.currentVersion, tt.latestVersion)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.False(t, got)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCheckForUpdate(t *testing.T) {
	t.Run("rejects nil context", func(t *testing.T) {
		client := &fakeUpdateClient{}
		useUpdateDependencies(t, client, nil, "", nil)
		var ctx context.Context

		release, available, err := CheckForUpdate(ctx, "1.2.3")

		require.ErrorIs(t, err, ErrNilContext)
		assert.Nil(t, release)
		assert.False(t, available)
		assert.Zero(t, client.detectCalls)
	})

	t.Run("preserves canceled context identity", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		client := &fakeUpdateClient{}
		useUpdateDependencies(t, client, nil, "", nil)

		release, available, err := CheckForUpdate(ctx, "1.2.3")

		require.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, release)
		assert.False(t, available)
		assert.Zero(t, client.detectCalls)
	})

	t.Run("stops when context is canceled during detection", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
		client := &fakeUpdateClient{
			release:     testRelease("1.2.3"),
			found:       true,
			afterDetect: cancel,
		}
		useUpdateDependencies(t, client, nil, "", nil)

		release, available, err := CheckForUpdate(ctx, "1.2.2")

		require.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, release)
		assert.False(t, available)
		assert.Equal(t, 1, client.detectCalls)
	})

	t.Run("rejects missing release metadata", func(t *testing.T) {
		client := &fakeUpdateClient{found: true}
		useUpdateDependencies(t, client, nil, "", nil)

		release, available, err := CheckForUpdate(context.Background(), "1.2.3")

		require.ErrorIs(t, err, ErrMissingRelease)
		assert.Nil(t, release)
		assert.False(t, available)
	})

	t.Run("propagates caller context", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), updateTestContextKey{}, "request")
		client := &fakeUpdateClient{release: testRelease("1.2.3"), found: true}
		useUpdateDependencies(t, client, nil, "", nil)

		release, available, err := CheckForUpdate(ctx, "1.2.2")

		require.NoError(t, err)
		assert.NotNil(t, release)
		assert.True(t, available)
		assert.Same(t, ctx, client.detectContext)
	})

	t.Run("client creation failure", func(t *testing.T) {
		clientErr := errors.New("client unavailable")
		useUpdateDependencies(t, nil, clientErr, "", nil)

		release, available, err := CheckForUpdate(context.Background(), "1.2.3")

		require.ErrorIs(t, err, clientErr)
		assert.Nil(t, release)
		assert.False(t, available)
	})

	t.Run("release detection failure", func(t *testing.T) {
		detectErr := errors.New("GitHub unavailable")
		client := &fakeUpdateClient{detectErr: detectErr}
		useUpdateDependencies(t, client, nil, "", nil)

		release, available, err := CheckForUpdate(context.Background(), "1.2.3")

		require.ErrorIs(t, err, detectErr)
		assert.Contains(t, err.Error(), "detecting latest version")
		assert.Nil(t, release)
		assert.False(t, available)
		assert.Equal(t, 1, client.detectCalls)
	})

	t.Run("no matching release", func(t *testing.T) {
		client := &fakeUpdateClient{}
		useUpdateDependencies(t, client, nil, "", nil)

		release, available, err := CheckForUpdate(context.Background(), "1.2.3")

		require.NoError(t, err)
		assert.Nil(t, release)
		assert.False(t, available)
	})

	t.Run("already current", func(t *testing.T) {
		client := &fakeUpdateClient{release: testRelease("1.2.3"), found: true}
		useUpdateDependencies(t, client, nil, "", nil)

		release, available, err := CheckForUpdate(context.Background(), "1.2.3")

		require.NoError(t, err)
		assert.Nil(t, release)
		assert.False(t, available)
	})

	t.Run("invalid current version", func(t *testing.T) {
		client := &fakeUpdateClient{release: testRelease("1.2.3"), found: true}
		useUpdateDependencies(t, client, nil, "", nil)

		release, available, err := CheckForUpdate(context.Background(), "broken")

		require.Error(t, err)
		assert.Contains(t, err.Error(), `parsing current version "broken"`)
		assert.Nil(t, release)
		assert.False(t, available)
	})

	for _, currentVersion := range []string{"1.2.2", "dev"} {
		t.Run("available from "+currentVersion, func(t *testing.T) {
			latest := testRelease("1.2.3")
			client := &fakeUpdateClient{release: latest, found: true}
			useUpdateDependencies(t, client, nil, "", nil)

			release, available, err := CheckForUpdate(context.Background(), currentVersion)

			require.NoError(t, err)
			assert.Same(t, latest, release)
			assert.True(t, available)
		})
	}
}

func TestUpdate(t *testing.T) {
	t.Run("rejects nil context", func(t *testing.T) {
		client := &fakeUpdateClient{}
		useUpdateDependencies(t, client, nil, "", nil)
		var ctx context.Context

		release, err := Update(ctx, "1.2.3")

		require.ErrorIs(t, err, ErrNilContext)
		assert.Nil(t, release)
		assert.Zero(t, client.detectCalls)
	})

	t.Run("preserves expired deadline identity", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 0)
		t.Cleanup(cancel)
		client := &fakeUpdateClient{}
		useUpdateDependencies(t, client, nil, "", nil)

		release, err := Update(ctx, "1.2.3")

		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Nil(t, release)
		assert.Zero(t, client.detectCalls)
	})

	t.Run("stops when canceled during detection", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		latest := testRelease("1.2.3")
		client := &fakeUpdateClient{release: latest, found: true, afterDetect: cancel}
		useUpdateDependencies(t, client, nil, "/opt/bin/bosun", nil)

		release, err := Update(ctx, "1.2.2")

		require.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, release)
		assert.Zero(t, client.updateCalls)
	})

	t.Run("stops when canceled before replacement", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		latest := testRelease("1.2.3")
		client := &fakeUpdateClient{release: latest, found: true}
		useUpdateDependencies(t, client, nil, "/opt/bin/bosun", nil)
		executablePath = func() (string, error) {
			cancel()
			return "/opt/bin/bosun", nil
		}

		release, err := Update(ctx, "1.2.2")

		require.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, release)
		assert.Zero(t, client.updateCalls)
	})

	t.Run("rejects missing release metadata", func(t *testing.T) {
		client := &fakeUpdateClient{found: true}
		useUpdateDependencies(t, client, nil, "/opt/bin/bosun", nil)

		release, err := Update(context.Background(), "1.2.3")

		require.ErrorIs(t, err, ErrMissingRelease)
		assert.Nil(t, release)
		assert.Zero(t, client.updateCalls)
	})

	t.Run("propagates caller context", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), updateTestContextKey{}, "request")
		latest := testRelease("1.2.3")
		client := &fakeUpdateClient{release: latest, found: true}
		useUpdateDependencies(t, client, nil, "/opt/bin/bosun", nil)

		release, err := Update(ctx, "1.2.2")

		require.NoError(t, err)
		assert.Same(t, latest, release)
		assert.Same(t, ctx, client.detectContext)
		assert.Same(t, ctx, client.updateContext)
	})

	t.Run("client creation failure", func(t *testing.T) {
		clientErr := errors.New("client unavailable")
		useUpdateDependencies(t, nil, clientErr, "", nil)

		release, err := Update(context.Background(), "1.2.3")

		require.ErrorIs(t, err, clientErr)
		assert.Nil(t, release)
	})

	t.Run("release detection failure", func(t *testing.T) {
		detectErr := errors.New("GitHub unavailable")
		client := &fakeUpdateClient{detectErr: detectErr}
		useUpdateDependencies(t, client, nil, "", nil)

		release, err := Update(context.Background(), "1.2.3")

		require.ErrorIs(t, err, detectErr)
		assert.Contains(t, err.Error(), "detecting latest version")
		assert.Nil(t, release)
	})

	t.Run("no matching release", func(t *testing.T) {
		client := &fakeUpdateClient{}
		useUpdateDependencies(t, client, nil, "", nil)

		release, err := Update(context.Background(), "1.2.3")

		require.EqualError(t, err, "no releases found for cameronsjo/bosun")
		assert.Nil(t, release)
	})

	t.Run("already current", func(t *testing.T) {
		client := &fakeUpdateClient{release: testRelease("1.2.3"), found: true}
		useUpdateDependencies(t, client, nil, "/tmp/bosun", nil)

		release, err := Update(context.Background(), "1.2.3")

		require.NoError(t, err)
		assert.Nil(t, release)
		assert.Zero(t, client.updateCalls)
	})

	t.Run("invalid current version", func(t *testing.T) {
		client := &fakeUpdateClient{release: testRelease("1.2.3"), found: true}
		useUpdateDependencies(t, client, nil, "/tmp/bosun", nil)

		release, err := Update(context.Background(), "broken")

		require.Error(t, err)
		assert.Contains(t, err.Error(), `parsing current version "broken"`)
		assert.Nil(t, release)
		assert.Zero(t, client.updateCalls)
	})

	t.Run("executable path failure", func(t *testing.T) {
		pathErr := errors.New("executable disappeared")
		client := &fakeUpdateClient{release: testRelease("1.2.3"), found: true}
		useUpdateDependencies(t, client, nil, "", pathErr)

		release, err := Update(context.Background(), "1.2.2")

		require.ErrorIs(t, err, pathErr)
		assert.Contains(t, err.Error(), "getting executable path")
		assert.Nil(t, release)
		assert.Zero(t, client.updateCalls)
	})

	t.Run("installation failure", func(t *testing.T) {
		updateErr := errors.New("replacement denied")
		latest := testRelease("1.2.3")
		client := &fakeUpdateClient{release: latest, found: true, updateErr: updateErr}
		useUpdateDependencies(t, client, nil, "/opt/bin/bosun", nil)

		release, err := Update(context.Background(), "1.2.2")

		require.ErrorIs(t, err, updateErr)
		assert.Contains(t, err.Error(), "updating binary")
		assert.Nil(t, release)
		assert.Equal(t, 1, client.updateCalls)
		assert.Same(t, latest, client.updatedRelease)
		assert.Equal(t, "/opt/bin/bosun", client.updatedPath)
	})

	for _, currentVersion := range []string{"1.2.2", "dev"} {
		t.Run("installs from "+currentVersion, func(t *testing.T) {
			latest := testRelease("1.2.3")
			client := &fakeUpdateClient{release: latest, found: true}
			useUpdateDependencies(t, client, nil, "/opt/bin/bosun", nil)

			release, err := Update(context.Background(), currentVersion)

			require.NoError(t, err)
			assert.Same(t, latest, release)
			assert.Equal(t, 1, client.updateCalls)
			assert.Same(t, latest, client.updatedRelease)
			assert.Equal(t, "/opt/bin/bosun", client.updatedPath)
		})
	}
}

type testSource struct {
	releases []selfupdate.SourceRelease
	listErr  error
}

func (s testSource) ListReleases(context.Context, selfupdate.Repository) ([]selfupdate.SourceRelease, error) {
	return s.releases, s.listErr
}

func (testSource) DownloadReleaseAsset(context.Context, *selfupdate.Release, int64) (io.ReadCloser, error) {
	return nil, errors.New("download is not expected")
}

type testSourceRelease struct {
	version     string
	publishedAt time.Time
	assets      []selfupdate.SourceAsset
}

func (r testSourceRelease) GetID() int64                        { return 7 }
func (r testSourceRelease) GetTagName() string                  { return r.version }
func (r testSourceRelease) GetDraft() bool                      { return false }
func (r testSourceRelease) GetPrerelease() bool                 { return false }
func (r testSourceRelease) GetPublishedAt() time.Time           { return r.publishedAt }
func (r testSourceRelease) GetReleaseNotes() string             { return "Release notes" }
func (r testSourceRelease) GetName() string                     { return "Bosun " + r.version }
func (r testSourceRelease) GetURL() string                      { return "https://example.test/" + r.version }
func (r testSourceRelease) GetAssets() []selfupdate.SourceAsset { return r.assets }

type testSourceAsset struct {
	name string
}

func (testSourceAsset) GetID() int64                    { return 11 }
func (a testSourceAsset) GetName() string               { return a.name }
func (testSourceAsset) GetSize() int                    { return 1024 }
func (a testSourceAsset) GetBrowserDownloadURL() string { return "https://example.test/" + a.name }

func TestSelfUpdateClientDetectLatest(t *testing.T) {
	t.Run("maps release metadata", func(t *testing.T) {
		publishedAt := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)
		assetName := "bosun_" + runtime.GOOS + "_" + runtime.GOARCH
		source := testSource{releases: []selfupdate.SourceRelease{testSourceRelease{
			version:     "v1.2.3",
			publishedAt: publishedAt,
			assets:      []selfupdate.SourceAsset{testSourceAsset{name: assetName}},
		}}}
		updater, err := selfupdate.NewUpdater(selfupdate.Config{Source: source})
		require.NoError(t, err)
		client := &selfUpdateClient{updater: updater}

		release, found, err := client.DetectLatest(context.Background())

		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, release)
		assert.Equal(t, "1.2.3", release.Version)
		assert.Equal(t, "https://example.test/v1.2.3", release.ReleaseURL)
		assert.Equal(t, "2026-08-26", release.PublishedAt)
		assert.Equal(t, "Release notes", release.Changelog)
		assert.NotNil(t, release.native)
	})

	t.Run("reports no matching release", func(t *testing.T) {
		updater, err := selfupdate.NewUpdater(selfupdate.Config{Source: testSource{}})
		require.NoError(t, err)
		client := &selfUpdateClient{updater: updater}

		release, found, err := client.DetectLatest(context.Background())

		require.NoError(t, err)
		assert.False(t, found)
		assert.Nil(t, release)
	})

	t.Run("propagates source error", func(t *testing.T) {
		listErr := errors.New("release source unavailable")
		updater, err := selfupdate.NewUpdater(selfupdate.Config{Source: testSource{listErr: listErr}})
		require.NoError(t, err)
		client := &selfUpdateClient{updater: updater}

		release, found, err := client.DetectLatest(context.Background())

		require.ErrorIs(t, err, listErr)
		assert.False(t, found)
		assert.Nil(t, release)
	})
}

func TestSelfUpdateClientUpdateToRejectsMissingNativeRelease(t *testing.T) {
	updater, err := selfupdate.NewUpdater(selfupdate.Config{Source: testSource{}})
	require.NoError(t, err)
	client := &selfUpdateClient{updater: updater}

	err = client.UpdateTo(context.Background(), &Release{}, t.TempDir()+"/bosun")

	require.ErrorIs(t, err, selfupdate.ErrInvalidRelease)
}

func TestDefaultUpdateClientConstruction(t *testing.T) {
	client, err := newUpdateClient()

	require.NoError(t, err)
	assert.IsType(t, &selfUpdateClient{}, client)
}

func TestGetPlatformInfo(t *testing.T) {
	assert.Equal(t, runtime.GOOS+"/"+runtime.GOARCH, GetPlatformInfo())
}
