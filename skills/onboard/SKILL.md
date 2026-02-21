---
name: bosun
description: "Get started with Bosun -- what it is, how to set it up, and how to use it"
---


Guide the user through getting started with **Bosun**.

## About

Bosun is a single-binary CLI tool that brings GitOps workflows to homelab Docker Compose deployments. Write short manifest files, bosun generates compose + Traefik + Gatus configs. Push to git, bosun reconciles your server automatically. No Kubernetes, just smooth sailing.

## Prerequisites

Check that the user has the following installed/configured:

- Go 1.24+ (only if building from source)
- Docker + Docker Compose v2
- Git
- SOPS + Age (for secret encryption) -- `age-keygen` to generate keys, `sops` CLI for encryption
- Linux or macOS (tested on Unraid, Debian, Ubuntu, macOS)
- Tailscale (optional, for webhook/tunnel via `bosun radio`)

## Setup

Walk the user through initial setup:

1. **Install Bosun.** Pick one method:
   ```bash
   # Quick install (recommended) -- downloads, verifies checksum, installs to /usr/local/bin
   curl -fsSL https://raw.githubusercontent.com/cameronsjo/bosun/main/scripts/install.sh | bash

   # Or via Go
   go install github.com/cameronsjo/bosun/cmd/bosun@latest

   # Or from source
   git clone https://github.com/cameronsjo/bosun.git
   cd bosun && make build
   ./build/bosun --version
   ```

2. **Generate an Age encryption key** (if not already done):
   ```bash
   age-keygen -o ~/.config/sops/age/keys.txt
   ```

3. **Create `.sops.yaml`** in the project root with the public key from step 2.

4. **Initialize the project:**
   ```bash
   bosun init
   ```

5. **Run pre-flight checks:**
   ```bash
   bosun doctor
   ```
   This validates Docker, Git, SOPS, and configuration. Fix any issues it reports.

6. **Start the yacht (Docker Compose stack):**
   ```bash
   bosun yacht up
   ```

7. **For development on Bosun itself**, run `make help` to see all available targets.

## First Use

Guide the user through their first interaction with the product:

1. After `bosun init`, inspect the generated `bosun.yaml` config file.
2. Run `bosun doctor` -- all checks should pass.
3. Run `bosun yacht status` to see the current state of Docker Compose services.
4. Run `bosun crew list` to see running containers.
5. If setting up GitOps daemon mode, run `bosun init --systemd` to generate systemd unit files.

## Key Files

Point the user to the most important files for understanding the project:

- `cmd/bosun/main.go` -- CLI entry point
- `internal/cmd/` -- Cobra command definitions (one file per command)
- `internal/manifest/` -- YAML rendering engine for service manifests
- `internal/reconcile/` -- GitOps engine (clone, decrypt, template, deploy)
- `internal/docker/` -- Docker SDK wrapper
- `bosun.yaml` -- Project configuration (looked for in cwd or `~/.config/bosun/config.yaml`)
- `Makefile` -- Build, test, lint, CI targets (`make help` for full list)

## Common Tasks

- **Build from source:** `make build` (output at `build/bosun`)
- **Run tests:** `make test` (or `make test-cover` for coverage report)
- **Lint:** `make lint` (requires `golangci-lint`)
- **Run a one-shot reconcile:** `bosun reconcile` (pulls repo, decrypts secrets, deploys)
- **Check for config drift:** `bosun drift` (or `bosun drift --live` for fresh Docker check)
- **Run containerized CI locally:** `make ci` (uses Dagger to run test + lint + build)
- **Add a new CLI command:** Create `internal/cmd/<name>.go`, define a Cobra command, add it to `rootCmd` in `init()`, update `docs/commands.md`
