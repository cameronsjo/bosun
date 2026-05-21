## 1. Drift self-heal attempt bounding (#259)

- [ ] 1.1 Add `MaxSelfHealAttempts` to the daemon config, parsed from `BOSUN_DRIFT_SELF_HEAL_MAX_ATTEMPTS` in `ConfigFromEnv` (default a small positive value)
- [ ] 1.2 Define a stable drift-signature key (e.g. sorted `service:type` set) and a per-signature attempt counter in the deploy state file
- [ ] 1.3 In `maybeSelfHeal`, increment the counter for the current drift signature before triggering; reset the counter when the signature changes or drift clears
- [ ] 1.4 Stop self-heal once the counter reaches the bound; mark the signature exhausted in state and emit a `self-heal-exhausted` alert (once per signature)
- [ ] 1.5 Resume self-heal when a new/changed drift signature appears or the exhausted signature later clears
- [ ] 1.6 Tests: out-of-band drift triggers self-heal N times then stops; new signature resets the counter; resolved drift clears exhausted state

## 2. Restart breaker baseline integrity (#265)

- [ ] 2.1 In `evaluateRestartBreaker`, when `delta > 0` and `elapsed > window`, do not silently reset the baseline to the current count; carry forward enough history that a sustained slow loop still trips
- [ ] 2.2 Preserve the earliest unresolved-restart baseline (timestamp + count) so accumulated restarts across long intervals are not discarded
- [ ] 2.3 At config-load, warn when `BOSUN_DRIFT_INTERVAL > BOSUN_RESTART_WINDOW` (breaker can never observe a window-bounded delta otherwise)
- [ ] 2.4 Tests: with `drift_interval > restart_window` and a looping container, breaker trips; doctor/config-load emits the misconfiguration warning

## 3. Restart breaker resolution attribution (#266)

- [ ] 3.1 Add a container-identity field (e.g. container ID) to `RestartTrackingEntry`
- [ ] 3.2 Capture the container identity when a service is first tracked / tripped
- [ ] 3.3 In the resolution branch, treat a changed container identity as recreation (not operator recovery); do not mark `Resolved` on a `RestartCount` reset that is explained by recreation
- [ ] 3.4 Require a post-recreate stability grace period (no further restarts across at least one check cycle) before declaring `Resolved`
- [ ] 3.5 Tests: reconcile-driven recreate does not emit `Resolved` while the container resumes looping; genuine operator recovery (stable, same or new container) does emit `Resolved`

## 4. State persistence & migration

- [ ] 4.1 Persist self-heal attempt counters and restart container-identity fields via the existing atomic write pattern
- [ ] 4.2 Ensure missing/legacy fields decode safely (zero values behave as "no prior attempts" / "identity unknown")

## 5. Documentation

- [ ] 5.1 Update `skills/onboard/resources/gitops.md` (self-heal bounding + restart-breaker semantics)
- [ ] 5.2 Add new env vars to the `CLAUDE.md` env-var table
