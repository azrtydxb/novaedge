#!/usr/bin/env bash
#
# NovaEdge Performance Test Suite
#
# Orchestrates end-to-end load tests against a live NovaEdge cluster using
# fortio (HTTP) and iperf3 (L4 TCP). Deploys test backends, NovaEdge CRDs,
# runs scenarios as Kubernetes Jobs, and collects results.
#
# Usage:
#   ./test/performance/run-perf.sh [OPTIONS]
#
# Options:
#   --scenario http|tcp|latency|ramp   Run specific scenario only
#   --collect-pprof                    Capture CPU/heap profiles during tests
#   --vip ADDRESS                      Override VIP address (default: $VIP_ADDRESS)
#   --no-cleanup                       Keep test resources after completion
#   --duration SECONDS                 Duration per test iteration (default: 30)
#   -h, --help                         Show this help message

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
K8S_DIR="${SCRIPT_DIR}/k8s"
RESULTS_DIR="${SCRIPT_DIR}/results"
NAMESPACE="novaedge-perf"
TIMESTAMP="$(date +%Y%m%d-%H%M%S)"
RUN_DIR="${RESULTS_DIR}/${TIMESTAMP}"

# Defaults
SCENARIO="all"
COLLECT_PPROF=false
VIP_ADDRESS="${VIP_ADDRESS:-}"
CLEANUP=true
DURATION="30s"
AGENT_ADMIN_PORT=9901
CONTROLLER_DEBUG_PORT=6060

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info()  { echo -e "${BLUE}[INFO]${NC}  $*"; }
log_ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }

usage() {
    sed -n '/^# Usage:/,/^$/p' "$0" | sed 's/^# //' | sed 's/^#//'
    exit 0
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --scenario)   SCENARIO="$2"; shift 2 ;;
        --collect-pprof) COLLECT_PPROF=true; shift ;;
        --vip)        VIP_ADDRESS="$2"; shift 2 ;;
        --no-cleanup) CLEANUP=false; shift ;;
        --duration)   DURATION="${2}s"; shift 2 ;;
        -h|--help)    usage ;;
        *)            log_error "Unknown option: $1"; usage ;;
    esac
done

# --------------------------------------------------------------------------
# Phase 1: Setup
# --------------------------------------------------------------------------

setup() {
    log_info "=== Phase 1: Setup ==="
    mkdir -p "${RUN_DIR}"

    log_info "Creating namespace ${NAMESPACE}..."
    kubectl apply -f "${K8S_DIR}/namespace.yaml"

    log_info "Deploying fortio backend (6 replicas)..."
    kubectl apply -f "${K8S_DIR}/test-backend.yaml"

    if [[ "${SCENARIO}" == "all" || "${SCENARIO}" == "tcp" ]]; then
        log_info "Deploying iperf3 server..."
        kubectl apply -f "${K8S_DIR}/iperf3-server.yaml"
    fi

    log_info "Applying NovaEdge CRDs..."
    kubectl apply -f "${K8S_DIR}/novaedge-resources.yaml"

    if [[ "${SCENARIO}" == "all" || "${SCENARIO}" == "tcp" ]]; then
        kubectl apply -f "${K8S_DIR}/l4-resources.yaml"
    fi

    log_info "Waiting for fortio backends to be ready..."
    kubectl -n "${NAMESPACE}" rollout status deployment/fortio-server --timeout=120s

    if [[ "${SCENARIO}" == "all" || "${SCENARIO}" == "tcp" ]]; then
        log_info "Waiting for iperf3 server to be ready..."
        kubectl -n "${NAMESPACE}" rollout status deployment/iperf3-server --timeout=120s
    fi

    log_ok "Setup complete"
}

# --------------------------------------------------------------------------
# Phase 2: Pre-flight checks
# --------------------------------------------------------------------------

preflight() {
    log_info "=== Phase 2: Pre-flight checks ==="

    # Check VIP
    if [[ -z "${VIP_ADDRESS}" ]]; then
        log_warn "No VIP_ADDRESS set. Attempting to discover from NovaEdge..."
        VIP_ADDRESS=$(kubectl -n "${NAMESPACE}" get proxygateways perf-gateway -o jsonpath='{.status.addresses[0].value}' 2>/dev/null || true)
        if [[ -z "${VIP_ADDRESS}" ]]; then
            log_error "Could not determine VIP address. Set VIP_ADDRESS env var or use --vip"
            exit 1
        fi
    fi
    log_ok "VIP address: ${VIP_ADDRESS}"

    # Check NovaEdge agents are running
    local agent_count
    agent_count=$(kubectl -n novaedge-system get pods -l app.kubernetes.io/name=novaedge-agent --no-headers 2>/dev/null | grep -c Running || echo 0)
    if [[ "${agent_count}" -eq 0 ]]; then
        log_warn "No running NovaEdge agents found in novaedge-system namespace"
    else
        log_ok "NovaEdge agents running: ${agent_count}"
    fi

    # Check fortio backends
    local ready_backends
    ready_backends=$(kubectl -n "${NAMESPACE}" get endpoints fortio-server -o jsonpath='{range .subsets[*].addresses[*]}{.ip}{"\n"}{end}' 2>/dev/null | wc -l | tr -d ' ')
    log_ok "Fortio backend endpoints ready: ${ready_backends}"

    # Record cluster state
    log_info "Recording cluster state..."
    kubectl get nodes -o wide > "${RUN_DIR}/nodes.txt" 2>&1 || true
    kubectl -n novaedge-system get pods -o wide > "${RUN_DIR}/novaedge-pods.txt" 2>&1 || true
    kubectl -n "${NAMESPACE}" get pods -o wide > "${RUN_DIR}/perf-pods.txt" 2>&1 || true

    log_ok "Pre-flight checks complete"
}

# --------------------------------------------------------------------------
# Helpers
# --------------------------------------------------------------------------

# Run a fortio Job and collect results
run_fortio_job() {
    local job_name="$1"
    local scenario="$2"
    local qps="$3"
    local concurrency="$4"
    local duration="$5"
    local target_url="http://${VIP_ADDRESS}/fortio/?size=1024"

    log_info "  Running: ${job_name} (qps=${qps}, c=${concurrency}, t=${duration})"

    # Generate Job manifest from template
    export JOB_NAME="${job_name}"
    export SCENARIO="${scenario}"
    export QPS="${qps}"
    export CONCURRENCY="${concurrency}"
    export DURATION="${duration}"
    export TARGET_URL="${target_url}"

    envsubst < "${K8S_DIR}/fortio-job.yaml" | kubectl apply -f -

    # Wait for Job to complete
    if ! kubectl -n "${NAMESPACE}" wait --for=condition=complete "job/${job_name}" --timeout=300s 2>/dev/null; then
        log_warn "  Job ${job_name} did not complete within timeout"
        kubectl -n "${NAMESPACE}" logs "job/${job_name}" > "${RUN_DIR}/${job_name}.json" 2>/dev/null || true
        return 1
    fi

    # Collect results
    kubectl -n "${NAMESPACE}" logs "job/${job_name}" > "${RUN_DIR}/${job_name}.json" 2>/dev/null || true
    log_ok "  Completed: ${job_name}"
}

# Start pprof collection in background
start_pprof_collection() {
    if [[ "${COLLECT_PPROF}" != "true" ]]; then
        return
    fi

    log_info "Starting pprof collection..."

    # Find first agent pod for port-forwarding
    local agent_pod
    agent_pod=$(kubectl -n novaedge-system get pods -l app.kubernetes.io/name=novaedge-agent -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
    if [[ -z "${agent_pod}" ]]; then
        log_warn "No agent pod found for pprof collection"
        return
    fi

    # Port-forward in background
    kubectl -n novaedge-system port-forward "pod/${agent_pod}" "${AGENT_ADMIN_PORT}:${AGENT_ADMIN_PORT}" &
    PPROF_PF_PID=$!
    sleep 2

    # Capture heap profile before test
    curl -s "http://127.0.0.1:${AGENT_ADMIN_PORT}/debug/pprof/heap" > "${RUN_DIR}/heap-before.pprof" 2>/dev/null || true
    log_ok "Heap profile (before) saved"
}

# Stop pprof collection
stop_pprof_collection() {
    if [[ "${COLLECT_PPROF}" != "true" ]]; then
        return
    fi

    log_info "Collecting final pprof profiles..."

    # Capture CPU profile (30s sample)
    curl -s "http://127.0.0.1:${AGENT_ADMIN_PORT}/debug/pprof/profile?seconds=30" > "${RUN_DIR}/cpu.pprof" 2>/dev/null || true

    # Capture heap profile after test
    curl -s "http://127.0.0.1:${AGENT_ADMIN_PORT}/debug/pprof/heap" > "${RUN_DIR}/heap-after.pprof" 2>/dev/null || true

    # Kill port-forward
    if [[ -n "${PPROF_PF_PID:-}" ]]; then
        kill "${PPROF_PF_PID}" 2>/dev/null || true
    fi
    log_ok "pprof profiles saved to ${RUN_DIR}/"
}

# --------------------------------------------------------------------------
# Phase 3: Test scenarios
# --------------------------------------------------------------------------

run_http_throughput() {
    log_info "=== Scenario: HTTP Throughput (max QPS at varying concurrency) ==="
    local concurrencies=(1 4 16 64 128 256 512)

    for c in "${concurrencies[@]}"; do
        run_fortio_job "http-throughput-c${c}" "http-throughput" "0" "${c}" "${DURATION}" || true
    done
}

run_http_latency() {
    log_info "=== Scenario: HTTP Latency (fixed QPS, latency percentiles) ==="
    local rates=(100 500 1000 2000 5000 10000)

    for qps in "${rates[@]}"; do
        local c=$((qps / 10))
        [[ $c -lt 4 ]] && c=4
        run_fortio_job "http-latency-qps${qps}" "http-latency" "${qps}" "${c}" "${DURATION}" || true
    done
}

run_connection_ramp() {
    log_info "=== Scenario: Connection Ramp (increasing connections, fixed QPS) ==="
    local connections=(64 128 256 512 1024 2048 4096)

    for c in "${connections[@]}"; do
        run_fortio_job "conn-ramp-c${c}" "connection-ramp" "100" "${c}" "${DURATION}" || true
    done
}

run_tcp_test() {
    log_info "=== Scenario: L4 TCP Throughput (iperf3 through proxy) ==="

    # Find an iperf3 client pod or run from a Job
    local job_name="tcp-iperf3-throughput"

    cat <<EOF | kubectl apply -f -
apiVersion: batch/v1
kind: Job
metadata:
  name: ${job_name}
  namespace: ${NAMESPACE}
  labels:
    app: iperf3-client
    scenario: tcp-throughput
spec:
  backoffLimit: 0
  ttlSecondsAfterFinished: 600
  template:
    metadata:
      labels:
        app: iperf3-client
    spec:
      restartPolicy: Never
      containers:
        - name: iperf3
          image: networkstatic/iperf3:latest
          args:
            - -c
            - "${VIP_ADDRESS}"
            - -p
            - "5201"
            - -t
            - "30"
            - -P
            - "4"
            - -J
          resources:
            requests:
              cpu: "1"
              memory: 128Mi
            limits:
              cpu: "2"
              memory: 256Mi
EOF

    if kubectl -n "${NAMESPACE}" wait --for=condition=complete "job/${job_name}" --timeout=120s 2>/dev/null; then
        kubectl -n "${NAMESPACE}" logs "job/${job_name}" > "${RUN_DIR}/${job_name}.json" 2>/dev/null || true
        log_ok "TCP throughput test complete"
    else
        log_warn "TCP throughput test did not complete within timeout"
        kubectl -n "${NAMESPACE}" logs "job/${job_name}" > "${RUN_DIR}/${job_name}.json" 2>/dev/null || true
    fi
}

# --------------------------------------------------------------------------
# Phase 4: Collect and summarize results
# --------------------------------------------------------------------------

summarize() {
    log_info "=== Phase 4: Results Summary ==="
    echo ""

    # Check for jq
    if ! command -v jq &>/dev/null; then
        log_warn "jq not found. Skipping JSON result parsing."
        log_info "Raw results saved to: ${RUN_DIR}/"
        return
    fi

    # HTTP Throughput table
    if ls "${RUN_DIR}"/http-throughput-*.json &>/dev/null; then
        echo "HTTP Throughput (max QPS)"
        echo "============================================================"
        printf "%-12s %10s %10s %10s %10s %8s\n" "Concurrency" "QPS" "p50(ms)" "p95(ms)" "p99(ms)" "Errors"
        echo "------------------------------------------------------------"
        for f in "${RUN_DIR}"/http-throughput-*.json; do
            local c qps p50 p95 p99 errs
            c=$(jq -r '.RunID // empty' "$f" 2>/dev/null | grep -oP 'c\K\d+' || basename "$f" .json | grep -oP 'c\K\d+' || echo "?")
            qps=$(jq -r '.ActualQPS // 0' "$f" 2>/dev/null || echo "0")
            p50=$(jq -r '.DurationHistogram.Percentiles[] | select(.Percentile == 50) | .Value * 1000' "$f" 2>/dev/null || echo "0")
            p95=$(jq -r '.DurationHistogram.Percentiles[] | select(.Percentile == 95) | .Value * 1000' "$f" 2>/dev/null || echo "0")
            p99=$(jq -r '.DurationHistogram.Percentiles[] | select(.Percentile == 99) | .Value * 1000' "$f" 2>/dev/null || echo "0")
            errs=$(jq -r '(.RetCodes | to_entries | map(select(.key != "200")) | map(.value) | add) // 0' "$f" 2>/dev/null || echo "0")
            printf "%-12s %10.0f %10.2f %10.2f %10.2f %8s\n" "${c}" "${qps}" "${p50}" "${p95}" "${p99}" "${errs}"
        done
        echo ""
    fi

    # HTTP Latency table
    if ls "${RUN_DIR}"/http-latency-*.json &>/dev/null; then
        echo "HTTP Latency (fixed QPS)"
        echo "============================================================"
        printf "%-12s %10s %10s %10s %10s %8s\n" "Target QPS" "Actual" "p50(ms)" "p95(ms)" "p99(ms)" "Errors"
        echo "------------------------------------------------------------"
        for f in "${RUN_DIR}"/http-latency-*.json; do
            local tqps aqps p50 p95 p99 errs
            tqps=$(jq -r '.RequestedQPS // "0"' "$f" 2>/dev/null || echo "0")
            aqps=$(jq -r '.ActualQPS // 0' "$f" 2>/dev/null || echo "0")
            p50=$(jq -r '.DurationHistogram.Percentiles[] | select(.Percentile == 50) | .Value * 1000' "$f" 2>/dev/null || echo "0")
            p95=$(jq -r '.DurationHistogram.Percentiles[] | select(.Percentile == 95) | .Value * 1000' "$f" 2>/dev/null || echo "0")
            p99=$(jq -r '.DurationHistogram.Percentiles[] | select(.Percentile == 99) | .Value * 1000' "$f" 2>/dev/null || echo "0")
            errs=$(jq -r '(.RetCodes | to_entries | map(select(.key != "200")) | map(.value) | add) // 0' "$f" 2>/dev/null || echo "0")
            printf "%-12s %10.0f %10.2f %10.2f %10.2f %8s\n" "${tqps}" "${aqps}" "${p50}" "${p95}" "${p99}" "${errs}"
        done
        echo ""
    fi

    # Connection Ramp table
    if ls "${RUN_DIR}"/conn-ramp-*.json &>/dev/null; then
        echo "Connection Ramp (100 QPS, increasing connections)"
        echo "============================================================"
        printf "%-14s %10s %10s %10s %10s %8s\n" "Connections" "QPS" "p50(ms)" "p95(ms)" "p99(ms)" "Errors"
        echo "------------------------------------------------------------"
        for f in "${RUN_DIR}"/conn-ramp-*.json; do
            local c qps p50 p95 p99 errs
            c=$(basename "$f" .json | grep -oP 'c\K\d+' || echo "?")
            qps=$(jq -r '.ActualQPS // 0' "$f" 2>/dev/null || echo "0")
            p50=$(jq -r '.DurationHistogram.Percentiles[] | select(.Percentile == 50) | .Value * 1000' "$f" 2>/dev/null || echo "0")
            p95=$(jq -r '.DurationHistogram.Percentiles[] | select(.Percentile == 95) | .Value * 1000' "$f" 2>/dev/null || echo "0")
            p99=$(jq -r '.DurationHistogram.Percentiles[] | select(.Percentile == 99) | .Value * 1000' "$f" 2>/dev/null || echo "0")
            errs=$(jq -r '(.RetCodes | to_entries | map(select(.key != "200")) | map(.value) | add) // 0' "$f" 2>/dev/null || echo "0")
            printf "%-14s %10.0f %10.2f %10.2f %10.2f %8s\n" "${c}" "${qps}" "${p50}" "${p95}" "${p99}" "${errs}"
        done
        echo ""
    fi

    # TCP throughput
    if [[ -f "${RUN_DIR}/tcp-iperf3-throughput.json" ]]; then
        echo "L4 TCP Throughput (iperf3, 4 parallel streams, 30s)"
        echo "============================================================"
        local bps
        bps=$(jq -r '.end.sum_sent.bits_per_second // 0' "${RUN_DIR}/tcp-iperf3-throughput.json" 2>/dev/null || echo "0")
        local gbps
        gbps=$(echo "scale=2; ${bps} / 1000000000" | bc 2>/dev/null || echo "?")
        printf "  Throughput: %s Gbps\n" "${gbps}"
        echo ""
    fi

    log_ok "Results saved to: ${RUN_DIR}/"
}

# --------------------------------------------------------------------------
# Phase 5: Cleanup
# --------------------------------------------------------------------------

cleanup() {
    if [[ "${CLEANUP}" != "true" ]]; then
        log_info "Skipping cleanup (--no-cleanup specified)"
        log_info "To clean up manually: kubectl delete namespace ${NAMESPACE}"
        return
    fi

    log_info "=== Cleanup ==="
    kubectl delete namespace "${NAMESPACE}" --ignore-not-found --wait=false
    log_ok "Cleanup complete"
}

# --------------------------------------------------------------------------
# Main
# --------------------------------------------------------------------------

main() {
    echo ""
    echo "========================================"
    echo "  NovaEdge Performance Test Suite"
    echo "========================================"
    echo "  Timestamp: ${TIMESTAMP}"
    echo "  Scenario:  ${SCENARIO}"
    echo "  Duration:  ${DURATION}/iteration"
    echo "  Pprof:     ${COLLECT_PPROF}"
    echo "========================================"
    echo ""

    setup
    preflight
    start_pprof_collection

    case "${SCENARIO}" in
        all)
            run_http_throughput
            run_http_latency
            run_connection_ramp
            run_tcp_test
            ;;
        http)
            run_http_throughput
            ;;
        latency)
            run_http_latency
            ;;
        ramp)
            run_connection_ramp
            ;;
        tcp)
            run_tcp_test
            ;;
        *)
            log_error "Unknown scenario: ${SCENARIO}"
            log_info "Valid scenarios: all, http, latency, ramp, tcp"
            exit 1
            ;;
    esac

    stop_pprof_collection
    summarize

    # Trap ensures cleanup runs even on failure
    trap cleanup EXIT
}

# Run
main "$@"
