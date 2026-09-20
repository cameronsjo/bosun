# Change: Report a reconcile that renders without secrets

## Why

`decryptSecrets` returns an empty map when no secrets file is configured. That
is correct — a repository with no secrets is valid — but it happens silently in
two ways that mislead:

- The skip logs at **Debug**, which the default level (`info`) hides. Nothing in
  a normal run says secrets were never read, while the rendered output looks
  complete.
- `ui.Info("Decrypting secrets...")` prints **before** the check, so the console
  actively claims a decryption that does not happen.

Found by the Opus security review gating the upgrade canary's first live run
(#680). The canary renders the same commit twice and compares the trees. Two
trees that both decrypted nothing compare equal, so it reports
`RENDER-IDENTICAL` and that reads as a passed comparison. The canary does not
parse this warning — the comparison is a tree diff — so the log line is the only
thing that tells the operator, which is why it has to be visible by default.
Any other consumer that treats a clean render as evidence the secrets path
worked has the same gap.

## What Changes

- The no-secrets skip SHALL be reported at a level visible under the default log
  level, and SHALL name `BOSUN_SECRETS_FILE` so the message says what to set.
- The "Decrypting secrets..." line SHALL NOT print on a run that decrypts
  nothing — the skip is announced before it, not after.
- Behavior is unchanged: an empty list still returns an empty map without error.
  This is a report, not a refusal.

## Impact

- Affected specs: `reconcile` — MODIFIED: Secret Decryption (one new paragraph,
  the existing "No secrets files configured" scenario gains two clauses, and one
  scenario is added for the control case).
- Affected code: `internal/reconcile/reconcile.go` — `decryptSecrets`, the
  ordering of the `ui.Info` call and the empty-list check.
- Consumers: every reconcile path reaches `decryptSecrets`; the visible change
  is one warning line on runs that were already rendering without secrets.
- No new env vars, no config surface, no exit-code change.

## Scope

Spec delta and implementation ship together. This is a single-clause change to
an existing requirement, not a new capability, so it does not go through the
separate spec-PR cycle — see the PR body for that call.
