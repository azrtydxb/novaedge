#!/bin/sh
set -e

# Substitute API_BACKEND_URL in nginx config
# Default: http://localhost:9080 (sidecar pattern, same Pod)
API_BACKEND_URL="${API_BACKEND_URL:-http://localhost:9080}"

sed -i "s|API_BACKEND_URL|${API_BACKEND_URL}|g" /etc/nginx/nginx.conf

exec nginx -g 'daemon off;'
