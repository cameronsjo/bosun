## ADDED Requirements

### Requirement: Standalone Reconcile Configuration Parity

The one-shot `bosun reconcile` command SHALL read the same environment variables as the daemon for every configuration field that determines which files are rendered into staging and where: repository URL and branch, secrets files, infrastructure directory (`BOSUN_INFRA_DIR`), targets (`BOSUN_TARGETS`), state directory (`BOSUN_STATE_DIR`), post-sync hooks, hook settle delay, deploy paths, and template include directory. For each of these, the CLI SHALL apply the daemon's precedence between environment and project config.

A test SHALL build the reconciler configuration through both the CLI path and the daemon path, and compare every field of `reconcile.Config`. Each field on which the two paths are allowed to differ SHALL be listed in that test with its reason. The test SHALL run at least two cases, so that matching values cannot hide a precedence difference:

- **override**: every field that both the environment and the project config can set gets a *different* value in each, so the environment must win;
- **fallback**: the same project config with those environment variables unset, so the project-config value must win.

The test SHALL fail when:

- two paths differ on a field that is not listed, in either case, or
- `reconcile.Config` gains a field that the test neither compares nor lists.

#### Scenario: CLI honors the infrastructure directory

- **GIVEN** `BOSUN_INFRA_DIR=unraid` in the environment
- **WHEN** `bosun reconcile` builds its reconciler configuration
- **THEN** `InfraSubDir` is `unraid`
- **AND** it equals the `InfraSubDir` the daemon builds from the same environment

#### Scenario: Environment overrides project config identically

- **GIVEN** `bosun.yaml` sets `deploy_paths` and `template_include_dir`
- **AND** `BOSUN_DEPLOY_PATHS` and `BOSUN_TEMPLATE_INCLUDE_DIR` are set to different values
- **WHEN** both paths build their reconciler configuration
- **THEN** both use the environment values

#### Scenario: Project config applies identically when the environment is unset

- **GIVEN** the same `bosun.yaml` and no `BOSUN_DEPLOY_PATHS` or `BOSUN_TEMPLATE_INCLUDE_DIR`
- **WHEN** both paths build their reconciler configuration
- **THEN** both use the `bosun.yaml` values

#### Scenario: Unlisted divergence fails the parity test

- **GIVEN** the CLI path stops reading an environment variable that the daemon still reads, for a field not on the allowed-difference list
- **WHEN** the parity test runs
- **THEN** it fails and names the field together with both values

#### Scenario: Unclassified new field fails the parity test

- **GIVEN** a new field is added to `reconcile.Config`
- **WHEN** the parity test runs without that field being compared or listed
- **THEN** it fails and names the field

### Requirement: Standalone Reconcile Alert Suppression

`bosun reconcile` SHALL accept a `--no-alerts` flag. With the flag, the reconciler SHALL be built with no alert manager, so that no success, failure, interruption, unhealthy, or recovery alert is sent, regardless of alert environment variables or project alert configuration. The command SHALL report that alerts are disabled.

`--dry-run` SHALL NOT imply `--no-alerts`. Without the flag, alert behavior SHALL be unchanged, for dry runs and real runs alike.

#### Scenario: Failing dry run with alerts suppressed

- **GIVEN** `DISCORD_WEBHOOK_URL` points at a reachable webhook receiver
- **AND** the repository cannot be synchronized, so the run fails
- **WHEN** `bosun reconcile --dry-run --no-alerts` runs
- **THEN** the receiver gets no request

#### Scenario: Alerts configured in the project file are suppressed

- **GIVEN** no alert environment variables
- **AND** `bosun.yaml` configures `alerts.discord_webhook_url` pointing at a reachable webhook receiver
- **AND** the repository cannot be synchronized, so the run fails
- **WHEN** `bosun reconcile --dry-run --no-alerts` runs
- **THEN** the receiver gets no request

#### Scenario: Dry run alone keeps alerting

- **GIVEN** the same environment and the same failing repository
- **WHEN** `bosun reconcile --dry-run` runs without `--no-alerts`
- **THEN** the receiver gets the failure alert, as before this change
