package reconcile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cameronsjo/bosun/internal/docker"
	bosunlog "github.com/cameronsjo/bosun/internal/log"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateStagingEvidenceTargets(t *testing.T) {
	baseDir := evalSymlinks(t, t.TempDir())
	base := &Config{StagingDir: filepath.Join(baseDir, "staging")}

	t.Run("accepts disjoint target slots", func(t *testing.T) {
		targets := []Target{
			{Name: "unraid", StagingDir: filepath.Join(baseDir, "unraid")},
			{Name: "pi", StagingDir: filepath.Join(baseDir, "pi")},
		}
		require.NoError(t, ValidateStagingEvidenceTargets(base, targets))
	})

	for _, tc := range []struct {
		name string
		a    string
		b    string
	}{
		{name: "equal", a: filepath.Join(baseDir, "shared"), b: filepath.Join(baseDir, "shared")},
		{name: "nested", a: filepath.Join(baseDir, "parent"), b: filepath.Join(baseDir, "parent", "child")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateStagingEvidenceTargets(base, []Target{
				{Name: "unraid", StagingDir: tc.a},
				{Name: "pi", StagingDir: tc.b},
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "equal or nested staging paths")
		})
	}

	t.Run("resolves symlinked ancestors before comparing", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation needs elevated privileges on Windows")
		}
		realParent := filepath.Join(baseDir, "real")
		require.NoError(t, os.MkdirAll(realParent, 0o755))
		aliasParent := filepath.Join(baseDir, "alias")
		require.NoError(t, os.Symlink(realParent, aliasParent))

		err := ValidateStagingEvidenceTargets(base, []Target{
			{Name: "unraid", StagingDir: filepath.Join(realParent, "slot")},
			{Name: "pi", StagingDir: filepath.Join(aliasParent, "slot")},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "equal or nested staging paths")
		assert.NotContains(t, err.Error(), realParent, "canonical symlink target must not be disclosed")
	})
}

func TestPreflightStagingEvidenceSecuresEverySlot(t *testing.T) {
	baseDir := evalSymlinks(t, t.TempDir())
	targets := []Target{
		{Name: "unraid", StagingDir: filepath.Join(baseDir, "unraid")},
		{Name: "pi", StagingDir: filepath.Join(baseDir, "pi")},
	}
	for _, target := range targets {
		dir := filepath.Join(target.StagingDir, "nested")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "secret.txt"), []byte(target.Name+"-secret"), 0o644))
	}

	require.NoError(t, PreflightStagingEvidence(context.Background(), &Config{StagingDir: filepath.Join(baseDir, "base")}, targets))
	for _, target := range targets {
		assertPrivateStagingTree(t, target.StagingDir)
		content, err := os.ReadFile(filepath.Join(target.StagingDir, "nested", "secret.txt"))
		require.NoError(t, err)
		assert.Equal(t, target.Name+"-secret", string(content))
	}
}

func TestPreflightRejectsOverlappingSlotsBeforeMutation(t *testing.T) {
	baseDir := evalSymlinks(t, t.TempDir())
	parent := filepath.Join(baseDir, "shared")
	child := filepath.Join(parent, "child")
	require.NoError(t, os.MkdirAll(child, 0o755))
	file := filepath.Join(child, "evidence")
	require.NoError(t, os.WriteFile(file, []byte("unchanged"), 0o644))
	targets := []Target{
		{Name: "unraid", StagingDir: parent},
		{Name: "pi", StagingDir: child},
	}

	err := PreflightStagingEvidence(context.Background(), &Config{StagingDir: filepath.Join(baseDir, "base")}, targets)
	require.Error(t, err)
	parentInfo, statErr := os.Stat(parent)
	require.NoError(t, statErr)
	fileInfo, statErr := os.Stat(file)
	require.NoError(t, statErr)
	assert.Equal(t, fs.FileMode(0o755), parentInfo.Mode().Perm(), "invalid set must not harden either slot")
	assert.Equal(t, fs.FileMode(0o644), fileInfo.Mode().Perm(), "invalid set must not mutate evidence")
}

func TestProtectOrDeleteStagingRejectsSymlinkWithoutDisclosure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privileges on Windows")
	}
	baseDir := evalSymlinks(t, t.TempDir())
	root := filepath.Join(baseDir, "staging")
	external := filepath.Join(baseDir, "do-not-log-super-secret")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(external, []byte("credential-value-must-not-log"), 0o644))
	require.NoError(t, os.Symlink(external, filepath.Join(root, "link")))

	var logs bytes.Buffer
	logger := zerolog.New(&logs)
	ctx := bosunlog.WithContext(context.Background(), &logger)
	outcome, err := protectOrDeleteStaging(ctx, "unraid", root, defaultStagingEvidenceOps(), "test")
	require.NoError(t, err)
	assert.Equal(t, "discarded", outcome)
	assert.NoDirExists(t, root)
	content, readErr := os.ReadFile(external)
	require.NoError(t, readErr)
	assert.Equal(t, "credential-value-must-not-log", string(content))
	assert.NotContains(t, logs.String(), "credential-value-must-not-log")
	assert.NotContains(t, logs.String(), external)
	assert.Contains(t, logs.String(), `"staging_evidence_outcome":"discarded"`)
	assert.Contains(t, logs.String(), `"target":"unraid"`)
	assert.Contains(t, logs.String(), root)
}

func TestProtectOrDeleteStagingRejectsSymlinkRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privileges on Windows")
	}
	baseDir := evalSymlinks(t, t.TempDir())
	external := filepath.Join(baseDir, "external")
	root := filepath.Join(baseDir, "staging")
	require.NoError(t, os.MkdirAll(external, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(external, "keep"), []byte("external"), 0o644))
	require.NoError(t, os.Symlink(external, root))

	outcome, err := protectOrDeleteStaging(context.Background(), "unraid", root, defaultStagingEvidenceOps(), "test")
	require.NoError(t, err)
	assert.Equal(t, "discarded", outcome)
	assert.NoFileExists(t, root)
	assert.Equal(t, "external", read(t, filepath.Join(external, "keep")))
}

func TestProtectOrDeleteStagingFailsClosedOnReplacementRace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privileges on Windows")
	}
	baseDir := evalSymlinks(t, t.TempDir())
	root := filepath.Join(baseDir, "staging")
	file := filepath.Join(root, "secret.txt")
	external := filepath.Join(baseDir, "external.txt")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(file, []byte("staged"), 0o644))
	require.NoError(t, os.WriteFile(external, []byte("external"), 0o644))

	ops := defaultStagingEvidenceOps()
	realChmod := ops.chmod
	replaced := false
	ops.chmod = func(pinned *os.File, mode fs.FileMode) error {
		if filepath.Base(pinned.Name()) == "secret.txt" && !replaced {
			replaced = true
			require.NoError(t, os.Remove(file))
			require.NoError(t, os.Symlink(external, file))
		}
		return realChmod(pinned, mode)
	}

	outcome, err := protectOrDeleteStaging(context.Background(), "unraid", root, ops, "test")
	require.NoError(t, err)
	assert.True(t, replaced)
	assert.Equal(t, "discarded", outcome)
	assert.NoDirExists(t, root)
	info, statErr := os.Stat(external)
	require.NoError(t, statErr)
	assert.Equal(t, fs.FileMode(0o644), info.Mode().Perm(), "external symlink target must not be chmodded")
}

func TestProtectOrDeleteStagingSurfacesHardenAndDeleteFailure(t *testing.T) {
	root := filepath.Join(evalSymlinks(t, t.TempDir()), "staging")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "secret.txt"), []byte("secret"), 0o644))

	ops := defaultStagingEvidenceOps()
	ops.chmod = func(*os.File, fs.FileMode) error { return errors.New("chmod injected") }
	ops.removeAll = func(string) error { return errors.New("remove injected") }
	outcome, err := protectOrDeleteStaging(context.Background(), "unraid", root, ops, "test")
	require.Error(t, err)
	assert.Equal(t, "unsafe", outcome)
	assert.ErrorContains(t, err, "chmod injected")
	assert.ErrorContains(t, err, "remove injected")
	assert.DirExists(t, root)
}

func TestRenderTemplatesPrivateRootPreservesPayloadModes(t *testing.T) {
	baseDir := evalSymlinks(t, t.TempDir())
	repoDir := filepath.Join(baseDir, "repo")
	stagingDir := filepath.Join(baseDir, "staging")
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "config.yml.tmpl"), []byte("token: {{ .token }}"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "bin", "entrypoint"), []byte("#!/bin/sh\n"), 0o755))

	r := NewReconciler(&Config{RepoDir: repoDir, StagingDir: stagingDir, InfraSubDir: "."})
	require.NoError(t, r.renderTemplates(context.Background(), map[string]any{"token": "secret"}))

	rootInfo, err := os.Stat(stagingDir)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o700), rootInfo.Mode().Perm())
	templateInfo, err := os.Stat(filepath.Join(stagingDir, "config.yml"))
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o644), templateInfo.Mode().Perm())
	execInfo, err := os.Stat(filepath.Join(stagingDir, "bin", "entrypoint"))
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o755), execInfo.Mode().Perm(), "active staging must preserve destination executable mode")
}

func TestStagingEvidenceStructuredOutcomesDoNotLogContent(t *testing.T) {
	baseDir := evalSymlinks(t, t.TempDir())
	repoDir := filepath.Join(baseDir, "repo")
	stagingDir := filepath.Join(baseDir, "staging")
	require.NoError(t, os.MkdirAll(repoDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "secret.yml.tmpl"), []byte("secret: {{ .secret }}"), 0o644))

	var logs bytes.Buffer
	logger := zerolog.New(&logs)
	ctx := bosunlog.WithContext(context.Background(), &logger)
	r := NewReconciler(&Config{TargetName: "unraid", RepoDir: repoDir, StagingDir: stagingDir, InfraSubDir: "."})
	require.NoError(t, r.renderTemplates(ctx, map[string]any{"secret": "rendered-value-must-not-log"}))
	outcome, err := protectOrDeleteStaging(ctx, "unraid", stagingDir, defaultStagingEvidenceOps(), "cleanup_failure")
	require.NoError(t, err)
	assert.Equal(t, "retained", outcome)

	output := logs.String()
	assert.Contains(t, output, `"staging_evidence_outcome":"replaced"`)
	assert.Contains(t, output, `"staging_evidence_outcome":"retained"`)
	assert.Contains(t, output, `"reason":"cleanup_failure"`)
	assert.Contains(t, output, `"target":"unraid"`)
	assert.Contains(t, output, stagingDir)
	assert.NotContains(t, output, "rendered-value-must-not-log")
}

func TestRenderTemplatesReplacesOnlyCurrentEvidenceSlot(t *testing.T) {
	baseDir := evalSymlinks(t, t.TempDir())
	repoDir := filepath.Join(baseDir, "repo")
	current := filepath.Join(baseDir, "staging", "unraid")
	sibling := filepath.Join(baseDir, "staging", "pi")
	require.NoError(t, os.MkdirAll(repoDir, 0o755))
	require.NoError(t, os.MkdirAll(current, 0o700))
	require.NoError(t, os.MkdirAll(sibling, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(current, "prior"), []byte("old"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(sibling, "prior"), []byte("sibling"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "current.yml.tmpl"), []byte("value: {{ .value }}"), 0o644))

	r := NewReconciler(&Config{TargetName: "unraid", RepoDir: repoDir, StagingDir: current, InfraSubDir: "."})
	require.NoError(t, r.renderTemplates(context.Background(), map[string]any{"value": "new"}))
	assert.NoFileExists(t, filepath.Join(current, "prior"))
	assert.Equal(t, "value: new", read(t, filepath.Join(current, "current.yml")))
	assert.Equal(t, "sibling", read(t, filepath.Join(sibling, "prior")))
}

func TestRenderTemplatesFailedReplacementWritesNoNewOutput(t *testing.T) {
	baseDir := evalSymlinks(t, t.TempDir())
	repoDir := filepath.Join(baseDir, "repo")
	stagingDir := filepath.Join(baseDir, "staging")
	require.NoError(t, os.MkdirAll(repoDir, 0o755))
	require.NoError(t, os.MkdirAll(stagingDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(stagingDir, "prior"), []byte("prior"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "new.yml.tmpl"), []byte("secret: {{ .secret }}"), 0o644))

	r := NewReconciler(&Config{RepoDir: repoDir, StagingDir: stagingDir, InfraSubDir: "."})
	r.stagingOps.removeAll = func(string) error { return errors.New("replacement remove injected") }
	err := r.renderTemplates(context.Background(), map[string]any{"secret": "must-not-write"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to clear staging directory")
	assert.Equal(t, "prior", read(t, filepath.Join(stagingDir, "prior")))
	assert.NoFileExists(t, filepath.Join(stagingDir, "new.yml"))
}

func TestRunRetainsPrivatePartialRenderEvidence(t *testing.T) {
	baseDir := evalSymlinks(t, t.TempDir())
	repoDir := filepath.Join(baseDir, "repo")
	stagingDir := filepath.Join(baseDir, "staging")
	appdataDir := filepath.Join(baseDir, "appdata")
	require.NoError(t, os.MkdirAll(repoDir, 0o755))
	require.NoError(t, os.MkdirAll(appdataDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "diagnostic.txt"), []byte("rendered-secret-evidence"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "broken.yml.tmpl"), []byte("{{ .missing }}"), 0o644))

	cfg := &Config{
		DryRun:           true,
		LockFile:         filepath.Join(baseDir, "reconcile.lock"),
		StateFile:        filepath.Join(baseDir, "state.json"),
		RepoDir:          repoDir,
		StagingDir:       stagingDir,
		LocalAppdataPath: appdataDir,
		InfraSubDir:      ".",
	}
	r := NewReconciler(cfg, WithGitOperations(&mockGitOps{syncChanged: true, syncBefore: "aaa", syncAfter: "bbb"}))
	err := r.Run(context.Background())
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to render templates")
	assertPrivateStagingTree(t, stagingDir)
	content, readErr := os.ReadFile(filepath.Join(stagingDir, "diagnostic.txt"))
	require.NoError(t, readErr)
	assert.Equal(t, "rendered-secret-evidence", string(content))
}

func TestRunFinalizesStagingBeforeReleasingTargetLock(t *testing.T) {
	baseDir := evalSymlinks(t, t.TempDir())
	repoDir := filepath.Join(baseDir, "repo")
	stagingDir := filepath.Join(baseDir, "staging")
	lockFile := filepath.Join(baseDir, "reconcile.lock")
	require.NoError(t, os.MkdirAll(repoDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "broken.yml.tmpl"), []byte("{{ .missing }}"), 0o644))

	cfg := &Config{
		DryRun:      true,
		LockFile:    lockFile,
		StateFile:   filepath.Join(baseDir, "state.json"),
		RepoDir:     repoDir,
		StagingDir:  stagingDir,
		DeployMode:  "local",
		InfraSubDir: ".",
	}
	r := NewReconciler(cfg, WithGitOperations(&mockGitOps{syncChanged: true, syncBefore: "aaa", syncAfter: "bbb"}))
	r.stagingOps = defaultStagingEvidenceOps()
	realChmod := r.stagingOps.chmod
	checkedWhileFinalizing := false
	r.stagingOps.chmod = func(file *os.File, mode fs.FileMode) error {
		if !checkedWhileFinalizing {
			checkedWhileFinalizing = true
			competitor := NewReconciler(&Config{LockFile: lockFile})
			require.Error(t, competitor.acquireLock(), "target lock must remain held while evidence is finalized")
		}
		return realChmod(file, mode)
	}

	require.Error(t, r.Run(context.Background()))
	assert.True(t, checkedWhileFinalizing)
	competitor := NewReconciler(&Config{LockFile: lockFile})
	require.NoError(t, competitor.acquireLock(), "target lock must release after evidence finalization")
	competitor.releaseLock()
}

func TestRunEarlyFailurePreservesPreflightedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*Config) []ReconcilerOption
		want    string
	}{
		{
			name: "git sync",
			prepare: func(*Config) []ReconcilerOption {
				return []ReconcilerOption{WithGitOperations(&mockGitOps{syncErr: errors.New("sync injected")})}
			},
			want: "failed to sync repository",
		},
		{
			name: "config reload",
			prepare: func(cfg *Config) []ReconcilerOption {
				cfg.ConfigReloader = func(string) (*ReloadedConfig, error) {
					return nil, fmt.Errorf("%w: reload injected", ErrInvalidPostSyncHooks)
				}
				return []ReconcilerOption{WithGitOperations(&mockGitOps{syncAfter: "bbb"})}
			},
			want: "invalid reloaded project configuration",
		},
		{
			name: "SOPS decryption",
			prepare: func(cfg *Config) []ReconcilerOption {
				cfg.SecretsFiles = []string{filepath.Join(filepath.Dir(cfg.StagingDir), "secrets.yaml")}
				return []ReconcilerOption{
					WithGitOperations(&mockGitOps{syncAfter: "bbb"}),
					WithSecretsDecryptor(&mockSecretsDecryptor{decryptErr: errors.New("decrypt injected")}),
				}
			},
			want: "failed to decrypt secrets",
		},
		{
			name: "deploy mode resolution",
			prepare: func(cfg *Config) []ReconcilerOption {
				cfg.LocalAppdataPath = filepath.Join(cfg.StagingDir, "missing-appdata")
				return []ReconcilerOption{WithGitOperations(&mockGitOps{syncAfter: "bbb"})}
			},
			want: "failed to resolve deploy mode",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseDir := evalSymlinks(t, t.TempDir())
			stagingDir := filepath.Join(baseDir, "staging")
			evidence := filepath.Join(stagingDir, "diagnostic.txt")
			require.NoError(t, os.MkdirAll(stagingDir, 0o755))
			require.NoError(t, os.WriteFile(evidence, []byte("prior-render"), 0o644))
			cfg := &Config{
				LockFile:   filepath.Join(baseDir, "reconcile.lock"),
				StateFile:  filepath.Join(baseDir, "state.json"),
				RepoDir:    baseDir,
				StagingDir: stagingDir,
			}
			require.NoError(t, os.WriteFile(filepath.Join(baseDir, "secrets.yaml"), []byte("encrypted fixture"), 0o600))
			require.NoError(t, PreflightStagingEvidence(context.Background(), cfg, []Target{{Name: DefaultTargetName}}))
			before, err := os.ReadFile(evidence)
			require.NoError(t, err)

			r := NewReconciler(cfg, tc.prepare(cfg)...)
			err = r.Run(context.Background())
			require.Error(t, err)
			assert.ErrorContains(t, err, tc.want)
			after, readErr := os.ReadFile(evidence)
			require.NoError(t, readErr)
			assert.Equal(t, before, after)
			assertPrivateStagingTree(t, stagingDir)
		})
	}
}

func TestRunPanicAfterRenderFinalizesEvidence(t *testing.T) {
	baseDir := evalSymlinks(t, t.TempDir())
	repoDir := filepath.Join(baseDir, "repo")
	stagingDir := filepath.Join(baseDir, "staging")
	appdataDir := filepath.Join(baseDir, "appdata")
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "appdata", "svc"), 0o755))
	require.NoError(t, os.MkdirAll(appdataDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "appdata", "svc", "config.yml"), []byte("secret: value"), 0o644))

	cfg := &Config{
		DeployMode:              "local",
		LockFile:                filepath.Join(baseDir, "reconcile.lock"),
		StateFile:               filepath.Join(baseDir, "state.json"),
		RepoDir:                 repoDir,
		StagingDir:              stagingDir,
		LocalAppdataPath:        appdataDir,
		BackupDir:               filepath.Join(baseDir, "backups"),
		InfraSubDir:             ".",
		AllowEmptyDeclaredState: false,
	}
	seedStubComposeService(t, cfg)
	r := NewReconciler(cfg,
		WithGitOperations(&mockGitOps{syncChanged: true, syncBefore: "aaa", syncAfter: "bbb"}),
		WithDockerClientFunc(func() *docker.Client { panic("post-render injected panic") }),
	)
	err := r.Run(context.Background())
	require.Error(t, err)
	assert.ErrorContains(t, err, "post-render injected panic")
	assertPrivateStagingTree(t, stagingDir)
}

func TestCleanupStagingFallsBackToPrivateRetention(t *testing.T) {
	root := filepath.Join(evalSymlinks(t, t.TempDir()), "staging")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "nested", "secret"), []byte("secret"), 0o644))

	r := NewReconciler(&Config{StagingDir: root})
	r.stagingOps = defaultStagingEvidenceOps()
	r.stagingOps.removeAll = func(string) error { return errors.New("remove injected") }
	require.NoError(t, r.cleanupStaging(), "secured cleanup fallback remains non-fatal")
	assertPrivateStagingTree(t, root)

	r.stagingOps.chmod = func(*os.File, fs.FileMode) error { return errors.New("chmod injected") }
	err := r.cleanupStaging()
	require.Error(t, err)
	assert.ErrorContains(t, err, "remove injected")
	assert.ErrorContains(t, err, "chmod injected")
}

func TestRunCleanupFallbackControlsSuccessfulState(t *testing.T) {
	for _, tc := range []struct {
		name       string
		failHarden bool
		wantErr    bool
		wantCommit string
	}{
		{name: "owner-only fallback records success", wantCommit: "bbb"},
		{name: "harden and delete failure blocks success", failHarden: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseDir := evalSymlinks(t, t.TempDir())
			repoDir := filepath.Join(baseDir, "repo")
			stagingDir := filepath.Join(baseDir, "staging")
			appdataDir := filepath.Join(baseDir, "appdata")
			require.NoError(t, os.MkdirAll(appdataDir, 0o755))

			cfg := &Config{
				DeployMode:       "local",
				LockFile:         filepath.Join(baseDir, "reconcile.lock"),
				StateFile:        filepath.Join(baseDir, "state.json"),
				RepoDir:          repoDir,
				StagingDir:       stagingDir,
				LocalAppdataPath: appdataDir,
				BackupDir:        filepath.Join(baseDir, "backups"),
				InfraSubDir:      ".",
			}
			seedStubComposeService(t, cfg)
			deploy := &DeployOps{
				ProjectName:     "test",
				ContentHashSync: true,
				composeUpFn:     func(context.Context, []string) error { return nil },
			}
			r := NewReconciler(cfg,
				WithGitOperations(&mockGitOps{syncChanged: true, syncBefore: "aaa", syncAfter: "bbb"}),
				WithDeployOps(deploy),
			)
			r.stagingOps = defaultStagingEvidenceOps()
			realRemoveAll := r.stagingOps.removeAll
			removeCalls := 0
			r.stagingOps.removeAll = func(path string) error {
				removeCalls++
				if removeCalls == 1 {
					return realRemoveAll(path)
				}
				return errors.New("cleanup remove injected")
			}
			if tc.failHarden {
				r.stagingOps.chmod = func(*os.File, fs.FileMode) error { return errors.New("cleanup chmod injected") }
			}

			err := r.Run(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				assert.ErrorContains(t, err, "cleanup remove injected")
				assert.ErrorContains(t, err, "cleanup chmod injected")
			} else {
				require.NoError(t, err)
				assertPrivateStagingTree(t, stagingDir)
			}
			assert.Equal(t, tc.wantCommit, LoadState(cfg.StateFile).LastDeployedCommit)
		})
	}
}

func assertPrivateStagingTree(t *testing.T, root string) {
	t.Helper()
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		require.NoError(t, err)
		info, err := os.Lstat(path)
		require.NoError(t, err)
		if info.IsDir() {
			assert.Equal(t, fs.FileMode(0o700), info.Mode().Perm(), path)
		} else {
			require.True(t, info.Mode().IsRegular(), "unexpected entry type at %s", path)
			assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm(), path)
		}
		return nil
	}))
}

func TestSafeStagingRootRejectsBroadPaths(t *testing.T) {
	for _, path := range []string{"", ".", string(filepath.Separator)} {
		t.Run(strings.ReplaceAll(path, string(filepath.Separator), "root"), func(t *testing.T) {
			_, err := safeStagingRoot(path)
			require.Error(t, err)
		})
	}
}
