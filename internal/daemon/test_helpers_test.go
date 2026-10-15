package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// evalSymlinks resolves symlinks for path comparison (macOS /var -> /private/var).
func evalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

// privateLockPath prepares the fixture directory for the Unix lock-parent
// checks and preserves the caller's requested lock basename. Go 1.26 test
// temp directories may be 0755, while a daemon-owned final lock directory
// must be owner-only.
func privateLockPath(t *testing.T, parent, name string) string {
	t.Helper()
	require.NoError(t, os.Chmod(parent, 0o700))
	return filepath.Join(parent, name)
}
