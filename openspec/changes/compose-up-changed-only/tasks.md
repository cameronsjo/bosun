# Tasks

- [x] 1. Pure selection function and compose input inspection with table tests,
  including every full-run fallback.
- [x] 2. Make the orphan pass explicit in isolated compose up; skip it on subset
  runs.
- [x] 3. Capture the fallback reason before the pre-deploy `NeedsRedeploy`
  marker and the running-service set from the pre-deploy health snapshot.
- [x] 4. Apply the selection in `deployLocal`; keep `lastComposeFiles` (rollback
  scope) the full list.
- [x] 5. Pipeline tests through `Run()`: one changed file, nothing changed, a
  stopped service, and the fallback cases. Staged breaks (orphan pass always on;
  fallback forced; fallback ignored) turn the matching tests red.
- [x] 6. Spec delta: `specs/reconcile/spec.md`, ADDED Changed-Only Compose Up.
- [x] 7. Review round 1: inputs outside the deploy manifest and a self-pulling
  `pull_policy` make a file always run; orphan detection counts containers in
  every state; per-file rules selecting every file make a full run; presence
  uses `ServiceIsPresent` with restart-breaker state; pipeline tests for a
  changed, unchanged, and deleted `env_file` (a staged break of the path
  mapping turns the changed case red); Service Orchestration MODIFIED.
- [x] 8. Review round 2: fail closed when any compose-directory entry is not
  in the deploy manifest (covers host-created `.env`, extra `*.yml`, stray
  files); services with more than one replica always run. Pipeline tests for
  each host-created case; a staged break removing the rule turns them red.
- [x] 9. Review round 3: record unmanaged compose-directory entry names in
  deploy state on success; run every file when the record is missing or a
  non-empty record differs from the current listing (host-deleted `.env`).
  Staged break of the comparison turns its table and pipeline tests red.
