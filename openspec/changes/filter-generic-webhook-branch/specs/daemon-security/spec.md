## ADDED Requirements

### Requirement: Generic webhook branch filter

The generic `/webhook` handler SHALL authorize a request before reading its
payload. After authorization it SHALL decide whether to start a reconcile:

- When an `X-GitHub-Event` header is present and is not `push`, the request
  SHALL be ignored.
- When the body is a JSON object with a top-level `ref` key, a reconcile SHALL
  start only when `ref` is a string equal to `refs/heads/<tracked branch>`.
  Any other value, including another branch, a tag ref, an empty string, a
  bare branch name, or a non-string value, SHALL be ignored.
- When the body carries no `ref` (empty, not JSON, not a JSON object, or an
  object without the key), a reconcile SHALL start as before, unless an
  `X-GitHub-Event` header is present. A GitHub push always carries a ref, so a
  GitHub push without a readable JSON `ref` (for example a form-encoded hook)
  SHALL be ignored.

An ignored request SHALL receive `200` with status `ignored` and a reason, SHALL
be logged with the sanitized ref or event type, and SHALL NOT start a reconcile
or increment the generic webhook trigger metric.

#### Scenario: Push to the tracked branch

- **WHEN** an authorized generic webhook body has `ref` `refs/heads/main` and the tracked branch is `main`
- **THEN** the response is `202` and a reconcile starts

#### Scenario: Push to another branch

- **WHEN** an authorized generic webhook body has `ref` `refs/heads/develop` and the tracked branch is `main`
- **THEN** the response is `200` with status `ignored` and no reconcile starts

#### Scenario: Tag push

- **WHEN** an authorized generic webhook body has `ref` `refs/tags/main`
- **THEN** the response is `200` with status `ignored` and no reconcile starts

#### Scenario: Body without a ref

- **WHEN** an authorized generic webhook body is empty, not JSON, or a JSON object without `ref`
- **THEN** the response is `202` and a reconcile starts

#### Scenario: GitHub push without a JSON ref

- **WHEN** an authorized generic webhook carries `X-GitHub-Event: push` and a form-encoded or ref-less body
- **THEN** the response is `200` with status `ignored` and no reconcile starts

#### Scenario: Non-push GitHub event

- **WHEN** an authorized generic webhook carries `X-GitHub-Event: issues` or `ping`
- **THEN** the response is `200` with status `ignored` and no reconcile starts

#### Scenario: Unauthorized request with a matching ref

- **WHEN** a webhook secret is configured and a request with `ref` `refs/heads/main` carries an invalid signature
- **THEN** the response is `401` and the payload decision is never reached
