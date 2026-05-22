## Tasks: Infra Directory Misconfiguration Hint

## 1. Candidate scanning helper

- [x] `internal/reconcile/drift.go` — add `findComposeCandidates(dir string) []string`: read immediate children, return names of directories containing a `compose/` subdirectory; exclude dot-prefixed children; reject `compose` entries that are files
- [x] `internal/reconcile/drift_test.go` — `TestFindComposeCandidates_SingleCandidate`
- [x] `internal/reconcile/drift_test.go` — `TestFindComposeCandidates_MultipleCandidates`
- [x] `internal/reconcile/drift_test.go` — `TestFindComposeCandidates_NoCandidate_Empty`
- [x] `internal/reconcile/drift_test.go` — `TestFindComposeCandidates_SkipsDotDirsAndComposeFile`

## 2. Wire the hint into ExtractDeclaredState

- [x] `internal/reconcile/drift.go` — when `composeDir` is absent, call `findComposeCandidates(stagingDir)` and include candidate names in the `ErrComposeDirMissing` message; keep the bare message when none found
- [x] `internal/reconcile/drift_test.go` — `TestExtractDeclaredState_MissingComposeDir_WithSibling_NamesCandidate`
- [x] No-sibling bare-error regression covered by existing `TestExtractDeclaredState_NoComposeDir` (empty dir → no candidates → bare `ErrComposeDirMissing`)

## 3. Compose the BOSUN_INFRA_DIR suggestion at the call site

- [x] `internal/reconcile/reconcile.go` — add `suggestInfraDir(infraSubDir, candidates)` (joins each candidate with `InfraSubDir`); at the stage-6 `ErrComposeDirMissing` case, append the suggestion to the surfaced error
- [x] `internal/reconcile/reconcile_test.go` — `TestSuggestInfraDir` (table: none / single-root / single-nested / multiple)
- [x] Failure still keyed on `ErrComposeDirMissing`; `BOSUN_ALLOW_EMPTY_DECLARED_STATE` does not suppress it (unchanged sentinel + switch arm)

## 4. Documentation

- [x] `docs/troubleshooting.md` — extend the "Deploy reports success but files unchanged" section with the suggestion behavior and an example
- [x] `skills/onboard/resources/gitops.md` — note that a missing compose dir now suggests the correct `BOSUN_INFRA_DIR`
- [x] `docs/error-handling.md` — update the `ErrComposeDirMissing` entry to mention the candidate hint

## 5. Verification

- [x] `go test ./internal/reconcile/` passes
- [x] `make build` succeeds
- [x] `openspec validate add-infra-dir-misconfig-hint --strict` passes
- [ ] Manual: point `BOSUN_INFRA_DIR=.` at a repo whose infra is under `unraid/`, run reconcile, confirm the error names `unraid` and suggests `BOSUN_INFRA_DIR=unraid` (post-merge / homelab validation)
