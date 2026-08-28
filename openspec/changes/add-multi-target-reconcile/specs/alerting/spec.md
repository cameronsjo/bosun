## ADDED Requirements

### Requirement: Named-Target Alert Context

Reconciliation, rollback, health-gate, drift, and restart-breaker alerts owned by a named target SHALL identify that target without replacing the canonical lifecycle-alert delivery, throttling, interruption, rollback, or severity rules.

Alert helper APIs MAY receive the target as a positional value. For a named target, the visible title SHALL append ` [<target>]` and the message or fields SHALL retain target context for routing and diagnosis. The implicit or lone `default` target SHALL pass the legacy empty/`local` alert label so single-target titles remain unchanged. Target context SHALL NOT expose configuration secrets or be inferred from the number of configured targets.

#### Scenario: Named target adds alert context

- **WHEN** target `pi` owns a deployment failure
- **THEN** the canonical deployment-failure alert title is `Deployment Failed [pi]`
- **AND** the alert message or fields identify `pi`
- **AND** canonical failure throttling and delivery rules remain unchanged

#### Scenario: Default target preserves legacy title

- **WHEN** the implicit or lone `default` target owns a deployment success
- **THEN** the canonical title remains `Deployment Successful`
- **AND** no `[default]` suffix is added

#### Scenario: Sibling alerts remain attributable

- **WHEN** one target fails and a later target succeeds while the shared context remains live
- **THEN** each emitted lifecycle alert identifies only its owning named target
- **AND** neither alert is attributed to the sibling

#### Scenario: Interruption ownership remains canonical

- **WHEN** a named target is interrupted by propagated caller cancellation
- **THEN** exactly the canonical interruption owner sends the applicable target-context alert
- **AND** stage and rollback companions remain suppressed as required by the canonical alerting and reconcile specs
