#!/usr/bin/env bash
set -euo pipefail

# Teardown script for NovaEdge functional tests

echo "=== NovaEdge Functional Test Teardown ==="

echo "Deleting policies..."
kubectl delete proxypolicy -n default --all 2>/dev/null || true

echo "Deleting route..."
kubectl delete proxyroute api-route -n default 2>/dev/null || true

echo "Deleting backends..."
kubectl delete proxybackend api-backend api-backend-v2 -n default 2>/dev/null || true

echo "Deleting gateway..."
kubectl delete proxygateway test-gateway -n default 2>/dev/null || true

echo "Deleting TLS secret..."
kubectl delete secret test-tls-cert -n default 2>/dev/null || true

echo ""
echo "=== Teardown Complete ==="
