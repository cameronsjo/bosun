package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creativeprojects/go-selfupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testReleaseID  int64 = 101
	testArchiveID  int64 = 102
	testChecksumID int64 = 103
)

type memoryDownload struct {
	releaseID int64
	assetID   int64
}

type memoryUpdateSource struct {
	releases       []selfupdate.SourceRelease
	assets         map[int64][]byte
	downloadErrors map[int64]error
	beforeDownload func(int64)
	downloads      []memoryDownload
	listCalls      int
}

func (s *memoryUpdateSource) ListReleases(
	ctx context.Context,
	_ selfupdate.Repository,
) ([]selfupdate.SourceRelease, error) {
	s.listCalls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.releases, nil
}

func (s *memoryUpdateSource) DownloadReleaseAsset(
	ctx context.Context,
	release *selfupdate.Release,
	assetID int64,
) (io.ReadCloser, error) {
	s.downloads = append(s.downloads, memoryDownload{releaseID: release.ReleaseID, assetID: assetID})
	if s.beforeDownload != nil {
		s.beforeDownload(assetID)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.downloadErrors[assetID]; err != nil {
		return nil, err
	}
	data, found := s.assets[assetID]
	if !found {
		return nil, selfupdate.ErrAssetNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

type memorySourceRelease struct {
	id           int64
	version      string
	prerelease   bool
	publishedAt  time.Time
	releaseNotes string
	assets       []selfupdate.SourceAsset
}

func (r memorySourceRelease) GetID() int64              { return r.id }
func (r memorySourceRelease) GetTagName() string        { return r.version }
func (memorySourceRelease) GetDraft() bool              { return false }
func (r memorySourceRelease) GetPrerelease() bool       { return r.prerelease }
func (r memorySourceRelease) GetPublishedAt() time.Time { return r.publishedAt }
func (r memorySourceRelease) GetReleaseNotes() string   { return r.releaseNotes }
func (r memorySourceRelease) GetName() string           { return "Bosun " + r.version }
func (r memorySourceRelease) GetURL() string {
	return fmt.Sprintf("https://example.test/releases/%d", r.id)
}
func (r memorySourceRelease) GetAssets() []selfupdate.SourceAsset { return r.assets }

type memorySourceAsset struct {
	id   int64
	name string
	size int
	url  string
}

func (a memorySourceAsset) GetID() int64                  { return a.id }
func (a memorySourceAsset) GetName() string               { return a.name }
func (a memorySourceAsset) GetSize() int                  { return a.size }
func (a memorySourceAsset) GetBrowserDownloadURL() string { return a.url }

type checksumFixture struct {
	client      *selfUpdateClient
	source      *memoryUpdateSource
	release     memorySourceRelease
	archiveName string
	checksum    []byte
}

func newChecksumFixture(t *testing.T, osName, archName, executable string) checksumFixture {
	t.Helper()

	archiveName := fmt.Sprintf("bosun_1.2.3_%s_%s.tar.gz", osName, archName)
	archiveBytes := makeTarGz(t, "bosun", []byte(executable))
	digest := sha256.Sum256(archiveBytes)
	checksum := []byte(fmt.Sprintf("%x  %s\n", digest, archiveName))
	archiveURL := "https://example.test/assets/" + archiveName
	checksumURL := "https://example.test/assets/" + checksumAssetName
	release := memorySourceRelease{
		id:           testReleaseID,
		version:      "v1.2.3",
		publishedAt:  time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC),
		releaseNotes: "Verified update",
		assets: []selfupdate.SourceAsset{
			memorySourceAsset{id: testArchiveID, name: archiveName, size: len(archiveBytes), url: archiveURL},
			memorySourceAsset{id: testChecksumID, name: checksumAssetName, size: len(checksum), url: checksumURL},
		},
	}
	source := &memoryUpdateSource{
		releases: []selfupdate.SourceRelease{release},
		assets: map[int64][]byte{
			testArchiveID:  archiveBytes,
			testChecksumID: checksum,
		},
		downloadErrors: make(map[int64]error),
	}
	updater, err := newChecksumUpdater(source, osName, archName)
	require.NoError(t, err)

	return checksumFixture{
		client:      &selfUpdateClient{updater: updater},
		source:      source,
		release:     release,
		archiveName: archiveName,
		checksum:    checksum,
	}
}

func makeTarGz(t *testing.T, executableName string, executable []byte) []byte {
	t.Helper()

	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	require.NoError(t, tarWriter.WriteHeader(&tar.Header{
		Name: executableName,
		Mode: 0o755,
		Size: int64(len(executable)),
	}))
	_, err := tarWriter.Write(executable)
	require.NoError(t, err)
	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())
	return buffer.Bytes()
}

func sentinelExecutable(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "bosun")
	require.NoError(t, os.WriteFile(path, []byte("original executable"), 0o755))
	return path
}

func assertExecutableContents(t *testing.T, path, want string) {
	t.Helper()

	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, want, string(contents))
}

func detectFixtureRelease(t *testing.T, fixture checksumFixture) *Release {
	t.Helper()

	release, found, err := fixture.client.DetectLatest(context.Background())
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, release)
	return release
}

func TestChecksumUpdateSucceedsForPublishedPlatforms(t *testing.T) {
	platforms := []struct {
		os   string
		arch string
	}{
		{os: "darwin", arch: "amd64"},
		{os: "darwin", arch: "arm64"},
		{os: "linux", arch: "amd64"},
		{os: "linux", arch: "arm64"},
	}

	for _, platform := range platforms {
		t.Run(platform.os+"_"+platform.arch, func(t *testing.T) {
			fixture := newChecksumFixture(t, platform.os, platform.arch, "verified executable")
			release := detectFixtureRelease(t, fixture)
			target := sentinelExecutable(t)

			require.Equal(t, fixture.archiveName, release.native.AssetName)
			require.Equal(t, testReleaseID, release.native.ReleaseID)
			require.Equal(t, testArchiveID, release.native.AssetID)
			require.Equal(t, testChecksumID, release.native.ValidationAssetID)
			require.Len(t, release.native.ValidationChain, 1)
			assert.Equal(t, checksumAssetName, release.native.ValidationChain[0].ValidationAssetName)
			assert.Equal(t, "https://example.test/assets/"+checksumAssetName, release.native.ValidationChain[0].ValidationAssetURL)

			err := fixture.client.UpdateTo(context.Background(), release, target)

			require.NoError(t, err)
			assertExecutableContents(t, target, "verified executable")
			assert.Equal(t, []memoryDownload{
				{releaseID: testReleaseID, assetID: testArchiveID},
				{releaseID: testReleaseID, assetID: testChecksumID},
			}, fixture.source.downloads)
		})
	}
}

func TestChecksumUpdateFailsClosed(t *testing.T) {
	tests := []struct {
		name      string
		manifest  func(checksumFixture) []byte
		wantError error
	}{
		{
			name: "malformed selected entry",
			manifest: func(fixture checksumFixture) []byte {
				return []byte("not-a-sha256  " + fixture.archiveName + "\n")
			},
			wantError: selfupdate.ErrIncorrectChecksumFile,
		},
		{
			name: "malformed content before selected entry",
			manifest: func(fixture checksumFixture) []byte {
				return append([]byte("malformed\n"), fixture.checksum...)
			},
			wantError: selfupdate.ErrIncorrectChecksumFile,
		},
		{
			name: "selected archive entry missing",
			manifest: func(fixture checksumFixture) []byte {
				return []byte(strings.Repeat("0", 64) + "  " + fixture.archiveName + ".backup\n")
			},
			wantError: selfupdate.ErrHashNotFound,
		},
		{
			name: "digest mismatch",
			manifest: func(fixture checksumFixture) []byte {
				return []byte(strings.Repeat("0", 64) + "  " + fixture.archiveName + "\n")
			},
			wantError: selfupdate.ErrChecksumValidationFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newChecksumFixture(t, "darwin", "arm64", "new executable")
			fixture.source.assets[testChecksumID] = tt.manifest(fixture)
			release := detectFixtureRelease(t, fixture)
			target := sentinelExecutable(t)

			err := fixture.client.UpdateTo(context.Background(), release, target)

			require.ErrorIs(t, err, tt.wantError)
			assert.Contains(t, err.Error(), fixture.archiveName)
			assertExecutableContents(t, target, "original executable")
			assert.Equal(t, []memoryDownload{
				{releaseID: testReleaseID, assetID: testArchiveID},
				{releaseID: testReleaseID, assetID: testChecksumID},
			}, fixture.source.downloads)
		})
	}
}

func TestChecksumUpdateStopsParsingAfterExactMatch(t *testing.T) {
	fixture := newChecksumFixture(t, "darwin", "arm64", "verified executable")
	fixture.source.assets[testChecksumID] = append(fixture.checksum, []byte("malformed trailing content\n")...)
	release := detectFixtureRelease(t, fixture)
	target := sentinelExecutable(t)

	err := fixture.client.UpdateTo(context.Background(), release, target)

	require.NoError(t, err)
	assertExecutableContents(t, target, "verified executable")
	assert.Equal(t, []memoryDownload{
		{releaseID: testReleaseID, assetID: testArchiveID},
		{releaseID: testReleaseID, assetID: testChecksumID},
	}, fixture.source.downloads)
}

func TestChecksumUpdateRejectsMissingManifestDuringDetection(t *testing.T) {
	fixture := newChecksumFixture(t, "darwin", "arm64", "new executable")
	fixture.release.assets = fixture.release.assets[:1]
	fixture.source.releases = []selfupdate.SourceRelease{fixture.release}
	target := sentinelExecutable(t)

	release, found, err := fixture.client.DetectLatest(context.Background())

	require.ErrorIs(t, err, selfupdate.ErrValidationAssetNotFound)
	assert.Contains(t, err.Error(), checksumAssetName)
	assert.False(t, found)
	assert.Nil(t, release)
	assert.Empty(t, fixture.source.downloads)
	assertExecutableContents(t, target, "original executable")
}

func TestChecksumUpdatePropagatesAssetDownloadFailures(t *testing.T) {
	tests := []struct {
		name          string
		assetID       int64
		wantAssetName string
		wantDownloads []memoryDownload
	}{
		{
			name:          "archive download",
			assetID:       testArchiveID,
			wantAssetName: "bosun_1.2.3_darwin_arm64.tar.gz",
			wantDownloads: []memoryDownload{{releaseID: testReleaseID, assetID: testArchiveID}},
		},
		{
			name:          "checksum download",
			assetID:       testChecksumID,
			wantAssetName: checksumAssetName,
			wantDownloads: []memoryDownload{
				{releaseID: testReleaseID, assetID: testArchiveID},
				{releaseID: testReleaseID, assetID: testChecksumID},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			downloadErr := errors.New("release asset unavailable")
			fixture := newChecksumFixture(t, "darwin", "arm64", "new executable")
			fixture.source.downloadErrors[tt.assetID] = downloadErr
			release := detectFixtureRelease(t, fixture)
			target := sentinelExecutable(t)

			err := fixture.client.UpdateTo(context.Background(), release, target)

			require.ErrorIs(t, err, downloadErr)
			assert.Contains(t, err.Error(), tt.wantAssetName)
			assert.Equal(t, tt.wantDownloads, fixture.source.downloads)
			assertExecutableContents(t, target, "original executable")
		})
	}
}

func TestChecksumUpdateRejectsAssetDisappearanceAfterDetection(t *testing.T) {
	tests := []struct {
		name          string
		assetID       int64
		wantAssetName string
		wantDownloads []memoryDownload
	}{
		{
			name:          "archive",
			assetID:       testArchiveID,
			wantAssetName: "bosun_1.2.3_darwin_arm64.tar.gz",
			wantDownloads: []memoryDownload{{releaseID: testReleaseID, assetID: testArchiveID}},
		},
		{
			name:          "checksum",
			assetID:       testChecksumID,
			wantAssetName: checksumAssetName,
			wantDownloads: []memoryDownload{
				{releaseID: testReleaseID, assetID: testArchiveID},
				{releaseID: testReleaseID, assetID: testChecksumID},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newChecksumFixture(t, "darwin", "arm64", "new executable")
			release := detectFixtureRelease(t, fixture)
			delete(fixture.source.assets, tt.assetID)
			target := sentinelExecutable(t)

			err := fixture.client.UpdateTo(context.Background(), release, target)

			require.ErrorIs(t, err, selfupdate.ErrAssetNotFound)
			assert.Contains(t, err.Error(), tt.wantAssetName)
			assert.Equal(t, tt.wantDownloads, fixture.source.downloads)
			assertExecutableContents(t, target, "original executable")
		})
	}
}

func TestChecksumUpdatePreservesDetectedReleaseAssets(t *testing.T) {
	fixture := newChecksumFixture(t, "darwin", "arm64", "first release executable")
	release := detectFixtureRelease(t, fixture)
	second := newChecksumFixture(t, "darwin", "arm64", "second release executable")
	second.release.id = 201
	second.release.version = "v1.2.4"
	second.release.assets[0] = memorySourceAsset{id: 202, name: "bosun_1.2.4_darwin_arm64.tar.gz", url: "https://example.test/assets/bosun_1.2.4_darwin_arm64.tar.gz"}
	second.release.assets[1] = memorySourceAsset{id: 203, name: checksumAssetName, url: "https://example.test/assets/v1.2.4/checksums.txt"}
	fixture.source.releases = []selfupdate.SourceRelease{second.release}
	target := sentinelExecutable(t)

	err := fixture.client.UpdateTo(context.Background(), release, target)

	require.NoError(t, err)
	assert.Equal(t, 1, fixture.source.listCalls)
	assert.Equal(t, []memoryDownload{
		{releaseID: testReleaseID, assetID: testArchiveID},
		{releaseID: testReleaseID, assetID: testChecksumID},
	}, fixture.source.downloads)
	assertExecutableContents(t, target, "first release executable")
}

func TestChecksumUpdatePreservesCancellation(t *testing.T) {
	fixture := newChecksumFixture(t, "darwin", "arm64", "new executable")
	release := detectFixtureRelease(t, fixture)
	ctx, cancel := context.WithCancel(context.Background())
	fixture.source.beforeDownload = func(assetID int64) {
		if assetID == testChecksumID {
			cancel()
		}
	}
	target := sentinelExecutable(t)

	err := fixture.client.UpdateTo(ctx, release, target)

	require.ErrorIs(t, err, context.Canceled)
	assert.Contains(t, err.Error(), checksumAssetName)
	assert.Equal(t, []memoryDownload{
		{releaseID: testReleaseID, assetID: testArchiveID},
		{releaseID: testReleaseID, assetID: testChecksumID},
	}, fixture.source.downloads)
	assertExecutableContents(t, target, "original executable")
}

func TestChecksumCheckOnlyUsesMetadataWithoutDownloads(t *testing.T) {
	fixture := newChecksumFixture(t, "darwin", "arm64", "new executable")
	useUpdateDependencies(t, fixture.client, nil, "", nil)

	release, available, err := CheckForUpdate(context.Background(), "1.2.2")

	require.NoError(t, err)
	require.NotNil(t, release)
	assert.True(t, available)
	assert.Equal(t, "1.2.3", release.Version)
	assert.Empty(t, fixture.source.downloads)
}

func TestChecksumCheckOnlyRejectsMissingManifestWithoutDownloads(t *testing.T) {
	fixture := newChecksumFixture(t, "darwin", "arm64", "new executable")
	fixture.release.assets = fixture.release.assets[:1]
	fixture.source.releases = []selfupdate.SourceRelease{fixture.release}
	useUpdateDependencies(t, fixture.client, nil, "", nil)

	release, available, err := CheckForUpdate(context.Background(), "1.2.2")

	require.ErrorIs(t, err, selfupdate.ErrValidationAssetNotFound)
	assert.Contains(t, err.Error(), checksumAssetName)
	assert.Nil(t, release)
	assert.False(t, available)
	assert.Empty(t, fixture.source.downloads)
}

func TestChecksumUpdaterPreservesStableReleaseSelection(t *testing.T) {
	fixture := newChecksumFixture(t, "darwin", "arm64", "stable executable")
	prerelease := fixture.release
	prerelease.id = 201
	prerelease.version = "v1.3.0-beta.1"
	prerelease.prerelease = true
	fixture.source.releases = []selfupdate.SourceRelease{prerelease, fixture.release}

	release := detectFixtureRelease(t, fixture)

	assert.Equal(t, "1.2.3", release.Version)
	assert.Equal(t, testReleaseID, release.native.ReleaseID)
	assert.Empty(t, fixture.source.downloads)
}

func TestChecksumUpdaterPreservesUnsupportedPlatformResults(t *testing.T) {
	fixture := newChecksumFixture(t, "darwin", "arm64", "new executable")
	updater, err := newChecksumUpdater(fixture.source, "windows", "amd64")
	require.NoError(t, err)
	client := &selfUpdateClient{updater: updater}
	useUpdateDependencies(t, client, nil, sentinelExecutable(t), nil)

	release, available, checkErr := CheckForUpdate(context.Background(), "1.2.2")
	installed, installErr := Update(context.Background(), "1.2.2")

	require.NoError(t, checkErr)
	assert.Nil(t, release)
	assert.False(t, available)
	require.EqualError(t, installErr, "no releases found for cameronsjo/bosun")
	assert.Nil(t, installed)
	assert.Equal(t, 2, fixture.source.listCalls)
	assert.Empty(t, fixture.source.downloads)
}

func TestChecksumUpdaterPreservesVersionBehavior(t *testing.T) {
	for _, current := range []string{"1.2.3", "1.2.4"} {
		t.Run("does not replace "+current, func(t *testing.T) {
			fixture := newChecksumFixture(t, "darwin", "arm64", "new executable")
			target := sentinelExecutable(t)
			useUpdateDependencies(t, fixture.client, nil, target, nil)

			release, err := Update(context.Background(), current)

			require.NoError(t, err)
			assert.Nil(t, release)
			assert.Empty(t, fixture.source.downloads)
			assertExecutableContents(t, target, "original executable")
		})
	}

	t.Run("development build remains eligible", func(t *testing.T) {
		fixture := newChecksumFixture(t, "darwin", "arm64", "verified executable")
		target := sentinelExecutable(t)
		useUpdateDependencies(t, fixture.client, nil, target, nil)

		release, err := Update(context.Background(), "dev")

		require.NoError(t, err)
		require.NotNil(t, release)
		assert.Equal(t, "1.2.3", release.Version)
		assertExecutableContents(t, target, "verified executable")
	})
}

func TestChecksumUpdateRetriesOnlyOnNewInvocation(t *testing.T) {
	downloadErr := errors.New("offline")
	fixture := newChecksumFixture(t, "darwin", "arm64", "verified executable")
	fixture.source.downloadErrors[testChecksumID] = downloadErr
	target := sentinelExecutable(t)
	useUpdateDependencies(t, fixture.client, nil, target, nil)

	release, firstErr := Update(context.Background(), "1.2.2")
	require.ErrorIs(t, firstErr, downloadErr)
	assert.Nil(t, release)
	assert.Equal(t, 1, fixture.source.listCalls)
	assert.Len(t, fixture.source.downloads, 2)
	assertExecutableContents(t, target, "original executable")

	delete(fixture.source.downloadErrors, testChecksumID)
	release, secondErr := Update(context.Background(), "1.2.2")

	require.NoError(t, secondErr)
	require.NotNil(t, release)
	assert.Equal(t, 2, fixture.source.listCalls)
	assert.Equal(t, []memoryDownload{
		{releaseID: testReleaseID, assetID: testArchiveID},
		{releaseID: testReleaseID, assetID: testChecksumID},
		{releaseID: testReleaseID, assetID: testArchiveID},
		{releaseID: testReleaseID, assetID: testChecksumID},
	}, fixture.source.downloads)
	assertExecutableContents(t, target, "verified executable")
}
