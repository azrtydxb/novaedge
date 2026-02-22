// SPDX-License-Identifier: Apache-2.0
// Copyright 2024 NovaEdge Authors.
//
// mesh_redirect.c — BPF_PROG_TYPE_SK_LOOKUP program for transparent service
// mesh interception.
//
// This program replaces nftables/iptables NAT REDIRECT for mesh traffic
// interception. When a packet's destination matches a mesh-intercepted
// ClusterIP:port, the program redirects the connection to the local TPROXY
// listener socket using bpf_sk_lookup_tcp + bpf_sk_assign.
//
// Key advantages over NAT REDIRECT:
//   - No conntrack entries created (reduces kernel memory and table pressure)
//   - No packet rewriting (preserves original dst for SO_ORIGINAL_DST)
//   - Runs before netfilter, so no priority ordering issues with kube-proxy
//   - Lower latency on high-connection-rate workloads

#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

// Network constants needed for BPF target compilation where kernel
// headers may not fully resolve (standard BPF practice).
#ifndef AF_INET
#define AF_INET 2
#endif
#ifndef IPPROTO_TCP
#define IPPROTO_TCP 6
#endif

// mesh_services maps {dst_ip, dst_port} -> {redirect_port}.
// When a connection matches, we redirect it to the local TPROXY listener
// on redirect_port.
struct mesh_svc_key {
    __u32 addr;     // destination IPv4 address (network byte order)
    __u16 port;     // destination port (network byte order)
    __u16 pad;      // padding for alignment
};

struct mesh_svc_value {
    __u32 redirect_port;   // local TPROXY listener port (host byte order)
};

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 65536);
    __type(key, struct mesh_svc_key);
    __type(value, struct mesh_svc_value);
} mesh_services SEC(".maps");

// stats_key indexes into the stats array map.
enum stats_key {
    STATS_LOOKUP_TOTAL = 0,
    STATS_REDIRECT_OK  = 1,
    STATS_REDIRECT_ERR = 2,
    STATS_PASS         = 3,
    STATS_MAX          = 4,
};

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, STATS_MAX);
    __type(key, __u32);
    __type(value, __u64);
} mesh_redirect_stats SEC(".maps");

static __always_inline void bump_stat(enum stats_key key) {
    __u32 k = key;
    __u64 *val = bpf_map_lookup_elem(&mesh_redirect_stats, &k);
    if (val)
        __sync_fetch_and_add(val, 1);
}

SEC("sk_lookup/mesh_redirect")
int mesh_redirect_prog(struct bpf_sk_lookup *ctx) {
    bump_stat(STATS_LOOKUP_TOTAL);

    // Only intercept TCP (protocol 6).
    if (ctx->protocol != IPPROTO_TCP) {
        bump_stat(STATS_PASS);
        return SK_PASS;
    }

    // Only intercept IPv4 for now. IPv6 support can be added with a
    // separate map keyed by 128-bit addresses.
    if (ctx->family != AF_INET) {
        bump_stat(STATS_PASS);
        return SK_PASS;
    }

    // Build lookup key from the connection's destination.
    struct mesh_svc_key key = {
        .addr = ctx->local_ip4,
        .port = ctx->local_port,   // already in host byte order in sk_lookup
        .pad  = 0,
    };
    // Note: local_port in sk_lookup context is the destination port of the
    // incoming packet, in host byte order.
    key.port = bpf_htons((__u16)ctx->local_port);

    struct mesh_svc_value *svc = bpf_map_lookup_elem(&mesh_services, &key);
    if (!svc) {
        // Not a mesh-intercepted service — let the packet proceed normally.
        bump_stat(STATS_PASS);
        return SK_PASS;
    }

    // Look up the local TPROXY listener socket.
    struct bpf_sock_tuple tuple = {};
    tuple.ipv4.daddr = ctx->local_ip4;
    tuple.ipv4.dport = bpf_htons((__u16)svc->redirect_port);
    tuple.ipv4.saddr = ctx->remote_ip4;
    tuple.ipv4.sport = bpf_htons(ctx->remote_port);

    struct bpf_sock *sk = bpf_sk_lookup_tcp(ctx, &tuple,
                                             sizeof(tuple.ipv4),
                                             BPF_F_CURRENT_NETNS, 0);
    if (!sk) {
        // Listener not found — this shouldn't happen if the mesh manager
        // is running. Pass through to avoid dropping traffic.
        bump_stat(STATS_REDIRECT_ERR);
        return SK_PASS;
    }

    // Assign the socket to this connection, bypassing the normal socket
    // lookup. The connection will be delivered to the TPROXY listener.
    long err = bpf_sk_assign(ctx, sk, 0);
    bpf_sk_release(sk);

    if (err) {
        bump_stat(STATS_REDIRECT_ERR);
        return SK_PASS;
    }

    bump_stat(STATS_REDIRECT_OK);
    return SK_PASS;
}

char _license[] SEC("license") = "Apache-2.0";
