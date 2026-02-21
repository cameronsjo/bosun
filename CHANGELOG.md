# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.5.0](https://github.com/cameronsjo/bosun/compare/v0.4.1...v0.5.0) (2026-02-21)


### Features

* add Claude Code plugin with onboarding skill ([db8bd2a](https://github.com/cameronsjo/bosun/commit/db8bd2a59a0d9ffc6a1eaf5ed4fa850a1717671c))
* **api:** add Homepage dashboard widget endpoint ([ce60071](https://github.com/cameronsjo/bosun/commit/ce60071fbc641e686a81ddb04c573a0cfe562048)), closes [#36](https://github.com/cameronsjo/bosun/issues/36)


### Bug Fixes

* **docker:** create /var/run/bosun directory in container image ([4f7a8fb](https://github.com/cameronsjo/bosun/commit/4f7a8fb3ba800ebb45977e17c907a746e103250f))

## [0.4.1](https://github.com/cameronsjo/bosun/compare/v0.4.0...v0.4.1) (2026-02-14)


### Bug Fixes

* **ci:** checkout release tag instead of HEAD for goreleaser ([d29549a](https://github.com/cameronsjo/bosun/commit/d29549af5b1d23bd11eeab94a29eb8eaeec507a7))

## [0.4.0](https://github.com/cameronsjo/bosun/compare/v0.3.1...v0.4.0) (2026-02-14)


### Features

* **ci:** add Docker image build+push to release workflow ([a57475d](https://github.com/cameronsjo/bosun/commit/a57475d6f3d3141a3e6e6ab2a5cba0d33e17358e))

## [0.3.1](https://github.com/cameronsjo/bosun/compare/v0.3.0...v0.3.1) (2026-02-14)


### Bug Fixes

* **ci:** move fromJSON out of env block to prevent template error ([5831547](https://github.com/cameronsjo/bosun/commit/58315474a81b92a681f58fd9442fbae8d7372523))

## [0.3.0](https://github.com/cameronsjo/bosun/compare/v0.2.10...v0.3.0) (2026-02-14)


### Features

* **logging:** comprehensive structured logging and fixes from code review ([c46a5af](https://github.com/cameronsjo/bosun/commit/c46a5afefad9cf4d7b88e462b30db036784b5343))
* **reconcile:** add openspec proposal for declared-vs-actual state feedback loop ([3173f28](https://github.com/cameronsjo/bosun/commit/3173f283eff3e910bf80593eaec32a783b30732e))
* **reconcile:** add state-based deploy tracking and circuit breaker ([da1f923](https://github.com/cameronsjo/bosun/commit/da1f9236509b90baa03e526ce7eb9c4ae15e339c))
* **reconcile:** add state-based deploy tracking and circuit breaker ([bf34cf2](https://github.com/cameronsjo/bosun/commit/bf34cf2519cee5d67b304eb852b1d80ac8f6d77f))
* **reconcile:** add state-based deploy tracking with circuit breaker ([#27](https://github.com/cameronsjo/bosun/issues/27)) ([da1f923](https://github.com/cameronsjo/bosun/commit/da1f9236509b90baa03e526ce7eb9c4ae15e339c))
* **reconcile:** declared-vs-actual state feedback loop with drift detection ([d11b07d](https://github.com/cameronsjo/bosun/commit/d11b07d79ec3f4160858a218f5491539d39f2e41))
* **reconcile:** implement declared-vs-actual state feedback loop ([2346c87](https://github.com/cameronsjo/bosun/commit/2346c8767578cadd5212e10f293cc91900224806))
* **sentry:** add opt-in error tracking and performance monitoring ([dd4d6f5](https://github.com/cameronsjo/bosun/commit/dd4d6f5901eaf15a2e62ff94a7ba7dc8aa386482))
* **sentry:** add opt-in error tracking and performance monitoring ([a04087f](https://github.com/cameronsjo/bosun/commit/a04087f4358d917157f01ee854e73c6463b271ca))


### Bug Fixes

* **ci:** bump minor version on feat commits pre-1.0 ([0f4bcdd](https://github.com/cameronsjo/bosun/commit/0f4bcdd85ca9c6e9ae4cd9b198ebf53305eca5c3))
* **ci:** increase max-turns and add timeout for code review ([5323f49](https://github.com/cameronsjo/bosun/commit/5323f49a303ad23a9156e15dc4797d9a0c670e73))
* **ci:** upgrade Claude workflow permissions to write for PRs and issues ([0b7a6ec](https://github.com/cameronsjo/bosun/commit/0b7a6ecb89f6392a22f79787be71263836d0b9c3))
* **drift:** address review findings from PR [#29](https://github.com/cameronsjo/bosun/issues/29) ([a409723](https://github.com/cameronsjo/bosun/commit/a409723434d993761eee64e4f44b833a73975015))
* **lint:** resolve errcheck and unused function lint errors ([7fc9e37](https://github.com/cameronsjo/bosun/commit/7fc9e373912dabeb613479c0a94859c1efeeaf0d))
* **lint:** resolve errcheck in drift command and daemon startup ([a1bbe25](https://github.com/cameronsjo/bosun/commit/a1bbe25068d86263c5915c5d77421013657298fb))
* **lint:** resolve errcheck violations and add local lint targets ([7c61406](https://github.com/cameronsjo/bosun/commit/7c614064aa4288a9876be81a482460378f92903d))
* **lock:** implement proper Windows file locking with LockFileEx ([a21df66](https://github.com/cameronsjo/bosun/commit/a21df6684bf0ba407deafeef2608c878ecb19b92))
* **observability:** add structured logging to drift detection pipeline ([f0bbdca](https://github.com/cameronsjo/bosun/commit/f0bbdcaf14fb0dce67d3c3ef0adfc4fa01b010d5))
* **openspec:** use SHALL in Drift Alerts requirement description ([6f969d7](https://github.com/cameronsjo/bosun/commit/6f969d7b0860f8b2d127bfd312ac242a7cc7417e))
* **reconcile:** skip permission test when running as root ([7aac83b](https://github.com/cameronsjo/bosun/commit/7aac83b2fc5dd488caeab00a72e2fc4230acf971))
* **reconcile:** use known_hosts for SSH host key verification ([f41eecd](https://github.com/cameronsjo/bosun/commit/f41eecd718bcc56d186c2826e8c229bc32b7a97f))
* resolve 11 bugs and code quality issues across codebase ([4c714a4](https://github.com/cameronsjo/bosun/commit/4c714a476bcc3f5b34bf9cc3ff6e39c2206a4ecd))
* resolve 3 low-hanging issues from codebase investigation ([e3cfedd](https://github.com/cameronsjo/bosun/commit/e3cfedd7e74cec0351b754cbe7dcff2e57db6960))
* resolve 3 low-hanging issues from codebase investigation ([06c2d31](https://github.com/cameronsjo/bosun/commit/06c2d31dc8023581320dfbc2be7c3ec04dde325c))
* resolve 6 just-do-it issues from backlog ([022c408](https://github.com/cameronsjo/bosun/commit/022c4083cbebf849c32d334f17e01de898e998c0))
* **sentry:** remove redundant io.Writer type assertion ([fca288a](https://github.com/cameronsjo/bosun/commit/fca288a839e21274cc46222af215392bab175d02))


### Performance Improvements

* **ci:** parallelize test, lint, and webui in Dagger pipeline ([26aaaa5](https://github.com/cameronsjo/bosun/commit/26aaaa5ae21e0b4636359c49b3beaa48312d61fd))

## [0.2.10](https://github.com/cameronsjo/bosun/compare/v0.2.9...v0.2.10) (2026-01-30)


### Features

* **ci:** add WebUI to Dagger pipeline ([5f97f14](https://github.com/cameronsjo/bosun/commit/5f97f14b90224a59bca5c5e5026a09f10576578f))
* **ci:** convert GitHub Actions to Dagger pipelines ([7364990](https://github.com/cameronsjo/bosun/commit/736499052370ff6d688b97b321c679d48db86e96))
* **ci:** convert GitHub Actions to Dagger pipelines ([3456db2](https://github.com/cameronsjo/bosun/commit/3456db2d84bc9a94b54f5ec3bc01452a371b2a80))
* **log:** add structured logging with zerolog ([871e65a](https://github.com/cameronsjo/bosun/commit/871e65a7162833ff67f3771092043dfa3f429476))
* **log:** add structured logging with zerolog ([cfc1eee](https://github.com/cameronsjo/bosun/commit/cfc1eeee289a02c657dae12cb54d1d79dff2b3a4))
* **manifest:** add Helm-aligned chart format ([#15](https://github.com/cameronsjo/bosun/issues/15)) ([aaa8e92](https://github.com/cameronsjo/bosun/commit/aaa8e92a8411707b7bdc048a56879340be96cc2c))
* **provision:** add project_name to compose output for container namespacing ([5772529](https://github.com/cameronsjo/bosun/commit/57725291ad1f230079ea70fafdba8d65da50e20b))
* **webui:** add React dashboard with maritime theme ([4a1348a](https://github.com/cameronsjo/bosun/commit/4a1348a137b9a929a6111e909823b82302f840c1))
* **webui:** add React dashboard with maritime theme ([60d973b](https://github.com/cameronsjo/bosun/commit/60d973b9d636f18dadb770a6f9eb825aa26f9f4d))


### Bug Fixes

* **ci:** disable cosign signing in goreleaser ([fcdd747](https://github.com/cameronsjo/bosun/commit/fcdd7473af61ba8b2205324fdacad1cc8bcffed9))
* **ci:** disable docker builds in goreleaser ([a24a9bb](https://github.com/cameronsjo/bosun/commit/a24a9bbe5073f4f600cdec1bd3cbe4496d059eba))
* **ci:** disable SLSA attestation step ([e8c922c](https://github.com/cameronsjo/bosun/commit/e8c922cc157e09ef3a26a876085125f2e9d16fbd))
* **ci:** install git in test container ([c25332b](https://github.com/cameronsjo/bosun/commit/c25332b54bb4127f73ebaba90f819e2d835dd7df))
* **ci:** remove -race flag from tests (requires CGO) ([6ba452c](https://github.com/cameronsjo/bosun/commit/6ba452c18c25c92667da08b7aae261f2cb3b3c31))
* **ci:** remove goreleaser prefix from exec commands ([02f0e00](https://github.com/cameronsjo/bosun/commit/02f0e00c581dfbbdf0354b553c88c7e5a084a5f2))
* **ci:** rename CIAll to All for proper CLI naming ([5593ee8](https://github.com/cameronsjo/bosun/commit/5593ee8587f4509b768a7e37c5ae5618eb8adb37))
* **ci:** rename Platform to buildTarget to avoid Dagger conflict ([9e43a40](https://github.com/cameronsjo/bosun/commit/9e43a40a33ddf98e8320173b87bae93f574adc38))
* **ci:** use correct dagger-for-github action ([a1c481b](https://github.com/cameronsjo/bosun/commit/a1c481b54015da8010a71cab3ca8b73be6c3cd11))
* **ci:** use full semver tag for dagger-for-github action ([6dedd29](https://github.com/cameronsjo/bosun/commit/6dedd29440310f129af8082ef7e746387dcf15eb))
* **ci:** use goreleaser:latest instead of non-existent v2 tag ([50d5ea7](https://github.com/cameronsjo/bosun/commit/50d5ea70ba8050dd5a5a2ff5aa7cff08263d6179))
* **docker:** add project name to compose commands to prevent orphan containers ([242c57c](https://github.com/cameronsjo/bosun/commit/242c57cceaece9a0c373fe43a9abf0cf33ec0a29))

## [0.2.10](https://github.com/cameronsjo/bosun/compare/v0.2.9...v0.2.10) (2026-01-30)


### Features

* **ci:** add WebUI to Dagger pipeline ([5f97f14](https://github.com/cameronsjo/bosun/commit/5f97f14b90224a59bca5c5e5026a09f10576578f))
* **ci:** convert GitHub Actions to Dagger pipelines ([7364990](https://github.com/cameronsjo/bosun/commit/736499052370ff6d688b97b321c679d48db86e96))
* **ci:** convert GitHub Actions to Dagger pipelines ([3456db2](https://github.com/cameronsjo/bosun/commit/3456db2d84bc9a94b54f5ec3bc01452a371b2a80))
* **log:** add structured logging with zerolog ([871e65a](https://github.com/cameronsjo/bosun/commit/871e65a7162833ff67f3771092043dfa3f429476))
* **log:** add structured logging with zerolog ([cfc1eee](https://github.com/cameronsjo/bosun/commit/cfc1eeee289a02c657dae12cb54d1d79dff2b3a4))
* **manifest:** add Helm-aligned chart format ([#15](https://github.com/cameronsjo/bosun/issues/15)) ([aaa8e92](https://github.com/cameronsjo/bosun/commit/aaa8e92a8411707b7bdc048a56879340be96cc2c))
* **provision:** add project_name to compose output for container namespacing ([5772529](https://github.com/cameronsjo/bosun/commit/57725291ad1f230079ea70fafdba8d65da50e20b))
* **webui:** add React dashboard with maritime theme ([4a1348a](https://github.com/cameronsjo/bosun/commit/4a1348a137b9a929a6111e909823b82302f840c1))
* **webui:** add React dashboard with maritime theme ([60d973b](https://github.com/cameronsjo/bosun/commit/60d973b9d636f18dadb770a6f9eb825aa26f9f4d))


### Bug Fixes

* **ci:** disable cosign signing in goreleaser ([fcdd747](https://github.com/cameronsjo/bosun/commit/fcdd7473af61ba8b2205324fdacad1cc8bcffed9))
* **ci:** disable docker builds in goreleaser ([a24a9bb](https://github.com/cameronsjo/bosun/commit/a24a9bbe5073f4f600cdec1bd3cbe4496d059eba))
* **ci:** install git in test container ([c25332b](https://github.com/cameronsjo/bosun/commit/c25332b54bb4127f73ebaba90f819e2d835dd7df))
* **ci:** remove -race flag from tests (requires CGO) ([6ba452c](https://github.com/cameronsjo/bosun/commit/6ba452c18c25c92667da08b7aae261f2cb3b3c31))
* **ci:** remove goreleaser prefix from exec commands ([02f0e00](https://github.com/cameronsjo/bosun/commit/02f0e00c581dfbbdf0354b553c88c7e5a084a5f2))
* **ci:** rename CIAll to All for proper CLI naming ([5593ee8](https://github.com/cameronsjo/bosun/commit/5593ee8587f4509b768a7e37c5ae5618eb8adb37))
* **ci:** rename Platform to buildTarget to avoid Dagger conflict ([9e43a40](https://github.com/cameronsjo/bosun/commit/9e43a40a33ddf98e8320173b87bae93f574adc38))
* **ci:** use correct dagger-for-github action ([a1c481b](https://github.com/cameronsjo/bosun/commit/a1c481b54015da8010a71cab3ca8b73be6c3cd11))
* **ci:** use full semver tag for dagger-for-github action ([6dedd29](https://github.com/cameronsjo/bosun/commit/6dedd29440310f129af8082ef7e746387dcf15eb))
* **ci:** use goreleaser:latest instead of non-existent v2 tag ([50d5ea7](https://github.com/cameronsjo/bosun/commit/50d5ea70ba8050dd5a5a2ff5aa7cff08263d6179))
* **docker:** add project name to compose commands to prevent orphan containers ([242c57c](https://github.com/cameronsjo/bosun/commit/242c57cceaece9a0c373fe43a9abf0cf33ec0a29))

## [0.2.10](https://github.com/cameronsjo/bosun/compare/v0.2.9...v0.2.10) (2026-01-30)


### Features

* **ci:** add WebUI to Dagger pipeline ([5f97f14](https://github.com/cameronsjo/bosun/commit/5f97f14b90224a59bca5c5e5026a09f10576578f))
* **ci:** convert GitHub Actions to Dagger pipelines ([7364990](https://github.com/cameronsjo/bosun/commit/736499052370ff6d688b97b321c679d48db86e96))
* **ci:** convert GitHub Actions to Dagger pipelines ([3456db2](https://github.com/cameronsjo/bosun/commit/3456db2d84bc9a94b54f5ec3bc01452a371b2a80))
* **log:** add structured logging with zerolog ([871e65a](https://github.com/cameronsjo/bosun/commit/871e65a7162833ff67f3771092043dfa3f429476))
* **log:** add structured logging with zerolog ([cfc1eee](https://github.com/cameronsjo/bosun/commit/cfc1eeee289a02c657dae12cb54d1d79dff2b3a4))
* **manifest:** add Helm-aligned chart format ([#15](https://github.com/cameronsjo/bosun/issues/15)) ([aaa8e92](https://github.com/cameronsjo/bosun/commit/aaa8e92a8411707b7bdc048a56879340be96cc2c))
* **provision:** add project_name to compose output for container namespacing ([5772529](https://github.com/cameronsjo/bosun/commit/57725291ad1f230079ea70fafdba8d65da50e20b))
* **webui:** add React dashboard with maritime theme ([4a1348a](https://github.com/cameronsjo/bosun/commit/4a1348a137b9a929a6111e909823b82302f840c1))
* **webui:** add React dashboard with maritime theme ([60d973b](https://github.com/cameronsjo/bosun/commit/60d973b9d636f18dadb770a6f9eb825aa26f9f4d))


### Bug Fixes

* **ci:** disable cosign signing in goreleaser ([fcdd747](https://github.com/cameronsjo/bosun/commit/fcdd7473af61ba8b2205324fdacad1cc8bcffed9))
* **ci:** install git in test container ([c25332b](https://github.com/cameronsjo/bosun/commit/c25332b54bb4127f73ebaba90f819e2d835dd7df))
* **ci:** remove -race flag from tests (requires CGO) ([6ba452c](https://github.com/cameronsjo/bosun/commit/6ba452c18c25c92667da08b7aae261f2cb3b3c31))
* **ci:** remove goreleaser prefix from exec commands ([02f0e00](https://github.com/cameronsjo/bosun/commit/02f0e00c581dfbbdf0354b553c88c7e5a084a5f2))
* **ci:** rename CIAll to All for proper CLI naming ([5593ee8](https://github.com/cameronsjo/bosun/commit/5593ee8587f4509b768a7e37c5ae5618eb8adb37))
* **ci:** rename Platform to buildTarget to avoid Dagger conflict ([9e43a40](https://github.com/cameronsjo/bosun/commit/9e43a40a33ddf98e8320173b87bae93f574adc38))
* **ci:** use correct dagger-for-github action ([a1c481b](https://github.com/cameronsjo/bosun/commit/a1c481b54015da8010a71cab3ca8b73be6c3cd11))
* **ci:** use full semver tag for dagger-for-github action ([6dedd29](https://github.com/cameronsjo/bosun/commit/6dedd29440310f129af8082ef7e746387dcf15eb))
* **ci:** use goreleaser:latest instead of non-existent v2 tag ([50d5ea7](https://github.com/cameronsjo/bosun/commit/50d5ea70ba8050dd5a5a2ff5aa7cff08263d6179))
* **docker:** add project name to compose commands to prevent orphan containers ([242c57c](https://github.com/cameronsjo/bosun/commit/242c57cceaece9a0c373fe43a9abf0cf33ec0a29))

## [0.2.10](https://github.com/cameronsjo/bosun/compare/v0.2.9...v0.2.10) (2026-01-30)


### Features

* **ci:** add WebUI to Dagger pipeline ([5f97f14](https://github.com/cameronsjo/bosun/commit/5f97f14b90224a59bca5c5e5026a09f10576578f))
* **ci:** convert GitHub Actions to Dagger pipelines ([7364990](https://github.com/cameronsjo/bosun/commit/736499052370ff6d688b97b321c679d48db86e96))
* **ci:** convert GitHub Actions to Dagger pipelines ([3456db2](https://github.com/cameronsjo/bosun/commit/3456db2d84bc9a94b54f5ec3bc01452a371b2a80))
* **log:** add structured logging with zerolog ([871e65a](https://github.com/cameronsjo/bosun/commit/871e65a7162833ff67f3771092043dfa3f429476))
* **log:** add structured logging with zerolog ([cfc1eee](https://github.com/cameronsjo/bosun/commit/cfc1eeee289a02c657dae12cb54d1d79dff2b3a4))
* **manifest:** add Helm-aligned chart format ([#15](https://github.com/cameronsjo/bosun/issues/15)) ([aaa8e92](https://github.com/cameronsjo/bosun/commit/aaa8e92a8411707b7bdc048a56879340be96cc2c))
* **provision:** add project_name to compose output for container namespacing ([5772529](https://github.com/cameronsjo/bosun/commit/57725291ad1f230079ea70fafdba8d65da50e20b))
* **webui:** add React dashboard with maritime theme ([4a1348a](https://github.com/cameronsjo/bosun/commit/4a1348a137b9a929a6111e909823b82302f840c1))
* **webui:** add React dashboard with maritime theme ([60d973b](https://github.com/cameronsjo/bosun/commit/60d973b9d636f18dadb770a6f9eb825aa26f9f4d))


### Bug Fixes

* **ci:** install git in test container ([c25332b](https://github.com/cameronsjo/bosun/commit/c25332b54bb4127f73ebaba90f819e2d835dd7df))
* **ci:** remove -race flag from tests (requires CGO) ([6ba452c](https://github.com/cameronsjo/bosun/commit/6ba452c18c25c92667da08b7aae261f2cb3b3c31))
* **ci:** remove goreleaser prefix from exec commands ([02f0e00](https://github.com/cameronsjo/bosun/commit/02f0e00c581dfbbdf0354b553c88c7e5a084a5f2))
* **ci:** rename CIAll to All for proper CLI naming ([5593ee8](https://github.com/cameronsjo/bosun/commit/5593ee8587f4509b768a7e37c5ae5618eb8adb37))
* **ci:** rename Platform to buildTarget to avoid Dagger conflict ([9e43a40](https://github.com/cameronsjo/bosun/commit/9e43a40a33ddf98e8320173b87bae93f574adc38))
* **ci:** use correct dagger-for-github action ([a1c481b](https://github.com/cameronsjo/bosun/commit/a1c481b54015da8010a71cab3ca8b73be6c3cd11))
* **ci:** use full semver tag for dagger-for-github action ([6dedd29](https://github.com/cameronsjo/bosun/commit/6dedd29440310f129af8082ef7e746387dcf15eb))
* **ci:** use goreleaser:latest instead of non-existent v2 tag ([50d5ea7](https://github.com/cameronsjo/bosun/commit/50d5ea70ba8050dd5a5a2ff5aa7cff08263d6179))
* **docker:** add project name to compose commands to prevent orphan containers ([242c57c](https://github.com/cameronsjo/bosun/commit/242c57cceaece9a0c373fe43a9abf0cf33ec0a29))

## [0.2.10](https://github.com/cameronsjo/bosun/compare/v0.2.9...v0.2.10) (2026-01-30)


### Features

* **ci:** add WebUI to Dagger pipeline ([5f97f14](https://github.com/cameronsjo/bosun/commit/5f97f14b90224a59bca5c5e5026a09f10576578f))
* **ci:** convert GitHub Actions to Dagger pipelines ([7364990](https://github.com/cameronsjo/bosun/commit/736499052370ff6d688b97b321c679d48db86e96))
* **ci:** convert GitHub Actions to Dagger pipelines ([3456db2](https://github.com/cameronsjo/bosun/commit/3456db2d84bc9a94b54f5ec3bc01452a371b2a80))
* **log:** add structured logging with zerolog ([871e65a](https://github.com/cameronsjo/bosun/commit/871e65a7162833ff67f3771092043dfa3f429476))
* **log:** add structured logging with zerolog ([cfc1eee](https://github.com/cameronsjo/bosun/commit/cfc1eeee289a02c657dae12cb54d1d79dff2b3a4))
* **manifest:** add Helm-aligned chart format ([#15](https://github.com/cameronsjo/bosun/issues/15)) ([aaa8e92](https://github.com/cameronsjo/bosun/commit/aaa8e92a8411707b7bdc048a56879340be96cc2c))
* **provision:** add project_name to compose output for container namespacing ([5772529](https://github.com/cameronsjo/bosun/commit/57725291ad1f230079ea70fafdba8d65da50e20b))
* **webui:** add React dashboard with maritime theme ([4a1348a](https://github.com/cameronsjo/bosun/commit/4a1348a137b9a929a6111e909823b82302f840c1))
* **webui:** add React dashboard with maritime theme ([60d973b](https://github.com/cameronsjo/bosun/commit/60d973b9d636f18dadb770a6f9eb825aa26f9f4d))


### Bug Fixes

* **ci:** install git in test container ([c25332b](https://github.com/cameronsjo/bosun/commit/c25332b54bb4127f73ebaba90f819e2d835dd7df))
* **ci:** remove -race flag from tests (requires CGO) ([6ba452c](https://github.com/cameronsjo/bosun/commit/6ba452c18c25c92667da08b7aae261f2cb3b3c31))
* **ci:** rename CIAll to All for proper CLI naming ([5593ee8](https://github.com/cameronsjo/bosun/commit/5593ee8587f4509b768a7e37c5ae5618eb8adb37))
* **ci:** rename Platform to buildTarget to avoid Dagger conflict ([9e43a40](https://github.com/cameronsjo/bosun/commit/9e43a40a33ddf98e8320173b87bae93f574adc38))
* **ci:** use correct dagger-for-github action ([a1c481b](https://github.com/cameronsjo/bosun/commit/a1c481b54015da8010a71cab3ca8b73be6c3cd11))
* **ci:** use full semver tag for dagger-for-github action ([6dedd29](https://github.com/cameronsjo/bosun/commit/6dedd29440310f129af8082ef7e746387dcf15eb))
* **ci:** use goreleaser:latest instead of non-existent v2 tag ([50d5ea7](https://github.com/cameronsjo/bosun/commit/50d5ea70ba8050dd5a5a2ff5aa7cff08263d6179))
* **docker:** add project name to compose commands to prevent orphan containers ([242c57c](https://github.com/cameronsjo/bosun/commit/242c57cceaece9a0c373fe43a9abf0cf33ec0a29))

## [0.2.10](https://github.com/cameronsjo/bosun/compare/v0.2.9...v0.2.10) (2026-01-30)


### Features

* **ci:** add WebUI to Dagger pipeline ([5f97f14](https://github.com/cameronsjo/bosun/commit/5f97f14b90224a59bca5c5e5026a09f10576578f))
* **ci:** convert GitHub Actions to Dagger pipelines ([7364990](https://github.com/cameronsjo/bosun/commit/736499052370ff6d688b97b321c679d48db86e96))
* **ci:** convert GitHub Actions to Dagger pipelines ([3456db2](https://github.com/cameronsjo/bosun/commit/3456db2d84bc9a94b54f5ec3bc01452a371b2a80))
* **log:** add structured logging with zerolog ([871e65a](https://github.com/cameronsjo/bosun/commit/871e65a7162833ff67f3771092043dfa3f429476))
* **log:** add structured logging with zerolog ([cfc1eee](https://github.com/cameronsjo/bosun/commit/cfc1eeee289a02c657dae12cb54d1d79dff2b3a4))
* **manifest:** add Helm-aligned chart format ([#15](https://github.com/cameronsjo/bosun/issues/15)) ([aaa8e92](https://github.com/cameronsjo/bosun/commit/aaa8e92a8411707b7bdc048a56879340be96cc2c))
* **provision:** add project_name to compose output for container namespacing ([5772529](https://github.com/cameronsjo/bosun/commit/57725291ad1f230079ea70fafdba8d65da50e20b))
* **webui:** add React dashboard with maritime theme ([4a1348a](https://github.com/cameronsjo/bosun/commit/4a1348a137b9a929a6111e909823b82302f840c1))
* **webui:** add React dashboard with maritime theme ([60d973b](https://github.com/cameronsjo/bosun/commit/60d973b9d636f18dadb770a6f9eb825aa26f9f4d))


### Bug Fixes

* **ci:** install git in test container ([c25332b](https://github.com/cameronsjo/bosun/commit/c25332b54bb4127f73ebaba90f819e2d835dd7df))
* **ci:** remove -race flag from tests (requires CGO) ([6ba452c](https://github.com/cameronsjo/bosun/commit/6ba452c18c25c92667da08b7aae261f2cb3b3c31))
* **ci:** rename CIAll to All for proper CLI naming ([5593ee8](https://github.com/cameronsjo/bosun/commit/5593ee8587f4509b768a7e37c5ae5618eb8adb37))
* **ci:** rename Platform to buildTarget to avoid Dagger conflict ([9e43a40](https://github.com/cameronsjo/bosun/commit/9e43a40a33ddf98e8320173b87bae93f574adc38))
* **ci:** use correct dagger-for-github action ([a1c481b](https://github.com/cameronsjo/bosun/commit/a1c481b54015da8010a71cab3ca8b73be6c3cd11))
* **ci:** use full semver tag for dagger-for-github action ([6dedd29](https://github.com/cameronsjo/bosun/commit/6dedd29440310f129af8082ef7e746387dcf15eb))
* **docker:** add project name to compose commands to prevent orphan containers ([242c57c](https://github.com/cameronsjo/bosun/commit/242c57cceaece9a0c373fe43a9abf0cf33ec0a29))

## [0.2.9](https://github.com/cameronsjo/bosun/compare/v0.2.8...v0.2.9) (2026-01-02)


### Features

* **manifest:** add compose overrides and network merging ([9ca81d3](https://github.com/cameronsjo/bosun/commit/9ca81d3255f5ae9e9580906591542a067abcea8f))

## [0.2.8](https://github.com/cameronsjo/bosun/compare/v0.2.7...v0.2.8) (2026-01-02)


### Bug Fixes

* **reconcile:** deploy all compose files, not just core.yml ([98d470e](https://github.com/cameronsjo/bosun/commit/98d470e1846f00b00b6d5a6c55860ae88b5e0d98))

## [0.2.7](https://github.com/cameronsjo/bosun/compare/v0.2.6...v0.2.7) (2026-01-02)


### Features

* **git:** add SSH key file support for git operations ([fb26cde](https://github.com/cameronsjo/bosun/commit/fb26cde35b9e044f46d7061a2fb36e4a9140fe86))


### Bug Fixes

* **config:** change InfraSubDir default from 'infrastructure' to '.' ([7759a86](https://github.com/cameronsjo/bosun/commit/7759a86139413a8343f6b9da2ffddfb2bc48c278))
* **reconcile:** override go-git DefaultAuthBuilder for SSH without agent ([4b0b831](https://github.com/cameronsjo/bosun/commit/4b0b83192ed92d74c672fe2bbdd116e263748ff7))

## [0.2.6](https://github.com/cameronsjo/bosun/compare/v0.2.5...v0.2.6) (2025-12-26)


### Features

* add native daemon mode with Unix socket API and webhook support ([34d05cf](https://github.com/cameronsjo/bosun/commit/34d05cf74f39ebc26d897c1265a3c4a17d27da4b))
* **cli:** add render command for local template preview ([b454313](https://github.com/cameronsjo/bosun/commit/b45431353561d943f36860918b5bd05b4badfcac))


### Bug Fixes

* **lint:** remove unused completeProvisionNames function ([e5e27e6](https://github.com/cameronsjo/bosun/commit/e5e27e6f6daa7d1ee51e199687c71a295e1e7f86))

## [0.2.5](https://github.com/cameronsjo/bosun/compare/v0.2.4...v0.2.5) (2025-12-24)


### Features

* **daemon:** add BOSUN_INFRA_DIR env var support ([84f74a3](https://github.com/cameronsjo/bosun/commit/84f74a3dc23e9d207f1310b695ce5d1f666e92dd))

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
