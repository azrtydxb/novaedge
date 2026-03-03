//! Sticky-session (session affinity) load balancing.

use std::collections::HashMap;
use std::net::IpAddr;
use std::sync::RwLock;

use super::{healthy_indices, healthy_weighted, Backend, LoadBalancer, RequestContext};

/// Sticky-session load balancer.
///
/// When a `sticky_cookie` is present in the request context and the
/// previously selected backend is still healthy, returns the same backend.
/// Otherwise falls back to round-robin and caches the new selection.
///
/// The affinity map stores `(IpAddr, u16)` (address + port) instead of a
/// backend index. This ensures that after a config rebuild that reorders
/// the backend slice, the cookie still routes to the correct backend
/// by address match rather than a stale index. (#869)
pub struct StickySession {
    /// Cookie value → backend (addr, port).
    affinity: RwLock<HashMap<String, (IpAddr, u16)>>,
    /// Round-robin counter for fallback selection.
    counter: std::sync::atomic::AtomicUsize,
}

impl StickySession {
    pub fn new() -> Self {
        Self {
            affinity: RwLock::new(HashMap::new()),
            counter: std::sync::atomic::AtomicUsize::new(0),
        }
    }
}

impl Default for StickySession {
    fn default() -> Self {
        Self::new()
    }
}

impl LoadBalancer for StickySession {
    fn select(&self, ctx: &RequestContext, backends: &[Backend]) -> Option<usize> {
        let healthy = healthy_indices(backends);
        if healthy.is_empty() {
            return None;
        }

        // Check sticky affinity by (addr, port) lookup. (#869)
        if let Some(cookie) = &ctx.sticky_cookie {
            let map = self.affinity.read().unwrap();
            if let Some(&(addr, port)) = map.get(cookie) {
                // Find the backend by address match (not index).
                if let Some(&idx) = healthy.iter().find(|&&i| {
                    backends[i].addr == addr && backends[i].port == port
                }) {
                    return Some(idx);
                }
                // Backend gone or unhealthy — fall through to round-robin.
            }
        }

        // Fallback: weighted round-robin.
        let weighted = healthy_weighted(backends);
        if weighted.is_empty() {
            return None;
        }
        let pos = self
            .counter
            .fetch_add(1, std::sync::atomic::Ordering::Relaxed)
            % weighted.len();
        let selected = weighted[pos];

        // Cache the selection by (addr, port) if a cookie is present.
        if let Some(cookie) = &ctx.sticky_cookie {
            let b = &backends[selected];
            let mut map = self.affinity.write().unwrap();
            map.insert(cookie.clone(), (b.addr, b.port));
        }

        Some(selected)
    }

    fn name(&self) -> &'static str {
        "sticky"
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::lb::test_helpers::*;
    use std::net::{IpAddr, Ipv4Addr};

    fn ctx_with_cookie(cookie: &str) -> RequestContext {
        RequestContext {
            src_ip: IpAddr::V4(Ipv4Addr::new(192, 168, 1, 1)),
            src_port: 12345,
            dst_port: 80,
            sticky_cookie: Some(cookie.to_string()),
            zone: None,
        }
    }

    #[test]
    fn affinity_maintained() {
        let lb = StickySession::new();
        let backends = make_backends(5);
        let ctx = ctx_with_cookie("session-abc");

        let first = lb.select(&ctx, &backends).unwrap();
        // Same cookie should always return the same backend.
        for _ in 0..100 {
            assert_eq!(lb.select(&ctx, &backends).unwrap(), first);
        }
    }

    #[test]
    fn different_cookies_independent() {
        let lb = StickySession::new();
        let backends = make_backends(5);
        let ctx1 = ctx_with_cookie("session-1");
        let ctx2 = ctx_with_cookie("session-2");

        let r1 = lb.select(&ctx1, &backends).unwrap();
        let r2 = lb.select(&ctx2, &backends).unwrap();
        // Both are valid — may or may not be the same.
        assert!(r1 < 5);
        assert!(r2 < 5);
    }

    #[test]
    fn falls_back_when_sticky_backend_unhealthy() {
        let lb = StickySession::new();
        let mut backends = make_backends(3);
        let ctx = ctx_with_cookie("session-x");

        let first = lb.select(&ctx, &backends).unwrap();
        // Mark the selected backend as unhealthy.
        backends[first].healthy = false;

        let second = lb.select(&ctx, &backends).unwrap();
        assert_ne!(second, first);
        assert!(backends[second].healthy);
    }

    #[test]
    fn no_cookie_works_like_round_robin() {
        let lb = StickySession::new();
        let backends = make_backends(3);
        let ctx = make_ctx(); // no sticky_cookie
        for _ in 0..100 {
            let idx = lb.select(&ctx, &backends).unwrap();
            assert!(idx < 3);
        }
    }

    #[test]
    fn skips_unhealthy() {
        let lb = StickySession::new();
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
        let lb = StickySession::new();
        let mut backends = make_backends(2);
        backends[0].healthy = false;
        backends[1].healthy = false;
        let ctx = make_ctx();
        assert!(lb.select(&ctx, &backends).is_none());
    }

    #[test]
    fn survives_backend_reorder() {
        // Regression test for #869: after config rebuild reorders backends,
        // sticky session should still route to the correct backend by address.
        let lb = StickySession::new();

        // Original order: [backend-0, backend-1, backend-2]
        let backends_v1 = make_backends(3);
        let ctx = ctx_with_cookie("session-reorder");

        let first = lb.select(&ctx, &backends_v1).unwrap();
        let first_addr = backends_v1[first].addr;
        let first_port = backends_v1[first].port;

        // Simulate config rebuild with reversed order.
        let mut backends_v2 = backends_v1.clone();
        backends_v2.reverse();

        let second = lb.select(&ctx, &backends_v2).unwrap();
        // Must point to the same (addr, port) even though indices changed.
        assert_eq!(backends_v2[second].addr, first_addr);
        assert_eq!(backends_v2[second].port, first_port);
    }
}
