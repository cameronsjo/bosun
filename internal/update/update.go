// Package update provides self-update functionality for bosun.
package update

import (
	"context"
	"fmt"
	"runtime"

	"github.com/Masterminds/semver/v3"
	"github.com/creativeprojects/go-selfupdate"
)

const (
	// Repository owner and name for GitHub releases.
	repoOwner = "cameronsjo"
	repoName  = "bosun"
)

// Release contains information about an available update.
type Release struct {
	Version     string
	ReleaseURL  string
	PublishedAt string
	Changelog   string
	native      *selfupdate.Release
}

type updateClient interface {
	DetectLatest(context.Context) (*Release, bool, error)
	UpdateTo(context.Context, *Release, string) error
}

type selfUpdateClient struct {
	updater *selfupdate.Updater
}

func (c *selfUpdateClient) DetectLatest(ctx context.Context) (*Release, bool, error) {
	latest, found, err := c.updater.DetectLatest(ctx, selfupdate.NewRepositorySlug(repoOwner, repoName))
	if err != nil || !found {
		return nil, found, err
	}

	return &Release{
		Version:     latest.Version(),
		ReleaseURL:  latest.URL,
		PublishedAt: latest.PublishedAt.Format("2006-01-02"),
		Changelog:   latest.ReleaseNotes,
		native:      latest,
	}, true, nil
}

func (c *selfUpdateClient) UpdateTo(ctx context.Context, release *Release, path string) error {
	return c.updater.UpdateTo(ctx, release.native, path)
}

var (
	newUpdateClient = func() (updateClient, error) {
		source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
		if err != nil {
			return nil, fmt.Errorf("creating update source: %w", err)
		}

		updater, err := selfupdate.NewUpdater(selfupdate.Config{Source: source})
		if err != nil {
			return nil, fmt.Errorf("creating updater: %w", err)
		}

		return &selfUpdateClient{updater: updater}, nil
	}
	executablePath = selfupdate.ExecutablePath
)

func updateAvailable(currentVersion, latestVersion string) (bool, error) {
	if currentVersion == "dev" {
		return true, nil
	}

	current, err := semver.NewVersion(currentVersion)
	if err != nil {
		return false, fmt.Errorf("parsing current version %q: %w", currentVersion, err)
	}
	latest, err := semver.NewVersion(latestVersion)
	if err != nil {
		return false, fmt.Errorf("parsing latest version %q: %w", latestVersion, err)
	}

	return latest.GreaterThan(current), nil
}

// CheckForUpdate checks if a newer version is available.
func CheckForUpdate(ctx context.Context, currentVersion string) (*Release, bool, error) {
	updater, err := newUpdateClient()
	if err != nil {
		return nil, false, err
	}

	latest, found, err := updater.DetectLatest(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("detecting latest version: %w", err)
	}
	if !found {
		return nil, false, nil
	}

	available, err := updateAvailable(currentVersion, latest.Version)
	if err != nil {
		return nil, false, err
	}
	if !available {
		return nil, false, nil
	}

	return latest, true, nil
}

// Update downloads and installs the latest version.
func Update(ctx context.Context, currentVersion string) (*Release, error) {
	updater, err := newUpdateClient()
	if err != nil {
		return nil, err
	}

	latest, found, err := updater.DetectLatest(ctx)
	if err != nil {
		return nil, fmt.Errorf("detecting latest version: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("no releases found for %s/%s", repoOwner, repoName)
	}

	available, err := updateAvailable(currentVersion, latest.Version)
	if err != nil {
		return nil, err
	}
	if !available {
		return nil, nil // Already up to date
	}

	// Get current executable
	exe, err := executablePath()
	if err != nil {
		return nil, fmt.Errorf("getting executable path: %w", err)
	}

	if err := updater.UpdateTo(ctx, latest, exe); err != nil {
		return nil, fmt.Errorf("updating binary: %w", err)
	}

	return latest, nil
}

// GetPlatformInfo returns the current platform information.
func GetPlatformInfo() string {
	return fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
}
