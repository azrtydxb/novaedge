//! Consistent ring-hash load balancing.

use std::collections::BTreeMap;

use super::{healthy_indices, Backend, LoadBalancer, RequestContext};

/// Consistent hash ring load balancer.
///
/// Each backend is placed at multiple positions on a virtual ring
/// (controlled by `replicas`). Requests are routed to the next backend
/// on the ring after the hash of the source address. This provides
/// consistent mapping even when backends are added or removed.
pub struct RingHash {
    replicas: usize,
}

impl RingHash {
    pub fn new(replicas: usize) -> Self {
        Self {
            replicas: replicas.max(1),
        }
    }

    /// Build the hash ring for the given healthy backends.
    fn build_ring(
        healthy: &[usize],
        backends: &[Backend],
        replicas: usize,
    ) -> BTreeMap<u64, usize> {
        let mut ring = BTreeMap::new();
        for &idx in healthy {
            let b = &backends[idx];
            for r in 0..replicas {
                let key = format!("{}:{}:{}", b.addr, b.port, r);
                let hash = fnv1a(key.as_bytes());
                ring.insert(hash, idx);
            }
        }
        ring
    }
}

impl LoadBalancer for RingHash {
    fn select(&self, ctx: &RequestContext, backends: &[Backend]) -> Option<usize> {
        let healthy = healthy_indices(backends);
        if healthy.is_empty() {
            return None;
        }

        let ring = Self::build_ring(&healthy, backends, self.replicas);

        let key = format!("{}:{}", ctx.src_ip, ctx.src_port);
        let hash = fnv1a(key.as_bytes());

        // Find the first entry >= hash (wrap around if needed).
        if let Some((&_pos, &idx)) = ring.range(hash..).next() {
            Some(idx)
        } else {
            // Wrap around to the first entry on the ring.
            ring.values().next().copied()
        }
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
        // 3 backends * 150 replicas = 450 entries (some may collide).
        assert!(ring.len() <= 450);
        assert!(ring.len() > 400); // Very unlikely to have >50 collisions.
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
        for i in 0..300u32 {
            let ctx = RequestContext {
                src_ip: IpAddr::V4(Ipv4Addr::new(10, (i >> 16) as u8, (i >> 8) as u8, i as u8)),
                src_port: (1000 + i) as u16,
                dst_port: 80,
                sticky_cookie: None,
                zone: None,
            };
            let idx = lb.select(&ctx, &backends).unwrap();
            counts[idx] += 1;
        }
        // Each backend should get a reasonable share.
        for &c in &counts {
            assert!(c > 50, "backend got too few requests: {c}");
        }
    }

    #[test]
    fn minimal_disruption_on_backend_change() {
        let lb = RingHash::new(150);
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
            let r3 = lb.select(&ctx, &backends_3).unwrap();
            let r4 = lb.select(&ctx, &backends_4).unwrap();
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
}
