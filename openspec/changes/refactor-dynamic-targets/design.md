## Context

The manifest package outputs to three hardcoded targets: compose, traefik, gatus. Every render, merge, write, and diff path has three explicit branches. This change replaces the concrete fields with a map-based approach to make targets data-driven.

The `Provision` and `Template` structs also have three concrete YAML fields (`compose:`, `traefik:`, `gatus:`). Existing provision/template files use these top-level keys. Backwards compatibility with existing YAML files is a hard requirement.

## Goals / Non-Goals

- Goals:
  - Replace `RenderOutput` struct fields with `Targets map[string]map[string]any`
  - Replace `Provision` and `Template` struct fields with the same map approach
  - Introduce `TargetRegistry` for filename mapping
  - Maintain full backwards compatibility with existing provision/template YAML files
  - All existing tests pass with equivalent assertions (`.Target("compose")` instead of `.Compose`)

- Non-Goals:
  - Adding new targets (that's a follow-up, this change only enables it)
  - Config-driven target registration (the registry is code-defined for now)
  - Changing the `ServiceManifest.Compose` field — this is a compose-specific *input* override (user says "merge this into the compose output"), not a generic target output. Generalizing input overrides to arbitrary targets is a future phase if demand materializes; this change focuses on the output side
  - Changing the `Chart.Compose` field (same reasoning as ServiceManifest.Compose)

## Decisions

### Use `map[string]map[string]any` over interface-based targets

**Decision:** Plain maps keyed by target name.

**Alternatives considered:**
- `Target` interface with `Name()`, `Merge()`, `WriteFile()` methods — over-engineered for what is fundamentally YAML-in, YAML-out with no per-target behavior differences today
- Slice of `TargetOutput{Name, Content}` — loses O(1) lookup by name, makes merge harder

**Rationale:** All three current targets share identical merge, write, and render semantics. A map is the simplest structure that removes the concrete-field ceiling.

**When to reconsider:** Move to an interface if two or more targets need distinct merge/write behavior (e.g., a target that requires non-YAML output, or per-target validation beyond map structure). The migration path: define a `Target` interface wrapping `map[string]any`, adapt existing map values behind it, and update callers incrementally. Until then, the map is simpler and sufficient.

### Custom YAML unmarshaling for Provision and Template

**Decision:** Implement `UnmarshalYAML` on `Provision` and `Template` that reads known top-level keys (`compose`, `traefik`, `gatus`, plus any unknown keys) into the `Targets` map.

**Alternatives considered:**
- Require users to migrate YAML to a `targets:` wrapper key — breaking change, rejected
- Use `yaml:",inline"` tag — doesn't work cleanly with a mix of typed and map fields in Go's yaml.v3

**Rationale:** Existing provision files look like `compose: {...}\ntraefik: {...}`. The unmarshaler reads these into `Targets["compose"]`, `Targets["traefik"]`, etc. No user-facing changes. New targets added by users would also be top-level keys, keeping the YAML flat and readable.

### TargetRegistry maps target name to output file metadata

**Decision:** A package-level `TargetRegistry` maps target name to `TargetConfig{Filename func(stackName string) string, Dir string}`.

**Rationale:** Today the filename mapping is inline in `WriteOutputs`:
- `compose` → `{stackName}.yml.tmpl`
- `traefik` → `dynamic.yml`
- `gatus` → `endpoints.yml`

The compose filename is stack-dependent (parameterized by stack name), while traefik/gatus are fixed. A `Filename func(string) string` handles both cases. The `Dir` field defaults to the target name itself (e.g., `compose/`, `traefik/`).

### Accessor method for compose-specific operations

**Decision:** Add `RenderOutput.Target(name string) map[string]any` accessor. The `provision.go` code that injects `output.Compose["name"]` becomes `output.Target("compose")["name"]`.

**Rationale:** Direct map access (`output.Targets["compose"]`) works but is verbose and loses nil-safety. The accessor initializes the map if nil, preventing panics on assignment to nil maps. The accessor does not validate against the registry — it's a convenience for nil-safe map access. Callers using target name constants (`TargetCompose`, etc.) get correctness via the constants, not runtime validation.

### showDiff uses TargetRegistry

**Decision:** `showDiff` (in `provision.go`) SHALL use `TargetRegistry` to resolve filenames instead of maintaining its own hardcoded list.

**Rationale:** Single source of truth. When a new target is registered, showDiff picks it up automatically without a parallel code change.

## Risks / Trade-offs

- **Loss of compile-time safety**: Replacing typed struct fields with `map[string]map[string]any` trades compile-time field access for runtime map lookups. Mitigation: target name constants (`TargetCompose`, etc.) prevent typos, and the `Target()` accessor prevents nil-map panics. The existing code already uses `map[string]any` for all values within each target — the change moves one level up, from struct-field dispatch to map-key dispatch. The trade-off is acceptable because the three hardcoded fields were never meaningfully type-checked beyond "is it a map?"
- **Migration of test assertions**: Every test that references `.Compose`, `.Traefik`, `.Gatus` must change to `.Target("compose")` etc. This is mechanical but touches ~50 assertions across 4 test files. Risk: typos in string keys. Mitigation: define `TargetCompose`, `TargetTraefik`, `TargetGatus` constants.
- **Merge semantics are unchanged**: The existing `DeepMerge` function (spec: Deep Merge requirement) handles all target merging today and will continue to do so. Merging iterates the `Targets` map and calls `DeepMerge` per target. No per-target merge customization is needed — all targets use identical recursive merge with union keys for networks/depends_on and extend keys for endpoints.
- **YAML marshal symmetry**: `MarshalYAML` must produce the same flat top-level structure as before (not nest under a `targets:` key). This is needed for `RenderToYAML` dry-run output.
- **Performance**: Map iteration order is non-deterministic in Go. `WriteOutputs` and `RenderToYAML` should sort target names for reproducible output.
