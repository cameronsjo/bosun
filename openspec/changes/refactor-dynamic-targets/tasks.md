## 1. Core Types

- [ ] 1.1 Define `TargetConfig` struct and `TargetRegistry` with default entries (compose, traefik, gatus)
- [ ] 1.2 Define target name constants (`TargetCompose`, `TargetTraefik`, `TargetGatus`)
- [ ] 1.3 Replace `RenderOutput` struct fields with `Targets map[string]map[string]any`
- [ ] 1.4 Add `Target(name string) map[string]any` accessor (auto-init on nil)
- [ ] 1.5 Update `NewRenderOutput()` to initialize `Targets` map with empty sub-maps for registered targets
- [ ] 1.6 Remove unused `TargetNames` var

## 2. Provision and Template YAML Compat

- [ ] 2.1 Replace `Provision` struct fields (Compose, Traefik, Gatus) with `Targets map[string]map[string]any`
- [ ] 2.2 Implement `Provision.UnmarshalYAML` to read flat top-level keys into `Targets`
- [ ] 2.3 Implement `Provision.MarshalYAML` for round-trip symmetry
- [ ] 2.4 Replace `Template` struct fields (Compose, Traefik, Gatus) with `Targets map[string]map[string]any`
- [ ] 2.5 Implement `Template.UnmarshalYAML` to read flat top-level keys into `Targets`
- [ ] 2.6 Implement `Template.MarshalYAML` for round-trip symmetry

## 3. Render Pipeline

- [ ] 3.1 Update `mergeProvision` (render.go) to iterate `Targets` map
- [ ] 3.2 Update `mergeOutput` (template.go) to iterate `Targets` map
- [ ] 3.3 Update `RenderTemplate` target extraction to iterate known keys dynamically
- [ ] 3.4 Update `RenderService` to use `Target()` accessor
- [ ] 3.5 Update `RenderStack` (render.go) to use `Target()` accessor for merge and networks
- [ ] 3.6 Update `RenderChart` (template.go) to use `Target()` accessor
- [ ] 3.7 Update `renderDependency` (template.go) to use `Target()` accessor
- [ ] 3.8 Update `RenderChart` (chart.go) and `RenderStack` (chart.go) to iterate targets
- [ ] 3.9 Update `LoadProvision` includes resolution (provision.go) to iterate targets

## 4. Output and Display

- [ ] 4.1 Update `WriteOutputs` to iterate `TargetRegistry` with sorted target names
- [ ] 4.2 Update `RenderToYAML` to iterate targets with sorted names
- [ ] 4.3 Update `showDiff` (provision.go) to use `TargetRegistry`
- [ ] 4.4 Update compose name injection in provision.go to use `Target()` accessor

## 5. Tests

- [ ] 5.1 Update render_test.go assertions (`.Compose` → `.Target(TargetCompose)`)
- [ ] 5.2 Update provision_test.go assertions
- [ ] 5.3 Update template_test.go assertions
- [ ] 5.4 Update chart_test.go assertions
- [ ] 5.5 Add test: custom YAML unmarshaling round-trips for Provision
- [ ] 5.6 Add test: custom YAML unmarshaling round-trips for Template
- [ ] 5.7 Add test: `Target()` accessor auto-initializes nil map
- [ ] 5.8 Add test: `WriteOutputs` produces sorted, reproducible output
- [ ] 5.9 Run full test suite, verify no regressions
