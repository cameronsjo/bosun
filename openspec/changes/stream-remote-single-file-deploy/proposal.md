# Change: Stream remote single-file deploys over SSH

## Why

`DeployRemoteFile` copies a discovered file with `scp` and passes the remote temp path unquoted (cameronsjo/bosun#795, item 2). Under the legacy SCP protocol (the default for OpenSSH clients older than 9.0, or `scp -O`) the remote login shell evaluates that path, so a staging entry name chosen by a repository pusher can split on spaces or run a command as the SSH user before compose up and the health gate. SFTP-mode `scp` takes the same path literally, so no single quoting is correct for both protocols. The temp name is also reused across retries and opened without an exclusive create.

Replacing `scp` with a plain stream has its own gap: with no protocol framing, a dropped connection ends the remote `cat` with EOF and exit 0, so a truncated file could replace the live target. `scp` fails on a short copy today; the replacement must not regress that.

The directory transfer has a related retry problem (item 3), fixed in code by a separate PR: a remote command's own failure (full disk, permission denied) was retried as if the payload were damaged. This change writes the retry rule into the spec so both transfer legs share it.

## What Changes

- Remote single-file deployment moves from `scp` to SSH streaming: one ssh leg creates a private staging directory beside the target with an exclusive (non-`-p`) `mkdir` under umask 077; a second streams the file into it, verifies its SHA-256 against the local hash, sets the source mode masked by the remote umask, and moves it over the target.
- A missing `sha256sum` fails the attempt without retry; a checksum mismatch is retried with fresh staging; the existing target is unchanged in both cases.
- Cleanup removes only a staging directory the same attempt created, so an entry already at that name is never written through or deleted.
- Every remote path reaches the remote shell as a single quoted word, and `scp` is no longer used by the deploy path.
- The retry rule for transfer legs is stated: transient SSH errors are retried; a remote command's own non-255 failure is not, except a transfer checksum mismatch; a staged tree that fails post-extraction verification is retried with fresh staging.

## Impact

- Affected specs: `reconcile` § File Deployment.
- Affected code: `internal/reconcile/ssh.go` (`DeployRemoteFile`, removal of `scpExecCommand`), its tests (`ssh_deploy_test.go`, `ssh_quoting_test.go`, `ssh_hostkey_test.go`, `deploy_remote_dispatch_test.go`, `deploy_mkdir_errors_test.go`), and docs (`docs/gitops.md`, `docs/security.md`, `skills/onboard/resources/gitops.md`).
- Consumers: `deployRemote` in `internal/reconcile/reconcile.go`, which calls `DeployRemoteFile` for every discovered single file and for the optional Compose Manager mirror under `/boot/config`.
- Remote host requirements: single-file deploys now need POSIX `sh`, `cat`, `chmod`, `mv`, `mkdir`, `rmdir`, and `sha256sum`. Directory deploys already require `sha256sum`.
- Security posture: the deploy channel becomes ssh-only, so the host-key policy has one call path. Single-file temp files live in a freshly created `0700` directory with an unpredictable name instead of a predictable `.tmp.<nanos>` path.

## Coordination

This delta restates the union of the § File Deployment blocks from `data-driven-deploy-paths` and `add-backup-integrity-semantics`, plus `harden-ssh-trust`, which this change's PR archives first. Each of those changes restates the whole block, so whichever archives last would overwrite the others. The two unarchived changes' File Deployment blocks are aligned to this text in the same PR, so any archive order yields the same requirement.
