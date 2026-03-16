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
  - Changing the `ServiceManifest.Compose` field (this stays — it's a compose-specific override, not a generic target)
  - Changing the `Chart.Compose` field (same reason — chart-level compose overrides are compose-specific)

## Decisions

### Use `map[string]map[string]any` over interface-based targets

**Decision:** Plain maps keyed by target name.

**Alternatives considered:**
- `Target` interface with `Name()`, `Merge()`, `WriteFile()` methods — over-engineered for what is fundamentally YAML-in, YAML-out with no per-target behavior differences today
- Slice of `TargetOutput{Name, Content}` — loses O(1) lookup by name, makes merge harder

**Rationale:** All three current targets share identical merge, write, and render semantics. A map is the simplest structure that removes the concrete-field ceiling. If per-target behavior diverges later, an interface can wrap the map values.

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

**Rationale:** Direct map access (`output.Targets["compose"]`) works but is verbose and loses nil-safety. The accessor initializes the map if nil, preventing panics on assignment to nil maps.

## Risks / Trade-offs

- **Migration of test assertions**: Every test that references `.Compose`, `.Traefik`, `.Gatus` must change to `.Target("compose")` etc. This is mechanical but touches ~50 assertions across 4 test files. Risk: typos in string keys. Mitigation: define `TargetCompose`, `TargetTraefik`, `TargetGatus` constants.
- **YAML marshal symmetry**: `MarshalYAML` must produce the same flat top-level structure as before (not nest under a `targets:` key). This is needed for `RenderToYAML` dry-run output.
- **Performance**: Map iteration order is non-deterministic in Go. `WriteOutputs` and `RenderToYAML` should sort target names for reproducible output.

## Open Questions

- Should `showDiff` (in `provision.go`) use the `TargetRegistry` for filenames, or should it keep its own hardcoded list? (Recommendation: use registry — single source of truth.)
