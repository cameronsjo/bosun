## ADDED Requirements

### Requirement: Multi-Target Configuration

Bosun SHALL accept an ordered `targets:` list of named target descriptors in `bosun.yaml` and an authoritative `BOSUN_TARGETS` JSON array with the same field surface. JSON `null` SHALL retain project-config targets; an explicit empty JSON array SHALL resolve to the backwards-compatible implicit default.

When no target descriptor is effective, Bosun SHALL synthesize one target named `default` from the flat configuration. A lone descriptor named `default`, case-insensitively, SHALL configure and normalize that compatibility target. A multi-target set containing `default` SHALL fail rather than silently discard or reinterpret it.

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

Every effective target SHALL use an independent path for the canonical deploy-state document. A named target without an explicit override SHALL derive `deploy-state-<target-name>.json` in the configured state directory; the implicit or lone default target SHALL preserve the legacy state path.

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

The direct `bosun reconcile` and `bosun drift` commands SHALL accept a target filter that selects one effective descriptor by name. An unknown target SHALL fail without falling back to the implicit default or running another target. Daemon trigger entry points SHALL continue to request a complete cycle and SHALL NOT gain target-specific trigger semantics in this change.

#### Scenario: Reconcile filter selects one target

- **WHEN** effective targets are `unraid` and `pi`
- **AND** the operator runs `bosun reconcile --target pi`
- **THEN** only `pi` runs its complete canonical pipeline
- **AND** `unraid` remains untouched

#### Scenario: Unknown filter fails closed

- **WHEN** the requested target name is not effective
- **THEN** the command returns an error naming the unknown target
- **AND** no target executes

### Requirement: Multi-Target Drift CLI

Cached drift inspection SHALL read each selected target's independent canonical state document. Unfiltered multi-target JSON SHALL be one object with a `targets` array, `{"targets":[...]}`, where every entry names its target and contains that target's drift result or target-specific unavailable error.

Live drift SHALL query only the Docker environment represented by the target. Until remote Docker inspection is supported, `drift --live` for a target with a non-empty remote host SHALL fail before any local Docker query and SHALL NOT mutate target state. Cached drift for that target SHALL remain available.

#### Scenario: Aggregate cached JSON has one targets array

- **WHEN** cached drift is requested as JSON for multiple effective targets
- **THEN** the top-level value is an object containing exactly the aggregate `targets` collection
- **AND** each entry identifies its target and isolated result

#### Scenario: Remote live drift fails before local inspection

- **WHEN** a selected named target has a non-empty remote host
- **AND** the operator requests live drift
- **THEN** the command returns a remote-live-drift unsupported error before opening the local Docker client
- **AND** no target state is changed

### Requirement: Target Configuration Validation

Bosun SHALL apply equivalent target validation after YAML and `BOSUN_TARGETS` decoding. It SHALL prevent unsafe names and case-insensitive duplicates from executing, reject `default` in a multi-target set, and reject resource collisions in state files, equal or nested staging slots, Docker host/project namespaces, and effective deploy destinations.

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

### Requirement: Per-Target Configuration Independence

Derivation and hot reload SHALL leave each effective target, every sibling, and the base configuration with independent mutable slices and maps. Nil slices SHALL inherit documented base values and explicitly present empty slices SHALL clear them.

Target decoding SHALL preserve scalar presence where omission and explicit empty have different meanings. Each scalar SHALL have a documented rule. In particular, an empty `target_host` SHALL mean local rather than inherit another host; an omitted inheritable scalar SHALL retain its documented base value; and an explicitly empty clearable scalar SHALL clear rather than silently select a foreign path or project.

#### Scenario: Explicit empty slice clears only its target

- **WHEN** a target explicitly configures an empty hook or deploy-filter list
- **THEN** that list is empty for the target
- **AND** the base and sibling lists remain unchanged

#### Scenario: Omitted and explicit-empty scalar remain distinguishable

- **WHEN** an inheritable scalar is omitted on one target and explicitly empty on another
- **THEN** the omitted field follows its documented inheritance rule
- **AND** the explicit empty follows its documented clear or local rule
- **AND** neither silently resolves to an unintended sibling or production target

### Requirement: Per-Target Daemon State Visibility

Daemon periodic drift, health/status, and circuit-breaker views SHALL enumerate the current effective targets and read each target's independent canonical state document. `bosun status` SHALL report the last deploy, drift, and breaker condition for each effective target. A single implicit or lone default target SHALL preserve the existing default presentation as closely as possible while reading the same legacy state path.

Reloaded target topology SHALL supersede stale target visibility at the same safe configuration boundary used for reconciliation. Removed targets SHALL NOT be reported as current, and a target's missing or malformed state SHALL be attributed to that target without substituting a sibling's state.

#### Scenario: Status reports independent target outcomes

- **WHEN** `unraid` has a successful current state and `pi` has a tripped breaker
- **THEN** daemon and CLI status visibility report those conditions against their respective targets
- **AND** neither state is read from the base default path by mistake

#### Scenario: Removed target is not current after reload

- **WHEN** a valid configuration reload removes target `pi`
- **THEN** subsequent daemon state visibility enumerates only the new effective set
- **AND** the old state file is not presented as a current target
