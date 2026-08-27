---
status: "in-flight"
updated: "2026-08-26"
branch: "fix/backup-native-tar"
body_sha256: "f51883edb39c81545b702eac2b70fbcd60bfb76d82cd28bf84ae2f15138e1a1f"
session: "hazel-compass"
session_id: "32b0b95a-c285-4137-9271-1d415c7e1a0e"
model: "claude-opus-5"
harness: "claude-code 2.1.247"
machine: "cf6e768835c7"
approved_in: "stone-mallet"
approved_session_id: "a5d93ee5-843b-47e8-95b3-07db295a04f3"
---

# Remove the llms.txt sync nudge workflow

## Context

`.github/workflows/llms-txt-sync.yml` posts an advisory "Does `llms.txt` need an update?" comment on any PR touching capability-bearing paths. Its `CLAIM_PATHS` list covers `internal/cmd/`, `internal/daemon/`, `internal/reconcile/`, `internal/manifest/`, `internal/config/`, `cmd/`, `go.mod`, `README.md`, and `docs/` — effectively every code PR in this repo. Each first-time comment sends Cameron a notification email as thread author, so the nudge fires as constant noise rather than signal (latest: `cameronsjo/bosun#594`).

The drift it guarded against (#293) stays covered by `AGENTS.md` § Skill Maintenance and ordinary review. Decision: delete the workflow.

## Change

Single deletion:

- `.github/workflows/llms-txt-sync.yml` — remove the file.

Leave alone:

- `docs/field-reports/documentation-drift-daemon-vs-receiver-webhooks.md` and `docs/field-reports/deploy-chain-hardening-managed-set-and-bind-mount-topology.md` reference the workflow, but they are historical narrative of what was true at the time — not live docs.
- `CHANGELOG.md` is release-please-managed; a `ci:` change cuts no release and gets no hand-written entry.

## Steps

1. Create a worktree off `origin/main` (bosun's primary checkout is branch-mode; `Write`/`Edit`/`git commit` are blocked there) — branch `ci/remove-llms-txt-nudge`.
2. `git rm .github/workflows/llms-txt-sync.yml`.
3. Commit as `ci: remove llms.txt sync nudge workflow` with the producer-tuple trailers, push `-u`, open a PR.

## Verification

- `command ls .github/workflows/` in the worktree no longer lists `llms-txt-sync.yml`.
- `gh run list --workflow=llms-txt-sync.yml --limit 1` on the PR's branch returns no new run for that PR — and the PR itself carries no 🪢 nudge comment.
- No other workflow references it: `command grep -rn "llms-txt-sync" .github/`.

## Panel

Panel: none — one-file workflow deletion, no design surface.
