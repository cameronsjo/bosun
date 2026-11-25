## ADDED Requirements

### Requirement: Template Function Allowlist

The Sprig functions available to reconcile templates SHALL be limited to an
explicit allowlist. The allowlist SHALL exclude every Sprig function that reads
the process environment or performs network access (`env`, `expandenv`,
`getHostByName`), and a Sprig function not on the allowlist SHALL NOT be
available to templates. A template that calls an excluded function SHALL fail
to parse, and no output file SHALL be written for it.

#### Scenario: Environment access refused

- **WHEN** a `.tmpl` file calls `{{ env "BOSUN_GIT_TOKEN" }}` or
  `{{ expandenv "$BOSUN_GIT_TOKEN" }}`
- **THEN** rendering fails with an undefined-function parse error naming the function
- **AND** no output file is created for that template

#### Scenario: Allowlisted Sprig functions still available

- **WHEN** a `.tmpl` file uses `{{ .name | default "x" | upper }}` or
  `{{ now | date "2006" }}`
- **THEN** the rendered output contains the expected value
