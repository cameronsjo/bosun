## ADDED Requirements

### Requirement: Drift Ignore Rule Validation

Drift ignore rules SHALL be validated at configuration load time and SHALL reject undocumented `type` values and invalid glob patterns, rather than silently accepting them and over-suppressing drift.

Each ignore rule's `type` field SHALL be validated against the implemented drift-type enum (`missing`, `image_mismatch`, `unhealthy`) plus the literal `*` wildcard. An unknown `type` value SHALL produce a load-time error naming the offending rule, because such a rule would silently never match and leave drift unreported.

Each ignore rule's `service` and `type` glob patterns SHALL be validated with `filepath.Match`. A pattern that returns a match error (e.g., an unclosed character class like `[unclosed`) SHALL be rejected at load time rather than silently treated as a non-match.

A total-suppression rule, where both `service` and `type` are `*`, SHALL NOT be accepted silently. It SHALL surface as a load-time error (or, where an intentional full mute is supported, a loud startup warning), because it disables the entire drift system.

This validation SHALL apply to ignore rules from the `drift_ignore` config-file key and to the `BOSUN_DRIFT_IGNORE` environment-variable JSON override with identical semantics.

#### Scenario: Unknown drift type is rejected

- **WHEN** a `drift_ignore` rule specifies `type: stopped` (not an implemented drift type)
- **THEN** configuration load fails with an error identifying the invalid rule
- **AND** the daemon does not start with the malformed rule

#### Scenario: Invalid glob pattern is rejected

- **WHEN** a `drift_ignore` rule specifies `service: "[unclosed"`
- **THEN** configuration load fails with a glob-validation error
- **AND** the rule is not silently treated as a non-match

#### Scenario: Total-suppression rule is not silently accepted

- **WHEN** a `drift_ignore` rule specifies `{service: "*", type: "*"}`
- **THEN** configuration load surfaces it as an error or loud warning
- **AND** drift detection is not silently disabled without operator awareness

#### Scenario: Valid rules are accepted

- **WHEN** a `drift_ignore` rule specifies `{service: "traefik", type: "unhealthy"}`
- **THEN** configuration load succeeds
- **AND** the rule suppresses matching drift items at check time

#### Scenario: Env-var override is validated identically

- **WHEN** `BOSUN_DRIFT_IGNORE` contains a rule with an unknown `type` value
- **THEN** the override fails validation at load with the same error as the config-file path
