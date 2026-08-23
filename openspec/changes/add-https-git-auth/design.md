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
- Decision: treat the pair as valid only for an `https://` repository URL. If
  either value is missing, if the URL uses another transport, or if the URL
  contains userinfo, Bosun fails with an actionable error before a request is
  sent. Anonymous HTTPS remains valid when both values are unset.
- Decision: do not add unprefixed aliases. These variables are new, so there is
  no legacy compatibility contract; avoiding generic `GIT_TOKEN`/`GIT_USERNAME`
  names also prevents accidental credential capture from unrelated tooling.
- Decision: use one transport-neutral authentication resolver for clone and
  fetch so standalone and daemon reconciliation cannot diverge. Existing SSH
  agent/key resolution remains the fallback for SSH URLs when HTTPS
  credentials are absent.
- Decision: repository URLs are sanitized before presentation, and Git
  transport errors are sanitized before logging or returning. Tests inject
  recognizable secret values and require their absence from every observable
  error/log/status path.

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
