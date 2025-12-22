# Roadmap Ideas

| Item | Priority | Effort | Notes |
|------|----------|--------|-------|
| `bosun init` - interactive setup wizard | p1 | medium | Zero to deployed in 2 minutes |
| `bosun doctor` - pre-flight checks | p1 | small | Docker, age key, webhook, SOPS validation |
| Dry-run deploys | p1 | medium | Show diff of what would change before applying |
| Rollback snapshots | p2 | medium | Auto-snapshot before deploy, `bosun mayday --rollback` |
| `bosun status` - health dashboard | p2 | medium | Unified view: containers, gatus, deploys, resources |
| Provision inheritance | p2 | small | `webapp` bundles `[container, reverse-proxy, healthcheck]` |
| Service templates | p2 | small | `bosun create webapp myapp` scaffolds manifest |
| Local dev mode | p3 | medium | `bosun dev up` watches filesystem, hot reload |
| Secret rotation helper | p3 | medium | `bosun secrets rotate` generates, re-encrypts, deploys |
| Plugin system | p4 | large | Lifecycle hooks in `bosun/plugins/` |
