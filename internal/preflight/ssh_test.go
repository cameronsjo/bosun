package preflight

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// evalDir resolves symlinks on a temp directory so macOS /var → /private/var
// comparisons don't cause false mismatches.
func evalDir(t *testing.T, dir string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	return resolved
}

func TestCheckSSHKeyPermissions_NoKeyFound(t *testing.T) {
	t.Setenv("BOSUN_SSH_KEY", "")
	t.Setenv("HOME", t.TempDir()) // HOME with no .ssh dir → no candidates exist

	result := CheckSSHKeyPermissions()

	assert.Empty(t, result.Path, "no key found → empty path")
	assert.Nil(t, result.Err, "no key found → not an error")
}

func TestCheckSSHKeyPermissions_SafePermissions(t *testing.T) {
	tests := []struct {
		name string
		mode os.FileMode
	}{
		{"0600", 0600},
		{"0400", 0400},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := evalDir(t, t.TempDir())
			keyPath := filepath.Join(dir, "id_ed25519")
			require.NoError(t, os.WriteFile(keyPath, []byte("fake key"), tc.mode))

			t.Setenv("BOSUN_SSH_KEY", keyPath)

			result := CheckSSHKeyPermissions()

			assert.Equal(t, keyPath, result.Path)
			assert.Equal(t, tc.mode, result.Mode)
			assert.Nil(t, result.Err, "safe permissions should not produce an error")
		})
	}
}

func TestCheckSSHKeyPermissions_UnsafePermissions(t *testing.T) {
	tests := []struct {
		name string
		mode os.FileMode
	}{
		{"0644", 0644},
		{"0755", 0755},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := evalDir(t, t.TempDir())
			keyPath := filepath.Join(dir, "deploy-key")
			// Write with a safe mode first, then chmod to the target mode so
			// the umask does not silently reduce the permission bits.
			require.NoError(t, os.WriteFile(keyPath, []byte("fake key"), 0600))
			require.NoError(t, os.Chmod(keyPath, tc.mode))

			t.Setenv("BOSUN_SSH_KEY", keyPath)

			result := CheckSSHKeyPermissions()

			assert.Equal(t, keyPath, result.Path)
			assert.Equal(t, tc.mode, result.Mode)
			assert.NotNil(t, result.Err, "unsafe permissions should produce an error")
			assert.Contains(t, result.Err.Error(), "chmod 600")
		})
	}
}

func TestCheckSSHKeyPermissions_EnvVarTakesPrecedence(t *testing.T) {
	dir := evalDir(t, t.TempDir())
	envKeyPath := filepath.Join(dir, "custom-key")
	require.NoError(t, os.WriteFile(envKeyPath, []byte("fake key"), 0600))

	// Also place a key in HOME/.ssh — should NOT be selected.
	home := evalDir(t, t.TempDir())
	sshDir := filepath.Join(home, ".ssh")
	require.NoError(t, os.MkdirAll(sshDir, 0700))
	homeKeyPath := filepath.Join(sshDir, "id_ed25519")
	require.NoError(t, os.WriteFile(homeKeyPath, []byte("fake key"), 0600))

	t.Setenv("BOSUN_SSH_KEY", envKeyPath)
	t.Setenv("HOME", home)

	result := CheckSSHKeyPermissions()

	assert.Equal(t, envKeyPath, result.Path, "BOSUN_SSH_KEY should take precedence over HOME/.ssh/id_ed25519")
}

func TestCheckSSHKeyPermissions_FallsBackToHomeSSH(t *testing.T) {
	home := evalDir(t, t.TempDir())
	sshDir := filepath.Join(home, ".ssh")
	require.NoError(t, os.MkdirAll(sshDir, 0700))
	keyPath := filepath.Join(sshDir, "id_ed25519")
	require.NoError(t, os.WriteFile(keyPath, []byte("fake key"), 0600))

	t.Setenv("BOSUN_SSH_KEY", "")
	t.Setenv("HOME", home)

	result := CheckSSHKeyPermissions()

	assert.Equal(t, keyPath, result.Path)
	assert.Nil(t, result.Err)
}

func TestSSHKeyCandidates_EmptyEnvVar(t *testing.T) {
	t.Setenv("BOSUN_SSH_KEY", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	candidates := sshKeyCandidates()

	for _, c := range candidates {
		assert.NotEmpty(t, c, "no candidate path should be an empty string")
	}
	// Should include the fixed paths and home-relative paths, but NOT empty strings.
	assert.Contains(t, candidates, "/config/deploy-key")
	assert.Contains(t, candidates, "/config/ssh-key")
	assert.Contains(t, candidates, filepath.Join(home, ".ssh", "id_ed25519"))
	assert.Contains(t, candidates, filepath.Join(home, ".ssh", "id_rsa"))
}
