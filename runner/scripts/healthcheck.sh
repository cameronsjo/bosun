#!/bin/bash
# Health check for GitOps runner

# Check if entrypoint is running
if ! pgrep -f "entrypoint.sh" > /dev/null; then
    echo "Entrypoint not running"
    exit 1
fi

# Check if we can access the repo directory
if [[ -d "/app/repo/.git" ]]; then
    # Verify git operations work
    cd /app/repo
    if ! git status > /dev/null 2>&1; then
        echo "Git operations failing"
        exit 1
    fi
fi

# Check webhook server if it should be running
if [[ -n "${WEBHOOK_SECRET:-}" ]]; then
    if ! pgrep -f "webhook" > /dev/null; then
        echo "Webhook server not running"
        exit 1
    fi
fi

echo "OK"
exit 0
