# Tasks

- [x] 1. `ValidateComposeIgnore` with table tests; reject invalid lists at
  config load (root and per target).
- [x] 2. Thread `compose_ignore` through the config file, `Target`
  (`BOSUN_TARGETS`), `ConfigForTarget`, CLI and daemon hydration, and hot
  reload (an absent key reloads as an empty list).
- [x] 3. Selection: ignored unmanaged entries skip the fail-closed rule and
  are dropped from the recorded unmanaged list and from the last record
  before comparison; managed entries are never ignored.
- [x] 4. Drop ignored files from the compose-file glob in the local deploy,
  before inspection, selection, and the rollback scope.
- [x] 5. Log configured and skipped entries each deploy; warn on a listed
  name bosun deploys.
- [x] 6. Pipeline tests through `Run()`: ignored stray directory keeps a
  subset run, a non-ignored one runs every file, a removed entry fails closed
  again, an added entry takes effect at once, an ignored `*.yml` is not run,
  a managed file (this run's or the last deploy's manifest) stays managed, an
  invalid list ignores nothing. Staged breaks
  of each rule turn the matching tests red.
- [x] 7. Spec delta and onboard docs.
