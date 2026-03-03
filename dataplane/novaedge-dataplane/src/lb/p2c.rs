//! Power-of-two-choices (P2C) load balancing.

use std::sync::atomic::{AtomicU32, Ordering};
use std::time::Duration;

use rand::Rng;

use super::{healthy_indices, prefer_same_zone, Backend, LoadBalancer, RequestContext};

/// Maximum number of backends tracked.
const MAX_BACKENDS: usize = 1024;

/// Power-of-two-choices load balancer.
///
/// Picks two random healthy backends and selects the one with fewer
/// active connections. This provides O(log log N) expected maximum load,
/// much better than pure random.
pub struct PowerOfTwoChoices {
    connections: Vec<AtomicU32>,
}

impl PowerOfTwoChoices {
    pub fn new() -> Self {
        let mut connections = Vec::with_capacity(MAX_BACKENDS);
        for _ in 0..MAX_BACKENDS {
            connections.push(AtomicU32::new(0));
        }
        Self { connections }
    }

    /// Increment connection count.
    pub fn increment(&self, idx: usize) {
        if idx < self.connections.len() {
            self.connections[idx].fetch_add(1, Ordering::Relaxed);
        }
    }

    /// Decrement connection count.
    pub fn decrement(&self, idx: usize) {
        if idx < self.connections.len() {
            let _ = self.connections[idx].fetch_update(Ordering::Relaxed, Ordering::Relaxed, |v| {
                Some(v.saturating_sub(1))
            });
        }
    }
}

impl Default for PowerOfTwoChoices {
    fn default() -> Self {
        Self::new()
    }
}

impl LoadBalancer for PowerOfTwoChoices {
    fn select(&self, ctx: &RequestContext, backends: &[Backend]) -> Option<usize> {
        let healthy = healthy_indices(backends);
        if healthy.is_empty() {
            return None;
        }

        // Prefer same-zone backends when zone is set.
        let candidates = prefer_same_zone(ctx, backends, &healthy);

        if candidates.len() == 1 {
            return Some(candidates[0]);
        }

        let mut rng = rand::thread_rng();
        let ai = rng.gen_range(0..candidates.len());
        let a = candidates[ai];
        // Ensure we pick two distinct candidates.
        let bi = if candidates.len() == 2 {
            1 - ai
        } else {
            let mut idx = rng.gen_range(0..candidates.len() - 1);
            if idx >= ai {
                idx += 1;
            }
            idx
        };
        let b = candidates[bi];

        let ca = self.connections[a].load(Ordering::Relaxed);
        let cb = self.connections[b].load(Ordering::Relaxed);
        let wa = backends[a].weight.max(1) as u32;
        let wb = backends[b].weight.max(1) as u32;

        // Weight-normalized: compare ca/wa vs cb/wb via cross-multiply.
        if ca * wb <= cb * wa {
            Some(a)
        } else {
            Some(b)
        }
    }

    fn report(&self, backend_idx: usize, _latency: Duration, success: bool) {
        if success {
            self.increment(backend_idx);
        } else {
            self.decrement(backend_idx);
        }
    }

    fn name(&self) -> &'static str {
        "p2c"
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::lb::test_helpers::*;

    #[test]
    fn selects_less_loaded() {
        let lb = PowerOfTwoChoices::new();
        let backends = make_backends(2);
        let ctx = make_ctx();

        // Backend 0 has 10 conns, backend 1 has 0.
        for _ in 0..10 {
            lb.increment(0);
        }

        // With only 2 backends, P2C always picks both, and should prefer backend 1.
        let selected = lb.select(&ctx, &backends).unwrap();
        assert_eq!(selected, 1);
    }

    #[test]
    fn skips_unhealthy() {
        let lb = PowerOfTwoChoices::new();
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
        let lb = PowerOfTwoChoices::new();
        let mut backends = make_backends(2);
        backends[0].healthy = false;
        backends[1].healthy = false;
        let ctx = make_ctx();
        assert!(lb.select(&ctx, &backends).is_none());
    }

    #[test]
    fn single_backend() {
        let lb = PowerOfTwoChoices::new();
        let mut backends = make_backends(3);
        backends[0].healthy = false;
        backends[2].healthy = false;
        let ctx = make_ctx();
        let selected = lb.select(&ctx, &backends).unwrap();
        assert_eq!(selected, 1);
    }

    #[test]
    fn load_distribution_tendency() {
        let lb = PowerOfTwoChoices::new();
        let backends = make_backends(5);
        // Backend 0 heavily loaded, rest at 0.
        for _ in 0..100 {
            lb.increment(0);
        }
        let ctx = make_ctx();
        let mut zero_count = 0;
        for _ in 0..100 {
            let idx = lb.select(&ctx, &backends).unwrap();
            if idx != 0 {
                zero_count += 1;
            }
        }
        // Should overwhelmingly prefer non-zero backends.
        assert!(zero_count > 70, "P2C should avoid heavily loaded backend");
    }
}
