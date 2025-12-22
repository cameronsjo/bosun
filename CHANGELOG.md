# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Schema versioning**: Manifests now support `apiVersion` and `kind` fields
  - `apiVersion: bosun.io/v1` for explicit version tracking
  - `kind: Provision|Stack|Service` for manifest type identification
  - New `bosun migrate` command to upgrade unversioned manifests
  - Backwards compatible - unversioned manifests work with warning
- **Manifest Phase 1**: Core renderer with provision-based service composition
  - 7 provisions: container, healthcheck, homepage, reverse-proxy, monitoring, postgres, redis
  - Variable interpolation with `${var}` syntax
  - Deep merge with proper semantics (dict merge, list replace, network union)
  - Sidecar injection for postgres/redis
  - Multi-target output: compose, traefik, gatus
- **Bosun**: GitOps orchestrator
  - Dockerfile with sops, age, webhook
  - Reconciliation script structure
  - Health check and notification scripts
- **Documentation**: 9 ADRs covering architecture decisions

### Changed

- **Template engine**: Migrated from chezmoi to native Go `text/template` with Sprig functions
  - No external binary dependency required
  - Secrets processed entirely in-memory (improved security)
  - All Sprig functions now available
  - Same Go template syntax - no breaking changes to existing templates
- Rebranded to "bosun" with Below Deck nautical theme
- Renamed conductor → bosun, composer → manifest, profiles → provisions

### Removed

- **chezmoi dependency**: Template rendering now uses built-in Go templates

## [0.1.0] - TBD

Initial release. Coming soon.
