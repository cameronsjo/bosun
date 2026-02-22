# GitOps Tool Comparison: Bosun vs Argo CD vs Flux CD

> Comparative analysis of Bosun's capabilities against the two dominant Kubernetes GitOps tools.
> Generated from Bosun's baseline OpenSpec specifications (58 requirements, ~160 scenarios).

## Architectural Context

| | Bosun | Argo CD | Flux CD |
|---|---|---|---|
| **Target Runtime** | Docker Compose on bare metal | Kubernetes clusters | Kubernetes clusters |
| **Architecture** | Single binary, file-based state | Controller + API server + repo-server | Modular controllers (source, kustomize, helm) |
| **State Model** | JSON file on disk | K8s Application CRD status | K8s CRD status conditions |
| **Deployment Model** | Push via rsync (local or SSH) | Apply via K8s API | Apply via K8s API |

Bosun operates without an API server, which means capabilities that K8s tools get "for free" (atomic applies, watch-based drift detection, CRD-based state) must be explicitly implemented. This drives Bosun's file-based locking, backup-before-deploy, and periodic drift polling.

## Reconciliation Pipeline

| Feature | Bosun | Argo CD | Flux CD |
|---|---|---|---|
| Pipeline model | Linear: lock -> git -> decrypt -> template -> backup -> deploy -> compose -> unlock | Refresh -> fetch -> render -> diff -> apply -> health | Source -> kustomize/helm -> apply |
| Trigger model | Webhook (GitHub/GitLab/Gitea/BB) + polling | Polling (3min default) + webhooks | Polling (configurable) + webhooks |
| Sync phases/waves | No (sequential single pipeline) | **Yes** (PreSync/Sync/PostSync hooks, wave ordering) | No native waves (Kustomize depends_on ordering) |
| Locking | **File-based flock** (cross-platform) | Not needed (K8s API is atomic) | Not needed (K8s API is atomic) |
| Backup before deploy | **Yes** (timestamped tar.gz) | No (K8s resources are versioned) | No (Helm release history serves this role) |

Bosun's locking and backup steps exist because Docker Compose has no API server. K8s tools get atomicity from the API server's optimistic concurrency. Bosun must prevent concurrent reconciliation with file locks and create explicit backups since rsync overwrites are destructive.

## State Tracking

| Feature | Bosun | Argo CD | Flux CD |
|---|---|---|---|
| State storage | JSON file on disk | K8s Application CRD status | K8s CRD status conditions |
| Skip logic | **HEAD comparison** (no-op on same commit) | Same-commit skip after failure | Implicit (no change = no reconcile) |
| Circuit breaker | **3 consecutive failures -> stop** | Same-commit block after failure | Retry limits -> `Stalled` condition |
| Force override | `--force` flag (per-invocation) | Force sync option | `spec.force: true` on CRD |
| Rollback | No automatic rollback | Manual rollback via UI/CLI | **Automated rollback** on Helm failure |

## Drift Detection

| Feature | Bosun | Argo CD | Flux CD |
|---|---|---|---|
| Detection model | **Periodic** (configurable interval) | **Real-time** (K8s watch API) | **Periodic** (interval-based) |
| Comparison method | Compose file -> Docker labels/state | Rendered manifests -> live cluster | Stored release -> live cluster (Helm) |
| Self-heal | No (detect + alert only) | **Yes** (`selfHeal: true`) | **Yes** (re-applies on each interval) |
| CLI for drift | `bosun drift --live --json` | `argocd app diff` | `flux get all` (status conditions) |
| Ignore rules | No | **Yes** (`ignoreDifferences`) | No built-in ignore rules |

## Deploy Resilience

| Feature | Bosun | Argo CD | Flux CD |
|---|---|---|---|
| Health checks | **3-category**: healthy/no-healthcheck=OK, unhealthy=warn, exited/restarting=error | Rich health model (6 statuses) + Lua scripts | Health assessment via K8s readiness |
| Tolerant deploy | **No `--wait`**, handles pre-existing unhealthy | Standard apply with health assessment | Standard Helm install/upgrade |
| Post-sync hooks | **Glob-based file matching** -> container restart | Sync hooks (PreSync/PostSync/SyncFail) | Not built-in |
| Retry logic | Alert throttling (1, 3, 10, 30, every 30th) | Exponential backoff (configurable) | Configurable retry count + backoff |
| Progressive delivery | No | No (requires Argo Rollouts) | No (requires Flagger) |

## Manifest / Template System

| Feature | Bosun | Argo CD | Flux CD |
|---|---|---|---|
| Primary format | Bosun manifests (provisions + services) | K8s YAML | K8s YAML |
| Helm support | **Helm-aligned charts** (Go templates + Sprig) | **Full Helm** (native) | **Full Helm** (native) |
| Kustomize | No | **Yes** (native) | **Yes** (native) |
| Jsonnet | No | **Yes** (native) | No |
| Variable interpolation | `${var}` syntax + Go templates | Helm values | Kustomize `postBuild.substitute` |
| Deep merge | **Custom merge** (union/extend/replace strategies) | K8s strategic merge patch | Kustomize patches |
| Format migration | **Built-in** (`bosun migrate helm`) | N/A | N/A |
| Plugin system | No | CMP sidecar plugins | No |

Bosun's manifest system is its most distinctive feature. While Argo/Flux delegate templating entirely to Helm/Kustomize, Bosun has its own manifest format with provisions (reusable templates), deep merge with configurable strategies, and a migration path from legacy to Helm-aligned format.

## Alerting and Notifications

| Feature | Bosun | Argo CD | Flux CD |
|---|---|---|---|
| Built-in | **Yes** (native) | **Yes** (bundled notifications controller) | **Yes** (notification-controller) |
| Providers | Discord, SendGrid, Twilio (**3**) | **20+** (Slack, Teams, PagerDuty, Opsgenie, GitHub, etc.) | **20+** (Slack, Teams, Discord, PagerDuty, etc.) |
| Alert throttling | **Progressive schedule** (1, 3, 10, 30, every 30th) | Event-based (no throttling needed) | Event-based (no throttling needed) |
| Inbound webhooks | Separate daemon endpoint | Separate | **Same controller** handles inbound + outbound |
| Cost control | **Twilio SMS only for error/critical** | N/A | N/A |

## Observability

| Feature | Bosun | Argo CD | Flux CD |
|---|---|---|---|
| Structured logging | **JSON + console** auto-detect | JSON to stdout | JSON to stdout |
| Log levels | Configurable (env var + CLI flag) | Configurable | Configurable |
| Correlation IDs | **request_id + reconcile_id** | No built-in correlation | No built-in correlation |
| Prometheus metrics | Not implemented | **Yes** (built-in) | **Yes** (built-in) |
| OpenTelemetry | Not implemented | No (proposed) | **Yes** (v2.7) |
| Grafana dashboards | No | **Yes** (community) | **Yes** (community) |

## Secret Management

| Feature | Bosun | Argo CD | Flux CD |
|---|---|---|---|
| SOPS + Age | **Built-in** (native decryption in pipeline) | No (requires KSOPS plugin) | **Built-in** (kustomize-controller) |
| Sealed Secrets | No | Via external controller | Via external controller |
| Vault integration | No | Via AVP plugin | Via External Secrets Operator |
| Cloud KMS | No | Via plugins | Via SOPS (AWS/GCP/Azure KMS) |

## Multi-Target

| Feature | Bosun | Argo CD | Flux CD |
|---|---|---|---|
| Multi-server | **Yes** (monorepo, SSH+rsync to remote) | **Yes** (multi-cluster, hundreds) | **Yes** (hub-spoke or standalone) |
| Auto-discovery | No | **ApplicationSets** (cluster/SCM/PR generators) | No built-in fleet generator |
| Multi-tenancy | No | **Projects + RBAC** | **Namespace isolation + SA impersonation** |

## Management Interfaces

| Feature | Bosun | Argo CD | Flux CD |
|---|---|---|---|
| CLI | **Yes** (`bosun` commands) | **Yes** (`argocd` CLI) | **Yes** (`flux` CLI) |
| Web UI | Planned (`add-webui` change) | **Yes** (full resource tree, diff, terminal) | No (Weave GitOps third-party) |
| REST API | Unix socket API (`/var/run/bosun.sock`) | **Full REST + gRPC** | No dedicated API (K8s API) |
| RBAC | No | **Fine-grained** (Casbin-based, v3.0+) | Kubernetes RBAC |

## Gap Analysis

### What Bosun is Missing

| Gap | Priority | Notes |
|---|---|---|
| Prometheus metrics | High | Both competitors expose `/metrics`. Table stakes for ops tooling |
| Self-healing | Medium | Argo/Flux auto-correct drift. Bosun detects + alerts but doesn't auto-remediate |
| Automated rollback | Medium | Flux does this natively for Helm. Bosun has no rollback mechanism |
| More alert providers | Low | 3 vs 20+. Slack is notably missing |
| Kustomize support | N/A | Not needed for Docker Compose target |
| Progressive delivery | Low | Even Argo/Flux need external tools for this |
| RBAC | Low | Single-user homelab tool, not a priority |

### Where Bosun Wins

| Advantage | Why It Matters |
|---|---|
| **Zero dependencies on target** | Single binary. No K8s, no controllers, no CRDs. Just Docker + Compose |
| **Native SOPS** | Built-in, not a plugin. Flux also has this; Argo CD does not |
| **Backup before deploy** | Neither competitor does this (K8s versioning makes it unnecessary, but bare metal needs it) |
| **Correlation IDs** | Neither competitor has built-in reconcile/request correlation in logs |
| **Alert throttling** | Smart progressive schedule prevents alert fatigue during extended outages |
| **Manifest migration tooling** | Built-in format detection + migration CLI. No equivalent in K8s tools |
| **File-based locking** | Solves a real problem that K8s tools don't face (concurrent rsync) |
| **Cost-aware alerting** | Twilio SMS gated to error/critical severity. Unique cost-consciousness |

## Conclusions

Bosun's 6 baseline specs cover the same core GitOps capabilities that Argo CD and Flux provide: reconciliation, state tracking, drift detection, deploy resilience, templating, alerting, and observability. The gaps (metrics, self-heal, rollback) are known and reasonable for a homelab tool's maturity stage.

The unique features (backups, locking, alert throttling, correlation IDs, cost-aware SMS) reflect the realities of bare-metal Docker Compose that K8s tools don't need to solve. These aren't missing features in K8s tools -- they're problems that don't exist in the K8s ecosystem.

The biggest strategic gap is **Prometheus metrics**. Both competitors expose rich metrics that feed Grafana dashboards. This is table stakes for any ops tool and would be the highest-impact addition to Bosun's observability story.
