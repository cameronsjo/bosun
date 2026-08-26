# Change: Verify self-update artifacts with published checksums

## Why

Bosun releases publish SHA-256 hashes for every supported archive in
`checksums.txt`, but `bosun update` currently downloads and installs the selected
archive without comparing it to that manifest. A corrupted or inconsistently
published archive can therefore reach the executable replacement path without
an integrity check.

## What Changes

- Configure the self-updater to require the `checksums.txt` asset from the same
  detected GitHub release and verify the exact downloaded archive before it is
  decompressed or installed.
- Fail closed when the checksum asset is missing, malformed, lacks the selected
  archive, or does not match the downloaded bytes; the existing executable must
  remain unchanged and the CLI must return a non-zero error without printing a
  success result.
- Preserve current stable-release selection, platform archive selection,
  no-downgrade/development-version behavior, check-only behavior, and
  go-selfupdate's replacement/rollback semantics.
- Add deterministic, no-network coverage for the successful and fail-closed
  paths, including asset-download failures and release mutation after
  detection, across the four release targets Bosun currently publishes.
- Correct command, onboarding, and model-context documentation so checksum
  verification and its same-release trust boundary are explicit, including
  removing stale claims about checksum signature assets releases do not publish.

## Impact

- Affected specs: `self-update` (new capability)
- Affected code: `internal/update/update.go`, `internal/update/update_test.go`,
  `internal/cmd/update.go`, `internal/cmd/update_test.go`
- Release contract: `.goreleaser.yaml` must continue publishing SHA-256 entries
  in the `checksums.txt` asset for every supported archive
- Documentation consumers: `docs/commands.md`,
  `skills/onboard/resources/commands.md`, `llms.txt`, and the update claims in
  `README.md`
- All consumers: the `bosun update` install path, the `bosun update --check`
  metadata path, the `selfupdate` alias, GoReleaser's four darwin/linux and
  amd64/arm64 archives, and operators recovering from a rejected update
- Integration dependency: PR #602 (`quality/update-hardening-9qk`) changes the
  updater API to accept Cobra's context and makes command failures non-zero; the
  implementation must land after it or preserve its behavior while rebasing
