//! Random load balancing.

use rand::Rng;

use super::{
    healthy_indices, prefer_same_zone, weighted_expand, Backend, LoadBalancer, RequestContext,
};

/// Random load balancer — selects a healthy backend at random.
///
/// Weight-aware: backends with higher weight appear more often in the
/// candidate pool, proportionally increasing their selection probability.
pub struct Random;

impl LoadBalancer for Random {
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
        let idx = rand::thread_rng().gen_range(0..weighted.len());
        Some(weighted[idx])
    }

    fn name(&self) -> &'static str {
        "random"
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::lb::test_helpers::*;

    #[test]
    fn selects_from_healthy() {
        let lb = Random;
        let backends = make_backends(3);
        let ctx = make_ctx();
        for _ in 0..100 {
            let idx = lb.select(&ctx, &backends).unwrap();
            assert!(idx < 3);
        }
    }

    #[test]
    fn skips_unhealthy() {
        let lb = Random;
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
        let lb = Random;
        let mut backends = make_backends(2);
        backends[0].healthy = false;
        backends[1].healthy = false;
        let ctx = make_ctx();
        assert!(lb.select(&ctx, &backends).is_none());
    }

    #[test]
    fn empty_backends() {
        let lb = Random;
        let ctx = make_ctx();
        assert!(lb.select(&ctx, &[]).is_none());
    }
}
