## ADDED Requirements

### Requirement: Standalone Reconcile Configuration Parity

The one-shot `bosun reconcile` command and the daemon SHALL build the same reconciler configuration from the same environment and project config, for every field that decides which files are rendered and what is deployed. That fixed set is: repository URL and branch, deploy target, secrets files, infrastructure directory (`BOSUN_INFRA_DIR`), targets (`BOSUN_TARGETS`), state directory (`BOSUN_STATE_DIR`), post-sync hooks, hook settle delay, deploy paths, template include directory, and the dry-run flag. For each of these, both paths SHALL apply the same parsing and the same precedence between environment and project config.

Three fields in that set are known to be parsed differently today and SHALL be made to agree:

- `SECRETS_FILES`: both SHALL split on commas, trim, and drop empty entries.
- `BOSUN_SECRETS_FILE`: both SHALL apply the same splitting as `SECRETS_FILES`.
- `DRY_RUN`: both SHALL accept the same boolean spellings.

Every field of `reconcile.Config` outside that set SHALL be classified, in one place, as exactly one of:

- **allowed difference** — the two paths legitimately differ (a one-shot invocation's own directories, its `Source`, its `FORCE` flag, the reloader function identity), each with its reason;
- **unset by both** — neither path assigns it, so a comparison proves nothing; naming it keeps a field that later becomes live from hiding in the compared set.

A test SHALL assert the classification against the live struct, so that a field added to `reconcile.Config` and left unclassified fails.

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

#### Scenario: Secrets files parse identically

- **GIVEN** `SECRETS_FILES=" a.yaml , ,b.yaml "`
- **WHEN** both paths build their reconciler configuration
- **THEN** both hold exactly `a.yaml` and `b.yaml`

#### Scenario: Dry run accepts the same spellings

- **GIVEN** `DRY_RUN=yes`
- **WHEN** both paths build their reconciler configuration
- **THEN** both report a dry run

### Requirement: Standalone Reconcile Alert Suppression

`bosun reconcile` SHALL accept a `--no-alerts` flag. With the flag, the reconciler SHALL be built with no alert manager, so that no success, failure, interruption, unhealthy, or recovery alert is sent, regardless of alert environment variables or project alert configuration. With the flag, the command SHALL NOT report configured alert providers, and SHALL state that alerts are disabled.

`--dry-run` SHALL NOT imply `--no-alerts`. Without the flag, alert behavior SHALL be unchanged, for dry runs and real runs alike.

#### Scenario: Failing dry run with alerts suppressed

- **GIVEN** `DISCORD_WEBHOOK_URL` points at a reachable webhook receiver
- **AND** the repository passes authentication validation but cannot be cloned, so the run fails
- **WHEN** `bosun reconcile --dry-run --no-alerts` runs
- **THEN** the receiver gets no request

#### Scenario: Alerts configured in the project file are suppressed

- **GIVEN** no alert environment variables
- **AND** `bosun.yaml` configures `alerts.discord_webhook_url` pointing at a reachable webhook receiver
- **AND** the same failing repository
- **WHEN** `bosun reconcile --dry-run --no-alerts` runs
- **THEN** the receiver gets no request

#### Scenario: Suppression is announced, not implied

- **GIVEN** `DISCORD_WEBHOOK_URL` is set
- **WHEN** `bosun reconcile --no-alerts` runs
- **THEN** the output says alerts are disabled
- **AND** it does not list configured alert providers

#### Scenario: Dry run alone keeps alerting

- **GIVEN** the same environment and the same failing repository
- **WHEN** `bosun reconcile --dry-run` runs without `--no-alerts`
- **THEN** the receiver gets the failure alert, as before this change
