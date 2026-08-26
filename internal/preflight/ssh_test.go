package preflight

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
	missingPath := filepath.Join(t.TempDir(), "missing")

	result := checkSSHKeyPermissions("", []string{missingPath}, runtime.GOOS, os.Stat)

	assert.Empty(t, result.Path, "no key found → empty path")
	assert.Nil(t, result.Err, "no key found → not an error")
}

func TestCheckSSHKeyPermissions_MetadataError(t *testing.T) {
	dir := evalDir(t, t.TempDir())
	parentFile := filepath.Join(dir, "not-a-directory")
	require.NoError(t, os.WriteFile(parentFile, []byte("file"), 0600))
	keyPath := filepath.Join(parentFile, "deploy-key")
	t.Setenv("BOSUN_SSH_KEY", keyPath)
	t.Setenv("HOME", t.TempDir())

	result := CheckSSHKeyPermissions()

	assert.Equal(t, keyPath, result.Path)
	assert.Zero(t, result.Mode)
	require.Error(t, result.Err)
	assert.Contains(t, result.Err.Error(), "reading SSH key metadata")
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
			assert.True(t, result.PermissionsChecked)
			assert.Nil(t, result.Err, "safe permissions should not produce an error")
		})
	}
}

func TestCheckSSHKeyPermissions_RejectsUnusablePaths(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, path string)
		wantErr string
	}{
		{
			name: "directory",
			prepare: func(t *testing.T, path string) {
				t.Helper()
				require.NoError(t, os.Mkdir(path, 0700))
			},
			wantErr: "not a regular file",
		},
		{
			name: "empty file",
			prepare: func(t *testing.T, path string) {
				t.Helper()
				require.NoError(t, os.WriteFile(path, nil, 0600))
			},
			wantErr: "is empty",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			keyPath := filepath.Join(evalDir(t, t.TempDir()), "deploy-key")
			tc.prepare(t, keyPath)
			t.Setenv("BOSUN_SSH_KEY", keyPath)

			result := CheckSSHKeyPermissions()

			assert.Equal(t, keyPath, result.Path)
			assert.False(t, result.PermissionsChecked)
			require.Error(t, result.Err)
			assert.Contains(t, result.Err.Error(), tc.wantErr)
		})
	}
}

func TestCheckSSHKeyPermissions_WindowsSkipsPOSIXModeCheck(t *testing.T) {
	dir := evalDir(t, t.TempDir())
	keyPath := filepath.Join(dir, "deploy-key")
	require.NoError(t, os.WriteFile(keyPath, []byte("fake key"), 0600))
	require.NoError(t, os.Chmod(keyPath, 0644))

	result := checkSSHKeyPermissions(keyPath, nil, "windows", os.Stat)

	assert.Equal(t, keyPath, result.Path)
	assert.Equal(t, os.FileMode(0644), result.Mode)
	assert.False(t, result.PermissionsChecked)
	assert.NoError(t, result.Err)
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

	result := checkSSHKeyPermissions("", []string{keyPath}, runtime.GOOS, os.Stat)

	assert.Equal(t, keyPath, result.Path)
	assert.Nil(t, result.Err)
}

func TestSSHKeyCandidates(t *testing.T) {
	home := t.TempDir()

	candidates := sshKeyCandidates(home)

	assert.Equal(t, []string{
		"/config/deploy-key",
		"/config/ssh-key",
		filepath.Join(home, ".ssh", "id_ed25519"),
		filepath.Join(home, ".ssh", "id_rsa"),
	}, candidates)
}

func TestSSHKeyCandidates_EmptyHomeMatchesRuntime(t *testing.T) {
	assert.Equal(t, []string{
		"/config/deploy-key",
		"/config/ssh-key",
		filepath.Join(".ssh", "id_ed25519"),
		filepath.Join(".ssh", "id_rsa"),
	}, sshKeyCandidates(""))
}

func TestCheckSSHKeyPermissions_ExplicitMissingDoesNotFallBack(t *testing.T) {
	dir := evalDir(t, t.TempDir())
	missingPath := filepath.Join(dir, "missing")
	fallbackPath := filepath.Join(dir, "fallback")
	require.NoError(t, os.WriteFile(fallbackPath, []byte("fake key"), 0600))

	result := checkSSHKeyPermissions(missingPath, []string{fallbackPath}, runtime.GOOS, os.Stat)

	assert.Equal(t, missingPath, result.Path)
	require.Error(t, result.Err)
	assert.ErrorIs(t, result.Err, os.ErrNotExist)
}

func TestCheckSSHKeyPermissions_ConventionalFallback(t *testing.T) {
	dir := evalDir(t, t.TempDir())
	unusablePath := filepath.Join(dir, "unusable")
	usablePath := filepath.Join(dir, "usable")
	require.NoError(t, os.Mkdir(unusablePath, 0700))
	require.NoError(t, os.WriteFile(usablePath, []byte("fake key"), 0600))

	result := checkSSHKeyPermissions("", []string{unusablePath, usablePath}, runtime.GOOS, os.Stat)

	assert.Equal(t, usablePath, result.Path)
	assert.NoError(t, result.Err)
}

func TestCheckSSHKeyPermissions_ReportsFirstUnusableConventionalCandidate(t *testing.T) {
	dir := evalDir(t, t.TempDir())
	firstPath := filepath.Join(dir, "first")
	secondPath := filepath.Join(dir, "second")
	require.NoError(t, os.Mkdir(firstPath, 0700))
	require.NoError(t, os.WriteFile(secondPath, nil, 0600))

	result := checkSSHKeyPermissions("", []string{firstPath, secondPath}, runtime.GOOS, os.Stat)

	assert.Equal(t, firstPath, result.Path)
	require.Error(t, result.Err)
	assert.Contains(t, result.Err.Error(), "not a regular file")
}

func TestCheckSSHKeyPermissions_FollowsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires privileges on Windows")
	}

	dir := evalDir(t, t.TempDir())
	targetPath := filepath.Join(dir, "target")
	symlinkPath := filepath.Join(dir, "key")
	require.NoError(t, os.WriteFile(targetPath, []byte("fake key"), 0600))
	require.NoError(t, os.Symlink(targetPath, symlinkPath))

	result := checkSSHKeyPermissions(symlinkPath, nil, runtime.GOOS, os.Stat)

	assert.Equal(t, symlinkPath, result.Path)
	assert.NoError(t, result.Err)
}

func TestCheckSSHKeyPermissions_RejectsSymlinkToDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires privileges on Windows")
	}

	dir := evalDir(t, t.TempDir())
	targetPath := filepath.Join(dir, "target")
	symlinkPath := filepath.Join(dir, "key")
	require.NoError(t, os.Mkdir(targetPath, 0700))
	require.NoError(t, os.Symlink(targetPath, symlinkPath))

	result := checkSSHKeyPermissions(symlinkPath, nil, runtime.GOOS, os.Stat)

	assert.Equal(t, symlinkPath, result.Path)
	require.Error(t, result.Err)
	assert.Contains(t, result.Err.Error(), "not a regular file")
}

func TestCheckSSHKeyPermissions_RejectsBrokenExplicitSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires privileges on Windows")
	}

	dir := evalDir(t, t.TempDir())
	symlinkPath := filepath.Join(dir, "key")
	require.NoError(t, os.Symlink(filepath.Join(dir, "missing"), symlinkPath))

	result := checkSSHKeyPermissions(symlinkPath, nil, runtime.GOOS, os.Stat)

	assert.Equal(t, symlinkPath, result.Path)
	require.Error(t, result.Err)
	assert.ErrorIs(t, result.Err, os.ErrNotExist)
}

func TestCheckSSHKeyPermissions_PreservesMetadataErrorIdentity(t *testing.T) {
	sentinel := errors.New("metadata unavailable")
	stat := func(string) (os.FileInfo, error) { return nil, sentinel }

	result := checkSSHKeyPermissions("key", nil, runtime.GOOS, stat)

	require.Error(t, result.Err)
	assert.ErrorIs(t, result.Err, sentinel)
}
