## ADDED Requirements

### Requirement: Per-Service Image Update Policy

The reconciler SHALL support per-service image update policies that control whether a service's image is pulled from the registry before `docker compose up`.

Two policies SHALL be supported:

- **pinned** (default): Deploy exactly the image declared in the compose file. No explicit pull is performed; Docker uses the locally cached image. This preserves current Bosun behavior.
- **auto**: Pull the latest image from the registry before compose up, even if a local copy exists. This replaces Watchtower for Bosun-managed services.

Policies SHALL be configured via `image_policies` in `bosun.yaml` as a map of compose service names to policy values:

```yaml
image_policies:
  traefik: pinned
  homepage: auto
  vaultwarden: auto
```

Services not listed in `image_policies` SHALL use the global default policy. The global default SHALL be `pinned` unless overridden by `BOSUN_IMAGE_POLICY` environment variable.

`BOSUN_IMAGE_POLICY` SHALL accept `pinned` or `auto` as values. Invalid values SHALL log a warning and fall back to `pinned`. When set, it changes the default for services not explicitly listed in `image_policies`; it does NOT override per-service entries.

The effective policy for a service SHALL be resolved in order:
1. Per-service entry in `image_policies` (highest priority)
2. `BOSUN_IMAGE_POLICY` env var (global default override)
3. `pinned` (hardcoded default)

Image policies SHALL always be reloaded from the repo's `bosun.yaml` after each git pull. `BOSUN_IMAGE_POLICY` provides a global default that per-service entries in the config file override.

#### Scenario: Service with auto policy gets pulled

- **WHEN** `image_policies` maps service `homepage` to `auto`
- **AND** a reconciliation triggers
- **THEN** the pre-pull stage runs `docker compose pull homepage` before compose up
- **AND** the latest image is fetched from the registry

#### Scenario: Service with pinned policy is not pulled

- **WHEN** `image_policies` maps service `traefik` to `pinned`
- **AND** a reconciliation triggers
- **THEN** the pre-pull stage does NOT include `traefik` in the pull command
- **AND** Docker uses the locally cached image during compose up

#### Scenario: Service without explicit policy uses global default

- **WHEN** `image_policies` does not include service `redis`
- **AND** `BOSUN_IMAGE_POLICY` is not set
- **THEN** `redis` uses the `pinned` default
- **AND** is not included in the selective pull

#### Scenario: Global default override via env var

- **WHEN** `BOSUN_IMAGE_POLICY` is set to `auto`
- **AND** `image_policies` maps `traefik` to `pinned`
- **AND** `homepage` has no explicit policy
- **THEN** `traefik` uses `pinned` (per-service overrides env var)
- **AND** `homepage` uses `auto` (inherits env var default)

#### Scenario: Invalid env var value falls back to pinned

- **WHEN** `BOSUN_IMAGE_POLICY` is set to `always`
- **THEN** a warning is logged with the invalid value
- **AND** the global default remains `pinned`

#### Scenario: Policies reloaded from repo after git pull

- **WHEN** the repo's `bosun.yaml` adds `homepage: auto` to `image_policies`
- **AND** the next reconciliation pulls this change
- **THEN** the reloaded config includes the updated `image_policies`
- **AND** `homepage` is included in the selective pull for that reconciliation

#### Scenario: No auto services skips selective pull

- **WHEN** all services resolve to `pinned` policy
- **THEN** the selective pull stage is skipped entirely
- **AND** compose up proceeds without an explicit pull

#### Scenario: Selective pull takes precedence over blanket pre-pull

- **WHEN** `add-image-prepull` is configured
- **AND** `image_policies` are also configured with per-service entries
- **THEN** selective pull takes precedence — only `auto` services are pulled
- **AND** `add-image-prepull` does NOT cause a blanket pull of all services
- **AND** if all services resolve to `pinned`, the pull stage is skipped entirely
- **NOTE** `add-image-prepull` acts as the fallback only when no `image_policies` are configured

#### Scenario: Pull failure for auto service aborts pipeline

- **WHEN** `image_policies` maps service `homepage` to `auto`
- **AND** `docker compose pull homepage` fails (e.g., registry unreachable)
- **THEN** the pipeline aborts with an error identifying `homepage` as the failed service
- **AND** compose up is NOT executed
- **AND** a throttled failure alert is sent

#### Scenario: Invalid policy value rejected at config load

- **WHEN** `image_policies` contains `homepage: always`
- **THEN** config loading rejects the value with a validation error
- **AND** the error message lists `always` as the invalid value and `pinned`, `auto` as valid options

#### Scenario: Unknown service name in policy map logs warning

- **WHEN** `image_policies` maps `nonexistent-service` to `auto`
- **AND** no compose file declares a service named `nonexistent-service`
- **THEN** a warning is logged identifying the unknown service name
- **AND** reconciliation continues without error
- **AND** the unknown entry is ignored during selective pull

#### Scenario: Selective pull for auto services in daemon

- **WHEN** the daemon triggers reconciliation
- **AND** 2 of 10 declared services have `auto` policy
- **THEN** the pre-pull runs `docker compose pull <svc1> <svc2>` with only the auto services
- **AND** the remaining 8 pinned services are not pulled

#### Scenario: Selective pull skipped in dry run

- **WHEN** `DryRun` is true
- **AND** `auto` services are configured
- **THEN** the selective pull is skipped
- **AND** a debug log indicates the skip

#### Scenario: Selective pull runs on remote deploy via SSH

- **WHEN** the deployment target is a remote host
- **AND** `auto` services are configured
- **THEN** the selective pull runs `docker compose pull <auto-services>` on the remote host via SSH before compose up
- **AND** the auto/pinned guarantees are consistent across local and remote deploy targets

### Requirement: Image Policy in Drift Context

The drift report SHALL include the image policy for each service when reporting drift items. This provides operators with context about whether an `image_mismatch` is expected (auto service pulled a newer image between reconciliations) or unexpected (pinned service has wrong image).

The `DeclaredService` struct SHALL include an `ImagePolicy` field populated from the resolved per-service policy during declared state extraction.

#### Scenario: Drift report includes policy for image mismatch

- **WHEN** a drift check finds `image_mismatch` for service `homepage`
- **AND** `homepage` has `auto` policy
- **THEN** the drift item includes `image_policy: auto`
- **AND** the drift display indicates the mismatch may be expected due to auto-pull policy

#### Scenario: Drift report includes policy for pinned service mismatch

- **WHEN** a drift check finds `image_mismatch` for service `traefik`
- **AND** `traefik` has `pinned` policy
- **THEN** the drift item includes `image_policy: pinned`
- **AND** the drift display treats this as an unexpected mismatch

## MODIFIED Requirements

### Requirement: Pipeline Orchestration

The reconciler SHALL execute stages in this fixed order:

1. Acquire lock
2. Git repository sync
3. Load deploy state and evaluate skip/circuit-breaker logic
4. Decrypt secrets (SOPS)
5. Render templates (Go text/template + Sprig)
6. Extract declared state from rendered compose
7. Resolve image policies per service
8. Pull images for `auto` services (`docker compose pull <services...>`)
9. Create configuration backup
10. Deploy files (local or remote)
11. Run `docker compose up`
12. Clean up staging directory
13. Critical container health gate (if configured)
14. Execute post-sync hooks
15. Post-deploy verification (drift check)
16. Record successful deployment in state file
17. Release lock

A failure at any stage SHALL abort the remaining stages and release the lock. The health gate (stage 13) failing SHALL trigger rollback before aborting.
The lock SHALL always be released via defer, even on panic.

#### Scenario: Full pipeline succeeds

- **WHEN** a reconciliation is triggered and a new commit is available
- **THEN** all stages execute in order
- **AND** the deploy state file records the deployed commit
- **AND** a success alert is sent

#### Scenario: Pipeline aborts on stage failure

- **WHEN** secret decryption fails
- **THEN** template rendering, backup, deploy, and compose stages are skipped
- **AND** a throttled failure alert is sent
- **AND** the lock is released

#### Scenario: Dry run mode

- **WHEN** `DryRun` is true
- **THEN** image pull, backup, deploy, compose up, health gate, post-sync hooks, and post-deploy verification are skipped
- **AND** template rendering still executes to validate templates

#### Scenario: Health gate failure triggers rollback

- **WHEN** compose up succeeds but a critical container fails the health gate
- **THEN** the reconciler triggers rollback to the backup compose files
- **AND** the deployment is NOT recorded as successful
- **AND** a failure alert is sent
- **AND** the lock is released

#### Scenario: Image policy resolution runs after declared state extraction

- **WHEN** declared state is extracted from rendered compose files
- **THEN** image policies are resolved per service before the pull stage
- **AND** only services with `auto` policy are included in the selective pull
