package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/creativeprojects/go-selfupdate"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	updatepkg "github.com/cameronsjo/bosun/internal/update"
)

type updateCommandContextKey struct{}

func TestUpdateCommandReturnsCheckFailure(t *testing.T) {
	checkErr := errors.New("release lookup failed")
	ctx := context.WithValue(context.Background(), updateCommandContextKey{}, "check")

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

func TestUpdateCommandRejectsNilContext(t *testing.T) {
	var ctx context.Context
	err := runUpdateCommand(ctx, "1.2.3", true, updateCommandDependencies{
		check: func(context.Context, string) (*updatepkg.Release, bool, error) {
			t.Fatal("check must not run without a context")
			return nil, false, nil
		},
	})

	require.ErrorIs(t, err, updatepkg.ErrNilContext)
}

func TestUpdateCommandPreservesInactiveContextIdentity(t *testing.T) {
	tests := []struct {
		name    string
		context func(t *testing.T) context.Context
		wantErr error
	}{
		{
			name: "canceled",
			context: func(t *testing.T) context.Context {
				t.Helper()
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			wantErr: context.Canceled,
		},
		{
			name: "deadline exceeded",
			context: func(t *testing.T) context.Context {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 0)
				t.Cleanup(cancel)
				return ctx
			},
			wantErr: context.DeadlineExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runUpdateCommand(tt.context(t), "1.2.3", true, updateCommandDependencies{
				check: func(context.Context, string) (*updatepkg.Release, bool, error) {
					t.Fatal("check must not run with an inactive context")
					return nil, false, nil
				},
			})

			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestUpdateCommandStopsWhenCheckCancelsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	err := runUpdateCommand(ctx, "1.2.3", true, updateCommandDependencies{
		check: func(context.Context, string) (*updatepkg.Release, bool, error) {
			cancel()
			return &updatepkg.Release{Version: "1.2.4"}, true, nil
		},
	})

	require.ErrorIs(t, err, context.Canceled)
}

func TestUpdateCommandRejectsMissingReleaseMetadata(t *testing.T) {
	err := runUpdateCommand(context.Background(), "1.2.3", true, updateCommandDependencies{
		check: func(context.Context, string) (*updatepkg.Release, bool, error) {
			return nil, true, nil
		},
	})

	require.ErrorIs(t, err, updatepkg.ErrMissingRelease)
}

func TestUpdateCommandReturnsInstallFailure(t *testing.T) {
	installErr := errors.New("replacement denied")
	ctx := context.WithValue(context.Background(), updateCommandContextKey{}, "install")

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

func TestUpdateCommandReturnsChecksumFailureWithoutSuccess(t *testing.T) {
	archiveName := "bosun_1.2.3_darwin_arm64.tar.gz"
	checksumErr := fmt.Errorf(
		"failed validating asset content %q: %w",
		archiveName,
		selfupdate.ErrChecksumValidationFailed,
	)
	var colorOutput bytes.Buffer
	previousColorOutput := color.Output
	color.Output = &colorOutput
	t.Cleanup(func() {
		color.Output = previousColorOutput
	})
	var runErr error
	stdout := captureStdout(t, func() {
		runErr = runUpdateCommand(context.Background(), "1.2.2", false, updateCommandDependencies{
			install: func(context.Context, string) (*updatepkg.Release, error) {
				return nil, checksumErr
			},
		})
	})

	require.ErrorIs(t, runErr, selfupdate.ErrChecksumValidationFailed)
	assert.Contains(t, runErr.Error(), archiveName)
	assert.NotContains(t, stdout+colorOutput.String(), "Successfully updated")
}

func TestUpdateCommandUsesErrorReturningHandler(t *testing.T) {
	assert.Nil(t, updateCmd.Run)
	assert.NotNil(t, updateCmd.RunE)

	checkErr := errors.New("release lookup failed")
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
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
