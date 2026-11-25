# Change: Remove environment and network access from reconcile templates

## Why

Reconcile templates come from the GitOps repository and render inside the
bosun daemon, whose environment holds the Git token, API bearer tokens,
alert credentials, and often the age key (`SOPS_AGE_KEY`). Sprig's `env` and
`expandenv` let any `.tmpl` file render those values into deployed,
world-readable files, and `getHostByName` lets a template make DNS lookups
during a reconcile (cameronsjo/bosun#790). Template data is meant to come
from the decrypted secrets map, not the process environment.

## What Changes

- **BREAKING** Reconcile and `bosun render` templates SHALL be offered an
  explicit allowlist of Sprig functions that excludes `env`, `expandenv`,
  and `getHostByName`. A template calling one fails to parse and is not
  rendered.
- `bosun render` has no requirement of its own in the reconcile spec. The
  delta below covers reconcile templates; `bosun render` uses the
  same allowlisted function map, and that is enforced by code and tests only.
- Templates that read secrets with `fromJson (env "SOPS_SECRETS")` (a
  pattern earlier docs taught, which current bosun no longer populates)
  must read the data context directly: `{{ .apps.myapp.db_password }}`.
- Chart templates (`bosun provision`, chart-templating spec) are unchanged.

## Impact

- Affected specs: reconcile (new requirement Template Function Allowlist;
  Template Rendering is unchanged, and its Sprig sentence now reads through
  the allowlist)
- Affected code: `internal/reconcile/template.go`, `internal/cmd/render.go`
- Not closed here: compose `${VAR}` interpolation and repo-reloaded hooks
  also reach the daemon environment; tracked separately.
