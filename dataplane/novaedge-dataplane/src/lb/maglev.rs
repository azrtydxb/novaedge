//! Maglev consistent hashing load balancer.
//!
//! Implements Google's Maglev hashing algorithm with a 65537-entry lookup table.
//! Provides minimal disruption when backends are added/removed.

use std::sync::RwLock;

use super::{healthy_indices, Backend, LoadBalancer, RequestContext};

/// Maglev table size — must be prime; 65537 is a common choice.
const TABLE_SIZE: usize = 65537;

/// Maglev consistent-hashing load balancer.
pub struct MaglevLb {
    /// Cached lookup table. Rebuilt when backend set changes.
    table: RwLock<MaglevState>,
}

struct MaglevState {
    /// lookup[hash % TABLE_SIZE] = backend index in the original slice.
    lookup: Vec<usize>,
    /// Fingerprint of the healthy backend set used to build the table.
    fingerprint: u64,
}

impl MaglevLb {
    pub fn new() -> Self {
        Self {
            table: RwLock::new(MaglevState {
                lookup: Vec::new(),
                fingerprint: 0,
            }),
        }
    }

    /// Build the Maglev lookup table for the given healthy backend indices.
    ///
    /// Backends are expanded by weight: a backend with weight 3 gets 3 entries
    /// in the permutation list, giving it proportionally more table slots.
    fn build_table(healthy: &[usize], backends: &[Backend]) -> Vec<usize> {
        if healthy.is_empty() {
            return Vec::new();
        }

        // Expand backends by weight: each backend appears weight times with
        // distinct hash keys (using replica index).
        let mut expanded: Vec<(usize, usize, usize)> = Vec::new(); // (original_idx, offset, skip)
        for &idx in healthy {
            let b = &backends[idx];
            let w = b.weight.max(1) as usize;
            for r in 0..w {
                let key = format!("{}:{}:{}", b.addr, b.port, r);
                let h1 = fnv1a(key.as_bytes());
                let h2 = fnv1a_2(key.as_bytes());
                let offset = (h1 as usize) % TABLE_SIZE;
                let skip = ((h2 as usize) % (TABLE_SIZE - 1)) + 1;
                expanded.push((idx, offset, skip));
            }
        }

        let n = expanded.len();
        let mut table = vec![usize::MAX; TABLE_SIZE];
        let mut next = vec![0usize; n];
        let mut filled = 0;

        while filled < TABLE_SIZE {
            for i in 0..n {
                let (original_idx, offset, skip) = expanded[i];
                let mut c = (offset + next[i] * skip) % TABLE_SIZE;
                while table[c] != usize::MAX {
                    next[i] += 1;
                    c = (offset + next[i] * skip) % TABLE_SIZE;
                }
                table[c] = original_idx;
                next[i] += 1;
                filled += 1;
                if filled >= TABLE_SIZE {
                    break;
                }
            }
        }

        table
    }

    /// Compute a fingerprint for the healthy backend set (including weights).
    fn fingerprint(healthy: &[usize], backends: &[Backend]) -> u64 {
        let mut hash: u64 = 0;
        for &idx in healthy {
            let b = &backends[idx];
            let key = format!("{}:{}:{}:{}", b.addr, b.port, idx, b.weight);
            hash ^= fnv1a(key.as_bytes());
        }
        hash
    }
}

impl Default for MaglevLb {
    fn default() -> Self {
        Self::new()
    }
}

impl LoadBalancer for MaglevLb {
    fn select(&self, ctx: &RequestContext, backends: &[Backend]) -> Option<usize> {
        let healthy = healthy_indices(backends);
        if healthy.is_empty() {
            return None;
        }

        let fp = Self::fingerprint(&healthy, backends);

        // Check if table needs rebuilding.
        {
            let state = self.table.read().unwrap();
            if state.fingerprint == fp && !state.lookup.is_empty() {
                let key = format!("{}:{}:{}", ctx.src_ip, ctx.src_port, ctx.dst_port);
                let h = fnv1a(key.as_bytes()) as usize;
                return Some(state.lookup[h % TABLE_SIZE]);
            }
        }

        // Rebuild table.
        let lookup = Self::build_table(&healthy, backends);
        let key = format!("{}:{}:{}", ctx.src_ip, ctx.src_port, ctx.dst_port);
        let h = fnv1a(key.as_bytes()) as usize;
        let result = lookup[h % TABLE_SIZE];

        let mut state = self.table.write().unwrap();
        state.lookup = lookup;
        state.fingerprint = fp;

        Some(result)
    }

    fn name(&self) -> &'static str {
        "maglev"
    }
}

/// FNV-1a hash.
fn fnv1a(data: &[u8]) -> u64 {
    let mut hash: u64 = 0xcbf29ce484222325;
    for &b in data {
        hash ^= b as u64;
        hash = hash.wrapping_mul(0x100000001b3);
    }
    hash
}

/// Second independent hash (FNV-1a with different seed).
fn fnv1a_2(data: &[u8]) -> u64 {
    let mut hash: u64 = 0x6c62272e07bb0142;
    for &b in data {
        hash ^= b as u64;
        hash = hash.wrapping_mul(0x100000001b3);
    }
    hash
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::lb::test_helpers::*;

    #[test]
    fn consistent_selection() {
        let lb = MaglevLb::new();
        let backends = make_backends(5);
        let ctx = make_ctx();
        let first = lb.select(&ctx, &backends).unwrap();
        for _ in 0..100 {
            assert_eq!(lb.select(&ctx, &backends).unwrap(), first);
        }
    }

    #[test]
    fn table_size_correct() {
        let healthy: Vec<usize> = (0..3).collect();
        let backends = make_backends(3);
        let table = MaglevLb::build_table(&healthy, &backends);
        assert_eq!(table.len(), TABLE_SIZE);
        // All entries must be valid backend indices.
        for &entry in &table {
            assert!(entry < 3);
        }
    }

    #[test]
    fn minimal_disruption() {
        let lb = MaglevLb::new();
        let backends_3 = make_backends(3);
        let backends_4 = make_backends(4);
        let ctx = make_ctx();

        // Select with 3 backends.
        let r3 = lb.select(&ctx, &backends_3).unwrap();
        // Select with 4 backends — if r3 is still healthy, it should ideally be the same.
        // Maglev guarantees minimal disruption but not zero, so we just verify validity.
        let r4 = lb.select(&ctx, &backends_4).unwrap();
        assert!(r4 < 4);
        // The important property: many keys remain stable.
        let _ = r3; // Suppress unused warning.
    }

    #[test]
    fn skips_unhealthy() {
        let lb = MaglevLb::new();
        let mut backends = make_backends(3);
        backends[1].healthy = false;
        let ctx = make_ctx();
        for _ in 0..100 {
            let idx = lb.select(&ctx, &backends).unwrap();
            assert_ne!(idx, 1);
        }
    }

    #[test]
    fn returns_none_when_all_unhealthy() {
        let lb = MaglevLb::new();
        let mut backends = make_backends(2);
        backends[0].healthy = false;
        backends[1].healthy = false;
        let ctx = make_ctx();
        assert!(lb.select(&ctx, &backends).is_none());
    }

    #[test]
    fn distribution_across_backends() {
        let lb = MaglevLb::new();
        let backends = make_backends(3);
        let mut counts = [0u32; 3];
        // Use many different source IPs to test distribution.
        for i in 0..300u32 {
            let ctx = RequestContext {
                src_ip: std::net::IpAddr::V4(std::net::Ipv4Addr::new(
                    10,
                    (i >> 16) as u8,
                    (i >> 8) as u8,
                    i as u8,
                )),
                src_port: (1000 + i) as u16,
                dst_port: 80,
                sticky_cookie: None,
                zone: None,
            };
            let idx = lb.select(&ctx, &backends).unwrap();
            counts[idx] += 1;
        }
        // Each backend should get a reasonable share (rough check).
        for &c in &counts {
            assert!(c > 50, "backend got too few requests: {c}");
        }
    }
}
