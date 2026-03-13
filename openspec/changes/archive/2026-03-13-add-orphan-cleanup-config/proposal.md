# Change: Orphan Container Cleanup Configuration

## Why

When a service is removed from a Bosun-managed compose template, `docker compose up -d` alone does not stop the old container. The deleted service becomes an orphan: still running, still consuming resources, invisible to GitOps. Docker Compose natively supports `--remove-orphans` to handle this, and Bosun already passes the flag unconditionally in all compose-up call sites. However, there is no way to disable it. In shared or cautious environments (e.g., services managed by multiple tools on the same Docker host), unconditional orphan removal can destroy containers that Bosun does not own. Making this configurable lets operators opt out when the flag would be destructive.

## What Changes

- Add `remove_orphans` field to `bosun.yaml` config (default: `true`, preserving current behavior)
- Add `BOSUN_REMOVE_ORPHANS` environment variable (overrides config file)
- Add `RemoveOrphans` field to `DeployOps` struct
- Conditionally append `--remove-orphans` to compose-up args based on the config value
- Update all compose-up call sites in the reconciler: `ComposeUpMultiple`, rollback path in `ComposeUpMultipleWithRollback`, and `ComposeUpRemote`

## Impact

- Affected specs: `reconcile`
- Affected code:
  - `internal/config/config.go` — new `remove_orphans` YAML field, getter, extraction
  - `internal/reconcile/deploy.go` — conditional `--remove-orphans` in `ComposeUpMultiple`, `ComposeUpMultipleWithRollback`, `ComposeUpRemote`
  - `internal/reconcile/reconcile.go` — pass config to `DeployOps`
  - `internal/cmd/emergency.go` — `runComposeUp()` in mayday restore (standalone, not reconciler-managed; keeps hardcoded `--remove-orphans` because emergency restore should always clean up)
- All consumers:
  - `deploy.go:875` — `ComposeUpMultiple` appends `--remove-orphans` unconditionally
  - `deploy.go:972` — rollback path in `ComposeUpMultipleWithRollback` appends `--remove-orphans` unconditionally
  - `deploy.go:1047` — `ComposeUpRemote` SSH command includes `--remove-orphans` unconditionally
  - `emergency.go:730` — `runComposeUp()` in mayday restore (standalone helper, not part of reconciler; intentionally unchanged)
  - `config.go:112` — comment referencing `--remove-orphans` on `ProjectName`
  - `provision.go:215` — comment referencing `--remove-orphans` on project name
