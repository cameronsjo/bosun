## ADDED Requirements

### Requirement: Standalone Reconcile Operational Parity

The one-shot `bosun reconcile` command and the daemon SHALL resolve the same value for each operational reconciler field from the same environment and project config, by calling one shared loader. The fields are: deploy mode (`BOSUN_DEPLOY_MODE`), deploy sync paths and excludes, critical containers, drift ignore rules, health gate timeout and scope, health check timeout and interval, compose up and backup timeouts, restart breaker enable, threshold and window, content-hash sync, orphan removal, and the empty-declared-state and skip-deploy-invariant overrides. An environment variable SHALL win over the project config. The `--local` flag SHALL outrank `BOSUN_DEPLOY_MODE`. Directories, `Source` and `Force` remain per-invocation differences.

#### Scenario: CLI honors an operational variable

- **GIVEN** `BOSUN_CRITICAL_CONTAINERS=["traefik"]` and `bosun.yaml` lists different critical containers
- **WHEN** both paths build their reconciler configuration
- **THEN** both hold `traefik`

#### Scenario: Project config applies when the variable is unset

- **GIVEN** `bosun.yaml` sets `health_gate_scope: declared` and no `BOSUN_HEALTH_GATE_SCOPE`
- **WHEN** both paths build their reconciler configuration
- **THEN** both hold `declared`

#### Scenario: Local flag outranks deploy mode variable

- **GIVEN** `BOSUN_DEPLOY_MODE=remote`
- **WHEN** `bosun reconcile --local` builds its configuration
- **THEN** the deploy mode is `local`
