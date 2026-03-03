//! Least-connections load balancing.

use std::sync::atomic::{AtomicU32, Ordering};
use std::time::Duration;

use super::{healthy_indices, prefer_same_zone, Backend, LoadBalancer, RequestContext};

/// Maximum number of backends tracked for connection counting.
const MAX_BACKENDS: usize = 1024;

/// Least-connections load balancer.
///
/// Selects the healthy backend with the fewest active connections.
/// Connection counts are maintained via [`report`]: callers should call
/// `report(idx, _, true)` when a connection starts and track completion
/// externally, or use the atomic counters directly.
pub struct LeastConn {
    /// Per-backend active connection counters.
    connections: Vec<AtomicU32>,
}

impl LeastConn {
    pub fn new() -> Self {
        let mut connections = Vec::with_capacity(MAX_BACKENDS);
        for _ in 0..MAX_BACKENDS {
            connections.push(AtomicU32::new(0));
        }
        Self { connections }
    }

    /// Get the active connection count for a backend.
    pub fn active(&self, idx: usize) -> u32 {
        if idx < self.connections.len() {
            self.connections[idx].load(Ordering::Relaxed)
        } else {
            0
        }
    }

    /// Increment connection count (called when a connection is established).
    pub fn increment(&self, idx: usize) {
        if idx < self.connections.len() {
            self.connections[idx].fetch_add(1, Ordering::Relaxed);
        }
    }

    /// Decrement connection count (called when a connection is released).
    pub fn decrement(&self, idx: usize) {
        if idx < self.connections.len() {
            // Use checked subtraction to avoid underflow.
            let _ = self.connections[idx].fetch_update(Ordering::Relaxed, Ordering::Relaxed, |v| {
                if v > 0 {
                    Some(v - 1)
                } else {
                    Some(0)
                }
            });
        }
    }
}

impl Default for LeastConn {
    fn default() -> Self {
        Self::new()
    }
}

impl LoadBalancer for LeastConn {
    fn select(&self, ctx: &RequestContext, backends: &[Backend]) -> Option<usize> {
        let healthy = healthy_indices(backends);
        if healthy.is_empty() {
            return None;
        }

        // Prefer same-zone backends when zone is set.
        let candidates = prefer_same_zone(ctx, backends, &healthy);

        let mut min_idx = candidates[0];
        let mut min_conn = self.connections[candidates[0]].load(Ordering::Relaxed);

        for &idx in &candidates[1..] {
            let conn = self.connections[idx].load(Ordering::Relaxed);
            if conn < min_conn {
                min_conn = conn;
                min_idx = idx;
            }
        }

        Some(min_idx)
    }

    fn report(&self, backend_idx: usize, _latency: Duration, success: bool) {
        if success {
            self.increment(backend_idx);
        } else {
            // Only decrement if there are active connections to avoid underflow.
            if self.active(backend_idx) > 0 {
                self.decrement(backend_idx);
            }
        }
    }

    fn name(&self) -> &'static str {
        "least-conn"
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::lb::test_helpers::*;

    #[test]
    fn selects_least_loaded() {
        let lb = LeastConn::new();
        let backends = make_backends(3);
        let ctx = make_ctx();

        // Backend 0 has 5 conns, backend 1 has 2, backend 2 has 10.
        for _ in 0..5 {
            lb.increment(0);
        }
        for _ in 0..2 {
            lb.increment(1);
        }
        for _ in 0..10 {
            lb.increment(2);
        }

        let selected = lb.select(&ctx, &backends).unwrap();
        assert_eq!(selected, 1);
    }

    #[test]
    fn skips_unhealthy() {
        let lb = LeastConn::new();
        let mut backends = make_backends(3);
        // Backend 1 has fewest but is unhealthy.
        backends[1].healthy = false;
        for _ in 0..5 {
            lb.increment(0);
        }
        for _ in 0..10 {
            lb.increment(2);
        }
        let ctx = make_ctx();
        let selected = lb.select(&ctx, &backends).unwrap();
        assert_eq!(selected, 0);
    }

    #[test]
    fn increment_and_decrement() {
        let lb = LeastConn::new();
        lb.increment(0);
        lb.increment(0);
        lb.increment(0);
        assert_eq!(lb.active(0), 3);
        lb.decrement(0);
        assert_eq!(lb.active(0), 2);
        lb.decrement(0);
        lb.decrement(0);
        assert_eq!(lb.active(0), 0);
        // Decrement below zero should stay at zero.
        lb.decrement(0);
        assert_eq!(lb.active(0), 0);
    }

    #[test]
    fn returns_none_when_all_unhealthy() {
        let lb = LeastConn::new();
        let mut backends = make_backends(2);
        backends[0].healthy = false;
        backends[1].healthy = false;
        let ctx = make_ctx();
        assert!(lb.select(&ctx, &backends).is_none());
    }

    #[test]
    fn prefers_zero_connections() {
        let lb = LeastConn::new();
        let backends = make_backends(3);
        lb.increment(0);
        lb.increment(1);
        // Backend 2 has 0 connections.
        let ctx = make_ctx();
        assert_eq!(lb.select(&ctx, &backends).unwrap(), 2);
    }
}
