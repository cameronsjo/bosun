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
