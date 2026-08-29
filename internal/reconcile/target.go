package reconcile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// ParseTargetsOverride decodes BOSUN_TARGETS and reports whether the decoded
// value is an authoritative override. A JSON empty array is authoritative,
// while JSON null leaves project-config targets in effect.
func ParseTargetsOverride(value string) ([]Target, bool, error) {
	var targets []Target
	if err := json.Unmarshal([]byte(value), &targets); err != nil {
		return nil, false, err
	}
	return targets, targets != nil, nil
}

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

// IsDefault returns true if this is the implicit default target. The comparison
// is case-insensitive so a case-variant name ("Default", "DEFAULT") cannot slip
// past as a distinct target and fragment state from the base (#228).
func (t Target) IsDefault() bool {
	return strings.EqualFold(t.Name, DefaultTargetName)
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

	// The implicit default preserves the base paths exactly, while a lone
	// explicit default target may override them just like BOSUN_TARGETS
	// (#273/#391). Named targets derive isolated paths when no override is set.
	if t.IsDefault() {
		if t.StateFile != "" {
			cp.StateFile = t.StateFile
		}
		if t.StagingDir != "" {
			cp.StagingDir = t.StagingDir
		}
	} else {
		stateDir := filepath.Dir(c.StateFile)
		cp.StateFile = TargetStateFile(stateDir, t)
		cp.StagingDir = TargetStagingDir(c.StagingDir, t)
		lockDir := filepath.Dir(c.LockFile)
		cp.LockFile = TargetLockFile(lockDir, t)
	}

	// Deep-copy all mutable slices so the returned config has independent backing
	// arrays. Both inherited slices (from base) and overrides (from target) are
	// cloned to prevent mutation of either source.
	// Use nil checks (not len > 0) so targets can explicitly clear inherited
	// slices with an empty list (e.g., critical_containers: []).
	cp.Targets = cloneTargets(cp.Targets)
	cp.SecretsFiles = cloneSlice(cp.SecretsFiles)
	cp.DeployPaths.Value = cloneSlice(cp.DeployPaths.Value)
	cp.DriftIgnore.Value = cloneSlice(cp.DriftIgnore.Value)
	if t.CriticalContainers != nil {
		cp.CriticalContainers.Value = cloneSlice(t.CriticalContainers)
	} else if cp.CriticalContainers.Value != nil {
		cp.CriticalContainers.Value = cloneSlice(cp.CriticalContainers.Value)
	}
	if t.PostSyncHooks != nil && !cp.PostSyncHooks.FromEnv() {
		if c.targetHooksFromEnv(t) {
			// A target-specific hook list supplied by BOSUN_TARGETS is an
			// authoritative environment replacement, including explicit [].
			// Preserve that ownership on the per-target config so repo reloads
			// cannot replace it through either root or target hooks.
			cp.PostSyncHooks = EnvConfigField(clonePostSyncHooks(t.PostSyncHooks))
		} else {
			cp.PostSyncHooks.SetFromFile(clonePostSyncHooks(t.PostSyncHooks))
		}
	} else if cp.PostSyncHooks.Value != nil {
		cp.PostSyncHooks.Value = clonePostSyncHooks(cp.PostSyncHooks.Value)
	}
	if t.DeploySyncPaths != nil {
		cp.DeploySyncPaths.Value = cloneSlice(t.DeploySyncPaths)
	} else if cp.DeploySyncPaths.Value != nil {
		cp.DeploySyncPaths.Value = cloneSlice(cp.DeploySyncPaths.Value)
	}
	if t.DeploySyncExclude != nil {
		cp.DeploySyncExclude.Value = cloneSlice(t.DeploySyncExclude)
	} else if cp.DeploySyncExclude.Value != nil {
		cp.DeploySyncExclude.Value = cloneSlice(cp.DeploySyncExclude.Value)
	}

	if t.SecretsScope != "" {
		cp.SecretsScope = t.SecretsScope
	}

	return &cp
}

// targetHooksFromEnv reports whether t carries an explicit hook override from
// BOSUN_TARGETS. ResolveTargets materializes inherited root hooks onto the
// implicit default Target, so TargetsFromEnv alone is insufficient: an empty
// BOSUN_TARGETS array and a lone default target with no hook key must continue
// to inherit file-owned root hooks and accept later repo reloads.
func (c *Config) targetHooksFromEnv(t Target) bool {
	if !c.TargetsFromEnv || t.PostSyncHooks == nil {
		return false
	}
	if !t.IsDefault() {
		return true
	}
	return len(c.Targets) == 1 && c.Targets[0].IsDefault() && c.Targets[0].PostSyncHooks != nil
}

func cloneSlice[T any](values []T) []T {
	if values == nil {
		return nil
	}
	cloned := make([]T, len(values))
	copy(cloned, values)
	return cloned
}

func cloneTargets(targets []Target) []Target {
	if targets == nil {
		return nil
	}
	cloned := make([]Target, len(targets))
	for i, target := range targets {
		cloned[i] = target
		cloned[i].CriticalContainers = cloneSlice(target.CriticalContainers)
		cloned[i].PostSyncHooks = clonePostSyncHooks(target.PostSyncHooks)
		cloned[i].DeploySyncPaths = cloneSlice(target.DeploySyncPaths)
		cloned[i].DeploySyncExclude = cloneSlice(target.DeploySyncExclude)
	}
	return cloned
}

// clonePostSyncHooks returns a deep copy of a PostSyncHook slice,
// including each hook's Paths and Command slices.
func clonePostSyncHooks(hooks []PostSyncHook) []PostSyncHook {
	if hooks == nil {
		return nil
	}
	cloned := make([]PostSyncHook, len(hooks))
	for i, h := range hooks {
		cloned[i] = h
		cloned[i].Paths = cloneSlice(h.Paths)
		cloned[i].Command = cloneSlice(h.Command)
	}
	return cloned
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
		BackupTimeout:         DefaultBackupTimeout,
		HealthCheckTimeout:    60 * time.Second,
		HealthCheckInterval:   5 * time.Second,
		RestartBreakerEnabled: true,
		RestartThreshold:      5,
		RestartWindow:         10 * time.Minute,
		OnFailure:             true,
		RemoveOrphans:         NewConfigField(true),
		HealthGateTimeout:     60 * time.Second,
	}
}

// ResolveTargets returns the effective target list for this config.
// When Targets is empty, it synthesizes a single implicit default target from
// the flat config fields for backwards compatibility. A lone target named
// `default` (case-insensitive) is the implicit default's configuration — its
// fields (project_name above all) are honored, not discarded (#391). A
// multi-target config that includes a `default` target is a hard error:
// silently dropping it would deploy that target project-less and collide.
func (c *Config) ResolveTargets() ([]Target, error) {
	if err := ValidatePostSyncHooks(c.PostSyncHooks.Value); err != nil {
		return nil, err
	}
	for _, target := range c.Targets {
		if err := ValidatePostSyncHooks(target.PostSyncHooks); err != nil {
			return nil, fmt.Errorf("target %q: %w", target.Name, err)
		}
	}
	return c.resolveTargetLayout()
}

// ValidateMultiTargetLayout validates structural multi-target configuration
// without re-validating operational hooks. Daemon startup uses this narrower
// entry point so it can preserve its existing aggregation of every hook error
// while rejecting target resource collisions before binding API listeners.
func (c *Config) ValidateMultiTargetLayout() error {
	if len(c.Targets) == 0 {
		return nil
	}
	if err := validateTargetDescriptors(c.Targets); err != nil {
		return err
	}
	if err := c.validateNamedTargetPathConfinement(c.Targets); err != nil {
		return err
	}
	// A lone target cannot collide with a sibling. Avoid requiring operational
	// base paths from partial embedded configs at daemon structural validation.
	if len(c.Targets) == 1 {
		return nil
	}
	_, err := c.resolveTargetLayout()
	return err
}

func (c *Config) resolveTargetLayout() ([]Target, error) {
	if len(c.Targets) > 0 {
		if len(c.Targets) == 1 && c.Targets[0].IsDefault() {
			defaults := []Target{c.implicitDefaultTarget(c.Targets[0])}
			if err := ValidateStagingEvidenceTargets(c, defaults); err != nil {
				return nil, err
			}
			return defaults, nil
		}

		if err := validateTargetDescriptors(c.Targets); err != nil {
			return nil, err
		}
		if err := c.validateNamedTargetPathConfinement(c.Targets); err != nil {
			return nil, err
		}
		if err := c.validateTargetResourceCollisions(c.Targets); err != nil {
			return nil, err
		}
		if err := ValidateStagingEvidenceTargets(c, c.Targets); err != nil {
			return nil, err
		}
		return c.Targets, nil
	}
	defaults := []Target{c.implicitDefaultTarget(Target{})}
	if err := ValidateStagingEvidenceTargets(c, defaults); err != nil {
		return nil, err
	}
	return defaults, nil
}

func validateTargetDescriptors(targets []Target) error {
	seen := make(map[string]string, len(targets))
	for _, target := range targets {
		if err := ValidateTargetName(target.Name); err != nil {
			return fmt.Errorf("target %q: %w", target.Name, err)
		}
		if len(targets) > 1 && target.IsDefault() {
			return fmt.Errorf("target %q uses the reserved default name in a multi-target config, expected every target to carry a distinct name: rename it, or make it the only target to configure the implicit default", target.Name)
		}
		key := strings.ToLower(target.Name)
		if first, ok := seen[key]; ok {
			return fmt.Errorf("targets %q and %q have duplicate names (case-insensitive)", first, target.Name)
		}
		seen[key] = target.Name
	}
	return nil
}

func (c *Config) validateNamedTargetPathConfinement(targets []Target) error {
	for _, target := range targets {
		// The implicit/lone-default compatibility target keeps its historical
		// exact overrides. R1 confines named target-owned paths.
		if target.IsDefault() {
			continue
		}
		if target.StateFile != "" {
			if c.StateFile == "" {
				return fmt.Errorf("target %q state_file %q has no configured state root", target.Name, target.StateFile)
			}
			if err := validateTargetPathWithinRoot(target.Name, "state_file", filepath.Dir(c.StateFile), target.StateFile); err != nil {
				return err
			}
		}
		if target.StagingDir != "" {
			if err := validateTargetPathWithinRoot(target.Name, "staging_dir", c.StagingDir, target.StagingDir); err != nil {
				return err
			}
		}
		if target.LocalAppdataPath != "" {
			if err := validateTargetPathWithinRoot(target.Name, "local_appdata_path", c.LocalAppdataPath, target.LocalAppdataPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateTargetPathWithinRoot(target, field, root, candidate string) error {
	if root == "" {
		return fmt.Errorf("target %q %s %q has no configured permitted root", target, field, candidate)
	}
	canonicalRoot, err := canonicalTargetPath(root)
	if err != nil {
		return fmt.Errorf("target %q %s root %q: %w", target, field, root, err)
	}
	canonicalCandidate, err := canonicalTargetPath(candidate)
	if err != nil {
		return fmt.Errorf("target %q %s %q: %w", target, field, candidate, err)
	}
	rel, err := filepath.Rel(canonicalRoot, canonicalCandidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("target %q %s %q escapes configured root %q", target, field, candidate, root)
	}
	return nil
}

// canonicalTargetPath resolves every existing ancestor so a symlink nested
// under an allowed root cannot redirect a target-owned path outside it. Missing
// suffixes are appended lexically because state and staging paths may not exist
// until the first reconciliation.
func canonicalTargetPath(value string) (string, error) {
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	abs = filepath.Clean(abs)

	probe := abs
	var suffix []string
	for {
		_, statErr := os.Lstat(probe)
		if statErr == nil {
			resolved, evalErr := filepath.EvalSymlinks(probe)
			if evalErr != nil {
				return "", fmt.Errorf("resolve existing path ancestor %q: %w", probe, evalErr)
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			abs = filepath.Clean(resolved)
			break
		}
		if !errors.Is(statErr, fs.ErrNotExist) {
			return "", fmt.Errorf("inspect path ancestor %q: %w", probe, statErr)
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", fmt.Errorf("no existing ancestor for path %q", value)
		}
		suffix = append(suffix, filepath.Base(probe))
		probe = parent
	}

	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		abs = strings.ToLower(abs)
	}
	return abs, nil
}

type targetResourceKey struct {
	host     string
	resource string
}

// validateTargetResourceCollisions rejects targets that would share local
// deploy state or operate on the same Docker Compose namespace or destination.
// State files are process-local regardless of target host, while namespace and
// deploy-path keys include the host so identical remote resources remain safe
// when they live on separate machines.
func (c *Config) validateTargetResourceCollisions(targets []Target) error {
	stateFiles := make(map[string]string, len(targets))
	namespaces := make(map[targetResourceKey]string, len(targets))
	deployPaths := make(map[targetResourceKey]string, len(targets))

	for _, target := range targets {
		effective := c.ConfigForTarget(target)
		stateFile := normalizeTargetStateFile(effective.StateFile)
		if first, ok := stateFiles[stateFile]; ok {
			return fmt.Errorf(
				"targets %q and %q resolve to the same state file: %q",
				first, target.Name, effective.StateFile,
			)
		}
		stateFiles[stateFile] = target.Name

		host := strings.ToLower(effective.TargetHost)

		projectName, projectLabel := normalizeTargetProjectName(effective.ProjectName)
		namespaceKey := targetResourceKey{host: host, resource: projectName}
		if first, ok := namespaces[namespaceKey]; ok {
			return fmt.Errorf(
				"targets %q and %q resolve to the same Docker namespace (host %q, project_name %q)",
				first, target.Name, targetHostLabel(effective.TargetHost), projectLabel,
			)
		}
		namespaces[namespaceKey] = target.Name

		deployPath := effective.RemoteAppdataPath
		if effective.TargetHost == "" {
			deployPath = effective.LocalAppdataPath
		}
		if deployPath == "" {
			continue
		}
		key := targetResourceKey{host: host, resource: normalizeTargetDeployPath(effective.TargetHost, deployPath)}
		if first, ok := deployPaths[key]; ok {
			return fmt.Errorf(
				"targets %q and %q resolve to the same deploy path on host %q: %q",
				first, target.Name, targetHostLabel(effective.TargetHost), deployPath,
			)
		}
		deployPaths[key] = target.Name
	}

	return nil
}

func normalizeTargetStateFile(stateFile string) string {
	normalized := filepath.Clean(stateFile)
	// Windows paths are case-insensitive. Most macOS deployments use the
	// case-insensitive APFS default, so reject case-only aliases there too rather
	// than permit two targets to overwrite one state file on the common setup.
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		normalized = strings.ToLower(normalized)
	}
	return normalized
}

func normalizeTargetProjectName(projectName string) (normalized, label string) {
	if projectName == "" {
		// Bosun runs both local and remote Compose commands from the fixed
		// appdata/compose directory. Without -p (or a compose-file name
		// override unavailable at config resolution), Compose derives "compose".
		return "compose", "compose (derived)"
	}
	return strings.ToLower(projectName), projectName
}

func normalizeTargetDeployPath(host, deployPath string) string {
	if host == "" {
		return filepath.Clean(deployPath)
	}
	// Remote deploy paths always use the target host's slash semantics, even
	// when Bosun itself is running on Windows.
	return path.Clean(deployPath)
}

func targetHostLabel(host string) string {
	if host == "" {
		return "local"
	}
	return host
}

// implicitDefaultTarget synthesizes the implicit default target from the flat
// config fields. When the config declared an explicit `name: default` target,
// its non-zero fields win over the flat values (#391) — the name itself is
// normalized to the canonical DefaultTargetName so case variants ("Default")
// share the legacy state/lock/staging paths (#228).
//
// Note this "is default" context differs from Target.IsDefault(): an empty
// explicit.Name here means "no explicit target declared" (caller passes the
// zero Target), while Target{Name: ""}.IsDefault() is false — don't try to
// consolidate the two predicates.
func (c *Config) implicitDefaultTarget(explicit Target) Target {
	t := Target{
		Name:               DefaultTargetName,
		TargetHost:         c.TargetHost,
		LocalAppdataPath:   c.LocalAppdataPath,
		RemoteAppdataPath:  c.RemoteAppdataPath,
		ProjectName:        c.ProjectName,
		StateFile:          c.StateFile,
		StagingDir:         c.StagingDir,
		CriticalContainers: c.CriticalContainers.Value,
		PostSyncHooks:      c.PostSyncHooks.Value,
		DeploySyncPaths:    c.DeploySyncPaths.Value,
		DeploySyncExclude:  c.DeploySyncExclude.Value,
	}
	if explicit.TargetHost != "" {
		t.TargetHost = explicit.TargetHost
	}
	if explicit.LocalAppdataPath != "" {
		t.LocalAppdataPath = explicit.LocalAppdataPath
	}
	if explicit.RemoteAppdataPath != "" {
		t.RemoteAppdataPath = explicit.RemoteAppdataPath
	}
	if explicit.ProjectName != "" {
		t.ProjectName = explicit.ProjectName
	}
	if explicit.StateFile != "" {
		t.StateFile = explicit.StateFile
	}
	if explicit.StagingDir != "" {
		t.StagingDir = explicit.StagingDir
	}
	if explicit.SecretsScope != "" {
		t.SecretsScope = explicit.SecretsScope
	}
	// Slice overrides use nil checks so an explicit empty list can clear the
	// inherited flat value, matching ConfigForTarget's semantics.
	if explicit.CriticalContainers != nil {
		t.CriticalContainers = explicit.CriticalContainers
	}
	if explicit.PostSyncHooks != nil {
		t.PostSyncHooks = explicit.PostSyncHooks
	}
	if explicit.DeploySyncPaths != nil {
		t.DeploySyncPaths = explicit.DeploySyncPaths
	}
	if explicit.DeploySyncExclude != nil {
		t.DeploySyncExclude = explicit.DeploySyncExclude
	}
	return t
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
	if strings.EqualFold(name, DefaultTargetName) {
		return nil // The implicit default is always valid (case-insensitive, #228)
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
