//! Slow start weight adjustment for newly added or recovered endpoints.
//!
//! Gradually ramps traffic to backends that recently joined the cluster
//! to avoid overwhelming them before caches are warm and connections pooled.

use std::collections::HashMap;
use std::sync::RwLock;
use std::time::{Duration, Instant};

/// Tracks when each endpoint first appeared or recovered, allowing
/// weight adjustment during a configurable warm-up window.
pub struct SlowStartTracker {
    /// Map from endpoint key (e.g. "ip:port") to the time it was first seen.
    first_seen: RwLock<HashMap<String, Instant>>,
}

impl SlowStartTracker {
    /// Create a new slow start tracker.
    pub fn new() -> Self {
        Self {
            first_seen: RwLock::new(HashMap::new()),
        }
    }

    /// Register an endpoint if not already tracked. Returns true if newly added.
    pub fn register(&self, key: &str) -> bool {
        let mut map = self.first_seen.write().unwrap();
        if map.contains_key(key) {
            false
        } else {
            map.insert(key.to_string(), Instant::now());
            true
        }
    }

    /// Remove endpoints that are no longer in the active set.
    pub fn retain_keys(&self, active_keys: &[String]) {
        let mut map = self.first_seen.write().unwrap();
        map.retain(|k, _| active_keys.contains(k));
    }

    /// Compute the weight multiplier for an endpoint given the slow start
    /// window duration and aggression factor.
    ///
    /// Returns a value in `[0.0, 1.0]` where 1.0 means fully ramped up.
    /// Formula: `min(1.0, (elapsed / window) ^ aggression)`
    ///
    /// - `aggression == 1.0`: linear ramp
    /// - `aggression < 1.0`: faster initial ramp (concave curve)
    /// - `aggression > 1.0`: slower initial ramp (convex curve)
    pub fn weight_multiplier(&self, key: &str, window: Duration, aggression: f64) -> f64 {
        if window.is_zero() {
            return 1.0;
        }
        let map = self.first_seen.read().unwrap();
        match map.get(key) {
            Some(first_seen) => {
                let elapsed = first_seen.elapsed();
                if elapsed >= window {
                    1.0
                } else {
                    let ratio = elapsed.as_secs_f64() / window.as_secs_f64();
                    ratio.powf(aggression).min(1.0).max(0.01) // Never go below 1%
                }
            }
            None => 1.0, // Unknown endpoint gets full weight
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn new_endpoint_gets_low_weight() {
        let tracker = SlowStartTracker::new();
        tracker.register("10.0.0.1:8080");
        let m = tracker.weight_multiplier(
            "10.0.0.1:8080",
            Duration::from_secs(60),
            1.0,
        );
        // Just registered, elapsed ~0, should be very low
        assert!(m < 0.1, "multiplier should be low for new endpoint: {m}");
    }

    #[test]
    fn unknown_endpoint_gets_full_weight() {
        let tracker = SlowStartTracker::new();
        let m = tracker.weight_multiplier(
            "10.0.0.1:8080",
            Duration::from_secs(60),
            1.0,
        );
        assert_eq!(m, 1.0);
    }

    #[test]
    fn zero_window_gives_full_weight() {
        let tracker = SlowStartTracker::new();
        tracker.register("10.0.0.1:8080");
        let m = tracker.weight_multiplier(
            "10.0.0.1:8080",
            Duration::ZERO,
            1.0,
        );
        assert_eq!(m, 1.0);
    }

    #[test]
    fn retain_removes_stale() {
        let tracker = SlowStartTracker::new();
        tracker.register("10.0.0.1:8080");
        tracker.register("10.0.0.2:8080");
        tracker.retain_keys(&["10.0.0.1:8080".to_string()]);
        // 10.0.0.2 removed, should get full weight (unknown)
        let m = tracker.weight_multiplier(
            "10.0.0.2:8080",
            Duration::from_secs(60),
            1.0,
        );
        assert_eq!(m, 1.0);
    }

    #[test]
    fn aggression_affects_curve() {
        let tracker = SlowStartTracker::new();
        tracker.register("a");
        // Sleep briefly so we have some elapsed time
        std::thread::sleep(Duration::from_millis(10));
        let linear = tracker.weight_multiplier("a", Duration::from_secs(1), 1.0);
        let convex = tracker.weight_multiplier("a", Duration::from_secs(1), 2.0);
        // With aggression > 1, the weight should be lower than linear
        assert!(convex < linear, "convex({convex}) should be < linear({linear})");
    }
}
