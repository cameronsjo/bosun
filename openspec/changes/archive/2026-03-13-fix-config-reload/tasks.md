## 1. Config Layer

- [x] 1.1 Add `config.LoadFrom(dir string) (*Config, error)` — loads config from a specific directory path (skips FindRoot)
- [x] 1.2 Write tests for LoadFrom (valid dir, no config file, malformed YAML)

## 2. Reconciler Config

- [x] 2.1 Add `PostSyncHooksFromEnv bool` and `HookSettleDelayFromEnv bool` to `reconcile.Config`
- [x] 2.2 Set these flags in daemon `ConfigFromEnv()` when env vars are present
- [x] 2.3 Set these flags in CLI `runReconcile()` when env vars are present

## 3. Reconcile Pipeline

- [x] 3.1 Add `reloadProjectConfig()` method on Reconciler (uses `ConfigReloaderFunc` to break import cycle)
- [x] 3.2 Call it after git sync (step 2) and before secrets decryption (step 4)
- [x] 3.3 Skip reload for fields where env var override is active
- [x] 3.4 Log when hooks or settle delay changed ("Reloaded project config from repo. Hooks: N, SettleDelay: Xs")
- [x] 3.5 On parse failure, log warning and keep existing config
- [x] 3.6 Write tests for reloadProjectConfig (hooks changed, no change, parse error, env override, nil reloader, no config)

## 4. Documentation

- [x] 4.1 Update onboard skill resource (`skills/onboard/resources/gitops.md`) to mention config reload
- [x] 4.2 Update AGENTS.md gotchas section — updated "config graceful degradation" note to mention runtime reload
