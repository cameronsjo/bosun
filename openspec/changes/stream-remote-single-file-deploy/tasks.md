## 1. Single-file deploy over SSH

- [ ] 1.1 Hash and stat the source once before the retry loop; rewind it before each attempt.
- [ ] 1.2 Per attempt, create a private staging directory beside the target with an exclusive `mkdir` under umask 077; arm cleanup only after it succeeds.
- [ ] 1.3 Stream the file into the staging directory, verify SHA-256 (exit 78 without `sha256sum`, exit 79 on mismatch), set the source mode masked by the remote umask, move it over the target, and remove the staging directory.
- [ ] 1.4 Classify stream failures with the shared remote-exit classifier.
- [ ] 1.5 Tolerate a `chmod` failure only for the Compose Manager mirror under `/boot/config`, and say so in code and docs.
- [ ] 1.6 Remove `scpExecCommand` and every scp reference in the deploy path.

## 2. Regression coverage

- [ ] 2.1 Rewrite the scp-based `DeployRemoteFile` tests and shims for the SSH stream.
- [ ] 2.2 Cover shell metacharacters in the remote path with a shim that evaluates the remote command in a real shell.
- [ ] 2.3 Cover a truncated stream, fresh staging per attempt, mode preservation, a pre-existing staging entry, a missing `sha256sum`, and host-key options on both ssh legs.
- [ ] 2.4 Cover each new error-return branch with fault injection.

## 3. Documentation

- [ ] 3.1 Update `docs/gitops.md`, `docs/security.md`, and `skills/onboard/resources/gitops.md` for the ssh-only deploy channel and the single-file remote requirements.
