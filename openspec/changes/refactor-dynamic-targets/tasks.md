## 1. Core Types

- [x] 1.1 Define `TargetConfig` struct and `TargetRegistry` with default entries (compose, traefik, gatus)
- [x] 1.2 Define target name constants (`TargetCompose`, `TargetTraefik`, `TargetGatus`)
- [x] 1.3 Replace `RenderOutput` struct fields with `Targets map[string]map[string]any`
- [x] 1.4 Add `Target(name string) map[string]any` accessor (auto-init on nil)
- [x] 1.5 Update `NewRenderOutput()` to initialize `Targets` map with empty sub-maps for registered targets
- [x] 1.6 Replace `TargetNames` var with `TargetNames()` func returning sorted registry keys

## 2. Provision and Template YAML Compat

- [x] 2.1 Replace `Provision` struct fields (Compose, Traefik, Gatus) with `Targets map[string]map[string]any`
- [x] 2.2 Implement `Provision.UnmarshalYAML` to read flat top-level keys into `Targets`
- [x] 2.3 Implement `Provision.MarshalYAML` for round-trip symmetry
- [x] 2.4 Replace `Template` struct fields (Compose, Traefik, Gatus) with `Targets map[string]map[string]any`
- [x] 2.5 Implement `Template.UnmarshalYAML` to read flat top-level keys into `Targets`
- [x] 2.6 Implement `Template.MarshalYAML` for round-trip symmetry

## 3. Render Pipeline

- [x] 3.1 Update `mergeProvision` (render.go) to iterate `Targets` map
- [x] 3.2 Update `mergeOutput` (template.go) to iterate `Targets` map
- [x] 3.3 Update `RenderTemplate` target extraction to iterate target keys from rendered YAML (not hardcoded fields)
- [x] 3.4 Update `RenderService` to use `Target()` accessor
- [x] 3.5 Update `RenderStack` (render.go) to use `Target()` accessor for merge and networks
- [x] 3.6 Update `RenderChart` (template.go) to use `Target()` accessor
- [x] 3.7 Update `renderDependency` (template.go) to use `Target()` accessor
- [x] 3.8 Update `RenderChart` (chart.go) and `RenderStack` (chart.go) to iterate targets
- [x] 3.9 Update `LoadProvision` includes resolution (provision.go) to iterate targets

## 4. Output and Display

- [x] 4.1 Update `WriteOutputs` to iterate `TargetRegistry` with sorted target names; log warning and skip unregistered targets
- [x] 4.2 Update `RenderToYAML` to iterate targets with sorted names; include unregistered targets for diagnostic visibility
- [x] 4.3 Update `showDiff` (provision.go) to use `TargetRegistry`; skip unregistered targets
- [x] 4.4 Update compose name injection in provision.go to use `Target()` accessor

## 5. Tests

- [x] 5.1 Update render_test.go assertions (`.Compose` → `.Target(TargetCompose)`)
- [x] 5.2 Update provision_test.go assertions
- [x] 5.3 Update template_test.go assertions
- [x] 5.4 Update chart_test.go assertions
- [x] 5.5 Add test: custom YAML unmarshaling round-trips for Provision
- [x] 5.6 Add test: custom YAML unmarshaling round-trips for Template
- [x] 5.7 Add test: `Target()` accessor auto-initializes nil map
- [x] 5.8 Add test: `WriteOutputs` produces sorted, reproducible output
- [x] 5.9 Add integration test: full render pipeline (service → provision → WriteOutputs) with all three targets
- [x] 5.10 Add test: `WriteOutputs` warns and skips unregistered target names
- [x] 5.11 Add test: `RenderToYAML` includes unregistered targets in output
- [x] 5.12 Verify migration completeness: grep for old field access patterns (`.Compose`, `.Traefik`, `.Gatus` on `RenderOutput`/`Provision`/`Template`) to confirm no stale references remain
- [x] 5.13 Run full test suite, verify no regressions
