## 1. Implementation

- [ ] 1.1 Add the allowlisted Sprig function map (Requirement: Template Function Allowlist) and use it in reconcile and `bosun render`
- [ ] 1.2 Tests for Template Function Allowlist: `env`, `expandenv`, and `getHostByName` fail to parse; allowlisted functions render; the allowlist is pinned as a sorted golden list
- [ ] 1.3 Restore the renderer's include directory on every return path, including a source-root open failure, with a test
- [ ] 1.4 Rewrite `env "SOPS_SECRETS"` examples and update docs and the onboard skill
