#!/bin/sh
set -e

# Default API URL (exported so envsubst sees it)
export BOSUN_API_URL="${BOSUN_API_URL:-http://bosun:9090}"

# Render nginx config from immutable template so restarts remain idempotent.
# shellcheck disable=SC2016
envsubst '${BOSUN_API_URL}' \
    < /etc/nginx/templates/default.conf.template \
    > /etc/nginx/conf.d/default.conf

# Inject BOSUN_BEARER_TOKEN server-side via an included nginx snippet rather
# than exposing the privileged control token over unauthenticated GET /config.js.
auth_inc="/etc/nginx/conf.d/auth-header.inc"
if [ -n "${BOSUN_BEARER_TOKEN:-}" ]; then
    case "${BOSUN_BEARER_TOKEN}" in
        *"$(printf '\n')"*|*"$(printf '\r')"*)
            echo "error: BOSUN_BEARER_TOKEN must not contain newline characters" >&2
            exit 1
            ;;
    esac
    escaped_token=$(printf '%s' "${BOSUN_BEARER_TOKEN}" | sed 's/\\/\\\\/g; s/"/\\"/g')
    printf 'proxy_set_header Authorization "Bearer %s";\n' "${escaped_token}" > "${auth_inc}"
else
    printf '# BOSUN_BEARER_TOKEN not configured\n' > "${auth_inc}"
fi

# Keep runtime config.js present for frontend boot without leaking bearer tokens.
cat > /usr/share/nginx/html/config.js << 'EOF'
window.BOSUN_CONFIG = {};
EOF

# Execute the main command
exec "$@"
