## ADDED Requirements

### Requirement: Multi-Target Configuration

Bosun SHALL accept an ordered `targets:` list of named target descriptors in `bosun.yaml` and an authoritative `BOSUN_TARGETS` JSON array with the same field surface. JSON `null` SHALL retain project-config targets; an explicit empty JSON array SHALL resolve to the backwards-compatible implicit default.

When no target descriptor is effective, Bosun SHALL synthesize one target named `default` from the flat configuration. A lone descriptor named `default`, case-insensitively, SHALL configure and normalize that compatibility target. Every configured descriptor SHALL have a non-empty valid name; an invalid descriptor or case-insensitive duplicate SHALL reject the complete effective set rather than be skipped or replaced by an implicit default. A multi-target set containing `default` SHALL likewise fail rather than silently discard or reinterpret it.

#### Scenario: YAML target order is preserved

- **WHEN** `bosun.yaml` declares named targets `unraid` and `pi` in that order
- **THEN** target resolution returns `unraid` followed by `pi`
- **AND** each descriptor retains its target-owned configuration

#### Scenario: Environment target array is authoritative

- **WHEN** project configuration declares target `unraid`
- **AND** `BOSUN_TARGETS` is a JSON array containing only target `pi`
- **THEN** the effective target list contains only `pi`

#### Scenario: Empty configuration preserves the default

- **WHEN** no target list is configured or `BOSUN_TARGETS` is the authoritative empty array
- **THEN** Bosun resolves one target named `default`
- **AND** that target uses the legacy flat configuration and path behavior

#### Scenario: Lone default descriptor is compatibility configuration

- **WHEN** the sole descriptor is named `DEFAULT` and supplies explicit target fields
- **THEN** Bosun normalizes its name to `default`
- **AND** honors its explicit fields on the compatibility target

### Requirement: Per-Target Staging Isolation

Each effective target SHALL own a staging slot distinct from every sibling's equal, ancestor, or descendant path. A named target without an explicit override SHALL derive its slot below the configured staging root; the implicit default SHALL preserve its legacy staging path.

Each complete canonical pipeline SHALL read and write only its target's slot. Verified success MAY clean that slot as allowed by the canonical pipeline. Failure and dry-run output SHALL follow the canonical Failed Staging Evidence Lifecycle: retain owner-only evidence, or securely delete it only when owner-only hardening cannot be proven. A later target SHALL NOT clean, overwrite, or relabel a sibling's retained evidence.

#### Scenario: Sibling staging slots are disjoint

- **WHEN** named targets `unraid` and `pi` use derived staging paths
- **THEN** each target renders only within its own derived slot
- **AND** neither target can remove or replace the other's tree

#### Scenario: Failed evidence survives sibling execution

- **WHEN** `unraid` fails after rendering and its evidence is hardened successfully
- **AND** the shared context remains live for `pi`
- **THEN** `unraid`'s owner-only evidence remains available
- **AND** `pi` proceeds only in its own staging slot

### Requirement: Per-Target State Persistence

Every effective target SHALL use an independent path for the canonical deploy-state document. A named target without a non-empty explicit override SHALL derive `deploy-state-<target-name>.json` in the configured state directory. The implicit default SHALL preserve the legacy state path; a lone default descriptor SHALL use that path unless it supplies a non-empty confined override.

Canonical state schema, atomic-write, attempt, interruption, circuit-breaker, drift, backup, image, skip, and stale-outcome rules SHALL apply independently to each target document. An outcome for one target SHALL NOT mutate a sibling's state.

#### Scenario: Success and failure update only their owners

- **WHEN** target `unraid` records a successful deployment of commit `abc123`
- **AND** target `pi` records a failed attempt of that commit
- **THEN** only `unraid` advances its last-deployed commit and success fields
- **AND** only `pi` advances its applicable attempt and failure fields

#### Scenario: Default state remains backwards compatible

- **WHEN** Bosun resolves only the implicit or lone `default` target
- **THEN** it reads and writes the configured legacy state path
- **AND** no named-target migration is required

### Requirement: Sequential Multi-Target Orchestration

Bosun SHALL run targets sequentially in resolved order. For each target it SHALL derive an independent configuration and invoke the complete canonical reconciliation pipeline, including that target's Git sync, secret decryption, rendering, backup, deploy, configured health gate, hooks, verification, state finalization, failed-staging handling, and locking.

Bosun SHALL NOT claim a once-per-cycle Git/decrypt phase, shared changed-file snapshot, or common commit pin across target attempts. An ordinary target failure SHALL be accumulated and later targets SHALL proceed while the shared cycle context remains live. Target iteration SHALL stop on a non-live shared context exactly as required by the canonical reconcile spec. After eligible targets finish, the cycle SHALL report an error when any target failed without undoing successful siblings.

Every structured log record owned by a target's `Reconciler` SHALL carry that target's normalized identifier whether the pipeline was started by the daemon or direct CLI. Cycle-level records that do not belong to one target are excluded. Target log context SHALL NOT contain secrets.

#### Scenario: Each target runs the full pipeline

- **WHEN** two named targets are eligible for reconciliation
- **THEN** the first target runs the complete canonical pipeline with its effective configuration
- **AND** the second target subsequently runs the complete canonical pipeline with its effective configuration
- **AND** Git sync and secret decryption occur inside each target's pipeline rather than once for the cycle

#### Scenario: Ordinary failure does not consume a live sibling

- **WHEN** `unraid` returns an ordinary compose or configured-health-gate failure
- **AND** the shared cycle context remains live
- **THEN** Bosun finalizes and alerts `unraid` under canonical rules
- **AND** proceeds to `pi`
- **AND** reports an aggregate cycle error after all eligible targets finish

#### Scenario: Non-live context leaves later targets untouched

- **WHEN** the shared cycle context becomes canceled or expired during the active target
- **THEN** canonical active-target finalization runs as applicable
- **AND** no later target starts, mutates state, stages files, acquires a lock, or sends an alert

#### Scenario: Reconciler logs identify their target

- **WHEN** daemon or direct CLI execution runs named target `pi`
- **THEN** every target-owned structured reconciliation log record carries target `pi`
- **AND** no secret value is added as target context

### Requirement: Daemon Multi-Target Admission

Daemon-owned polling, webhook, socket, TCP, manual, and drift-self-heal entry points SHALL share the daemon's in-process single-flight admission. Triggers received during an active cycle SHALL coalesce into the daemon's bounded follow-up-cycle behavior.

A separately invoked CLI process SHALL NOT be described as sharing that in-memory admission. Every per-target reconciler SHALL instead acquire its derived file lock under the canonical Reconciliation Locking requirement, and a competing process SHALL fail immediately when the lock is held.

#### Scenario: Daemon trigger coalesces during a target cycle

- **WHEN** a daemon cycle is reconciling a target
- **AND** one or more daemon-owned triggers arrive
- **THEN** no concurrent daemon cycle starts
- **AND** the daemon records the applicable follow-up work under its dirty-trigger contract

#### Scenario: Separate process uses the file-lock boundary

- **WHEN** one process holds a target's reconcile lock
- **AND** a separate CLI process starts reconciliation for the same derived lock path
- **THEN** the CLI attempt fails immediately under canonical file-lock semantics
- **AND** no shared in-memory mutex is assumed

### Requirement: Per-Target Secrets Scoping

Top-level decrypted values SHALL remain shared input. When a target has a secrets scope, values under `targets.<scope>` SHALL overlay same-named shared values for only that target's rendering. A target without a scope SHALL receive shared values without another target's overlay. Diagnostic logging MAY name overridden keys but SHALL NOT persist or emit secret values.

#### Scenario: Scoped value overlays shared value

- **WHEN** shared secrets contain `db_password: shared`
- **AND** `targets.unraid.db_password` is `specific`
- **AND** target `unraid` selects scope `unraid`
- **THEN** its templates receive `specific`
- **AND** sibling targets do not receive that scoped value unless they select the same scope

#### Scenario: Unscoped target receives shared values

- **WHEN** a target has no secrets scope
- **THEN** its templates receive top-level shared values
- **AND** target-scoped subtrees are not promoted into its render map

### Requirement: Multi-Target CLI Filtering

The direct `bosun reconcile` and `bosun drift` commands SHALL accept a target filter that selects one effective descriptor by name. When project targets are available, an unknown target SHALL fail without falling back to the implicit default or running another target. When no project target configuration can be discovered, `drift --target` MAY retain the legacy compatibility fallback that derives only the requested cached-state path; it SHALL NOT execute a reconciliation target. Daemon trigger entry points SHALL continue to request a complete cycle and SHALL NOT gain target-specific trigger semantics in this change.

#### Scenario: Reconcile filter selects one target

- **WHEN** effective targets are `unraid` and `pi`
- **AND** the operator runs `bosun reconcile --target pi`
- **THEN** only `pi` runs its complete canonical pipeline
- **AND** `unraid` remains untouched

#### Scenario: Unknown reconcile filter fails closed

- **WHEN** the requested reconcile target name is not effective
- **THEN** `bosun reconcile` returns an error naming the unknown target
- **AND** no target executes

#### Scenario: Configured drift filter fails closed

- **WHEN** project configuration exposes effective targets `unraid` and `pi`
- **AND** the operator requests cached or live drift for target `nas`
- **THEN** `bosun drift` returns an error naming the unknown target
- **AND** it does not substitute the implicit default or inspect another target

#### Scenario: No-config cached drift preserves compatibility

- **WHEN** no project target configuration can be discovered
- **AND** the operator requests cached drift for target `pi`
- **THEN** Bosun MAY derive and inspect only `pi`'s legacy-compatible state path
- **AND** it does not execute a target or silently inspect the default state path

### Requirement: Multi-Target Drift CLI

Cached drift inspection SHALL read each selected target's independent canonical state document. Unfiltered multi-target JSON SHALL be one object with a `targets` array, `{"targets":[...]}`, where every entry names its target and contains that target's drift result or target-specific unavailable error.

Live drift SHALL query only the Docker environment represented by the target. Bosun SHALL preflight the complete selected live set before opening Docker. Until remote Docker inspection is supported, any selection containing a target with a non-empty remote host SHALL fail as a whole before any local Docker query and SHALL NOT mutate any selected target state. Cached drift for every target SHALL remain available.

#### Scenario: Aggregate cached JSON has one targets array

- **WHEN** cached drift is requested as JSON for multiple effective targets
- **THEN** the top-level value is an object containing exactly the aggregate `targets` collection
- **AND** each entry identifies its target and isolated result

#### Scenario: Remote live drift fails before local inspection

- **WHEN** a selected named target has a non-empty remote host
- **AND** the operator requests live drift
- **THEN** the command returns a remote-live-drift unsupported error before opening the local Docker client
- **AND** no target state is changed

#### Scenario: Mixed unfiltered live drift fails atomically

- **WHEN** unfiltered live drift selects local target `pi` and remote target `unraid`
- **THEN** Bosun rejects the request before inspecting either target's Docker environment
- **AND** neither target's state is changed

### Requirement: Target Configuration Validation

Bosun SHALL apply equivalent target validation after YAML and `BOSUN_TARGETS` decoding. An empty or unsafe name, a case-insensitive duplicate, `default` in a multi-target set, or a resource collision in state files, equal or nested staging slots, Docker host/project namespaces, or effective deploy destinations SHALL reject the complete effective target set.

Explicit state and staging overrides SHALL be confined to their configured roots. Local appdata/deploy paths SHALL be confined to the permitted local deploy root. A confinement failure SHALL reject the complete effective target set before Git sync, staging preparation, lock acquisition, state mutation, deployment, or alerting.

#### Scenario: Explicit state path escapes its root

- **WHEN** any YAML or environment target resolves an explicit state path outside the configured state root
- **THEN** target resolution rejects the complete set before reconciliation begins
- **AND** no sibling target runs

#### Scenario: Staging paths overlap

- **WHEN** two effective staging slots are equal or one is an ancestor of the other
- **THEN** target resolution rejects the complete set before either slot is prepared

#### Scenario: Lone default differs from default in a set

- **WHEN** a case-insensitive `default` descriptor is the only target
- **THEN** it configures the compatibility target
- **BUT WHEN** that descriptor appears with a named sibling
- **THEN** the complete set is rejected

#### Scenario: Invalid descriptor cannot expose a sibling or fallback

- **WHEN** any YAML or environment descriptor has an empty or unsafe name or duplicates a sibling case-insensitively
- **THEN** target resolution rejects the complete effective set
- **AND** no valid sibling or synthesized default target executes

### Requirement: Per-Target Configuration Independence

Derivation and hot reload SHALL leave each effective target, every sibling, and the base configuration with independent mutable slices and maps. Nil slices SHALL inherit documented base values and explicitly present empty slices SHALL clear them.

Target decoding SHALL preserve scalar presence where omission and explicit empty have different meanings. Scalar fields SHALL follow these rules:

- `name` is required and empty is invalid.
- An omitted `target_host` inherits the base host; explicit empty selects local execution.
- Omitted `local_appdata_path` and `remote_appdata_path` values inherit their base values; explicit empty is invalid rather than selecting an unrelated or secrets-derived deploy path.
- An omitted `project_name` inherits the base project; explicit empty selects the Compose-derived project namespace.
- An omitted `secrets_scope` inherits the base scope; explicit empty disables the target-scoped overlay.
- For a named target, omitted or empty `state_file` and `staging_dir` values select their documented per-target derived paths; for the implicit or lone default they preserve the legacy paths. A non-empty value is an exact override subject to root confinement.

No scalar rule SHALL silently select a sibling's host, path, project, or secrets scope.

#### Scenario: Explicit empty slice clears only its target

- **WHEN** a target explicitly configures an empty hook or deploy-filter list
- **THEN** that list is empty for the target
- **AND** the base and sibling lists remain unchanged

#### Scenario: Omitted and explicit-empty scalar remain distinguishable

- **WHEN** an inheritable scalar is omitted on one target and explicitly empty on another
- **THEN** the omitted field follows its documented inheritance rule
- **AND** the explicit empty follows its documented clear or local rule
- **AND** neither silently resolves to an unintended sibling or production target

#### Scenario: Explicit-empty deploy path is invalid

- **WHEN** a target explicitly sets its applicable local or remote appdata path to empty
- **THEN** target resolution rejects the effective set
- **AND** Bosun does not fall back to a sibling or secrets-derived deploy path

#### Scenario: Explicit-empty project uses Compose derivation

- **WHEN** a target explicitly sets `project_name` to empty
- **THEN** Bosun leaves the effective Compose project name unset for Compose to derive
- **AND** it does not inherit the base target's explicit project name

### Requirement: Per-Target Daemon State Visibility

The daemon SHALL use one centralized enumeration of its startup-resolved target snapshot and each target's independent canonical state document for periodic drift, circuit-breaker diagnostics, and operator status. Structural target changes SHALL require a daemon restart; file reload SHALL NOT silently replace the active topology. After restart, a removed target SHALL NOT be reported as current. Missing or malformed state SHALL be attributed to its target without substituting a sibling's state.

Periodic live drift SHALL inspect every local target against its own Docker namespace. It SHALL NOT inspect the local Docker daemon on behalf of a remote target; that target SHALL receive an attributable unsupported/unavailable result without mutating cached state, while eligible local siblings continue.

The local or authenticated operator `/status` view and direct `bosun status` output SHALL report last deploy, drift, and circuit-breaker condition per effective target. Multi-target `/status` SHALL add an ordered target collection; a single implicit or lone default SHALL preserve the existing top-level presentation and legacy state path. The public `/health` response SHALL remain the bounded readiness projection required by the daemon-security spec and SHALL NOT expose the target collection, target names, state paths, drift details, or breaker details.

#### Scenario: Operator status reports independent target outcomes

- **WHEN** `unraid` has a successful current state and `pi` has a tripped breaker
- **THEN** authenticated or local `/status` and `bosun status` attribute those conditions to their respective targets
- **AND** neither state is read from the base default path by mistake

#### Scenario: Public health remains bounded

- **WHEN** multiple targets have deploy, drift, or breaker details
- **THEN** public `/health` still exposes only the bounded readiness fields required by daemon security
- **AND** it does not expose target names, state paths, drift details, or breaker details

#### Scenario: Periodic drift separates local and remote targets

- **WHEN** periodic drift enumerates local target `pi` and remote target `unraid`
- **THEN** it inspects `pi` against `pi`'s Docker namespace
- **AND** marks `unraid` unsupported or unavailable without opening local Docker on its behalf or mutating its cached state

#### Scenario: Structural change becomes current after restart

- **WHEN** configuration removes target `pi` while the daemon is running
- **THEN** the startup-resolved snapshot continues to govern until restart
- **AND WHEN** the daemon restarts with the valid new configuration
- **THEN** `pi` is no longer presented as a current target
