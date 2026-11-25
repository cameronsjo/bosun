## MODIFIED Requirements

### Requirement: Template Rendering

The reconciler SHALL render Go `text/template` files (`.tmpl` extension) from
the infrastructure subdirectory to a staging directory. Non-template files SHALL
be copied as-is. Rendered output SHALL strip the `.tmpl` extension.

Template rendering SHALL use an explicit allowlist of Sprig functions plus
custom bosun functions: `include` (reads file contents) and `fromJsonFile`
(reads and parses JSON file). The allowlist SHALL exclude every Sprig function
that reads the process environment or performs network access (`env`,
`expandenv`, `getHostByName`), and a Sprig function not on the allowlist SHALL
NOT be available to templates. A template that calls an excluded function
SHALL fail to parse, and no output file SHALL be written for it.

Template data SHALL be the merged secrets map, accessible via `{{ .key }}` syntax.

Rendered files SHALL be written atomically: write to temp file, set permissions
(0644), then rename to final path. This prevents malformed output from partial
writes.

The staging directory SHALL be cleared before rendering to prevent stale files
from previous runs.

#### Scenario: Template rendered with secrets

- **WHEN** a `.tmpl` file references `{{ .network.unraid_ip }}`
- **AND** the secrets contain a `network.unraid_ip` value
- **THEN** the rendered output contains the interpolated value

#### Scenario: Non-template file copied verbatim

- **WHEN** a file without `.tmpl` extension exists in the source directory
- **THEN** it is copied to the staging directory without modification

#### Scenario: Atomic write prevents partial output

- **WHEN** template execution fails mid-render
- **THEN** no output file is created (temp file is cleaned up)
- **AND** previously rendered files from other templates are unaffected

#### Scenario: Environment access refused

- **WHEN** a `.tmpl` file calls `{{ env "BOSUN_GIT_TOKEN" }}` or
  `{{ expandenv "$BOSUN_GIT_TOKEN" }}`
- **THEN** rendering fails with an undefined-function parse error naming the function
- **AND** no output file is created for that template

#### Scenario: Allowlisted Sprig functions still available

- **WHEN** a `.tmpl` file uses `{{ .name | default "x" | upper }}` or
  `{{ now | date "2006" }}`
- **THEN** the rendered output contains the expected value
