# Change: Harden ingress authorization and output boundaries

## Why

A socket connector could read the webhook control credential from `/config` without peer authorization. The standalone receiver forwarded unsigned requests when no secret resolved. Untrusted source attribution and health output reached operator sinks.

## What Changes

- Apply peer authorization to socket `/config`; make credential inclusion explicit in response construction.
- Reject standalone receiver triggers without a secret unless the existing explicit opt-out is enabled.
- Strip controls before capping provider attribution, pushed refs, socket/TCP sources and container health output.

## Impact

- Part of #668; SSH, restart-breaker and filesystem/lock changes are separate units.
- Affected specs: daemon-security (overlaps pending add-daemon-security without changing its existing contracts).
- All consumers: internal/daemon/{api.go,api_test.go,socket.go,tcp.go,pure.go,server.go,git_auth_test.go,pure_test.go,socket_test.go,tcp_test.go,webhook_attribution_test.go}; internal/cmd/webhook.go; internal/reconcile/{drift.go,healthgate_test.go}; internal/log; docs/security.md, docs/gitops.md, docs/commands.md, README.md, llms.txt and skills/onboard/resources/{commands.md,gitops.md,configuration.md}.
