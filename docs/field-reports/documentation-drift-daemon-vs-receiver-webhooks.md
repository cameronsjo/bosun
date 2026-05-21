# Documentation Drift: Daemon-vs-Receiver Webhook Claims — Field Report

**Date:** 2026-05-21
**Type:** investigation
**Project:** bosun

## Goal

Issue #293 ("webhook: no Gitea/GitLab/Bitbucket webhook handler in daemon despite docs claiming support") drew an external comment that read as bad press: our instruction files (`llms.txt`, `CLAUDE.md`) assert "multi-provider webhooks" as fact, so an agent treating them as ground truth would tell an operator to point Gitea at the daemon's `/webhook` — the exact fail-open auth path. The ask: a full audit of `llms.txt` to make sure every claim is "kosher," then stop the drift from recurring.

## Root Cause

The drift came from conflating **two distinct webhook surfaces**:

- **Daemon** (`internal/daemon/server.go`) registers only `/webhook` (generic HMAC, checks `X-Signature` then falls back to `X-Hub-Signature-256`), `/webhook/github`, and `/webhook/manual`. No GitLab/Gitea/Bitbucket handler.
- **Standalone `bosun webhook` receiver** (`internal/cmd/webhook.go`) registers `/webhook/{github,gitlab,gitea,bitbucket}` with correct per-provider signature validation (`X-Gitlab-Token`, `X-Gitea-Signature`, `X-Event-Key`), then forwards normalized triggers to the daemon over its Unix socket. This is the container-vs-daemon split from ADR 0008.

So "multi-provider webhooks" is **true of the receiver, false of the daemon**. The provider-parsing code genuinely exists — someone saw it and assumed it was wired into the daemon mux. Every doc that said *"the daemon provides multi-provider webhooks"* was wrong; every doc describing the *receiver* as multi-provider was right. The operator hazard is concrete: a Gitea webhook hitting the daemon's generic `/webhook` sends `X-Gitea-Signature`, which the generic handler doesn't read, so validation fails — nudging the operator toward emptying the secret to "make it work" (fail-open).

## What We Tried

The audit was a claims-vs-code diff, not a code change. Process that worked:

1. **Read the actual route registration** (`server.go:53-68`) rather than trusting prose. Confirmed the daemon mux: health, ready, `/webhook`, `/webhook/github`, `/webhook/manual`, `/api/widget`, `/metrics`.
2. **Verified every other factual claim** in `llms.txt` against code: all 12 doc links resolve, all 12 referenced commands exist, socket path default, `/health` `/ready`, and circuit-breaker-after-3 (`MaxAttempts = 3` in `reconcile/state.go`). Only two claims were wrong.
3. **Grepped the whole repo** for the false claim and the stale Go version to find the blast radius — drift is rarely confined to one file.
4. **Read the receiver** (`internal/cmd/webhook.go:113-118`) to confirm multi-provider support is real *somewhere*, so the fix could relocate the claim instead of deleting a true capability.

## What Worked

Two false claims, both repo-wide, fixed by attributing capability to the correct component:

- **"Multi-provider webhooks" → daemon serves GitHub + generic; receiver handles the rest.** Corrected in `llms.txt`, `README.md` (×2), `docs/gitops.md`, `docs/commands.md`, `skills/onboard/resources/gitops.md`, `openspec/project.md`.
- **"Go 1.24+" → "Go 1.25+"** to match `go.mod` (`go 1.25.0`). Corrected in the doc copies.
- **Bonus skill-name bug:** `skills/onboard/SKILL.md` declared `name: bosun` (the plugin name) instead of `onboard`, failing the name-matches-directory convention enforced by a frontmatter-validation hook. Renamed.

**The loop-closer:** a non-blocking GitHub Action (`.github/workflows/llms-txt-sync.yml`) that, on PR open/synchronize, lists changed files and posts a sticky comment when capability-bearing code or claim-bearing docs change without an `llms.txt` update. It resolves its own comment when the condition clears. This directly answers the commenter's root point — "the drift recurs unless something checks it."

## What Didn't Work

- **Trusting my own first-pass audit categorization.** I initially flagged `docs/commands.md:502-504` as "correct (describes the receiver)." On reading the surrounding context, that endpoint table was under the **daemon** section and *also* falsely listed provider routes (and omitted the real `/webhook/manual`). Lesson: a grep hit's correctness depends on the section header three lines up — read the frame, not just the line.
- **Over-eager scope.** I flagged `docs/architecture/decisions.md` and `bosun-architecture-review.md` as drift candidates. On reading them, both correctly attribute multi-provider to the `bosun webhook` *container*. Editing them would have been churn that rewrites an accurate design record. Left intact.

## Gotchas

- **CodeRabbit's least-privilege suggestion was subtly wrong.** It said the workflow "only calls issues APIs," so drop `pull-requests` entirely and add `issues: write`. But the script also calls `github.rest.pulls.listFiles`, which needs `pull-requests: read`. Applying the suggestion verbatim would have caused a runtime 403 that `actionlint` can't catch (it doesn't model API-to-scope mapping). Correct set: `issues: write` + `pull-requests: read`. **Triage findings against what the code actually does.**
- **GitHub 504 on merge ≠ merge failed.** `gh pr merge --squash` returned a 504 Gateway Timeout "Unicorn" page, but the merge had committed server-side (`state: MERGED`, commit `4e03c4b`). Checking PR state before retrying avoided a confusing double-merge attempt on an already-merged PR.
- **`gh api graphql` trips the git-guardrails `guard-gh-write` hook** — it can't determine a target repo for `gh api` and demands `-R`, which `gh api` doesn't accept. Workaround: use `gh pr view -R owner/repo --json ...` for reads instead of raw GraphQL.
- **Squash vs merge-commit matters under release-please.** The branch had mixed prefixes (`docs:`, `ci:`, `fix(skill):`). A merge commit would land the `fix(skill):` on `main` and trigger a spurious patch release for a docs/metadata change. Squashing under the single `docs:` subject kept `main`'s commit stream honest.

## Recommendations

- **Attribute capabilities to components, not to the product.** "Bosun supports X" hides which binary/path provides X. The drift was born from "the daemon provides multi-provider webhooks" — a sentence that was never true. Name the surface.
- **The CI guard is a nudge, not a gate.** It can't know whether `llms.txt` *needs* updating — only that code changed without it. Keep it non-blocking; a blocking check would train people to touch `llms.txt` cosmetically to pass.
- **When auditing docs, read route/handler registration directly.** Prose drifts; `mux.HandleFunc` calls don't lie.
- **Pin third-party Actions to commit SHAs.** `actions/github-script@v7` → `@f28e40c…` (v7.1.0). Resolve the tag with `gh api repos/actions/github-script/git/ref/tags/v7`.

## Key Takeaways

- Capability claims in instruction files (`llms.txt`, `CLAUDE.md`) are higher-stakes than stale code comments: an agent treats them as ground truth and acts on them, so a wrong claim actively misleads both the model and the human downstream.
- The daemon-vs-receiver webhook split is the recurring trap — multi-provider lives in the standalone `bosun webhook` receiver, never the daemon. Captured in project memory (`project_webhook_surfaces.md`) so it survives context resets.
- Doc drift is a *class* of bug. The durable fix isn't correcting the line; it's the mechanism that diffs claims against code on every PR.
- Triage reviewer findings against actual code behavior — even a good reviewer (CodeRabbit, ASSERTIVE profile) can suggest a permission change that compiles/lints but 403s at runtime.
