#!/usr/bin/env bash
set -euo pipefail

# Setup script for NovaEdge functional tests
# Prerequisites: kubectl configured, NovaEdge controller + agents running

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "=== NovaEdge Functional Test Setup ==="

# Step 1: Clean up any existing test resources
echo "Cleaning up existing resources..."
kubectl delete proxyroute api-route -n default 2>/dev/null || true
kubectl delete proxypolicy -n default --all 2>/dev/null || true
kubectl delete proxygateway test-gateway -n default 2>/dev/null || true
kubectl delete proxybackend api-backend api-backend-v2 -n default 2>/dev/null || true
kubectl delete secret test-tls-cert -n default 2>/dev/null || true
sleep 5

# Step 2: Create TLS certificate
echo "Creating TLS certificate..."
openssl req -x509 -newkey rsa:2048 -keyout /tmp/tls.key -out /tmp/tls.crt \
  -days 365 -nodes -subj '/CN=*.example.com' \
  -addext "subjectAltName=DNS:*.example.com,DNS:api.example.com" 2>/dev/null
kubectl create secret tls test-tls-cert --cert=/tmp/tls.crt --key=/tmp/tls.key -n default
rm -f /tmp/tls.key /tmp/tls.crt

# Step 3: Apply CRDs in dependency order
echo "Applying gateway..."
kubectl apply -f "$SCRIPT_DIR/02-proxygateway.yaml"

echo "Applying backend..."
kubectl apply -f "$SCRIPT_DIR/03-proxybackend.yaml"

echo "Applying route..."
kubectl apply -f "$SCRIPT_DIR/04-proxyroute.yaml"

# Step 4: Wait for snapshot propagation
echo "Waiting for config snapshot propagation..."
sleep 15

# Step 5: Find an agent pod
AGENT_POD=$(kubectl get pods -n nova-system -l app.kubernetes.io/name=novaedge-agent \
  -o jsonpath='{.items[0].metadata.name}')
ACTIVE_NODE=$(kubectl get pod "$AGENT_POD" -n nova-system -o jsonpath='{.spec.nodeName}')
echo "Agent pod: $AGENT_POD (node: $ACTIVE_NODE)"

# Step 6: Verify agent has listeners
LISTENERS=$(kubectl logs "$AGENT_POD" -n nova-system --tail=5 | grep -o 'active_http_listeners":[0-9]*' | tail -1 | cut -d: -f2)
if [ "${LISTENERS:-0}" -lt 1 ]; then
    echo "WARNING: Agent has $LISTENERS HTTP listeners (expected >= 1)"
else
    echo "Agent has $LISTENERS HTTP listener(s)"
fi

echo ""
echo "=== Setup Complete ==="
echo "Active Node: $ACTIVE_NODE"
echo "Agent Pod:   $AGENT_POD"
echo ""
echo "Run tests with: ./run-tests.sh"
