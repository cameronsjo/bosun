# Change: Refactor manifest output targets from concrete struct fields to data-driven map

## Why

`RenderOutput` is a concrete struct with three named fields (`Compose`, `Traefik`, `Gatus`). Every merge, render, write, and diff function contains three explicit code paths — one per target. Adding a fourth output target (e.g., Caddy, Nginx Proxy Manager, Uptime Kuma) requires modifying the struct definition, every function that touches it, and the YAML deserialization for `Provision` and `Template`. This creates a structural ceiling that grows linearly with the number of targets.

## What Changes

- Replace `RenderOutput.Compose/Traefik/Gatus` fields with `Targets map[string]map[string]any`
- Replace `Provision.Compose/Traefik/Gatus` fields with `Targets map[string]map[string]any` (custom YAML unmarshaling preserves backwards compatibility with existing provision files)
- Replace `Template.Compose/Traefik/Gatus` fields with `Targets map[string]map[string]any` (same backwards-compatible unmarshaling)
- Make `WriteOutputs` iterate `Targets` dynamically using a filename registry instead of an inline map
- Make `mergeProvision`, `mergeOutput`, `RenderTemplate`, `RenderToYAML`, and `showDiff` iterate the map instead of listing three explicit branches
- Introduce a `TargetRegistry` that maps target name to output filename (replacing the unused `TargetNames` var)
- Default registry ships with `compose`, `traefik`, `gatus` — identical to today's behavior

## Impact

- Affected specs: `manifest-system` (Output Writing, Service Rendering, Provision System, Chart Template Engine)
- Affected code:
  - `internal/manifest/types.go` — `RenderOutput`, `Provision`, `Template`, `NewRenderOutput`, `TargetNames`
  - `internal/manifest/render.go` — `RenderService`, `RenderStack`, `mergeProvision`, `WriteOutputs`, `RenderToYAML`
  - `internal/manifest/template.go` — `RenderTemplate`, `RenderChart`, `renderDependency`, `mergeOutput`
  - `internal/manifest/provision.go` — `LoadProvision` (includes resolution)
  - `internal/manifest/chart.go` — `RenderChart`, `RenderStack`
  - `internal/cmd/provision.go` — `showDiff`, `provisionHelm`, `provisionLegacy`, compose name injection
  - `internal/manifest/*_test.go` — all tests referencing `.Compose`, `.Traefik`, `.Gatus`
- All consumers:
  - `internal/manifest/types.go:95` — struct definition
  - `internal/manifest/types.go:107` — `NewRenderOutput()`
  - `internal/manifest/types.go:285` — `TargetNames` (unused today)
  - `internal/manifest/render.go:46` — `RenderService()` return type
  - `internal/manifest/render.go:177` — `mergeProvision()`
  - `internal/manifest/render.go:190` — `RenderStack()` return type
  - `internal/manifest/render.go:310` — `WriteOutputs()`
  - `internal/manifest/render.go:351` — `RenderToYAML()`
  - `internal/manifest/template.go:157` — `RenderTemplate()` return type
  - `internal/manifest/template.go:222` — `RenderChart()` return type
  - `internal/manifest/template.go:291` — `renderDependency()` return type
  - `internal/manifest/template.go:364` — `mergeOutput()`
  - `internal/manifest/provision.go:105-112` — include resolution (3 explicit if-blocks)
  - `internal/manifest/provision.go:133-139` — target extraction after parsing
  - `internal/manifest/chart.go:94` — `ChartLoader.RenderChart()` return type
  - `internal/manifest/chart.go:148` — `ChartLoader.RenderStack()` return type
  - `internal/manifest/chart.go:196-198` — 3-field merge in stack rendering
  - `internal/cmd/provision.go:173` — `var output *manifest.RenderOutput`
  - `internal/cmd/provision.go:190` — `manifest.RenderToYAML(output)`
  - `internal/cmd/provision.go:216-217` — `output.Compose["name"]` injection
  - `internal/cmd/provision.go:220` — `manifest.WriteOutputs()`
  - `internal/cmd/provision.go:412-420` — `showDiff()` with 3-target slice
