## MODIFIED Requirements

### Requirement: Secret Decryption

The reconciler SHALL decrypt SOPS-encrypted YAML files using the Age encryption
backend. Decryption SHALL use the go-sops library for in-process decryption
without requiring an external `sops` binary.

Age key discovery SHALL follow this order: `SOPS_AGE_KEY` environment variable,
`SOPS_AGE_KEY_FILE` environment variable, default path
`~/.config/sops/age/keys.txt`.

When multiple secret files are configured, the reconciler SHALL decrypt each file
independently and deep-merge the results. Later files SHALL override earlier
files for duplicate keys, with recursive merging for nested maps.

The reconciler SHALL validate that each file contains the `sops` metadata key
before attempting decryption, and SHALL sanitize decryption error messages to
prevent leaking sensitive information (partial keys, decrypted content).

When no secrets file is configured, the reconciler SHALL proceed without secrets
and SHALL report that it did so at a level visible under the default log level,
naming the variable that would configure one. It SHALL NOT announce that
decryption is starting on a run that decrypts nothing. A repository with no
secrets is valid, so this is a report and not a refusal.

#### Scenario: Single secret file decrypted

- **WHEN** one SOPS-encrypted YAML file is configured
- **THEN** it is decrypted and returned as a map of key-value pairs

#### Scenario: Multiple secret files merged

- **WHEN** two secret files are configured with overlapping keys
- **THEN** the second file's values override the first
- **AND** nested maps are merged recursively

#### Scenario: Missing age key

- **WHEN** no age key is found in any discovery location
- **THEN** decryption fails with an actionable error listing setup instructions

#### Scenario: Invalid SOPS file

- **WHEN** a configured secrets file lacks the `sops` metadata key
- **THEN** decryption fails with a message indicating the file is not SOPS-encrypted
- **AND** includes the `sops --encrypt` command to fix it

#### Scenario: No secrets files configured

- **WHEN** the secrets file list is empty
- **THEN** decryption returns an empty map without error
- **AND** a warning names `BOSUN_SECRETS_FILE` and states that rendering proceeds without secrets
- **AND** no message claims that decryption is starting

#### Scenario: Configured secrets file does not warn

- **WHEN** at least one secrets file is configured
- **THEN** the no-secrets warning is not emitted, whatever the outcome of decryption
