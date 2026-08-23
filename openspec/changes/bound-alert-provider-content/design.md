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
  limits, and the aggregate text never exceeds 6000 provider units.
- Discord's documentation defines limits in characters but does not define an
  encoding unit. Bosun conservatively measures Discord budgets in UTF-16 code
  units, so a supplementary Unicode code point consumes two units and cannot be
  underestimated relative to a Unicode-scalar count. Truncation never splits a
  code point and uses the ASCII ellipsis `...`, whose three units are included
  in every limit.
- Discord first applies every individual component limit, then applies the 6000
  aggregate budget in this fixed priority: title, description, footer, and
  metadata fields in sorted-key order. A component that exceeds the remaining
  budget is prefix-truncated with the ellipsis. Metadata fields are evaluated
  as complete name/value pairs against a tentative budget, with the bounded name
  evaluated before the bounded value; a pair is omitted when both non-empty
  components cannot fit, and later fields continue to be considered. Empty
  metadata keys and values are omitted before the 25-field limit is applied.
- Twilio chooses its one-segment budget from the complete formatted message. A
  message containing only characters from the full GSM 03.38 default and
  extension alphabets uses a 160-septet budget. Default-alphabet characters,
  including non-ASCII characters such as `é` and `Δ`, cost one septet. The
  extension characters form feed, `^`, `{`, `}`, `\`, `[`, `~`, `]`, `|`, and
  `€` cost two septets. Any other character selects a 70-UTF-16-code-unit
  budget. Truncation reserves three units for the ASCII ellipsis `...`.
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

No configuration or persisted data changes are required. Component text that
fits the provider's bounds is sent unchanged. Discord metadata field order
becomes stable sorted-key order; oversized Discord and Twilio payloads are the
only payload text intentionally shortened by this change.

## Provider References

- Discord Message Resource, "Embed Limits":
  <https://docs.discord.com/developers/resources/message#embed-object-embed-limits>
- Twilio, "How long can a message be?":
  <https://www.twilio.com/docs/glossary/what-sms-character-limit>
- Twilio, "What is GSM-7 character encoding?":
  <https://www.twilio.com/docs/glossary/what-is-gsm-7-character-encoding>
