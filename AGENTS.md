<!-- OPENSPEC:START -->
# OpenSpec Instructions

These instructions are for AI assistants working in this project.

Always open `@/openspec/AGENTS.md` when the request:
- Mentions planning or proposals (words like proposal, spec, change, plan)
- Introduces new capabilities, breaking changes, architecture shifts, or big performance/security work
- Sounds ambiguous and you need the authoritative spec before coding

Use `@/openspec/AGENTS.md` to learn:
- How to create and apply change proposals
- Spec format and conventions
- Project structure and guidelines

Keep this managed block so 'openspec update' can refresh the instructions.

<!-- OPENSPEC:END -->

# Bosun - AI Context

@llms.txt

## Building and Running

```bash
# Build
make build              # -> build/bosun

# Run without building
make run ARGS="doctor"

# Development build (no optimizations)
make dev

# Install to GOPATH/bin
make install

# Build for all platforms
make build-all
```

## Testing

```bash
make test               # Run all tests
make test-cover         # With coverage (creates coverage.out + coverage.html)
```

**Patterns:**

- `testify/assert` + `testify/require` (not stdlib assertions)
- Table-driven subtests: `t.Run("case", func(t *testing.T) { ... })`
- Temp dirs via `t.TempDir()` with `evalSymlinks()` helper for macOS `/var` → `/private/var`
- Project root tests: create `manifest/` or `bosun.yaml` in temp dir, `os.Chdir()`, defer restore
- Config tests: `loadConfigFile(tmpDir)` + `extract*()` (unit) or `Load()` with chdir (integration)

## Key Packages

### internal/cmd

Cobra commands following the pattern:

```go
var exampleCmd = &cobra.Command{
    Use:     "example",
    Aliases: []string{"alias"},
    Short:   "Short description",
    Long:    "Long description...",
    Run:     runExample,
}

func runExample(cmd *cobra.Command, args []string) {
    // Implementation
}

func init() {
    rootCmd.AddCommand(exampleCmd)
}
```

### internal/config

Project discovery and configuration. Searches upward for `bosun.yaml` / `bosun.yml` / `bosun/` / `manifest/`.

- **`FindRoot()`** — walk up directories to find project root
- **`Load()`** — parse config file, extract all sections, return `*Config`
- **Pattern**: raw `configFile` (YAML DTO) → `extract*()` helpers → public `Config` with getters
- **Config sections**: infrastructure containers, tunnel, alerts, post-sync hooks

### internal/daemon

Long-running GitOps daemon with webhook reception, polling, and health checks.

- **Interfaces**: Unix socket API (`/var/run/bosun.sock`), HTTP webhooks, optional TCP API
- **Concurrency**: single-flight reconciliation with dirty-flag coalescing
- **Features**: deploy state tracking, circuit breaker (3 failures), drift detection, alert throttling
- **`ConfigFromEnv()`** — builds daemon config from environment variables

### internal/docker

Docker SDK wrapper. Uses `github.com/docker/docker/client`.

```go
client, err := docker.NewClient()
defer client.Close()

containers, err := client.ListContainers(ctx, onlyRunning)
err := client.RestartContainer(ctx, name)
```

### internal/manifest

YAML rendering engine.

- **Types**: `ServiceManifest`, `StackManifest`, `RenderOutput`
- **Rendering**: `RenderStack()`, `RenderService()`
- **Merge**: `DeepMerge()` with special handling for networks/depends_on
- **Interpolation**: `${var}` syntax resolved from service config

### internal/reconcile

GitOps engine. Pipeline:

1. Lock → 2. Git clone/pull → 3. SOPS decrypt → 4. Go text/template + Sprig → 5. Backup → 6. Deploy (local copy or tar-over-SSH) → 7. Docker compose up → 8. Post-sync hooks → 9. Unlock

- **`PostSyncHook`** — glob-matched container restarts on file changes
- **`EvaluatePostSyncHooks()`** — match changed files against hook patterns (deduped by container)
- **Deploy modes**: local (file copy) or remote (tar-over-SSH)

### internal/alert

Native alerting with pluggable providers. `Provider` interface: `Name()`, `IsConfigured()`, `Send()`.

- **Providers**: `DiscordProvider`, `SendGrid`, `Twilio`
- **Manager**: fan-out to all configured providers, helper methods for deploy/drift/doctor events
- **Throttling**: exponential backoff on repeated failures

### internal/log

Structured logging via zerolog. Two output modes: `console` (human-readable, colored) and `json` (structured).

- **`Component(name)`** — returns a sub-logger with component field
- **Constants**: `ComponentReconcile`, `ComponentDaemon`, `ComponentDocker`, etc.
- **Field helpers**: `FieldPath`, `FieldContainer`, `FieldStack`, etc.

### internal/ui

Colored console output with nautical theme. Wraps zerolog for user-facing messages.

```go
ui.Success("Container started!")   // ✓ green
ui.Warning("Traefik not running")  // ⚠ yellow
ui.Error("Failed: %v", err)        // ✗ red
ui.Fatal("Critical: %v", err)      // ✗ red + os.Exit(1)
ui.Step(1, "Cloning repo...")      // numbered step
ui.Header("Deploy Summary")        // bold section header
```

Nautical helpers: `Anchor()`, `Ship()`, `Compass()`, `Mayday()`, `Snapshot()`, `Package()`.

### internal/lock

File-based locking (`flock` on Unix, `LockFileEx` on Windows). Prevents concurrent reconciliation runs.

### internal/preflight

Pre-flight validation for `bosun doctor`. Checks Docker, Compose v2, Git, SOPS, Age key, project structure.

### internal/sentry

Opt-in Sentry error tracking and performance monitoring. Enabled via `BOSUN_SENTRY_DSN` env var.

### internal/snapshot

Snapshot management for manifest output files. Used for rollback via `bosun mayday`.

### internal/tunnel

Abstraction layer for tunnel providers (Tailscale Funnel, Cloudflare Tunnel). Used by `bosun radio`.

### internal/update

Self-update via GitHub releases. Used by `bosun update`.

### internal/fileutil

Common file operations (copy, ensure directory, atomic write).

## Design Principles

1. **Captain gives orders, bosun executes** - Push to git, everything updates
2. **Single binary** - No Python, uv, or bash dependencies on target
3. **Every crew member has a backup** - Batteries included, all swappable
4. **One yacht, many ports** - Monorepo support for multi-server

## Spec Before Code

When a feature adds or changes behavior covered by `openspec/specs/`, **MUST** create a change proposal under `openspec/changes/<id>/` with spec deltas BEFORE writing implementation code. Plan mode plans inform the proposal but do not replace it. See `openspec/AGENTS.md` for the full workflow.

## Spec Review Workflow

Spec PRs follow an iterative review cycle before implementation begins:

1. **Open PR** from spec branch (`spec/<change-id>`) targeting `main`
2. **CodeRabbit reviews** automatically on push
3. **Fix all findings** — including nitpicks (consistency prevents implementation bugs)
4. **Iterate** until convergence (0 actionable or only stale findings)
5. **Label `ready-to-build`** — spec is stable, implementation can begin
6. **Rate-limit handling**: If CodeRabbit doesn't review within ~5min, trigger manually with `@coderabbitai review`

The `ready-to-build` label is a gate: do not start implementation until it's applied. Use `/spec-review` to automate the cycle.

## Adding a New Command

1. Create file in `internal/cmd/<name>.go`
2. Define command and flags
3. Add to `rootCmd` in `init()`
4. Update `docs/commands.md`
5. Update `skills/onboard/resources/commands.md` (see Skill Maintenance)

Example:

```go
// internal/cmd/example.go
package cmd

import (
    "github.com/spf13/cobra"
    "github.com/cameronsjo/bosun/internal/ui"
)

var exampleCmd = &cobra.Command{
    Use:   "example",
    Short: "Example command",
    Run: func(cmd *cobra.Command, args []string) {
        ui.Success("Example ran!")
    },
}

func init() {
    rootCmd.AddCommand(exampleCmd)
}
```

## Dependencies

Core (direct):

- `github.com/spf13/cobra` - CLI framework
- `github.com/docker/docker` - Docker SDK
- `github.com/go-git/go-git/v5` - Git operations (in-process clone/pull)
- `github.com/getsops/sops/v3` - Secret decryption (SOPS + Age)
- `github.com/rs/zerolog` - Structured logging
- `github.com/Masterminds/sprig/v3` - Template functions
- `github.com/fatih/color` - Colored output
- `github.com/getsentry/sentry-go` - Error tracking (opt-in)
- `github.com/creativeprojects/go-selfupdate` - Self-update from GitHub releases
- `gopkg.in/yaml.v3` - YAML parsing
- `github.com/stretchr/testify` - Test assertions

## Versioning and Releases

Releases are fully automated via **release-please** + **goreleaser**.

- `version = "dev"` in `internal/cmd/root.go` — goreleaser injects the real version at build time via `-ldflags`
- **Conventional Commits drive versioning**: `feat:` bumps minor, `fix:` bumps patch, `feat!:`/`BREAKING CHANGE:` bumps minor pre-1.0 (major post-1.0)
- **Never manually edit version numbers** — release-please manages `.release-please-manifest.json` and `CHANGELOG.md`

**Release pipeline:**

1. Push to `main` with conventional commit prefixes
2. Release-please opens/updates a release PR (auto-generated changelog)
3. Merge the release PR
4. Release-please creates a GitHub Release + git tag
5. Goreleaser (via Dagger) builds binaries, pushes to GHCR, signs with Cosign

**Config files:**

- `release-please-config.json` — release-please behavior
- `.release-please-manifest.json` — current version (do not edit manually)
- `.goreleaser.yml` — build matrix and artifact config

## Skill Maintenance

When a feature is added, changed, or removed, **MUST** update the onboard skill resource files to match:

- `skills/onboard/resources/configuration.md` — config file schema, fields, examples
- `skills/onboard/resources/gitops.md` — reconcile pipeline, daemon, webhooks, drift
- `skills/onboard/resources/commands.md` — CLI commands, flags, usage
- `skills/onboard/resources/manifests.md` — manifest system, provisions, stacks

The skill is the primary consumer-facing documentation. If the code changes but the skill doesn't, users and AI agents get stale information.

## Environment Variables

All bosun-specific env vars use the `BOSUN_` prefix. Legacy unprefixed vars (`REPO_URL`, `POLL_INTERVAL`, etc.) are supported but `BOSUN_` variants take precedence.

| Variable | Package | Description |
|----------|---------|-------------|
| `BOSUN_REPO_URL` | daemon, reconcile | Git repository URL |
| `BOSUN_REPO_BRANCH` | daemon, reconcile | Branch to track (default: `main`) |
| `BOSUN_POLL_INTERVAL` | daemon | Polling interval in seconds (default: `3600`) |
| `BOSUN_SOCKET_PATH` | daemon | Unix socket path (default: `/var/run/bosun.sock`) |
| `BOSUN_ENABLE_TCP` | daemon | Enable TCP API (`true`/`false`) |
| `BOSUN_TCP_ADDR` | daemon | TCP listen address (default: `:8080`) |
| `BOSUN_BEARER_TOKEN` | daemon, trigger | Bearer token for TCP auth |
| `BOSUN_DISABLE_HTTP` | daemon | Disable HTTP webhook server |
| `BOSUN_SECRETS_FILE` | daemon, render | SOPS secrets file path |
| `BOSUN_INFRA_DIR` | daemon | Infrastructure directory |
| `BOSUN_STATE_DIR` | daemon, reconcile | Deploy state directory |
| `BOSUN_POST_SYNC_HOOKS` | daemon, reconcile | JSON array overriding config file hooks |
| `BOSUN_HOOK_SETTLE_DELAY` | daemon, reconcile | Global pause before post-sync hooks run (e.g., `2s`) |
| `BOSUN_DEPLOY_PATHS` | daemon, reconcile | JSON array of glob patterns for deploy-relevant paths (overrides config file) |
| `BOSUN_COMPOSE_UP_TIMEOUT` | daemon, reconcile | Timeout for `docker compose up` (default: `10m`; accepts Go durations or plain seconds) |
| `BOSUN_HEALTH_CHECK_TIMEOUT` | daemon, reconcile | Post-deploy health verification timeout (default: `60s`; set to `0` to disable) |
| `BOSUN_HEALTH_CHECK_INTERVAL` | daemon, reconcile | Poll interval for health verification (default: `5s`) |
| `BOSUN_RESTART_BREAKER` | daemon, reconcile | Enable restart circuit breaker (default: `true`) |
| `BOSUN_RESTART_THRESHOLD` | daemon, reconcile | Restart count delta to trip breaker (default: `5`; must be positive) |
| `BOSUN_RESTART_WINDOW` | daemon, reconcile | Time window for restart delta evaluation (default: `10m`) |
| `BOSUN_RECONCILE_TIMEOUT` | daemon | Reconciliation timeout |
| `BOSUN_SHUTDOWN_TIMEOUT` | daemon | Graceful shutdown timeout |
| `BOSUN_API_TIMEOUT` | daemon | API request timeout |
| `BOSUN_DRIFT_INTERVAL` | daemon | Drift check interval |
| `BOSUN_DRIFT_ALERT_COOLDOWN` | daemon | Cooldown between repeated drift alerts (default: `1h`) |
| `BOSUN_DRIFT_ALERT_DEBOUNCE` | daemon | Debounce window before first drift alert fires (default: `0` = disabled) |
| `BOSUN_DRIFT_RESOLVE_ALERTS` | daemon | Send "drift resolved" notifications (default: `true`) |
| `BOSUN_CONTENT_HASH_SYNC` | daemon, reconcile | Compare file hashes before writing to skip unchanged files (default: `true`) |
| `BOSUN_REMOVE_ORPHANS` | daemon, reconcile | Pass `--remove-orphans` to docker compose up (default: `true`; overrides config file) |
| `BOSUN_DISCORD_WEBHOOK_URL` | config | Discord webhook URL (overrides config file; legacy: `DISCORD_WEBHOOK_URL`) |
| `BOSUN_SENDGRID_API_KEY` | config | SendGrid API key (overrides config file; legacy: `SENDGRID_API_KEY`) |
| `BOSUN_SENDGRID_FROM_EMAIL` | config | SendGrid sender email (overrides config file; legacy: `SENDGRID_FROM_EMAIL`) |
| `BOSUN_SENDGRID_FROM_NAME` | config | SendGrid sender name (overrides config file; legacy: `SENDGRID_FROM_NAME`) |
| `BOSUN_TWILIO_ACCOUNT_SID` | config | Twilio account SID (overrides config file; legacy: `TWILIO_ACCOUNT_SID`) |
| `BOSUN_TWILIO_AUTH_TOKEN` | config | Twilio auth token (overrides config file; legacy: `TWILIO_AUTH_TOKEN`) |
| `BOSUN_TWILIO_FROM_NUMBER` | config | Twilio sender number (overrides config file; legacy: `TWILIO_FROM_NUMBER`) |
| `BOSUN_SSH_KEY` | reconcile | SSH key path for git operations |
| `BOSUN_SSH_KNOWN_HOSTS` | reconcile | Known hosts file path |
| `BOSUN_SSH_INSECURE_HOST_KEY` | reconcile | Skip host key verification (`true`/`false`) |
| `BOSUN_DAEMON_MODE` | log, sentry | Set automatically when daemon starts |
| `BOSUN_LOG_FORMAT` | log | Log format: `console` or `json` |
| `BOSUN_LOG_LEVEL` | log | Log level: `debug`, `info`, `warn`, `error` |
| `BOSUN_SENTRY_DSN` | sentry | Sentry DSN (empty = disabled) |
| `BOSUN_SENTRY_ENVIRONMENT` | sentry | Sentry environment tag |
| `BOSUN_SENTRY_TRACES_SAMPLE_RATE` | sentry | Trace sample rate (0.0–1.0) |

## Gotchas

- **CLAUDE.md is a symlink** to `AGENTS.md`. When staging for git, `git add AGENTS.md` (not `CLAUDE.md`)
- **Circuit breaker**: after 3 consecutive deploy failures, daemon stops retrying. Reset with `bosun trigger -f`
- **FUSE mounts**: Traefik (and similar services) don't detect config changes on Unraid's FUSE filesystem — this is why post-sync hooks exist
- **macOS temp dirs**: `/var` symlinks to `/private/var`, so `t.TempDir()` paths need `filepath.EvalSymlinks()` in tests
- **Config graceful degradation**: `config.Load()` errors are swallowed at startup, and `config.LoadFrom()` failures during reconciliation are logged as warnings — reconcile works without a project config file. The reconciler re-reads `bosun.yaml` from the repo after each git pull to pick up hook changes
- **Env var precedence**: `BOSUN_POST_SYNC_HOOKS` (JSON) completely *replaces* hooks from `bosun.yaml`, it does not merge
- **Dirty repo is non-fatal**: `Pull()` warns on uncommitted changes and proceeds — the hard reset discards them. Don't add a dirty-state failure gate; these are stale reconciliation artifacts

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd sync
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
