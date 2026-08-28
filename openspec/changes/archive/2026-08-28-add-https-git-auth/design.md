## Context

Bosun uses go-git for both clone and fetch. Its current authentication resolver
only recognizes SSH URLs, so private HTTPS repositories are attempted without
credentials. Embedding credentials in `BOSUN_REPO_URL` is not an acceptable
fallback because that URL is surfaced in debug/error logs, validation output,
and daemon status.

## Goals / Non-Goals

- Goals:
  - Authenticate private HTTPS clone and fetch operations with one consistent
    operator-controlled credential pair.
  - Fail before network I/O when credentials are incomplete or would be sent
    over an insecure or unrelated transport.
  - Keep all credentials out of logs, errors, diagnostics, and status output.
  - Preserve existing anonymous HTTPS and SSH behavior when HTTPS credentials
    are unset.
  - Prevent Basic credentials from following an unsafe redirect.
- Non-Goals:
  - Git credential helpers, `.netrc`, OAuth device flows, or provider-specific
    token discovery.
  - Bearer-token Git transports or plaintext HTTP credential transmission.
  - TLS verification opt-outs.

## Decisions

- Decision: use go-git's native HTTP Basic authentication with
  `BOSUN_GIT_USERNAME` as the username and `BOSUN_GIT_TOKEN` as the password.
  Both values are required together because Gitea and similar servers require
  an account name even when the password is a personal access token.
- Decision: treat the pair as valid only for an absolute `https://` repository
  URL with a non-empty host (scheme comparison is case-insensitive). If
  either value is missing, if the URL uses another transport, or if the URL
  contains userinfo, Bosun fails with an actionable error before a request is
  sent. Anonymous HTTPS remains valid when both values are unset.
- Decision: parse standard URLs and reject any non-nil URL userinfo component,
  including username-only and percent-encoded forms. SCP-like SSH URLs such as
  `git@example.com:owner/repo.git` are not URL userinfo and preserve existing
  SSH behavior when the HTTPS variables are unset. Error paths never quote the
  unsanitized repository URL.
- Decision: do not add unprefixed aliases. These variables are new, so there is
  no legacy compatibility contract; avoiding generic `GIT_TOKEN`/`GIT_USERNAME`
  names also prevents accidental credential capture from unrelated tooling.
  The pair applies to the effective repository URL after the existing
  `BOSUN_REPO_URL`-over-`REPO_URL` precedence rule; no `bosun.yaml` keys exist.
- Decision: use one transport-neutral authentication resolver for clone and
  fetch so standalone and daemon reconciliation cannot diverge. Existing SSH
  agent/key resolution remains the fallback for SSH URLs when HTTPS
  credentials are absent.
- Decision: invoke the same resolver from standalone command validation,
  daemon `ValidateConfig`, `bosun validate`, clone, and fetch. Daemon startup
  rejects unsafe configuration before listeners or background loops start;
  standalone reconcile rejects it before entering the pipeline. Clone and
  fetch still resolve immediately before network I/O so no later call site can
  bypass the guard.
- Decision: authenticated redirects are allowed only when every hop remains
  HTTPS and has the same case-insensitive hostname and effective port as the
  configured origin (omitted HTTPS port and explicit `:443` are equivalent).
  Cross-origin and HTTPS-to-HTTP redirects fail without forwarding the
  Authorization header. This is stricter than relying on a standard library
  redirect heuristic and makes the "HTTPS only" guarantee cover the complete
  request chain.
- Decision: credentials remain process-environment secrets. They are converted
  to a short-lived go-git BasicAuth value for an operation and are not copied
  into `reconcile.Config`, serialized state, project YAML, metrics, traces, or
  daemon responses. Operators rotate them by changing the process environment
  and restarting Bosun; project config hot reload neither reads nor changes
  them.
- Decision: repository URLs are sanitized before presentation, and Git
  transport errors are sanitized before logging or returning. A parseable URL
  is displayed without `URL.User`; an invalid URL that cannot be safely parsed
  is represented by a fixed redacted placeholder rather than echoed. Tests
  inject recognizable raw and percent-encoded userinfo, username/token values,
  and the resulting Basic Authorization value, and require their absence from
  every observable error/log/status path.

## Risks / Trade-offs

- Environment variables are visible to the Bosun process and its container
  configuration. This matches Bosun's existing secret configuration model, but
  operators must restrict container inspection access and rotate tokens.
- Rejecting URL-embedded credentials may break an undocumented workaround.
  The migration is explicit environment variables, which removes the larger
  risk of credentials appearing in logs and status APIs.
- HTTP Basic authentication depends on the Git server's HTTPS endpoint. Bosun
  intentionally does not add provider-specific behavior; operators supply the
  username their server expects and a token accepted as its password.
- Same-origin-only redirects may reject a hosting setup that redirects Git
  traffic to another hostname. The operator must configure the canonical HTTPS
  clone URL; silently forwarding credentials to the redirected host is not an
  acceptable fallback.
- Redacting the configured username as well as the token can remove a matching
  low-entropy word from an upstream error. Bosun favors non-disclosure and
  supplies its own stable authentication guidance rather than returning raw
  transport text that may stringify go-git's BasicAuth value.

## Migration Plan

1. Remove username/password userinfo from `BOSUN_REPO_URL`.
2. Set `BOSUN_GIT_USERNAME` and `BOSUN_GIT_TOKEN` in the Bosun environment.
3. Restart Bosun so the process receives the new environment.
4. Run a reconciliation and verify clone/fetch succeeds without credential
   values appearing in logs or daemon status.

Rollback removes both HTTPS credential variables and returns Bosun to anonymous
HTTPS or existing SSH authentication.

## Open Questions

None.
