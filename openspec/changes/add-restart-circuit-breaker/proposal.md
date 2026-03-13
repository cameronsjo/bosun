# Proposal: Restart Circuit Breaker

**Status:** approved
**Issue:** bosun-0ht
**GitHub:** #118

## Problem

Containers with restart policies (e.g., `restart: unless-stopped`) can enter infinite restart loops when they crash on startup. Docker dutifully restarts them each time, consuming CPU, filling disk with logs, and generating noise in drift alerts — all without ever recovering. The operator must manually notice and intervene.

## Changes

Add a restart circuit breaker to the drift detection loop that:

1. Tracks container restart count deltas between drift checks
2. Trips when a container's restart count increases by more than a threshold within a time window
3. Stops the container gracefully on trip (preserving logs for debugging)
4. Sends a critical alert when tripped and an info alert when resolved

## Impact

- **Drift check loop:** Enrichment pass inspects running containers to get restart counts
- **Docker client:** New `StopContainer()` method
- **State file:** New `RestartTracking` map for per-container restart state
- **Alert manager:** New `SendRestartCircuitBreaker` / `SendRestartCircuitBreakerResolved` methods
- **Daemon config:** Three new env vars (`BOSUN_RESTART_THRESHOLD`, `BOSUN_RESTART_WINDOW`, `BOSUN_RESTART_BREAKER`)

## All Consumers

| Consumer | How affected |
|----------|-------------|
| Daemon drift check loop | Calls enrichment, handles alert dispatch, persists state |
| Docker client | New `StopContainer()` method on `Client` and `DockerAPI` interface |
| State file (`DeployState`) | New `RestartTracking` field |
| Alert manager | New alert methods for restart circuit breaker events |
| `ConfigFromEnv()` | Parses three new env vars into reconcile config |
