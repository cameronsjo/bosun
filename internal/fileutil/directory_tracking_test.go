package fileutil

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMkdirIfMissingWithOps_PropagatesErrors(t *testing.T) {
	t.Parallel()

	t.Run("mkdir failure", func(t *testing.T) {
		t.Parallel()

		statCalled := false
		created, err := mkdirIfMissingWithOps(
			"destination",
			0755,
			func(string, fs.FileMode) error { return fs.ErrPermission },
			func(string) (fs.FileInfo, error) {
				statCalled = true
				return nil, nil
			},
		)

		require.ErrorIs(t, err, fs.ErrPermission)
		assert.False(t, created)
		assert.False(t, statCalled, "a non-collision mkdir error must return without a stat")
	})

	t.Run("stat failure after collision", func(t *testing.T) {
		t.Parallel()

		created, err := mkdirIfMissingWithOps(
			"destination",
			0755,
			func(string, fs.FileMode) error { return fs.ErrExist },
			func(string) (fs.FileInfo, error) { return nil, fs.ErrPermission },
		)

		require.ErrorIs(t, err, fs.ErrPermission)
		assert.False(t, created)
	})
}
