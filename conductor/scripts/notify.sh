#!/bin/bash
# Discord notification helpers

notify_discord() {
    local color="$1"
    local title="$2"
    local message="$3"

    if [[ -z "${DISCORD_WEBHOOK_URL:-}" ]]; then
        return 0
    fi

    local payload=$(jq -n \
        --arg title "$title" \
        --arg desc "$message" \
        --argjson color "$color" \
        --arg ts "$(date -Iseconds)" \
        '{
            embeds: [{
                title: $title,
                description: $desc,
                color: $color,
                footer: { text: "GitOps Runner" },
                timestamp: $ts
            }]
        }')

    curl -s -H "Content-Type: application/json" \
        -d "$payload" \
        "$DISCORD_WEBHOOK_URL" >/dev/null 2>&1 || true
}

notify_success() {
    local message="${1:-Deployment successful}"
    notify_discord 3066993 "Deployment Success" "$message"
}

notify_error() {
    local message="${1:-Deployment failed}"
    notify_discord 15158332 "Deployment Failed" "$message"
}

notify_info() {
    local message="${1:-Info}"
    notify_discord 3447003 "GitOps Info" "$message"
}
