//! Exponentially weighted moving average (EWMA) latency-based load balancing.

use std::sync::RwLock;
use std::time::Duration;

use super::{healthy_indices, prefer_same_zone, Backend, LoadBalancer, RequestContext};

/// Maximum number of backends tracked.
const MAX_BACKENDS: usize = 1024;

/// Default smoothing factor.
const DEFAULT_ALPHA: f64 = 0.5;

/// EWMA load balancer.
///
/// Tracks per-backend exponentially weighted moving average of request
/// latency. Selects the backend with the lowest EWMA latency.
pub struct Ewma {
    /// Per-backend EWMA latency in microseconds.
    latencies: RwLock<Vec<f64>>,
    /// Smoothing factor (0..1). Higher values weight recent observations more.
    alpha: f64,
}

impl Ewma {
    pub fn new() -> Self {
        Self::with_alpha(DEFAULT_ALPHA)
    }

    pub fn with_alpha(alpha: f64) -> Self {
        Self {
            latencies: RwLock::new(vec![0.0; MAX_BACKENDS]),
            alpha: alpha.clamp(0.01, 0.99),
        }
    }
}

impl Default for Ewma {
    fn default() -> Self {
        Self::new()
    }
}

impl LoadBalancer for Ewma {
    fn select(&self, ctx: &RequestContext, backends: &[Backend]) -> Option<usize> {
        let healthy = healthy_indices(backends);
        if healthy.is_empty() {
            return None;
        }

        // Prefer same-zone backends when zone is set.
        let candidates = prefer_same_zone(ctx, backends, &healthy);

        let latencies = self.latencies.read().unwrap();
        // Weight-normalized: effective_latency = latency / weight.
        let mut min_idx = candidates[0];
        let mut min_lat = latencies[candidates[0]];
        let mut min_weight = backends[candidates[0]].weight.max(1) as f64;

        for &idx in &candidates[1..] {
            let lat = latencies[idx];
            let weight = backends[idx].weight.max(1) as f64;
            // Compare lat/weight vs min_lat/min_weight.
            if lat * min_weight < min_lat * weight {
                min_lat = lat;
                min_weight = weight;
                min_idx = idx;
            }
        }

        Some(min_idx)
    }

    fn report(&self, backend_idx: usize, latency: Duration, _success: bool) {
        if backend_idx >= MAX_BACKENDS {
            return;
        }
        let lat_us = latency.as_micros() as f64;
        let mut latencies = self.latencies.write().unwrap();
        let old = latencies[backend_idx];
        if old == 0.0 {
            // First sample — use raw value.
            latencies[backend_idx] = lat_us;
        } else {
            latencies[backend_idx] = self.alpha * lat_us + (1.0 - self.alpha) * old;
        }
    }

    fn name(&self) -> &'static str {
        "ewma"
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::lb::test_helpers::*;

    #[test]
    fn selects_lowest_latency() {
        let lb = Ewma::new();
        let backends = make_backends(3);
        let ctx = make_ctx();

        // Report latencies: backend 0 = 100ms, 1 = 10ms, 2 = 50ms.
        lb.report(0, Duration::from_millis(100), true);
        lb.report(1, Duration::from_millis(10), true);
        lb.report(2, Duration::from_millis(50), true);

        assert_eq!(lb.select(&ctx, &backends).unwrap(), 1);
    }

    #[test]
    fn ewma_smoothing() {
        let lb = Ewma::with_alpha(0.5);
        // First report: EWMA = 100ms.
        lb.report(0, Duration::from_millis(100), true);
        // Second report: 0.5 * 200 + 0.5 * 100 = 150ms.
        lb.report(0, Duration::from_millis(200), true);

        let latencies = lb.latencies.read().unwrap();
        let expected = 150_000.0; // 150ms in microseconds
        assert!(
            (latencies[0] - expected).abs() < 1.0,
            "expected ~{expected}, got {}",
            latencies[0]
        );
    }

    #[test]
    fn skips_unhealthy() {
        let lb = Ewma::new();
        let mut backends = make_backends(3);
        backends[1].healthy = false;
        let ctx = make_ctx();

        // Backend 1 has lowest latency but is unhealthy.
        lb.report(0, Duration::from_millis(100), true);
        lb.report(1, Duration::from_millis(1), true);
        lb.report(2, Duration::from_millis(50), true);

        let selected = lb.select(&ctx, &backends).unwrap();
        assert_ne!(selected, 1);
    }

    #[test]
    fn returns_none_when_all_unhealthy() {
        let lb = Ewma::new();
        let mut backends = make_backends(2);
        backends[0].healthy = false;
        backends[1].healthy = false;
        let ctx = make_ctx();
        assert!(lb.select(&ctx, &backends).is_none());
    }

    #[test]
    fn prefers_unreported_backends() {
        let lb = Ewma::new();
        let backends = make_backends(3);
        let ctx = make_ctx();

        // Only report latency for backend 0.
        lb.report(0, Duration::from_millis(100), true);
        // Backends 1 and 2 have EWMA = 0, so they should be preferred.
        let selected = lb.select(&ctx, &backends).unwrap();
        assert_ne!(selected, 0);
    }
}
