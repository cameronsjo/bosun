# Change: Migration output stays a valid versioned manifest

## Why

`bosun migrate version --write` prepends `apiVersion` and `kind` to the raw file bytes. A file that starts with `---` becomes a two-document stream whose first document holds only the header, so the manifest body is no longer read. A file that already has `apiVersion` or `kind` but not both gets the key a second time, and the duplicate key makes the file unparseable. `MigrateFile` writes the result without parsing it, so the broken file is saved to disk.

## What Changes

- Add one requirement: a migrated manifest is written only when it parses as one document that carries both `apiVersion` and `kind`.
- A leading `---` marker and the original body are kept; for a half-versioned file only the missing field is added and the existing value is kept.
- When the migration cannot meet this, the file is left unchanged and reported as an error.
- `bosun migrate version` exits non-zero when any file reports an error. This is command behavior and is not specced here.

Already-versioned files and the three unversioned cases in "Manifest Migration" behave as before, so that requirement is not modified.

## Impact

- Affected specs: `manifest-system` (Migration Output Validity added)
- Affected code: `internal/manifest/migrate.go` (`MigrateToV1`, `MigrateFile`), `internal/cmd/migrate.go` (`runMigrateVersion`), tests in `internal/manifest` and `internal/cmd`
- Behavior change: inputs that used to be rewritten into a broken file are now migrated correctly or reported as errors without being written.

Part of cameronsjo/bosun#801.
