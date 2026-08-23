## 1. Discord content bounds

- [ ] 1.1 Bound title, description, footer, metadata fields, field count, and
      total embed text before marshaling the request
- [ ] 1.2 Make metadata budgeting deterministic and truncation Unicode-safe
- [ ] 1.3 Add provider-request tests for individual, aggregate, Unicode, and
      exact-boundary behavior

## 2. Twilio segment bounds

- [ ] 2.1 Detect GSM-7 versus Unicode formatted messages and truncate to one
      provider segment without splitting Unicode
- [ ] 2.2 Add boundary tests for GSM-7, extension-table characters, BMP Unicode,
      and supplementary Unicode characters
- [ ] 2.3 Verify the bounded body is sent through the provider request path

## 3. Documentation and validation

- [ ] 3.1 Update alerting documentation and onboard skill resources with the
      provider-specific bounds
- [ ] 3.2 Update `llms.txt` if its alert-provider summary needs the new contract
- [ ] 3.3 Run focused and full tests, race tests, lint, vet, build, OpenSpec
      validation, and Cadence conformance
