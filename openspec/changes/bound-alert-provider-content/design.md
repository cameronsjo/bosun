## Context

Bosun passes a shared alert model to providers, but Discord and Twilio impose
different limits. Discord validates individual embed components and a 6000
character aggregate. Twilio accepts long messages but bills and delivers them
as segments; one segment holds 160 GSM-7 septets or 70 UTF-16 code units.

## Goals / Non-Goals

- Goals: prevent Discord rejection from oversized content, bound Twilio cost to
  one segment, preserve readable leading context, and never split Unicode text.
- Non-goals: change the shared alert model, impose limits on email or generic
  webhook providers, or add configurable provider limits.

## Decisions

- Discord content is bounded while constructing the provider payload. Title,
  description, footer, field names, and field values respect their component
  limits, and the aggregate text never exceeds 6000 characters.
- Discord metadata keys are sorted before applying the remaining aggregate
  budget so the same alert produces the same bounded payload.
- Twilio chooses its one-segment budget from the complete formatted message. A
  message containing only GSM-7 characters uses a 160-septet budget, including
  two-septet extension-table characters. Any other character selects a
  70-UTF-16-code-unit budget. Truncation reserves room for an ASCII ellipsis.
- Truncation retains the prefix because severity, title, commit, target, and the
  beginning of the failure reason appear first in Bosun alert messages.

## Risks / Trade-offs

- A bounded provider payload omits trailing detail. The alert remains
  deliverable and retains identifying context; full failure detail remains in
  Bosun logs.
- Unicode grapheme clusters can contain multiple code points. The implementation
  prevents invalid UTF-8 and split UTF-16 surrogate pairs, but does not add a
  grapheme-segmentation dependency.

## Migration Plan

No configuration or persisted data changes are required. Existing alerts are
sent unchanged when they fit the provider's bound.
