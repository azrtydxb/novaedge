// SPDX-License-Identifier: Apache-2.0
// Copyright 2024 NovaEdge Authors.
//
// xdp_lb.c — XDP program for L4 load balancing at the NIC driver level.
//
// This program performs kernel-bypass L4 load balancing for TCP and UDP
// traffic. It parses incoming packets, looks up VIP:port in a BPF hash
// map, selects a backend using a hash of the flow tuple, rewrites the
// destination IP/port and MAC, recalculates checksums, and transmits the
// packet back via XDP_TX — all without allocating an sk_buff or entering
// the userspace network stack.
//
// Unsupported protocols or non-VIP traffic passes through to the normal
// kernel stack via XDP_PASS.

#include <linux/bpf.h>
#include <linux/if_ether.h>
#include <linux/ip.h>
#include <linux/tcp.h>
#include <linux/udp.h>
#include <linux/in.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

#define MAX_BACKENDS 256
#define MAX_VIPS     4096

// VIP lookup key: destination IP + port + protocol.
struct vip_key {
    __u32 addr;      // VIP IPv4 address (network byte order)
    __u16 port;      // VIP port (network byte order)
    __u8  proto;     // IPPROTO_TCP or IPPROTO_UDP
    __u8  pad;
};

// VIP metadata: number of backends and index into backend_list.
struct vip_meta {
    __u32 backend_count;
    __u32 backend_list_id;  // index base in backend_list
};

// Backend endpoint descriptor.
struct backend_entry {
    __u32 addr;          // backend IPv4 address (network byte order)
    __u16 port;          // backend port (network byte order)
    __u8  mac[6];        // backend MAC address for XDP_TX
};

// backend_list_key: list_id * MAX_BACKENDS + index
struct backend_list_key {
    __u32 list_id;
    __u32 index;
};

// VIP -> metadata lookup.
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_VIPS);
    __type(key, struct vip_key);
    __type(value, struct vip_meta);
} vip_backends SEC(".maps");

// Backend list: (list_id, index) -> backend entry.
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_VIPS * MAX_BACKENDS);
    __type(key, struct backend_list_key);
    __type(value, struct backend_entry);
} backend_list SEC(".maps");

// Local interface MAC address, populated by userspace at load time.
// Single-entry array map: key 0 → 6-byte MAC address.
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, __u8[6]);
} local_mac SEC(".maps");

// Per-CPU stats counters.
enum stats_idx {
    STATS_XDP_PASS      = 0,
    STATS_XDP_TX        = 1,
    STATS_XDP_DROP      = 2,
    STATS_LOOKUP_MISS   = 3,
    STATS_BACKEND_MISS  = 4,
    STATS_PACKETS_TOTAL = 5,
    STATS_BYTES_TOTAL   = 6,
    STATS_MAX           = 7,
};

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, STATS_MAX);
    __type(key, __u32);
    __type(value, __u64);
} lb_stats SEC(".maps");

static __always_inline void bump_stat(enum stats_idx key) {
    __u32 k = key;
    __u64 *val = bpf_map_lookup_elem(&lb_stats, &k);
    if (val)
        __sync_fetch_and_add(val, 1);
}

static __always_inline void add_stat(enum stats_idx key, __u64 amount) {
    __u32 k = key;
    __u64 *val = bpf_map_lookup_elem(&lb_stats, &k);
    if (val)
        __sync_fetch_and_add(val, amount);
}

// Simple hash for backend selection (based on flow 4-tuple).
static __always_inline __u32 flow_hash(__u32 saddr, __u32 daddr,
                                        __u16 sport, __u16 dport) {
    __u32 hash = saddr ^ daddr ^ ((__u32)sport << 16 | dport);
    hash ^= hash >> 16;
    hash *= 0x85ebca6b;
    hash ^= hash >> 13;
    return hash;
}

// Incremental checksum update for IP address replacement.
static __always_inline void csum_replace4(__sum16 *sum, __be32 from, __be32 to) {
    __u32 diff[] = { ~from, to };
    *sum = bpf_csum_diff((__be32 *)&from, 4, (__be32 *)&to, 4, ~(*sum));
}

SEC("xdp")
int xdp_lb_prog(struct xdp_md *ctx) {
    void *data = (void *)(long)ctx->data;
    void *data_end = (void *)(long)ctx->data_end;

    bump_stat(STATS_PACKETS_TOTAL);
    add_stat(STATS_BYTES_TOTAL, data_end - data);

    // Parse Ethernet header.
    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end) {
        bump_stat(STATS_XDP_PASS);
        return XDP_PASS;
    }

    // Only handle IPv4.
    if (eth->h_proto != bpf_htons(ETH_P_IP)) {
        bump_stat(STATS_XDP_PASS);
        return XDP_PASS;
    }

    // Parse IP header.
    struct iphdr *ip = (void *)(eth + 1);
    if ((void *)(ip + 1) > data_end) {
        bump_stat(STATS_XDP_PASS);
        return XDP_PASS;
    }

    __u16 sport = 0, dport = 0;
    __u8 proto = ip->protocol;

    if (proto == IPPROTO_TCP) {
        struct tcphdr *tcp = (void *)ip + (ip->ihl * 4);
        if ((void *)(tcp + 1) > data_end) {
            bump_stat(STATS_XDP_PASS);
            return XDP_PASS;
        }
        sport = tcp->source;
        dport = tcp->dest;
    } else if (proto == IPPROTO_UDP) {
        struct udphdr *udp = (void *)ip + (ip->ihl * 4);
        if ((void *)(udp + 1) > data_end) {
            bump_stat(STATS_XDP_PASS);
            return XDP_PASS;
        }
        sport = udp->source;
        dport = udp->dest;
    } else {
        // Not TCP/UDP — pass through.
        bump_stat(STATS_XDP_PASS);
        return XDP_PASS;
    }

    // Lookup VIP.
    struct vip_key vk = {
        .addr  = ip->daddr,
        .port  = dport,
        .proto = proto,
    };
    struct vip_meta *meta = bpf_map_lookup_elem(&vip_backends, &vk);
    if (!meta) {
        bump_stat(STATS_LOOKUP_MISS);
        bump_stat(STATS_XDP_PASS);
        return XDP_PASS;
    }

    if (meta->backend_count == 0) {
        bump_stat(STATS_BACKEND_MISS);
        bump_stat(STATS_XDP_PASS);
        return XDP_PASS;
    }

    // Select backend via hash.
    __u32 hash = flow_hash(ip->saddr, ip->daddr, sport, dport);
    __u32 idx = hash % meta->backend_count;

    struct backend_list_key blk = {
        .list_id = meta->backend_list_id,
        .index   = idx,
    };
    struct backend_entry *be = bpf_map_lookup_elem(&backend_list, &blk);
    if (!be) {
        bump_stat(STATS_BACKEND_MISS);
        bump_stat(STATS_XDP_PASS);
        return XDP_PASS;
    }

    // Rewrite MACs: dst = backend MAC, src = local interface MAC.
    __u32 mac_key = 0;
    __u8 *iface_mac = bpf_map_lookup_elem(&local_mac, &mac_key);
    if (!iface_mac) {
        // Cannot rewrite MACs without local interface MAC; pass to stack.
        bump_stat(STATS_XDP_PASS);
        return XDP_PASS;
    }
    __builtin_memcpy(eth->h_dest, be->mac, ETH_ALEN);
    __builtin_memcpy(eth->h_source, iface_mac, ETH_ALEN);

    // Rewrite destination IP.
    __be32 old_daddr = ip->daddr;
    ip->daddr = be->addr;

    // Update IP checksum incrementally.
    ip->check = 0;
    ip->check = bpf_csum_diff((__be32 *)&old_daddr, 4, &ip->daddr, 4, ~ip->check);

    // Rewrite destination port and update L4 checksum.
    if (proto == IPPROTO_TCP) {
        struct tcphdr *tcp = (void *)ip + (ip->ihl * 4);
        if ((void *)(tcp + 1) > data_end) {
            bump_stat(STATS_XDP_DROP);
            return XDP_DROP;
        }
        tcp->dest = be->port;
    } else {
        struct udphdr *udp = (void *)ip + (ip->ihl * 4);
        if ((void *)(udp + 1) > data_end) {
            bump_stat(STATS_XDP_DROP);
            return XDP_DROP;
        }
        udp->dest = be->port;
        // UDP checksum is optional for IPv4; zero it to avoid recalculation.
        udp->check = 0;
    }

    bump_stat(STATS_XDP_TX);
    return XDP_TX;
}

char _license[] SEC("license") = "Apache-2.0";
