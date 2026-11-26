# Change: Migration output stays a valid versioned manifest

## Why

`bosun migrate version --write` prepends `apiVersion` and `kind` to the raw file bytes. A file that starts with `---` becomes a two-document stream whose first document holds only the header, so the manifest body is no longer read. A file that already has `apiVersion` or `kind` but not both gets the key a second time, and the duplicate key makes the file unparseable. `MigrateFile` writes the result without parsing it, so the broken file is saved to disk.

## What Changes

- Add one requirement: a migrated manifest is written only when it parses as one document that carries both `apiVersion` and `kind`.
- A leading `---` marker and the original body are kept, and the header follows the marker.
- For a half-versioned file only the missing field is added and the existing value is kept. A key present with an empty or null value counts as missing, and its value is replaced in place.
- When the migration cannot meet this, including for a file of several documents, the file is left unchanged and reported as an error. Dry-run reports the same error instead of "would migrate".
- Add a second requirement: `bosun migrate version` exits non-zero when any file reports an error.

The "Manifest Migration" scenarios that say the header is prepended still hold for files without a leading marker, so that requirement is not modified.

## Impact

- Affected specs: `manifest-system` (Migration Output Validity and Migrate Version Exit Status added)
- Affected code: `internal/manifest/migrate.go` (`MigrateToV1`, `MigrateFile`), `internal/cmd/migrate.go` (`runMigrateVersion`), tests in `internal/manifest` and `internal/cmd`
- Behavior change: inputs that used to be rewritten into a broken file are now migrated correctly or reported as errors without being written, and a per-file error now fails the command.

Part of cameronsjo/bosun#801.
