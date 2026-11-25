## 1. Implementation

- [ ] 1.1 Add `preferContainer` over `docker.ContainerInfo` and use it in `CollectActualState` (Requirement: Service State Selection); `ActualService` also lists every container of the service so the breaker can evaluate each one
- [ ] 1.2 Tests for Service State Selection: stopped beside running in both list orders, all-stopped oldest and ID tie-break, paused counts as stopped, oldest wins among two running replicas, healthy-older beats unhealthy-newer, and a pure `preferContainer` table with `Created` set on every fixture
- [ ] 1.3 Restart breaker (Requirement: Restart Breaker Replica Tracking): one tracking entry per container, a replica that is stopped by the breaker stays tripped while a sibling runs, every crash-looping replica is stopped, and a crash-looping replica that is not the selected one is watched
- [ ] 1.4 Breaker tests: every replica crash-loops, a newer replica loops beside a healthy older one, a stopped replica stays tripped with no resolved alert, a replacement container resolves after a grace sample, a replica alternating restarting/running still trips, and a one-container service behaves as before
- [ ] 1.5 Callers: confirm the eight direct production call sites need no change; add `CompareDrift`, `hasMissingDeclaredServices` and `classifyHealth` (post-deploy health poll) cases over an exited-plus-running pair; the skip check reads a tripped replica of the service
- [ ] 1.6 Update the container-matching text in `docs/gitops.md`, `docs/commands.md` and `skills/onboard/resources/gitops.md` identically, and the breaker text
