## 1. Migration output validity

- [ ] 1.1 `MigrateToV1` keeps a leading `---` marker and the original body
- [ ] 1.2 `MigrateToV1` adds only the missing field of a half-versioned manifest and keeps the existing value
- [ ] 1.3 `MigrateFile` parses the migrated content and reports an error, writing nothing, when it does not parse as a versioned manifest
- [ ] 1.4 `bosun migrate version` exits non-zero when any file reports an error
- [ ] 1.5 Tests: leading marker, `apiVersion` only, `kind` only, leading comment, unparseable result not written, command exit status
