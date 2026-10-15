## 1. SSH trust enforcement

- [x] 1.1 Make go-git host-key resolution return an error for absent or
  malformed configured candidates while preserving the explicit insecure
  opt-out.
- [x] 1.2 Propagate host-key errors through agent and key-file auth resolution
  and daemon startup validation.
- [x] 1.3 Replace deploy `accept-new` fallback with strict host-key checking
  while preserving configured-file pinning and OpenSSH default trust stores.

## 2. Regression coverage

- [x] 2.1 Cover absent and malformed Git `known_hosts` files, valid verification,
  and the explicit opt-out.
- [x] 2.2 Cover key-file and usable-agent auth with both strict trust and the
  opt-out, including connection cleanup on policy failure.
- [x] 2.3 Cover strict deploy command construction and all remote SSH legs using
  the shared command seam.
- [x] 2.4 Add daemon startup coverage proving a trust-policy error prevents
  listeners from binding.

## 3. Documentation and verification

- [x] 3.1 Update the reconcile spec delta and operator/onboard SSH guidance.
- [x] 3.2 Validate OpenSpec and format the changed Go files.
- [x] 3.3 Run the required gated targeted tests and report any resource-gate
  rejection without bypassing it.
- [x] 3.4 Stage a small break patch that removes the fail-closed controls and
  names the regression tests expected to fail.

