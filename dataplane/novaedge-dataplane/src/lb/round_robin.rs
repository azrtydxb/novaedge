//! Weighted round-robin load balancing.

use std::sync::atomic::{AtomicUsize, Ordering};
use std::time::Duration;

use super::{
    healthy_indices, prefer_same_zone, weighted_expand, Backend, LoadBalancer, RequestContext,
};

/// Weighted round-robin load balancer.
///
/// Backends are expanded by their weight, then selected in rotation via an
/// atomic counter. Unhealthy backends are filtered out before expansion.
pub struct RoundRobin {
    counter: AtomicUsize,
}

impl RoundRobin {
    pub fn new() -> Self {
        Self {
            counter: AtomicUsize::new(0),
        }
    }
}

impl Default for RoundRobin {
    fn default() -> Self {
        Self::new()
    }
}

impl LoadBalancer for RoundRobin {
    fn select(&self, ctx: &RequestContext, backends: &[Backend]) -> Option<usize> {
        let healthy = healthy_indices(backends);
        if healthy.is_empty() {
            return None;
        }
        // Prefer same-zone backends when zone is set.
        let candidates = prefer_same_zone(ctx, backends, &healthy);
        // Expand by weight for proportional selection.
        let weighted = weighted_expand(&candidates, backends);
        if weighted.is_empty() {
            return None;
        }
        let idx = self.counter.fetch_add(1, Ordering::Relaxed) % weighted.len();
        Some(weighted[idx])
    }

    fn report(&self, _backend_idx: usize, _latency: Duration, _success: bool) {}

    fn name(&self) -> &'static str {
        "round-robin"
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::lb::test_helpers::*;

    #[test]
    fn distributes_evenly() {
        let lb = RoundRobin::new();
        let backends = make_backends(3);
        let ctx = make_ctx();
        let mut counts = [0u32; 3];
        for _ in 0..300 {
            let idx = lb.select(&ctx, &backends).unwrap();
            counts[idx] += 1;
        }
        assert_eq!(counts[0], 100);
        assert_eq!(counts[1], 100);
        assert_eq!(counts[2], 100);
    }

    #[test]
    fn skips_unhealthy() {
        let lb = RoundRobin::new();
        let mut backends = make_backends(3);
        backends[1].healthy = false;
        let ctx = make_ctx();
        for _ in 0..100 {
            let idx = lb.select(&ctx, &backends).unwrap();
            assert_ne!(idx, 1);
        }
    }

    #[test]
    fn weighted_distribution() {
        let lb = RoundRobin::new();
        let mut backends = make_backends(2);
        backends[0].weight = 3;
        backends[1].weight = 1;
        let ctx = make_ctx();
        let mut counts = [0u32; 2];
        for _ in 0..400 {
            let idx = lb.select(&ctx, &backends).unwrap();
            counts[idx] += 1;
        }
        assert_eq!(counts[0], 300);
        assert_eq!(counts[1], 100);
    }

    #[test]
    fn returns_none_when_all_unhealthy() {
        let lb = RoundRobin::new();
        let mut backends = make_backends(2);
        backends[0].healthy = false;
        backends[1].healthy = false;
        let ctx = make_ctx();
        assert!(lb.select(&ctx, &backends).is_none());
    }

    #[test]
    fn empty_backends() {
        let lb = RoundRobin::new();
        let ctx = make_ctx();
        assert!(lb.select(&ctx, &[]).is_none());
    }
}
