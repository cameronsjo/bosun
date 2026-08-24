package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const blockingCommandBody = `printf started > "$BOSUN_TEST_START_MARKER"
exec /bin/sleep 30
`

func installPATHCommand(t *testing.T, name, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("PATH shim tests require a POSIX shell")
	}

	dir := t.TempDir()
	commandPath := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(commandPath, []byte("#!/bin/sh\n"+body), 0755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	marker := filepath.Join(dir, "started")
	t.Setenv("BOSUN_TEST_START_MARKER", marker)
	return marker
}

func waitForCommandStart(t *testing.T, marker string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for subprocess marker %s", marker)
}

func cancelAfterCommandStarts(t *testing.T, marker string, cancel context.CancelFunc, errCh <-chan error) error {
	t.Helper()
	waitForCommandStart(t, marker)
	cancel()
	select {
	case err := <-errCh:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("subprocess did not stop promptly after cancellation")
		return nil
	}
}

func canceledInit(t *testing.T, tool, keyContents string) (string, error) {
	t.Helper()
	marker := installPATHCommand(t, tool, blockingCommandBody)
	targetDir := t.TempDir()
	keyFile := filepath.Join(t.TempDir(), "keys.txt")
	if keyContents != "" {
		require.NoError(t, os.WriteFile(keyFile, []byte(keyContents), 0600))
	}
	t.Setenv("SOPS_AGE_KEY_FILE", keyFile)

	previousYes, previousSystemd, previousDomain := initYes, initSystemd, initDomain
	initYes, initSystemd, initDomain = true, false, ""
	t.Cleanup(func() {
		initYes, initSystemd, initDomain = previousYes, previousSystemd, previousDomain
	})

	ctx, cancel := context.WithCancel(context.Background())
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	errCh := make(chan error, 1)
	go func() {
		errCh <- runInit(cmd, []string{targetDir})
	}()

	err := cancelAfterCommandStarts(t, marker, cancel, errCh)
	assert.NoFileExists(t, filepath.Join(targetDir, "bosun", "docker-compose.yml"))
	return targetDir, err
}

func canceledCheck(t *testing.T, tool string, check func(context.Context) (CheckResult, error)) (CheckResult, error) {
	t.Helper()
	marker := installPATHCommand(t, tool, blockingCommandBody)
	ctx, cancel := context.WithCancel(context.Background())
	type response struct {
		result CheckResult
		err    error
	}
	responseCh := make(chan response, 1)
	go func() {
		result, err := check(ctx)
		responseCh <- response{result: result, err: err}
	}()

	waitForCommandStart(t, marker)
	cancel()
	select {
	case got := <-responseCh:
		return got.result, got.err
	case <-time.After(3 * time.Second):
		t.Fatal("subprocess did not stop promptly after cancellation")
		return CheckResult{}, nil
	}
}

func TestValidateComposeFile_CancelsDockerCompose(t *testing.T) {
	marker := installPATHCommand(t, "docker", blockingCommandBody)
	composeFile := filepath.Join(t.TempDir(), "docker-compose.yml")
	require.NoError(t, os.WriteFile(composeFile, []byte("services: {}\n"), 0644))

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- validateComposeFile(ctx, composeFile)
	}()

	err := cancelAfterCommandStarts(t, marker, cancel, errCh)
	assert.ErrorIs(t, err, context.Canceled)
	assert.NotContains(t, err.Error(), "invalid compose file")
}

func TestRunInit_CancelsGitInit(t *testing.T) {
	key := "# public key: age1existing\n"
	_, err := canceledInit(t, "git", key)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestRunInit_CancelsAgeKeyGeneration(t *testing.T) {
	targetDir, err := canceledInit(t, "age-keygen", "")
	assert.ErrorIs(t, err, context.Canceled)
	assert.NoFileExists(t, filepath.Join(targetDir, ".sops.yaml"), "canceled setup must not write a placeholder key")
}

func TestRunInit_CancelsAgePublicKeyDerivation(t *testing.T) {
	key := "key material without a public-key comment\n"
	targetDir, err := canceledInit(t, "age-keygen", key)
	assert.ErrorIs(t, err, context.Canceled)
	assert.NoFileExists(t, filepath.Join(targetDir, ".sops.yaml"), "canceled setup must not write a placeholder key")
}

func TestCheckDockerCompose_CancelsVersionProbe(t *testing.T) {
	result, err := canceledCheck(t, "docker", checkDockerCompose)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, CheckResult{}, result)
}

func TestCheckSOPS_CancelsVersionProbe(t *testing.T) {
	result, err := canceledCheck(t, "sops", checkSOPS)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, CheckResult{}, result)
}

func TestSubprocessHelpers_RejectCanceledContextBeforeWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	composeFile := filepath.Join(t.TempDir(), "docker-compose.yml")
	require.NoError(t, os.WriteFile(composeFile, []byte("services: {}\n"), 0644))
	keyFile := filepath.Join(t.TempDir(), "keys.txt")
	require.NoError(t, os.WriteFile(keyFile, []byte("# public key: age1existing\n"), 0600))
	t.Setenv("SOPS_AGE_KEY_FILE", keyFile)

	assert.ErrorIs(t, validateComposeFile(ctx, composeFile), context.Canceled)
	_, err := setupAgeKey(ctx)
	assert.ErrorIs(t, err, context.Canceled)
	_, err = extractAgePublicKey(ctx, keyFile)
	assert.ErrorIs(t, err, context.Canceled)
	composeResult, err := checkDockerCompose(ctx)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, CheckResult{}, composeResult)
	sopsResult, err := checkSOPS(ctx)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, CheckResult{}, sopsResult)
}

func TestValidateComposeFile_CommandResults(t *testing.T) {
	composeFile := filepath.Join(t.TempDir(), "docker-compose.yml")
	require.NoError(t, os.WriteFile(composeFile, []byte("services: {}\n"), 0644))

	t.Run("success", func(t *testing.T) {
		installPATHCommand(t, "docker", "exit 0\n")
		assert.NoError(t, validateComposeFile(context.Background(), composeFile))
	})

	t.Run("invalid compose remains actionable", func(t *testing.T) {
		installPATHCommand(t, "docker", "printf 'bad compose' >&2\nexit 1\n")
		err := validateComposeFile(context.Background(), composeFile)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid compose file: bad compose")
		assert.False(t, errors.Is(err, context.Canceled))
	})
}

func TestDoctorSubprocessChecks_CommandResults(t *testing.T) {
	t.Run("docker compose success", func(t *testing.T) {
		installPATHCommand(t, "docker", "printf 'v2.27.0\\n'\n")
		result, err := checkDockerCompose(context.Background())
		require.NoError(t, err)
		assert.Equal(t, CheckResult{Passed: 1}, result)
	})

	t.Run("docker compose failure", func(t *testing.T) {
		installPATHCommand(t, "docker", "exit 1\n")
		result, err := checkDockerCompose(context.Background())
		require.NoError(t, err)
		assert.Equal(t, CheckResult{Failed: 1}, result)
	})

	t.Run("SOPS version success", func(t *testing.T) {
		installPATHCommand(t, "sops", "printf 'sops 3.9.0\\n'\n")
		result, err := checkSOPS(context.Background())
		require.NoError(t, err)
		assert.Equal(t, CheckResult{Passed: 1}, result)
	})

	t.Run("SOPS version failure preserves installed status", func(t *testing.T) {
		installPATHCommand(t, "sops", "exit 1\n")
		result, err := checkSOPS(context.Background())
		require.NoError(t, err)
		assert.Equal(t, CheckResult{Passed: 1}, result)
	})
}

func TestAgeKeyCommands_CommandResults(t *testing.T) {
	t.Run("generation success", func(t *testing.T) {
		installPATHCommand(t, "age-keygen", `printf 'generated key material\n' > "$2"
printf 'Public key: age1generated\n'
`)
		keyFile := filepath.Join(t.TempDir(), "keys.txt")
		t.Setenv("SOPS_AGE_KEY_FILE", keyFile)

		publicKey, err := setupAgeKey(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "age1generated", publicKey)
		info, err := os.Stat(keyFile)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	})

	t.Run("generation failure", func(t *testing.T) {
		installPATHCommand(t, "age-keygen", "exit 1\n")
		keyFile := filepath.Join(t.TempDir(), "keys.txt")
		t.Setenv("SOPS_AGE_KEY_FILE", keyFile)

		_, err := setupAgeKey(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "generate age key")
		assert.False(t, errors.Is(err, context.Canceled))
	})

	t.Run("public key derivation success", func(t *testing.T) {
		installPATHCommand(t, "age-keygen", "printf 'age1derived\\n'\n")
		keyFile := filepath.Join(t.TempDir(), "keys.txt")
		require.NoError(t, os.WriteFile(keyFile, []byte("key material without a public-key comment\n"), 0600))

		publicKey, err := extractAgePublicKey(context.Background(), keyFile)
		require.NoError(t, err)
		assert.Equal(t, "age1derived", publicKey)
	})

	t.Run("public key derivation failure", func(t *testing.T) {
		installPATHCommand(t, "age-keygen", "exit 1\n")
		keyFile := filepath.Join(t.TempDir(), "keys.txt")
		require.NoError(t, os.WriteFile(keyFile, []byte("key material without a public-key comment\n"), 0600))

		_, err := extractAgePublicKey(context.Background(), keyFile)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "could not extract public key")
		assert.False(t, errors.Is(err, context.Canceled))
	})
}
