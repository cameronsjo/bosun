#!/bin/bash
# GitOps Runner entrypoint
# Starts webhook server and polling loop
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOG_FILE="/app/logs/runner.log"

log() {
    echo "[$(date -Iseconds)] $1" | tee -a "$LOG_FILE"
}

# Validate required environment
validate_env() {
    local missing=()

    [[ -z "${REPO_URL:-}" ]] && missing+=("REPO_URL")
    [[ ! -f "${SOPS_AGE_KEY_FILE:-/config/age-key.txt}" ]] && missing+=("SOPS_AGE_KEY_FILE (or /config/age-key.txt)")

    if [[ ${#missing[@]} -gt 0 ]]; then
        log "ERROR: Missing required configuration: ${missing[*]}"
        exit 1
    fi

    # Set SOPS key file if not set
    export SOPS_AGE_KEY_FILE="${SOPS_AGE_KEY_FILE:-/config/age-key.txt}"
}

# Setup SSH for git operations
setup_ssh() {
    if [[ -f "/config/deploy-key" ]]; then
        log "Setting up SSH deploy key..."
        mkdir -p ~/.ssh
        cp /config/deploy-key ~/.ssh/id_ed25519
        chmod 600 ~/.ssh/id_ed25519
        ssh-keyscan github.com >> ~/.ssh/known_hosts 2>/dev/null
    fi

    # Configure git
    git config --global user.email "gitops@homelab.local"
    git config --global user.name "GitOps Runner"
}

# Polling loop
start_polling() {
    log "Starting polling loop (interval: ${POLL_INTERVAL}s)..."

    while true; do
        sleep "$POLL_INTERVAL"
        log "Polling for changes..."
        "$SCRIPT_DIR/reconcile.sh" 2>&1 | tee -a "$LOG_FILE" || log "Reconciliation failed"
    done
}

# Main
main() {
    log "=== GitOps Runner starting ==="
    log "  Repo: ${REPO_URL:-not set}"
    log "  Branch: ${REPO_BRANCH:-main}"
    log "  Poll interval: ${POLL_INTERVAL:-3600}s"
    log "  Target: ${DEPLOY_TARGET:-unraid}"
    log "  Dry run: ${DRY_RUN:-false}"

    validate_env
    setup_ssh

    # Initial reconciliation
    log "Running initial reconciliation..."
    FORCE=true "$SCRIPT_DIR/reconcile.sh" 2>&1 | tee -a "$LOG_FILE" || log "Initial reconciliation failed"

    # Start webhook server in background if secret is set
    if [[ -n "${WEBHOOK_SECRET:-}" ]]; then
        log "Starting webhook server on port ${WEBHOOK_PORT:-9000}..."
        webhook -hooks /config/hooks.yaml -port "${WEBHOOK_PORT:-9000}" -verbose &
    else
        log "No WEBHOOK_SECRET set, webhook server disabled"
    fi

    # Start polling loop (foreground)
    start_polling
}

main "$@"
