## ADDED Requirements

### Requirement: Webhook branch-delete pushes are ignored

Every trigger endpoint that reads a push payload (the daemon's `/webhook` and
`/webhook/github`, and the standalone receiver's `/webhook`,
`/webhook/github`, `/webhook/gitlab`, and `/webhook/gitea`) SHALL ignore a
push that deletes a branch. A push deletes a branch when its JSON body has
`deleted` set to `true` or an `after` value made only of `0` characters. The
check SHALL run after authorization and after the branch filter.

An ignored delete SHALL receive `200` with status `ignored` and a reason,
SHALL be logged at warn level with the sanitized ref, and SHALL NOT be
forwarded to the daemon or start a reconcile. The receiver's Bitbucket
endpoint SHALL ignore a push whose every change has no `new` target, whether
or not a tracked branch is known.

#### Scenario: GitHub push deletes the tracked branch

- **WHEN** an authorized GitHub push has `ref` `refs/heads/main`, `deleted` `true`, and an all-zero `after`, and the tracked branch is `main`
- **THEN** the response is `200` with status `ignored`, a warning is logged, and no reconcile starts

#### Scenario: GitLab or Gitea push deletes the tracked branch

- **WHEN** an authorized GitLab or Gitea push has `ref` `refs/heads/main` and an all-zero `after`
- **THEN** the response is `200` with status `ignored` and nothing is forwarded

#### Scenario: Ordinary push to the tracked branch

- **WHEN** an authorized push to the tracked branch has `deleted` `false` and a non-zero `after`
- **THEN** it is forwarded or triggers a reconcile as before

### Requirement: Webhook delivery replay is dropped

The daemon's HTTP listener and the standalone receiver SHALL each keep an
in-memory record of two replay keys per authorized request: the delivery ID
and the SHA-256 of the raw request body, in separate key namespaces. The
delivery ID is the first non-empty header of `X-GitHub-Delivery`,
`X-Gitea-Delivery`, `webhook-id`, `Idempotency-Key`, `X-Gitlab-Event-UUID`,
and `X-Request-UUID`. `X-Gitlab-Webhook-UUID` SHALL NOT be used, because it
names the webhook configuration rather than a delivery. An empty body SHALL
have no body key.

Each trigger endpoint SHALL check the record only after its signature or token
check passes. The body key SHALL apply on token-authenticated endpoints
(GitLab) as well as signature-authenticated ones. A request with either key
already in the record SHALL receive `200` with status `ignored`, SHALL be
logged at warn level, SHALL NOT be forwarded or start a reconcile, and SHALL
NOT record its other key.

The record SHALL hold at most 1024 entries, SHALL forget an entry after 1
hour, SHALL drop its oldest entry when full, and SHALL store a fixed-size
digest of each key rather than the input. When forwarding or starting the
reconcile fails, the endpoint SHALL remove both keys from the record so the
provider's retry is processed.

#### Scenario: Repeated delivery

- **WHEN** an authorized push with delivery ID `d-1` is accepted and the same request is sent again within an hour
- **THEN** the second response is `200` with status `ignored` and only one reconcile is triggered

#### Scenario: Repeated delivery with a bad signature

- **WHEN** a request repeats a recorded delivery ID but carries an invalid signature
- **THEN** the response is `401`, not `ignored`

#### Scenario: Same body with a changed delivery header

- **WHEN** an authorized push is accepted and the same body is sent again with a different or missing delivery ID
- **THEN** the second response is `200` with status `ignored`

#### Scenario: Distinct pushes to the same branch

- **WHEN** two authorized pushes to the tracked branch have different bodies and different delivery IDs
- **THEN** both are processed

#### Scenario: Plain signed trigger

- **WHEN** an authorized empty-body request is sent to the generic endpoint twice
- **THEN** both trigger a reconcile

#### Scenario: Failed forward is retried

- **WHEN** the receiver fails to forward a delivery to the daemon and the provider retries the same body
- **THEN** the retry is forwarded

#### Scenario: Body-only repeat is logged as such

- **WHEN** a request is refused because only its body key matched
- **THEN** the warning log carries `match=body` instead of an empty delivery ID

#### Scenario: Entry expires

- **WHEN** a delivery ID and body were recorded more than an hour ago
- **THEN** a request with that ID and body is processed again

### Requirement: Manual trigger does not accept webhook bodies

The daemon's `/webhook/manual` accepts the same HMAC as `/webhook` and
`/webhook/github`, so it SHALL NOT be usable to replay their bodies. After
authorization it SHALL decode its body strictly: an empty body, or one JSON
object with only `force`, `source`, and `nonce`. Any other body, including a
push payload, SHALL receive `400` and SHALL NOT start a reconcile.

It SHALL then look up the body's digest in the replay record without recording
anything. A body already in the record SHALL receive `409` with a message that
names the `nonce` field, SHALL be logged at warn level, and SHALL NOT start a
reconcile. Manual triggers SHALL NOT be recorded, so a manual body stays
repeatable; a caller retriggers with a body a webhook endpoint used by adding
a fresh `nonce`.

#### Scenario: Captured push body sent to the manual endpoint

- **WHEN** a signed GitHub push body accepted by `/webhook/github` is sent to `/webhook/manual` with the same signature under `X-Signature`
- **THEN** the response is `400` and no reconcile starts

#### Scenario: Manual-shaped body already handled by a webhook endpoint

- **WHEN** `/webhook` accepted the body `{"source":"cron"}` and the same signed body is sent to `/webhook/manual`
- **THEN** the response is `409` and no reconcile starts

#### Scenario: Manual retrigger with a nonce

- **WHEN** the same request is sent with a fresh `nonce` field
- **THEN** the response is `202`

#### Scenario: Repeated manual trigger

- **WHEN** the same manual body that no webhook endpoint recorded is sent twice
- **THEN** both are accepted
