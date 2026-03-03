//! Source-IP hash load balancing.

use super::{
    healthy_indices, prefer_same_zone, weighted_expand, Backend, LoadBalancer, RequestContext,
};

/// Source-hash load balancer — deterministically maps a client (src_ip, src_port)
/// to a backend using FNV-1a hashing.
pub struct SourceHash;

impl SourceHash {
    /// FNV-1a hash for arbitrary bytes.
    fn fnv1a(data: &[u8]) -> u64 {
        let mut hash: u64 = 0xcbf29ce484222325;
        for &b in data {
            hash ^= b as u64;
            hash = hash.wrapping_mul(0x100000001b3);
        }
        hash
    }
}

impl LoadBalancer for SourceHash {
    fn select(&self, ctx: &RequestContext, backends: &[Backend]) -> Option<usize> {
        let healthy = healthy_indices(backends);
        if healthy.is_empty() {
            return None;
        }
        // Prefer same-zone backends when zone is set.
        let candidates = prefer_same_zone(ctx, backends, &healthy);
        // Expand by weight for proportional hashing.
        let weighted = weighted_expand(&candidates, backends);
        if weighted.is_empty() {
            return None;
        }
        // Hash (src_ip, src_port).
        let key = format!("{}:{}", ctx.src_ip, ctx.src_port);
        let h = Self::fnv1a(key.as_bytes());
        let idx = (h as usize) % weighted.len();
        Some(weighted[idx])
    }

    fn name(&self) -> &'static str {
        "source-hash"
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::lb::test_helpers::*;
    use std::net::{IpAddr, Ipv4Addr};

    #[test]
    fn consistent_selection() {
        let lb = SourceHash;
        let backends = make_backends(5);
        let ctx = make_ctx();
        let first = lb.select(&ctx, &backends).unwrap();
        // Same context must always return the same backend.
        for _ in 0..100 {
            assert_eq!(lb.select(&ctx, &backends).unwrap(), first);
        }
    }

    #[test]
    fn different_clients_may_differ() {
        let lb = SourceHash;
        let backends = make_backends(10);
        let ctx1 = RequestContext {
            src_ip: IpAddr::V4(Ipv4Addr::new(1, 2, 3, 4)),
            src_port: 1000,
            dst_port: 80,
            sticky_cookie: None,
            zone: None,
        };
        let ctx2 = RequestContext {
            src_ip: IpAddr::V4(Ipv4Addr::new(5, 6, 7, 8)),
            src_port: 2000,
            dst_port: 80,
            sticky_cookie: None,
            zone: None,
        };
        let r1 = lb.select(&ctx1, &backends).unwrap();
        let r2 = lb.select(&ctx2, &backends).unwrap();
        // With 10 backends, two different clients are likely mapped differently.
        // This is probabilistic so we just verify both are valid.
        assert!(r1 < 10);
        assert!(r2 < 10);
    }

    #[test]
    fn skips_unhealthy() {
        let lb = SourceHash;
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
        let lb = SourceHash;
        let mut backends = make_backends(2);
        backends[0].healthy = false;
        backends[1].healthy = false;
        let ctx = make_ctx();
        assert!(lb.select(&ctx, &backends).is_none());
    }
}
