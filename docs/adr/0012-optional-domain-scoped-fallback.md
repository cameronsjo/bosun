# ADR-0012: Optional Domain with Scoped Fallback

## Status

Proposed

## Context

The `add-traefik-defaults` proposal introduces a project-level `domain` field in `bosun.yaml` that several features consume: Traefik's `defaultRule` for zero-config service routing, the `reverse-proxy` provision as a fallback for per-service `${domain}`, the `bosun upgrade traefik` command, `bosun doctor` diagnostics, and `bosun init` scaffolding.

The question: should `domain` be required or optional? If optional, how do downstream consumers behave when it's absent?

The provision system currently resolves `${var}` placeholders exclusively from a service manifest's `config` map. Adding a project-level fallback introduces a new resolution layer. A blanket merge of all project config keys into service variables risks surprising inheritance (e.g., a project-level `port: 8080` silently applying to a service that forgot to set its own port). A scoped approach limits fallback to explicitly blessed keys.

## Decision

`domain` in `bosun.yaml` is **optional**. When present, it serves as a fallback for the `reverse-proxy` provision's `${domain}` variable. The fallback uses **scoped injection** — only `domain` is merged from project config into the provision variable map, not arbitrary keys.

### Resolution order

1. Service `config.domain` (explicit per-service) — highest priority
2. `bosun.yaml` `domain` field (project-level fallback)
3. Unresolved — existing "missing variables: domain" error

### Consumer behavior when domain is absent

| Consumer | Behavior | Severity |
|----------|----------|----------|
| `defaultRule` (Traefik static config) | Omitted from generated config | Graceful skip |
| `reverse-proxy` provision | Error if service also lacks `config.domain` (today's behavior, no regression) | Hard error |
| `bosun upgrade traefik` | Skips `defaultRule` recommendation, suggests `bosun config set domain` | Info message |
| `bosun doctor` | Info: "defaultRule not configured (no domain in bosun.yaml)" | Info, not warning |
| `bosun init` | User can skip domain prompt; generates config without `defaultRule` | Graceful skip |

### Rate limiting

`default-ratelimit` middleware is **opt-in only** (not in the default middleware chain). Rate limits are context-dependent and a bad default — an API backend and a static site have very different thresholds. Available as a separate provision for services that want it.

### Upgrade command

`bosun upgrade` is a **top-level command with subcommands** (`bosun upgrade traefik`, future: `bosun upgrade provisions`, `bosun upgrade config`). This follows the existing pattern of `bosun crew`, `bosun yacht`, etc.

## Consequences

### Pros

- No breaking changes — existing configs without `domain` work exactly as before
- Services can drop `config.domain` boilerplate when a project-level domain is set
- Every consumer degrades gracefully: features that need domain are skipped, features that don't are unaffected
- Scoped fallback prevents accidental variable inheritance from project config
- `defaultRule` is purely additive — services with explicit `Host()` rules are unaffected

### Cons

- Scoped fallback is ad-hoc: only `domain` gets this treatment. If more project-level variables need fallback later, we'll need to decide whether to generalize or add more one-off injections
- Two places to set domain (service config vs project config) increases the surface area for confusion, though the precedence rule is simple: service wins
- Services that rely on project-level domain will break if `bosun.yaml` is removed or the field is deleted — but this is true of any config dependency

## Alternatives Considered

| Alternative | Why not |
|-------------|---------|
| Required domain | Forces all existing users to add a field to `bosun.yaml`. Breaking change for a convenience feature |
| Blanket merge of project config into service variables | Unpredictable inheritance. A typo in `bosun.yaml` (e.g., `port: 80`) could silently affect every service |
| Blanket merge with allowlist | Same mechanism as scoped fallback but pretends to be general-purpose. Over-engineering for a single key today |
| Domain only from secrets (not `bosun.yaml`) | Domain isn't a secret. Forces SOPS setup for basic routing. Blocks `bosun upgrade` from reading it |
| No project-level domain at all | Every service must set `config.domain`. Repetitive for mono-domain setups (the common case for homelabs) |

## References

- Proposal: `openspec/changes/add-traefik-defaults/proposal.md`
- Design: `openspec/changes/add-traefik-defaults/design.md`
- Traefik defaultRule docs: https://doc.traefik.io/traefik/providers/docker/#defaultrule
- Existing provision system: `openspec/specs/manifest-system/spec.md` (Provision System requirement)
