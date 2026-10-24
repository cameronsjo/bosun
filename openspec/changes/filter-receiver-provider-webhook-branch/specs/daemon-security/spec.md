## ADDED Requirements

### Requirement: Receiver provider webhook branch filter

The standalone `bosun webhook` receiver's provider endpoints
(`/webhook/github`, `/webhook/gitlab`, `/webhook/gitea`, `/webhook/bitbucket`)
SHALL forward a push only when it updates the tracked branch. Each endpoint
SHALL run its signature or token check and its event-type check first, and
SHALL decide on the branch only after both pass.

- GitHub, GitLab, and Gitea: the payload's `ref` SHALL equal
  `refs/heads/<tracked branch>`. Another branch, a tag ref, a bare branch
  name, or a missing or unreadable `ref` SHALL be ignored.
- Bitbucket: the push SHALL be forwarded when any `push.changes[]` entry has a
  `new` object with `type` `branch` and `name` equal to the tracked branch. A
  tag, a deletion (`new` is null), another branch, or an empty `changes` list
  SHALL be ignored.

An ignored push SHALL receive `200` with status `ignored` and a reason, SHALL
be logged with the sanitized ref and reason, and SHALL NOT be forwarded to the
daemon.

The receiver SHALL use the tracked branch resolved for the generic `/webhook`
(`--branch`, then `BOSUN_REPO_BRANCH`, then the daemon's `GET /config`). When
no branch is known, the provider endpoints SHALL NOT check the branch, and the
startup warning SHALL state that the ref filter is off.

#### Scenario: Provider push to the tracked branch

- **WHEN** the receiver tracks `main` and an authorized GitHub, GitLab, or Gitea push has `ref` `refs/heads/main`
- **THEN** the push is forwarded to the daemon and the response is `202`

#### Scenario: Provider push to another branch

- **WHEN** the receiver tracks `main` and an authorized GitHub, GitLab, or Gitea push has `ref` `refs/heads/develop`
- **THEN** the response is `200` with status `ignored` and nothing is forwarded

#### Scenario: Provider tag push

- **WHEN** the receiver tracks `main` and an authorized GitHub, GitLab, or Gitea push has `ref` `refs/tags/v1.2.3`
- **THEN** the response is `200` with status `ignored` and nothing is forwarded

#### Scenario: Bitbucket push to the tracked branch

- **WHEN** the receiver tracks `main` and an authorized Bitbucket push has a change whose `new` is `{"type":"branch","name":"main"}`
- **THEN** the push is forwarded to the daemon and the response is `202`

#### Scenario: Bitbucket tag or other branch

- **WHEN** the receiver tracks `main` and every change in an authorized Bitbucket push names a tag, another branch, or a deletion
- **THEN** the response is `200` with status `ignored` and nothing is forwarded

#### Scenario: Bad signature with a matching branch

- **WHEN** a provider push names the tracked branch but carries an invalid signature or token
- **THEN** the response is `401` and the branch decision is never reached

#### Scenario: Receiver with no known branch

- **WHEN** the receiver has no tracked branch
- **THEN** a provider push to any branch is forwarded, and startup logs that the ref filter is off
