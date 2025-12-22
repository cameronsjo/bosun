#!/bin/bash
# Reconcile script - pulls latest changes and deploys
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="/app/repo"
STAGING_DIR="/app/staging"
BACKUP_DIR="/app/backups"
LOCK_FILE="/tmp/reconcile.lock"
LOG_FILE="/app/logs/reconcile.log"

# Source notify helper
source "$SCRIPT_DIR/notify.sh"

log() {
    local msg="[$(date -Iseconds)] $1"
    echo "$msg" | tee -a "$LOG_FILE"
}

error() {
    log "ERROR: $1"
    notify_error "$1"
    exit 1
}

# Acquire lock to prevent concurrent runs
acquire_lock() {
    exec 200>"$LOCK_FILE"
    if ! flock -n 200; then
        log "Another reconciliation is in progress, skipping"
        exit 0
    fi
}

# Clone or update repo
sync_repo() {
    log "Syncing repository..."

    if [[ ! -d "$REPO_DIR/.git" ]]; then
        log "  Cloning $REPO_URL (branch: $REPO_BRANCH)"
        git clone --branch "$REPO_BRANCH" --single-branch --depth 1 "$REPO_URL" "$REPO_DIR"
    else
        cd "$REPO_DIR"
        local before=$(git rev-parse HEAD)
        git fetch origin "$REPO_BRANCH" --depth 1
        git reset --hard "origin/$REPO_BRANCH"
        local after=$(git rev-parse HEAD)

        if [[ "$before" == "$after" ]]; then
            log "  No changes detected"
            return 1
        fi

        log "  Updated: $before -> $after"
        log "  $(git log --oneline -1)"
    fi
    return 0
}

# Decrypt SOPS secrets
decrypt_secrets() {
    log "Decrypting secrets..."

    local secrets_dir="$REPO_DIR/infrastructure/secrets"

    if [[ ! -f "$secrets_dir/unraid.yaml.sops" ]]; then
        error "Secrets file not found: $secrets_dir/unraid.yaml.sops"
    fi

    # Decrypt and merge secrets
    local unraid_secrets shared_secrets
    unraid_secrets=$(sops --input-type yaml --output-type json -d "$secrets_dir/unraid.yaml.sops") || error "Failed to decrypt unraid.yaml.sops"
    shared_secrets=$(sops --input-type yaml --output-type json -d "$secrets_dir/shared.yaml.sops") || error "Failed to decrypt shared.yaml.sops"

    export SOPS_SECRETS=$(echo "$unraid_secrets" "$shared_secrets" | jq -s '.[0] * .[1]')
    log "  Secrets decrypted successfully"
}

# Render templates
render_templates() {
    log "Rendering templates..."

    rm -rf "$STAGING_DIR"
    mkdir -p "$STAGING_DIR"

    local infra_dir="$REPO_DIR/infrastructure"

    # Copy non-template files
    rsync -a --exclude '*.tmpl' "$infra_dir/unraid/" "$STAGING_DIR/unraid/" 2>/dev/null || true

    # Render .tmpl files
    find "$infra_dir" -name "*.tmpl" -type f | while read -r tmpl; do
        local rel_path="${tmpl#$infra_dir/}"
        local output_path="$STAGING_DIR/${rel_path%.tmpl}"

        mkdir -p "$(dirname "$output_path")"
        log "  Rendering: ${rel_path%.tmpl}"
        chezmoi execute-template < "$tmpl" > "$output_path"
    done
}

# Create backup before deploy
create_backup() {
    log "Creating backup..."

    local backup_name="backup-$(date +%Y%m%d-%H%M%S)"
    local backup_path="$BACKUP_DIR/$backup_name"
    mkdir -p "$backup_path"

    # Backup current configs (local or remote)
    if [[ -d "/mnt/appdata" ]]; then
        tar -czf "$backup_path/configs.tar.gz" \
            /mnt/appdata/traefik \
            /mnt/appdata/authelia/configuration.yml \
            /mnt/appdata/agentgateway/config.yaml \
            /mnt/appdata/gatus/config.yaml 2>/dev/null || log "  Warning: Backup partially failed"
    else
        local unraid_ip=$(echo "$SOPS_SECRETS" | jq -r '.network.unraid_ip')
        ssh "root@$unraid_ip" "tar -czf - /mnt/user/appdata/traefik /mnt/user/appdata/authelia/configuration.yml /mnt/user/appdata/agentgateway/config.yaml /mnt/user/appdata/gatus/config.yaml 2>/dev/null" > "$backup_path/configs.tar.gz" || log "  Warning: Backup partially failed"
    fi

    # Keep only last 5 backups
    ls -1dt "$BACKUP_DIR"/backup-* 2>/dev/null | tail -n +6 | xargs -r rm -rf

    log "  Backup saved: $backup_name"
}

# Deploy to target (local mode uses mounted paths, remote uses SSH)
deploy() {
    log "Deploying to $DEPLOY_TARGET..."

    # Check if running in local mode (appdata mounted)
    if [[ -d "/mnt/appdata" ]]; then
        deploy_local
    else
        deploy_remote
    fi

    log "Deployment complete!"
}

# Deploy locally via mounted paths
deploy_local() {
    log "  Using local deployment mode"

    local rsync_opts="-av --delete"
    if [[ "${DRY_RUN:-false}" == "true" ]]; then
        rsync_opts="$rsync_opts --dry-run"
        log "  DRY RUN MODE - no changes will be made"
    fi

    log "  Syncing Traefik configs..."
    rsync $rsync_opts "$STAGING_DIR/unraid/appdata/traefik/" "/mnt/appdata/traefik/"

    log "  Syncing agentgateway config..."
    rsync $rsync_opts "$STAGING_DIR/unraid/appdata/agentgateway/config.yaml" "/mnt/appdata/agentgateway/config.yaml"

    log "  Syncing authelia config..."
    rsync $rsync_opts "$STAGING_DIR/unraid/appdata/authelia/configuration.yml" "/mnt/appdata/authelia/configuration.yml"

    log "  Syncing gatus config..."
    rsync $rsync_opts "$STAGING_DIR/unraid/appdata/gatus/config.yaml" "/mnt/appdata/gatus/config.yaml"

    log "  Syncing tailscale-gateway config..."
    mkdir -p "/mnt/appdata/tailscale-gateway" 2>/dev/null || true
    rsync $rsync_opts "$STAGING_DIR/unraid/appdata/tailscale-gateway/serve.json" "/mnt/appdata/tailscale-gateway/serve.json"

    log "  Syncing compose files..."
    mkdir -p "/mnt/appdata/compose" 2>/dev/null || true
    rsync $rsync_opts "$STAGING_DIR/unraid/compose/" "/mnt/appdata/compose/"

    # Compose Manager sync requires root access to /boot - skip in container
    # Core configs are synced to /mnt/appdata/compose/ which is the source of truth
    log "  Note: Compose Manager sync skipped (requires host access)"

    if [[ "${DRY_RUN:-false}" != "true" ]]; then
        log "  Reloading services..."
        docker compose -f "/mnt/appdata/compose/core.yml" up -d --remove-orphans 2>/dev/null || log "  Warning: Could not recreate core stack"
        docker kill --signal=SIGHUP agentgateway 2>/dev/null || log "  Warning: Could not reload agentgateway"
    fi
}

# Deploy remotely via SSH
deploy_remote() {
    log "  Using remote deployment mode (SSH)"

    local unraid_ip=$(echo "$SOPS_SECRETS" | jq -r '.network.unraid_ip')
    local unraid_host="root@$unraid_ip"

    local rsync_opts="-avz --delete"
    if [[ "${DRY_RUN:-false}" == "true" ]]; then
        rsync_opts="$rsync_opts --dry-run"
        log "  DRY RUN MODE - no changes will be made"
    fi

    # Sync configs
    log "  Syncing Traefik configs..."
    rsync $rsync_opts "$STAGING_DIR/unraid/appdata/traefik/" "$unraid_host:/mnt/user/appdata/traefik/"

    log "  Syncing agentgateway config..."
    rsync $rsync_opts "$STAGING_DIR/unraid/appdata/agentgateway/config.yaml" "$unraid_host:/mnt/user/appdata/agentgateway/config.yaml"

    log "  Syncing authelia config..."
    rsync $rsync_opts "$STAGING_DIR/unraid/appdata/authelia/configuration.yml" "$unraid_host:/mnt/user/appdata/authelia/configuration.yml"

    log "  Syncing gatus config..."
    rsync $rsync_opts "$STAGING_DIR/unraid/appdata/gatus/config.yaml" "$unraid_host:/mnt/user/appdata/gatus/config.yaml"

    log "  Syncing tailscale-gateway config..."
    ssh "$unraid_host" "mkdir -p /mnt/user/appdata/tailscale-gateway" 2>/dev/null || true
    rsync $rsync_opts "$STAGING_DIR/unraid/appdata/tailscale-gateway/serve.json" "$unraid_host:/mnt/user/appdata/tailscale-gateway/serve.json"

    log "  Syncing compose files..."
    ssh "$unraid_host" "mkdir -p /mnt/user/appdata/compose" 2>/dev/null || true
    rsync $rsync_opts "$STAGING_DIR/unraid/compose/" "$unraid_host:/mnt/user/appdata/compose/"

    # Sync to Compose Manager
    log "  Syncing core compose to Compose Manager..."
    ssh "$unraid_host" "mkdir -p /boot/config/plugins/compose.manager/projects/core" 2>/dev/null || true
    rsync $rsync_opts "$STAGING_DIR/unraid/compose/core.yml" "$unraid_host:/boot/config/plugins/compose.manager/projects/core/docker-compose.yml"

    if [[ "${DRY_RUN:-false}" != "true" ]]; then
        log "  Reloading services..."
        ssh "$unraid_host" "cd /boot/config/plugins/compose.manager/projects/core && docker compose up -d --remove-orphans" || log "  Warning: Could not recreate core stack"
        ssh "$unraid_host" "docker kill --signal=SIGHUP agentgateway 2>/dev/null" || log "  Warning: Could not reload agentgateway"
    fi
}

# Main
main() {
    acquire_lock

    log "=== Starting reconciliation ==="

    local start_time=$(date +%s)
    local changes_detected=false

    if sync_repo; then
        changes_detected=true
    fi

    # Always run if forced or changes detected
    if [[ "${FORCE:-false}" == "true" ]] || [[ "$changes_detected" == "true" ]]; then
        decrypt_secrets
        render_templates

        if [[ "${DRY_RUN:-false}" != "true" ]]; then
            create_backup
        fi

        deploy

        local end_time=$(date +%s)
        local duration=$((end_time - start_time))

        notify_success "Deployment completed in ${duration}s"
        log "=== Reconciliation completed in ${duration}s ==="
    else
        log "=== No changes, skipping deployment ==="
    fi
}

main "$@"
