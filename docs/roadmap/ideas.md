# Roadmap Ideas

| Item | Priority | Effort | Notes |
|------|----------|--------|-------|
| Linux-first docs & examples | p1 | medium | Unraid-tested but generic Linux instructions |
| Rollback snapshots | p2 | medium | Auto-snapshot before deploy, `bosun mayday --rollback` |
| Values overlays | p2 | medium | `--values prod.yaml` for env-specific config |
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

## Done

| Item | Notes |
|------|-------|
| `bosun init` - interactive setup wizard | Scaffolds project, sets up age/sops, creates starter files |
| `bosun doctor` - pre-flight checks | Validates Docker, Compose, Git, Age, SOPS, uv, webhook |
| `bosun lint` - validate before deploy | Validates provisions, services, stacks with dry-run render |
| Dry-run deploys | `provision --dry-run` shows output, `--diff` shows changes |
| Dependency ordering | Lint validates deps, yacht up checks traefik |
| Provision inheritance | `webapp` bundles container, healthcheck, reverse-proxy, etc. |
| Service templates | `bosun create webapp myapp` scaffolds from templates |
| `bosun status` - health dashboard | Shows crew, infrastructure, resources, recent activity |
| `bosun log` - release history | Manifest changes, provision timestamps, deploy tags |
| Port conflict detection | Lint warns when services claim same port |
| `bosun drift` - drift detection | Compares git manifests vs running containers |
| Dependency declarations | `needs: [postgres]` auto-provisions with defaults |
