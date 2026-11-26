## ADDED Requirements

### Requirement: Migration Output Validity

The system SHALL write a migrated manifest only when the result parses as one YAML document carrying both `apiVersion` and `kind`. Migration SHALL keep the original body and a leading `---` marker, SHALL keep an existing `apiVersion` or `kind` value and add only the missing field, and SHALL leave the file unchanged and report an error when it cannot meet this.

#### Scenario: Leading document marker is preserved

- **WHEN** an unversioned manifest begins with `---`
- **THEN** the migrated file is one document that keeps its original fields and carries `apiVersion` and `kind`

#### Scenario: Half-versioned manifest is completed

- **WHEN** a manifest has `apiVersion` but no `kind`, or `kind` but no `apiVersion`
- **THEN** only the missing field is added and the existing value is unchanged

#### Scenario: Unparseable result is not written

- **WHEN** the migrated content would not parse as a versioned manifest
- **THEN** the file on disk is unchanged and the file is reported as an error
