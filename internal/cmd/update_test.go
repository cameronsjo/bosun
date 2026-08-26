package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	updatepkg "github.com/cameronsjo/bosun/internal/update"
)

func TestUpdateCommandReturnsCheckFailure(t *testing.T) {
	checkErr := errors.New("release lookup failed")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runUpdateCommand(ctx, "1.2.3", true, updateCommandDependencies{
		check: func(gotCtx context.Context, currentVersion string) (*updatepkg.Release, bool, error) {
			assert.Same(t, ctx, gotCtx)
			assert.Equal(t, "1.2.3", currentVersion)
			return nil, false, checkErr
		},
		install: func(context.Context, string) (*updatepkg.Release, error) {
			t.Fatal("install must not run in check-only mode")
			return nil, nil
		},
	})

	require.ErrorIs(t, err, checkErr)
	assert.Contains(t, err.Error(), "check for updates")
}

func TestUpdateCommandReturnsInstallFailure(t *testing.T) {
	installErr := errors.New("replacement denied")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runUpdateCommand(ctx, "1.2.3", false, updateCommandDependencies{
		check: func(context.Context, string) (*updatepkg.Release, bool, error) {
			t.Fatal("check dependency must not run in install mode")
			return nil, false, nil
		},
		install: func(gotCtx context.Context, currentVersion string) (*updatepkg.Release, error) {
			assert.Same(t, ctx, gotCtx)
			assert.Equal(t, "1.2.3", currentVersion)
			return nil, installErr
		},
	})

	require.ErrorIs(t, err, installErr)
	assert.Contains(t, err.Error(), "update bosun")
}

func TestUpdateCommandUsesErrorReturningHandler(t *testing.T) {
	assert.Nil(t, updateCmd.Run)
	assert.NotNil(t, updateCmd.RunE)

	checkErr := errors.New("release lookup failed")
	cmd := &cobra.Command{}
	err := runUpdateWithDependencies(cmd, "1.2.3", true, updateCommandDependencies{
		check: func(context.Context, string) (*updatepkg.Release, bool, error) {
			return nil, false, checkErr
		},
	})

	require.ErrorIs(t, err, checkErr)
	assert.True(t, cmd.SilenceUsage)
}

func TestUpdateCommandPreservesSuccessResults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	unavailableCheck := func(gotCtx context.Context, currentVersion string) (*updatepkg.Release, bool, error) {
		assert.Same(t, ctx, gotCtx)
		assert.Equal(t, "1.2.3", currentVersion)
		return nil, false, nil
	}
	availableRelease := &updatepkg.Release{
		Version:     "1.2.4",
		PublishedAt: "2026-08-26",
		Changelog:   strings.Repeat("change\n", 11),
	}
	availableCheck := func(context.Context, string) (*updatepkg.Release, bool, error) {
		return availableRelease, true, nil
	}
	currentInstall := func(context.Context, string) (*updatepkg.Release, error) {
		return nil, nil
	}
	updatedInstall := func(context.Context, string) (*updatepkg.Release, error) {
		return availableRelease, nil
	}
	tests := []struct {
		name      string
		onlyCheck bool
		deps      updateCommandDependencies
	}{
		{name: "no update available", onlyCheck: true, deps: updateCommandDependencies{check: unavailableCheck}},
		{name: "update available", onlyCheck: true, deps: updateCommandDependencies{check: availableCheck}},
		{name: "already current", deps: updateCommandDependencies{install: currentInstall}},
		{name: "updated", deps: updateCommandDependencies{install: updatedInstall}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var runErr error
			output := captureStdout(t, func() {
				runErr = runUpdateCommand(ctx, "1.2.3", tt.onlyCheck, tt.deps)
			})

			require.NoError(t, runErr)
			if tt.name == "update available" || tt.name == "updated" {
				assert.Contains(t, output, "... (2 more lines)")
			}
		})
	}
}
