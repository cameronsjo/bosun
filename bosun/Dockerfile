# Bosun - GitOps for Docker Compose
# Single binary daemon that handles webhooks, polling, and reconciliation
#
# Build: This Dockerfile is used by goreleaser with pre-built binary.
#        For standalone builds, use `make build` then copy binary here.
#
# Size optimizations applied:
# - Alpine base image (~5MB)
# - Single RUN layer for all package installations
# - Single RUN layer for user/directory setup
# - No build dependencies in final image (binary is pre-built)
# - ca-certificates for HTTPS, tzdata for timezone support
#
# Required runtime dependencies:
# - docker-cli + docker-cli-compose: Container orchestration
# - git: Repository operations (diagnostics, init wizard)
# - openssh-client: SSH key authentication for git
# - tini: Proper PID 1 signal handling
# - ca-certificates: HTTPS/TLS connections
# - tzdata: Timezone support
#
# NOT required (handled by Go libraries):
# - sops: Decryption uses go-sops library in-process
# - age: Key handling done by go-sops

FROM alpine:3.21

# Layer 1: Install all runtime dependencies in single layer
# Order: Most frequently changed last (none change often here)
# Using --no-cache to avoid caching apk index (smaller image)
RUN apk add --no-cache \
    ca-certificates \
    docker-cli \
    docker-cli-compose \
    git \
    openssh-client \
    tini \
    tzdata

# Layer 2: Create non-root user and required directories
# Home at /home/bosun for SSH config compatibility (mounts to ~/.ssh)
# Combined into single RUN to minimize layers
RUN addgroup -g 1000 bosun && \
    adduser -D -u 1000 -G bosun -h /home/bosun -s /sbin/nologin bosun && \
    mkdir -p /app/repo /app/staging /app/backups /app/logs /config /home/bosun/.ssh /var/lib/bosun /var/run/bosun && \
    chown -R bosun:bosun /app /config /home/bosun /var/lib/bosun /var/run/bosun && \
    chmod 0700 /var/lib/bosun

# Layer 3: Copy the Go binary (built by goreleaser or make build)
# --chmod=755 ensures executable permissions without extra layer
COPY --chmod=755 bosun /usr/local/bin/bosun

# Set working directory and switch to non-root user
WORKDIR /app
USER bosun

# Environment defaults
# BOSUN_ prefix for all app-specific config (per .claude/rules/standards.md)
# PORT without prefix for common HTTP server convention
ENV TZ=America/Chicago \
    BOSUN_REPO_URL="" \
    BOSUN_REPO_BRANCH="main" \
    BOSUN_POLL_INTERVAL=3600 \
    PORT=8080 \
    WEBHOOK_SECRET="" \
    DISCORD_WEBHOOK_URL="" \
    DRY_RUN="false"

# Health check via the daemon's /health endpoint
# Using wget (included in busybox) instead of curl to avoid extra dependency
HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 \
    CMD wget -q -O /dev/null http://localhost:${PORT:-8080}/health || exit 1

EXPOSE 8080

# Use tini as init process for proper signal handling
ENTRYPOINT ["/sbin/tini", "--"]
CMD ["bosun", "daemon"]
