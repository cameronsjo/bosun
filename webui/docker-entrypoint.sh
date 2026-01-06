#!/bin/sh
set -e

# Default API URL
BOSUN_API_URL="${BOSUN_API_URL:-http://bosun:9090}"
BOSUN_BEARER_TOKEN="${BOSUN_BEARER_TOKEN:-}"

# Substitute environment variables in nginx config
envsubst '${BOSUN_API_URL}' < /etc/nginx/conf.d/default.conf > /tmp/default.conf
mv /tmp/default.conf /etc/nginx/conf.d/default.conf

# Generate runtime config.js with bearer token
cat > /usr/share/nginx/html/config.js << EOF
window.BOSUN_CONFIG = {
  bearerToken: "${BOSUN_BEARER_TOKEN}"
};
EOF

# Execute the main command
exec "$@"
