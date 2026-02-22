# eBPF/XDP Data Plane Acceleration

NovaEdge uses eBPF/XDP acceleration by default for the data plane, providing
kernel-bypass packet processing that dramatically reduces latency and increases
throughput for L4 load balancing and service mesh traffic interception.

## Overview

Three eBPF acceleration features are available. All are **enabled by default**
and auto-detected at runtime. If the kernel does not support a feature, the
agent transparently falls back to the legacy path.

| Feature | Program Type | Legacy Fallback | Auto-detected |
|---------|-------------|----------------|---------------|
| **eBPF Mesh Redirect** | `BPF_PROG_TYPE_SK_LOOKUP` | nftables/iptables TPROXY | Yes |
| **XDP L4 Load Balancing** | `BPF_PROG_TYPE_XDP` | Userspace TCP/UDP proxy | Yes (requires `--xdp-interface`) |
| **AF_XDP Zero-Copy** | XDP + `AF_XDP` socket | Kernel network stack | Yes (requires `--xdp-interface`) |

To **force** the legacy path, use `--force-legacy-lb` (L4/AF_XDP) or
`--force-legacy-mesh` (mesh interception).

## Prerequisites

### Kernel Requirements

| Feature | Minimum Kernel | Required Support |
|---------|---------------|-----------------|
| eBPF Mesh Redirect | 5.9+ | `BPF_PROG_TYPE_SK_LOOKUP` |
| XDP L4 LB | 5.8+ | XDP driver mode on NIC |
| AF_XDP | 5.10+ | XDP + AF_XDP socket support |

Check your kernel version:

```bash
uname -r
```

### BTF Support

BTF (BPF Type Format) is recommended for CO-RE (Compile Once, Run Everywhere)
portability. Verify BTF is available:

```bash
ls /sys/kernel/btf/vmlinux
```

### Capabilities

The agent pod requires these Linux capabilities for eBPF acceleration:

- `CAP_BPF` — load and manage BPF programs and maps
- `CAP_NET_ADMIN` — attach XDP programs to network interfaces
- `CAP_SYS_ADMIN` — required on kernels < 5.8 that lack `CAP_BPF`

These are included in the default Helm chart security context.

## Helm Configuration

eBPF acceleration is enabled by default. Set `xdpInterface` to specify the
NIC for XDP/AF_XDP attachment:

```yaml
# charts/novaedge-agent/values.yaml
ebpf:
  # Mount /sys/fs/bpf for BPF map pinning
  bpffsMount: true

  # NIC for XDP/AF_XDP — enables L4 LB and zero-copy acceleration
  xdpInterface: eth0

  # Force legacy paths (default: false — eBPF auto-detected)
  forceLegacyLb: false
  forceLegacyMesh: false

# Capabilities are included by default
securityContext:
  capabilities:
    add:
      - NET_ADMIN
      - NET_RAW
      - NET_BIND_SERVICE
      - BPF
      - SYS_ADMIN
    drop:
      - ALL
```

## Agent Flags

```bash
# Default: eBPF auto-detected, XDP on eth0
novaedge-agent \
  --xdp-interface eth0 \
  --mesh-enabled

# Force legacy userspace L4 proxy (skip XDP/AF_XDP)
novaedge-agent \
  --force-legacy-lb \
  --mesh-enabled

# Force legacy nftables/iptables mesh interception (skip eBPF sk_lookup)
novaedge-agent \
  --xdp-interface eth0 \
  --mesh-enabled \
  --force-legacy-mesh
```

The eBPF mesh redirect is automatically detected and used when
`--mesh-enabled` is set and the kernel supports `SK_LOOKUP`.

## Architecture

### Packet Flow Without eBPF (Legacy)

```mermaid
graph LR
    NIC[NIC] --> KS[Kernel Stack]
    KS --> US[Userspace Proxy]
    US --> KS2[Kernel Stack]
    KS2 --> NIC2[NIC]
```

Every packet crosses the kernel-userspace boundary twice, incurring context
switches, memory copies, and syscall overhead.

### Packet Flow With XDP LB

```mermaid
graph LR
    NIC[NIC] --> XDP[XDP Program]
    XDP -->|Match| NIC2[NIC TX]
    XDP -->|No match| KS[Kernel Stack]
    KS --> US[Userspace Proxy]
```

Matched VIP traffic is rewritten and forwarded at the NIC driver level
without ever entering the kernel network stack. Non-matching traffic
passes through normally.

### Packet Flow With AF_XDP

```mermaid
graph LR
    NIC[NIC] --> XDP[XDP Filter]
    XDP -->|Match| XSK[AF_XDP Socket]
    XSK --> US[Userspace Worker]
    US --> XSK2[AF_XDP Socket]
    XSK2 --> NIC2[NIC]
    XDP -->|No match| KS[Kernel Stack]
```

AF_XDP provides zero-copy packet I/O between the NIC and userspace via
shared memory ring buffers, eliminating kernel stack traversal while
maintaining full userspace programmability.

## How It Works

### eBPF Mesh Redirect

The `SK_LOOKUP` program intercepts socket lookups for TCP connections matching
service mesh targets. Instead of using nftables TPROXY rules, the BPF program
directly assigns the connection to the TPROXY listener socket using
`bpf_sk_assign()`. This eliminates the overhead of traversing the entire
netfilter/nftables rule chain.

### XDP L4 Load Balancing

The XDP program runs at the earliest point in the receive path — before
`sk_buff` allocation. For each incoming packet:

1. Parse Ethernet, IPv4, and TCP/UDP headers
2. Look up destination VIP in a BPF hash map
3. Select a backend using a flow-based hash
4. Rewrite destination IP, port, and MAC address
5. Return `XDP_TX` to transmit the modified packet back out the NIC

Only plain TCP/UDP L4 listeners are offloaded. TLS passthrough listeners
remain in userspace because they require SNI inspection.

### AF_XDP Zero-Copy

AF_XDP extends XDP with userspace zero-copy packet processing:

1. An XDP filter program matches flows against a VIP set
2. Matched packets are redirected to an AF_XDP socket via `bpf_redirect_map()`
3. The userspace worker reads packets from the UMEM ring buffer
4. Processed responses are written back through the TX ring
5. Non-matching packets pass to the normal kernel stack

## Monitoring

### Prometheus Metrics

All eBPF subsystems expose Prometheus metrics:

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `novaedge_ebpf_programs_loaded` | Gauge | `subsystem` | Number of loaded BPF programs |
| `novaedge_ebpf_map_operations_total` | Counter | `map`, `op`, `result` | BPF map operations |
| `novaedge_ebpf_errors_total` | Counter | `subsystem`, `type` | BPF-related errors |
| `novaedge_ebpf_attach_duration_seconds` | Histogram | `subsystem` | Time to load and attach programs |

### Verifying with bpftool

```bash
# List loaded BPF programs
bpftool prog list

# Show XDP programs attached to interfaces
bpftool net show

# Dump BPF map contents
bpftool map dump name vip_backends

# Show per-CPU statistics
bpftool map dump name lb_stats
```

### Agent Logs

Look for these log messages to confirm eBPF acceleration is active:

```
# eBPF mesh redirect (auto-detected)
{"msg": "using eBPF sk_lookup backend for mesh interception"}

# XDP L4 LB (auto-detected when --xdp-interface is set)
{"msg": "XDP L4 load balancing active", "interface": "eth0"}

# AF_XDP (auto-detected when --xdp-interface is set)
{"msg": "AF_XDP zero-copy fast path enabled", "interface": "eth0"}

# Fallback messages (kernel doesn't support eBPF feature)
{"msg": "XDP L4 LB not available, using userspace proxy"}
{"msg": "AF_XDP not available, using kernel stack"}
{"msg": "eBPF mesh redirect disabled by --force-legacy-mesh, using nftables/iptables"}
```

## Troubleshooting

### BPF program fails to load

**Symptom:** Agent logs `XDP L4 LB not available, using userspace proxy`

**Common causes:**

1. **Kernel too old** — check `uname -r` against the requirements table above
2. **Missing capabilities** — ensure `CAP_BPF` and `CAP_NET_ADMIN` are granted
3. **BTF not available** — check `/sys/kernel/btf/vmlinux` exists
4. **NIC driver doesn't support XDP** — not all NICs support XDP driver mode;
   virtual NICs (veth, bridge) use generic XDP which is slower

### XDP program attached but no traffic accelerated

**Check the BPF maps:**

```bash
# Verify VIP entries exist
bpftool map dump name vip_backends
# Should show entries for your configured VIPs

# Check statistics
bpftool map dump name lb_stats
# xdp_tx counter should increase with traffic
```

**Common causes:**

1. VIP address not yet assigned to this node
2. No L4 listeners configured for the VIP port
3. Backends not ready (health checks failing)

### Permission denied errors

Ensure the agent container runs with sufficient privileges:

```yaml
securityContext:
  capabilities:
    add: [BPF, NET_ADMIN, SYS_ADMIN, NET_RAW, NET_BIND_SERVICE]
```

On some distributions, you may also need `privileged: true` in the pod
security context.

### Forcing legacy mode

If eBPF causes issues, you can force the legacy path without changing
the kernel or capabilities:

```bash
# Force legacy for L4 load balancing and AF_XDP
novaedge-agent --force-legacy-lb ...

# Force legacy for mesh interception
novaedge-agent --force-legacy-mesh ...
```

Or via Helm:

```yaml
ebpf:
  forceLegacyLb: true
  forceLegacyMesh: true
```
