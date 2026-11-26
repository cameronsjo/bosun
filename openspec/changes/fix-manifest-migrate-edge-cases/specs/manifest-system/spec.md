## ADDED Requirements

### Requirement: Migration Output Validity

The system SHALL write a migrated manifest only when the result parses as one YAML document carrying both `apiVersion` and `kind`. Migration SHALL keep the original body and a leading `---` marker with the header after it, keep a non-empty existing `apiVersion` or `kind`, replace an empty or null one in place, and add only a missing field. If it cannot, including for a multi-document file, it SHALL leave the file unchanged and report an error, in dry-run too.

#### Scenario: Leading document marker is preserved

- **WHEN** an unversioned manifest begins with `---`
- **THEN** the migrated file is one document that keeps its original fields and carries `apiVersion` and `kind`
- **AND** the header follows the marker

#### Scenario: Comment before the marker stays first

- **WHEN** an unversioned manifest has a comment and then `---`
- **THEN** the comment and the marker stay at the top and the header follows the marker

#### Scenario: Half-versioned manifest is completed

- **WHEN** a manifest has `apiVersion` but no `kind`, or `kind` but no `apiVersion`
- **THEN** only the missing field is added and the existing value is unchanged

#### Scenario: Empty header value is replaced

- **WHEN** a manifest has `kind:` or `apiVersion:` with an empty or null value
- **THEN** that value is replaced in place and no second key of the same name is added

#### Scenario: Unparseable result is not written

- **WHEN** the migrated content would not parse as a versioned manifest
- **THEN** the file on disk is unchanged and the file is reported as an error

#### Scenario: Dry run reports the same error

- **WHEN** migration runs in dry-run mode on a file whose migrated content would not parse
- **THEN** it reports the error and does not report that the file would migrate

#### Scenario: Several documents are reported as an error

- **WHEN** a file holds more than one YAML document
- **THEN** the file is reported as an error and left unchanged

### Requirement: Migrate Version Exit Status

`bosun migrate version` SHALL exit non-zero when any file reports an error, with or without `--write`, after it has printed the results for every file.

#### Scenario: One bad file fails the command

- **WHEN** `bosun migrate version` finds one file that reports an error among several valid files
- **THEN** every file's result is printed and the command exits non-zero
