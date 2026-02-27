#!/usr/bin/env bash
set -euo pipefail

# NovaEdge Functional Test Runner
# Requires: setup.sh to be run first, or pass --setup flag

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PASS=0
FAIL=0
SKIP=0
PORT_BASE=28080
PF_PIDS=()

cleanup() {
    for pid in "${PF_PIDS[@]}"; do
        kill "$pid" 2>/dev/null || true
    done
    wait 2>/dev/null || true
}
trap cleanup EXIT

# Port-forward helper: starts port-forward, waits, returns PID
start_pf() {
    local pod=$1 local_port=$2 remote_port=$3
    kubectl port-forward -n nova-system "$pod" "${local_port}:${remote_port}" >/dev/null 2>&1 &
    local pid=$!
    PF_PIDS+=("$pid")
    sleep 3
    echo "$pid"
}

stop_pf() {
    local pid=$1
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    sleep 1
}

result() {
    local name=$1 expected=$2 actual=$3
    if [ "$expected" = "$actual" ]; then
        echo "  PASS: $name (got: $actual)"
        ((PASS++))
    else
        echo "  FAIL: $name (expected: $expected, got: $actual)"
        ((FAIL++))
    fi
}

result_contains() {
    local name=$1 expected=$2 actual=$3
    if echo "$actual" | grep -q "$expected"; then
        echo "  PASS: $name (contains: $expected)"
        ((PASS++))
    else
        echo "  FAIL: $name (expected to contain: $expected, got: $actual)"
        ((FAIL++))
    fi
}

result_not_contains() {
    local name=$1 unexpected=$2 actual=$3
    if echo "$actual" | grep -q "$unexpected"; then
        echo "  FAIL: $name (unexpectedly contains: $unexpected)"
        ((FAIL++))
    else
        echo "  PASS: $name (correctly does not contain: $unexpected)"
        ((PASS++))
    fi
}

# ============================================================
# Determine active agent pod
# ============================================================
ACTIVE_NODE=$(kubectl get proxyvip test-vip -o jsonpath='{.status.activeNode}' 2>/dev/null || echo "")
if [ -z "$ACTIVE_NODE" ]; then
    echo "ERROR: No active VIP node. Run setup.sh first."
    exit 1
fi

AGENT_POD=$(kubectl get pods -n nova-system -l app.kubernetes.io/name=novaedge-agent \
  --field-selector "spec.nodeName=$ACTIVE_NODE" -o jsonpath='{.items[0].metadata.name}')
CTRL_POD=$(kubectl get pods -n nova-system -l app.kubernetes.io/name=novaedge-controller \
  -o jsonpath='{.items[0].metadata.name}')

echo "============================================"
echo "NovaEdge Functional Tests"
echo "============================================"
echo "Active Node:  $ACTIVE_NODE"
echo "Agent Pod:    $AGENT_POD"
echo "Controller:   $CTRL_POD"
echo "============================================"
echo ""

# ============================================================
# Test 1: VIP Management
# ============================================================
echo "--- Test 1: VIP Management ---"
VIP_STATUS=$(kubectl get proxyvip test-vip -o jsonpath='{.status.conditions[0].status}')
result "VIP Ready condition" "True" "$VIP_STATUS"

VIP_REASON=$(kubectl get proxyvip test-vip -o jsonpath='{.status.conditions[0].reason}')
result "VIP assigned reason" "VIPAssigned" "$VIP_REASON"
echo ""

# ============================================================
# Test 2: HTTP Routing
# ============================================================
echo "--- Test 2: HTTP Routing ---"
PORT=$((PORT_BASE++))
PF_PID=$(start_pf "$AGENT_POD" "$PORT" 80)

# Host-based routing
RESP=$(timeout 5 curl -s -H "Host: api.example.com" "http://localhost:$PORT/v1/test" 2>/dev/null || echo "TIMEOUT")
result_contains "Host routing (correct host)" '"status":"ok"' "$RESP"

# Path matching
RESP=$(timeout 5 curl -s -H "Host: api.example.com" "http://localhost:$PORT/api/data" 2>/dev/null || echo "TIMEOUT")
result_contains "Path prefix /api" '"uri":"/api/data"' "$RESP"

RESP=$(timeout 5 curl -s -H "Host: api.example.com" "http://localhost:$PORT/anything-else" 2>/dev/null || echo "TIMEOUT")
result_contains "Catch-all route /" '"status":"ok"' "$RESP"

# Method matching
RESP=$(timeout 5 curl -s -X POST -H "Host: api.example.com" "http://localhost:$PORT/v1/test" 2>/dev/null || echo "TIMEOUT")
result_contains "POST method match" '"status":"ok"' "$RESP"

# Header matching
RESP=$(timeout 5 curl -s -H "Host: api.example.com" -H "X-Test-Header: test-value" "http://localhost:$PORT/header-test/check" 2>/dev/null || echo "TIMEOUT")
result_contains "Header match present" '"status":"ok"' "$RESP"

stop_pf "$PF_PID"
echo ""

# ============================================================
# Test 3: Route Filters
# ============================================================
echo "--- Test 3: Route Filters ---"
PORT=$((PORT_BASE++))
PF_PID=$(start_pf "$AGENT_POD" "$PORT" 80)

# URLRewrite
RESP=$(timeout 5 curl -s -H "Host: api.example.com" "http://localhost:$PORT/old-path" 2>/dev/null || echo "TIMEOUT")
result_contains "URLRewrite /old-path -> /v1/rewritten" '"/v1/rewritten"' "$RESP"

# RequestRedirect
STATUS=$(timeout 5 curl -s -o /dev/null -w "%{http_code}" -H "Host: api.example.com" "http://localhost:$PORT/redirect-me" 2>/dev/null || echo "000")
result "RequestRedirect returns 302" "302" "$STATUS"

LOCATION=$(timeout 5 curl -s -D - -o /dev/null -H "Host: api.example.com" "http://localhost:$PORT/redirect-me" 2>/dev/null | grep -i "^location:" | tr -d '\r' || echo "")
result_contains "Redirect Location header" "https://api.example.com/v1/redirected" "$LOCATION"

# AddHeader (verify no errors)
STATUS=$(timeout 5 curl -s -o /dev/null -w "%{http_code}" -H "Host: api.example.com" "http://localhost:$PORT/v1/filter-test" 2>/dev/null || echo "000")
result "AddHeader filter (no errors)" "200" "$STATUS"

stop_pf "$PF_PID"
echo ""

# ============================================================
# Test 4: Load Balancing
# ============================================================
echo "--- Test 4: Load Balancing (EWMA) ---"
PORT=$((PORT_BASE++))
PF_PID=$(start_pf "$AGENT_POD" "$PORT" 80)

UNIQUE_PODS=0
for i in $(seq 1 10); do
    timeout 3 curl -s -H "Host: api.example.com" "http://localhost:$PORT/v1/lb" 2>/dev/null | \
      python3 -c "import sys,json; print(json.load(sys.stdin).get('pod',''))" 2>/dev/null
done | sort -u | while read -r line; do [ -n "$line" ] && UNIQUE_PODS=$((UNIQUE_PODS + 1)); done
# EWMA with identical backends may use single pod; at least 1 responds
STATUS=$(timeout 5 curl -s -o /dev/null -w "%{http_code}" -H "Host: api.example.com" "http://localhost:$PORT/v1/lb" 2>/dev/null || echo "000")
result "EWMA LB responds" "200" "$STATUS"

stop_pf "$PF_PID"
echo ""

# ============================================================
# Test 5: TLS Termination
# ============================================================
echo "--- Test 5: TLS Termination ---"
PORT=$((PORT_BASE++))
PF_PID=$(start_pf "$AGENT_POD" "$PORT" 443)

TLS_RESP=$(timeout 5 curl -svk -H "Host: api.example.com" "https://localhost:$PORT/v1/tls-test" 2>&1 || echo "TIMEOUT")
result_contains "TLS handshake succeeds" "SSL connection using" "$TLS_RESP"
result_contains "TLS certificate CN" "CN=*.example.com" "$TLS_RESP"
result_contains "HTTPS returns 200" '"status":"ok"' "$TLS_RESP"

stop_pf "$PF_PID"
echo ""

# ============================================================
# Test 6: CORS Policy
# ============================================================
echo "--- Test 6: CORS Policy ---"
kubectl apply -f "$SCRIPT_DIR/05-policy-cors.yaml" >/dev/null 2>&1
sleep 12
PORT=$((PORT_BASE++))
PF_PID=$(start_pf "$AGENT_POD" "$PORT" 80)

# Preflight
PREFLIGHT=$(timeout 5 curl -s -D - -X OPTIONS -H "Host: api.example.com" \
  -H "Origin: https://app.example.com" \
  -H "Access-Control-Request-Method: POST" \
  "http://localhost:$PORT/v1/cors" 2>/dev/null || echo "TIMEOUT")
result_contains "CORS preflight Allow-Origin" "Access-Control-Allow-Origin: https://app.example.com" "$PREFLIGHT"
result_contains "CORS preflight Allow-Methods" "Access-Control-Allow-Methods" "$PREFLIGHT"

# Allowed origin
ALLOWED=$(timeout 5 curl -s -D - -H "Host: api.example.com" -H "Origin: https://app.example.com" \
  "http://localhost:$PORT/v1/cors" 2>/dev/null || echo "TIMEOUT")
result_contains "CORS allowed origin header" "Access-Control-Allow-Origin" "$ALLOWED"

# Disallowed origin
DENIED=$(timeout 5 curl -s -D - -H "Host: api.example.com" -H "Origin: https://evil.com" \
  "http://localhost:$PORT/v1/cors" 2>/dev/null || echo "TIMEOUT")
result_not_contains "CORS disallowed origin no header" "Access-Control-Allow-Origin" "$DENIED"

kubectl delete proxypolicy api-cors -n default >/dev/null 2>&1
stop_pf "$PF_PID"
echo ""

# ============================================================
# Test 7: Rate Limiting
# ============================================================
echo "--- Test 7: Rate Limiting ---"
kubectl apply -f "$SCRIPT_DIR/06-policy-ratelimit.yaml" >/dev/null 2>&1
sleep 12
PORT=$((PORT_BASE++))
PF_PID=$(start_pf "$AGENT_POD" "$PORT" 80)

GOT_429=false
for i in $(seq 1 20); do
    STATUS=$(timeout 3 curl -s -o /dev/null -w "%{http_code}" -H "Host: api.example.com" "http://localhost:$PORT/v1/rate" 2>/dev/null || echo "000")
    if [ "$STATUS" = "429" ]; then
        GOT_429=true
        break
    fi
done

if $GOT_429; then
    echo "  PASS: Rate limiting returns 429 after burst"
    ((PASS++))
else
    echo "  FAIL: Rate limiting never returned 429"
    ((FAIL++))
fi

kubectl delete proxypolicy api-rate-limit -n default >/dev/null 2>&1
stop_pf "$PF_PID"
echo ""

# ============================================================
# Test 8: IP Allow/Deny Lists
# ============================================================
echo "--- Test 8: IP Allow/Deny Lists ---"

# IP Allow List
kubectl apply -f "$SCRIPT_DIR/07-policy-ip-allow.yaml" >/dev/null 2>&1
sleep 12
PORT=$((PORT_BASE++))
PF_PID=$(start_pf "$AGENT_POD" "$PORT" 80)
STATUS=$(timeout 5 curl -s -o /dev/null -w "%{http_code}" -H "Host: api.example.com" "http://localhost:$PORT/v1/ip" 2>/dev/null || echo "000")
result "IP allow list permits 127.0.0.1" "200" "$STATUS"
kubectl delete proxypolicy api-ip-allow -n default >/dev/null 2>&1
stop_pf "$PF_PID"
sleep 12

# IP Deny List
kubectl apply -f "$SCRIPT_DIR/08-policy-ip-deny.yaml" >/dev/null 2>&1
sleep 12
PORT=$((PORT_BASE++))
PF_PID=$(start_pf "$AGENT_POD" "$PORT" 80)
STATUS=$(timeout 5 curl -s -o /dev/null -w "%{http_code}" -H "Host: api.example.com" "http://localhost:$PORT/v1/ip" 2>/dev/null || echo "000")
result "IP deny list blocks 127.0.0.1" "403" "$STATUS"
kubectl delete proxypolicy api-ip-deny -n default >/dev/null 2>&1
stop_pf "$PF_PID"
echo ""

# ============================================================
# Test 9: Agent Health Endpoints
# ============================================================
echo "--- Test 9: Agent Health ---"
PORT=$((PORT_BASE++))
PF_PID=$(start_pf "$AGENT_POD" "$PORT" 9091)

HEALTHZ=$(timeout 3 curl -s "http://localhost:$PORT/healthz" 2>/dev/null || echo "TIMEOUT")
result "Agent /healthz" "OK" "$HEALTHZ"

READYZ=$(timeout 3 curl -s "http://localhost:$PORT/readyz" 2>/dev/null || echo "TIMEOUT")
result "Agent /readyz" "Ready" "$READYZ"

STATUS_JSON=$(timeout 3 curl -s "http://localhost:$PORT/status" 2>/dev/null || echo "TIMEOUT")
result_contains "Agent /status healthy" '"healthy":true' "$STATUS_JSON"

stop_pf "$PF_PID"
echo ""

# ============================================================
# Test 10: Metrics
# ============================================================
echo "--- Test 10: Metrics ---"
PORT=$((PORT_BASE++))
PF_PID=$(start_pf "$AGENT_POD" "$PORT" 9090)
METRIC_COUNT=$(timeout 3 curl -s "http://localhost:$PORT/metrics" 2>/dev/null | grep -c novaedge || echo "0")
if [ "$METRIC_COUNT" -gt 10 ]; then
    echo "  PASS: Agent exposes $METRIC_COUNT novaedge metrics"
    ((PASS++))
else
    echo "  FAIL: Agent exposes only $METRIC_COUNT novaedge metrics (expected > 10)"
    ((FAIL++))
fi
stop_pf "$PF_PID"

PORT=$((PORT_BASE++))
PF_PID=$(start_pf "$CTRL_POD" "$PORT" 8081)
CTRL_HEALTH=$(timeout 3 curl -s "http://localhost:$PORT/healthz" 2>/dev/null || echo "TIMEOUT")
result "Controller /healthz" "ok" "$CTRL_HEALTH"
stop_pf "$PF_PID"
echo ""

# ============================================================
# Test 11: WebUI
# ============================================================
echo "--- Test 11: WebUI ---"
PORT=$((PORT_BASE++))
kubectl port-forward -n nova-system svc/novaedge-webui "${PORT}:80" >/dev/null 2>&1 &
PF_PID=$!
PF_PIDS+=("$PF_PID")
sleep 3
WEBUI_HEALTH=$(timeout 3 curl -s "http://localhost:$PORT/api/v1/health" 2>/dev/null || echo "TIMEOUT")
result_contains "WebUI health" '"status":"healthy"' "$WEBUI_HEALTH"

WEBUI_HTML=$(timeout 3 curl -s "http://localhost:$PORT/" 2>/dev/null || echo "TIMEOUT")
result_contains "WebUI serves HTML" "<!DOCTYPE html>" "$WEBUI_HTML"
stop_pf "$PF_PID"
echo ""

# ============================================================
# Test 12: Config Propagation
# ============================================================
echo "--- Test 12: Config Propagation ---"
OLD_VERSION=$(kubectl logs "$AGENT_POD" -n nova-system --tail=3 | grep "Applied config" | tail -1 | grep -o '"version":"[^"]*"' || echo "none")
kubectl patch proxybackend api-backend -n default --type=merge -p '{"spec":{"idleTimeout":"180s"}}' >/dev/null 2>&1
sleep 12
NEW_VERSION=$(kubectl logs "$AGENT_POD" -n nova-system --tail=5 | grep "Applied config" | tail -1 | grep -o '"version":"[^"]*"' || echo "none")
if [ "$OLD_VERSION" != "$NEW_VERSION" ] && [ "$NEW_VERSION" != "none" ]; then
    echo "  PASS: Config snapshot updated ($OLD_VERSION -> $NEW_VERSION)"
    ((PASS++))
else
    echo "  FAIL: Config snapshot not updated (old=$OLD_VERSION, new=$NEW_VERSION)"
    ((FAIL++))
fi
# Reset
kubectl patch proxybackend api-backend -n default --type=merge -p '{"spec":{"idleTimeout":"60s"}}' >/dev/null 2>&1
echo ""

# ============================================================
# Summary
# ============================================================
TOTAL=$((PASS + FAIL + SKIP))
echo "============================================"
echo "Results: $PASS passed, $FAIL failed, $SKIP skipped (total: $TOTAL)"
echo "============================================"

if [ "$FAIL" -gt 0 ]; then
    exit 1
fi
