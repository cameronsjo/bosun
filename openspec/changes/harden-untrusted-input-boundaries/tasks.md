# Tasks: Harden the reconcile and daemon boundaries against untrusted input

All ten units below are implemented on `fix/security-hardening-2026-09`, one commit each, in the order listed. The merged tree builds clean, passes `go vet ./...`, and passes `go test -race ./...` across all 20 packages.

## 1. Git SSH host key verification fails closed

- [x] 1.1 Return an error from `getHostKeyCallback` when no `known_hosts` candidate resolves and the insecure opt-out is unset
- [x] 1.2 Make an unparseable candidate terminal rather than a reason to try the next one
- [x] 1.3 Evaluate `BOSUN_SSH_INSECURE_HOST_KEY` before any candidate resolution
- [x] 1.4 Resolve the host key policy before handing agent signers to the transport, and close the agent connection when it refuses
- [x] 1.5 Surface the error through `ResolveGitAuth` so `ValidateGitAuthentication` refuses at daemon startup
- [x] 1.6 Update `docs/security.md` and the onboard skill resources

## 2. Deploy SSH channel refuses an unpinned host

- [x] 2.1 Emit `StrictHostKeyChecking=yes` instead of `accept-new` when no bosun-managed candidate resolves
- [x] 2.2 Document why trust-on-first-use is unacceptable on the channel that carries rendered secrets under a read-only ssh mount

## 3. Rollback extraction pinned against chained symlinks

- [x] 3.1 Pin the extraction root once and perform every create, rename, link, and removal relative to it
- [x] 3.2 Refuse an entry whose existing ancestor directory is a symlink
- [x] 3.3 Keep confined symlink and hardlink members extracting normally
- [x] 3.4 Add regression tests for the chained-symlink archive and for a legitimate archive with symlinks

## 4. Template rendering refuses symlinked sources

- [x] 4.1 Refuse a non-regular template source before reading it
- [x] 4.2 Skip a symlinked template entry with a warning and continue the walk
- [x] 4.3 Add a regression test proving the target's contents never reach the staging tree

## 5. Socket `/config` requires peer authorization

- [x] 5.1 Route `GET /config` through the same peer-credential check as `POST /trigger`
- [x] 5.2 Emit the webhook secret from the response builder only when explicitly requested
- [x] 5.3 Leave `/status` and `/health` resting on socket file permissions
- [x] 5.4 Fold in the receiver fail-closed gate (see task 6), which the `/config` change makes reachable

## 6. Standalone webhook receiver fails closed

- [x] 6.1 Reject every provider endpoint with 403 when no secret resolves
- [x] 6.2 Read the same `BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK` opt-out with the same strict match
- [x] 6.3 Warn at startup and on each accepted unauthenticated receipt
- [x] 6.4 Keep `--fetch-secret` failures in the fail-closed state

## 7. Deploy destination writes pinned to a root handle

- [x] 7.1 Open the deployment root once and perform directory creation, temp-file creation, rename, removal, and the directory sync relative to it
- [x] 7.2 Cover the single-file entry point, whose `os.MkdirAll` succeeded silently on an existing symlink-to-directory with no race at all
- [x] 7.3 Open the root lazily so a failed walk leaves no destination directory behind
- [x] 7.4 Add deterministic swap-fixture regression tests in `internal/fileutil/destination_test.go`
- [ ] 7.5 Commit the racing harness. During development a concurrent probe measured the unpatched code escaping 11 of 40 rounds and the patched code 0 of 150, but that harness was not committed, so the tree carries only the sequential fixtures. Either land it or record the measurement as evidence rather than as a delivered test

## 8. Restart breaker scoped to its Compose project

- [x] 8.1 Resolve the scope from a single configured target's project name, else an explicitly configured root-level project name
- [x] 8.2 Reject the directory-name fallback as a scope
- [x] 8.3 Guard at the destructive call so every caller is covered
- [x] 8.4 Preserve rather than advance restart tracking when no scope resolves
- [x] 8.5 Announce the inactive state at startup, per drift cycle, and in `bosun doctor`

## 9. Reconcile lock created owner-only

- [x] 9.1 Create the lock file with owner-only permissions, without following a final-component symlink
- [x] 9.2 Tighten an existing permissive lock through the open descriptor, warning and continuing on failure
- [x] 9.3 Create lock directories owner-only, leaving a pre-existing directory's mode alone

## 10. Operator-facing text sinks neutralize untrusted input

- [x] 10.1 Sanitize attribution and refs for all four providers on both the daemon handler and the standalone receiver
- [x] 10.2 Sanitize the socket `/trigger` source string
- [x] 10.3 Sanitize container health-check output before the drift printout and the health-gate error path
- [x] 10.4 Strip before capping, so a control character cannot survive past the truncation point
- [ ] 10.5 Commit the differential harness. During development 40,013 inputs were run through the old and new sanitizer bodies with zero drift, but that harness was not committed; `internal/log/sanitize_test.go` is a case table. Either land it or record the measurement as evidence rather than as a delivered test

## 11. Gaps the review pass found in the above

Two independent reviewers read the ten fixes back. These are their confirmed findings, fixed on the same branch.

- [x] 11.1 Pin the directory deploy at `appdata`, not at `appdata/<service>`. The single-file path pinned one level above the container-writable component; the directory path pinned at the component itself, and the pinning call resolves its own root by path, so a swap at the service directory redirected the whole rendered tree
- [x] 11.2 Refuse an escaping destination in `CopyFileUnderRootIfChanged` instead of skipping. The change decision hashed the destination by path, so a pre-placed copy behind a symlinked directory returned "no change" — a silent skip that never entered `WrittenFiles`, so the deploy invariant check could not see it
- [x] 11.3 Delete the unpinned `os.MkdirAll` before the pinned single-file write, the last path-resolved destination mutation on that path
- [x] 11.4 Sanitize the TCP `/trigger` source, which the socket handler already did. Bearer-token gating narrows who reaches it; it does not make the string trustworthy
- [x] 11.5 Fix the stale host key table in `docs/security.md`, which still described the deploy channel as trust-on-first-use and contradicted another section in the same file
- [x] 11.6 Document the three controls that shipped undocumented: template source type refusal, pinned deploy and extraction roots, and the owner-only lock

## Verification

- [x] `go build ./...` clean on the merged tree
- [x] `go vet ./...` exit 0
- [x] `go test -race ./...` exit 0, all 20 packages `ok`
- [x] `bosun doctor` exercised in three real configurations for the restart-breaker announcement
