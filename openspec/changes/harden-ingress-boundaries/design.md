## Context

The Unix socket uses peer identity, while external webhooks use signatures. Returning a signing secret grants trigger authority even on GET. A standalone receiver is itself an ingress boundary because its authorized socket forwarding does not repeat HTTP signature authorization.

## Decisions

Retain existing peer policy and opt-outs; protect `/config` with that same policy. Keep secret emission explicit in the response builder. Authenticate every standalone provider before processing. Sanitize presentation values after authentication too, stripping before length limits. Preserve #701 per-target health/status behavior and existing HTTPS URL redaction.

## Compatibility and residuals

Receiver UID must already be authorized to trigger; it must now also be authorized to fetch secrets. No listener, polling, target or transport authorization policy changes. Sanitization is presentation defense, not an input authorization decision. Existing opt-outs remain loudly logged.

## Verification

Regression tests cover authorized/unauthorized/missing peer credentials, explicit opt-out, all receiver providers, secret precedence, poisoned source strings and UTF-8-safe health caps. Staged control removals must fail the corresponding tests; restored code runs full race CI. Local compilation is gated by scripts/agent-go-gate.sh.
