# Contributing to bosun

Thanks for your interest in contributing! Bosun is designed to be simple and stay simple.

## Philosophy

Before contributing, understand the project philosophy:

- **Shell scripts over frameworks.** ~100 lines of bash beats 10,000 lines of Go.
- **Batteries included, batteries swappable.** Defaults work. Replace any component.
- **Escape hatches everywhere.** Raw passthrough when abstractions don't fit.
- **Guardrails matter.** Manifest stays under 250 lines. Max 10 provisions.

## How to Contribute

### Reporting Bugs

1. Check existing issues first
2. Include: OS, Docker version, steps to reproduce, expected vs actual behavior
3. Attach logs if relevant (`docker logs bosun`)

### Suggesting Features

1. Open an issue with `[Feature]` prefix
2. Explain the use case, not just the solution
3. Consider: Does this fit the philosophy? Is there a simpler way?

### Pull Requests

1. Fork the repo
2. Create a branch: `git checkout -b feat/my-feature`
3. Make your changes
4. Test locally
5. Commit with conventional commits: `feat:`, `fix:`, `docs:`
6. Open a PR

### Code Style

- Shell: Use `shellcheck`
- Python: Use `ruff` for linting, type hints everywhere
- YAML: 2-space indent
- Markdown: Pass `markdownlint`

## What We're Looking For

### High Priority

- Bug fixes with tests
- Documentation improvements
- New provisions (if broadly useful)
- Cloudflare Tunnel integration

### Lower Priority

- Major refactors (unless discussed first)
- New dependencies
- Features that increase complexity

### Not Accepting

- Kubernetes support (use Flux/ArgoCD)
- Complex orchestration features
- Anything that breaks the "~100 lines" constraint

## Development Setup

```bash
# Clone
git clone https://github.com/cameronsjo/bosun.git
cd bosun

# Test manifest
cd manifest
uv run manifest.py render stacks/apps.yml --dry-run

# Test bosun (requires Docker)
cd bosun
docker compose up -d
```

## ADR Process

Significant changes require an ADR (Architecture Decision Record):

1. Copy `docs/adr/TEMPLATE.md` to `docs/adr/NNNN-title.md`
2. Fill in context, decision, consequences
3. Submit with your PR
4. ADR is accepted when PR merges

## Questions?

Open an issue with `[Question]` prefix or start a discussion.
