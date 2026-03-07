# Performance Tuning Guide

## Overview

NovaEdge includes several built-in performance optimizations that reduce latency, increase throughput, and lower CPU usage under high connection rates. Most features are enabled automatically with no configuration required. This guide documents each optimization, the recommended kernel parameters for production workloads, and how to apply them.

## SO_REUSEPORT (Multi-Core Accept Parallelism)

NovaEdge sets the `SO_REUSEPORT` socket option on all listener sockets. This allows the kernel to create multiple independent accept queues for the same address and port, distributing incoming connections across CPU cores without lock contention in the kernel accept path.

**Applies to:** All listener types -- HTTP, HTTPS, HTTP/3 (QUIC), TCP proxy, UDP proxy, and TLS passthrough.

**How it works:**

- Each listener socket is opened with `SO_REUSEPORT` via a custom `net.ListenConfig` control function.
- The kernel hashes incoming connections (by source IP and port) and assigns them to one of the listening sockets.
- This eliminates the single-socket bottleneck where all cores contend on a single accept queue.

**Performance impact:** 50-100% throughput improvement under high connection rates compared to a single accept queue.

**Requirements:** Linux 3.9 or later. On non-Linux platforms, the option is set on a best-effort basis; if the OS does not support it, the listener still functions normally with a single accept queue.

**Configuration:** None. This is always active.

## Zero-Copy Forwarding (splice)

For L4 TCP forwarding, NovaEdge uses the Linux `splice()` system call to move data directly between socket file descriptors through a kernel pipe, bypassing userspace entirely. Data never crosses the kernel-userspace boundary, eliminating memory copies and reducing CPU overhead.

**Applies to:** L4 TCP and TLS passthrough forwarding in the Rust dataplane (`dataplane/novaedge-dataplane/src/l4/`).

**How it works:**

1. NovaEdge creates a kernel pipe with a 1 MB buffer.
2. Data is spliced from the source socket into the pipe, then from the pipe into the destination socket.
3. Both directions of the bidirectional connection use splice independently.
4. If either connection is not a raw TCP socket (for example, a TLS-terminated connection that has been unwrapped in userspace), splice is not applicable and NovaEdge falls back to standard `io.Copy` with pooled buffers.

**Performance impact:** 20-30% throughput improvement and measurably lower CPU usage for L4 TCP workloads.

**Requirements:** Linux only. On non-Linux platforms or for non-TCP connections, the automatic fallback to userspace copy is transparent with no configuration change needed.

**Configuration:** None. Splice is attempted automatically for every L4 TCP connection. No flag or setting controls this behavior.

## eBPF/XDP Data Plane Acceleration

NovaEdge uses eBPF/XDP by default for data plane acceleration. All features are auto-detected at runtime and require no configuration beyond setting `--xdp-interface` for AF_XDP. If the kernel does not support a feature, the agent transparently falls back to the legacy path. Kubernetes Service L4 load balancing is handled by [NovaNet](https://github.com/azrtydxb/novanet).

| Feature | Program Type | Minimum Kernel | Fallback |
|---------|-------------|---------------|----------|
| **AF_XDP Zero-Copy** | XDP + `AF_XDP` socket | 5.10+ | Kernel network stack |
| **eBPF Mesh Redirect** | `BPF_PROG_TYPE_SK_LOOKUP` | 5.9+ | nftables/iptables TPROXY |
| **SOCKMAP Same-Node Bypass** | `BPF_PROG_TYPE_SOCK_OPS` | 5.4+ | Kernel network stack |
| **Conntrack** | `BPF_MAP_TYPE_LRU_HASH` | 5.4+ | Kernel conntrack |

**Performance impact:**

- **AF_XDP**: Zero-copy packet I/O via shared-memory ring buffers between NIC and userspace, removing all memory copies from the data path.
- **eBPF Mesh Redirect**: Socket lookup redirection via `bpf_sk_assign()` replaces the full nftables/iptables rule chain traversal.
- **SOCKMAP**: Bypasses the kernel network stack for same-node pod-to-pod traffic by short-circuiting socket pairs.
- **Conntrack**: eBPF-based connection tracking for efficient flow state management.

**Verifying acceleration is active:**

```bash
# Check agent logs for eBPF status
kubectl logs -n nova-system -l app.kubernetes.io/name=novaedge-agent | grep -E "XDP|AF_XDP|eBPF|sk_lookup"

# List loaded BPF programs
bpftool prog list

# Show XDP programs attached to interfaces
bpftool net show
```

To force the legacy path (for debugging or compatibility), use `--force-legacy-mesh` (mesh interception). See [eBPF/XDP Acceleration](../user-guide/ebpf-acceleration.md) for full details.

## Kernel Parameter Tuning

The NovaEdge agent checks kernel tuning parameters at startup and logs a warning for each parameter that is below the recommended value. These warnings appear at the `WARN` log level with the message `"kernel parameter below recommended value"` and include the current and recommended values.

The agent never modifies kernel parameters itself. Tuning must be applied externally by the operator.

### Recommended Parameters

The following parameters are checked and recommended for production proxy workloads:

| Parameter | Recommended Value | Description |
|-----------|------------------|-------------|
| `net.core.somaxconn` | `65535` | Maximum length of the listen backlog queue. The default (typically 4096) can cause connection drops under burst load. |
| `net.core.netdev_max_backlog` | `65535` | Maximum number of packets queued on the NIC receive path before the kernel processes them. Prevents packet drops under high packet rates. |
| `net.ipv4.tcp_max_syn_backlog` | `65535` | Maximum number of pending SYN requests (half-open connections). Protects against SYN floods and handles burst connection rates. |
| `net.core.rmem_max` | `16777216` (16 MB) | Maximum receive socket buffer size. Allows TCP auto-tuning to scale buffers for high-bandwidth connections. |
| `net.core.wmem_max` | `16777216` (16 MB) | Maximum send socket buffer size. Same rationale as `rmem_max`. |
| `net.ipv4.tcp_rmem` | `4096 87380 16777216` | TCP receive buffer auto-tuning range (min, default, max). The max must match `rmem_max` for full utilization on high-BDP paths. |
| `net.ipv4.tcp_wmem` | `4096 65536 16777216` | TCP send buffer auto-tuning range (min, default, max). The max must match `wmem_max`. |
| `net.ipv4.tcp_fin_timeout` | `10` | Time (seconds) a socket stays in FIN-WAIT-2 state. Lowering from the default 60s frees sockets faster on busy proxies. |
| `net.ipv4.tcp_tw_reuse` | `1` | Allow reuse of TIME_WAIT sockets for new outbound connections when safe (matching TCP timestamps). Critical for proxies making many short-lived upstream connections. |
| `net.ipv4.ip_local_port_range` | `1024 65535` | Range of ephemeral ports available for outbound connections. Widening from the default (32768-60999) increases the number of simultaneous upstream connections. |
| `net.ipv4.tcp_fastopen` | `3` | Enable TCP Fast Open for both client (1) and server (2) roles. Value 3 enables both. Saves one RTT on repeated connections from the same client. |
| `net.ipv4.tcp_slow_start_after_idle` | `0` | Disable congestion window reset after idle periods. Keeps the congestion window warm for persistent connections that have intermittent traffic. |
| `net.ipv4.tcp_keepalive_time` | `60` | Seconds before the first keepalive probe is sent on an idle connection. The default (7200s) is far too long for proxy workloads. |
| `net.ipv4.tcp_keepalive_intvl` | `10` | Seconds between keepalive probes after the first probe. |
| `net.ipv4.tcp_keepalive_probes` | `6` | Number of unacknowledged keepalive probes before the connection is considered dead. With the above settings, a dead connection is detected in 60 + (6 * 10) = 120 seconds. |
| `net.core.optmem_max` | `65536` (64 KB) | Maximum ancillary buffer size per socket. Used for socket options and control messages. |
| `net.ipv4.tcp_max_tw_buckets` | `2000000` | Maximum number of TIME_WAIT sockets allowed system-wide. Prevents the kernel from dropping TIME_WAIT entries prematurely, which can cause connection issues. |
| `net.ipv4.tcp_notsent_lowat` | `16384` (16 KB) | Threshold for unsent data before the kernel signals the socket as writable. Reduces bufferbloat and latency by preventing the send buffer from filling up with unsent data. |

### Applying Kernel Parameters

#### Method 1: Manual sysctl Commands

Apply parameters immediately (does not persist across reboots):

```bash
sysctl -w net.core.somaxconn=65535
sysctl -w net.core.netdev_max_backlog=65535
sysctl -w net.ipv4.tcp_max_syn_backlog=65535
sysctl -w net.core.rmem_max=16777216
sysctl -w net.core.wmem_max=16777216
sysctl -w net.ipv4.tcp_rmem="4096 87380 16777216"
sysctl -w net.ipv4.tcp_wmem="4096 65536 16777216"
sysctl -w net.ipv4.tcp_fin_timeout=10
sysctl -w net.ipv4.tcp_tw_reuse=1
sysctl -w net.ipv4.ip_local_port_range="1024 65535"
sysctl -w net.ipv4.tcp_fastopen=3
sysctl -w net.ipv4.tcp_slow_start_after_idle=0
sysctl -w net.ipv4.tcp_keepalive_time=60
sysctl -w net.ipv4.tcp_keepalive_intvl=10
sysctl -w net.ipv4.tcp_keepalive_probes=6
sysctl -w net.core.optmem_max=65536
sysctl -w net.ipv4.tcp_max_tw_buckets=2000000
sysctl -w net.ipv4.tcp_notsent_lowat=16384
```

#### Method 2: Persistent Configuration File

Create `/etc/sysctl.d/99-novaedge.conf` on each node:

```ini
# NovaEdge recommended kernel parameters
# Apply with: sysctl --system

net.core.somaxconn = 65535
net.core.netdev_max_backlog = 65535
net.ipv4.tcp_max_syn_backlog = 65535
net.core.rmem_max = 16777216
net.core.wmem_max = 16777216
net.ipv4.tcp_rmem = 4096 87380 16777216
net.ipv4.tcp_wmem = 4096 65536 16777216
net.ipv4.tcp_fin_timeout = 10
net.ipv4.tcp_tw_reuse = 1
net.ipv4.ip_local_port_range = 1024 65535
net.ipv4.tcp_fastopen = 3
net.ipv4.tcp_slow_start_after_idle = 0
net.ipv4.tcp_keepalive_time = 60
net.ipv4.tcp_keepalive_intvl = 10
net.ipv4.tcp_keepalive_probes = 6
net.core.optmem_max = 65536
net.ipv4.tcp_max_tw_buckets = 2000000
net.ipv4.tcp_notsent_lowat = 16384
```

Apply immediately without reboot:

```bash
sysctl --system
```

#### Method 3: Helm Chart Init Container

When deploying with the NovaEdge Helm chart, add a privileged init container to the agent DaemonSet that applies sysctl parameters before the agent starts. Add the following to your Helm values:

```yaml
agent:
  initContainers:
    - name: sysctl-tuning
      image: busybox:1.36
      securityContext:
        privileged: true
      command:
        - /bin/sh
        - -c
        - |
          sysctl -w net.core.somaxconn=65535
          sysctl -w net.core.netdev_max_backlog=65535
          sysctl -w net.ipv4.tcp_max_syn_backlog=65535
          sysctl -w net.core.rmem_max=16777216
          sysctl -w net.core.wmem_max=16777216
          sysctl -w net.ipv4.tcp_rmem="4096 87380 16777216"
          sysctl -w net.ipv4.tcp_wmem="4096 65536 16777216"
          sysctl -w net.ipv4.tcp_fin_timeout=10
          sysctl -w net.ipv4.tcp_tw_reuse=1
          sysctl -w net.ipv4.ip_local_port_range="1024 65535"
          sysctl -w net.ipv4.tcp_fastopen=3
          sysctl -w net.ipv4.tcp_slow_start_after_idle=0
          sysctl -w net.ipv4.tcp_keepalive_time=60
          sysctl -w net.ipv4.tcp_keepalive_intvl=10
          sysctl -w net.ipv4.tcp_keepalive_probes=6
          sysctl -w net.core.optmem_max=65536
          sysctl -w net.ipv4.tcp_max_tw_buckets=2000000
          sysctl -w net.ipv4.tcp_notsent_lowat=16384
```

The init container requires `privileged: true` because modifying kernel parameters in `/proc/sys` requires `CAP_SYS_ADMIN`. Since the NovaEdge agent already runs with `hostNetwork: true` and elevated privileges for VIP and network operations, this does not change the security posture of the DaemonSet.

## Running Performance Tests

NovaEdge includes an automated performance test suite that deploys load generators into the cluster and runs standardized scenarios against a live NovaEdge deployment.

### Go Micro-Benchmarks

Run the built-in Go micro-benchmarks for LB algorithms, routing, connection pooling, and policy enforcement:

```bash
make benchmark
```

This runs `go test -bench` with 3 iterations and memory allocation stats across the core agent packages.

### Live Cluster Performance Tests

The `test/performance/` directory contains a full end-to-end test suite using [fortio](https://github.com/fortio/fortio) for HTTP load testing and iperf3 for L4 TCP throughput:

```bash
# Run all scenarios (HTTP throughput, latency, connection ramp, TCP)
make perf-test

# Run HTTP throughput tests only
make perf-test-http

# Run TCP throughput tests only
make perf-test-tcp

# Run with pprof CPU/memory profiling
make perf-profile
```

The suite deploys fortio and iperf3 servers, creates NovaEdge CRDs (ProxyGateway, ProxyRoute, ProxyBackend), runs load as Kubernetes Jobs, and parses results into summary tables. See `test/performance/README.md` for full usage details.

### pprof Profiling

Both the agent and controller expose pprof endpoints for CPU and memory profiling during load tests:

- **Agent**: `127.0.0.1:9901/debug/pprof/` (via AdminServer, localhost only)
- **Controller**: `127.0.0.1:6060/debug/pprof/` (via debug server, localhost only)

Use `kubectl port-forward` to access from outside the cluster:

```bash
# Agent CPU profile (30 seconds)
kubectl -n nova-system port-forward pod/<agent-pod> 9901:9901
curl -o cpu.pprof http://127.0.0.1:9901/debug/pprof/profile?seconds=30
go tool pprof cpu.pprof

# Controller heap profile
kubectl -n nova-system port-forward pod/<controller-pod> 6060:6060
curl -o heap.pprof http://127.0.0.1:6060/debug/pprof/heap
go tool pprof heap.pprof
```

## Benchmarking

Use the following tools to measure the impact of performance tuning:

**HTTP throughput and latency:**

```bash
# Basic HTTP benchmark with wrk (10 threads, 400 connections, 30 seconds)
wrk -t10 -c400 -d30s http://<novaedge-vip>:<port>/

# Constant-rate latency measurement with wrk2 (10k req/s target)
wrk2 -t4 -c100 -d60s -R10000 http://<novaedge-vip>:<port>/
```

`wrk` is useful for measuring maximum throughput. `wrk2` is better for latency measurement because it maintains a constant request rate and produces accurate latency percentiles without coordinated omission.

**L4 TCP throughput:**

```bash
# Start iperf3 server behind NovaEdge
iperf3 -s -p 5201

# Measure TCP throughput through the proxy
iperf3 -c <novaedge-vip> -p <l4-port> -t 30 -P 4
```

Use multiple parallel streams (`-P`) to saturate the proxy and measure aggregate throughput. Compare results with and without kernel tuning to quantify the improvement.

**Key metrics to monitor during benchmarks:**

- Requests per second and p50/p99 latency (from wrk/wrk2 output)
- CPU usage of the novaedge-agent process (`top`, `pidstat`, or Prometheus `process_cpu_seconds_total`)
- Kernel network counters (`netstat -s` or `nstat`) -- look for SYN overflows, listen drops, and retransmissions
- NovaEdge Prometheus metrics: `novaedge_agent_request_count`, `novaedge_agent_request_duration_seconds`, `novaedge_agent_active_connections`
