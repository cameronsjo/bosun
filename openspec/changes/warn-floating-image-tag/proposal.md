# Change: Warn when a service sits on a floating image tag

## Why

A service whose `image:` is `:latest` (or untagged) never redeploys when a new
image is published: the compose file does not change, and `docker compose up -d`
does not pull an image that is already present. The deploy reports success and
the old container keeps running. `bosun trigger -f` does not help for the same
reason. Nothing in a run says so (cameronsjo/bosun#648).

## What Changes

- A local deploy SHALL log a warning, naming each affected service and image,
  when a compose service uses a floating tag and no pull policy re-reads it.
  The warning names the fix: `pull_policy: always` or a digest pin.
- Behavior is unchanged otherwise. Bosun does not pull or resolve digests; the
  per-service pull opt-in stays with `add-image-update-policy`.

## Impact

- Affected specs: `reconcile` (ADDED: Floating Image Tag Warning).
- Affected code: `internal/reconcile/compose_select.go`.
