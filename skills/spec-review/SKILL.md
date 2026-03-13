---
name: spec-review
description: "Iterate on CodeRabbit spec PR reviews until convergence, then label ready-to-build"
---

Automate the spec review cycle for open spec PRs. Fetch CodeRabbit findings, fix issues, iterate until convergence, and label PRs `ready-to-build`.

## Workflow

### 1. Discover Spec PRs

If PR numbers were provided as arguments, use those. Otherwise, find all open PRs matching `spec/*` branches:

```bash
gh pr list --state open --head "spec/" --json number,headRefName,title,labels,updatedAt
```

If no spec PRs are found, report that and stop.

### 2. For Each PR — Assess Review Status

For each spec PR, gather:

- **CodeRabbit review status**: Check PR reviews and inline comments
- **Actionable comment count**: Comments that are not resolved or outdated
- **Last review timestamp**: When CodeRabbit last reviewed
- **Current labels**: Whether `ready-to-build` is already applied

```bash
# Get PR reviews
gh pr view <number> --json reviews,comments,labels,headRefName

# Get inline review comments with file paths
gh api repos/{owner}/{repo}/pulls/<number>/comments --jq '.[] | {path: .path, body: .body, line: .line, created_at: .created_at, in_reply_to_id: .in_reply_to_id}'
```

### 3. Present Summary

Display a summary table showing the status of each spec PR:

| PR | Branch | Title | Actionable | Last Review | Status |
|----|--------|-------|------------|-------------|--------|
| #42 | spec/add-alerts | Add alert providers | 3 | 2h ago | Needs fixes |
| #43 | spec/update-drift | Update drift detection | 0 | 1h ago | Converged |

### 4. Fix Findings

For PRs with actionable findings, offer to dispatch worktree-isolated agents (one per PR) to:

1. Check out the spec branch in a worktree
2. Read each CodeRabbit comment with file path and suggested fix
3. Apply fixes to the spec files
4. Commit and push the fixes

Use the Agent tool with `isolation: "worktree"` to prevent cross-PR conflicts.

After fixes are pushed, CodeRabbit will automatically re-review. Wait ~5 minutes for the review. If no review appears, suggest triggering manually:

```bash
gh pr comment <number> --body "@coderabbitai review"
```

### 5. Iterate

Re-run the assessment (step 2) after each fix round. Continue until:

- **Converged**: 0 actionable comments, or only stale/repeated findings
- **Stalled**: Same findings persist after 2 fix rounds (escalate to user)

### 6. Label `ready-to-build`

When a PR converges, apply the label:

```bash
gh pr edit <number> --add-label "ready-to-build"
```

Report final status for all PRs.

## Convergence Criteria

A spec PR is converged when ANY of:

- CodeRabbit leaves 0 new comments on the latest push
- All remaining comments are resolved or are repeats of previously addressed findings
- The review summary shows no actionable items

## Rate Limiting

CodeRabbit may rate-limit reviews on rapid successive pushes. If a review doesn't appear within ~5 minutes after a push:

1. Check if the PR has a pending review status
2. Trigger manually with `@coderabbitai review` as a PR comment
3. Wait another 5 minutes before escalating

## Usage Examples

```bash
# Review all open spec PRs
/spec-review

# Review specific PRs
/spec-review 42 43

# Review a single PR
/spec-review 42
```
