# GitOps Runner for Unraid Infrastructure
# Polls git repo and deploys on changes, also accepts webhooks

FROM alpine:3.21

# Build args
ARG SOPS_VERSION=3.9.2
ARG AGE_VERSION=1.2.0
ARG CHEZMOI_VERSION=2.53.1
ARG WEBHOOK_VERSION=2.8.2

# Install base packages
RUN apk add --no-cache \
    bash \
    curl \
    docker-cli \
    docker-compose \
    git \
    jq \
    openssh-client \
    rsync \
    tini \
    tzdata

# Install sops
RUN curl -fsSL "https://github.com/getsops/sops/releases/download/v${SOPS_VERSION}/sops-v${SOPS_VERSION}.linux.amd64" \
    -o /usr/local/bin/sops && chmod +x /usr/local/bin/sops

# Install age
RUN curl -fsSL "https://github.com/FiloSottile/age/releases/download/v${AGE_VERSION}/age-v${AGE_VERSION}-linux-amd64.tar.gz" \
    | tar -xz -C /tmp && mv /tmp/age/age /usr/local/bin/ && mv /tmp/age/age-keygen /usr/local/bin/ \
    && rm -rf /tmp/age

# Install chezmoi (musl version for Alpine)
RUN curl -fsSL "https://github.com/twpayne/chezmoi/releases/download/v${CHEZMOI_VERSION}/chezmoi_${CHEZMOI_VERSION}_linux-musl_amd64.tar.gz" \
    | tar -xz -C /usr/local/bin chezmoi

# Install webhook
RUN curl -fsSL "https://github.com/adnanh/webhook/releases/download/${WEBHOOK_VERSION}/webhook-linux-amd64.tar.gz" \
    | tar -xz -C /tmp && mv /tmp/webhook-linux-amd64/webhook /usr/local/bin/ \
    && rm -rf /tmp/webhook-linux-amd64

# Create app user and directories
RUN adduser -D -h /app gitops && \
    mkdir -p /app/repo /app/staging /app/backups /app/logs /config && \
    chown -R gitops:gitops /app /config

# Copy scripts
COPY --chmod=755 scripts/ /app/scripts/

# Copy webhook config
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

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD /app/scripts/healthcheck.sh

EXPOSE 9000

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["/app/scripts/entrypoint.sh"]
