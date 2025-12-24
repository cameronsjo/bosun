# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.4](https://github.com/cameronsjo/bosun/compare/v0.2.3...v0.2.4) (2025-12-24)


### Features

* **daemon:** add native daemon mode with HTTP server ([dea3ade](https://github.com/cameronsjo/bosun/commit/dea3ade4dd8395e78304aef6182a9286270d59db))
* **daemon:** add Unix socket API with multi-provider webhook support ([a43308e](https://github.com/cameronsjo/bosun/commit/a43308e6177d36a0767278d55e8557e92ed95ca6))
* **daemon:** Unix socket API with multi-provider webhooks ([6298f80](https://github.com/cameronsjo/bosun/commit/6298f80dfe3bbd2d31f1b221936cf9d6ece6dd3f))


### Bug Fixes

* **lint:** fix all remaining errcheck issues in webhook.go ([bf8f3f7](https://github.com/cameronsjo/bosun/commit/bf8f3f7c4ea3ec8cd9e35a452eeaea2814b7e66d))
* **lint:** resolve errcheck issues in daemon package ([28b0f7b](https://github.com/cameronsjo/bosun/commit/28b0f7b6bd3295e4fd79fe6fb7ba0189260dbfab))
* **lint:** resolve remaining errcheck issues ([87507d4](https://github.com/cameronsjo/bosun/commit/87507d45555ad2ef1f0ea6f56507a0b8aa97d9a3))

## [0.2.3](https://github.com/cameronsjo/bosun/compare/v0.2.2...v0.2.3) (2025-12-24)


### Bug Fixes

* **docker:** simplify Dockerfile to use bosun daemon directly ([5f291ed](https://github.com/cameronsjo/bosun/commit/5f291ed97d0776d87f9c6040713f8ec283a9b322))

## [0.2.2](https://github.com/cameronsjo/bosun/compare/v0.2.1...v0.2.2) (2025-12-24)


### Bug Fixes

* **lint:** add nolint directives for deprecated Docker SDK types ([ff1dc4e](https://github.com/cameronsjo/bosun/commit/ff1dc4e740b6a3a07609f91540f6e606183dfb39))
* **lint:** fix remaining cmd.Help errcheck issues ([3a0b94d](https://github.com/cameronsjo/bosun/commit/3a0b94dae9ab304954ba092b701fb81af315f91a))
* **lint:** resolve all remaining errcheck issues ([4c618df](https://github.com/cameronsjo/bosun/commit/4c618dfc98877de1035f8ff61bd6f7902cee6119))
* **release:** fix goreleaser config and lint issues ([477102c](https://github.com/cameronsjo/bosun/commit/477102ccb5f810c4d6cd6efeb9ff4be0b751b251))

## [0.2.1](https://github.com/cameronsjo/bosun/compare/v0.2.0...v0.2.1) (2025-12-23)


### Features

* add bosun CLI and restore ASCII diagram to README ([1081e8d](https://github.com/cameronsjo/bosun/commit/1081e8d21f6846da3a1e3c79b6fb66d588ccadcf))
* **alert:** add native alerting system with Discord, SendGrid, Twilio ([7126cf4](https://github.com/cameronsjo/bosun/commit/7126cf48303c446f4aef07dc5289cca9fc816cd7))
* **ci:** add GitHub Actions CI/CD and self-update command ([fad639d](https://github.com/cameronsjo/bosun/commit/fad639d3b8ae24a0180de303802e942e817e7bea))
* **ci:** replace manual release with release-please ([d270336](https://github.com/cameronsjo/bosun/commit/d270336e631b05eee4d7cacb0285bee72527da8e))
* **cli:** add bosun drift command for config drift detection ([f615103](https://github.com/cameronsjo/bosun/commit/f61510340678d0ffb3d69e78c83766e597d9249a))
* **cli:** add bosun log command for release history ([1287ab6](https://github.com/cameronsjo/bosun/commit/1287ab68ba256fbcd99c61b52be8cc876ae1b579))
* **cli:** add core commands and P2 features ([e43080a](https://github.com/cameronsjo/bosun/commit/e43080a3eca55f30ad0c692a483103726c134d9d))
* **cli:** add secret pirate aliases 🏴‍☠️ ([7edd376](https://github.com/cameronsjo/bosun/commit/7edd3760a8461af1690dad4076e996acf9ec52a0))
* **composer:** implement service composer for Phase 1 ([537c2f4](https://github.com/cameronsjo/bosun/commit/537c2f401ea48ddf5c8673b558b57a4c0a84fa43))
* **go:** add comprehensive tests and release config (Phases 8-9) ([c48eb42](https://github.com/cameronsjo/bosun/commit/c48eb42ae495335a746902d564cf2a393a89103d))
* **go:** implement phases 2-5 in parallel ([78d62cd](https://github.com/cameronsjo/bosun/commit/78d62cd3ca7dc7d20bfcca4b1ff07c6cccd62bf4))
* **go:** implement phases 6-7 (init, comms, reconcile) ([6761e8c](https://github.com/cameronsjo/bosun/commit/6761e8caa9fb155b02c3fd26496a202d706e12b1))
* **go:** scaffold Go CLI foundation (Phase 1) ([6d7fcf9](https://github.com/cameronsjo/bosun/commit/6d7fcf9614229661c897037428062942094e4c8b))
* initial unops scaffold ([2f1b379](https://github.com/cameronsjo/bosun/commit/2f1b3798e148a27c52e59b98a23b81cc6d12b76b))
* **lint:** add port conflict detection ([957cf9a](https://github.com/cameronsjo/bosun/commit/957cf9af19aec6b1b9d83ed50b45b13d031b3175))
* **manifest:** add 'needs' shorthand for dependencies ([5df611e](https://github.com/cameronsjo/bosun/commit/5df611e9d541858efe15d4888f7cdda521d79859))
* **mayday:** add rollback snapshots ([5b54cc2](https://github.com/cameronsjo/bosun/commit/5b54cc250e38e6afc18dc6876b0352da3314f023))
* **provision:** add values overlays for env-specific config ([e07c238](https://github.com/cameronsjo/bosun/commit/e07c238f2a20d29bcec52bc6926a463ba34e11c8))
* rebrand to bosun with Below Deck nautical theme ([3672125](https://github.com/cameronsjo/bosun/commit/3672125f66c997be1aafaa103243dacac503abd1))
* **release:** add cosign signing, SLSA attestation, and install script ([62c5da6](https://github.com/cameronsjo/bosun/commit/62c5da61f0ae97826fb3da2fd56dc33014a6442f))
* **release:** add Docker image build to goreleaser ([2dd0297](https://github.com/cameronsjo/bosun/commit/2dd02974c86fda14e699d70484d64c196b520b12))
* remove external CLI dependencies, add schema versioning ([a248732](https://github.com/cameronsjo/bosun/commit/a2487329cf264594936e09e1a6fe96491f0fcc8d))


### Bug Fixes

* address critical and high severity production issues ([b84a025](https://github.com/cameronsjo/bosun/commit/b84a025a9ab3386d562578248a597b33e41dbc17))
* address critical edge cases from security analysis ([5926c4f](https://github.com/cameronsjo/bosun/commit/5926c4f876aba2cb1ba4f808e305f5fb4cc01785))
* address low-priority edge cases and improve UX ([a99a8a9](https://github.com/cameronsjo/bosun/commit/a99a8a977759d0abd2fb839191f4f7d33bf14543))
* address medium-priority edge cases and add preflight checks ([63d4fe8](https://github.com/cameronsjo/bosun/commit/63d4fe8f401ccf455235b0a4f24cdc6be739b9b2))
* address remaining high-priority edge cases ([a05f483](https://github.com/cameronsjo/bosun/commit/a05f483cd2337dedca1e242d3c7a4f484fbcd313))
* **ci:** bootstrap release-please and increase lint timeout ([46ff5fc](https://github.com/cameronsjo/bosun/commit/46ff5fc1b620f8079b8455e8a88365c707438e49))
* **lint:** resolve golangci-lint issues ([6d2f03b](https://github.com/cameronsjo/bosun/commit/6d2f03b696dc2c52231b88ee87ede049ae423ab5))
* **lint:** resolve remaining errcheck issues ([a5bc3a2](https://github.com/cameronsjo/bosun/commit/a5bc3a275cb14b0a897563fcbc7d6ca5385f1f07))

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
