## ADDED Requirements

### Requirement: Template Function Allowlist

Reconcile templates SHALL be offered only an explicit allowlist of Sprig
functions, which narrows the Sprig function library named in Template
Rendering. The allowlist SHALL start as the Sprig text function map minus
`env`, `expandenv`, and `getHostByName`, which read the process environment or
the network. A Sprig function not on the allowlist, including one Sprig adds
later, SHALL NOT be available. A template that calls one SHALL fail to parse,
and no output file SHALL be written for it.

#### Scenario: Environment access refused

- **WHEN** a `.tmpl` file calls `{{ env "BOSUN_GIT_TOKEN" }}` or
  `{{ expandenv "$BOSUN_GIT_TOKEN" }}`
- **THEN** rendering fails with an undefined-function parse error naming the function
- **AND** no output file is created for that template

#### Scenario: Network access refused

- **WHEN** a `.tmpl` file calls `{{ getHostByName "example.com" }}`
- **THEN** rendering fails with an undefined-function parse error naming the function
- **AND** no output file is created for that template
- **AND** no DNS lookup is made

#### Scenario: Unlisted Sprig function refused

- **WHEN** the Sprig library offers a function that is not on the allowlist
- **AND** a `.tmpl` file calls that function
- **THEN** rendering fails with an undefined-function parse error naming the function
- **AND** no output file is created for that template

#### Scenario: Allowlisted Sprig functions still available

- **WHEN** the secrets contain `name` with the value `web`
- **AND** a `.tmpl` file contains `{{ .name | default "x" | upper }}` and
  `{{ now | date "2006" }}`
- **THEN** the rendered output contains `WEB` and a four-digit year
