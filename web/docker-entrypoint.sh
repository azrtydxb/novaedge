#!/bin/sh
set -e

# Substitute API_BACKEND_URL in nginx config
# Default: http://localhost:9080 (sidecar pattern, same Pod)
API_BACKEND_URL="${API_BACKEND_URL:-http://localhost:9080}"

# sed -i creates a temp file in the same directory which requires dir write access;
# use redirect to /tmp instead, then copy back over the owned file
sed "s|API_BACKEND_URL|${API_BACKEND_URL}|g" /etc/nginx/nginx.conf > /tmp/nginx.conf.tmp
cp /tmp/nginx.conf.tmp /etc/nginx/nginx.conf
rm -f /tmp/nginx.conf.tmp

exec nginx -g 'daemon off;'
