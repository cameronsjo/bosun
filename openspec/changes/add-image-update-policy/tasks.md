## 1. Configuration Surface

- [ ] 1.1 Add `ImagePolicy` string type with constants `ImagePolicyPinned` and `ImagePolicyAuto` in `internal/reconcile/types.go` or `internal/reconcile/reconcile.go`
- [ ] 1.2 Add `image_policies` map field to `configFile` struct in `internal/config/config.go`
- [ ] 1.3 Implement `extractImagePolicies()` helper following existing `extract*()` pattern
- [ ] 1.4 Add `ImagePolicies()` getter to `Config` struct
- [ ] 1.5 Add `ImagePolicies` field to `ReloadedConfig` in `internal/reconcile/reconcile.go`
- [ ] 1.6 Wire `ImagePolicies` through `LoadFrom()` and the `ConfigReloaderFunc` path

## 2. Environment Variable Override

- [ ] 2.1 Parse `BOSUN_IMAGE_POLICY` in `internal/daemon/daemon.go` `ConfigFromEnv()`
- [ ] 2.2 Validate value is `pinned` or `auto`, warn and default to `pinned` on invalid
- [ ] 2.3 Store global default policy in reconcile `Config`
- [ ] 2.4 Document env var in `AGENTS.md` environment variables table

## 3. Pre-Pull Integration

- [ ] 3.1 Add `ComposePullServices(ctx, composeFiles, serviceNames)` method to `DeployOps` (depends on `add-image-prepull`)
- [ ] 3.2 In reconcile pipeline, resolve effective policy per service (config map > env var default > `pinned`)
- [ ] 3.3 Filter declared services by `auto` policy and pass service names to selective pull
- [ ] 3.4 Skip selective pull when no `auto` services exist
- [ ] 3.5 Add structured logging: which services are being pulled, which are pinned

## 4. Drift Reporting Context

- [ ] 4.1 Add `ImagePolicy` field to `DeclaredService` struct
- [ ] 4.2 Populate `ImagePolicy` from resolved policies in `ExtractDeclaredState` or at pipeline level
- [ ] 4.3 Include policy in drift report output for `image_mismatch` items

## 5. Documentation

- [ ] 5.1 Update `skills/onboard/resources/configuration.md` with `image_policies` schema
- [ ] 5.2 Update `skills/onboard/resources/gitops.md` with image policy behavior
- [ ] 5.3 Update `docs/commands.md` if any CLI flags are added

## 6. Testing

- [ ] 6.1 Unit test: `extractImagePolicies()` with valid map, empty map, missing key
- [ ] 6.2 Unit test: effective policy resolution (config map > env default > hardcoded default)
- [ ] 6.3 Unit test: `ComposePullServices` passes correct service arguments
- [ ] 6.4 Unit test: selective pull skipped when no `auto` services
- [ ] 6.5 Integration test: full pipeline with mixed `pinned` and `auto` services
- [ ] 6.6 Unit test: `ReloadedConfig` carries image policies through reload path
