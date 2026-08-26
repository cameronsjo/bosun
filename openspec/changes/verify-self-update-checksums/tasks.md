## 1. Integrate checksum validation

- [x] 1.1 Land after PR #602 or rebase it, preserving caller-context propagation
  and non-zero Cobra error returns while adding checksum verification.
- [x] 1.2 Configure the production go-selfupdate client with
  `ChecksumValidator{UniqueFilename: "checksums.txt"}` and preserve existing
  stable-release, no-downgrade/development-version, prerelease, and platform
  selection.
- [x] 1.3 Preserve wrapped validation errors through `internal/update` and the
  CLI, including underlying error identity where available and relevant asset
  context; suppress success output on failure and add no unchecked fallback.
- [x] 1.4 Preserve the archive and checksum asset identities captured from one
  detected release through `UpdateTo`; do not redetect, switch releases, or use
  cached/unchecked data during an install attempt.

## 2. Add deterministic acceptance coverage

- [x] 2.1 Build a no-network in-memory release fixture containing a small
  platform-named tar.gz archive and GoReleaser-format `checksums.txt`.
- [x] 2.2 Verify a matching digest replaces a temporary sentinel executable only
  after the compressed archive validates.
- [x] 2.3 Verify missing manifest, malformed selected entry or malformed content
  before it, missing selected-archive entry, and mismatched digest errors
  preserve the sentinel executable byte-for-byte and never report success.
- [x] 2.4 Inject archive-download and checksum-download failures, cancellation,
  and asset disappearance after detection; verify wrapped causes, captured
  same-release asset identities, no fallback/retry, and no replacement.
- [x] 2.5 Table-test unchanged archive selection for darwin/amd64,
  darwin/arm64, linux/amd64, and linux/arm64.
- [x] 2.6 Table-test unchanged version behavior for equal/newer stable versions,
  the development version, and prerelease exclusion, plus unsupported-platform
  check-only and install results.
- [x] 2.7 Verify `bosun update --check` checks same-release asset metadata only,
  downloads no assets, and returns non-zero when the required manifest asset is
  absent.
- [x] 2.8 Add a deterministic release-contract check for `.goreleaser.yaml`'s
  SHA-256 `checksums.txt` name and supported archive matrix, asserting exactly
  one well-formed entry for each generated supported archive.

## 3. Update consumer documentation

- [x] 3.1 Update `docs/commands.md`,
  `skills/onboard/resources/commands.md`, `llms.txt`, and the README update claim
  to describe fail-closed archive checksum verification.
- [x] 3.2 State that same-release checksums provide integrity but not independent
  publisher authenticity, and correct the README's existing
  `checksums.txt.pem`/`checksums.txt.sig` instructions because the release does
  not publish those signature assets.

## 4. Verify and release

- [x] 4.1 Run focused updater and command tests, including race coverage, through
  `scripts/agent-go-gate.sh` with the shared caches.
- [x] 4.2 Run the full test, build, vet, lint, module-tidiness, documentation, and
  OpenSpec validation gates through the repository's guarded workflow.
- [x] 4.3 Run a GoReleaser snapshot and assert every generated supported archive
  has exactly one SHA-256 entry in `checksums.txt`.
- [ ] 4.4 After release, inspect the live GitHub assets and complete one
  platform-appropriate self-update smoke test without weakening verification.
