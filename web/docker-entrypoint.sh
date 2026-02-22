#!/bin/sh
set -e

# Substitute API_BACKEND_URL in nginx config
# Default: http://localhost:9080 (sidecar pattern, same Pod)
API_BACKEND_URL="${API_BACKEND_URL:-http://localhost:9080}"

# Perform in-place substitution using a temp file and shell redirection.
# We avoid `sed -i` (needs dir write) and `cp` (fails on overlayfs).
sed "s|API_BACKEND_URL|${API_BACKEND_URL}|g" /etc/nginx/nginx.conf > /tmp/nginx.conf.tmp
cat /tmp/nginx.conf.tmp > /etc/nginx/nginx.conf
rm -f /tmp/nginx.conf.tmp

exec nginx -g 'daemon off;'
