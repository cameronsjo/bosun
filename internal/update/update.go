// Package update provides self-update functionality for bosun.
package update

import (
	"context"
	"fmt"
	"runtime"

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
}

// CheckForUpdate checks if a newer version is available.
func CheckForUpdate(currentVersion string) (*Release, bool, error) {
	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return nil, false, fmt.Errorf("creating update source: %w", err)
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source: source,
	})
	if err != nil {
		return nil, false, fmt.Errorf("creating updater: %w", err)
	}

	latest, found, err := updater.DetectLatest(context.Background(), selfupdate.NewRepositorySlug(repoOwner, repoName))
	if err != nil {
		return nil, false, fmt.Errorf("detecting latest version: %w", err)
	}
	if !found {
		return nil, false, nil
	}

	// Compare versions
	if latest.LessOrEqual(currentVersion) {
		return nil, false, nil
	}

	return &Release{
		Version:     latest.Version(),
		ReleaseURL:  latest.URL,
		PublishedAt: latest.PublishedAt.Format("2006-01-02"),
		Changelog:   latest.ReleaseNotes,
	}, true, nil
}

// Update downloads and installs the latest version.
func Update(currentVersion string) (*Release, error) {
	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return nil, fmt.Errorf("creating update source: %w", err)
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source: source,
	})
	if err != nil {
		return nil, fmt.Errorf("creating updater: %w", err)
	}

	latest, found, err := updater.DetectLatest(context.Background(), selfupdate.NewRepositorySlug(repoOwner, repoName))
	if err != nil {
		return nil, fmt.Errorf("detecting latest version: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("no releases found for %s/%s", repoOwner, repoName)
	}

	if latest.LessOrEqual(currentVersion) {
		return nil, nil // Already up to date
	}

	release := &Release{
		Version:     latest.Version(),
		ReleaseURL:  latest.URL,
		PublishedAt: latest.PublishedAt.Format("2006-01-02"),
		Changelog:   latest.ReleaseNotes,
	}

	// Get current executable
	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return nil, fmt.Errorf("getting executable path: %w", err)
	}

	if err := updater.UpdateTo(context.Background(), latest, exe); err != nil {
		return nil, fmt.Errorf("updating binary: %w", err)
	}

	return release, nil
}

// GetPlatformInfo returns the current platform information.
func GetPlatformInfo() string {
	return fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
}
