## 1. Config Loading

- [ ] 1.1 Add `PostSyncHooks` field to `configFile` struct (YAML tag: `post_sync_hooks`)
- [ ] 1.2 Add `postSyncHooks` field to `Config` struct with `PostSyncHooks()` getter
- [ ] 1.3 Add `extractPostSyncHooks()` helper following `extractAlertConfig` pattern
- [ ] 1.4 Wire into `Load()` alongside existing extract calls

## 2. CLI Reconcile Integration

- [ ] 2.1 Load project config hooks in `runReconcile()` via `config.Load()`
- [ ] 2.2 Add `BOSUN_POST_SYNC_HOOKS` env var override (JSON, same as daemon)

## 3. Homelab Configuration

- [ ] 3.1 Add `post_sync_hooks` stanza to homelab `bosun.yaml` for Traefik

## 4. Testing

- [ ] 4.1 Add `TestPostSyncHooksFromConfig` — YAML parsing populates hooks
- [ ] 4.2 `make test` passes
- [ ] 4.3 `make build` succeeds
