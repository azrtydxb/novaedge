//! Consistent ring-hash load balancing.

use std::collections::BTreeMap;
use std::sync::RwLock;

use super::{healthy_indices, Backend, LoadBalancer, RequestContext};

/// Consistent hash ring load balancer.
///
/// Each backend is placed at multiple positions on a virtual ring
/// (controlled by `replicas`). Requests are routed to the next backend
/// on the ring after the hash of the source IP. This provides
/// consistent mapping even when backends are added or removed.
///
/// The ring is cached with a fingerprint and only rebuilt when the
/// healthy backend set changes. (#870)
pub struct RingHash {
    replicas: usize,
    cache: RwLock<RingState>,
}

struct RingState {
    ring: BTreeMap<u64, usize>,
    fingerprint: u64,
}

impl RingHash {
    pub fn new(replicas: usize) -> Self {
        Self {
            replicas: replicas.max(1),
            cache: RwLock::new(RingState {
                ring: BTreeMap::new(),
                fingerprint: 0,
            }),
        }
    }

    /// Build the hash ring for the given healthy backends.
    ///
    /// Each backend gets `replicas * weight` virtual nodes, so higher-weight
    /// backends occupy proportionally more of the ring.
    fn build_ring(
        healthy: &[usize],
        backends: &[Backend],
        replicas: usize,
    ) -> BTreeMap<u64, usize> {
        let mut ring = BTreeMap::new();
        for &idx in healthy {
            let b = &backends[idx];
            let weight_replicas = replicas * b.weight.max(1) as usize;
            for r in 0..weight_replicas {
                let key = format!("{}:{}:{}", b.addr, b.port, r);
                let hash = fnv1a(key.as_bytes());
                ring.insert(hash, idx);
            }
        }
        ring
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

    /// Look up the ring for the given hash, with wrap-around.
    fn ring_lookup(ring: &BTreeMap<u64, usize>, hash: u64) -> Option<usize> {
        if let Some((&_pos, &idx)) = ring.range(hash..).next() {
            Some(idx)
        } else {
            // Wrap around to the first entry on the ring.
            ring.values().next().copied()
        }
    }
}

impl LoadBalancer for RingHash {
    fn select(&self, ctx: &RequestContext, backends: &[Backend]) -> Option<usize> {
        let healthy = healthy_indices(backends);
        if healthy.is_empty() {
            return None;
        }

        let fp = Self::fingerprint(&healthy, backends);
        // Hash only src_ip for true per-client affinity. (#870)
        let key = format!("{}", ctx.src_ip);
        let hash = fnv1a(key.as_bytes());

        // Fast path: use cached ring if fingerprint matches.
        {
            let state = self.cache.read().unwrap();
            if state.fingerprint == fp && !state.ring.is_empty() {
                return Self::ring_lookup(&state.ring, hash);
            }
        }

        // Slow path: rebuild ring.
        let ring = Self::build_ring(&healthy, backends, self.replicas);
        let result = Self::ring_lookup(&ring, hash);

        let mut state = self.cache.write().unwrap();
        state.ring = ring;
        state.fingerprint = fp;

        result
    }

    fn name(&self) -> &'static str {
        "ring-hash"
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

#[cfg(test)]
mod tests {
    use super::*;
    use crate::lb::test_helpers::*;
    use std::net::{IpAddr, Ipv4Addr};

    #[test]
    fn consistent_selection() {
        let lb = RingHash::new(150);
        let backends = make_backends(5);
        let ctx = make_ctx();
        let first = lb.select(&ctx, &backends).unwrap();
        for _ in 0..100 {
            assert_eq!(lb.select(&ctx, &backends).unwrap(), first);
        }
    }

    #[test]
    fn ring_entries_count() {
        let healthy: Vec<usize> = (0..3).collect();
        let backends = make_backends(3);
        let ring = RingHash::build_ring(&healthy, &backends, 150);
        // 3 backends * 150 replicas * weight 1 = 450 entries (some may collide).
        assert!(ring.len() <= 450);
        assert!(ring.len() > 400); // Very unlikely to have >50 collisions.
    }

    #[test]
    fn weighted_ring_entries() {
        let mut backends = make_backends(2);
        backends[0].weight = 3;
        backends[1].weight = 1;
        let healthy: Vec<usize> = vec![0, 1];
        let ring = RingHash::build_ring(&healthy, &backends, 100);
        // Backend 0: 100*3=300 replicas, Backend 1: 100*1=100 replicas = 400 total.
        assert!(ring.len() <= 400);
        assert!(ring.len() > 350);
        // Backend 0 should own ~75% of positions.
        let b0_count = ring.values().filter(|&&v| v == 0).count();
        let ratio = b0_count as f64 / ring.len() as f64;
        assert!(
            ratio > 0.6,
            "weighted backend should own more ring space: {ratio:.2}"
        );
    }

    #[test]
    fn skips_unhealthy() {
        let lb = RingHash::new(150);
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
        let lb = RingHash::new(150);
        let mut backends = make_backends(2);
        backends[0].healthy = false;
        backends[1].healthy = false;
        let ctx = make_ctx();
        assert!(lb.select(&ctx, &backends).is_none());
    }

    #[test]
    fn different_clients_distribute() {
        let lb = RingHash::new(150);
        let backends = make_backends(3);
        let mut counts = [0u32; 3];
        // Use 900 distinct IPs across a wider range for better distribution.
        for i in 0..900u32 {
            let ctx = RequestContext {
                src_ip: IpAddr::V4(Ipv4Addr::new(
                    10,
                    ((i / 256) % 256) as u8,
                    (i % 256) as u8,
                    ((i * 7 + 13) % 256) as u8,
                )),
                src_port: 1000,
                dst_port: 80,
                sticky_cookie: None,
                zone: None,
            };
            let idx = lb.select(&ctx, &backends).unwrap();
            counts[idx] += 1;
        }
        // Each backend should get a reasonable share (>10% of 900 = 90).
        for &c in &counts {
            assert!(c > 90, "backend got too few requests: {c}");
        }
    }

    #[test]
    fn minimal_disruption_on_backend_change() {
        // Need two separate instances since each has its own cache.
        let lb3 = RingHash::new(150);
        let lb4 = RingHash::new(150);
        let backends_3 = make_backends(3);
        let backends_4 = make_backends(4);

        let mut same_count = 0;
        let total = 1000;

        for i in 0..total {
            let ctx = RequestContext {
                src_ip: IpAddr::V4(Ipv4Addr::new(172, 16, (i >> 8) as u8, (i & 0xFF) as u8)),
                src_port: (2000 + i) as u16,
                dst_port: 80,
                sticky_cookie: None,
                zone: None,
            };
            let r3 = lb3.select(&ctx, &backends_3).unwrap();
            let r4 = lb4.select(&ctx, &backends_4).unwrap();
            if r3 == r4 {
                same_count += 1;
            }
        }
        // With consistent hashing, most keys should stay the same.
        let stability = same_count as f64 / total as f64;
        assert!(
            stability > 0.5,
            "ring hash stability too low: {stability:.2}"
        );
    }

    #[test]
    fn same_ip_different_port_same_backend() {
        let lb = RingHash::new(150);
        let backends = make_backends(5);
        let ctx1 = RequestContext {
            src_ip: IpAddr::V4(Ipv4Addr::new(10, 0, 0, 1)),
            src_port: 50000,
            dst_port: 80,
            sticky_cookie: None,
            zone: None,
        };
        let ctx2 = RequestContext {
            src_ip: IpAddr::V4(Ipv4Addr::new(10, 0, 0, 1)),
            src_port: 50001,
            dst_port: 80,
            sticky_cookie: None,
            zone: None,
        };
        assert_eq!(
            lb.select(&ctx1, &backends).unwrap(),
            lb.select(&ctx2, &backends).unwrap(),
            "same IP with different ports should select same backend"
        );
    }

    #[test]
    fn cache_is_reused() {
        let lb = RingHash::new(150);
        let backends = make_backends(3);
        let ctx = make_ctx();

        // First call builds the ring.
        let first = lb.select(&ctx, &backends).unwrap();

        // Second call should use the cache (same result, no rebuild).
        let second = lb.select(&ctx, &backends).unwrap();
        assert_eq!(first, second);

        // Verify fingerprint was set.
        let state = lb.cache.read().unwrap();
        assert!(!state.ring.is_empty());
        assert_ne!(state.fingerprint, 0);
    }
}
