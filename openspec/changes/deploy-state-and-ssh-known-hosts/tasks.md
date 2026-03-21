## 1. Deploy-relevance diff base (Bug #170)

- [ ] 1.1 Update `internal/reconcile/reconcile.go`: pass `state.CommitHash`
      (not `PullResult.CommitBefore`) as the base commit when evaluating
      post-sync hooks and deploy-path diffs
- [ ] 1.2 Handle the empty-state case: when `state.CommitHash == ""` (first
      deploy or missing state file), treat all files as changed — do not skip
      hooks or deploy-path evaluation
- [ ] 1.3 Add/update unit tests in `internal/reconcile/reconcile_test.go`:
      - Failed pipeline does not advance the diff base
      - First deploy (no prior state) treats all files as changed
      - Successful deploy uses the new commit as the next diff base

## 2. SSH known_hosts resolution (Bug #173)

- [ ] 2.1 Update `internal/reconcile/git.go`: remove `~/.ssh/known_hosts`
      from the known_hosts search order; keep only `BOSUN_SSH_KNOWN_HOSTS`
      and `/config/known_hosts`
- [ ] 2.2 Add/update unit tests in `internal/reconcile/git_test.go`:
      - `~/.ssh/known_hosts` is NOT consulted when `BOSUN_SSH_KNOWN_HOSTS`
        is unset and `/config/known_hosts` is absent
      - `BOSUN_SSH_KNOWN_HOSTS` is used when set
      - `/config/known_hosts` is used when present and env var is unset
      - Falls back to insecure mode with a warning when neither path exists
