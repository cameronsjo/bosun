## 1. Discord content bounds

- [ ] 1.1 Bound title, description, footer, metadata fields, field count, and
      total embed text before marshaling the request, using the specified
      component priority and tentative pair budgeting
- [ ] 1.2 Make metadata ordering deterministic and truncation Unicode-safe with
      conservative UTF-16-unit accounting
- [ ] 1.3 Add provider-request tests for every individual limit, empty metadata,
      the 25-field limit, aggregate priority, skipped oversized pairs, BMP and
      supplementary Unicode, exact boundaries, and unchanged in-bound content

## 2. Twilio segment bounds

- [ ] 2.1 Detect GSM-7 versus Unicode formatted messages and truncate to one
      provider segment without splitting Unicode
- [ ] 2.2 Add boundary tests for ASCII and non-ASCII GSM-7 default characters,
      every extension-table character, BMP Unicode, supplementary Unicode, and
      a non-GSM character beyond the retained prefix
- [ ] 2.3 Verify the bounded body is sent through the provider request path

## 3. Documentation and validation

- [ ] 3.1 Update alerting documentation and onboard skill resources with the
      provider-specific bounds, and correct the provider/severity table so it
      agrees that Twilio skips info and warning alerts
- [ ] 3.2 Update `llms.txt` if its alert-provider summary needs the new contract
- [ ] 3.3 Run focused and full tests, race tests, lint, vet, build, OpenSpec
      validation, and Cadence conformance
