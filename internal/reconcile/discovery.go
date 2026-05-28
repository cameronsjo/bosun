package reconcile

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// DeployTarget represents a discovered directory or file in the staging area
// that should be synced to the target host during deployment.
type DeployTarget struct {
	// RelPath is the path relative to the staging subdirectory (e.g. "appdata/traefik", "compose").
	// Used for source path construction and filter matching.
	RelPath string
	// TargetPath is the path relative to the appdata base directory on the deploy target.
	// For appdata children, this strips the "appdata/" prefix (e.g. "traefik").
	// For top-level entries, this matches RelPath (e.g. "compose").
	TargetPath string
	// IsDir indicates whether the target is a directory (true) or a single file (false).
	IsDir bool
}

// discoverDeployTargets scans the staging subdirectory and returns deploy targets.
//
// The staging directory is expected to have this structure:
//
//	staging/<infra-sub-dir>/
//	  appdata/
//	    traefik/       -> DeployTarget{RelPath: "appdata/traefik", IsDir: true}
//	    authelia/      -> DeployTarget{RelPath: "appdata/authelia", IsDir: true}
//	  compose/         -> DeployTarget{RelPath: "compose", IsDir: true}
//
// For the "appdata" directory, targets are expanded one level deeper to get
// per-service directories/files. All other top-level entries are returned as-is.
//
// When syncPaths is non-empty, only targets matching at least one pattern are included.
// When excludePaths is non-empty, targets matching any pattern are excluded.
// Exclude always wins over include.
//
// Results are sorted by RelPath for deterministic deploy order.
func discoverDeployTargets(stagingSubDir string, syncPaths, excludePaths []string) ([]DeployTarget, error) {
	entries, err := os.ReadDir(stagingSubDir)
	if err != nil {
		return nil, err
	}

	var targets []DeployTarget

	for _, entry := range entries {
		name := entry.Name()

		if name == "appdata" && entry.IsDir() {
			// Expand appdata one level to get per-service targets.
			appdataTargets, err := expandAppdata(filepath.Join(stagingSubDir, "appdata"))
			if err != nil {
				return nil, err
			}
			targets = append(targets, appdataTargets...)
			continue
		}

		targets = append(targets, DeployTarget{
			RelPath:    name,
			TargetPath: name,
			IsDir:      entry.IsDir(),
		})
	}

	// Apply allowlist filter.
	if len(syncPaths) > 0 {
		targets = filterInclude(targets, syncPaths)
	}

	// Apply blocklist filter (exclude wins over include).
	if len(excludePaths) > 0 {
		targets = filterExclude(targets, excludePaths)
	}

	sort.Slice(targets, func(i, j int) bool {
		return targets[i].RelPath < targets[j].RelPath
	})

	return targets, nil
}

// expandAppdata reads the appdata directory and returns a DeployTarget for each child.
func expandAppdata(appdataDir string) ([]DeployTarget, error) {
	entries, err := os.ReadDir(appdataDir)
	if err != nil {
		return nil, err
	}

	targets := make([]DeployTarget, 0, len(entries))
	for _, entry := range entries {
		targets = append(targets, DeployTarget{
			RelPath:    filepath.Join("appdata", entry.Name()),
			TargetPath: entry.Name(), // Strip "appdata/" — appdata base is the deploy target root
			IsDir:      entry.IsDir(),
		})
	}
	return targets, nil
}

// filterInclude returns only targets whose RelPath matches at least one pattern.
func filterInclude(targets []DeployTarget, patterns []string) []DeployTarget {
	var result []DeployTarget
	for _, t := range targets {
		for _, p := range patterns {
			if matchGlob(p, t.RelPath) {
				result = append(result, t)
				break
			}
		}
	}
	return result
}

// filterExclude returns targets whose RelPath does not match any pattern.
func filterExclude(targets []DeployTarget, patterns []string) []DeployTarget {
	var result []DeployTarget
	for _, t := range targets {
		excluded := false
		for _, p := range patterns {
			if matchGlob(p, t.RelPath) {
				excluded = true
				break
			}
		}
		if !excluded {
			result = append(result, t)
		}
	}
	return result
}

// backupFilesFromTargets enumerates the destination files that make up each
// target's deployed footprint — the files bosun renders into staging and syncs
// to appdata — so the pre-deploy backup captures only managed config, not the
// unrelated runtime data (media, databases, caches) co-located in the same
// appdata directory. Backing up whole target directories let the archive grow
// without bound and time out (bosun-5qx / GH#330 secondary).
//
// For each target it walks the STAGING source (small, rendered output) and maps
// every regular file onto its appdata destination path under appdataBase. A
// file target maps directly to its destination. Symlinks and directories are
// skipped via IsRegular() (Lstat semantics), matching the copy path — which
// never deploys symlinks — and verifyDeployTarget. Destinations that do not yet
// exist on disk (a brand-new file this deploy adds) are left for Backup() to
// filter out; there is nothing to back up for them.
//
// Returns the destination file paths and the first walk error encountered.
// Callers treat the error as non-fatal and back up whatever was enumerated.
func backupFilesFromTargets(stagingSubDir string, targets []DeployTarget, appdataBase string) ([]string, error) {
	var (
		files    []string
		firstErr error
	)
	for _, t := range targets {
		dst := filepath.Join(appdataBase, t.TargetPath)
		if !t.IsDir {
			files = append(files, dst)
			continue
		}
		src := filepath.Join(stagingSubDir, t.RelPath)
		walkErr := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			// IsRegular() is false for directories and symlinks alike, matching
			// the copy path's symlink skip and verifyDeployTarget.
			if !d.Type().IsRegular() {
				return nil
			}
			rel, relErr := filepath.Rel(src, path)
			if relErr != nil {
				return relErr
			}
			files = append(files, filepath.Join(dst, rel))
			return nil
		})
		if walkErr != nil && firstErr == nil {
			firstErr = walkErr
		}
	}
	return files, firstErr
}
