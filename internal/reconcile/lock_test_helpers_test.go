package reconcile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// privateLockPath gives test reconcilers the same private final-directory
// precondition required by the Unix production lock path. Go 1.26 test temp
// directories may be 0755 on CI, while a real daemon-owned lock directory is
// required to be 0700.
func privateLockPath(t *testing.T, parent, name string) string {
	t.Helper()
	require.NoError(t, os.Chmod(parent, 0o700))
	return filepath.Join(parent, name)
}
