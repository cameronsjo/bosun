## 1. HTTPS Git authentication

- [ ] 1.1 Add a single Git authentication resolver that validates
  `BOSUN_GIT_USERNAME`/`BOSUN_GIT_TOKEN`, rejects URL userinfo, and preserves
  existing anonymous HTTPS plus SSH agent/key behavior.
- [ ] 1.2 Apply the resolved Basic authentication to both `GitOps.Clone` and
  `GitOps.Pull` without transmitting it over plaintext HTTP or SSH.
- [ ] 1.3 Sanitize repository URLs and transport errors before every reconcile
  log, validation diagnostic, returned error, and daemon status response.
- [ ] 1.4 Ensure standalone `bosun reconcile` and daemon reconciliation consume
  the same authentication contract.

## 2. Tests

- [ ] 2.1 Add in-process HTTPS Git server tests proving authenticated clone and
  authenticated fetch send the expected Basic credentials.
- [ ] 2.2 Test anonymous public HTTPS remains unchanged and authentication
  rejection returns an actionable error.
- [ ] 2.3 Test missing username/token pairs, credentials on non-HTTPS URLs, and
  URL-embedded userinfo fail before network I/O.
- [ ] 2.4 Test standalone and daemon reconcile consumers use the same credential
  resolution and that `BOSUN_` credentials have no unprefixed aliases.
- [ ] 2.5 Inject recognizable username/token/userinfo values and prove they are
  absent from clone/fetch errors, logs, validation output, and daemon status.
- [ ] 2.6 Run focused tests repeatedly and under the race detector.

## 3. Documentation

- [ ] 3.1 Document `BOSUN_GIT_USERNAME` and `BOSUN_GIT_TOKEN`, the required pair,
  HTTPS-only restriction, anonymous behavior, and URL-userinfo migration in
  `AGENTS.md`, `README.md`, and `docs/gitops.md`.
- [ ] 3.2 Update `skills/onboard/resources/configuration.md` and
  `skills/onboard/resources/gitops.md` with the same contract.

## 4. Verification

- [ ] 4.1 Run `go test -p=1 ./... -count=1`.
- [ ] 4.2 Run relevant race tests, `go vet ./...`, `go build ./...`, and
  `golangci-lint run --timeout=5m --new-from-rev=origin/main ./...`.
- [ ] 4.3 Run `openspec validate add-https-git-auth --strict` and full strict
  OpenSpec validation.
