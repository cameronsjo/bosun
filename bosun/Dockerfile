# Bosun - GitOps for Docker Compose
# Single binary daemon that handles webhooks, polling, and reconciliation

FROM alpine:3.21

# Install minimal runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    docker-cli \
    docker-cli-compose \
    openssh-client \
    tini \
    tzdata

# Create app user and directories
RUN adduser -D -h /app bosun && \
    mkdir -p /app/repo /app/staging /app/backups /app/logs /config && \
    chown -R bosun:bosun /app /config

# Copy the Go binary (built by goreleaser)
COPY --chmod=755 bosun /usr/local/bin/bosun

WORKDIR /app
USER bosun

# Environment defaults (BOSUN_ prefix for all app config)
ENV TZ=America/Chicago \
    BOSUN_REPO_URL="" \
    BOSUN_REPO_BRANCH="main" \
    BOSUN_POLL_INTERVAL=3600 \
    PORT=8080 \
    WEBHOOK_SECRET="" \
    DISCORD_WEBHOOK_URL="" \
    DRY_RUN="false"

# Health check via the daemon's /health endpoint
HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 \
    CMD wget -q -O /dev/null http://localhost:${PORT:-8080}/health || exit 1

EXPOSE 8080

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["bosun", "daemon"]
