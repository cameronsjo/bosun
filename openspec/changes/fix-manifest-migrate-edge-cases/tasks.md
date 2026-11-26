## 1. Migration output validity

- [x] 1.1 `MigrateToV1` keeps a leading `---` marker and the original body, with the header after the marker
- [x] 1.2 `MigrateToV1` adds only the missing field of a half-versioned manifest, keeps an existing value, and replaces an empty or null value in place
- [x] 1.3 `MigrateFile` parses the migrated content and reports an error, writing nothing, when it does not parse as one versioned document; dry-run reports the same error
- [x] 1.4 A file of several YAML documents is reported as an error and left unchanged
- [x] 1.5 Tests: leading marker, comment before the marker, `apiVersion` only, `kind` only, empty `kind:`, several documents, unparseable result not written, dry run error

## 2. Migrate version exit status

- [x] 2.1 `bosun migrate version` prints every file's result, then exits non-zero when any file reports an error, with and without `--write`
- [x] 2.2 Test: one bad file among valid files makes the command return an error
