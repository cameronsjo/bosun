# Worktree Agent Bug Sprint — Field Report

**Date:** 2026-03-22
**Type:** pipeline
**Project:** bosun

## Goal

Fix four production bugs (#170–#173) in a single session using parallel worktree agents, iterate each PR through CodeRabbit review, and merge everything. Along the way, a fifth bug (#186) was discovered during root-cause investigation of #170 and fixed via red/green TDD.

## Pipeline Overview

The session followed a three-phase pattern: **triage → parallel dispatch → sequential CodeRabbit iteration**.

```text
1. Triage (15 min)
   - Read all 4 GitHub issues
   - Explore codebase to map file boundaries
   - Determine which bugs need specs vs direct fixes
   - Group by file overlap to avoid merge conflicts

2. Parallel dispatch (3 worktree agents)
   - spec-writer: #170 + #173 → OpenSpec proposal + CodeRabbit loop
   - ssh-fixer: #172 → preflight check implementation + PR
   - symlink-fixer: #171 → file copier fix + PR

3. Sequential iteration
   - Monitor agent completion
   - Relay CodeRabbit findings to agents via SendMessage
   - Fix remaining findings manually when agents completed
   - Merge PRs in dependency order

4. Bonus bug (#186)
   - Discovered while reading deploy code for #170
   - Red test → green fix → PR → merge
```

## What Worked

**Worktree isolation via `isolation: "worktree"` on Agent calls.** Each agent got its own `.claude/worktrees/agent-<id>` directory with a separate git branch. No file conflicts between agents. The key is that worktrees live under the project directory (not `/tmp/`), so the sandbox recognizes them.

**Grouping by spec surface.** Bugs #170 and #173 both touched the reconcile spec, so bundling them into one proposal avoided a merge conflict on `spec.md`. The two direct bug fixes (#171, #172) were fully independent — no coordination needed.

**CodeRabbit as a quality gate.** Each PR went through 2–5 CodeRabbit rounds. The tool caught real issues: macOS symlink path normalization in tests, duplicate test cases, heading hierarchy in docs, absolute vs relative paths in `WrittenFiles`. Without it, the `DeployLocalFile` absolute path bug would have shipped.

**Red/green TDD for #186.** Writing the failing test first (`TestDeployLocalFullPath/written_files_have_infra-relative_paths_for_hook_matching`) made the fix obvious and gave confidence it was correct. The test reproduced the exact production scenario: `"configuration.yml"` failing to match `"appdata/authelia/**"`.

**`SendMessage` to relay CodeRabbit findings.** When agents were still running, sending them CodeRabbit findings via `SendMessage` let them fix issues without a new dispatch cycle.

## What Didn't Work

**Agents failed on `gh` auth.** The first round of agents all hit `Not logged in — Please run /login` when trying to push or create PRs. The session hadn't logged into `gh` yet. Recovery: user ran `/login`, then agents were re-dispatched. Future: `gh auth status` should be a preflight check before spawning agents that need GitHub access.

**CodeRabbit incremental review skips small commits.** After fixing findings and pushing, CodeRabbit sometimes didn't generate a new review — its incremental mode decided the changes were too small to re-review. This left PRs in `CHANGES_REQUESTED` state even though findings were addressed. The workaround was merging anyway after manual verification.

**One agent worktree disappeared.** The symlink-fixer's first worktree was auto-cleaned because it made no changes before failing on `gh` auth. The worktree cleanup is aggressive — if an agent fails before committing, all work is lost. The re-dispatch succeeded, but the first attempt was wasted.

**`PrefixLatest` needed iteration.** The initial fix for #186 missed two edge cases that CodeRabbit caught: (1) `DeployLocalFile` stored absolute paths that `filepath.Join` silently ignored, and (2) single-file targets needed `filepath.Dir(t.RelPath)` instead of `t.RelPath` as the prefix. Three rounds to get it right.

## Gotchas

- **`filepath.Join("prefix", "/absolute/path")`** returns `/absolute/path` — the prefix is silently dropped. Always store relative paths in `WrittenFiles`.
- **Content-hash sync and git-diff produce different path formats.** Hooks that work with git-diff (repo-relative) may fail with content-hash sync (staging-relative). The two code paths are not interchangeable.
- **Agent worktree branches default to `worktree-agent-<id>`** — agents must create or checkout the target branch themselves. The `isolation: "worktree"` parameter doesn't set the branch.
- **CodeRabbit's `CHANGES_REQUESTED` state persists** even after findings are fixed. GitHub's review API doesn't auto-dismiss when new commits land. This blocks merge if branch protection requires approved reviews.
- **`CLAUDE.md` is a symlink** — the `Edit` tool tracks modification times on the target (`AGENTS.md`). External modifications to the target cause `Edit` to refuse with "file modified since read." Always `Read` the target file immediately before editing.

## Recommendations

**Preflight before agent dispatch:** Check `gh auth status` and `git remote -v` before spawning agents that need GitHub or remote access. A 5-second check saves 2 minutes of failed agent time.

**Use subagents (not teams) for independent bugs.** These 4 bugs had zero inter-agent communication needs. The Agent tool with `isolation: "worktree"` gives the isolation benefits without the team coordination overhead. Reserve teams for work that requires debate or shared findings.

**Budget 3–5 CodeRabbit rounds for implementation PRs.** Spec-only PRs converge faster (2–3 rounds). Implementation PRs with tests consistently need more iterations because CodeRabbit catches test-specific issues (path normalization, duplicates, assertion consistency).

**Seed deploy-path tests with prior state.** After the #170 fix, `state.LastDeployedCommit` must be non-empty for the path-aware skip check to activate. Every test that exercises deploy-path logic needs `SaveState(stateFile, &DeployState{LastDeployedCommit: "aaa111"})`.

## Key Takeaways

- Worktree agents work reliably when paths are under `.claude/worktrees/` — the sandbox allows tool access. `/tmp/` paths fail.
- Group bugs by shared spec/file surface to avoid merge conflicts. Independent bugs get independent agents.
- Content-hash sync `WrittenFiles` paths need explicit prefixing — `CopyDirIfChanged` returns target-dir-relative paths, not repo-relative or staging-relative.
- CodeRabbit is a genuine quality gate, not just a style checker — it caught the `DeployLocalFile` absolute path bug that would have broken all single-file hook matching.
- Red/green TDD is the fastest path from "I think this is the bug" to "I know the fix is correct."
