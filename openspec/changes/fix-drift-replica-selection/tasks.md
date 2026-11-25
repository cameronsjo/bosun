## 1. Implementation

- [ ] 1.1 Add `preferContainer` over `docker.ContainerInfo` and use it in `CollectActualState` (Requirement: Service State Selection); `ActualService` gains no fields
- [ ] 1.2 Tests for Service State Selection: stopped beside running in both list orders, all-stopped oldest and ID tie-break, paused counts as stopped, oldest wins among two running replicas, healthy-older beats unhealthy-newer, and a pure `preferContainer` table with `Created` set on every fixture
- [ ] 1.3 Restart-breaker tests: a replica alternating restarting/running across cycles still trips; a replica caught `exited` for one sample mid-loop is pinned as known behavior; a breaker-stopped replica hands over to a live sibling and resolves after the next unchanged-count sample
- [ ] 1.4 Callers: confirm the nine production call sites need no change; add `CompareDrift`, `hasMissingDeclaredServices` and `classifyHealth` (post-deploy health poll) cases over an exited-plus-running pair
- [ ] 1.5 Update the container-matching text in `docs/gitops.md`, `docs/commands.md` and `skills/onboard/resources/gitops.md` identically
