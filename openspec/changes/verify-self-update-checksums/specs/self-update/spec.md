## ADDED Requirements

### Requirement: Verify the selected release archive
The system SHALL verify the SHA-256 digest of the exact compressed archive selected for self-update against that archive's entry in the `checksums.txt` asset from the same detected GitHub release before decompression, staging, or executable replacement.

#### Scenario: Matching archive checksum
- **WHEN** the selected release contains `checksums.txt` with a valid SHA-256 entry for the downloaded archive and the digest matches its bytes
- **THEN** Bosun may decompress the archive and proceed through the existing replacement and rollback path

#### Scenario: Checksum covers compressed release artifact
- **WHEN** Bosun validates a selected `.tar.gz` release asset
- **THEN** it computes the digest over the downloaded compressed archive bytes named in `checksums.txt`, not over the extracted executable

#### Scenario: Detected release identity remains stable
- **WHEN** release detection captures a platform archive and `checksums.txt` from one release and the source's latest release changes before installation finishes
- **THEN** Bosun validates only the captured archive against the captured manifest and does not redetect or combine assets from different releases

### Requirement: Fail closed on checksum-contract errors
The system MUST reject an update when `checksums.txt` is missing, cannot yield a valid entry for the exact selected archive, or contains a selected digest that does not match the downloaded archive, without an unchecked fallback.

#### Scenario: Checksum asset is missing
- **WHEN** the detected release has a platform archive but no asset named exactly `checksums.txt`
- **THEN** release detection fails and Bosun does not download, decompress, stage, or replace the executable

#### Scenario: Selected checksum cannot be parsed
- **WHEN** the selected archive's entry is malformed or malformed content is encountered before any exact matching entry
- **THEN** validation fails before decompression or executable replacement and the existing executable remains byte-for-byte unchanged

#### Scenario: Selected archive entry is missing
- **WHEN** `checksums.txt` is valid but has no entry whose filename exactly matches the selected archive
- **THEN** validation fails before decompression or executable replacement and the existing executable remains byte-for-byte unchanged

#### Scenario: Downloaded archive digest does not match
- **WHEN** the selected archive's calculated SHA-256 digest differs from its `checksums.txt` entry
- **THEN** validation fails before decompression or executable replacement and the existing executable remains byte-for-byte unchanged

#### Scenario: Captured asset download fails
- **WHEN** downloading the captured archive or captured `checksums.txt` asset fails or either asset disappears after detection
- **THEN** the invocation returns a non-zero error, preserves the existing executable, and does not switch releases, use cached unchecked data, or retry automatically

#### Scenario: Command reports verification failure
- **WHEN** checksum validation rejects an update
- **THEN** `bosun update` and its `selfupdate` alias return a non-zero error that preserves the underlying cause where available, identifies the failed stage and relevant asset, and does not print a successful-update result

### Requirement: Preserve release and platform selection
The system SHALL preserve stable semantic-version release selection and select only the existing GoReleaser archive for the runtime's supported OS and architecture while adding checksum validation.

#### Scenario: Darwin AMD64 archive
- **WHEN** the updater runs for `darwin/amd64`
- **THEN** it selects `bosun_<version>_darwin_amd64.tar.gz` and validates that exact filename's checksum entry

#### Scenario: Darwin ARM64 archive
- **WHEN** the updater runs for `darwin/arm64`
- **THEN** it selects `bosun_<version>_darwin_arm64.tar.gz` and validates that exact filename's checksum entry

#### Scenario: Linux AMD64 archive
- **WHEN** the updater runs for `linux/amd64`
- **THEN** it selects `bosun_<version>_linux_amd64.tar.gz` and validates that exact filename's checksum entry

#### Scenario: Linux ARM64 archive
- **WHEN** the updater runs for `linux/arm64`
- **THEN** it selects `bosun_<version>_linux_arm64.tar.gz` and validates that exact filename's checksum entry

#### Scenario: Prerelease remains excluded
- **WHEN** a newer prerelease and an older stable release are available
- **THEN** the updater preserves its existing default of selecting only the stable release and validates that release's selected archive

#### Scenario: Installed stable version is not downgraded or reinstalled
- **WHEN** the installed stable version is equal to or newer than the latest eligible stable release
- **THEN** Bosun reports no update and does not download or replace an executable

#### Scenario: Development version behavior remains unchanged
- **WHEN** the installed version is the development build marker and an eligible stable release exists
- **THEN** Bosun continues to treat that stable release as an available update and validates its selected archive before replacement

#### Scenario: Runtime platform is unsupported
- **WHEN** the runtime OS and architecture do not match one of the four published tuples
- **THEN** the install path preserves its existing no-suitable-release error, the check-only path preserves its existing no-update result, and neither path selects another platform's archive

### Requirement: Preserve check-only behavior
The system SHALL keep `bosun update --check` metadata-only while requiring the detected release to advertise the validation asset needed for a safe install.

#### Scenario: Installable update is available
- **WHEN** `--check` detects a newer supported release containing the selected archive and `checksums.txt`
- **THEN** it reports the update after checking same-release asset presence and identity metadata without downloading or parsing the archive or checksum asset bytes

#### Scenario: Latest release lacks checksum metadata
- **WHEN** `--check` detects a newer platform archive but the same release does not contain `checksums.txt`
- **THEN** it returns a non-zero validation-asset error instead of reporting the release as safely installable

### Requirement: Document the checksum trust boundary
The system MUST describe same-release SHA-256 verification as an integrity control and MUST NOT represent it as independent publisher authentication or signature verification.

#### Scenario: Consumer reads update documentation
- **WHEN** a user or AI agent reads the command reference, onboarding resource, README update guidance, or `llms.txt`
- **THEN** the material states that self-update fails closed on a missing or invalid same-release checksum and that replacing both release assets remains outside this control
