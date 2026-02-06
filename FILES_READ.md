# Files Read During Investigation

Investigation date: 2026-02-05

## Entry Point

- `cmd/bosun/main.go`

## Build & Config

- `go.mod`
- `Makefile`

## internal/cmd/ (CLI Commands)

- `internal/cmd/root.go`
- `internal/cmd/daemon.go`
- `internal/cmd/helpers.go`
- `internal/cmd/diagnostics.go`
- `internal/cmd/crew.go`
- `internal/cmd/yacht.go`
- `internal/cmd/provision.go`
- `internal/cmd/render.go`
- `internal/cmd/reconcile.go`
- `internal/cmd/init.go`
- `internal/cmd/migrate.go`
- `internal/cmd/webhook.go`
- `internal/cmd/trigger.go`
- `internal/cmd/validate.go`
- `internal/cmd/status.go`
- `internal/cmd/emergency.go`
- `internal/cmd/comms.go`
- `internal/cmd/update.go`
- `internal/cmd/completions.go`
- `internal/cmd/alert.go`

## internal/config/

- `internal/config/config.go`

## internal/docker/

- `internal/docker/client.go`
- `internal/docker/compose.go`
- `internal/docker/containers.go`
- `internal/docker/interface.go`

## internal/ui/

- `internal/ui/color.go`

## internal/log/

- `internal/log/log.go`
- `internal/log/fields.go`
- `internal/log/context.go`

## internal/daemon/

- `internal/daemon/daemon.go`
- `internal/daemon/api.go`
- `internal/daemon/server.go`
- `internal/daemon/socket.go`
- `internal/daemon/tcp.go`
- `internal/daemon/client.go`

## internal/reconcile/

- `internal/reconcile/reconcile.go`
- `internal/reconcile/git.go`
- `internal/reconcile/deploy.go`
- `internal/reconcile/sops.go`
- `internal/reconcile/template.go`
- `internal/reconcile/validation.go`
- `internal/reconcile/interfaces.go`
- `internal/reconcile/lock_unix.go`
- `internal/reconcile/lock_windows.go`

## internal/alert/

- `internal/alert/alert.go`
- `internal/alert/discord.go`
- `internal/alert/sendgrid.go`
- `internal/alert/twilio.go`

## internal/manifest/

- `internal/manifest/doc.go`
- `internal/manifest/types.go`
- `internal/manifest/render.go`
- `internal/manifest/merge.go`
- `internal/manifest/interpolate.go`
- `internal/manifest/provision.go`
- `internal/manifest/validate.go`
- `internal/manifest/chart.go`
- `internal/manifest/template.go`
- `internal/manifest/migrate.go`

## internal/tunnel/

- `internal/tunnel/tunnel.go`
- `internal/tunnel/tailscale.go`
- `internal/tunnel/cloudflare.go`

## internal/update/

- `internal/update/update.go`

## internal/snapshot/

- `internal/snapshot/snapshot.go`

## internal/lock/

- `internal/lock/lock.go`

## internal/preflight/

- `internal/preflight/preflight.go`

## internal/fileutil/

- `internal/fileutil/fileutil.go`

## Top-Level Docs

- `llms.txt`
- `CLAUDE.md`

## Total: 66 source files read
