# GitOps Runner for Unraid Infrastructure
# Polls git repo and deploys on changes, also accepts webhooks

FROM alpine:3.21 AS base

ARG WEBHOOK_VERSION=2.8.2

# Install minimal runtime dependencies
RUN apk add --no-cache \
    bash \
    curl \
    docker-cli \
    docker-cli-compose \
    openssh-client \
    tini \
    tzdata

# Install webhook (for GitHub/manual triggers)
RUN curl -fsSL "https://github.com/adnanh/webhook/releases/download/${WEBHOOK_VERSION}/webhook-linux-amd64.tar.gz" \
    | tar -xz -C /tmp && mv /tmp/webhook-linux-amd64/webhook /usr/local/bin/ \
    && rm -rf /tmp/webhook-linux-amd64

# Create app user and directories
RUN adduser -D -h /app gitops && \
    mkdir -p /app/repo /app/staging /app/backups /app/logs /config && \
    chown -R gitops:gitops /app /config

# Copy the Go binary (built externally)
# Build with: go build -o bosun ./cmd/bosun
COPY --chmod=755 bosun /usr/local/bin/bosun

# Copy entrypoint and webhook config
COPY --chmod=755 scripts/entrypoint.sh /app/entrypoint.sh
COPY hooks.yaml /config/hooks.yaml

WORKDIR /app
USER gitops

# Environment defaults
ENV TZ=America/Chicago \
    REPO_URL="" \
    REPO_BRANCH="main" \
    POLL_INTERVAL=3600 \
    DEPLOY_TARGET="unraid" \
    WEBHOOK_PORT=9000 \
    WEBHOOK_SECRET="" \
    DISCORD_WEBHOOK_URL="" \
    DRY_RUN="false"

# Health check using bosun doctor
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD bosun doctor --quiet || exit 1

EXPOSE 9000

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["/app/entrypoint.sh"]
