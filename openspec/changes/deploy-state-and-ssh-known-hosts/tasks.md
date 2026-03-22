# Tasks: Deploy state tracking and SSH known_hosts resolution

## 1. Deploy-relevance diff base (Bug #170)

- [x] 1.1 Update `internal/reconcile/reconcile.go`: pass `state.LastDeployedCommit`
      (not `PullResult.CommitBefore`) as the base commit when evaluating
      post-sync hooks and deploy-path diffs
- [x] 1.2 Handle the empty-state case: when `state.LastDeployedCommit == ""` (first
      deploy or missing state file), skip post-sync hooks entirely (no diff
      base available; existing first-deploy guard is preserved)
- [x] 1.3 Add/update unit tests in `internal/reconcile/reconcile_test.go`:
      - Failed pipeline does not advance the diff base
      - First deploy (no prior state) skips post-sync hooks
      - Successful deploy uses `state.LastDeployedCommit` as the base for the next run

## 2. SSH known_hosts resolution (Bug #173)

- [x] 2.1 Update `internal/reconcile/git.go`: remove `~/.ssh/known_hosts`
      from the known_hosts search order; keep only `BOSUN_SSH_KNOWN_HOSTS`
      and `/config/known_hosts`
- [x] 2.2 Add/update unit tests in `internal/reconcile/pure_test.go`:
      - `~/.ssh/known_hosts` is NOT consulted when `BOSUN_SSH_KNOWN_HOSTS`
        is unset and `/config/known_hosts` is absent
      - `BOSUN_SSH_KNOWN_HOSTS` is used when set
      - Falls back to `/config/known_hosts` only when env var is unset

## 3. Onboarding documentation

- [x] 3.1 Update `skills/onboard/resources/gitops.md` to reflect:
      - deploy-relevance diff uses `state.LastDeployedCommit` (last successful deploy),
        not the pull's `commit_before`
      - SSH known_hosts resolution excludes `~/.ssh/known_hosts`; only
        `BOSUN_SSH_KNOWN_HOSTS` and `/config/known_hosts` are consulted
      - Completed in this PR (commit `f145290`)
