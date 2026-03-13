## MODIFIED Requirements

### Requirement: Post-Sync Container Restart Hooks

The reconciler SHALL support configurable post-sync hooks that restart containers
when specific file paths change during deployment.

Hooks SHALL be configured via `PostSyncHooks` with fields: `Paths` (glob patterns
matched against changed files relative to repo root), `Action` (the action to
perform, currently only `restart`), and `Container` (the container name to act on).

Hooks SHALL be declarable in the project config file (`bosun.yaml`) under the
`post_sync_hooks` key as a YAML array. Each entry SHALL have `paths` (list of
glob patterns), `action` (string), and `container` (string) fields.

The `BOSUN_POST_SYNC_HOOKS` environment variable (JSON array) SHALL override
hooks from the config file when set. This applies to both daemon and CLI modes.

The `bosun reconcile` CLI command SHALL load hooks from the project config file.
If `config.Load()` fails (e.g., no project root found), hooks SHALL be empty
with no error (reconcile can run without a project config).

After a successful deployment, the reconciler SHALL diff the previous and current
commits, match changed files against hook glob patterns, and execute matching
actions. Each container SHALL be restarted at most once per deployment, even if
multiple patterns match.

Hooks SHALL only execute when a Docker client is available, dry run is false, hooks
are configured, and a previous commit exists (not on first deploy).

Glob patterns SHALL support `**` for recursive directory matching.

#### Scenario: Hooks declared in bosun.yaml

- **WHEN** `bosun.yaml` contains a `post_sync_hooks` entry with paths `["traefik/conf.d/**"]`, action `restart`, and container `traefik`
- **THEN** `config.Load()` returns a `Config` with one `PostSyncHook`
- **AND** the reconciler uses this hook during post-deploy evaluation

#### Scenario: Environment variable overrides config file hooks

- **WHEN** `bosun.yaml` declares hooks for container `traefik`
- **AND** `BOSUN_POST_SYNC_HOOKS` is set with hooks for container `authelia`
- **THEN** only the `authelia` hook from the environment variable is used
- **AND** the `traefik` hook from the config file is ignored

#### Scenario: CLI reconcile loads hooks from project config

- **WHEN** `bosun reconcile` is invoked
- **AND** `bosun.yaml` declares post-sync hooks
- **THEN** the hooks are loaded and passed to the reconciler

#### Scenario: CLI reconcile without project config

- **WHEN** `bosun reconcile` is invoked
- **AND** no `bosun.yaml` is found
- **THEN** hooks are empty and reconciliation proceeds without post-sync hooks

#### Scenario: Daemon loads hooks from project config

- **WHEN** the daemon starts via `ConfigFromEnv()`
- **AND** `bosun.yaml` declares post-sync hooks
- **AND** `BOSUN_POST_SYNC_HOOKS` is not set
- **THEN** the hooks from `bosun.yaml` are loaded into the reconcile config
- **AND** the daemon uses these hooks during reconciliation

#### Scenario: Daemon env var overrides project config hooks

- **WHEN** the daemon starts via `ConfigFromEnv()`
- **AND** `bosun.yaml` declares hooks for container `traefik`
- **AND** `BOSUN_POST_SYNC_HOOKS` is set with hooks for container `authelia`
- **THEN** only the `authelia` hook from the environment variable is used
- **AND** the `traefik` hook from the config file is ignored

#### Scenario: Daemon without project config

- **WHEN** the daemon starts via `ConfigFromEnv()`
- **AND** no `bosun.yaml` is found
- **THEN** hooks are empty unless `BOSUN_POST_SYNC_HOOKS` provides them

#### Scenario: Container restarted after config change

- **WHEN** a hook is configured with paths `["traefik/conf.d/**"]` and container `traefik`
- **AND** a deployment changes `traefik/conf.d/dynamic.yml`
- **THEN** the reconciler restarts the `traefik` container after compose up
- **AND** logs the hook execution

#### Scenario: No restart when unrelated files change

- **WHEN** a hook is configured with paths `["traefik/conf.d/**"]` and container `traefik`
- **AND** a deployment only changes `docker-compose.yml`
- **THEN** the reconciler does not restart the `traefik` container

#### Scenario: Unsupported hook action skipped

- **WHEN** a hook has an action other than `restart`
- **THEN** the hook is skipped with a warning log

#### Scenario: First deploy skips hooks

- **WHEN** there is no previous commit (first deployment)
- **THEN** post-sync hooks are not evaluated
