package reconcile

import (
	"fmt"
	"path/filepath"
	"regexp"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
)

// DefaultTargetName is the name used for the implicit single-target backwards-compat target.
const DefaultTargetName = "default"

// DefaultLockFile is the default path for the reconciliation lock file.
const DefaultLockFile = "/var/run/bosun/reconcile.lock"

// DefaultLockDir is the default directory for per-target lock files.
const DefaultLockDir = "/var/run/bosun"

// Target describes a single deployment target (a server/host to deploy to).
// Each target has its own host, appdata paths, project name, state file,
// staging directory, secrets scope, and operational overrides.
type Target struct {
	// Name identifies this target (e.g., "unraid", "pi"). Used in file paths and logs.
	Name string `json:"name"`
	// TargetHost is empty for local deployment, or "user@host" for remote.
	TargetHost string `json:"target_host,omitempty"`
	// LocalAppdataPath is the path to appdata when running locally.
	LocalAppdataPath string `json:"local_appdata_path,omitempty"`
	// RemoteAppdataPath is the path to appdata on the remote host.
	RemoteAppdataPath string `json:"remote_appdata_path,omitempty"`
	// ProjectName is the docker compose project name for this target.
	ProjectName string `json:"project_name,omitempty"`
	// StateFile overrides the derived state file path. When empty, derived from Name.
	StateFile string `json:"state_file,omitempty"`
	// StagingDir overrides the derived staging directory. When empty, derived from Name.
	StagingDir string `json:"staging_dir,omitempty"`
	// SecretsScope is the key prefix for per-target secrets (e.g., "unraid" -> targets.unraid.*).
	SecretsScope string `json:"secrets_scope,omitempty"`
	// CriticalContainers overrides the global list for this target.
	CriticalContainers []string `json:"critical_containers,omitempty"`
	// PostSyncHooks overrides the global hooks for this target.
	PostSyncHooks []PostSyncHook `json:"post_sync_hooks,omitempty"`
	// DeploySyncPaths overrides the global allowlist for this target.
	DeploySyncPaths []string `json:"deploy_sync_paths,omitempty"`
	// DeploySyncExclude overrides the global blocklist for this target.
	DeploySyncExclude []string `json:"deploy_sync_exclude,omitempty"`
}

// IsDefault returns true if this is the implicit default target.
func (t Target) IsDefault() bool {
	return t.Name == DefaultTargetName
}

// ConfigForTarget returns a shallow copy of the base config with per-target
// fields (TargetHost, appdata paths, ProjectName, StagingDir, StateFile,
// LockFile, CriticalContainers, PostSyncHooks, DeploySyncPaths/Exclude)
// overridden from the given Target. This lets the existing pipeline run
// unchanged per-target - the daemon creates a ConfigForTarget copy before
// each reconciler instantiation.
func (c *Config) ConfigForTarget(t Target) *Config {
	cp := *c // shallow copy

	// Override per-target fields.
	cp.TargetName = t.Name
	cp.TargetHost = t.TargetHost
	if t.LocalAppdataPath != "" {
		cp.LocalAppdataPath = t.LocalAppdataPath
	}
	if t.RemoteAppdataPath != "" {
		cp.RemoteAppdataPath = t.RemoteAppdataPath
	}
	if t.ProjectName != "" {
		cp.ProjectName = t.ProjectName
	}

	// Derive per-target paths from the base config's directories,
	// preserving any custom paths the caller configured.
	// For the default (implicit) target, keep the original paths unchanged
	// so the daemon/CLI share the same lock and state files as pre-multi-target.
	if !t.IsDefault() {
		stateDir := filepath.Dir(c.StateFile)
		cp.StateFile = TargetStateFile(stateDir, t)
		cp.StagingDir = TargetStagingDir(c.StagingDir, t)
		lockDir := filepath.Dir(c.LockFile)
		cp.LockFile = TargetLockFile(lockDir, t)
	}

	// Deep-copy inherited slices to prevent mutation of the base config
	// (shallow copy shares backing arrays). Then apply per-target overrides.
	// Use nil checks (not len > 0) so targets can explicitly clear inherited
	// slices with an empty list (e.g., critical_containers: []).
	if t.CriticalContainers != nil {
		cp.CriticalContainers = t.CriticalContainers
	} else if cp.CriticalContainers != nil {
		cp.CriticalContainers = append([]string(nil), cp.CriticalContainers...)
	}
	if t.PostSyncHooks != nil {
		cp.PostSyncHooks = t.PostSyncHooks
	} else if cp.PostSyncHooks != nil {
		hooks := make([]PostSyncHook, len(cp.PostSyncHooks))
		copy(hooks, cp.PostSyncHooks)
		cp.PostSyncHooks = hooks
	}
	if t.DeploySyncPaths != nil {
		cp.DeploySyncPaths = t.DeploySyncPaths
	} else if cp.DeploySyncPaths != nil {
		cp.DeploySyncPaths = append([]string(nil), cp.DeploySyncPaths...)
	}
	if t.DeploySyncExclude != nil {
		cp.DeploySyncExclude = t.DeploySyncExclude
	} else if cp.DeploySyncExclude != nil {
		cp.DeploySyncExclude = append([]string(nil), cp.DeploySyncExclude...)
	}

	cp.SecretsScope = t.SecretsScope

	return &cp
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		RepoBranch:            "main",
		RepoDir:               "/app/repo",
		StagingDir:            "/app/staging",
		BackupDir:             "/app/backups",
		LogDir:                "/app/logs",
		LockFile:              DefaultLockFile,
		StateFile:             filepath.Join(DefaultStateDir, DefaultStateFile),
		LocalAppdataPath:      "/mnt/appdata",
		RemoteAppdataPath:     "/mnt/user/appdata",
		InfraSubDir:           ".",
		BackupsToKeep:         5,
		HealthCheckTimeout:    60 * time.Second,
		HealthCheckInterval:   5 * time.Second,
		RestartBreakerEnabled: true,
		RestartThreshold:      5,
		RestartWindow:         10 * time.Minute,
		OnFailure:             true,
		RemoveOrphans:         true,
		HealthGateTimeout:     60 * time.Second,
	}
}

// ResolveTargets returns the effective target list for this config.
// When Targets is non-empty, returns it as-is.
// When Targets is empty, synthesizes a single implicit default target
// from the flat config fields for backwards compatibility.
func (c *Config) ResolveTargets() []Target {
	if len(c.Targets) > 0 {
		// Validate target names before returning to prevent path traversal.
		valid := make([]Target, 0, len(c.Targets))
		for _, t := range c.Targets {
			if err := ValidateTargetName(t.Name); err != nil {
				log.Warn().Str("target", t.Name).Err(err).Msg("Skipping target with invalid name")
				continue
			}
			valid = append(valid, t)
		}
		if len(valid) > 0 {
			return valid
		}
		// All configured targets had invalid names — fall through to implicit default.
		log.Warn().Int("configured", len(c.Targets)).Msg("All configured targets have invalid names, falling back to default target")
	}
	return []Target{
		{
			Name:               DefaultTargetName,
			TargetHost:         c.TargetHost,
			LocalAppdataPath:   c.LocalAppdataPath,
			RemoteAppdataPath:  c.RemoteAppdataPath,
			ProjectName:        c.ProjectName,
			StateFile:          c.StateFile,
			StagingDir:         c.StagingDir,
			CriticalContainers: c.CriticalContainers,
			PostSyncHooks:      c.PostSyncHooks,
			DeploySyncPaths:    c.DeploySyncPaths,
			DeploySyncExclude:  c.DeploySyncExclude,
		},
	}
}

// safeTargetNamePattern matches safe target names: alphanumeric, hyphens, underscores.
// Must start with a letter or digit.
var safeTargetNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// ValidateTargetName checks that a target name is safe for use in filesystem paths.
// Rejects empty names, path traversal attempts, absolute paths, and special characters.
func ValidateTargetName(name string) error {
	if name == "" {
		return fmt.Errorf("target name must not be empty")
	}
	if name == DefaultTargetName {
		return nil // The implicit default is always valid
	}
	if !safeTargetNamePattern.MatchString(name) {
		return fmt.Errorf("target name %q contains unsafe characters (allowed: alphanumeric, hyphens, underscores)", name)
	}
	return nil
}

// TargetStateFile returns the state file path for a target.
// The default target uses the legacy path; named targets use deploy-state-<name>.json.
func TargetStateFile(baseStateDir string, t Target) string {
	if t.StateFile != "" {
		return t.StateFile
	}
	if t.IsDefault() {
		return filepath.Join(baseStateDir, DefaultStateFile)
	}
	return filepath.Join(baseStateDir, fmt.Sprintf("deploy-state-%s.json", t.Name))
}

// TargetStagingDir returns the staging directory for a target.
// The default target uses the base staging dir; named targets use <staging>/<name>/.
func TargetStagingDir(baseStagingDir string, t Target) string {
	if t.StagingDir != "" {
		return t.StagingDir
	}
	if t.IsDefault() {
		return baseStagingDir
	}
	return filepath.Join(baseStagingDir, t.Name)
}

// TargetLockFile returns the lock file path for a target.
// The default target uses the legacy path; named targets use reconcile-<name>.lock.
func TargetLockFile(baseLockDir string, t Target) string {
	if t.IsDefault() {
		return filepath.Join(baseLockDir, "reconcile.lock")
	}
	return filepath.Join(baseLockDir, fmt.Sprintf("reconcile-%s.lock", t.Name))
}
