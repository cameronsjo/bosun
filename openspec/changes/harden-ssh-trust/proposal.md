# Change: Fail closed on SSH host-key trust

## Why

Bosun currently accepts an SSH Git host when neither configured `known_hosts`
path can be used, and remote deployment uses OpenSSH `accept-new` when no
candidate resolves. A missing or malformed trust file can therefore turn a
secret-bearing Git or deploy operation into an unauthenticated first contact.

## What Changes

- Make Git SSH host-key resolution fail closed when the configured candidates
  are absent or malformed.
- Keep the explicit `BOSUN_SSH_INSECURE_HOST_KEY=true` opt-out available and
  visible in diagnostics and documentation.
- Make remote `ssh` and `scp` operations use strict host-key checking when no
  configured `known_hosts` candidate resolves; retain configured-file pinning
  when one does.
- Propagate Git trust-policy errors through key-file and agent authentication
  resolution so standalone reconcile and daemon startup reject the operation
  before network access.
- Add regression coverage for absent and malformed trust files, valid trust,
  agent and key authentication, strict deploy command construction, and the
  explicit opt-out.

## Impact

- Affected specs: `reconcile` SSH authentication, repository sync, and remote
  file deployment requirements.
- Affected code: `internal/reconcile/git.go`, `internal/reconcile/ssh.go`,
  `internal/reconcile/git_auth.go`, existing host-key/auth tests, and daemon
  startup validation tests.
- Consumers: `ResolveGitAuth` and `ValidateGitAuthentication` in
  `internal/reconcile/git_auth.go`; go-git's `ssh.DefaultAuthBuilder` and
  `GitOps` in `internal/reconcile/git.go`; every remote deploy, backup,
  compose, and signal operation routed through `sshExecCommand` or
  `newSSHTransferCommand` in `internal/reconcile/ssh.go`; daemon startup's
  `ValidateConfig`; standalone `bosun reconcile`; and the SSH guidance in
  `docs/gitops.md`, `docs/security.md`,
  `skills/onboard/resources/configuration.md`, and
  `skills/onboard/resources/gitops.md`.
