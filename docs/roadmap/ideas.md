# Roadmap Ideas

| Item | Priority | Effort | Notes |
|------|----------|--------|-------|
| `bosun init` - interactive setup wizard | p1 | medium | Zero to deployed in 2 minutes |
| `bosun doctor` - pre-flight checks | p1 | small | Docker, age key, webhook, SOPS validation |
| Dry-run deploys | p1 | medium | Show diff of what would change before applying |
| `bosun lint` - validate before deploy | p1 | small | Catch manifest errors early |
| Dependency ordering | p1 | medium | Postgres before app, traefik before everything |
| Rollback snapshots | p2 | medium | Auto-snapshot before deploy, `bosun mayday --rollback` |
| `bosun status` - health dashboard | p2 | medium | Unified view: containers, gatus, deploys, resources |
| Provision inheritance | p2 | small | `webapp` bundles `[container, reverse-proxy, healthcheck]` |
| Service templates | p2 | small | `bosun create webapp myapp` scaffolds manifest |
| `bosun log` - release history | p2 | small | Deploy timeline with git SHAs |
| `bosun drift` - drift detection | p2 | medium | Show diff between git and running state |
| Values overlays | p2 | medium | `--values prod.yaml` for env-specific config |
| Dependency declarations | p2 | small | "myapp needs postgres 17" auto-provisions sidecars |
| Port conflict detection | p2 | small | Warn before two services claim same port |
| Local dev mode | p3 | medium | `bosun dev up` watches filesystem, hot reload |
| Secret rotation helper | p3 | medium | `bosun secrets rotate` generates, re-encrypts, deploys |
| `bosun watch` - scheduled tasks | p3 | medium | Nautical cron - watches, bells, tides |
| Rolling updates | p3 | large | `bosun yacht up --rolling` - one container at a time |
| `bosun outdated` - update check | p3 | medium | Show containers with newer images available |
| `bosun backup` - volume snapshots | p3 | medium | Snapshot volumes before upgrade |
| Resource limits | p3 | small | Declare memory/CPU caps in provisions |
| Replica scaling | p4 | large | `replicas: 3` in manifest, bosun manages instances |
| Auto-prune orphans | p4 | medium | Remove containers no longer in manifest (opt-in) |
| Plugin system | p4 | large | Lifecycle hooks in `bosun/plugins/` |
