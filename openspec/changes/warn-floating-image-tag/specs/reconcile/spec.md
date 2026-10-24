## ADDED Requirements

### Requirement: Floating Image Tag Warning

A local deploy SHALL log a warning at the default log level for each compose
service whose `image` has no tag or the tag `latest` (and no digest), unless the
service's `pull_policy` is one that pulls (`always`, `daily`, `weekly`,
`every_<duration>`, `build`). The warning SHALL name each service and image and
SHALL state that a new image will not be deployed and that `pull_policy: always`
or a digest pin fixes it. Images containing `${...}` interpolation are not
judged. The warning SHALL NOT change what the deploy does.

#### Scenario: Service on :latest

- **WHEN** a compose file declares a service with `image: app:latest` and no `pull_policy`
- **THEN** the deploy logs a warning naming that service and image

#### Scenario: Service pulls on every up

- **WHEN** the same service sets `pull_policy: always`
- **THEN** no floating-tag warning is logged for it

#### Scenario: Pinned image

- **WHEN** a service uses `image: app:1.2.3` or an `@sha256:` digest
- **THEN** no floating-tag warning is logged for it
