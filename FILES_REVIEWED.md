# Logging Audit - Files Reviewed

This file tracks progress of the logging audit across the bosun codebase.

## Status
- Total files: 69
- Files reviewed: 69
- Issues found: 26 files with logging issues

## Summary by Severity

| Severity | Count | Files |
|----------|-------|-------|
| High | 8 | daemon.go (cmd), emergency.go, reconcile.go (cmd), deploy.go, git.go, client.go (docker), compose.go, snapshot.go |
| Medium | 18 | alert.go, discord.go, sendgrid.go, twilio.go, alert.go (cmd), diagnostics.go, init.go, migrate.go, provision.go, render.go, webhook.go, config.go, api.go, server.go, socket.go, tcp.go, reconcile.go, sops.go, template.go (reconcile), chart.go, provision.go (manifest), render.go (manifest), template.go (manifest), fileutil.go, lock.go, cloudflare.go, tailscale.go, update.go, log.go |
| Low | 8 | comms.go, crew.go, root.go, status.go, daemon.go, peercred_linux.go, lock_unix.go, lock_windows.go, preflight.go, migrate.go (manifest), context.go, fields.go |
| None | 12 | completions.go, trigger.go, update.go (cmd), validate.go (cmd), yacht.go, client.go (daemon), peercred_other.go, doc.go (docker), interface.go (docker), interfaces.go (reconcile), validation.go, tunnel.go, color.go, doc.go (manifest), interpolate.go, merge.go, types.go, validate.go (manifest) |

## Files Reviewed

| File | Status | Severity | Issues |
|------|--------|----------|--------|
| internal/alert/alert.go | ✓ | Medium | No structured logging for alert dispatch |
| internal/alert/discord.go | ✓ | Medium | No beginning/success logging, silent early return |
| internal/alert/sendgrid.go | ✓ | Medium | No logging for email operations |
| internal/alert/twilio.go | ✓ | High | Partial failures lost silently |
| internal/cmd/alert.go | ✓ | Medium | External API calls lack story pattern |
| internal/cmd/comms.go | ✓ | Low | HTTP/tunnel calls lack beginning context |
| internal/cmd/completions.go | ✓ | None | Correct - completions should be silent |
| internal/cmd/crew.go | ✓ | Low | Docker API calls lack observability logs |
| internal/cmd/daemon.go | ✓ | High | Daemon startup has no structured logging |
| internal/cmd/diagnostics.go | ✓ | Medium | Docker and git operations need logs |
| internal/cmd/emergency.go | ✓ | High | Recovery operations need audit trail |
| internal/cmd/helpers.go | ✓ | Low | Thin helper, logging in callers |
| internal/cmd/init.go | ✓ | Medium | Age key generation needs security logging |
| internal/cmd/migrate.go | ✓ | Medium | Migrations change state without logs |
| internal/cmd/provision.go | ✓ | Medium | No operation lifecycle logging |
| internal/cmd/reconcile.go | ✓ | High | Critical GitOps workflow lacks structured logging |
| internal/cmd/render.go | ✓ | Medium | No template rendering lifecycle logging |
| internal/cmd/root.go | ✓ | Low | Missing startup context logging |
| internal/cmd/status.go | ✓ | Low | Read-only command, lower priority |
| internal/cmd/trigger.go | ✓ | Low | CLI command, acceptable |
| internal/cmd/update.go | ✓ | Low | CLI command, acceptable |
| internal/cmd/validate.go | ✓ | Low | CLI command, acceptable |
| internal/cmd/webhook.go | ✓ | Medium | HTTP server needs operational logging |
| internal/cmd/yacht.go | ✓ | Low | CLI commands, acceptable |
| internal/config/config.go | ✓ | Medium | Silent failures in config loading |
| internal/daemon/api.go | ✓ | Medium | No logging for API endpoints |
| internal/daemon/client.go | ✓ | None | Client library - caller logs |
| internal/daemon/daemon.go | ✓ | Low | Mix of ui.* and structured logging |
| internal/daemon/peercred_linux.go | ✓ | Low | Debug logs would help |
| internal/daemon/peercred_other.go | ✓ | None | No-op stub |
| internal/daemon/server.go | ✓ | Medium | Webhook security events not logged |
| internal/daemon/socket.go | ✓ | Medium | Uses ui.Info instead of structured |
| internal/daemon/tcp.go | ✓ | Medium | Uses ui.* instead of structured |
| internal/docker/client.go | ✓ | High | State mutations have zero logging |
| internal/docker/compose.go | ✓ | High | Core deployment ops have no logging |
| internal/docker/containers.go | ✓ | Medium | Container ops need visibility |
| internal/docker/doc.go | ✓ | None | Documentation only |
| internal/docker/interface.go | ✓ | None | Interface definitions |
| internal/fileutil/fileutil.go | ✓ | Medium | No logging for file operations |
| internal/lock/lock.go | ✓ | Medium | Lock operations not logged |
| internal/log/context.go | ✓ | Low | Minor gaps in context propagation |
| internal/log/fields.go | ✓ | Low | Missing retry/batch field constants |
| internal/log/log.go | ✓ | Medium | Unused Options.Output, thread safety |
| internal/manifest/chart.go | ✓ | Medium | Uses stdlib log instead of zerolog |
| internal/manifest/doc.go | ✓ | None | Documentation only |
| internal/manifest/interpolate.go | ✓ | None | Pure functions, caller logs |
| internal/manifest/merge.go | ✓ | None | Utility functions |
| internal/manifest/migrate.go | ✓ | Low | Result-based design acceptable |
| internal/manifest/provision.go | ✓ | Medium | No beginning/success logs |
| internal/manifest/render.go | ✓ | Medium | Warnings only, no story pattern |
| internal/manifest/template.go | ✓ | High | Zero logging in complex rendering |
| internal/manifest/types.go | ✓ | None | Type definitions |
| internal/manifest/validate.go | ✓ | None | Pure validation functions |
| internal/preflight/preflight.go | ✓ | Low | Returns structured results |
| internal/reconcile/deploy.go | ✓ | High | Critical operations have no logging |
| internal/reconcile/git.go | ✓ | High | Git operations have no logging |
| internal/reconcile/interfaces.go | ✓ | None | Interface definitions |
| internal/reconcile/lock_unix.go | ✓ | Low | Lock ops not logged |
| internal/reconcile/lock_windows.go | ✓ | Low | Lock ops not logged |
| internal/reconcile/reconcile.go | ✓ | Medium | Partial logging, some gaps |
| internal/reconcile/sops.go | ✓ | Medium | Decrypt operations not logged |
| internal/reconcile/template.go | ✓ | Medium | Template rendering not logged |
| internal/reconcile/validation.go | ✓ | None | Pure validation functions |
| internal/snapshot/snapshot.go | ✓ | High | Uses stderr instead of structured |
| internal/tunnel/cloudflare.go | ✓ | Medium | External service integration |
| internal/tunnel/tailscale.go | ✓ | Medium | External service integration |
| internal/tunnel/tunnel.go | ✓ | None | Type definitions |
| internal/ui/color.go | ✓ | None | This IS the logging facade |
| internal/update/update.go | ✓ | Medium | Self-update feature needs logging |

## Key Findings

### Critical Gaps (High Severity)
1. **Docker package** - Container lifecycle operations (up, down, restart, remove) have zero logging
2. **Reconcile deploy/git** - Critical GitOps operations have no visibility
3. **Emergency commands** - Recovery operations need audit trail
4. **Snapshot** - Uses raw stderr instead of structured logging
5. **Manifest template** - Complex template rendering is invisible

### Patterns to Address
1. Inconsistent use of `ui.*` vs structured `log.*`
2. Missing "story pattern" (beginning/success/failure) in most packages
3. Security events (auth failures, signature validation) not logged
4. External API calls lack request/response logging
5. State mutations (restart, remove, deploy) have no audit trail
