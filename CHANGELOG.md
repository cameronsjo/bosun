# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Composer Phase 1**: Core renderer with profile-based service composition
  - 7 profiles: container, healthcheck, homepage, reverse-proxy, monitoring, postgres, redis
  - Variable interpolation with `${var}` syntax
  - Deep merge with proper semantics (dict merge, list replace, network union)
  - Sidecar injection for postgres/redis
  - Multi-target output: compose, traefik, gatus
- **Conductor**: GitOps orchestrator skeleton
  - Dockerfile with sops, age, chezmoi, webhook
  - Reconciliation script structure
  - Health check and notification scripts
- **Documentation**: 6 ADRs covering architecture decisions
  - ADR-0001: Service Composer
  - ADR-0002: Watchtower Webhook Deploy
  - ADR-0003: Dagger for Conductor (deferred)
  - ADR-0004: Multi-Server Monorepo
  - ADR-0005: Tunnel Providers (Tailscale vs Cloudflare)
  - ADR-0006: Conductor Authentication

### Changed

- Renamed "runner" to "conductor" (musical theme: conductor leads the orchestra)

## [0.1.0] - TBD

Initial release. Coming soon.
