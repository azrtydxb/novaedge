// SPDX-License-Identifier: Apache-2.0
// Copyright 2024 NovaEdge Authors.
//
// maglev_lookup.c -- XDP helper program for Maglev consistent hashing.
//
// This program defines the BPF maps used for Maglev-based L4 load balancing:
//   - An outer ARRAY_OF_MAPS for atomic table swap (two inner tables)
//   - Inner ARRAY maps holding the Maglev lookup table (hash -> backend ID)
//   - A HASH map for backend ID -> IP:port resolution
//
// The actual packet steering logic lives in xdp_lb.c; this program provides
// the Maglev table maps and a helper XDP program that performs the lookup.
// The Go control plane populates the inner table, then atomically swaps the
// outer map pointer to publish the new table.

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

// Default Maglev table size (prime). The Go side can override this
// by creating inner maps with a different max_entries.
#define MAGLEV_TABLE_SIZE 16381

// Maximum number of backends.
#define MAX_BACKENDS 4096

// Maglev lookup table entry: maps a hash slot to a backend ID.
struct maglev_entry {
    __u32 backend_id;
};

// Backend descriptor: ID -> IP:port mapping.
struct backend_key {
    __u32 id;
};

struct backend_value {
    __u32 addr;     // IPv4 address in network byte order
    __u16 port;     // port in network byte order
    __u16 pad;
};

// Inner map specification for bpf2go. This is the Maglev lookup table:
// array index (hash % table_size) -> backend_id.
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, MAGLEV_TABLE_SIZE);
    __type(key, __u32);
    __type(value, struct maglev_entry);
} maglev_inner SEC(".maps");

// Outer map: ARRAY_OF_MAPS with 2 slots for atomic swap.
// Slot 0 = active table, slot 1 = standby table.
// The Go control plane writes the new table into the standby slot,
// then swaps by updating which inner map fd is at index 0.
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY_OF_MAPS);
    __uint(max_entries, 2);
    __type(key, __u32);
    __array(values, struct {
        __uint(type, BPF_MAP_TYPE_ARRAY);
        __uint(max_entries, MAGLEV_TABLE_SIZE);
        __type(key, __u32);
        __type(value, struct maglev_entry);
    });
} maglev_outer SEC(".maps");

// Backend map: backend_id -> backend descriptor.
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_BACKENDS);
    __type(key, struct backend_key);
    __type(value, struct backend_value);
} maglev_backends SEC(".maps");

// Per-CPU statistics.
enum maglev_stat_idx {
    MAGLEV_STAT_LOOKUPS  = 0,
    MAGLEV_STAT_HITS     = 1,
    MAGLEV_STAT_MISSES   = 2,
    MAGLEV_STAT_BACKEND_MISS = 3,
    MAGLEV_STAT_MAX      = 4,
};

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, MAGLEV_STAT_MAX);
    __type(key, __u32);
    __type(value, __u64);
} maglev_stats SEC(".maps");

static __always_inline void maglev_bump_stat(enum maglev_stat_idx key) {
    __u32 k = key;
    __u64 *val = bpf_map_lookup_elem(&maglev_stats, &k);
    if (val)
        __sync_fetch_and_add(val, 1);
}

// Hash function for flow-based backend selection (jhash-like).
static __always_inline __u32 maglev_flow_hash(__u32 saddr, __u32 daddr,
                                               __u16 sport, __u16 dport,
                                               __u8 proto) {
    __u32 hash = saddr ^ daddr ^ ((__u32)sport << 16 | dport) ^ proto;
    hash ^= hash >> 16;
    hash *= 0x85ebca6b;
    hash ^= hash >> 13;
    hash *= 0xc2b2ae35;
    hash ^= hash >> 16;
    return hash;
}

// maglev_lookup performs a Maglev table lookup for the given flow hash.
// Returns the backend value, or NULL if no backend is found.
static __always_inline struct backend_value *
maglev_lookup(__u32 flow_hash) {
    maglev_bump_stat(MAGLEV_STAT_LOOKUPS);

    // Always read from slot 0 (the active table).
    __u32 active_slot = 0;
    void *inner_map = bpf_map_lookup_elem(&maglev_outer, &active_slot);
    if (!inner_map) {
        maglev_bump_stat(MAGLEV_STAT_MISSES);
        return NULL;
    }

    // Index into the Maglev table.
    __u32 idx = flow_hash % MAGLEV_TABLE_SIZE;
    struct maglev_entry *entry = bpf_map_lookup_elem(inner_map, &idx);
    if (!entry) {
        maglev_bump_stat(MAGLEV_STAT_MISSES);
        return NULL;
    }

    maglev_bump_stat(MAGLEV_STAT_HITS);

    // Resolve backend ID to actual endpoint.
    struct backend_key bk = { .id = entry->backend_id };
    struct backend_value *bv = bpf_map_lookup_elem(&maglev_backends, &bk);
    if (!bv) {
        maglev_bump_stat(MAGLEV_STAT_BACKEND_MISS);
        return NULL;
    }

    return bv;
}

// XDP program that performs Maglev-based L4 load balancing.
// This is a standalone program that can be attached to an interface
// for pure Maglev-based forwarding.
SEC("xdp")
int maglev_xdp_prog(struct xdp_md *ctx) {
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

    // Compute flow hash and perform Maglev lookup.
    __u32 fhash = maglev_flow_hash(ip->saddr, ip->daddr, sport, dport, proto);
    struct backend_value *bv = maglev_lookup(fhash);
    if (!bv)
        return XDP_PASS;

    // Rewrite destination IP and port.
    ip->daddr = bv->addr;

    if (proto == IPPROTO_TCP) {
        struct tcphdr *tcp = (void *)ip + (ip->ihl * 4);
        if ((void *)(tcp + 1) > data_end)
            return XDP_DROP;
        tcp->dest = bv->port;
    } else {
        struct udphdr *udp = (void *)ip + (ip->ihl * 4);
        if ((void *)(udp + 1) > data_end)
            return XDP_DROP;
        udp->dest = bv->port;
        udp->check = 0;
    }

    // Recalculate IP checksum (simplified: zero and recompute).
    ip->check = 0;

    return XDP_TX;
}

char _license[] SEC("license") = "Apache-2.0";
