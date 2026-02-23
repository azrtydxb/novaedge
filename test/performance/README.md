# NovaEdge Performance Testing

End-to-end performance test suite for NovaEdge, using [fortio](https://github.com/fortio/fortio) for HTTP load testing and [iperf3](https://iperf.fr/) for L4 TCP throughput measurement.

## Prerequisites

- A running Kubernetes cluster with NovaEdge deployed (e.g., k3s with Helm)
- `kubectl` configured to access the cluster
- `jq` for result parsing (optional but recommended)
- `envsubst` (part of `gettext`, usually pre-installed)

## Quick Start

```bash
# Run all performance tests
make perf-test

# Run HTTP throughput tests only
make perf-test-http

# Run TCP throughput tests only
make perf-test-tcp

# Run with pprof CPU/memory profiling
make perf-profile
```

## Manual Usage

```bash
# Run all scenarios
./test/performance/run-perf.sh

# Run specific scenario
./test/performance/run-perf.sh --scenario http
./test/performance/run-perf.sh --scenario latency
./test/performance/run-perf.sh --scenario tcp
./test/performance/run-perf.sh --scenario ramp

# Override VIP address
./test/performance/run-perf.sh --vip 192.168.1.100

# Keep test resources after completion (for debugging)
./test/performance/run-perf.sh --no-cleanup

# Collect pprof CPU/heap profiles during tests
./test/performance/run-perf.sh --collect-pprof

# Custom test duration per iteration
./test/performance/run-perf.sh --duration 60
```

## Test Scenarios

### HTTP Throughput
Measures maximum requests per second at varying concurrency levels (1, 4, 16, 64, 128, 256, 512 connections). Uses `fortio load -qps=0` (unlimited rate) to find the throughput ceiling.

### HTTP Latency
Measures latency percentiles (p50, p95, p99) at fixed request rates (100, 500, 1k, 2k, 5k, 10k QPS). Uses fortio's constant-rate mode to avoid coordinated omission.

### Connection Ramp
Tests how NovaEdge handles increasing connection counts (64 to 4096) at a fixed 100 QPS. Reveals connection handling overhead and memory scaling.

### L4 TCP Throughput
Runs iperf3 through the L4 TCP proxy with 4 parallel streams for 30 seconds. Measures raw TCP throughput in Gbps.

## Results

Results are saved to `test/performance/results/<timestamp>/` with:
- Fortio JSON output per test iteration
- Cluster state snapshots (nodes, pods)
- Optional pprof profiles (CPU, heap before/after)

A summary table is printed at the end of each run.

## Go Micro-Benchmarks

Run the existing Go micro-benchmarks locally:

```bash
make benchmark
```

This runs `go test -bench` across the LB, router, upstream, and policy packages with 3 iterations and memory allocation stats.

## Architecture

```
test/performance/
├── run-perf.sh              # Orchestration script
├── k8s/
│   ├── namespace.yaml       # novaedge-perf namespace
│   ├── test-backend.yaml    # fortio server (6 replicas)
│   ├── novaedge-resources.yaml  # ProxyGateway + Route + Backend
│   ├── l4-resources.yaml    # L4 ProxyGateway + Backend
│   ├── iperf3-server.yaml   # iperf3 server deployment
│   └── fortio-job.yaml      # Template Job (parameterized by run-perf.sh)
└── results/                 # Test results (gitignored)
```
