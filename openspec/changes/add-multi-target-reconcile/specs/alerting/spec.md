## MODIFIED Requirements

### Requirement: Reconciliation Lifecycle Alerts

The alert system SHALL provide convenience methods for reconciliation lifecycle events with pre-formatted messages:

- **Deploy Success**: title "Deployment Successful", severity info, source "reconcile", includes short commit (first 8 chars) and target name
- **Deploy Failure**: title "Deployment Failed", severity error, source "reconcile", includes short commit, target name, and error reason
- **Deploy Recovery**: title "Deployment Recovered", severity info, source "reconcile", includes short commit, target name, and count of prior failures
- **Rollback Success**: title "Rollback Successful", severity warning, source "reconcile", includes target name and backup name
- **Rollback Failure**: title "CRITICAL: Rollback Failed", severity critical, source "reconcile", includes target name, error reason, and "Manual intervention required!" message
- **Unhealthy Containers**: title "Unhealthy Containers Detected", severity warning, source "reconcile", includes target name and comma-separated container names
- **Drift Detected**: title "Drift Detected", severity warning, source "drift", includes target name and comma-separated drift items
- **Drift Resolved**: title "Drift Resolved", severity info, source "drift", includes target name and comma-separated resolved item keys
- **Doctor Alert**: severity-dependent title (critical: "CRITICAL: Health Check Failed", error: "Health Check Errors", warning: "Health Check Warnings", info: "Health Check Complete"), source "doctor", message is newline-joined issues

When multiple targets are configured, each alert SHALL clearly identify which target it pertains to. The target name SHALL appear in both the alert title (e.g., "Deployment Successful [unraid]") and the metadata fields, so operators can filter and route alerts per target.

When only the implicit default target is configured, the target name SHALL be omitted from alert titles to preserve backwards-compatible alert formatting.

#### Scenario: Deploy success alert formatting

- **WHEN** a deploy success alert is sent for commit `abc123def456` to target `unraid`
- **THEN** the alert title is "Deployment Successful"
- **AND** the message contains the short commit `abc123de`
- **AND** metadata includes the full commit hash and target

#### Scenario: Multi-target alert includes target name in title

- **WHEN** multiple targets are configured
- **AND** a deploy failure alert is sent for target `pi`
- **THEN** the alert title is "Deployment Failed [pi]"
- **AND** the message body includes the target name

#### Scenario: Single default target omits target name from title

- **WHEN** only the implicit default target is configured
- **AND** a deploy success alert is sent
- **THEN** the alert title is "Deployment Successful" (no target suffix)
- **AND** alert formatting is identical to pre-multi-target versions

#### Scenario: Rollback failure triggers critical severity

- **WHEN** a rollback failure alert is sent
- **THEN** the alert severity is critical
- **AND** the message includes "Manual intervention required!"

#### Scenario: Short commits are not truncated

- **WHEN** a deploy success alert is sent for a 3-character commit `abc`
- **THEN** the message contains `abc` without truncation
