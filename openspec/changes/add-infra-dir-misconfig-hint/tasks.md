# Tasks: Infra Directory Misconfiguration Hint

## 1. Candidate scanning helper

- [ ] `internal/reconcile/drift.go` — add `findComposeCandidates(dir string) []string`: read immediate children, return names of directories containing a `compose/` subdirectory; exclude dot-prefixed children; reject `compose` entries that are files
- [ ] `internal/reconcile/drift_test.go` — `TestFindComposeCandidates_SingleCandidate`
- [ ] `internal/reconcile/drift_test.go` — `TestFindComposeCandidates_MultipleCandidates`
- [ ] `internal/reconcile/drift_test.go` — `TestFindComposeCandidates_NoCandidate_Empty`
- [ ] `internal/reconcile/drift_test.go` — `TestFindComposeCandidates_SkipsDotDirsAndComposeFile`

## 2. Wire the hint into ExtractDeclaredState

- [ ] `internal/reconcile/drift.go` — when `composeDir` is absent, call `findComposeCandidates(stagingDir)` and include candidate names in the `ErrComposeDirMissing` message (relative to the infra dir); keep the bare message when none found
- [ ] `internal/reconcile/drift_test.go` — `TestExtractDeclaredState_MissingComposeDir_WithSibling_NamesCandidate`
- [ ] `internal/reconcile/drift_test.go` — `TestExtractDeclaredState_MissingComposeDir_NoSibling_BareError` (regression: existing message preserved)

## 3. Compose the BOSUN_INFRA_DIR suggestion at the call site

- [ ] `internal/reconcile/reconcile.go` — at the stage-6 `ExtractDeclaredState` call site, when the error is `ErrComposeDirMissing` and candidates exist, surface a suggested `BOSUN_INFRA_DIR=<filepath.Join(InfraSubDir, candidate)>` in the wrapped error and log
- [ ] `internal/reconcile/reconcile_test.go` (or drift integration test) — assert the surfaced error contains `BOSUN_INFRA_DIR=unraid` for `InfraSubDir="."` + candidate `unraid`
- [ ] confirm `BOSUN_ALLOW_EMPTY_DECLARED_STATE` does not suppress the failure (sentinel is still `ErrComposeDirMissing`)

## 4. Documentation

- [ ] `docs/troubleshooting.md` — extend the "Deploy reports success but files unchanged" / compose-dir-missing section with the new suggestion behavior and an example
- [ ] `skills/onboard/resources/gitops.md` — note that a missing compose dir now suggests the correct `BOSUN_INFRA_DIR`
- [ ] `docs/error-handling.md` — update the `ErrComposeDirMissing` entry to mention the candidate hint (if the sentinel is documented there)

## 5. Verification

- [ ] `go test ./internal/reconcile/` passes
- [ ] `make build` succeeds
- [ ] `openspec validate add-infra-dir-misconfig-hint --strict` passes
- [ ] Manual: point `BOSUN_INFRA_DIR=.` at a repo whose infra is under `unraid/`, run reconcile, confirm the error names `unraid` and suggests `BOSUN_INFRA_DIR=unraid`
