// SPDX-License-Identifier: Apache-2.0
// Copyright 2024 NovaEdge Authors.
//
// afxdp_redirect.c — XDP program that redirects matching flows to an
// AF_XDP (XSK) socket for zero-copy userspace packet processing.
// Non-matching traffic is passed to the normal kernel stack via XDP_PASS.

#include <linux/bpf.h>
#include <linux/if_ether.h>
#include <linux/ip.h>
#include <linux/tcp.h>
#include <linux/udp.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

// Network constants needed for BPF target compilation where kernel
// headers may not fully resolve (standard BPF practice).
#ifndef IPPROTO_TCP
#define IPPROTO_TCP 6
#endif
#ifndef IPPROTO_UDP
#define IPPROTO_UDP 17
#endif

// VIP key for flow matching — same as xdp_lb.c for consistency.
struct vip_key {
    __u32 addr;     // IPv4 address in network byte order
    __u16 port;     // port in network byte order
    __u8  proto;    // IPPROTO_TCP or IPPROTO_UDP
    __u8  pad;
};

// XSK map for AF_XDP socket redirection.
// Each entry maps a queue ID to an AF_XDP socket file descriptor.
struct {
    __uint(type, BPF_MAP_TYPE_XSKMAP);
    __uint(max_entries, 64);
    __type(key, __u32);
    __type(value, __u32);
} xsk_map SEC(".maps");

// Hash set of VIP keys to redirect.
// If a flow matches an entry here, it is sent to the AF_XDP socket.
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 4096);
    __type(key, struct vip_key);
    __type(value, __u32);  // value unused; presence = match
} afxdp_vips SEC(".maps");

// Per-CPU statistics.
enum stat_idx {
    STAT_XDP_PASS = 0,
    STAT_XDP_REDIRECT,
    STAT_XDP_DROP,
    STAT_MATCH,
    STAT_MISS,
    __STAT_MAX,
};

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, __STAT_MAX);
    __type(key, __u32);
    __type(value, __u64);
} afxdp_stats SEC(".maps");

static __always_inline void inc_stat(enum stat_idx idx)
{
    __u32 key = idx;
    __u64 *val = bpf_map_lookup_elem(&afxdp_stats, &key);
    if (val)
        __sync_fetch_and_add(val, 1);
}

SEC("xdp")
int afxdp_redirect_prog(struct xdp_md *ctx)
{
    void *data     = (void *)(long)ctx->data;
    void *data_end = (void *)(long)ctx->data_end;

    // --- Parse Ethernet header ---
    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end) {
        inc_stat(STAT_XDP_PASS);
        return XDP_PASS;
    }

    if (eth->h_proto != bpf_htons(ETH_P_IP)) {
        inc_stat(STAT_XDP_PASS);
        return XDP_PASS;
    }

    // --- Parse IPv4 header ---
    struct iphdr *ip = (void *)(eth + 1);
    if ((void *)(ip + 1) > data_end) {
        inc_stat(STAT_XDP_PASS);
        return XDP_PASS;
    }

    __u8 proto = ip->protocol;
    if (proto != IPPROTO_TCP && proto != IPPROTO_UDP) {
        inc_stat(STAT_XDP_PASS);
        return XDP_PASS;
    }

    // --- Extract destination port ---
    __u16 dport = 0;
    __u32 l4_off = sizeof(*eth) + (ip->ihl * 4);

    if (proto == IPPROTO_TCP) {
        struct tcphdr *tcp = data + l4_off;
        if ((void *)(tcp + 1) > data_end) {
            inc_stat(STAT_XDP_PASS);
            return XDP_PASS;
        }
        dport = tcp->dest;
    } else {
        struct udphdr *udp = data + l4_off;
        if ((void *)(udp + 1) > data_end) {
            inc_stat(STAT_XDP_PASS);
            return XDP_PASS;
        }
        dport = udp->dest;
    }

    // --- Lookup VIP ---
    struct vip_key vk = {
        .addr  = ip->daddr,
        .port  = dport,
        .proto = proto,
    };

    if (!bpf_map_lookup_elem(&afxdp_vips, &vk)) {
        inc_stat(STAT_MISS);
        inc_stat(STAT_XDP_PASS);
        return XDP_PASS;
    }

    inc_stat(STAT_MATCH);

    // --- Redirect to AF_XDP socket on queue 0 ---
    __u32 queue_id = ctx->rx_queue_index;
    int ret = bpf_redirect_map(&xsk_map, queue_id, XDP_PASS);
    if (ret == XDP_REDIRECT) {
        inc_stat(STAT_XDP_REDIRECT);
    } else {
        // Fallback: socket not attached for this queue, pass to stack.
        inc_stat(STAT_XDP_PASS);
    }

    return ret;
}

char _license[] SEC("license") = "Apache-2.0";
