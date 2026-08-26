# CodeRabbit Review & Parallel Pattern Consistency Fixes — Field Report

**Date:** 2026-02-25
**Type:** investigation
**Project:** bosun

## Goal

Run a comprehensive CodeRabbit review against recent changes (v0.13.0..main), fix all findings in parallel using worktree-isolated agents, trace and fix the missing v0.15.0 release, and configure a GitHub App for the release pipeline to prevent the same class of bug from recurring.

## What We Tried

### CodeRabbit CLI Review

Ran `coderabbit review --plain --base v0.13.0` to audit all changes since the last meaningful release baseline. CodeRabbit found 12 findings, all nitpicks except one potential_issue:

| Category | Count | Severity |
|----------|------:|----------|
| `log.Component()` → `log.ComponentCtx()` | 10 | nitpick |
| Misleading socket audit log | 1 | nitpick |
| Cloudflare stderr sensitive data | 1 | potential_issue |

A parallel docs exploration agent scored the documentation 9.5/10, finding only one issue: ADR-0001 still referenced Python-era `manifest.py` without noting the Go rewrite.

### Parallel Worktree Agents

Dispatched 4 agents simultaneously:

1. **ComponentCtx migration** (worktree) — 14 mechanical replacements across 6 files
2. **Socket log + cloudflare redaction** (worktree) — 2 files, including a new `redactSensitiveOutput()` function with 3 regex patterns
3. **ADR-0001 cleanup** (worktree) — added clarifying notes about Python references
4. **v0.15.0 investigation** (research, no worktree) — traced the root cause

All 3 code worktrees merged cleanly into main. Tests passed on the unified result.

## Root Cause

### Missing v0.15.0 Release

The release-please pipeline had a fundamental design flaw. The sequence:

1. Release-please creates PR #66 (v0.15.0 changelog + manifest bump)
2. PR auto-merges via `GITHUB_TOKEN`
3. **No push event fires** — GitHub's loop prevention suppresses events from `GITHUB_TOKEN` merges
4. Release-please never sees the merge, never creates the v0.15.0 tag/release
5. Goreleaser never triggers (depends on `release_created` output)

This is the same bug tracked as `bosun-f1a`. The fix: use a GitHub App installation token (via `actions/create-github-app-token@v2`) for the auto-merge step. App tokens produce real push events that trigger follow-up workflows.

### Graceful Fallback

The initial fix hard-required the App token, which killed the entire release-please job when `RELEASE_APP_ID` wasn't set yet. Fixed with:

```yaml
- name: Generate App Token
  if: ${{ steps.release.outputs.pr && vars.RELEASE_APP_ID }}
  # ...

- name: Auto-merge release PR
  env:
    GH_TOKEN: ${{ steps.app-token.outputs.token || secrets.GITHUB_TOKEN }}
```

This means: use the App token if available, fall back to `GITHUB_TOKEN` if not (auto-merge still works, just won't trigger goreleaser).

> **Current contract:** This fallback was later removed because a
> `GITHUB_TOKEN` merge cannot trigger the release workflow. Missing or invalid
> Release App credentials now warn and leave the release PR open for manual
> merge; auto-merge requires a successfully generated App token.

## What Worked

### Worktree Agent Parallelism

Four agents completed independently in ~2 minutes total. The worktree isolation meant zero merge conflicts — each agent had its own copy of the repo. Merging was a clean fast-forward for each branch.

### ComponentCtx Migration Pattern

The `log.Component(name)` → `log.ComponentCtx(ctx, name)` migration was perfectly mechanical. Every call site already had `ctx context.Context` as a parameter — the only change was threading it through to the logger constructor. This is exactly the kind of work that benefits from automated review (CodeRabbit) + automated fixing (worktree agents).

### Cloudflare Stderr Redaction

Added three pre-compiled regex patterns to catch bearer tokens, authorization headers, and long base64 blobs in cloudflared's stderr output:

```go
var (
    bearerTokenPattern = regexp.MustCompile(`Bearer [A-Za-z0-9._-]+`)
    authHeaderPattern  = regexp.MustCompile(`(?i)Authorization:.*`)
    base64BlobPattern  = regexp.MustCompile(`[A-Za-z0-9+/=]{40,}`)
)
```

Applied to all 6 stderr logging sites across 3 functions.

## Decisions Made

| Decision | Rationale |
|----------|-----------|
| GitHub App `forge-bellows` for release pipeline | App tokens bypass GITHUB_TOKEN loop prevention. Named after the bellows in a forge — it fans the flames (triggers workflows) |
| Graceful fallback to GITHUB_TOKEN | Don't break the pipeline if the App isn't configured. Auto-merge still works, just without follow-up workflow triggers |
| Delete Claude Code Review workflow | Was failing on Dependabot PRs (missing API key in Dependabot secret scope). Not worth the maintenance overhead for a single-developer project |
| Neutral audit log message | "Handled socket request" instead of "Successfully handled socket request" — the word "successfully" is misleading for 4xx/5xx responses |

## Gotchas

- **GitHub Apps can't be created via CLI** — `gh` has no `app create` command. Must use the web UI at `github.com/settings/apps/new`. The App ID and PEM can then be set via `gh variable set` and `gh secret set`.

- **Worktree branch deletion order matters** — `git branch -d` fails if the worktree is still attached. Must `git worktree remove` first, then delete the branch.

- **`actions/create-github-app-token@v2` errors are non-obvious** — when `app-id` is empty (variable not set), it fails with "appId option is required" which kills the entire job. The `if` conditional on the step is the safety valve.

- **v0.15.0 was eventually recovered** — merging Dependabot PR #67 triggered a new release-please run that saw the merged v0.15.0 PR and created the tag. The release exists, just arrived late.

- **14 out of how many?** — The ComponentCtx migration touched 14 call sites across 6 files. These were the ones that had `ctx` available but weren't using it. There may be deeper call sites where `ctx` isn't threaded yet — that's a separate, larger refactor.

## Key Takeaways

- **CodeRabbit CLI is effective for consistency audits** — it found 14 real consistency gaps that manual review missed. Running `coderabbit review --plain --base <tag>` against a release baseline is a good practice before cutting a new release.
- **GITHUB_TOKEN loop prevention is a known GitHub Actions footgun** — any workflow that auto-merges PRs and expects a follow-up workflow to fire MUST use an App token or PAT. This is documented but easy to miss.
- **Worktree agents are ideal for mechanical, parallel fixes** — when changes don't overlap (different files or different hunks), worktree isolation eliminates merge risk entirely. Four agents finished in the time one would have taken.
- **Graceful degradation must preserve the release invariant** — Release App credential failures should leave the generated PR open and keep release generation successful, but must not fall back to a token whose merge suppresses the follow-up release workflow.
- **Delete workflows that cause more noise than value** — the Claude Code Review workflow was failing on every Dependabot PR. Removing it was the right call for a single-developer project.
