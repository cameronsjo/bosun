# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.42.2](https://github.com/cameronsjo/bosun/compare/v0.42.1...v0.42.2) (2026-08-29)


### Bug Fixes

* **reconcile:** reject unsafe target sets ([#639](https://github.com/cameronsjo/bosun/issues/639)) ([0a9f5d4](https://github.com/cameronsjo/bosun/commit/0a9f5d4ac885de5ff617a113a9801edea47ef67b))

## [0.42.1](https://github.com/cameronsjo/bosun/compare/v0.42.0...v0.42.1) (2026-08-29)


### Bug Fixes

* **reconcile:** ground FUSE hook fallback behavior ([#634](https://github.com/cameronsjo/bosun/issues/634)) ([aaa182a](https://github.com/cameronsjo/bosun/commit/aaa182ac47f38222f0cc6f946c75b70351e7ed30))

## [0.42.0](https://github.com/cameronsjo/bosun/compare/v0.41.0...v0.42.0) (2026-08-28)


### Features

* **reconcile:** distinguish interrupted attempts ([#626](https://github.com/cameronsjo/bosun/issues/626)) ([5e4e517](https://github.com/cameronsjo/bosun/commit/5e4e5177cbc76681c5aff5ae18c9aed0b2df621f))

## [0.41.0](https://github.com/cameronsjo/bosun/compare/v0.40.7...v0.41.0) (2026-08-26)


### Features

* **update:** verify self-update release checksums ([#607](https://github.com/cameronsjo/bosun/issues/607)) ([4f6e2d8](https://github.com/cameronsjo/bosun/commit/4f6e2d85331c466f5302278636cb2382bfdcb758))


### Bug Fixes

* **fileutil:** reject blocking special files ([#605](https://github.com/cameronsjo/bosun/issues/605)) ([c1340fc](https://github.com/cameronsjo/bosun/commit/c1340fcff569b76749ab8d20784da98e27a2cb04))
* **manifest:** synchronize template cache ([#606](https://github.com/cameronsjo/bosun/issues/606)) ([ab11496](https://github.com/cameronsjo/bosun/commit/ab11496c42e4007c962f8c50a988356c9ece7a19))
* **preflight:** reject unusable SSH key paths ([#601](https://github.com/cameronsjo/bosun/issues/601)) ([bc73ee2](https://github.com/cameronsjo/bosun/commit/bc73ee27041715afcc89c9bfec505018daef3adf))
* **update:** propagate cancellation and command failures ([#602](https://github.com/cameronsjo/bosun/issues/602)) ([1f80dae](https://github.com/cameronsjo/bosun/commit/1f80dae94148ddae82793207ce6be169cb596acf))

## [0.40.7](https://github.com/cameronsjo/bosun/compare/v0.40.6...v0.40.7) (2026-08-26)


### Bug Fixes

* **alert:** bound Twilio error responses ([a1c796b](https://github.com/cameronsjo/bosun/commit/a1c796bd53be33bc67cb343e5a6590f66f60e131))
* **ci:** recover releases after invalid app credentials ([7390404](https://github.com/cameronsjo/bosun/commit/7390404e357165c62868fb85056a7ffac4c5c7ad))
* **dev:** harden agent disk guard limits ([c5c69e4](https://github.com/cameronsjo/bosun/commit/c5c69e4b399721fb3a3a9ef702b181438a4be378))
* **fileutil:** bound equal-hash comparisons ([6de24f5](https://github.com/cameronsjo/bosun/commit/6de24f596aca30aa60c802f0564638a086dee6c2))
* **lock:** preserve lock inode across releases ([a012e2c](https://github.com/cameronsjo/bosun/commit/a012e2caaa7699d22b62b22298b14f7671536e18))
* **log:** preserve configured writers in daemon mode ([2536faa](https://github.com/cameronsjo/bosun/commit/2536faafbb92eeecdb8ee629b46ccacc1f88d6e1))
* **sentry:** synchronize lifecycle state ([eef5a5f](https://github.com/cameronsjo/bosun/commit/eef5a5fa9b89126ef01b32645a29e61a24f6bf11))
* **snapshot:** preserve rollback failure causes ([99211eb](https://github.com/cameronsjo/bosun/commit/99211ebbc025dfe54190759a3b576ad5315217f7))
* **telemetry:** validate OTLP endpoint URLs ([cbb4388](https://github.com/cameronsjo/bosun/commit/cbb4388fe3f584915633fed881f1613e6cb1093d))
* **tunnel:** make hostname cache race-safe ([fea5f05](https://github.com/cameronsjo/bosun/commit/fea5f0593bba4877921146c570bbfb70739c012d))

## [0.40.6](https://github.com/cameronsjo/bosun/compare/v0.40.5...v0.40.6) (2026-08-26)


### Bug Fixes

* **cmd:** stabilize compose service completions ([db406fb](https://github.com/cameronsjo/bosun/commit/db406fba78a6021a3514e07ad69cabef949d9a16))
* **daemon:** bound public health responses ([110b863](https://github.com/cameronsjo/bosun/commit/110b863d41f6883e120bacb90a39f272dcc151fb))
* **daemon:** preserve client response read errors ([d1acaaf](https://github.com/cameronsjo/bosun/commit/d1acaaf4db148e539a237dfc7eb9492ed7dae462))
* **update:** handle development version checks safely ([27efc3a](https://github.com/cameronsjo/bosun/commit/27efc3a8aab3e4f102d45b30bbb77c5f52b8d8a3))

## [0.40.5](https://github.com/cameronsjo/bosun/compare/v0.40.4...v0.40.5) (2026-08-26)


### Bug Fixes

* **docker:** make response reads cancellation-safe ([#579](https://github.com/cameronsjo/bosun/issues/579)) ([c97290f](https://github.com/cameronsjo/bosun/commit/c97290fe7e69f2942ba50ac35f4c852a42c747f6))

## [0.40.4](https://github.com/cameronsjo/bosun/compare/v0.40.3...v0.40.4) (2026-08-26)


### Bug Fixes

* **cmd:** preserve alert test error semantics ([#575](https://github.com/cameronsjo/bosun/issues/575)) ([273d92b](https://github.com/cameronsjo/bosun/commit/273d92b6577dea0447279ee245ff636b6f61bd19))
* **reconcile:** defer backup retention until success ([3adad60](https://github.com/cameronsjo/bosun/commit/3adad60ddf85bbcc11f3bcdc823bab9c386151ee))
* **reconcile:** make local sync cancellation-safe ([#572](https://github.com/cameronsjo/bosun/issues/572)) ([3465a0e](https://github.com/cameronsjo/bosun/commit/3465a0e69820616389d6dc66ac89c8d9dd70e6c4))
* **reconcile:** validate mounted secrets before startup ([#574](https://github.com/cameronsjo/bosun/issues/574)) ([28994fb](https://github.com/cameronsjo/bosun/commit/28994fb65a5df9979b283dd84fc42ca0902aa6da))

## [0.40.3](https://github.com/cameronsjo/bosun/compare/v0.40.2...v0.40.3) (2026-08-24)


### Bug Fixes

* **container:** persist writable deploy state ([e446b56](https://github.com/cameronsjo/bosun/commit/e446b56124a67a77b7fa7c86db8c4748891f6078)), refs [#478](https://github.com/cameronsjo/bosun/issues/478)
* **fileutil:** reject recursive copy destinations ([31b1599](https://github.com/cameronsjo/bosun/commit/31b15993fc993fb01f2c29a14a41ccf9fd8661af)), refs [#435](https://github.com/cameronsjo/bosun/issues/435)

## [0.40.2](https://github.com/cameronsjo/bosun/compare/v0.40.1...v0.40.2) (2026-08-24)


### Bug Fixes

* **cli:** honor cancellation in setup and diagnostics ([7460c47](https://github.com/cameronsjo/bosun/commit/7460c47a72adcaf5cc99a65c2c81a7a67e8449e9))
* **daemon:** bound client error responses ([e56a650](https://github.com/cameronsjo/bosun/commit/e56a650c084d8b291c7aaf225ffc641f2ddeb9d1))
* **fileutil:** track created deploy directories ([f6ed4a3](https://github.com/cameronsjo/bosun/commit/f6ed4a34d76b067d9dae7748d6a78fbdbdbc378f))
* **reconcile:** clear stale state on path skip ([40547ab](https://github.com/cameronsjo/bosun/commit/40547ab091ed143bd5fda4184620d7c16194edf6)), closes [#361](https://github.com/cameronsjo/bosun/issues/361)
* **reconcile:** deliver alerts after cancellation ([70d9df4](https://github.com/cameronsjo/bosun/commit/70d9df436ef4253d764e2afaab06b6acd17195fc)), refs [#242](https://github.com/cameronsjo/bosun/issues/242)
* **reconcile:** harden local rollback extraction ([537a5e3](https://github.com/cameronsjo/bosun/commit/537a5e30dbc959f05aa8fb0ca9c1488100917093)), closes [#449](https://github.com/cameronsjo/bosun/issues/449)
* **reconcile:** reject colliding target state files ([66ef6a7](https://github.com/cameronsjo/bosun/commit/66ef6a70dd1e8cce4bafe68fdc6a4c4e7b588531)), closes [#260](https://github.com/cameronsjo/bosun/issues/260)
* **reconcile:** skip empty remote backups ([74c7268](https://github.com/cameronsjo/bosun/commit/74c7268241bf8bc6ed2074712039ebf492ebe53f)), closes [#459](https://github.com/cameronsjo/bosun/issues/459)


### Performance Improvements

* **fileutil:** batch destination directory syncs ([41a06ee](https://github.com/cameronsjo/bosun/commit/41a06eed3da9697ad1cb91daf65974d4bb6f1890)), closes [#414](https://github.com/cameronsjo/bosun/issues/414)

## [0.40.1](https://github.com/cameronsjo/bosun/compare/v0.40.0...v0.40.1) (2026-08-24)


### Bug Fixes

* **daemon:** authorize Unix socket peer UIDs ([c8f6ea0](https://github.com/cameronsjo/bosun/commit/c8f6ea0bdaf730d123576ab0fe4df3553bda656d))
* **daemon:** bound drift self-heal attempts ([eb7e630](https://github.com/cameronsjo/bosun/commit/eb7e63045515f2237a7dae3ba02d15afe81ec977))
* **daemon:** close socket cleanup race ([f6eddda](https://github.com/cameronsjo/bosun/commit/f6eddda06346c473ca404328ff04c96aa331fb14))
* **daemon:** pin socket ownership identity ([66f35cd](https://github.com/cameronsjo/bosun/commit/66f35cda2ef13c57eb6514adfcbae4dc4ffd2d90))
* **daemon:** preserve trigger lifecycle correlation ([9916b22](https://github.com/cameronsjo/bosun/commit/9916b227a84b29e75efa5a454144e424a9a72f91))
* **daemon:** publish Unix socket at configured mode ([e112aaa](https://github.com/cameronsjo/bosun/commit/e112aaa258c0c9d21b222a15bb4a00cb2edc6112))
* **daemon:** serialize drift self-heal admission ([dd3511d](https://github.com/cameronsjo/bosun/commit/dd3511d1e86b52b5d196e60e9b788af65e7610e4))
* **daemon:** track trigger reconciles through shutdown ([321d0ba](https://github.com/cameronsjo/bosun/commit/321d0ba284594b3469a450ba25908be1a32a6868))
* **reconcile:** bound hook mismatch diagnostics ([fe187fb](https://github.com/cameronsjo/bosun/commit/fe187fb8288cbb09a15c4ff7d319e34b81a164c3))
* **reconcile:** cancel compose process groups ([8d1661f](https://github.com/cameronsjo/bosun/commit/8d1661fb0a47cdae3f8f4eae00c620a8482a5154))
* **reconcile:** cancel compose work gracefully ([06d7360](https://github.com/cameronsjo/bosun/commit/06d7360c9b245a620ffe5e4daf49d234673d23f5))
* **reconcile:** close staging evidence race gaps ([3d4190d](https://github.com/cameronsjo/bosun/commit/3d4190dc29a7abfe84f2cf69e6717df11e1794d2))
* **reconcile:** preserve failed staging evidence ([1ad8e29](https://github.com/cameronsjo/bosun/commit/1ad8e294efebfc4024801aacf9749984d5035c04))
* **reconcile:** preserve hook reload ownership ([eb90dd6](https://github.com/cameronsjo/bosun/commit/eb90dd6c7ca85a12dca3d5a13aec0757ab08e760))
* **reconcile:** preserve hook reload snapshots ([24d511b](https://github.com/cameronsjo/bosun/commit/24d511be0c46118d333baad23aa7ca0202d76918))
* **reconcile:** report zero matched hook files ([de4142a](https://github.com/cameronsjo/bosun/commit/de4142a59b40c2a6b885f98ecc620f2636c9e473))
* **reconcile:** surface unmatched hook patterns ([323f0aa](https://github.com/cameronsjo/bosun/commit/323f0aa033fdd53ba13afecf402aec6a1ebe4c4a))
* **reconcile:** verify remote archives before promotion ([131699b](https://github.com/cameronsjo/bosun/commit/131699b9525cd09a1bd5f2e04647ae4aeea3084c)), closes [#252](https://github.com/cameronsjo/bosun/issues/252)

## [0.40.0](https://github.com/cameronsjo/bosun/compare/v0.39.14...v0.40.0) (2026-08-23)


### Features

* **reconcile:** add authenticated HTTPS Git sync ([#538](https://github.com/cameronsjo/bosun/issues/538)) ([596183c](https://github.com/cameronsjo/bosun/commit/596183ccbf83dabfcc5792e74560705e6a6158ce))


### Bug Fixes

* **reconcile:** require stable identity before restart resolution ([#537](https://github.com/cameronsjo/bosun/issues/537)) ([b7a75dd](https://github.com/cameronsjo/bosun/commit/b7a75dd2473d78d7266c58287fe03c5c1a8fb274))

## [0.39.14](https://github.com/cameronsjo/bosun/compare/v0.39.13...v0.39.14) (2026-08-23)


### Bug Fixes

* **daemon:** preserve coalesced trigger metadata ([#532](https://github.com/cameronsjo/bosun/issues/532)) ([2ab2925](https://github.com/cameronsjo/bosun/commit/2ab2925a6a9b2b1334a6c697e9309537ba9d83fb))
* **reconcile:** handle shallow diff history ([#531](https://github.com/cameronsjo/bosun/issues/531)) ([ffb1081](https://github.com/cameronsjo/bosun/commit/ffb10819b4b082a38b31d84ab01f1b3de08d8dea))
* **reconcile:** preserve slow restart baselines ([#533](https://github.com/cameronsjo/bosun/issues/533)) ([5deaad2](https://github.com/cameronsjo/bosun/commit/5deaad2266fdac22d1215f66211322a6848b2bc8))

## [0.39.13](https://github.com/cameronsjo/bosun/compare/v0.39.12...v0.39.13) (2026-08-23)


### Bug Fixes

* **reconcile:** preserve post-write hook tracking ([#529](https://github.com/cameronsjo/bosun/issues/529)) ([09b84cd](https://github.com/cameronsjo/bosun/commit/09b84cd2d0cf696f7172c07ba45b645dabe00cf5))
* **reconcile:** reject colliding target resources ([#528](https://github.com/cameronsjo/bosun/issues/528)) ([ccc7d6d](https://github.com/cameronsjo/bosun/commit/ccc7d6d7858721fb9686d22d3a90aa9270b22470))
* **reconcile:** skip failed files in orphan pass ([#527](https://github.com/cameronsjo/bosun/issues/527)) ([2da5e66](https://github.com/cameronsjo/bosun/commit/2da5e669d81a676d55b6996a7bd7e5422b221271))

## [0.39.12](https://github.com/cameronsjo/bosun/compare/v0.39.11...v0.39.12) (2026-08-23)


### Bug Fixes

* **alert:** bound provider payload content ([#525](https://github.com/cameronsjo/bosun/issues/525)) ([04d5610](https://github.com/cameronsjo/bosun/commit/04d5610815976b4ebbf67243b5b424b55576acca))
* **config:** support target path overrides in YAML ([#526](https://github.com/cameronsjo/bosun/issues/526)) ([e52b206](https://github.com/cameronsjo/bosun/commit/e52b2066a53c4855d1d80f2489d6bb52084cb178))
* **reconcile:** isolate SSH retries from app stderr ([#523](https://github.com/cameronsjo/bosun/issues/523)) ([c1c07af](https://github.com/cameronsjo/bosun/commit/c1c07af4c9133c7c917936ec046ed7d70127249c))

## [0.39.11](https://github.com/cameronsjo/bosun/compare/v0.39.10...v0.39.11) (2026-08-23)


### Bug Fixes

* **daemon:** distinguish ignored drift from resolution ([#518](https://github.com/cameronsjo/bosun/issues/518)) ([3b38815](https://github.com/cameronsjo/bosun/commit/3b38815789a360832f2c5980317b772d16cd5248))
* **daemon:** sanitize webhook pusher attribution ([#519](https://github.com/cameronsjo/bosun/issues/519)) ([28ec22d](https://github.com/cameronsjo/bosun/commit/28ec22dc8b5dca30c9736d775185408ace916246))
* **reconcile:** reject empty exec hook commands ([#520](https://github.com/cameronsjo/bosun/issues/520)) ([c56905e](https://github.com/cameronsjo/bosun/commit/c56905ebb355eacfba758a1e02a6534147eb3bcb))

## [0.39.10](https://github.com/cameronsjo/bosun/compare/v0.39.9...v0.39.10) (2026-08-23)


### Bug Fixes

* **daemon:** bound HTTP request headers ([#514](https://github.com/cameronsjo/bosun/issues/514)) ([67fe867](https://github.com/cameronsjo/bosun/commit/67fe86767a2d0dc56d0c9191f954353391ba6990))
* **reconcile:** reject invalid deploy snapshots ([#515](https://github.com/cameronsjo/bosun/issues/515)) ([cdcd4f2](https://github.com/cameronsjo/bosun/commit/cdcd4f2d6edcda62a8f40d697c38560e73397a30))
* **sops:** classify sanitized decryption errors ([#516](https://github.com/cameronsjo/bosun/issues/516)) ([0d11ba1](https://github.com/cameronsjo/bosun/commit/0d11ba100ad8ff7f3b67f08833d2819dba44143c))

## [0.39.9](https://github.com/cameronsjo/bosun/compare/v0.39.8...v0.39.9) (2026-08-23)


### Bug Fixes

* **cmd:** honor empty target override ([#510](https://github.com/cameronsjo/bosun/issues/510)) ([07843f7](https://github.com/cameronsjo/bosun/commit/07843f78420cbe212c256b4206b56982eb3581b6))
* **render:** confine template file reads ([#512](https://github.com/cameronsjo/bosun/issues/512)) ([1916ab3](https://github.com/cameronsjo/bosun/commit/1916ab30a713b474b9396e45be210fe9c8a7fcee))
* **sops:** infer secrets file format ([#511](https://github.com/cameronsjo/bosun/issues/511)) ([fdf4092](https://github.com/cameronsjo/bosun/commit/fdf40922357eb4865e778b71c69cdd6f868e614f))

## [0.39.8](https://github.com/cameronsjo/bosun/compare/v0.39.7...v0.39.8) (2026-08-23)


### Bug Fixes

* **daemon:** avoid false drift resolutions ([#509](https://github.com/cameronsjo/bosun/issues/509)) ([3552508](https://github.com/cameronsjo/bosun/commit/3552508ab98ea2adaa73406dd995018de5f40a71))
* **reconcile:** allow zero health gate timeout ([#507](https://github.com/cameronsjo/bosun/issues/507)) ([97e81c5](https://github.com/cameronsjo/bosun/commit/97e81c52bc53b9dfd05fe7c10ba393310a602dd0))
* **reconcile:** isolate hot-reload config slices ([#506](https://github.com/cameronsjo/bosun/issues/506)) ([d077ccc](https://github.com/cameronsjo/bosun/commit/d077ccc123b0a31ed4cb9b1b8df5c9723b998810))

## [0.39.7](https://github.com/cameronsjo/bosun/compare/v0.39.6...v0.39.7) (2026-08-23)


### Bug Fixes

* **drift:** resolve named target state files ([#503](https://github.com/cameronsjo/bosun/issues/503)) ([8af0ee4](https://github.com/cameronsjo/bosun/commit/8af0ee47d2397b8c5781a25e78d0f16c2e854aa2))
* **reconcile:** propagate deploy directory errors ([#504](https://github.com/cameronsjo/bosun/issues/504)) ([f78df14](https://github.com/cameronsjo/bosun/commit/f78df14fd19c0a42a673e0cd1ca846f426b555c0))
* **template:** fail on missing keys ([#502](https://github.com/cameronsjo/bosun/issues/502)) ([042cafb](https://github.com/cameronsjo/bosun/commit/042cafbc663adf2ee10b79af693e033af35afc9e))

## [0.39.6](https://github.com/cameronsjo/bosun/compare/v0.39.5...v0.39.6) (2026-08-22)


### Bug Fixes

* **reconcile:** detach remote cleanup from cancellation ([#500](https://github.com/cameronsjo/bosun/issues/500)) ([905f2bf](https://github.com/cameronsjo/bosun/commit/905f2bf2507cfa3fe296b0e66a8e2c59dff4d3bd))
* **reconcile:** skip symlinks during template copy ([#498](https://github.com/cameronsjo/bosun/issues/498)) ([965b5ca](https://github.com/cameronsjo/bosun/commit/965b5caa36f98bfc00957af7611a93da47453b8a))

## [0.39.5](https://github.com/cameronsjo/bosun/compare/v0.39.4...v0.39.5) (2026-08-22)


### Bug Fixes

* **ci:** require app token for release auto-merge ([#496](https://github.com/cameronsjo/bosun/issues/496)) ([02957c5](https://github.com/cameronsjo/bosun/commit/02957c5b408dd3bfa8252e9b3b1b8278d7f65800))
* **deps:** update Go dependencies ([05d7f49](https://github.com/cameronsjo/bosun/commit/05d7f490bb75a428aeddfc100ad296d72a4fda29))

## [0.39.4](https://github.com/cameronsjo/bosun/compare/v0.39.3...v0.39.4) (2026-08-22)


### Bug Fixes

* **cmd:** prepare reconcile state directory ([#492](https://github.com/cameronsjo/bosun/issues/492)) ([05ff6bf](https://github.com/cameronsjo/bosun/commit/05ff6bf919784e5db5b9ce4cb28ab664fb690aa6))
* **docker:** honor context cancellation while reading streams ([#487](https://github.com/cameronsjo/bosun/issues/487)) ([fd4c76d](https://github.com/cameronsjo/bosun/commit/fd4c76d001894089f14491bce1f58cf7c01f9160))
* **fileutil:** validate permissions before copy ([424c598](https://github.com/cameronsjo/bosun/commit/424c5989a48af5f0686cea4a026db332e36311ca))
* **reconcile:** handle file-directory transitions ([b7e405b](https://github.com/cameronsjo/bosun/commit/b7e405b028f3047bb09496361e8bd976bc0176b9))
* **reconcile:** honor configured health gate interval ([#489](https://github.com/cameronsjo/bosun/issues/489)) ([3ebc11b](https://github.com/cameronsjo/bosun/commit/3ebc11bac1c9b8f6e326922ba7671fec69ed57d2))
* **reconcile:** reap tar when ssh startup fails ([#488](https://github.com/cameronsjo/bosun/issues/488)) ([6b52585](https://github.com/cameronsjo/bosun/commit/6b52585a700d46579e2186423275f332411f0fef))
* **reconcile:** validate required SOPS metadata ([#490](https://github.com/cameronsjo/bosun/issues/490)) ([be472a5](https://github.com/cameronsjo/bosun/commit/be472a52a72a7b66df48f90157d669501917e095))

## [0.39.3](https://github.com/cameronsjo/bosun/compare/v0.39.2...v0.39.3) (2026-07-19)


### Bug Fixes

* template include allowlist, trigger body cap, fail-closed metrics auth ([#294](https://github.com/cameronsjo/bosun/issues/294), [#295](https://github.com/cameronsjo/bosun/issues/295), [#296](https://github.com/cameronsjo/bosun/issues/296)) ([1f3a4cf](https://github.com/cameronsjo/bosun/commit/1f3a4cf00dd9fd2ebc1bdb397cc18565e015aee0))

## [0.39.2](https://github.com/cameronsjo/bosun/compare/v0.39.1...v0.39.2) (2026-07-19)


### Bug Fixes

* content-free backup is not a rollback anchor; retention verifies backups ([#360](https://github.com/cameronsjo/bosun/issues/360), [#353](https://github.com/cameronsjo/bosun/issues/353)) ([1636248](https://github.com/cameronsjo/bosun/commit/1636248664595b8cd4aee9422e867c76b6260e91))

## [0.39.1](https://github.com/cameronsjo/bosun/compare/v0.39.0...v0.39.1) (2026-07-19)


### Bug Fixes

* full managed-tree rollback via RollbackSet chokepoint ([#445](https://github.com/cameronsjo/bosun/issues/445)) ([3473459](https://github.com/cameronsjo/bosun/commit/3473459bbb9462d3ba560e84c09722600f23c35f))

## [0.39.0](https://github.com/cameronsjo/bosun/compare/v0.38.5...v0.39.0) (2026-07-19)


### Features

* three-way health_gate_scope (critical|declared|off) with declared-service rollback ([#339](https://github.com/cameronsjo/bosun/issues/339)) ([fa0272f](https://github.com/cameronsjo/bosun/commit/fa0272fdfc4dba434c25e63ecd4c1a83cfec23d3))


### Bug Fixes

* record deploy success only after health verification, local-gated ([#336](https://github.com/cameronsjo/bosun/issues/336)) ([c54f051](https://github.com/cameronsjo/bosun/commit/c54f051d04623318b99778d6ff27543b9d01fd01))

## [0.38.5](https://github.com/cameronsjo/bosun/compare/v0.38.4...v0.38.5) (2026-07-19)


### Bug Fixes

* verify remote transfers before promotion, wire remote rollback, quote ssh argv ([#334](https://github.com/cameronsjo/bosun/issues/334), [#340](https://github.com/cameronsjo/bosun/issues/340), [#437](https://github.com/cameronsjo/bosun/issues/437)) ([8bdc263](https://github.com/cameronsjo/bosun/commit/8bdc2632d8b2a68bd06b0ffc2b1bc6b214811951))

## [0.38.4](https://github.com/cameronsjo/bosun/compare/v0.38.3...v0.38.4) (2026-07-18)


### Bug Fixes

* reconcile timeout guard, health-gate baseline, daemon panic recovery ([#444](https://github.com/cameronsjo/bosun/issues/444)) ([270fb2a](https://github.com/cameronsjo/bosun/commit/270fb2a84ee292527b78c04a3001f51fe28eca00))

## [0.38.3](https://github.com/cameronsjo/bosun/compare/v0.38.2...v0.38.3) (2026-07-18)


### Bug Fixes

* **reconcile:** write local backups with native Go tar; fail loudly on error ([#439](https://github.com/cameronsjo/bosun/issues/439)) ([70596d7](https://github.com/cameronsjo/bosun/commit/70596d75f0007968a250086a50fdc79db21a8028)), closes [#395](https://github.com/cameronsjo/bosun/issues/395) [#352](https://github.com/cameronsjo/bosun/issues/352)

## [0.38.2](https://github.com/cameronsjo/bosun/compare/v0.38.1...v0.38.2) (2026-07-18)


### Bug Fixes

* daemon resilience batch — readiness gate, self-heal force, breaker recovery ([#427](https://github.com/cameronsjo/bosun/issues/427)) ([e0d3a5c](https://github.com/cameronsjo/bosun/commit/e0d3a5cfeb9992c92e4744947a59bdba684be332))

## [0.38.1](https://github.com/cameronsjo/bosun/compare/v0.38.0...v0.38.1) (2026-07-18)


### Bug Fixes

* honor SSH host-key policy on deploy path ([#426](https://github.com/cameronsjo/bosun/issues/426)) ([38f5b6a](https://github.com/cameronsjo/bosun/commit/38f5b6a09c468de4682599ee2e126dc5e2fd7562))

## [0.38.0](https://github.com/cameronsjo/bosun/compare/v0.37.10...v0.38.0) (2026-07-17)


### ⚠ BREAKING CHANGES

* with no WEBHOOK_SECRET set, webhook trigger requests are rejected with 403. Set WEBHOOK_SECRET or opt out explicitly with BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true.

### Bug Fixes

* resilience slate — fail-closed webhooks ([#345](https://github.com/cameronsjo/bosun/issues/345)), default-target config ([#390](https://github.com/cameronsjo/bosun/issues/390)/[#391](https://github.com/cameronsjo/bosun/issues/391)), retain-old deploy swap ([#343](https://github.com/cameronsjo/bosun/issues/343)) ([2c75ca0](https://github.com/cameronsjo/bosun/commit/2c75ca023b8ff3c16a7d18d0b0012b6581452283))

## [0.37.10](https://github.com/cameronsjo/bosun/compare/v0.37.9...v0.37.10) (2026-07-02)


### Bug Fixes

* **reconcile:** BackupRemote fails on SSH error, verifies archive integrity, resets stream per retry ([#406](https://github.com/cameronsjo/bosun/issues/406)) ([e0eaa94](https://github.com/cameronsjo/bosun/commit/e0eaa9464d5de1c25f2d35ba4da5cf9a6e71f0d7))

## [0.37.9](https://github.com/cameronsjo/bosun/compare/v0.37.8...v0.37.9) (2026-07-02)


### Bug Fixes

* **drift:** validate drift_ignore types and globs at config load; correct type docs ([#409](https://github.com/cameronsjo/bosun/issues/409)) ([5ca4db0](https://github.com/cameronsjo/bosun/commit/5ca4db0b5d8d40d5724eea4b02f5f7491e6cc28c))

## [0.37.8](https://github.com/cameronsjo/bosun/compare/v0.37.7...v0.37.8) (2026-07-02)


### Bug Fixes

* **daemon:** bound startup/poll/self-heal reconciles with ReconcileTimeout ([#410](https://github.com/cameronsjo/bosun/issues/410)) ([706ef2d](https://github.com/cameronsjo/bosun/commit/706ef2d04cc550074fcca3e1b8a8c79a7383a10f))
* **drift:** don't advance alert throttle when delivery fails ([#408](https://github.com/cameronsjo/bosun/issues/408)) ([79a09e2](https://github.com/cameronsjo/bosun/commit/79a09e2e972109d45800230ce22dd13cd7852280))
* **reconcile:** auto-create lock file directory to prevent fresh-install paralysis ([#411](https://github.com/cameronsjo/bosun/issues/411)) ([ee5856a](https://github.com/cameronsjo/bosun/commit/ee5856a8919406c19ca8faff4a01ee52ef7690cd))

## [0.37.7](https://github.com/cameronsjo/bosun/compare/v0.37.6...v0.37.7) (2026-07-02)


### Bug Fixes

* **reconcile:** casefold reserved "default" target checks to prevent state fragmentation ([#407](https://github.com/cameronsjo/bosun/issues/407)) ([30aff14](https://github.com/cameronsjo/bosun/commit/30aff14b3f5250684ac13d7fce026b5ccb0153ef))

## [0.37.6](https://github.com/cameronsjo/bosun/compare/v0.37.5...v0.37.6) (2026-07-02)


### Bug Fixes

* **hooks:** fsync dest dir after rename; settle-delay default + doctor warn for FUSE/Unraid paths ([#402](https://github.com/cameronsjo/bosun/issues/402)) ([5d7902f](https://github.com/cameronsjo/bosun/commit/5d7902f331b8d4ef277a93231ab4900624086092))
* **hooks:** use doublestar so ** glob suffixes are honored ([#401](https://github.com/cameronsjo/bosun/issues/401)) ([e2670e6](https://github.com/cameronsjo/bosun/commit/e2670e61f6db92f3c943e6301fbdaa38144a799a))
* **reconcile:** health-gate rollback redeploys backup files instead of re-running the failed deploy ([#403](https://github.com/cameronsjo/bosun/issues/403)) ([c4c8146](https://github.com/cameronsjo/bosun/commit/c4c8146c5ed9647b2edc670c3d00191cd59218ab))
* **reconcile:** pollContainerHealth honors its timeout when the Docker API errors ([#404](https://github.com/cameronsjo/bosun/issues/404)) ([d69e759](https://github.com/cameronsjo/bosun/commit/d69e759bc80fda1af074c8ea62718fce09800a52))
* **reconcile:** record removeStaleFiles deletions so post-sync hooks fire on mixed commits ([#405](https://github.com/cameronsjo/bosun/issues/405)) ([8cade1c](https://github.com/cameronsjo/bosun/commit/8cade1ca2ba0ab0a142f6b1ecde34e3a079f483f))

## [0.37.5](https://github.com/cameronsjo/bosun/compare/v0.37.4...v0.37.5) (2026-05-28)


### Bug Fixes

* **reconcile:** scope pre-deploy backup to deployed config footprint ([#375](https://github.com/cameronsjo/bosun/issues/375)) ([ca1a30a](https://github.com/cameronsjo/bosun/commit/ca1a30ac20b42a736d5677107f6ddd53f40effe1))

## [0.37.4](https://github.com/cameronsjo/bosun/compare/v0.37.3...v0.37.4) (2026-05-28)


### Bug Fixes

* **reconcile:** harden deploy invariant with content-equality + symlink semantics ([#371](https://github.com/cameronsjo/bosun/issues/371)) ([c616c51](https://github.com/cameronsjo/bosun/commit/c616c511d6a9f6f4b67e414b48f7763ed78d55c2))

## [0.37.3](https://github.com/cameronsjo/bosun/compare/v0.37.2...v0.37.3) (2026-05-28)


### Bug Fixes

* **reconcile:** treat partial compose failure as deploy failure ([#333](https://github.com/cameronsjo/bosun/issues/333)) ([def175a](https://github.com/cameronsjo/bosun/commit/def175a660758779c224539b8dc8aebfd071ab23))

## [0.37.2](https://github.com/cameronsjo/bosun/compare/v0.37.1...v0.37.2) (2026-05-28)


### Bug Fixes

* **reconcile:** no-op content-hash sync no longer trips empty-write invariant ([#330](https://github.com/cameronsjo/bosun/issues/330)) ([a567eaf](https://github.com/cameronsjo/bosun/commit/a567eaf37aaf9b4224d58c9ed6a54260eaaae217))

## [0.37.1](https://github.com/cameronsjo/bosun/compare/v0.37.0...v0.37.1) (2026-05-26)


### Bug Fixes

* **reconcile:** managed-set prune ([#331](https://github.com/cameronsjo/bosun/issues/331)) + rollback archive extraction ([#332](https://github.com/cameronsjo/bosun/issues/332)/[#335](https://github.com/cameronsjo/bosun/issues/335)) ([#366](https://github.com/cameronsjo/bosun/issues/366)) ([c1ed700](https://github.com/cameronsjo/bosun/commit/c1ed70042009a4b87c351ae40b584db9e2f32788))

## [0.37.0](https://github.com/cameronsjo/bosun/compare/v0.36.0...v0.37.0) (2026-05-25)


### Features

* **log:** narrative structured logging across operational core ([#316](https://github.com/cameronsjo/bosun/issues/316)) ([6057d3d](https://github.com/cameronsjo/bosun/commit/6057d3de57aaa2808307035e4466353ea937e2c4))

## [0.36.0](https://github.com/cameronsjo/bosun/compare/v0.35.1...v0.36.0) (2026-05-25)


### Features

* **log:** structured logging for docker and manifest packages (bosun-ixv) ([#323](https://github.com/cameronsjo/bosun/issues/323)) ([e19a958](https://github.com/cameronsjo/bosun/commit/e19a9582d5e5d782362c26d03ce4daff7a6bb79d))

## [0.35.1](https://github.com/cameronsjo/bosun/compare/v0.35.0...v0.35.1) (2026-05-25)


### Bug Fixes

* **cmd:** bound recovery compose-up + cap webhook receiver body size (bosun-9nm) ([#321](https://github.com/cameronsjo/bosun/issues/321)) ([ded388e](https://github.com/cameronsjo/bosun/commit/ded388e91dee98630fa33cb9efc6617f2361e540))
* **reconcile:** stop pre-deploy backup wedging the reconcile (GH[#319](https://github.com/cameronsjo/bosun/issues/319)) ([#324](https://github.com/cameronsjo/bosun/issues/324)) ([e0748aa](https://github.com/cameronsjo/bosun/commit/e0748aa945ee5d0d36669553699ff3a64beb7022))

## [0.35.0](https://github.com/cameronsjo/bosun/compare/v0.34.3...v0.35.0) (2026-05-22)


### Features

* **reconcile:** suggest BOSUN_INFRA_DIR when compose dir is missing (GH[#214](https://github.com/cameronsjo/bosun/issues/214) Layer 3) ([#307](https://github.com/cameronsjo/bosun/issues/307)) ([4e49b6a](https://github.com/cameronsjo/bosun/commit/4e49b6ae014989927be9f972baa6eff284fb5b91))

## [0.34.3](https://github.com/cameronsjo/bosun/compare/v0.34.2...v0.34.3) (2026-05-20)


### Bug Fixes

* shell injection in remote SSH commands (Cluster A) ([#298](https://github.com/cameronsjo/bosun/issues/298)) ([f5a99cf](https://github.com/cameronsjo/bosun/commit/f5a99cf6453dae75cb1e0655157b137e520cf458))

## [0.34.2](https://github.com/cameronsjo/bosun/compare/v0.34.1...v0.34.2) (2026-05-20)


### Bug Fixes

* doctor and discovery diagnostic accuracy (Cluster J) ([#299](https://github.com/cameronsjo/bosun/issues/299)) ([a9ccc58](https://github.com/cameronsjo/bosun/commit/a9ccc582165ef8c712f367567aafa7aad75aaf8b))

## [0.34.1](https://github.com/cameronsjo/bosun/compare/v0.34.0...v0.34.1) (2026-05-20)


### Bug Fixes

* centralize BOSUN_X env-var precedence (Cluster G) ([#300](https://github.com/cameronsjo/bosun/issues/300)) ([9e6275a](https://github.com/cameronsjo/bosun/commit/9e6275a2e8b287edb8d0c27c2c24d91907beeaeb))

## [0.34.0](https://github.com/cameronsjo/bosun/compare/v0.33.3...v0.34.0) (2026-05-17)


### Features

* **docs:** revise pipeline and architecture diagrams ([0e0bbec](https://github.com/cameronsjo/bosun/commit/0e0bbec1cc86a4b1c11fc3e0b2bfe9f00af94eac))
* **reconcile:** deploy-sync invariants and per-file write observability ([#214](https://github.com/cameronsjo/bosun/issues/214)) ([fd41601](https://github.com/cameronsjo/bosun/commit/fd41601a77eda18f26fbcb1140132198ae32ae3b))

## [0.33.3](https://github.com/cameronsjo/bosun/compare/v0.33.2...v0.33.3) (2026-04-05)


### Bug Fixes

* deploy bug fixes — FUSE guard, HTTP force-trigger, name conflict warnings ([#222](https://github.com/cameronsjo/bosun/issues/222)) ([912260f](https://github.com/cameronsjo/bosun/commit/912260f7ff1c6bfe32525cf7b15cda525867a306))

## [0.33.2](https://github.com/cameronsjo/bosun/compare/v0.33.1...v0.33.2) (2026-04-05)


### Bug Fixes

* **reconcile:** local appdata path takes priority over secrets-based host ([2ffbba1](https://github.com/cameronsjo/bosun/commit/2ffbba16146d187bfc0934cff911ce5f4095477d))

## [0.33.1](https://github.com/cameronsjo/bosun/compare/v0.33.0...v0.33.1) (2026-03-26)


### Bug Fixes

* propagate span context and fix orphaned doc comment ([d653e25](https://github.com/cameronsjo/bosun/commit/d653e25950ebed33f6ea58941bc36f07226d3e4f))
* return matched count and error when Docker client unavailable ([b72df0b](https://github.com/cameronsjo/bosun/commit/b72df0bae2b792992672b8ea02fd5278bd67c838))

## [0.33.0](https://github.com/cameronsjo/bosun/compare/v0.32.6...v0.33.0) (2026-03-25)


### Features

* **reconcile:** hot-reload per-target operational overrides ([b1c749a](https://github.com/cameronsjo/bosun/commit/b1c749abadf798164b3928b28410c981b3c4ae2b))


### Bug Fixes

* address CodeRabbit review findings on ConfigField[T] PR ([931bc83](https://github.com/cameronsjo/bosun/commit/931bc8360d9e42e998c14af881ff988a11ad52ce))
* check os.Unsetenv error to satisfy errcheck lint ([1a76409](https://github.com/cameronsjo/bosun/commit/1a764095e681fd83fbe8b130748150fea151a9ab))
* **config:** return error when bosun.yaml exists but fails to parse ([200fd28](https://github.com/cameronsjo/bosun/commit/200fd2818a6274977085a5510ba97f35fe3e0453))
* ensure deterministic env state in HookSettleDelay test cases ([9dd5f6d](https://github.com/cameronsjo/bosun/commit/9dd5f6d42867997ae3357c154ef9e8dd297009be))
* **reconcile:** deploy mode secrets gap + per-target hot-reload ([fd19ed3](https://github.com/cameronsjo/bosun/commit/fd19ed324f35d9bd29b66bb1162c482f76319d8a))
* **reconcile:** propagate deploy mode to post-sync hooks and strengthen test assertions ([8a54cc5](https://github.com/cameronsjo/bosun/commit/8a54cc584ccaf942927bc719bf9dde9d68186c15))
* **reconcile:** resolve deploy mode using secrets-based target host ([1d2fcda](https://github.com/cameronsjo/bosun/commit/1d2fcdac3a10770b7b21158e355881d6dbed2b99))
* **reconcile:** surface clear error when appdata mount is inaccessible ([2777b96](https://github.com/cameronsjo/bosun/commit/2777b96fb57f92c765e0226f0966cdfbf3b3f2b5))
* **reconcile:** validate source directory exists in copyNonTemplateFiles ([ccffa64](https://github.com/cameronsjo/bosun/commit/ccffa64b93140956c8226d9a57677dd0c887ac54))

## [0.32.6](https://github.com/cameronsjo/bosun/compare/v0.32.5...v0.32.6) (2026-03-24)


### Bug Fixes

* **reconcile:** address CodeRabbit review feedback for removeStaleFiles ([94c3bb3](https://github.com/cameronsjo/bosun/commit/94c3bb32d4d671bdfb18e8b79aefa2ca382c2451))
* **reconcile:** log warnings when removeStaleFiles cannot delete files ([f39e4af](https://github.com/cameronsjo/bosun/commit/f39e4af836f84e3dc6d1dc7fac270321deccf117))

## [0.32.5](https://github.com/cameronsjo/bosun/compare/v0.32.4...v0.32.5) (2026-03-24)


### Bug Fixes

* **reconcile:** fire all post-sync hooks unconditionally for remote deploys ([1fe3b29](https://github.com/cameronsjo/bosun/commit/1fe3b29d6dd94015338aa753dc36cf51696094bf))
* **reconcile:** fire all post-sync hooks unconditionally for remote deploys ([54d22eb](https://github.com/cameronsjo/bosun/commit/54d22ebc61e76676be7f42ee1d7dde9bc6d2f4eb)), closes [#197](https://github.com/cameronsjo/bosun/issues/197)

## [0.32.4](https://github.com/cameronsjo/bosun/compare/v0.32.3...v0.32.4) (2026-03-23)


### Bug Fixes

* **reconcile:** render templates to correct staging path when InfraSubDir is set ([43f29b5](https://github.com/cameronsjo/bosun/commit/43f29b511cf824276c244a60271729b9c7987493))
* **reconcile:** render templates to correct staging path when InfraSubDir is set ([95b131e](https://github.com/cameronsjo/bosun/commit/95b131e411994efa1dc7ec9bee58f87c4277d9e0)), closes [#190](https://github.com/cameronsjo/bosun/issues/190)

## [0.32.3](https://github.com/cameronsjo/bosun/compare/v0.32.2...v0.32.3) (2026-03-23)


### Bug Fixes

* **deps:** patch critical grpc and high jsonparser vulnerabilities ([29a6064](https://github.com/cameronsjo/bosun/commit/29a606453e6aa53a299d2bcb6c32d43487748fd5))

## [0.32.2](https://github.com/cameronsjo/bosun/compare/v0.32.1...v0.32.2) (2026-03-23)


### Bug Fixes

* add bounds check to PrefixLatest ([ba66397](https://github.com/cameronsjo/bosun/commit/ba663970a2067f3bfe6926eea1c53d26bbefb82a))
* **reconcile:** prefix written file paths for post-sync hook matching ([6d6f047](https://github.com/cameronsjo/bosun/commit/6d6f0471dedeb5b44a4e8c623ebc3ec48c3c8240))
* **reconcile:** prefix written file paths for post-sync hook matching ([820045d](https://github.com/cameronsjo/bosun/commit/820045d94352aee7b7a39a947958d4618637c70c)), closes [#186](https://github.com/cameronsjo/bosun/issues/186)
* use filepath.Dir for file target prefix in PrefixLatest ([9ef0ea4](https://github.com/cameronsjo/bosun/commit/9ef0ea4f524e87b993e5b43388e2d160d7cf0473))
* use relative paths in DeployLocalFile WrittenFiles ([cbb398e](https://github.com/cameronsjo/bosun/commit/cbb398e75ed8727836814f70819d282d5a61fea1))

## [0.32.1](https://github.com/cameronsjo/bosun/compare/v0.32.0...v0.32.1) (2026-03-22)


### Bug Fixes

* address CodeRabbit findings on deploy-state PR ([92c67a2](https://github.com/cameronsjo/bosun/commit/92c67a2aafb3e2bb515a78729bbe1252c9540f54))
* address CodeRabbit R3 findings ([7bccfdd](https://github.com/cameronsjo/bosun/commit/7bccfddda146219abdc8361f121145626816b8f9))
* **reconcile:** remove ~/.ssh/known_hosts from SSH host key resolution ([9c2f513](https://github.com/cameronsjo/bosun/commit/9c2f513340e72dcab43977519c476b621026b956)), closes [#173](https://github.com/cameronsjo/bosun/issues/173)
* **reconcile:** use last deployed commit for deploy-path relevance diff ([3240691](https://github.com/cameronsjo/bosun/commit/3240691d82767025190f63ff5b6d0d7c11fd9a2b))
* **reconcile:** use last deployed commit for deploy-path relevance diff ([d1a7ad6](https://github.com/cameronsjo/bosun/commit/d1a7ad6fee447fd29b712aa27cca859aa3bc9467)), closes [#170](https://github.com/cameronsjo/bosun/issues/170)

## [0.32.0](https://github.com/cameronsjo/bosun/compare/v0.31.0...v0.32.0) (2026-03-21)


### Features

* **manifest:** add port registry with conflict detection ([ce97f07](https://github.com/cameronsjo/bosun/commit/ce97f073738966ed31cb94fe42e48fff5b9511d6))


### Bug Fixes

* **ports:** cap free-port range at 65535, handle float64 port entries ([58abb85](https://github.com/cameronsjo/bosun/commit/58abb859a7a611b51640c8d57335534d00d0831b))
* **ports:** normalize 0.0.0.0 and :: as wildcard bind addresses ([e82bbe6](https://github.com/cameronsjo/bosun/commit/e82bbe69f1542f11384290aad6b47d7ee105544d))

## [0.31.0](https://github.com/cameronsjo/bosun/compare/v0.30.4...v0.31.0) (2026-03-21)


### Features

* **daemon:** hot-reload bosun.yaml config on changes ([4db0bbc](https://github.com/cameronsjo/bosun/commit/4db0bbc40cd01e188ca8ec5711af4a8d9660c90f))


### Bug Fixes

* **daemon:** address CodeRabbit findings on force-flag PR ([1fcd7cd](https://github.com/cameronsjo/bosun/commit/1fcd7cdcc46907ef92b5593da8565d40571e67c8))
* **daemon:** address CodeRabbit findings on hot-reload PR ([ba7d151](https://github.com/cameronsjo/bosun/commit/ba7d1518f1d41d0b0cf0ba2cb0e996246f0f0b5e))
* **daemon:** propagate --force flag through socket trigger handler ([a3aa348](https://github.com/cameronsjo/bosun/commit/a3aa3488ec6ae62c57ef95e9a9135502d61e058a))
* **daemon:** use DefaultConfig for cooldown fallback, clarify reload design ([8267a72](https://github.com/cameronsjo/bosun/commit/8267a72becc80f904e8d8d0aefc8d5f078b1e911))

## [0.30.4](https://github.com/cameronsjo/bosun/compare/v0.30.3...v0.30.4) (2026-03-21)


### Bug Fixes

* **reconcile:** detect missing services in already-deployed commits ([9319717](https://github.com/cameronsjo/bosun/commit/93197178c9f11d9b5e59eeec2581c379234637d3))
* **reconcile:** skip symlinks during template file copy ([8a0bb18](https://github.com/cameronsjo/bosun/commit/8a0bb18f5479be2da6b4f091e164c7a704b7551b))

## [0.30.3](https://github.com/cameronsjo/bosun/compare/v0.30.2...v0.30.3) (2026-03-21)


### Bug Fixes

* **preflight:** address CodeRabbit review findings ([4f4077b](https://github.com/cameronsjo/bosun/commit/4f4077b80d6a5792e3183bd936ea2de2511406e6))
* **preflight:** validate SSH deploy key permissions ([1808a33](https://github.com/cameronsjo/bosun/commit/1808a33fe08e85b355534f64a99e0ee561551e4d))
* **preflight:** validate SSH deploy key permissions ([b04a8ac](https://github.com/cameronsjo/bosun/commit/b04a8ac799c60dfd242cf1795bfacca64d86c514)), closes [#172](https://github.com/cameronsjo/bosun/issues/172)

## [0.30.2](https://github.com/cameronsjo/bosun/compare/v0.30.1...v0.30.2) (2026-03-21)


### Bug Fixes

* **reconcile:** clone Command slice in clonePostSyncHooks ([3b086a8](https://github.com/cameronsjo/bosun/commit/3b086a87e1b5a2af154999585ab858ef3109f025))
* **reconcile:** deep-copy target slices and reject duplicate names ([6d27611](https://github.com/cameronsjo/bosun/commit/6d27611de5846b88e501ed118ecbb99a004af60a))
* **reconcile:** reject reserved 'default' target name, fix SecretsScope inheritance ([91c86e2](https://github.com/cameronsjo/bosun/commit/91c86e20b92bd0eb795a4eb18a0ec7eed4c9085b))

## [0.30.1](https://github.com/cameronsjo/bosun/compare/v0.30.0...v0.30.1) (2026-03-21)


### Bug Fixes

* **cmd:** address CodeRabbit findings on diagnostics split ([918d83d](https://github.com/cameronsjo/bosun/commit/918d83d015de3d0926bef7cbc262fe91079de6bf))
* **cmd:** address round 2 CodeRabbit nitpicks on diagnostics split ([cf37256](https://github.com/cameronsjo/bosun/commit/cf372565bb36033365cfbcc69f0d0a30ca315792))
* **cmd:** use tagged switch to satisfy staticcheck QF1003 ([1c94c7b](https://github.com/cameronsjo/bosun/commit/1c94c7b17c60370685cb404c08d524e6787a7bb3))

## [0.30.0](https://github.com/cameronsjo/bosun/compare/v0.29.2...v0.30.0) (2026-03-20)


### Features

* **alert:** add per-target context to alert titles ([823eba5](https://github.com/cameronsjo/bosun/commit/823eba56efd5a8a153a9400a2df8f6245644af09))
* **cmd:** add --target flag to reconcile and drift commands ([e04e914](https://github.com/cameronsjo/bosun/commit/e04e914c488f6f9b8e06389ccda90f81e6a2f0c3))
* multi-target reconciliation (one yacht, many ports) ([3fdb569](https://github.com/cameronsjo/bosun/commit/3fdb5695aabddd97dcf12f233fb8a95e91781490))
* **reconcile:** add Targets to ReloadedConfig for hot-reload ([93d9f7d](https://github.com/cameronsjo/bosun/commit/93d9f7dc714852802a9dcb59827ce862e526eb9e))


### Bug Fixes

* address CodeRabbit findings on test coverage PR ([9402acd](https://github.com/cameronsjo/bosun/commit/9402acddf7518dc12774da38e598dfc0429a31d8))
* address round 2 CodeRabbit findings ([00e0986](https://github.com/cameronsjo/bosun/commit/00e0986ea32d337a41fa430231a40a50102ce522))
* **reconcile:** address CodeRabbit review findings on multi-target ([c4e4b74](https://github.com/cameronsjo/bosun/commit/c4e4b749da192a8b5d5fcc071ee7e3c9dfbc429d))
* **reconcile:** address round 2 CodeRabbit findings ([f0393e3](https://github.com/cameronsjo/bosun/commit/f0393e36e6d4f02e69d6a02eb8eda5635adb2464))
* **reconcile:** move orphaned sendSuccessAlert docstring to correct function ([bd6780f](https://github.com/cameronsjo/bosun/commit/bd6780f438d86fc68c0965e6aebf24e6750ed9ea))
* **reconcile:** validate target names and document daemon limitations ([7d437d7](https://github.com/cameronsjo/bosun/commit/7d437d7159864c4f97a965a1ff352bf91463f7c6))

## [0.29.2](https://github.com/cameronsjo/bosun/compare/v0.29.1...v0.29.2) (2026-03-18)


### Bug Fixes

* **manifest:** address CodeRabbit review findings on dynamic targets ([f994e47](https://github.com/cameronsjo/bosun/commit/f994e47cc567dd489e5ec9296389c4f32d401499))

## [0.29.1](https://github.com/cameronsjo/bosun/compare/v0.29.0...v0.29.1) (2026-03-17)


### Bug Fixes

* **openspec:** address round 3 nitpicks ([309307f](https://github.com/cameronsjo/bosun/commit/309307fa2773f9a44aa87c94a7553bfd45a81a50))
* **openspec:** define unregistered target behavior ([8b60aaf](https://github.com/cameronsjo/bosun/commit/8b60aaf6bd0c85d3ef9cc754d52db780ddb3cd17))

## [0.29.0](https://github.com/cameronsjo/bosun/compare/v0.28.0...v0.29.0) (2026-03-16)


### Features

* **reconcile:** per-file compose up with isolated rollback ([864eddc](https://github.com/cameronsjo/bosun/commit/864eddcea41f27fe3e3fbbd1a9e111040b9f22a2))


### Bug Fixes

* **reconcile:** add timeout to Phase 2 orphan reconciliation pass ([2eb737c](https://github.com/cameronsjo/bosun/commit/2eb737cab91efd35f22be69a54be0cb53bf436d0))
* **reconcile:** disable --remove-orphans in Phase 1 and guard empty input ([183500e](https://github.com/cameronsjo/bosun/commit/183500eb5b6ad0d3bb573b536f2371b6686ffc5c))

## [0.28.0](https://github.com/cameronsjo/bosun/compare/v0.27.1...v0.28.0) (2026-03-16)


### Features

* add Claude Code plugin with onboarding skill ([db8bd2a](https://github.com/cameronsjo/bosun/commit/db8bd2a59a0d9ffc6a1eaf5ed4fa850a1717671c))
* add critical container health gate with rollback ([f3820fc](https://github.com/cameronsjo/bosun/commit/f3820fcd1753789a13084ed113ddb25b70116c65))
* add critical container health gate with rollback ([#129](https://github.com/cameronsjo/bosun/issues/129)) ([f3820fc](https://github.com/cameronsjo/bosun/commit/f3820fcd1753789a13084ed113ddb25b70116c65))
* add deploy resilience — alert throttling, --wait removal, post-sync hooks ([7b278d7](https://github.com/cameronsjo/bosun/commit/7b278d7c840fc4be6b4d95d5eaf92d1b20959bee))
* add native daemon mode with Unix socket API and webhook support ([34d05cf](https://github.com/cameronsjo/bosun/commit/34d05cf74f39ebc26d897c1265a3c4a17d27da4b))
* **alert:** add deploy success/failure notifications with services and duration ([819435b](https://github.com/cameronsjo/bosun/commit/819435b641ccb316f13fc94a19df6f93e5117081))
* **alert:** add deploy success/failure notifications with services and duration ([038b412](https://github.com/cameronsjo/bosun/commit/038b412d2600342d76dbfaa3b3fae27637a6036a))
* **alert:** add deploy success/failure notifications with services and duration ([#133](https://github.com/cameronsjo/bosun/issues/133)) ([819435b](https://github.com/cameronsjo/bosun/commit/819435b641ccb316f13fc94a19df6f93e5117081))
* **alert:** add drift alert debounce to suppress transient flaps ([#94](https://github.com/cameronsjo/bosun/issues/94)) ([8dc2d9b](https://github.com/cameronsjo/bosun/commit/8dc2d9b85850eb279d7381bcd339a80b5e42a0f4))
* **alert:** add generic webhook alert provider ([2cd2ca7](https://github.com/cameronsjo/bosun/commit/2cd2ca7e88ea889b8402d48ceda99cbb46cf9028))
* **alert:** add generic webhook alert provider ([1089be4](https://github.com/cameronsjo/bosun/commit/1089be4b7fcadbf0148db764022fb0093ad793ac))
* **alert:** add Slack webhook alert provider ([f2f2e73](https://github.com/cameronsjo/bosun/commit/f2f2e7303905e3c030bab41fb0d8a268b1df0e17))
* **alert:** add Slack webhook alert provider ([6143ca1](https://github.com/cameronsjo/bosun/commit/6143ca18ed72473490dc7ae1231d8563af266151))
* **api:** add Homepage dashboard widget endpoint ([ce60071](https://github.com/cameronsjo/bosun/commit/ce60071fbc641e686a81ddb04c573a0cfe562048)), closes [#36](https://github.com/cameronsjo/bosun/issues/36)
* apply essentials scaffold ([e877efb](https://github.com/cameronsjo/bosun/commit/e877efb809c9925f50b02f81a36d1ccd149e1127))
* **ci:** add Docker image build+push to release workflow ([a57475d](https://github.com/cameronsjo/bosun/commit/a57475d6f3d3141a3e6e6ab2a5cba0d33e17358e))
* **ci:** add WebUI to Dagger pipeline ([5f97f14](https://github.com/cameronsjo/bosun/commit/5f97f14b90224a59bca5c5e5026a09f10576578f))
* **ci:** convert GitHub Actions to Dagger pipelines ([7364990](https://github.com/cameronsjo/bosun/commit/736499052370ff6d688b97b321c679d48db86e96))
* **ci:** convert GitHub Actions to Dagger pipelines ([3456db2](https://github.com/cameronsjo/bosun/commit/3456db2d84bc9a94b54f5ec3bc01452a371b2a80))
* **cli:** add dynamic shell completions ([615f837](https://github.com/cameronsjo/bosun/commit/615f837da6710301e4c64d9092c917bba540a6a1))
* **cli:** add render command for local template preview ([b454313](https://github.com/cameronsjo/bosun/commit/b45431353561d943f36860918b5bd05b4badfcac))
* **config:** add configurable orphan container cleanup ([#93](https://github.com/cameronsjo/bosun/issues/93)) ([aa90339](https://github.com/cameronsjo/bosun/commit/aa903397397a08c75a004f19b564f14646e3b478))
* **config:** add critical containers and health gate timeout config surface ([339fcc0](https://github.com/cameronsjo/bosun/commit/339fcc02a838f76d1ed09e07cf5eaf849c3aae77))
* **config:** add post_sync_hooks to bosun.yaml config surface ([0396d60](https://github.com/cameronsjo/bosun/commit/0396d607abe2889c619a08b2932621c31611b9cd)), closes [#38](https://github.com/cameronsjo/bosun/issues/38)
* **daemon:** add BOSUN_INFRA_DIR env var support ([84f74a3](https://github.com/cameronsjo/bosun/commit/84f74a3dc23e9d207f1310b695ce5d1f666e92dd))
* **daemon:** add drift alert deduplication with per-item cooldown ([6efc0e6](https://github.com/cameronsjo/bosun/commit/6efc0e6b155adbcff4f38598aeb988bf3cf1ead7))
* **daemon:** add native daemon mode with HTTP server ([dea3ade](https://github.com/cameronsjo/bosun/commit/dea3ade4dd8395e78304aef6182a9286270d59db))
* **daemon:** add structured logging to API handlers and TCP auth ([8ca9409](https://github.com/cameronsjo/bosun/commit/8ca940926112c1628664b05d9f1f088633251c44))
* **daemon:** add subsystem breakdown to health endpoint ([f4e3aa5](https://github.com/cameronsjo/bosun/commit/f4e3aa597639e47f03a0d82d36bdf007523a8bbd))
* **daemon:** add subsystem breakdown to health endpoint ([25ca040](https://github.com/cameronsjo/bosun/commit/25ca040b14d11f20147ed2f8a8fe7c695b2b4a9f))
* **daemon:** add Unix socket API with multi-provider webhook support ([a43308e](https://github.com/cameronsjo/bosun/commit/a43308e6177d36a0767278d55e8557e92ed95ca6))
* **daemon:** replace hand-rolled metrics with Prometheus ([2cfcf14](https://github.com/cameronsjo/bosun/commit/2cfcf14efb048b826b7474f2009635c9f21f45f8))
* **daemon:** replace hand-rolled metrics with Prometheus ([03f1016](https://github.com/cameronsjo/bosun/commit/03f1016c7b2b537e7e282880c227374a79762683))
* **daemon:** Unix socket API with multi-provider webhooks ([6298f80](https://github.com/cameronsjo/bosun/commit/6298f80dfe3bbd2d31f1b221936cf9d6ece6dd3f))
* **docker:** add graceful container shutdown with configurable timeout ([4186310](https://github.com/cameronsjo/bosun/commit/41863106bfbc39b134212a1ba9de74cdc5ee81c0))
* **docker:** add graceful container shutdown with configurable timeout ([6eb33bd](https://github.com/cameronsjo/bosun/commit/6eb33bda9c48f1bc54abc37240ad1daccf2ae30a)), closes [#375](https://github.com/cameronsjo/bosun/issues/375)
* **docker:** add graceful container shutdown with configurable timeout ([#135](https://github.com/cameronsjo/bosun/issues/135)) ([4186310](https://github.com/cameronsjo/bosun/commit/41863106bfbc39b134212a1ba9de74cdc5ee81c0))
* **docs:** add mermaid-to-ascii rendering script ([cdd80ae](https://github.com/cameronsjo/bosun/commit/cdd80ae7a88df7a3b2b6d065faba41143119e14d))
* **docs:** render README diagrams as ASCII art ([4f2ea70](https://github.com/cameronsjo/bosun/commit/4f2ea70ae1e69472e5f4b14880b67192b484df53))
* **drift:** enrich unhealthy drift items with health check diagnostics ([58137ca](https://github.com/cameronsjo/bosun/commit/58137ca32c0180a52f8f2fed94b9b9419ab88ef1)), closes [#61](https://github.com/cameronsjo/bosun/issues/61)
* **git:** add SSH key file support for git operations ([fb26cde](https://github.com/cameronsjo/bosun/commit/fb26cde35b9e044f46d7061a2fb36e4a9140fe86))
* **hooks:** add exec action to post-sync hooks ([b966ae4](https://github.com/cameronsjo/bosun/commit/b966ae4f63909e440ff6cebeeba03e675ad87cab))
* **hooks:** add exec action to post-sync hooks ([4907039](https://github.com/cameronsjo/bosun/commit/49070392681b9c63d535c4f8a9bca0ed910dd027))
* **init:** add domain prompt and Traefik config generation ([b1fe645](https://github.com/cameronsjo/bosun/commit/b1fe6452bd5baeef19dc2a461ace10974ad11583))
* **log:** add ComponentCtx for context-aware logger construction ([cacb01e](https://github.com/cameronsjo/bosun/commit/cacb01e2fa9489f41f6fde23d9c1782a5d7492de))
* **log:** add structured logging with zerolog ([871e65a](https://github.com/cameronsjo/bosun/commit/871e65a7162833ff67f3771092043dfa3f429476))
* **log:** add structured logging with zerolog ([cfc1eee](https://github.com/cameronsjo/bosun/commit/cfc1eeee289a02c657dae12cb54d1d79dff2b3a4))
* **log:** adopt context-aware logger across codebase ([664f795](https://github.com/cameronsjo/bosun/commit/664f79538de70019415dfbd33b70c2d70ec61eb1))
* **log:** adopt context-aware logger across codebase ([#63](https://github.com/cameronsjo/bosun/issues/63)) ([664f795](https://github.com/cameronsjo/bosun/commit/664f79538de70019415dfbd33b70c2d70ec61eb1))
* **log:** enrich context at pipeline entry points and migrate sub-operations ([832fa8c](https://github.com/cameronsjo/bosun/commit/832fa8c1c4adee155d04a5fc5dcaebea77860aa7))
* **logging:** add debug logging to SOPS, template rendering, and snapshot operations ([b11d65b](https://github.com/cameronsjo/bosun/commit/b11d65b26cac59ae57108acbe48b3726c7968604))
* **logging:** add P3 logging to SSH auth, ConfigFromEnv, alert providers, and CLI commands ([a2106fb](https://github.com/cameronsjo/bosun/commit/a2106fbc8fb25c27957ee18ead0bd123cda934b4))
* **logging:** add structured logging across reconcile, daemon, and drift ([67b1a44](https://github.com/cameronsjo/bosun/commit/67b1a4445caae3ebf8cdf94caa1b673f32e9c851))
* **logging:** comprehensive structured logging and fixes from code review ([c46a5af](https://github.com/cameronsjo/bosun/commit/c46a5afefad9cf4d7b88e462b30db036784b5343))
* **log:** migrate docker client and add retry logging ([98d2200](https://github.com/cameronsjo/bosun/commit/98d220059489932743d2c949f324835fb3c7a732))
* **manifest:** add compose overrides and network merging ([9ca81d3](https://github.com/cameronsjo/bosun/commit/9ca81d3255f5ae9e9580906591542a067abcea8f))
* **manifest:** add Helm-aligned chart format ([#15](https://github.com/cameronsjo/bosun/issues/15)) ([aaa8e92](https://github.com/cameronsjo/bosun/commit/aaa8e92a8411707b7bdc048a56879340be96cc2c))
* **mascot:** add bosun mascot with transparent PNG pipeline ([7e489f6](https://github.com/cameronsjo/bosun/commit/7e489f66086415fadaadc1b940d273e10763cca2))
* **openspec:** formalize spec review workflow with Stage 1.5 gate ([9f1b827](https://github.com/cameronsjo/bosun/commit/9f1b827dd72714c8258a5d851e6542b467ce47ff))
* **openspec:** formalize spec review workflow with Stage 1.5 gate ([d7009a1](https://github.com/cameronsjo/bosun/commit/d7009a117da72ef18bf7a9b756f455521ecc35de))
* **provision:** add project_name to compose output for container namespacing ([5772529](https://github.com/cameronsjo/bosun/commit/57725291ad1f230079ea70fafdba8d65da50e20b))
* **reconcile:** add alert throttling with exponential backoff ([f45498b](https://github.com/cameronsjo/bosun/commit/f45498be23bc02aa9baf55b8177971eb9782bcf5))
* **reconcile:** add content-hash file sync to skip unchanged writes ([6577383](https://github.com/cameronsjo/bosun/commit/6577383fa2e0dc91d1fe630496218f5d75da001d))
* **reconcile:** add deploy_paths allowlist for path-aware deploy skipping ([03191a8](https://github.com/cameronsjo/bosun/commit/03191a8319e6fc7617baf7196b75c804669fef76)), closes [#56](https://github.com/cameronsjo/bosun/issues/56)
* **reconcile:** add openspec proposal for declared-vs-actual state feedback loop ([3173f28](https://github.com/cameronsjo/bosun/commit/3173f283eff3e910bf80593eaec32a783b30732e))
* **reconcile:** add post-deploy health verification with polling ([16a434f](https://github.com/cameronsjo/bosun/commit/16a434f2d6f50918424c3a4cd4d111df1888c5d3))
* **reconcile:** add post-sync container restart hooks ([b71f494](https://github.com/cameronsjo/bosun/commit/b71f494f32e8847768fb60b855417adcaa0c4fe6))
* **reconcile:** add post-sync hook delay controls ([002fb99](https://github.com/cameronsjo/bosun/commit/002fb99a30b39f6cf1068c95b890798926a9335d))
* **reconcile:** add post-sync hook delay controls ([7c51f75](https://github.com/cameronsjo/bosun/commit/7c51f759b614e37599399e20d77b540a083e9aac))
* **reconcile:** add restart circuit breaker ([b959fc3](https://github.com/cameronsjo/bosun/commit/b959fc30cc2a748be34c8df3904aac4ddc0db7ae))
* **reconcile:** add restart circuit breaker to detect crash-looping containers ([36d3ac4](https://github.com/cameronsjo/bosun/commit/36d3ac4ae302a04f30bd5b669ac83258f7874efd))
* **reconcile:** add state-based deploy tracking and circuit breaker ([da1f923](https://github.com/cameronsjo/bosun/commit/da1f9236509b90baa03e526ce7eb9c4ae15e339c))
* **reconcile:** add state-based deploy tracking and circuit breaker ([bf34cf2](https://github.com/cameronsjo/bosun/commit/bf34cf2519cee5d67b304eb852b1d80ac8f6d77f))
* **reconcile:** add state-based deploy tracking with circuit breaker ([#27](https://github.com/cameronsjo/bosun/issues/27)) ([da1f923](https://github.com/cameronsjo/bosun/commit/da1f9236509b90baa03e526ce7eb9c4ae15e339c))
* **reconcile:** data-driven deploy paths ([5193fdd](https://github.com/cameronsjo/bosun/commit/5193fddda7ae3fdd7a1cd2823aedffba0cd73e06))
* **reconcile:** declared-vs-actual state feedback loop with drift detection ([d11b07d](https://github.com/cameronsjo/bosun/commit/d11b07d79ec3f4160858a218f5491539d39f2e41))
* **reconcile:** implement critical container health gate ([433d82c](https://github.com/cameronsjo/bosun/commit/433d82ceaa69f7a7b0eaaa0124af51da6f6003b6))
* **reconcile:** implement declared-vs-actual state feedback loop ([2346c87](https://github.com/cameronsjo/bosun/commit/2346c8767578cadd5212e10f293cc91900224806))
* **reconcile:** replace hardcoded deploy paths with auto-discovery ([6a17f59](https://github.com/cameronsjo/bosun/commit/6a17f5986c3ca861e78ccab81f2d46e292b3c658))
* **reconcile:** wire health gate into pipeline with rollback ([16d4927](https://github.com/cameronsjo/bosun/commit/16d4927cb4a1b34795de5a9d9b02feafafbec303))
* **retry:** add retry utility with exponential backoff and jitter ([cf45845](https://github.com/cameronsjo/bosun/commit/cf45845722cfd9b019a7eac47893cd45fb228d12))
* **retry:** add retry utility with exponential backoff and jitter ([f5c033a](https://github.com/cameronsjo/bosun/commit/f5c033a35ae6490add73d0f9fa14d2b2f11583df))
* **sentry:** add opt-in error tracking and performance monitoring ([dd4d6f5](https://github.com/cameronsjo/bosun/commit/dd4d6f5901eaf15a2e62ff94a7ba7dc8aa386482))
* **sentry:** add opt-in error tracking and performance monitoring ([a04087f](https://github.com/cameronsjo/bosun/commit/a04087f4358d917157f01ee854e73c6463b271ca))
* **traefik:** add batteries-included security defaults (Phase 1) ([999d25d](https://github.com/cameronsjo/bosun/commit/999d25d45cf5e5803b415c05451061ffb878c669))
* **traefik:** add upgrade command and doctor diagnostics (Phase 2) ([e9b1e01](https://github.com/cameronsjo/bosun/commit/e9b1e0140aecc44e8cfb755505f87a804ba60cc7))
* **traefik:** batteries-included security defaults ([bec448c](https://github.com/cameronsjo/bosun/commit/bec448c26f02c06e363b2341ccf908d8b727b10f))
* **tunnel:** add structured logging to cloudflare and tailscale providers ([bcc1724](https://github.com/cameronsjo/bosun/commit/bcc1724a937efd5051453d9cd1b7a824da102a9c))
* **webui:** add React dashboard with maritime theme ([4a1348a](https://github.com/cameronsjo/bosun/commit/4a1348a137b9a929a6111e909823b82302f840c1))
* **webui:** add React dashboard with maritime theme ([60d973b](https://github.com/cameronsjo/bosun/commit/60d973b9d636f18dadb770a6f9eb825aa26f9f4d))


### Bug Fixes

* add missing language tag to fenced code block in commands.md ([eaafd88](https://github.com/cameronsjo/bosun/commit/eaafd8838ee39b3a061c07ef08502282e4c2b84e))
* add text language specifier to remaining code blocks in gitops comparison (MD040) ([d5ac933](https://github.com/cameronsjo/bosun/commit/d5ac9335b81bd2ea135149728d12c1058c69b9f5))
* address CodeRabbit follow-up comments from PR [#42](https://github.com/cameronsjo/bosun/issues/42) round 2 ([0694912](https://github.com/cameronsjo/bosun/commit/0694912a386de9b7036cb8db4e18b63f81b44cc5))
* address CodeRabbit PR [#42](https://github.com/cameronsjo/bosun/issues/42) review — 16 items across docs, specs, and scripts ([9e8e6ef](https://github.com/cameronsjo/bosun/commit/9e8e6ef6250bad854f241f71b23c5b3843470b37))
* address CodeRabbit review and CI lint failures ([774f98b](https://github.com/cameronsjo/bosun/commit/774f98bc4032225d35ae43db6dea21cd0d5618fc))
* address CodeRabbit review feedback ([03ae721](https://github.com/cameronsjo/bosun/commit/03ae721edebb2e87886d1347998f622436b931b8))
* address CodeRabbit review round 8 findings ([f1984fe](https://github.com/cameronsjo/bosun/commit/f1984fedb9177cbf76eca9aa306203bc831dd5b4))
* address CodeRabbit round 2 review feedback ([307c4b9](https://github.com/cameronsjo/bosun/commit/307c4b972fde77cd3b2292ab9bedc8d2dc8150a2))
* address CodeRabbit round 3 review feedback ([e7a57db](https://github.com/cameronsjo/bosun/commit/e7a57db548531a758204eaed4b66f4220332efa4))
* **alert:** address CodeRabbit findings on Slack provider ([8b1f5c3](https://github.com/cameronsjo/bosun/commit/8b1f5c3efc3cf2d1f5dc69d300b16053eeacb386))
* **alert:** address CodeRabbit findings on webhook provider ([fa75633](https://github.com/cameronsjo/bosun/commit/fa756331fe5c9c87a56fef71389f79bd197ec48b))
* **alert:** tighten test assertions for metadata absence and duration bounds ([5bc9415](https://github.com/cameronsjo/bosun/commit/5bc94153682b7d91546c6719477127df5a038bee))
* **alert:** wire httptest server in severity skip test and move assertions out of handler goroutine ([fd38c96](https://github.com/cameronsjo/bosun/commit/fd38c9602259ba5e9a0336835ea0b1e7becf8ad2))
* **ci:** bump golangci-lint image from v1.64 to v2.1 ([b68e7fe](https://github.com/cameronsjo/bosun/commit/b68e7fef8295c54f689cc5adb38991afe40c8bc3))
* **ci:** bump minor version on feat commits pre-1.0 ([0f4bcdd](https://github.com/cameronsjo/bosun/commit/0f4bcdd85ca9c6e9ae4cd9b198ebf53305eca5c3))
* **ci:** checkout release tag instead of HEAD for goreleaser ([d29549a](https://github.com/cameronsjo/bosun/commit/d29549af5b1d23bd11eeab94a29eb8eaeec507a7))
* **ci:** disable cosign signing in goreleaser ([fcdd747](https://github.com/cameronsjo/bosun/commit/fcdd7473af61ba8b2205324fdacad1cc8bcffed9))
* **ci:** disable docker builds in goreleaser ([a24a9bb](https://github.com/cameronsjo/bosun/commit/a24a9bbe5073f4f600cdec1bd3cbe4496d059eba))
* **ci:** disable SLSA attestation step ([e8c922c](https://github.com/cameronsjo/bosun/commit/e8c922cc157e09ef3a26a876085125f2e9d16fbd))
* **ci:** graceful fallback when release App token is not configured ([197f460](https://github.com/cameronsjo/bosun/commit/197f46019818f015dfb683a8c3775a5c7a75ae71))
* **ci:** increase max-turns and add timeout for code review ([5323f49](https://github.com/cameronsjo/bosun/commit/5323f49a303ad23a9156e15dc4797d9a0c670e73))
* **ci:** install git in test container ([c25332b](https://github.com/cameronsjo/bosun/commit/c25332b54bb4127f73ebaba90f819e2d835dd7df))
* **ci:** migrate golangci-lint to v2 and fix all errcheck/staticcheck violations ([379408d](https://github.com/cameronsjo/bosun/commit/379408d19ae71b3e31da9bd99c91317aea71ff16))
* **ci:** move fromJSON out of env block to prevent template error ([5831547](https://github.com/cameronsjo/bosun/commit/58315474a81b92a681f58fd9442fbae8d7372523))
* **ci:** remove -race flag from tests (requires CGO) ([6ba452c](https://github.com/cameronsjo/bosun/commit/6ba452c18c25c92667da08b7aae261f2cb3b3c31))
* **ci:** remove goreleaser prefix from exec commands ([02f0e00](https://github.com/cameronsjo/bosun/commit/02f0e00c581dfbbdf0354b553c88c7e5a084a5f2))
* **ci:** rename CIAll to All for proper CLI naming ([5593ee8](https://github.com/cameronsjo/bosun/commit/5593ee8587f4509b768a7e37c5ae5618eb8adb37))
* **ci:** rename Platform to buildTarget to avoid Dagger conflict ([9e43a40](https://github.com/cameronsjo/bosun/commit/9e43a40a33ddf98e8320173b87bae93f574adc38))
* **ci:** upgrade Claude workflow permissions to write for PRs and issues ([0b7a6ec](https://github.com/cameronsjo/bosun/commit/0b7a6ecb89f6392a22f79787be71263836d0b9c3))
* **ci:** use correct dagger-for-github action ([a1c481b](https://github.com/cameronsjo/bosun/commit/a1c481b54015da8010a71cab3ca8b73be6c3cd11))
* **ci:** use full semver tag for dagger-for-github action ([6dedd29](https://github.com/cameronsjo/bosun/commit/6dedd29440310f129af8082ef7e746387dcf15eb))
* **ci:** use GitHub App token for release PR auto-merge ([3cee031](https://github.com/cameronsjo/bosun/commit/3cee031bfdb4d9a4aba07011d66b1b26dc51f1a5))
* **ci:** use goreleaser:latest instead of non-existent v2 tag ([50d5ea7](https://github.com/cameronsjo/bosun/commit/50d5ea70ba8050dd5a5a2ff5aa7cff08263d6179))
* **config:** change InfraSubDir default from 'infrastructure' to '.' ([7759a86](https://github.com/cameronsjo/bosun/commit/7759a86139413a8343f6b9da2ffddfb2bc48c278))
* **daemon,tunnel:** neutralize audit log message and redact cloudflared stderr ([83c242f](https://github.com/cameronsjo/bosun/commit/83c242f9a51d6b83f77e316c5725161766603a73))
* **daemon:** add panic recovery to background goroutines ([306ad06](https://github.com/cameronsjo/bosun/commit/306ad064d336d6453f09c71ea7af433d5b63d32e))
* **daemon:** inject mock Docker client in health subsystem tests for CI ([d775cab](https://github.com/cameronsjo/bosun/commit/d775cabdde0c0f8b05e962e646679c83df665e0c))
* **daemon:** load post_sync_hooks from bosun.yaml in ConfigFromEnv ([2e1a952](https://github.com/cameronsjo/bosun/commit/2e1a952df6f499d61e6df93d1beba7ae4ae2ec6b)), closes [#49](https://github.com/cameronsjo/bosun/issues/49)
* **daemon:** panic recovery and template path validation ([cc4d378](https://github.com/cameronsjo/bosun/commit/cc4d37800195622c1fb1f387c856833963992694))
* **daemon:** use live Docker ping for health subsystem check ([71ca556](https://github.com/cameronsjo/bosun/commit/71ca5568b61194ddea71fc13e85a7e7804a60b2c))
* **daemon:** validate non-positive duration env vars for timeouts ([f288e76](https://github.com/cameronsjo/bosun/commit/f288e76a058536a4191f3d64324f336ff6932070))
* **deps:** bump go.opentelemetry.io/otel/sdk to v1.40.0 ([#103](https://github.com/cameronsjo/bosun/issues/103)) ([d5bb4a0](https://github.com/cameronsjo/bosun/commit/d5bb4a05fef822c6ff33e368099a44bc7d5bd83d))
* **deps:** run go mod tidy for prometheus dependency ([d441093](https://github.com/cameronsjo/bosun/commit/d441093172477c29f3c06082eff8f3ed77fd6baf))
* **deps:** upgrade go-git and edwards25519 to patch security vulnerabilities ([6b785c5](https://github.com/cameronsjo/bosun/commit/6b785c5dff8e6a87dd4826efac1caa45df7b8e1f))
* **deps:** upgrade otel SDK to v1.42.0 to resolve CVE path hijacking ([720a6cb](https://github.com/cameronsjo/bosun/commit/720a6cb57bb8515f27cf5422e58b458342f464be))
* **docker:** add project name to compose commands to prevent orphan containers ([242c57c](https://github.com/cameronsjo/bosun/commit/242c57cceaece9a0c373fe43a9abf0cf33ec0a29))
* **docker:** create /var/run/bosun directory in container image ([4f7a8fb](https://github.com/cameronsjo/bosun/commit/4f7a8fb3ba800ebb45977e17c907a746e103250f))
* **docker:** simplify Dockerfile to use bosun daemon directly ([5f291ed](https://github.com/cameronsjo/bosun/commit/5f291ed97d0776d87f9c6040713f8ec283a9b322))
* **drift:** address review findings from PR [#29](https://github.com/cameronsjo/bosun/issues/29) ([a409723](https://github.com/cameronsjo/bosun/commit/a409723434d993761eee64e4f44b833a73975015))
* **drift:** include container names in drift check log entries ([c94a671](https://github.com/cameronsjo/bosun/commit/c94a6712a57d6d5f32aa610afb04d775a20c831b)), closes [#61](https://github.com/cameronsjo/bosun/issues/61)
* **hooks:** address CodeRabbit findings on exec post-sync hooks ([7d48c57](https://github.com/cameronsjo/bosun/commit/7d48c572c092fa20a0ce77df49cac1cc560412a9))
* **lint:** add nolint directives for deprecated Docker SDK types ([ff1dc4e](https://github.com/cameronsjo/bosun/commit/ff1dc4e740b6a3a07609f91540f6e606183dfb39))
* **lint:** fix all remaining errcheck issues in webhook.go ([bf8f3f7](https://github.com/cameronsjo/bosun/commit/bf8f3f7c4ea3ec8cd9e35a452eeaea2814b7e66d))
* **lint:** fix remaining cmd.Help errcheck issues ([3a0b94d](https://github.com/cameronsjo/bosun/commit/3a0b94dae9ab304954ba092b701fb81af315f91a))
* **lint:** remove unused completeProvisionNames function ([e5e27e6](https://github.com/cameronsjo/bosun/commit/e5e27e6f6daa7d1ee51e199687c71a295e1e7f86))
* **lint:** resolve all remaining errcheck issues ([4c618df](https://github.com/cameronsjo/bosun/commit/4c618dfc98877de1035f8ff61bd6f7902cee6119))
* **lint:** resolve errcheck and unused function lint errors ([7fc9e37](https://github.com/cameronsjo/bosun/commit/7fc9e373912dabeb613479c0a94859c1efeeaf0d))
* **lint:** resolve errcheck in drift command and daemon startup ([a1bbe25](https://github.com/cameronsjo/bosun/commit/a1bbe25068d86263c5915c5d77421013657298fb))
* **lint:** resolve errcheck issues in daemon package ([28b0f7b](https://github.com/cameronsjo/bosun/commit/28b0f7b6bd3295e4fd79fe6fb7ba0189260dbfab))
* **lint:** resolve errcheck violations and add local lint targets ([7c61406](https://github.com/cameronsjo/bosun/commit/7c614064aa4288a9876be81a482460378f92903d))
* **lint:** resolve remaining errcheck issues ([87507d4](https://github.com/cameronsjo/bosun/commit/87507d45555ad2ef1f0ea6f56507a0b8aa97d9a3))
* **lock:** implement proper Windows file locking with LockFileEx ([a21df66](https://github.com/cameronsjo/bosun/commit/a21df6684bf0ba407deafeef2608c878ecb19b92))
* **log:** address CodeRabbit review feedback ([18af41b](https://github.com/cameronsjo/bosun/commit/18af41b89ae1634cb6a18879d148a10d1789c385))
* **log:** address CodeRabbit round 2 review feedback ([0f378a8](https://github.com/cameronsjo/bosun/commit/0f378a80ab6dfa02910b6c859bf85f09bf5af538))
* **log:** complete ComponentCtx migration for remaining call sites ([6e8d510](https://github.com/cameronsjo/bosun/commit/6e8d51076e1a7bdfdf72f818ef5b092f99c03d49))
* **logging:** add structured logging to remote deploy ops and replace ui.* in daemon paths ([27d492f](https://github.com/cameronsjo/bosun/commit/27d492f09bb275f0dc7f14ae62c171e6e3f811f6))
* **logging:** address CodeRabbit review feedback across 13 files ([cc03511](https://github.com/cameronsjo/bosun/commit/cc035118a8bfcf737c79cc8f2f0a25efe8832896))
* **logging:** address CodeRabbit review round 2 ([c8bef3d](https://github.com/cameronsjo/bosun/commit/c8bef3dc8bf8ba4b3dadd123ccdc7bf0eb71461e))
* **logging:** replace ui.* with structured logging in daemon audit middleware and add retry logging ([fc3565f](https://github.com/cameronsjo/bosun/commit/fc3565f7451f03c5f340b9d8525acdbd40f17b08))
* **log:** use FromContext to avoid zerolog key duplication ([170915e](https://github.com/cameronsjo/bosun/commit/170915e45a7e1fcece598355c5d4a9e0d3c572db))
* **observability:** add structured logging to drift detection pipeline ([f0bbdca](https://github.com/cameronsjo/bosun/commit/f0bbdcaf14fb0dce67d3c3ef0adfc4fa01b010d5))
* **openspec:** add failure scenarios and clarify pull interaction for image policy ([e4386a3](https://github.com/cameronsjo/bosun/commit/e4386a355ffb6bc4ecc6551b0088c358ab4c39db))
* **openspec:** address CodeRabbit findings on deploy paths proposal ([ba9d64e](https://github.com/cameronsjo/bosun/commit/ba9d64e26aef3fd404d528147e6cd83309b5076e))
* **openspec:** address CodeRabbit findings on image update policy proposal ([2f2a748](https://github.com/cameronsjo/bosun/commit/2f2a748d3cfb56aca4aab2858a2596d3fb3fb013))
* **openspec:** address CodeRabbit findings on manifest schema proposal ([4864c9e](https://github.com/cameronsjo/bosun/commit/4864c9eea586f983b51c5635fe103867e3547735))
* **openspec:** polish manifest schema spec wording ([ecd60f3](https://github.com/cameronsjo/bosun/commit/ecd60f3103f174fa1e34991d14108f539499bafd))
* **openspec:** use SHALL in Drift Alerts requirement description ([6f969d7](https://github.com/cameronsjo/bosun/commit/6f969d7b0860f8b2d127bfd312ac242a7cc7417e))
* **reconcile:** address CodeRabbit findings on atomic deploy ([c8c8e05](https://github.com/cameronsjo/bosun/commit/c8c8e05d570c403c2a8e7f3267b1f5384aac6f64))
* **reconcile:** address CodeRabbit review feedback ([1469ed9](https://github.com/cameronsjo/bosun/commit/1469ed9a0cbef90ed78e609b3d27eecac0efd194))
* **reconcile:** address CodeRabbit review findings for restart breaker ([9321408](https://github.com/cameronsjo/bosun/commit/93214081eb2d8b736751f1ea8b99f70cfafce9c1))
* **reconcile:** address CodeRabbit review findings on health gate ([9c173be](https://github.com/cameronsjo/bosun/commit/9c173beb39e452b6fc7ecd52b8a6ee70abf30d27))
* **reconcile:** alert on all pipeline failure stages with on_failure/on_success gates ([#95](https://github.com/cameronsjo/bosun/issues/95)) ([3974504](https://github.com/cameronsjo/bosun/commit/39745046a602aa2a6fc9c2b1cc70c32d4bf01483))
* **reconcile:** classify compose failures to tolerate unhealthy containers ([#92](https://github.com/cameronsjo/bosun/issues/92)) ([d73332e](https://github.com/cameronsjo/bosun/commit/d73332edceae9141ae257f5fcd2e4018f0062c34))
* **reconcile:** deploy all compose files, not just core.yml ([98d470e](https://github.com/cameronsjo/bosun/commit/98d470e1846f00b00b6d5a6c55860ae88b5e0d98))
* **reconcile:** fail fast when pre-deploy NeedsRedeploy persistence fails ([c838c1f](https://github.com/cameronsjo/bosun/commit/c838c1f685677ec9b7eef3354a9144236d1681fb))
* **reconcile:** fire post-sync hooks when DiffFiles fails on shallow repo ([a5757eb](https://github.com/cameronsjo/bosun/commit/a5757ebe86e6fbc3f8ad6c4e86fe7822eddc0074)), closes [#55](https://github.com/cameronsjo/bosun/issues/55)
* **reconcile:** gate Compose Manager sync on discovered compose target ([64aa37a](https://github.com/cameronsjo/bosun/commit/64aa37a0811885af3f0aefc19274230b662e8df9))
* **reconcile:** make compose up timeout configurable via BOSUN_COMPOSE_UP_TIMEOUT ([b3e0b8a](https://github.com/cameronsjo/bosun/commit/b3e0b8a7906efe4e7b00aa7b875c1be3ef5d652f)), closes [#83](https://github.com/cameronsjo/bosun/issues/83)
* **reconcile:** override go-git DefaultAuthBuilder for SSH without agent ([4b0b831](https://github.com/cameronsjo/bosun/commit/4b0b83192ed92d74c672fe2bbdd116e263748ff7))
* **reconcile:** post-sync hooks regression + deploy_paths allowlist ([fa922da](https://github.com/cameronsjo/bosun/commit/fa922dabb4e7186b12b92bd05bbd442d87ced364))
* **reconcile:** propagate remote compose up failure ([b56697a](https://github.com/cameronsjo/bosun/commit/b56697a3a6880e1f7cf467ded4174de3b98e0a6d))
* **reconcile:** propagate remote compose up failure instead of swallowing ([540e99f](https://github.com/cameronsjo/bosun/commit/540e99ff4c63149e6bc3014ba577967fe893a9a5))
* **reconcile:** reload bosun.yaml from repo after git pull ([2e55bc2](https://github.com/cameronsjo/bosun/commit/2e55bc268f78dabf59ea3c559148c3a78a55ef42))
* **reconcile:** remove --wait from compose up, add unhealthy container alerts ([91bd3bb](https://github.com/cameronsjo/bosun/commit/91bd3bb8798d8e976e436f9fd0f8482c1ac41c70))
* **reconcile:** skip permission test when running as root ([7aac83b](https://github.com/cameronsjo/bosun/commit/7aac83b2fc5dd488caeab00a72e2fc4230acf971))
* **reconcile:** track partial deploy failure for retry on next reconcile ([dda6ea8](https://github.com/cameronsjo/bosun/commit/dda6ea8907004b71c408c83b000077a47e3ab314))
* **reconcile:** track partial deploy failure for retry on next reconcile ([cc3aa0f](https://github.com/cameronsjo/bosun/commit/cc3aa0fe01fec80056d91f1d9a2d65402d6f44ee))
* **reconcile:** track partial deploy failure for retry on next reconcile ([#132](https://github.com/cameronsjo/bosun/issues/132)) ([dda6ea8](https://github.com/cameronsjo/bosun/commit/dda6ea8907004b71c408c83b000077a47e3ab314))
* **reconcile:** use atomic rename-aside pattern in DeployLocal ([ebc918c](https://github.com/cameronsjo/bosun/commit/ebc918cfdbb42cb4ea1751649bdec6dde5b56c29))
* **reconcile:** use atomic rename-aside pattern in DeployLocal ([e2dcf44](https://github.com/cameronsjo/bosun/commit/e2dcf444f33fbabd2836215c7acd845b5da6ea26))
* **reconcile:** use known_hosts for SSH host key verification ([f41eecd](https://github.com/cameronsjo/bosun/commit/f41eecd718bcc56d186c2826e8c229bc32b7a97f))
* **reconcile:** use nil guards for deploy sync reload and add env vars to docs ([43d5148](https://github.com/cameronsjo/bosun/commit/43d5148a5ea3fac40000c9310d87b90632f4ab2d))
* **reconcile:** validate paths in template include and fromJsonFile ([50da42f](https://github.com/cameronsjo/bosun/commit/50da42f9f151f8296f727bfdc984e1eab0471115))
* **reconcile:** warn on dirty repo instead of blocking pull ([c82e91c](https://github.com/cameronsjo/bosun/commit/c82e91cc6217ab4fcbe96afe20622d3025b9505d))
* **release:** fix goreleaser config and lint issues ([477102c](https://github.com/cameronsjo/bosun/commit/477102ccb5f810c4d6cd6efeb9ff4be0b751b251))
* resolve 11 bugs and code quality issues across codebase ([4c714a4](https://github.com/cameronsjo/bosun/commit/4c714a476bcc3f5b34bf9cc3ff6e39c2206a4ecd))
* resolve 3 low-hanging issues from codebase investigation ([e3cfedd](https://github.com/cameronsjo/bosun/commit/e3cfedd7e74cec0351b754cbe7dcff2e57db6960))
* resolve 3 low-hanging issues from codebase investigation ([06c2d31](https://github.com/cameronsjo/bosun/commit/06c2d31dc8023581320dfbc2be7c3ec04dde325c))
* resolve 6 just-do-it issues from backlog ([022c408](https://github.com/cameronsjo/bosun/commit/022c4083cbebf849c32d334f17e01de898e998c0))
* resolve merge conflicts with main ([5ba4823](https://github.com/cameronsjo/bosun/commit/5ba4823e32cdd9de655a5bbe592d5610b3ee2d84))
* **retry:** address CodeRabbit findings on retry utility ([b5fef4f](https://github.com/cameronsjo/bosun/commit/b5fef4f43594c37242a68b9e55d78e580e8af0eb))
* **scripts:** harden diagram renderer per CodeRabbit review ([afe34f2](https://github.com/cameronsjo/bosun/commit/afe34f2470f4150061937ecfc7e64395baf99d1b))
* **sentry:** remove redundant io.Writer type assertion ([fca288a](https://github.com/cameronsjo/bosun/commit/fca288a839e21274cc46222af215392bab175d02))
* **spec:** address CodeRabbit findings on auth health gate ([ea83fbd](https://github.com/cameronsjo/bosun/commit/ea83fbdb883d6559b32c7130012121ef90736b96))
* **spec:** clarify rollback is local-only per CodeRabbit review ([0cd0321](https://github.com/cameronsjo/bosun/commit/0cd0321667189eb068c12cea42b99d7906334b04))


### Performance Improvements

* **ci:** parallelize test, lint, and webui in Dagger pipeline ([26aaaa5](https://github.com/cameronsjo/bosun/commit/26aaaa5ae21e0b4636359c49b3beaa48312d61fd))


### Reverts

* undo golangci-lint v2 bump (needs separate migration) ([0650a1a](https://github.com/cameronsjo/bosun/commit/0650a1a38a242fe122f079fd0ea129740ae457f6))
* undo otel SDK bump that broke CI Go version ([40ae291](https://github.com/cameronsjo/bosun/commit/40ae29187b3a39f8bdcf63776c7c8951c6c59bb8))

## [0.27.1](https://github.com/cameronsjo/bosun/compare/v0.27.0...v0.27.1) (2026-03-14)


### Bug Fixes

* **openspec:** polish manifest schema spec wording ([ecd60f3](https://github.com/cameronsjo/bosun/commit/ecd60f3103f174fa1e34991d14108f539499bafd))

## [0.27.0](https://github.com/cameronsjo/bosun/compare/v0.26.0...v0.27.0) (2026-03-14)


### Features

* **docker:** add graceful container shutdown with configurable timeout ([4186310](https://github.com/cameronsjo/bosun/commit/41863106bfbc39b134212a1ba9de74cdc5ee81c0))
* **docker:** add graceful container shutdown with configurable timeout ([6eb33bd](https://github.com/cameronsjo/bosun/commit/6eb33bda9c48f1bc54abc37240ad1daccf2ae30a)), closes [#375](https://github.com/cameronsjo/bosun/issues/375)
* **docker:** add graceful container shutdown with configurable timeout ([#135](https://github.com/cameronsjo/bosun/issues/135)) ([4186310](https://github.com/cameronsjo/bosun/commit/41863106bfbc39b134212a1ba9de74cdc5ee81c0))


### Bug Fixes

* **openspec:** address CodeRabbit findings on deploy paths proposal ([ba9d64e](https://github.com/cameronsjo/bosun/commit/ba9d64e26aef3fd404d528147e6cd83309b5076e))
* **reconcile:** fail fast when pre-deploy NeedsRedeploy persistence fails ([c838c1f](https://github.com/cameronsjo/bosun/commit/c838c1f685677ec9b7eef3354a9144236d1681fb))
* **reconcile:** track partial deploy failure for retry on next reconcile ([dda6ea8](https://github.com/cameronsjo/bosun/commit/dda6ea8907004b71c408c83b000077a47e3ab314))
* **reconcile:** track partial deploy failure for retry on next reconcile ([cc3aa0f](https://github.com/cameronsjo/bosun/commit/cc3aa0fe01fec80056d91f1d9a2d65402d6f44ee))
* **reconcile:** track partial deploy failure for retry on next reconcile ([#132](https://github.com/cameronsjo/bosun/issues/132)) ([dda6ea8](https://github.com/cameronsjo/bosun/commit/dda6ea8907004b71c408c83b000077a47e3ab314))

## [0.26.0](https://github.com/cameronsjo/bosun/compare/v0.25.1...v0.26.0) (2026-03-13)


### Features

* add critical container health gate with rollback ([f3820fc](https://github.com/cameronsjo/bosun/commit/f3820fcd1753789a13084ed113ddb25b70116c65))
* add critical container health gate with rollback ([#129](https://github.com/cameronsjo/bosun/issues/129)) ([f3820fc](https://github.com/cameronsjo/bosun/commit/f3820fcd1753789a13084ed113ddb25b70116c65))


### Bug Fixes

* **reconcile:** address CodeRabbit review findings on health gate ([9c173be](https://github.com/cameronsjo/bosun/commit/9c173beb39e452b6fc7ecd52b8a6ee70abf30d27))

## [0.25.1](https://github.com/cameronsjo/bosun/compare/v0.25.0...v0.25.1) (2026-03-13)


### Bug Fixes

* **reconcile:** propagate remote compose up failure ([b56697a](https://github.com/cameronsjo/bosun/commit/b56697a3a6880e1f7cf467ded4174de3b98e0a6d))
* **reconcile:** propagate remote compose up failure instead of swallowing ([540e99f](https://github.com/cameronsjo/bosun/commit/540e99ff4c63149e6bc3014ba577967fe893a9a5))

## [0.25.0](https://github.com/cameronsjo/bosun/compare/v0.24.0...v0.25.0) (2026-03-13)


### Features

* **hooks:** add exec action to post-sync hooks ([b966ae4](https://github.com/cameronsjo/bosun/commit/b966ae4f63909e440ff6cebeeba03e675ad87cab))

## [0.24.0](https://github.com/cameronsjo/bosun/compare/v0.23.0...v0.24.0) (2026-03-13)


### Features

* add bosun CLI and restore ASCII diagram to README ([1081e8d](https://github.com/cameronsjo/bosun/commit/1081e8d21f6846da3a1e3c79b6fb66d588ccadcf))
* add Claude Code plugin with onboarding skill ([db8bd2a](https://github.com/cameronsjo/bosun/commit/db8bd2a59a0d9ffc6a1eaf5ed4fa850a1717671c))
* add deploy resilience — alert throttling, --wait removal, post-sync hooks ([7b278d7](https://github.com/cameronsjo/bosun/commit/7b278d7c840fc4be6b4d95d5eaf92d1b20959bee))
* add native daemon mode with Unix socket API and webhook support ([34d05cf](https://github.com/cameronsjo/bosun/commit/34d05cf74f39ebc26d897c1265a3c4a17d27da4b))
* **alert:** add drift alert debounce to suppress transient flaps ([#94](https://github.com/cameronsjo/bosun/issues/94)) ([8dc2d9b](https://github.com/cameronsjo/bosun/commit/8dc2d9b85850eb279d7381bcd339a80b5e42a0f4))
* **alert:** add generic webhook alert provider ([2cd2ca7](https://github.com/cameronsjo/bosun/commit/2cd2ca7e88ea889b8402d48ceda99cbb46cf9028))
* **alert:** add generic webhook alert provider ([1089be4](https://github.com/cameronsjo/bosun/commit/1089be4b7fcadbf0148db764022fb0093ad793ac))
* **alert:** add native alerting system with Discord, SendGrid, Twilio ([7126cf4](https://github.com/cameronsjo/bosun/commit/7126cf48303c446f4aef07dc5289cca9fc816cd7))
* **alert:** add Slack webhook alert provider ([f2f2e73](https://github.com/cameronsjo/bosun/commit/f2f2e7303905e3c030bab41fb0d8a268b1df0e17))
* **alert:** add Slack webhook alert provider ([6143ca1](https://github.com/cameronsjo/bosun/commit/6143ca18ed72473490dc7ae1231d8563af266151))
* **api:** add Homepage dashboard widget endpoint ([ce60071](https://github.com/cameronsjo/bosun/commit/ce60071fbc641e686a81ddb04c573a0cfe562048)), closes [#36](https://github.com/cameronsjo/bosun/issues/36)
* apply essentials scaffold ([e877efb](https://github.com/cameronsjo/bosun/commit/e877efb809c9925f50b02f81a36d1ccd149e1127))
* **ci:** add Docker image build+push to release workflow ([a57475d](https://github.com/cameronsjo/bosun/commit/a57475d6f3d3141a3e6e6ab2a5cba0d33e17358e))
* **ci:** add GitHub Actions CI/CD and self-update command ([fad639d](https://github.com/cameronsjo/bosun/commit/fad639d3b8ae24a0180de303802e942e817e7bea))
* **ci:** add WebUI to Dagger pipeline ([5f97f14](https://github.com/cameronsjo/bosun/commit/5f97f14b90224a59bca5c5e5026a09f10576578f))
* **ci:** convert GitHub Actions to Dagger pipelines ([7364990](https://github.com/cameronsjo/bosun/commit/736499052370ff6d688b97b321c679d48db86e96))
* **ci:** convert GitHub Actions to Dagger pipelines ([3456db2](https://github.com/cameronsjo/bosun/commit/3456db2d84bc9a94b54f5ec3bc01452a371b2a80))
* **ci:** replace manual release with release-please ([d270336](https://github.com/cameronsjo/bosun/commit/d270336e631b05eee4d7cacb0285bee72527da8e))
* **cli:** add bosun drift command for config drift detection ([f615103](https://github.com/cameronsjo/bosun/commit/f61510340678d0ffb3d69e78c83766e597d9249a))
* **cli:** add bosun log command for release history ([1287ab6](https://github.com/cameronsjo/bosun/commit/1287ab68ba256fbcd99c61b52be8cc876ae1b579))
* **cli:** add core commands and P2 features ([e43080a](https://github.com/cameronsjo/bosun/commit/e43080a3eca55f30ad0c692a483103726c134d9d))
* **cli:** add dynamic shell completions ([615f837](https://github.com/cameronsjo/bosun/commit/615f837da6710301e4c64d9092c917bba540a6a1))
* **cli:** add render command for local template preview ([b454313](https://github.com/cameronsjo/bosun/commit/b45431353561d943f36860918b5bd05b4badfcac))
* **cli:** add secret pirate aliases 🏴‍☠️ ([7edd376](https://github.com/cameronsjo/bosun/commit/7edd3760a8461af1690dad4076e996acf9ec52a0))
* **composer:** implement service composer for Phase 1 ([537c2f4](https://github.com/cameronsjo/bosun/commit/537c2f401ea48ddf5c8673b558b57a4c0a84fa43))
* **config:** add configurable orphan container cleanup ([#93](https://github.com/cameronsjo/bosun/issues/93)) ([aa90339](https://github.com/cameronsjo/bosun/commit/aa903397397a08c75a004f19b564f14646e3b478))
* **config:** add post_sync_hooks to bosun.yaml config surface ([0396d60](https://github.com/cameronsjo/bosun/commit/0396d607abe2889c619a08b2932621c31611b9cd)), closes [#38](https://github.com/cameronsjo/bosun/issues/38)
* **daemon:** add BOSUN_INFRA_DIR env var support ([84f74a3](https://github.com/cameronsjo/bosun/commit/84f74a3dc23e9d207f1310b695ce5d1f666e92dd))
* **daemon:** add drift alert deduplication with per-item cooldown ([6efc0e6](https://github.com/cameronsjo/bosun/commit/6efc0e6b155adbcff4f38598aeb988bf3cf1ead7))
* **daemon:** add native daemon mode with HTTP server ([dea3ade](https://github.com/cameronsjo/bosun/commit/dea3ade4dd8395e78304aef6182a9286270d59db))
* **daemon:** add structured logging to API handlers and TCP auth ([8ca9409](https://github.com/cameronsjo/bosun/commit/8ca940926112c1628664b05d9f1f088633251c44))
* **daemon:** add subsystem breakdown to health endpoint ([f4e3aa5](https://github.com/cameronsjo/bosun/commit/f4e3aa597639e47f03a0d82d36bdf007523a8bbd))
* **daemon:** add subsystem breakdown to health endpoint ([25ca040](https://github.com/cameronsjo/bosun/commit/25ca040b14d11f20147ed2f8a8fe7c695b2b4a9f))
* **daemon:** add Unix socket API with multi-provider webhook support ([a43308e](https://github.com/cameronsjo/bosun/commit/a43308e6177d36a0767278d55e8557e92ed95ca6))
* **daemon:** replace hand-rolled metrics with Prometheus ([2cfcf14](https://github.com/cameronsjo/bosun/commit/2cfcf14efb048b826b7474f2009635c9f21f45f8))
* **daemon:** replace hand-rolled metrics with Prometheus ([03f1016](https://github.com/cameronsjo/bosun/commit/03f1016c7b2b537e7e282880c227374a79762683))
* **daemon:** Unix socket API with multi-provider webhooks ([6298f80](https://github.com/cameronsjo/bosun/commit/6298f80dfe3bbd2d31f1b221936cf9d6ece6dd3f))
* **docs:** add mermaid-to-ascii rendering script ([cdd80ae](https://github.com/cameronsjo/bosun/commit/cdd80ae7a88df7a3b2b6d065faba41143119e14d))
* **docs:** render README diagrams as ASCII art ([4f2ea70](https://github.com/cameronsjo/bosun/commit/4f2ea70ae1e69472e5f4b14880b67192b484df53))
* **drift:** enrich unhealthy drift items with health check diagnostics ([58137ca](https://github.com/cameronsjo/bosun/commit/58137ca32c0180a52f8f2fed94b9b9419ab88ef1)), closes [#61](https://github.com/cameronsjo/bosun/issues/61)
* **git:** add SSH key file support for git operations ([fb26cde](https://github.com/cameronsjo/bosun/commit/fb26cde35b9e044f46d7061a2fb36e4a9140fe86))
* **go:** add comprehensive tests and release config (Phases 8-9) ([c48eb42](https://github.com/cameronsjo/bosun/commit/c48eb42ae495335a746902d564cf2a393a89103d))
* **go:** implement phases 2-5 in parallel ([78d62cd](https://github.com/cameronsjo/bosun/commit/78d62cd3ca7dc7d20bfcca4b1ff07c6cccd62bf4))
* **go:** implement phases 6-7 (init, comms, reconcile) ([6761e8c](https://github.com/cameronsjo/bosun/commit/6761e8caa9fb155b02c3fd26496a202d706e12b1))
* **go:** scaffold Go CLI foundation (Phase 1) ([6d7fcf9](https://github.com/cameronsjo/bosun/commit/6d7fcf9614229661c897037428062942094e4c8b))
* **init:** add domain prompt and Traefik config generation ([b1fe645](https://github.com/cameronsjo/bosun/commit/b1fe6452bd5baeef19dc2a461ace10974ad11583))
* initial unops scaffold ([2f1b379](https://github.com/cameronsjo/bosun/commit/2f1b3798e148a27c52e59b98a23b81cc6d12b76b))
* **lint:** add port conflict detection ([957cf9a](https://github.com/cameronsjo/bosun/commit/957cf9af19aec6b1b9d83ed50b45b13d031b3175))
* **log:** add ComponentCtx for context-aware logger construction ([cacb01e](https://github.com/cameronsjo/bosun/commit/cacb01e2fa9489f41f6fde23d9c1782a5d7492de))
* **log:** add structured logging with zerolog ([871e65a](https://github.com/cameronsjo/bosun/commit/871e65a7162833ff67f3771092043dfa3f429476))
* **log:** add structured logging with zerolog ([cfc1eee](https://github.com/cameronsjo/bosun/commit/cfc1eeee289a02c657dae12cb54d1d79dff2b3a4))
* **log:** adopt context-aware logger across codebase ([664f795](https://github.com/cameronsjo/bosun/commit/664f79538de70019415dfbd33b70c2d70ec61eb1))
* **log:** adopt context-aware logger across codebase ([#63](https://github.com/cameronsjo/bosun/issues/63)) ([664f795](https://github.com/cameronsjo/bosun/commit/664f79538de70019415dfbd33b70c2d70ec61eb1))
* **log:** enrich context at pipeline entry points and migrate sub-operations ([832fa8c](https://github.com/cameronsjo/bosun/commit/832fa8c1c4adee155d04a5fc5dcaebea77860aa7))
* **logging:** add debug logging to SOPS, template rendering, and snapshot operations ([b11d65b](https://github.com/cameronsjo/bosun/commit/b11d65b26cac59ae57108acbe48b3726c7968604))
* **logging:** add P3 logging to SSH auth, ConfigFromEnv, alert providers, and CLI commands ([a2106fb](https://github.com/cameronsjo/bosun/commit/a2106fbc8fb25c27957ee18ead0bd123cda934b4))
* **logging:** add structured logging across reconcile, daemon, and drift ([67b1a44](https://github.com/cameronsjo/bosun/commit/67b1a4445caae3ebf8cdf94caa1b673f32e9c851))
* **logging:** comprehensive structured logging and fixes from code review ([c46a5af](https://github.com/cameronsjo/bosun/commit/c46a5afefad9cf4d7b88e462b30db036784b5343))
* **log:** migrate docker client and add retry logging ([98d2200](https://github.com/cameronsjo/bosun/commit/98d220059489932743d2c949f324835fb3c7a732))
* **manifest:** add 'needs' shorthand for dependencies ([5df611e](https://github.com/cameronsjo/bosun/commit/5df611e9d541858efe15d4888f7cdda521d79859))
* **manifest:** add compose overrides and network merging ([9ca81d3](https://github.com/cameronsjo/bosun/commit/9ca81d3255f5ae9e9580906591542a067abcea8f))
* **manifest:** add Helm-aligned chart format ([#15](https://github.com/cameronsjo/bosun/issues/15)) ([aaa8e92](https://github.com/cameronsjo/bosun/commit/aaa8e92a8411707b7bdc048a56879340be96cc2c))
* **mascot:** add bosun mascot with transparent PNG pipeline ([7e489f6](https://github.com/cameronsjo/bosun/commit/7e489f66086415fadaadc1b940d273e10763cca2))
* **mayday:** add rollback snapshots ([5b54cc2](https://github.com/cameronsjo/bosun/commit/5b54cc250e38e6afc18dc6876b0352da3314f023))
* **openspec:** formalize spec review workflow with Stage 1.5 gate ([9f1b827](https://github.com/cameronsjo/bosun/commit/9f1b827dd72714c8258a5d851e6542b467ce47ff))
* **openspec:** formalize spec review workflow with Stage 1.5 gate ([d7009a1](https://github.com/cameronsjo/bosun/commit/d7009a117da72ef18bf7a9b756f455521ecc35de))
* **provision:** add project_name to compose output for container namespacing ([5772529](https://github.com/cameronsjo/bosun/commit/57725291ad1f230079ea70fafdba8d65da50e20b))
* **provision:** add values overlays for env-specific config ([e07c238](https://github.com/cameronsjo/bosun/commit/e07c238f2a20d29bcec52bc6926a463ba34e11c8))
* rebrand to bosun with Below Deck nautical theme ([3672125](https://github.com/cameronsjo/bosun/commit/3672125f66c997be1aafaa103243dacac503abd1))
* **reconcile:** add alert throttling with exponential backoff ([f45498b](https://github.com/cameronsjo/bosun/commit/f45498be23bc02aa9baf55b8177971eb9782bcf5))
* **reconcile:** add content-hash file sync to skip unchanged writes ([6577383](https://github.com/cameronsjo/bosun/commit/6577383fa2e0dc91d1fe630496218f5d75da001d))
* **reconcile:** add deploy_paths allowlist for path-aware deploy skipping ([03191a8](https://github.com/cameronsjo/bosun/commit/03191a8319e6fc7617baf7196b75c804669fef76)), closes [#56](https://github.com/cameronsjo/bosun/issues/56)
* **reconcile:** add openspec proposal for declared-vs-actual state feedback loop ([3173f28](https://github.com/cameronsjo/bosun/commit/3173f283eff3e910bf80593eaec32a783b30732e))
* **reconcile:** add post-deploy health verification with polling ([16a434f](https://github.com/cameronsjo/bosun/commit/16a434f2d6f50918424c3a4cd4d111df1888c5d3))
* **reconcile:** add post-sync container restart hooks ([b71f494](https://github.com/cameronsjo/bosun/commit/b71f494f32e8847768fb60b855417adcaa0c4fe6))
* **reconcile:** add post-sync hook delay controls ([002fb99](https://github.com/cameronsjo/bosun/commit/002fb99a30b39f6cf1068c95b890798926a9335d))
* **reconcile:** add post-sync hook delay controls ([7c51f75](https://github.com/cameronsjo/bosun/commit/7c51f759b614e37599399e20d77b540a083e9aac))
* **reconcile:** add restart circuit breaker ([b959fc3](https://github.com/cameronsjo/bosun/commit/b959fc30cc2a748be34c8df3904aac4ddc0db7ae))
* **reconcile:** add restart circuit breaker to detect crash-looping containers ([36d3ac4](https://github.com/cameronsjo/bosun/commit/36d3ac4ae302a04f30bd5b669ac83258f7874efd))
* **reconcile:** add state-based deploy tracking and circuit breaker ([da1f923](https://github.com/cameronsjo/bosun/commit/da1f9236509b90baa03e526ce7eb9c4ae15e339c))
* **reconcile:** add state-based deploy tracking and circuit breaker ([bf34cf2](https://github.com/cameronsjo/bosun/commit/bf34cf2519cee5d67b304eb852b1d80ac8f6d77f))
* **reconcile:** add state-based deploy tracking with circuit breaker ([#27](https://github.com/cameronsjo/bosun/issues/27)) ([da1f923](https://github.com/cameronsjo/bosun/commit/da1f9236509b90baa03e526ce7eb9c4ae15e339c))
* **reconcile:** declared-vs-actual state feedback loop with drift detection ([d11b07d](https://github.com/cameronsjo/bosun/commit/d11b07d79ec3f4160858a218f5491539d39f2e41))
* **reconcile:** implement declared-vs-actual state feedback loop ([2346c87](https://github.com/cameronsjo/bosun/commit/2346c8767578cadd5212e10f293cc91900224806))
* **release:** add cosign signing, SLSA attestation, and install script ([62c5da6](https://github.com/cameronsjo/bosun/commit/62c5da61f0ae97826fb3da2fd56dc33014a6442f))
* **release:** add Docker image build to goreleaser ([2dd0297](https://github.com/cameronsjo/bosun/commit/2dd02974c86fda14e699d70484d64c196b520b12))
* remove external CLI dependencies, add schema versioning ([a248732](https://github.com/cameronsjo/bosun/commit/a2487329cf264594936e09e1a6fe96491f0fcc8d))
* **retry:** add retry utility with exponential backoff and jitter ([cf45845](https://github.com/cameronsjo/bosun/commit/cf45845722cfd9b019a7eac47893cd45fb228d12))
* **retry:** add retry utility with exponential backoff and jitter ([f5c033a](https://github.com/cameronsjo/bosun/commit/f5c033a35ae6490add73d0f9fa14d2b2f11583df))
* **sentry:** add opt-in error tracking and performance monitoring ([dd4d6f5](https://github.com/cameronsjo/bosun/commit/dd4d6f5901eaf15a2e62ff94a7ba7dc8aa386482))
* **sentry:** add opt-in error tracking and performance monitoring ([a04087f](https://github.com/cameronsjo/bosun/commit/a04087f4358d917157f01ee854e73c6463b271ca))
* **traefik:** add batteries-included security defaults (Phase 1) ([999d25d](https://github.com/cameronsjo/bosun/commit/999d25d45cf5e5803b415c05451061ffb878c669))
* **traefik:** add upgrade command and doctor diagnostics (Phase 2) ([e9b1e01](https://github.com/cameronsjo/bosun/commit/e9b1e0140aecc44e8cfb755505f87a804ba60cc7))
* **traefik:** batteries-included security defaults ([bec448c](https://github.com/cameronsjo/bosun/commit/bec448c26f02c06e363b2341ccf908d8b727b10f))
* **tunnel:** add structured logging to cloudflare and tailscale providers ([bcc1724](https://github.com/cameronsjo/bosun/commit/bcc1724a937efd5051453d9cd1b7a824da102a9c))
* **webui:** add React dashboard with maritime theme ([4a1348a](https://github.com/cameronsjo/bosun/commit/4a1348a137b9a929a6111e909823b82302f840c1))
* **webui:** add React dashboard with maritime theme ([60d973b](https://github.com/cameronsjo/bosun/commit/60d973b9d636f18dadb770a6f9eb825aa26f9f4d))


### Bug Fixes

* add missing language tag to fenced code block in commands.md ([eaafd88](https://github.com/cameronsjo/bosun/commit/eaafd8838ee39b3a061c07ef08502282e4c2b84e))
* add text language specifier to remaining code blocks in gitops comparison (MD040) ([d5ac933](https://github.com/cameronsjo/bosun/commit/d5ac9335b81bd2ea135149728d12c1058c69b9f5))
* address CodeRabbit follow-up comments from PR [#42](https://github.com/cameronsjo/bosun/issues/42) round 2 ([0694912](https://github.com/cameronsjo/bosun/commit/0694912a386de9b7036cb8db4e18b63f81b44cc5))
* address CodeRabbit PR [#42](https://github.com/cameronsjo/bosun/issues/42) review — 16 items across docs, specs, and scripts ([9e8e6ef](https://github.com/cameronsjo/bosun/commit/9e8e6ef6250bad854f241f71b23c5b3843470b37))
* address CodeRabbit review and CI lint failures ([774f98b](https://github.com/cameronsjo/bosun/commit/774f98bc4032225d35ae43db6dea21cd0d5618fc))
* address CodeRabbit review feedback ([03ae721](https://github.com/cameronsjo/bosun/commit/03ae721edebb2e87886d1347998f622436b931b8))
* address CodeRabbit review round 8 findings ([f1984fe](https://github.com/cameronsjo/bosun/commit/f1984fedb9177cbf76eca9aa306203bc831dd5b4))
* address CodeRabbit round 2 review feedback ([307c4b9](https://github.com/cameronsjo/bosun/commit/307c4b972fde77cd3b2292ab9bedc8d2dc8150a2))
* address CodeRabbit round 3 review feedback ([e7a57db](https://github.com/cameronsjo/bosun/commit/e7a57db548531a758204eaed4b66f4220332efa4))
* address critical and high severity production issues ([b84a025](https://github.com/cameronsjo/bosun/commit/b84a025a9ab3386d562578248a597b33e41dbc17))
* address critical edge cases from security analysis ([5926c4f](https://github.com/cameronsjo/bosun/commit/5926c4f876aba2cb1ba4f808e305f5fb4cc01785))
* address low-priority edge cases and improve UX ([a99a8a9](https://github.com/cameronsjo/bosun/commit/a99a8a977759d0abd2fb839191f4f7d33bf14543))
* address medium-priority edge cases and add preflight checks ([63d4fe8](https://github.com/cameronsjo/bosun/commit/63d4fe8f401ccf455235b0a4f24cdc6be739b9b2))
* address remaining high-priority edge cases ([a05f483](https://github.com/cameronsjo/bosun/commit/a05f483cd2337dedca1e242d3c7a4f484fbcd313))
* **alert:** address CodeRabbit findings on Slack provider ([8b1f5c3](https://github.com/cameronsjo/bosun/commit/8b1f5c3efc3cf2d1f5dc69d300b16053eeacb386))
* **alert:** address CodeRabbit findings on webhook provider ([fa75633](https://github.com/cameronsjo/bosun/commit/fa756331fe5c9c87a56fef71389f79bd197ec48b))
* **alert:** wire httptest server in severity skip test and move assertions out of handler goroutine ([fd38c96](https://github.com/cameronsjo/bosun/commit/fd38c9602259ba5e9a0336835ea0b1e7becf8ad2))
* **ci:** bootstrap release-please and increase lint timeout ([46ff5fc](https://github.com/cameronsjo/bosun/commit/46ff5fc1b620f8079b8455e8a88365c707438e49))
* **ci:** bump minor version on feat commits pre-1.0 ([0f4bcdd](https://github.com/cameronsjo/bosun/commit/0f4bcdd85ca9c6e9ae4cd9b198ebf53305eca5c3))
* **ci:** checkout release tag instead of HEAD for goreleaser ([d29549a](https://github.com/cameronsjo/bosun/commit/d29549af5b1d23bd11eeab94a29eb8eaeec507a7))
* **ci:** disable cosign signing in goreleaser ([fcdd747](https://github.com/cameronsjo/bosun/commit/fcdd7473af61ba8b2205324fdacad1cc8bcffed9))
* **ci:** disable docker builds in goreleaser ([a24a9bb](https://github.com/cameronsjo/bosun/commit/a24a9bbe5073f4f600cdec1bd3cbe4496d059eba))
* **ci:** disable SLSA attestation step ([e8c922c](https://github.com/cameronsjo/bosun/commit/e8c922cc157e09ef3a26a876085125f2e9d16fbd))
* **ci:** graceful fallback when release App token is not configured ([197f460](https://github.com/cameronsjo/bosun/commit/197f46019818f015dfb683a8c3775a5c7a75ae71))
* **ci:** increase max-turns and add timeout for code review ([5323f49](https://github.com/cameronsjo/bosun/commit/5323f49a303ad23a9156e15dc4797d9a0c670e73))
* **ci:** install git in test container ([c25332b](https://github.com/cameronsjo/bosun/commit/c25332b54bb4127f73ebaba90f819e2d835dd7df))
* **ci:** move fromJSON out of env block to prevent template error ([5831547](https://github.com/cameronsjo/bosun/commit/58315474a81b92a681f58fd9442fbae8d7372523))
* **ci:** remove -race flag from tests (requires CGO) ([6ba452c](https://github.com/cameronsjo/bosun/commit/6ba452c18c25c92667da08b7aae261f2cb3b3c31))
* **ci:** remove goreleaser prefix from exec commands ([02f0e00](https://github.com/cameronsjo/bosun/commit/02f0e00c581dfbbdf0354b553c88c7e5a084a5f2))
* **ci:** rename CIAll to All for proper CLI naming ([5593ee8](https://github.com/cameronsjo/bosun/commit/5593ee8587f4509b768a7e37c5ae5618eb8adb37))
* **ci:** rename Platform to buildTarget to avoid Dagger conflict ([9e43a40](https://github.com/cameronsjo/bosun/commit/9e43a40a33ddf98e8320173b87bae93f574adc38))
* **ci:** upgrade Claude workflow permissions to write for PRs and issues ([0b7a6ec](https://github.com/cameronsjo/bosun/commit/0b7a6ecb89f6392a22f79787be71263836d0b9c3))
* **ci:** use correct dagger-for-github action ([a1c481b](https://github.com/cameronsjo/bosun/commit/a1c481b54015da8010a71cab3ca8b73be6c3cd11))
* **ci:** use full semver tag for dagger-for-github action ([6dedd29](https://github.com/cameronsjo/bosun/commit/6dedd29440310f129af8082ef7e746387dcf15eb))
* **ci:** use GitHub App token for release PR auto-merge ([3cee031](https://github.com/cameronsjo/bosun/commit/3cee031bfdb4d9a4aba07011d66b1b26dc51f1a5))
* **ci:** use goreleaser:latest instead of non-existent v2 tag ([50d5ea7](https://github.com/cameronsjo/bosun/commit/50d5ea70ba8050dd5a5a2ff5aa7cff08263d6179))
* **config:** change InfraSubDir default from 'infrastructure' to '.' ([7759a86](https://github.com/cameronsjo/bosun/commit/7759a86139413a8343f6b9da2ffddfb2bc48c278))
* **daemon,tunnel:** neutralize audit log message and redact cloudflared stderr ([83c242f](https://github.com/cameronsjo/bosun/commit/83c242f9a51d6b83f77e316c5725161766603a73))
* **daemon:** add panic recovery to background goroutines ([306ad06](https://github.com/cameronsjo/bosun/commit/306ad064d336d6453f09c71ea7af433d5b63d32e))
* **daemon:** inject mock Docker client in health subsystem tests for CI ([d775cab](https://github.com/cameronsjo/bosun/commit/d775cabdde0c0f8b05e962e646679c83df665e0c))
* **daemon:** load post_sync_hooks from bosun.yaml in ConfigFromEnv ([2e1a952](https://github.com/cameronsjo/bosun/commit/2e1a952df6f499d61e6df93d1beba7ae4ae2ec6b)), closes [#49](https://github.com/cameronsjo/bosun/issues/49)
* **daemon:** panic recovery and template path validation ([cc4d378](https://github.com/cameronsjo/bosun/commit/cc4d37800195622c1fb1f387c856833963992694))
* **daemon:** use live Docker ping for health subsystem check ([71ca556](https://github.com/cameronsjo/bosun/commit/71ca5568b61194ddea71fc13e85a7e7804a60b2c))
* **daemon:** validate non-positive duration env vars for timeouts ([f288e76](https://github.com/cameronsjo/bosun/commit/f288e76a058536a4191f3d64324f336ff6932070))
* **deps:** bump go.opentelemetry.io/otel/sdk to v1.40.0 ([#103](https://github.com/cameronsjo/bosun/issues/103)) ([d5bb4a0](https://github.com/cameronsjo/bosun/commit/d5bb4a05fef822c6ff33e368099a44bc7d5bd83d))
* **deps:** run go mod tidy for prometheus dependency ([d441093](https://github.com/cameronsjo/bosun/commit/d441093172477c29f3c06082eff8f3ed77fd6baf))
* **deps:** upgrade go-git and edwards25519 to patch security vulnerabilities ([6b785c5](https://github.com/cameronsjo/bosun/commit/6b785c5dff8e6a87dd4826efac1caa45df7b8e1f))
* **docker:** add project name to compose commands to prevent orphan containers ([242c57c](https://github.com/cameronsjo/bosun/commit/242c57cceaece9a0c373fe43a9abf0cf33ec0a29))
* **docker:** create /var/run/bosun directory in container image ([4f7a8fb](https://github.com/cameronsjo/bosun/commit/4f7a8fb3ba800ebb45977e17c907a746e103250f))
* **docker:** simplify Dockerfile to use bosun daemon directly ([5f291ed](https://github.com/cameronsjo/bosun/commit/5f291ed97d0776d87f9c6040713f8ec283a9b322))
* **drift:** address review findings from PR [#29](https://github.com/cameronsjo/bosun/issues/29) ([a409723](https://github.com/cameronsjo/bosun/commit/a409723434d993761eee64e4f44b833a73975015))
* **drift:** include container names in drift check log entries ([c94a671](https://github.com/cameronsjo/bosun/commit/c94a6712a57d6d5f32aa610afb04d775a20c831b)), closes [#61](https://github.com/cameronsjo/bosun/issues/61)
* **lint:** add nolint directives for deprecated Docker SDK types ([ff1dc4e](https://github.com/cameronsjo/bosun/commit/ff1dc4e740b6a3a07609f91540f6e606183dfb39))
* **lint:** fix all remaining errcheck issues in webhook.go ([bf8f3f7](https://github.com/cameronsjo/bosun/commit/bf8f3f7c4ea3ec8cd9e35a452eeaea2814b7e66d))
* **lint:** fix remaining cmd.Help errcheck issues ([3a0b94d](https://github.com/cameronsjo/bosun/commit/3a0b94dae9ab304954ba092b701fb81af315f91a))
* **lint:** remove unused completeProvisionNames function ([e5e27e6](https://github.com/cameronsjo/bosun/commit/e5e27e6f6daa7d1ee51e199687c71a295e1e7f86))
* **lint:** resolve all remaining errcheck issues ([4c618df](https://github.com/cameronsjo/bosun/commit/4c618dfc98877de1035f8ff61bd6f7902cee6119))
* **lint:** resolve errcheck and unused function lint errors ([7fc9e37](https://github.com/cameronsjo/bosun/commit/7fc9e373912dabeb613479c0a94859c1efeeaf0d))
* **lint:** resolve errcheck in drift command and daemon startup ([a1bbe25](https://github.com/cameronsjo/bosun/commit/a1bbe25068d86263c5915c5d77421013657298fb))
* **lint:** resolve errcheck issues in daemon package ([28b0f7b](https://github.com/cameronsjo/bosun/commit/28b0f7b6bd3295e4fd79fe6fb7ba0189260dbfab))
* **lint:** resolve errcheck violations and add local lint targets ([7c61406](https://github.com/cameronsjo/bosun/commit/7c614064aa4288a9876be81a482460378f92903d))
* **lint:** resolve golangci-lint issues ([6d2f03b](https://github.com/cameronsjo/bosun/commit/6d2f03b696dc2c52231b88ee87ede049ae423ab5))
* **lint:** resolve remaining errcheck issues ([87507d4](https://github.com/cameronsjo/bosun/commit/87507d45555ad2ef1f0ea6f56507a0b8aa97d9a3))
* **lint:** resolve remaining errcheck issues ([a5bc3a2](https://github.com/cameronsjo/bosun/commit/a5bc3a275cb14b0a897563fcbc7d6ca5385f1f07))
* **lock:** implement proper Windows file locking with LockFileEx ([a21df66](https://github.com/cameronsjo/bosun/commit/a21df6684bf0ba407deafeef2608c878ecb19b92))
* **log:** address CodeRabbit review feedback ([18af41b](https://github.com/cameronsjo/bosun/commit/18af41b89ae1634cb6a18879d148a10d1789c385))
* **log:** address CodeRabbit round 2 review feedback ([0f378a8](https://github.com/cameronsjo/bosun/commit/0f378a80ab6dfa02910b6c859bf85f09bf5af538))
* **log:** complete ComponentCtx migration for remaining call sites ([6e8d510](https://github.com/cameronsjo/bosun/commit/6e8d51076e1a7bdfdf72f818ef5b092f99c03d49))
* **logging:** add structured logging to remote deploy ops and replace ui.* in daemon paths ([27d492f](https://github.com/cameronsjo/bosun/commit/27d492f09bb275f0dc7f14ae62c171e6e3f811f6))
* **logging:** address CodeRabbit review feedback across 13 files ([cc03511](https://github.com/cameronsjo/bosun/commit/cc035118a8bfcf737c79cc8f2f0a25efe8832896))
* **logging:** address CodeRabbit review round 2 ([c8bef3d](https://github.com/cameronsjo/bosun/commit/c8bef3dc8bf8ba4b3dadd123ccdc7bf0eb71461e))
* **logging:** replace ui.* with structured logging in daemon audit middleware and add retry logging ([fc3565f](https://github.com/cameronsjo/bosun/commit/fc3565f7451f03c5f340b9d8525acdbd40f17b08))
* **log:** use FromContext to avoid zerolog key duplication ([170915e](https://github.com/cameronsjo/bosun/commit/170915e45a7e1fcece598355c5d4a9e0d3c572db))
* **observability:** add structured logging to drift detection pipeline ([f0bbdca](https://github.com/cameronsjo/bosun/commit/f0bbdcaf14fb0dce67d3c3ef0adfc4fa01b010d5))
* **openspec:** use SHALL in Drift Alerts requirement description ([6f969d7](https://github.com/cameronsjo/bosun/commit/6f969d7b0860f8b2d127bfd312ac242a7cc7417e))
* **reconcile:** address CodeRabbit findings on atomic deploy ([c8c8e05](https://github.com/cameronsjo/bosun/commit/c8c8e05d570c403c2a8e7f3267b1f5384aac6f64))
* **reconcile:** address CodeRabbit review feedback ([1469ed9](https://github.com/cameronsjo/bosun/commit/1469ed9a0cbef90ed78e609b3d27eecac0efd194))
* **reconcile:** address CodeRabbit review findings for restart breaker ([9321408](https://github.com/cameronsjo/bosun/commit/93214081eb2d8b736751f1ea8b99f70cfafce9c1))
* **reconcile:** alert on all pipeline failure stages with on_failure/on_success gates ([#95](https://github.com/cameronsjo/bosun/issues/95)) ([3974504](https://github.com/cameronsjo/bosun/commit/39745046a602aa2a6fc9c2b1cc70c32d4bf01483))
* **reconcile:** classify compose failures to tolerate unhealthy containers ([#92](https://github.com/cameronsjo/bosun/issues/92)) ([d73332e](https://github.com/cameronsjo/bosun/commit/d73332edceae9141ae257f5fcd2e4018f0062c34))
* **reconcile:** deploy all compose files, not just core.yml ([98d470e](https://github.com/cameronsjo/bosun/commit/98d470e1846f00b00b6d5a6c55860ae88b5e0d98))
* **reconcile:** fire post-sync hooks when DiffFiles fails on shallow repo ([a5757eb](https://github.com/cameronsjo/bosun/commit/a5757ebe86e6fbc3f8ad6c4e86fe7822eddc0074)), closes [#55](https://github.com/cameronsjo/bosun/issues/55)
* **reconcile:** make compose up timeout configurable via BOSUN_COMPOSE_UP_TIMEOUT ([b3e0b8a](https://github.com/cameronsjo/bosun/commit/b3e0b8a7906efe4e7b00aa7b875c1be3ef5d652f)), closes [#83](https://github.com/cameronsjo/bosun/issues/83)
* **reconcile:** override go-git DefaultAuthBuilder for SSH without agent ([4b0b831](https://github.com/cameronsjo/bosun/commit/4b0b83192ed92d74c672fe2bbdd116e263748ff7))
* **reconcile:** post-sync hooks regression + deploy_paths allowlist ([fa922da](https://github.com/cameronsjo/bosun/commit/fa922dabb4e7186b12b92bd05bbd442d87ced364))
* **reconcile:** reload bosun.yaml from repo after git pull ([2e55bc2](https://github.com/cameronsjo/bosun/commit/2e55bc268f78dabf59ea3c559148c3a78a55ef42))
* **reconcile:** remove --wait from compose up, add unhealthy container alerts ([91bd3bb](https://github.com/cameronsjo/bosun/commit/91bd3bb8798d8e976e436f9fd0f8482c1ac41c70))
* **reconcile:** skip permission test when running as root ([7aac83b](https://github.com/cameronsjo/bosun/commit/7aac83b2fc5dd488caeab00a72e2fc4230acf971))
* **reconcile:** use atomic rename-aside pattern in DeployLocal ([ebc918c](https://github.com/cameronsjo/bosun/commit/ebc918cfdbb42cb4ea1751649bdec6dde5b56c29))
* **reconcile:** use atomic rename-aside pattern in DeployLocal ([e2dcf44](https://github.com/cameronsjo/bosun/commit/e2dcf444f33fbabd2836215c7acd845b5da6ea26))
* **reconcile:** use known_hosts for SSH host key verification ([f41eecd](https://github.com/cameronsjo/bosun/commit/f41eecd718bcc56d186c2826e8c229bc32b7a97f))
* **reconcile:** validate paths in template include and fromJsonFile ([50da42f](https://github.com/cameronsjo/bosun/commit/50da42f9f151f8296f727bfdc984e1eab0471115))
* **reconcile:** warn on dirty repo instead of blocking pull ([c82e91c](https://github.com/cameronsjo/bosun/commit/c82e91cc6217ab4fcbe96afe20622d3025b9505d))
* **release:** fix goreleaser config and lint issues ([477102c](https://github.com/cameronsjo/bosun/commit/477102ccb5f810c4d6cd6efeb9ff4be0b751b251))
* resolve 11 bugs and code quality issues across codebase ([4c714a4](https://github.com/cameronsjo/bosun/commit/4c714a476bcc3f5b34bf9cc3ff6e39c2206a4ecd))
* resolve 3 low-hanging issues from codebase investigation ([e3cfedd](https://github.com/cameronsjo/bosun/commit/e3cfedd7e74cec0351b754cbe7dcff2e57db6960))
* resolve 3 low-hanging issues from codebase investigation ([06c2d31](https://github.com/cameronsjo/bosun/commit/06c2d31dc8023581320dfbc2be7c3ec04dde325c))
* resolve 6 just-do-it issues from backlog ([022c408](https://github.com/cameronsjo/bosun/commit/022c4083cbebf849c32d334f17e01de898e998c0))
* **retry:** address CodeRabbit findings on retry utility ([b5fef4f](https://github.com/cameronsjo/bosun/commit/b5fef4f43594c37242a68b9e55d78e580e8af0eb))
* **scripts:** harden diagram renderer per CodeRabbit review ([afe34f2](https://github.com/cameronsjo/bosun/commit/afe34f2470f4150061937ecfc7e64395baf99d1b))
* **sentry:** remove redundant io.Writer type assertion ([fca288a](https://github.com/cameronsjo/bosun/commit/fca288a839e21274cc46222af215392bab175d02))


### Performance Improvements

* **ci:** parallelize test, lint, and webui in Dagger pipeline ([26aaaa5](https://github.com/cameronsjo/bosun/commit/26aaaa5ae21e0b4636359c49b3beaa48312d61fd))

## [0.23.0](https://github.com/cameronsjo/bosun/compare/v0.22.0...v0.23.0) (2026-03-13)


### Features

* **daemon:** add subsystem breakdown to health endpoint ([f4e3aa5](https://github.com/cameronsjo/bosun/commit/f4e3aa597639e47f03a0d82d36bdf007523a8bbd))


### Bug Fixes

* **daemon:** panic recovery and template path validation ([cc4d378](https://github.com/cameronsjo/bosun/commit/cc4d37800195622c1fb1f387c856833963992694))

## [0.22.0](https://github.com/cameronsjo/bosun/compare/v0.21.0...v0.22.0) (2026-03-13)


### Features

* **alert:** add generic webhook alert provider ([2cd2ca7](https://github.com/cameronsjo/bosun/commit/2cd2ca7e88ea889b8402d48ceda99cbb46cf9028))
* **alert:** add Slack webhook alert provider ([f2f2e73](https://github.com/cameronsjo/bosun/commit/f2f2e7303905e3c030bab41fb0d8a268b1df0e17))
* **openspec:** formalize spec review workflow with Stage 1.5 gate ([9f1b827](https://github.com/cameronsjo/bosun/commit/9f1b827dd72714c8258a5d851e6542b467ce47ff))
* **retry:** add retry utility with exponential backoff and jitter ([cf45845](https://github.com/cameronsjo/bosun/commit/cf45845722cfd9b019a7eac47893cd45fb228d12))

## [0.21.0](https://github.com/cameronsjo/bosun/compare/v0.20.0...v0.21.0) (2026-03-13)


### Features

* **daemon:** replace hand-rolled metrics with Prometheus ([2cfcf14](https://github.com/cameronsjo/bosun/commit/2cfcf14efb048b826b7474f2009635c9f21f45f8))


### Bug Fixes

* **reconcile:** address CodeRabbit findings on atomic deploy ([c8c8e05](https://github.com/cameronsjo/bosun/commit/c8c8e05d570c403c2a8e7f3267b1f5384aac6f64))
* **reconcile:** use atomic rename-aside pattern in DeployLocal ([ebc918c](https://github.com/cameronsjo/bosun/commit/ebc918cfdbb42cb4ea1751649bdec6dde5b56c29))

## [0.20.0](https://github.com/cameronsjo/bosun/compare/v0.19.0...v0.20.0) (2026-03-13)


### Features

* **reconcile:** add restart circuit breaker ([b959fc3](https://github.com/cameronsjo/bosun/commit/b959fc30cc2a748be34c8df3904aac4ddc0db7ae))
* **reconcile:** add restart circuit breaker to detect crash-looping containers ([36d3ac4](https://github.com/cameronsjo/bosun/commit/36d3ac4ae302a04f30bd5b669ac83258f7874efd))


### Bug Fixes

* **reconcile:** address CodeRabbit review findings for restart breaker ([9321408](https://github.com/cameronsjo/bosun/commit/93214081eb2d8b736751f1ea8b99f70cfafce9c1))

## [0.19.0](https://github.com/cameronsjo/bosun/compare/v0.18.1...v0.19.0) (2026-03-13)


### Features

* **reconcile:** add post-deploy health verification with polling ([16a434f](https://github.com/cameronsjo/bosun/commit/16a434f2d6f50918424c3a4cd4d111df1888c5d3))


### Bug Fixes

* **daemon:** validate non-positive duration env vars for timeouts ([f288e76](https://github.com/cameronsjo/bosun/commit/f288e76a058536a4191f3d64324f336ff6932070))
* **reconcile:** make compose up timeout configurable via BOSUN_COMPOSE_UP_TIMEOUT ([b3e0b8a](https://github.com/cameronsjo/bosun/commit/b3e0b8a7906efe4e7b00aa7b875c1be3ef5d652f)), closes [#83](https://github.com/cameronsjo/bosun/issues/83)

## [0.18.1](https://github.com/cameronsjo/bosun/compare/v0.18.0...v0.18.1) (2026-03-08)


### Bug Fixes

* **reconcile:** alert on all pipeline failure stages with on_failure/on_success gates ([#95](https://github.com/cameronsjo/bosun/issues/95)) ([3974504](https://github.com/cameronsjo/bosun/commit/39745046a602aa2a6fc9c2b1cc70c32d4bf01483))

## [0.18.0](https://github.com/cameronsjo/bosun/compare/v0.17.0...v0.18.0) (2026-03-08)


### Features

* **alert:** add drift alert debounce to suppress transient flaps ([#94](https://github.com/cameronsjo/bosun/issues/94)) ([8dc2d9b](https://github.com/cameronsjo/bosun/commit/8dc2d9b85850eb279d7381bcd339a80b5e42a0f4))


### Bug Fixes

* **reconcile:** classify compose failures to tolerate unhealthy containers ([#92](https://github.com/cameronsjo/bosun/issues/92)) ([d73332e](https://github.com/cameronsjo/bosun/commit/d73332edceae9141ae257f5fcd2e4018f0062c34))

## [0.17.0](https://github.com/cameronsjo/bosun/compare/v0.16.2...v0.17.0) (2026-03-07)


### Features

* **config:** add configurable orphan container cleanup ([#93](https://github.com/cameronsjo/bosun/issues/93)) ([aa90339](https://github.com/cameronsjo/bosun/commit/aa903397397a08c75a004f19b564f14646e3b478))

## [0.16.2](https://github.com/cameronsjo/bosun/compare/v0.16.1...v0.16.2) (2026-03-07)


### Bug Fixes

* **deps:** bump go.opentelemetry.io/otel/sdk to v1.40.0 ([#103](https://github.com/cameronsjo/bosun/issues/103)) ([d5bb4a0](https://github.com/cameronsjo/bosun/commit/d5bb4a05fef822c6ff33e368099a44bc7d5bd83d))

## [0.16.1](https://github.com/cameronsjo/bosun/compare/v0.16.0...v0.16.1) (2026-03-02)


### Bug Fixes

* address CodeRabbit review and CI lint failures ([774f98b](https://github.com/cameronsjo/bosun/commit/774f98bc4032225d35ae43db6dea21cd0d5618fc))
* address CodeRabbit review round 8 findings ([f1984fe](https://github.com/cameronsjo/bosun/commit/f1984fedb9177cbf76eca9aa306203bc831dd5b4))
* **alert:** wire httptest server in severity skip test and move assertions out of handler goroutine ([fd38c96](https://github.com/cameronsjo/bosun/commit/fd38c9602259ba5e9a0336835ea0b1e7becf8ad2))

## [0.16.0](https://github.com/cameronsjo/bosun/compare/v0.15.1...v0.16.0) (2026-02-28)


### Features

* **init:** add domain prompt and Traefik config generation ([b1fe645](https://github.com/cameronsjo/bosun/commit/b1fe6452bd5baeef19dc2a461ace10974ad11583))
* **traefik:** add batteries-included security defaults (Phase 1) ([999d25d](https://github.com/cameronsjo/bosun/commit/999d25d45cf5e5803b415c05451061ffb878c669))
* **traefik:** add upgrade command and doctor diagnostics (Phase 2) ([e9b1e01](https://github.com/cameronsjo/bosun/commit/e9b1e0140aecc44e8cfb755505f87a804ba60cc7))
* **traefik:** batteries-included security defaults ([bec448c](https://github.com/cameronsjo/bosun/commit/bec448c26f02c06e363b2341ccf908d8b727b10f))


### Bug Fixes

* address CodeRabbit review feedback ([03ae721](https://github.com/cameronsjo/bosun/commit/03ae721edebb2e87886d1347998f622436b931b8))
* address CodeRabbit round 2 review feedback ([307c4b9](https://github.com/cameronsjo/bosun/commit/307c4b972fde77cd3b2292ab9bedc8d2dc8150a2))
* address CodeRabbit round 3 review feedback ([e7a57db](https://github.com/cameronsjo/bosun/commit/e7a57db548531a758204eaed4b66f4220332efa4))

## [0.15.1](https://github.com/cameronsjo/bosun/compare/v0.15.0...v0.15.1) (2026-02-28)


### Bug Fixes

* **ci:** graceful fallback when release App token is not configured ([197f460](https://github.com/cameronsjo/bosun/commit/197f46019818f015dfb683a8c3775a5c7a75ae71))
* **ci:** use GitHub App token for release PR auto-merge ([3cee031](https://github.com/cameronsjo/bosun/commit/3cee031bfdb4d9a4aba07011d66b1b26dc51f1a5))
* **daemon,tunnel:** neutralize audit log message and redact cloudflared stderr ([83c242f](https://github.com/cameronsjo/bosun/commit/83c242f9a51d6b83f77e316c5725161766603a73))
* **log:** complete ComponentCtx migration for remaining call sites ([6e8d510](https://github.com/cameronsjo/bosun/commit/6e8d51076e1a7bdfdf72f818ef5b092f99c03d49))

## [0.15.0](https://github.com/cameronsjo/bosun/compare/v0.14.0...v0.15.0) (2026-02-25)


### Features

* **logging:** add structured logging across reconcile, daemon, and drift ([67b1a44](https://github.com/cameronsjo/bosun/commit/67b1a4445caae3ebf8cdf94caa1b673f32e9c851))

## [0.14.0](https://github.com/cameronsjo/bosun/compare/v0.13.0...v0.14.0) (2026-02-25)


### Features

* **daemon:** add structured logging to API handlers and TCP auth ([8ca9409](https://github.com/cameronsjo/bosun/commit/8ca940926112c1628664b05d9f1f088633251c44))
* **log:** add ComponentCtx for context-aware logger construction ([cacb01e](https://github.com/cameronsjo/bosun/commit/cacb01e2fa9489f41f6fde23d9c1782a5d7492de))
* **log:** adopt context-aware logger across codebase ([664f795](https://github.com/cameronsjo/bosun/commit/664f79538de70019415dfbd33b70c2d70ec61eb1))
* **log:** adopt context-aware logger across codebase ([#63](https://github.com/cameronsjo/bosun/issues/63)) ([664f795](https://github.com/cameronsjo/bosun/commit/664f79538de70019415dfbd33b70c2d70ec61eb1))
* **log:** enrich context at pipeline entry points and migrate sub-operations ([832fa8c](https://github.com/cameronsjo/bosun/commit/832fa8c1c4adee155d04a5fc5dcaebea77860aa7))
* **log:** migrate docker client and add retry logging ([98d2200](https://github.com/cameronsjo/bosun/commit/98d220059489932743d2c949f324835fb3c7a732))
* **tunnel:** add structured logging to cloudflare and tailscale providers ([bcc1724](https://github.com/cameronsjo/bosun/commit/bcc1724a937efd5051453d9cd1b7a824da102a9c))


### Bug Fixes

* **log:** address CodeRabbit review feedback ([18af41b](https://github.com/cameronsjo/bosun/commit/18af41b89ae1634cb6a18879d148a10d1789c385))
* **log:** address CodeRabbit round 2 review feedback ([0f378a8](https://github.com/cameronsjo/bosun/commit/0f378a80ab6dfa02910b6c859bf85f09bf5af538))
* **log:** use FromContext to avoid zerolog key duplication ([170915e](https://github.com/cameronsjo/bosun/commit/170915e45a7e1fcece598355c5d4a9e0d3c572db))

## [0.13.0](https://github.com/cameronsjo/bosun/compare/v0.12.1...v0.13.0) (2026-02-23)


### Features

* **reconcile:** add deploy_paths allowlist for path-aware deploy skipping ([03191a8](https://github.com/cameronsjo/bosun/commit/03191a8319e6fc7617baf7196b75c804669fef76)), closes [#56](https://github.com/cameronsjo/bosun/issues/56)


### Bug Fixes

* **reconcile:** address CodeRabbit review feedback ([1469ed9](https://github.com/cameronsjo/bosun/commit/1469ed9a0cbef90ed78e609b3d27eecac0efd194))
* **reconcile:** fire post-sync hooks when DiffFiles fails on shallow repo ([a5757eb](https://github.com/cameronsjo/bosun/commit/a5757ebe86e6fbc3f8ad6c4e86fe7822eddc0074)), closes [#55](https://github.com/cameronsjo/bosun/issues/55)
* **reconcile:** post-sync hooks regression + deploy_paths allowlist ([fa922da](https://github.com/cameronsjo/bosun/commit/fa922dabb4e7186b12b92bd05bbd442d87ced364))

## [0.12.1](https://github.com/cameronsjo/bosun/compare/v0.12.0...v0.12.1) (2026-02-23)


### Bug Fixes

* **reconcile:** reload bosun.yaml from repo after git pull ([2e55bc2](https://github.com/cameronsjo/bosun/commit/2e55bc268f78dabf59ea3c559148c3a78a55ef42))

## [0.12.0](https://github.com/cameronsjo/bosun/compare/v0.11.0...v0.12.0) (2026-02-23)


### Features

* **reconcile:** add content-hash file sync to skip unchanged writes ([6577383](https://github.com/cameronsjo/bosun/commit/6577383fa2e0dc91d1fe630496218f5d75da001d))

## [0.11.0](https://github.com/cameronsjo/bosun/compare/v0.10.0...v0.11.0) (2026-02-23)


### Features

* apply essentials scaffold ([e877efb](https://github.com/cameronsjo/bosun/commit/e877efb809c9925f50b02f81a36d1ccd149e1127))

## [0.10.0](https://github.com/cameronsjo/bosun/compare/v0.9.0...v0.10.0) (2026-02-23)


### Features

* **daemon:** add drift alert deduplication with per-item cooldown ([6efc0e6](https://github.com/cameronsjo/bosun/commit/6efc0e6b155adbcff4f38598aeb988bf3cf1ead7))

## [0.9.0](https://github.com/cameronsjo/bosun/compare/v0.8.0...v0.9.0) (2026-02-23)


### Features

* **mascot:** add bosun mascot with transparent PNG pipeline ([7e489f6](https://github.com/cameronsjo/bosun/commit/7e489f66086415fadaadc1b940d273e10763cca2))

## [0.8.0](https://github.com/cameronsjo/bosun/compare/v0.7.3...v0.8.0) (2026-02-22)


### Features

* **reconcile:** add post-sync hook delay controls ([002fb99](https://github.com/cameronsjo/bosun/commit/002fb99a30b39f6cf1068c95b890798926a9335d))
* **reconcile:** add post-sync hook delay controls ([7c51f75](https://github.com/cameronsjo/bosun/commit/7c51f759b614e37599399e20d77b540a083e9aac))

## [0.7.3](https://github.com/cameronsjo/bosun/compare/v0.7.2...v0.7.3) (2026-02-22)


### Bug Fixes

* **reconcile:** warn on dirty repo instead of blocking pull ([c82e91c](https://github.com/cameronsjo/bosun/commit/c82e91cc6217ab4fcbe96afe20622d3025b9505d))

## [0.7.2](https://github.com/cameronsjo/bosun/compare/v0.7.1...v0.7.2) (2026-02-22)


### Bug Fixes

* **daemon:** load post_sync_hooks from bosun.yaml in ConfigFromEnv ([2e1a952](https://github.com/cameronsjo/bosun/commit/2e1a952df6f499d61e6df93d1beba7ae4ae2ec6b)), closes [#49](https://github.com/cameronsjo/bosun/issues/49)

## [0.7.1](https://github.com/cameronsjo/bosun/compare/v0.7.0...v0.7.1) (2026-02-22)


### Bug Fixes

* **deps:** upgrade go-git and edwards25519 to patch security vulnerabilities ([6b785c5](https://github.com/cameronsjo/bosun/commit/6b785c5dff8e6a87dd4826efac1caa45df7b8e1f))

## [0.7.0](https://github.com/cameronsjo/bosun/compare/v0.6.1...v0.7.0) (2026-02-22)


### Features

* **config:** add post_sync_hooks to bosun.yaml config surface ([0396d60](https://github.com/cameronsjo/bosun/commit/0396d607abe2889c619a08b2932621c31611b9cd)), closes [#38](https://github.com/cameronsjo/bosun/issues/38)

## [0.6.1](https://github.com/cameronsjo/bosun/compare/v0.6.0...v0.6.1) (2026-02-22)


### Bug Fixes

* add text language specifier to remaining code blocks in gitops comparison (MD040) ([d5ac933](https://github.com/cameronsjo/bosun/commit/d5ac9335b81bd2ea135149728d12c1058c69b9f5))
* address CodeRabbit follow-up comments from PR [#42](https://github.com/cameronsjo/bosun/issues/42) round 2 ([0694912](https://github.com/cameronsjo/bosun/commit/0694912a386de9b7036cb8db4e18b63f81b44cc5))
* address CodeRabbit PR [#42](https://github.com/cameronsjo/bosun/issues/42) review — 16 items across docs, specs, and scripts ([9e8e6ef](https://github.com/cameronsjo/bosun/commit/9e8e6ef6250bad854f241f71b23c5b3843470b37))

## [0.6.0](https://github.com/cameronsjo/bosun/compare/v0.5.0...v0.6.0) (2026-02-22)


### Features

* add deploy resilience — alert throttling, --wait removal, post-sync hooks ([7b278d7](https://github.com/cameronsjo/bosun/commit/7b278d7c840fc4be6b4d95d5eaf92d1b20959bee))
* **reconcile:** add alert throttling with exponential backoff ([f45498b](https://github.com/cameronsjo/bosun/commit/f45498be23bc02aa9baf55b8177971eb9782bcf5))
* **reconcile:** add post-sync container restart hooks ([b71f494](https://github.com/cameronsjo/bosun/commit/b71f494f32e8847768fb60b855417adcaa0c4fe6))


### Bug Fixes

* **reconcile:** remove --wait from compose up, add unhealthy container alerts ([91bd3bb](https://github.com/cameronsjo/bosun/commit/91bd3bb8798d8e976e436f9fd0f8482c1ac41c70))

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
