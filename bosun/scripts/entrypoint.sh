#!/bin/bash
# GitOps Runner entrypoint
# Starts webhook server and polling loop using bosun CLI
set -euo pipefail

LOG_FILE="/app/logs/runner.log"

log() {
    echo "[$(date -Iseconds)] $1" | tee -a "$LOG_FILE"
}

error() {
    log "ERROR: $1"
    exit 1
}

# Validate required environment
validate_env() {
    local missing=()

    [[ -z "${REPO_URL:-}" ]] && missing+=("REPO_URL")
    [[ ! -f "${SOPS_AGE_KEY_FILE:-/config/age-key.txt}" ]] && missing+=("SOPS_AGE_KEY_FILE (or /config/age-key.txt)")

    if [[ ${#missing[@]} -gt 0 ]]; then
        error "Missing required configuration: ${missing[*]}"
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
        ssh-keyscan gitlab.com >> ~/.ssh/known_hosts 2>/dev/null
    fi
}

# Run reconciliation
run_reconcile() {
    local args=()

    [[ "${DRY_RUN:-false}" == "true" ]] && args+=("--dry-run")
    [[ -n "${DEPLOY_TARGET:-}" ]] && args+=("--target" "$DEPLOY_TARGET")

    bosun reconcile "${args[@]}" 2>&1 | tee -a "$LOG_FILE"
}

# Polling loop
start_polling() {
    log "Starting polling loop (interval: ${POLL_INTERVAL}s)..."

    while true; do
        sleep "$POLL_INTERVAL"
        log "Polling for changes..."
        run_reconcile || log "Reconciliation failed"
    done
}

# Main
main() {
    log "=== GitOps Runner starting ==="
    log "  Version: $(bosun --version 2>/dev/null || echo 'unknown')"
    log "  Repo: ${REPO_URL:-not set}"
    log "  Branch: ${REPO_BRANCH:-main}"
    log "  Poll interval: ${POLL_INTERVAL:-3600}s"
    log "  Target: ${DEPLOY_TARGET:-unraid}"
    log "  Dry run: ${DRY_RUN:-false}"

    validate_env
    setup_ssh

    # Run doctor to verify environment
    log "Running environment checks..."
    bosun doctor 2>&1 | tee -a "$LOG_FILE" || log "Some checks failed (continuing anyway)"

    # Initial reconciliation
    log "Running initial reconciliation..."
    run_reconcile || log "Initial reconciliation failed"

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
