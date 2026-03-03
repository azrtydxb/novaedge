// SPDX-License-Identifier: Apache-2.0
// Copyright 2024 NovaEdge Authors.
//
// conntrack.c -- eBPF LRU connection tracking for XDP load balancing.
//
// This program defines an LRU hash map for tracking established connections
// and pinning them to specific backends across Maglev table rebuilds.
//
// Flow: on packet arrival, first check the conntrack table. If a match is
// found, use the pinned backend (bypassing the Maglev lookup). If no match,
// fall through to the Maglev table and create a new conntrack entry.
//
// The LRU eviction policy ensures that idle connections are reclaimed
// automatically without requiring explicit garbage collection in the
// critical path. The Go control plane runs periodic GC to remove entries
// that have exceeded their maximum age.

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

// Default max entries for the LRU conntrack table.
#define CT_MAX_ENTRIES 65536

// Connection states.
#define CT_STATE_SYN_SENT    0
#define CT_STATE_ESTABLISHED 1
#define CT_STATE_FIN_WAIT    2
#define CT_STATE_CLOSED      3

// Connection tracking key: 5-tuple identifying a flow.
struct ct_key {
    __u32 src_ip;       // source IPv4 (network byte order)
    __u32 dst_ip;       // destination IPv4 (network byte order)
    __u16 src_port;     // source port (network byte order)
    __u16 dst_port;     // destination port (network byte order)
    __u8  proto;        // IPPROTO_TCP or IPPROTO_UDP
    __u8  pad[3];       // padding to 16 bytes
};

// Connection tracking entry: pinned backend and metadata.
struct ct_entry {
    __u32 backend_ip;    // resolved backend IPv4 (network byte order)
    __u16 backend_port;  // resolved backend port (network byte order)
    __u8  state;         // connection state (CT_STATE_*)
    __u8  pad;
    __u64 timestamp;     // last packet timestamp (ktime_ns)
    __u64 rx_bytes;      // bytes received from client
    __u64 tx_bytes;      // bytes sent to client
    __u8  backend_mac[6]; // resolved backend MAC address for XDP_TX
    __u8  pad2[2];       // alignment padding
};

// LRU hash map for connection tracking. The kernel automatically evicts
// the least-recently-used entries when the map is full, providing
// bounded memory usage without explicit cleanup in the data path.
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, CT_MAX_ENTRIES);
    __type(key, struct ct_key);
    __type(value, struct ct_entry);
} novaedge_ct SEC(".maps");

// Local interface MAC address, populated by userspace at load time.
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, __u8[6]);
} ct_local_mac SEC(".maps");

// Per-CPU statistics for conntrack operations.
enum ct_stat_idx {
    CT_STAT_LOOKUPS  = 0,
    CT_STAT_HITS     = 1,
    CT_STAT_MISSES   = 2,
    CT_STAT_INSERTS  = 3,
    CT_STAT_UPDATES  = 4,
    CT_STAT_MAX      = 5,
};

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, CT_STAT_MAX);
    __type(key, __u32);
    __type(value, __u64);
} ct_stats SEC(".maps");

static __always_inline void ct_bump_stat(enum ct_stat_idx key) {
    __u32 k = key;
    __u64 *val = bpf_map_lookup_elem(&ct_stats, &k);
    if (val)
        __sync_fetch_and_add(val, 1);
}

// ct_lookup checks the conntrack table for an existing flow. Returns the
// entry if found, or NULL if no entry exists.
static __always_inline struct ct_entry *
ct_lookup(struct ct_key *key) {
    ct_bump_stat(CT_STAT_LOOKUPS);

    struct ct_entry *entry = bpf_map_lookup_elem(&novaedge_ct, key);
    if (entry) {
        ct_bump_stat(CT_STAT_HITS);
        // Update timestamp on every hit to refresh LRU ordering.
        entry->timestamp = bpf_ktime_get_ns();
        return entry;
    }

    ct_bump_stat(CT_STAT_MISSES);
    return NULL;
}

// ct_insert creates a new conntrack entry for a flow.
static __always_inline int
ct_insert(struct ct_key *key, __u32 backend_ip, __u16 backend_port,
          __u8 *backend_mac, __u8 state, __u64 pkt_len) {
    struct ct_entry entry = {
        .backend_ip   = backend_ip,
        .backend_port = backend_port,
        .state        = state,
        .timestamp    = bpf_ktime_get_ns(),
        .rx_bytes     = pkt_len,
        .tx_bytes     = 0,
    };
    if (backend_mac)
        __builtin_memcpy(entry.backend_mac, backend_mac, 6);

    int ret = bpf_map_update_elem(&novaedge_ct, key, &entry, BPF_ANY);
    if (ret == 0)
        ct_bump_stat(CT_STAT_INSERTS);

    return ret;
}

// ct_update_bytes increments byte counters on an existing entry.
static __always_inline void
ct_update_bytes(struct ct_entry *entry, __u64 rx, __u64 tx) {
    __sync_fetch_and_add(&entry->rx_bytes, rx);
    __sync_fetch_and_add(&entry->tx_bytes, tx);
    ct_bump_stat(CT_STAT_UPDATES);
}

// Stub XDP program demonstrating conntrack-aware load balancing.
// In production, the conntrack lookup is integrated into the main
// xdp_lb_prog or maglev_xdp_prog. This program serves as a
// standalone reference implementation.
SEC("xdp")
int ct_xdp_prog(struct xdp_md *ctx) {
    void *data = (void *)(long)ctx->data;
    void *data_end = (void *)(long)ctx->data_end;

    // Parse Ethernet header.
    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end)
        return XDP_PASS;

    if (eth->h_proto != bpf_htons(ETH_P_IP))
        return XDP_PASS;

    // Parse IP header.
    struct iphdr *ip = (void *)(eth + 1);
    if ((void *)(ip + 1) > data_end)
        return XDP_PASS;

    __u16 sport = 0, dport = 0;
    __u8 proto = ip->protocol;

    if (proto == IPPROTO_TCP) {
        struct tcphdr *tcp = (void *)ip + (ip->ihl * 4);
        if ((void *)(tcp + 1) > data_end)
            return XDP_PASS;
        sport = tcp->source;
        dport = tcp->dest;
    } else if (proto == IPPROTO_UDP) {
        struct udphdr *udp = (void *)ip + (ip->ihl * 4);
        if ((void *)(udp + 1) > data_end)
            return XDP_PASS;
        sport = udp->source;
        dport = udp->dest;
    } else {
        return XDP_PASS;
    }

    // Build conntrack key from packet 5-tuple.
    struct ct_key ck = {
        .src_ip   = ip->saddr,
        .dst_ip   = ip->daddr,
        .src_port = sport,
        .dst_port = dport,
        .proto    = proto,
    };

    __u64 pkt_len = data_end - data;

    // Check conntrack first.
    struct ct_entry *ct = ct_lookup(&ck);
    if (ct) {
        // Existing connection: use pinned backend.
        ct_update_bytes(ct, pkt_len, 0);

        // Look up local interface MAC for source rewrite.
        __u32 mac_key = 0;
        __u8 *iface_mac = bpf_map_lookup_elem(&ct_local_mac, &mac_key);
        if (!iface_mac)
            return XDP_PASS;

        // Rewrite MACs: dst = backend, src = local interface.
        __builtin_memcpy(eth->h_dest, ct->backend_mac, ETH_ALEN);
        __builtin_memcpy(eth->h_source, iface_mac, ETH_ALEN);

        // Rewrite destination to pinned backend.
        ip->daddr = ct->backend_ip;

        if (proto == IPPROTO_TCP) {
            struct tcphdr *tcp = (void *)ip + (ip->ihl * 4);
            if ((void *)(tcp + 1) > data_end)
                return XDP_DROP;
            tcp->dest = ct->backend_port;
        } else {
            struct udphdr *udp = (void *)ip + (ip->ihl * 4);
            if ((void *)(udp + 1) > data_end)
                return XDP_DROP;
            udp->dest = ct->backend_port;
            udp->check = 0;
        }

        ip->check = 0;
        return XDP_TX;
    }

    // No conntrack entry: this is where the Maglev lookup would happen
    // in an integrated program. For this stub, pass to stack.
    return XDP_PASS;
}

char _license[] SEC("license") = "Apache-2.0";
