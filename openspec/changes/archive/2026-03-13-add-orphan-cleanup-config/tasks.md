## 1. Configuration Surface

- [ ] 1.1 Add `RemoveOrphans` field (default `true`) to `configFile` struct in `internal/config/config.go`
- [ ] 1.2 Add `removeOrphans` field to `Config` struct and `RemoveOrphans()` getter
- [ ] 1.3 Extract `remove_orphans` in `extractConfig()` helper
- [ ] 1.4 Write unit tests for config parsing with and without the field

## 2. Environment Variable

- [ ] 2.1 Parse `BOSUN_REMOVE_ORPHANS` in daemon `ConfigFromEnv()` (bool, overrides config file)
- [ ] 2.2 Write tests for env var parsing and precedence over config file

## 3. Deploy Layer

- [ ] 3.1 Add `RemoveOrphans bool` field to `DeployOps` struct
- [ ] 3.2 Update `ComposeUpMultiple` to conditionally append `--remove-orphans` based on `RemoveOrphans`
- [ ] 3.3 Update rollback path in `ComposeUpMultipleWithRollback` to use same conditional
- [ ] 3.4 Update `ComposeUpRemote` SSH command to conditionally include `--remove-orphans`
- [ ] 3.5 Write tests for compose arg construction with `RemoveOrphans` true and false

## 4. Reconciler Wiring

- [ ] 4.1 Pass `RemoveOrphans` from reconcile config to `DeployOps` during setup
- [ ] 4.2 Verify daemon config propagation to reconciler

## 5. Documentation

- [ ] 5.1 Add `BOSUN_REMOVE_ORPHANS` to AGENTS.md env var table
- [ ] 5.2 Update `skills/onboard/resources/configuration.md` with `remove_orphans` field
- [ ] 5.3 Update `skills/onboard/resources/gitops.md` with orphan cleanup behavior
