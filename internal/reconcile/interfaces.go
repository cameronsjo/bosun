package reconcile

import "context"

// GitOperations defines git sync operations.
type GitOperations interface {
	// Sync clones or pulls depending on whether repo exists.
	// Returns (changed, beforeCommit, afterCommit, error).
	// For fresh clones, changed is always true.
	Sync(ctx context.Context) (changed bool, before, after string, err error)

	// IsRepo checks if the directory is a git repository.
	// Uses the provided context for timeout control.
	IsRepo(ctx context.Context) bool
}

// SecretsDecryptor handles SOPS decryption.
type SecretsDecryptor interface {
	// DecryptFiles decrypts multiple SOPS files and merges them into a single map.
	// Later files override earlier ones for duplicate keys.
	DecryptFiles(ctx context.Context, files []string) (map[string]any, error)

	// CheckAgeKey verifies that an age key is available for SOPS decryption.
	CheckAgeKey() error
}

// TemplateRenderer handles chezmoi templating.
type TemplateRenderer interface {
	// Render processes all .tmpl files in srcDir and renders them to dstDir.
	// Non-template files are copied as-is.
	Render(ctx context.Context, srcDir, dstDir string, secrets map[string]any) error
}

// Deployer handles file deployment.
type Deployer interface {
	// Deploy syncs files to the target directory.
	// For local deployment, host should be empty.
	// For remote deployment, host should be "user@host".
	Deploy(ctx context.Context, srcDir, host, dstDir string) error

	// DeployFile syncs a single file to the target.
	DeployFile(ctx context.Context, srcFile, host, dstFile string) error

	// Backup creates a timestamped backup of the specified paths.
	// For local backup, host should be empty.
	// For remote backup, host should be "user@host".
	Backup(ctx context.Context, host, backupDir string, paths []string) (string, error)

	// EnsureDir ensures a directory exists.
	// For local, host should be empty.
	// For remote, host should be "user@host".
	EnsureDir(ctx context.Context, host, dir string) error

	// ComposeUp runs docker compose up for the specified compose file or directory.
	// For local, host should be empty.
	// For remote, host should be "user@host".
	ComposeUp(ctx context.Context, host, composePath string) error

	// SignalContainer sends a signal to a Docker container.
	// For local, host should be empty.
	// For remote, host should be "user@host".
	SignalContainer(ctx context.Context, host, containerName, signal string) error

	// CleanupBackups removes old backups, keeping only the most recent N.
	CleanupBackups(backupDir string, keep int) error
}

// Compile-time interface verification.
var (
	_ GitOperations    = (*GitOps)(nil)
	_ SecretsDecryptor = (*SOPSOps)(nil)
)

// Note: TemplateOps and DeployOps require adapter methods to implement
// the simplified interfaces. See templateAdapter and deployAdapter below.
