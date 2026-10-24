# Tasks

- [x] 1. `PreDeployHook` type, `ValidatePreDeployHooks` with table tests,
  deep clone.
- [x] 2. Runner: exact argv, cwd = staging tree, minimal environment, per-hook
  timeout under the cycle context, process-group cancellation, capped
  output in the failure log.
- [x] 3. Pipeline step after render and declared-state extraction, before
  backup, file sync, and compose up; failure alerts and returns.
- [x] 4. Thread `pre_deploy_hooks` through the config file, `Target`
  (`BOSUN_TARGETS`), `ConfigForTarget`, CLI and daemon hydration, and hot
  reload (absent key clears; invalid reload aborts).
- [x] 5. Pipeline tests through `Run()`: a passing hook proceeds (cwd and
  environment checked), a failing hook blocks with no compose calls, no file
  sync, and state unchanged, a missing program blocks, a hook timeout blocks,
  the cycle deadline blocks, invalid config fails before anything runs.
  Staged breaks (ignore the hook error; inherit bosun's environment) turn the
  matching tests red.
- [x] 6. Spec delta and onboard docs.
- [x] 7. Review round 1: honest trust docs (hooks are code execution as bosun;
  env stripping is hygiene, not isolation); dry-run block sends no alert and
  is not counted; kill the hook's process group after every exit; sanitize
  hook names and output; tests for group death on timeout, no surviving
  background child after a pass, dry-run accounting, sanitization, and a
  `BOSUN_TARGETS` JSON list surviving reload. Staged breaks of the group
  kill, the process-group setup, and the dry-run branch turn the matching
  tests red.
