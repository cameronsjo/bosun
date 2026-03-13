## Context

Homelabs commonly use Watchtower to auto-update containers running mutable tags like `:latest`. When Bosun manages the same host, both tools can pull images and restart containers independently, causing unpredictable state. Operators need a single tool (Bosun) that can handle both pinned deployments (deploy exactly what Git declares) and auto-update deployments (pull latest before compose up).

This proposal layers per-service policy on top of the `add-image-prepull` change, which adds a blanket `docker compose pull` stage. With policies, the pre-pull stage becomes selective: only services with `auto` policy get pulled.

## Goals / Non-Goals

- Goals:
  - Per-service `pinned` vs `auto` image policy in `bosun.yaml`
  - Selective `docker compose pull` targeting only `auto` services
  - Global default override via `BOSUN_IMAGE_POLICY` env var
  - Replace Watchtower for Bosun-managed services
  - Policy reloaded from repo after each git pull (like `post_sync_hooks`)

- Non-Goals:
  - Scheduled polling for new images (Bosun pulls at reconciliation time, not on a timer)
  - Registry webhook integration (out of scope; future work)
  - Digest pinning or image signing verification (future work)
  - Watchtower removal automation

## Decisions

### Decision: Map-based config over inline compose labels

Per-service policies live in `bosun.yaml` under `image_policies`, not as Docker Compose labels. This keeps Bosun config in one place and avoids requiring compose file modifications. The map keys are Docker Compose service names.

```yaml
# bosun.yaml
image_policies:
  traefik: pinned    # deploy exactly the image:tag from compose
  homepage: auto     # pull latest before compose up
  vaultwarden: auto
```

Alternatives considered:
- Compose labels (`bosun.image-policy: auto`): Requires modifying every compose file; couples policy to service definition rather than deployment config.
- Global-only setting: Too coarse; most services should be pinned while only a few need auto-pull.

### Decision: Default to `pinned`

Services without an explicit policy default to `pinned`. This preserves current Bosun behavior (deploy exactly what Git declares) and makes `auto` an opt-in per service.

### Decision: Selective `docker compose pull` with service arguments

`docker compose pull` accepts service name arguments to pull only specific services. When `auto` services exist, the pre-pull stage runs `docker compose pull <svc1> <svc2>` instead of pulling all services. When no `auto` services exist, the pre-pull stage is skipped entirely (or runs for all services if `add-image-prepull` is configured independently).

### Decision: `BOSUN_IMAGE_POLICY` sets global default, not per-service

The env var sets the default policy for services not listed in `image_policies`. Value: `pinned` (default) or `auto`. This allows `BOSUN_IMAGE_POLICY=auto` to make all services auto-pull without listing each one, while `image_policies` in the config file still overrides per service.

## Risks / Trade-offs

- **Race with Watchtower**: During migration, both tools may run simultaneously. Mitigation: document the migration path (configure `auto` in Bosun, then disable Watchtower for those services).
- **Mutable tag drift**: `auto` services will show `image_mismatch` drift between reconciliations when a new image is pushed to the registry. This is expected behavior; the drift report should note the policy as context.
- **Network dependency**: `auto` policy adds a registry pull on every reconciliation. If the registry is unreachable, the pre-pull fails and aborts the pipeline. Mitigation: pre-pull failure is non-fatal for `pinned` services (they continue with cached images); only `auto` services require successful pulls.

## Migration Plan

1. Identify services currently managed by Watchtower
2. Add `image_policies` entries with `auto` for those services in `bosun.yaml`
3. Push to Git, verify Bosun pulls images on next reconcile
4. Disable Watchtower for migrated services (or remove Watchtower entirely)

## Open Questions

- Should `auto` policy failure be non-fatal (warn and continue with cached image) or fatal (abort pipeline)? Current proposal: fatal, matching `add-image-prepull` behavior. Operators can switch to `pinned` if registry reliability is a concern.
- Should the policy affect drift reporting (suppress `image_mismatch` for `auto` services between reconciliations)?
