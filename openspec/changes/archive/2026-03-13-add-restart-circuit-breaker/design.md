# Design: Restart Circuit Breaker

## Decisions

### D1: Delta-based detection (not absolute)

Docker's `RestartCount` is monotonically increasing from container creation. We track the count at each drift check and compute the delta. If the delta exceeds the threshold within the configured window, the container is restart-looping.

**Rationale:** A container with `RestartCount: 50` that hasn't restarted in weeks is stable. Only recent restart velocity matters.

### D2: Stop on trip, don't remove

When tripped, stop the container gracefully (10s timeout). Don't remove it — the operator needs `docker logs` to diagnose. The container stays stopped until manual intervention or the next successful deploy.

**Rationale:** Removing destroys logs and state. Stopping is reversible and preserves debugging context.

### D3: Detection in drift check loop

Restart detection runs as an enrichment pass within `RunDriftCheck()`, not in the post-deploy health verification. Only containers in "running" or "restarting" state are inspected.

**Rationale:** Restart loops are a runtime concern, not a deploy concern. The drift check loop already runs periodically and has alert infrastructure. Post-deploy health verification has its own timeout-based detection.

### D4: Per-container state in deploy state file

Track restart counts in `DeployState.RestartTracking` keyed by service name. Entries are created on first observation and cleaned up when the container stabilizes (no restart delta for one full window) or is removed.

**Rationale:** Reuses existing state persistence. Per-service keying matches drift alerting patterns.

### D5: Defaults — 5 restarts in 10 minutes

Threshold: 5 restart count increase. Window: 10 minutes. These are conservative — a healthy container with a restart policy might restart once during a transient failure. Five restarts in 10 minutes strongly signals a crash loop.

## Configuration

| Env Var | Type | Default | Description |
|---------|------|---------|-------------|
| `BOSUN_RESTART_BREAKER` | bool | `true` | Enable/disable restart circuit breaker |
| `BOSUN_RESTART_THRESHOLD` | int | `5` | Restart count delta that trips the breaker |
| `BOSUN_RESTART_WINDOW` | duration | `10m` | Time window for measuring restart velocity |
