# Change: Harden the reconcile and daemon boundaries against untrusted input

## Why

A September 2026 scan of `internal/daemon` and `internal/reconcile` produced nineteen verified findings. Ten are fixed here. They are not one bug repeated; they are one *shape* repeated — a boundary that accepts input from a party the code does not trust, and then resolves, renders or logs that input as though it did.

Four of them let an attacker escape a containment boundary the code already claimed to defend:

- **Host key verification failed open on both SSH channels.** The git channel fell back to ignoring the host key whenever no `known_hosts` candidate existed or one failed to parse, which is the shipped default. The deploy channel emitted `accept-new`, which relies on persisting a pin that the shipped compose file makes impossible by mounting the ssh directory read-only — so every reconcile was a first connection. One channel pulls the tree bosun deploys as root; the other streams every rendered SOPS value to whichever host answers.
- **Three path-resolution boundaries checked lexically and then wrote by path.** The rollback extractor, the deploy writer and the template renderer each validated a path with string arithmetic and then handed the same string back to the operating system, which resolved it again through whatever symlinks had appeared in between. A compromised deploy target could write anywhere on the control-plane host; a container could redirect a root-privileged deploy write out of its own volume; a repository committer could read the daemon's age key.

Two more collapsed an authorization model:

- **The socket `/config` route issued a control credential to any connector.** The webhook secret it returned is exactly what authorizes a forced reconcile on the HTTP listener, so the documented connect-versus-mutate split was bypassable by reading the secret and signing a trigger instead.
- **The restart breaker's project scope was never populated**, and an empty scope matched every container, turning a daemon that holds the Docker socket into a confused deputy that could stop arbitrary co-hosted services.

The rest are the same shape at lower severity: a lock file whose permissions let any local principal block every deploy, and three text sinks — webhook attribution, pushed refs, and container health-check output — that carried attacker-chosen control characters into operator-facing logs and alerts.

The common repair is to make each boundary *resolve against the thing it is protecting* rather than against a string, and to fail closed when it cannot.

## What Changes

- **Host key verification fails closed on both SSH channels.** Git SSH returns an error that surfaces at daemon startup; the deploy channel refuses an unpinned host while leaving openssh's own default known-hosts files in play. `BOSUN_SSH_INSECURE_HOST_KEY=true` remains the single opt-out. **BREAKING**: an SSH repository or deploy target with no pinned key and no opt-out now refuses rather than proceeding unverified.
- **Path resolution is pinned, not lexical.** Rollback extraction, deploy destination writes (both the directory and single-file entry points) and template rendering resolve through a pinned root handle or refuse a non-regular source, so a symlink planted between the check and the write cannot redirect the operation outside its root.
- **The socket `/config` route requires the same peer authorization as `/trigger`**, and the response builder emits the webhook secret only when explicitly asked.
- **The standalone webhook receiver fails closed** when no secret is resolved, reading the same opt-out variable the daemon already uses, because it forwards over the peer-authorized socket which never re-applies the daemon's own webhook gate.
- **The restart breaker refuses to act without a resolved Compose project scope**, announced at startup, per drift cycle, and in `bosun doctor`. An explicitly configured root-level `project_name` is honoured; the directory-name fallback is not, because it names no project Docker labels containers with.
- **The reconcile lock is owner-only** and is opened without following a final-component symlink; an existing permissive lock has its mode tightened on next acquire.
- **Every operator-facing text sink neutralizes untrusted input.** Webhook attribution and refs from all four providers, the socket trigger source, and container health-check output are stripped of control, formatting and separator characters before reaching a log, an error string or an alert body.

## Known residuals

Two are worth naming rather than implying the boundaries are now total.

- **The skip-path gate closes the reported scenario, not the full race.** `CopyFileUnderRootIfChanged` now resolves its destination through the pinned handle before comparing, so a pre-placed copy behind a symlinked directory errors instead of reporting "no change". The comparison itself still reads the destination by path, so a swap landing between the gate and the comparison is still read by path. Closing that fully changes a shared signature and five existing call sites, and is left as follow-up work.
- **`DeployLocal` keeps one unpinned directory creation** before the content-hash copy, as the `localFS` test seam. It cannot create a tree outside `appdata`: the untrusted component is `<service>`, and if that is already a symlink the creation makes nothing and the pinned copy then refuses.

## Impact

- Affected specs: `reconcile`, `daemon-security`
- Affected code: `internal/reconcile`, `internal/daemon`, `internal/fileutil`, `internal/log`, `internal/cmd`, `internal/config`
- **Operator action on upgrade**: an SSH deployment with no pinned host key must add one or set the opt-out. A daemon running the restart breaker without a resolved project scope will announce that the breaker is inactive until `project_name` is set.

## Findings not fixed here

Five of the nineteen were attempted and declined, because the fix was correct but the surrounding change was not yet right. Each is recorded with what a passing revision requires: the sprig environment-function removal (operator guidance names a remedy that does not exist in the chart path), the cleartext TCP guard (shipped a security opt-out pre-enabled in a copy-me compose file), the rollback live-write pinning (undeclared change to a missing-appdata-root restore), the per-target backup namespace (a single-target install loses sight of its pre-upgrade anchors), and the backup listing bound (capping remote stderr narrows the retry window).
