//! Retry budget tracking to prevent retry storms.
//!
//! Tracks per-cluster total requests and retry requests over a sliding
//! window and rejects retries that would exceed the configured budget ratio.

use std::collections::HashMap;
use std::sync::RwLock;
use std::time::{Duration, Instant};

/// Default retry budget: max 20% of requests can be retries.
const DEFAULT_BUDGET_RATIO: f64 = 0.2;

/// Default sliding window for retry budget calculation.
const DEFAULT_WINDOW: Duration = Duration::from_secs(10);

/// Minimum number of retries always allowed per window regardless of ratio.
/// This prevents the budget from starving retries at low request rates.
const MIN_RETRIES_PER_WINDOW: u64 = 3;

/// Per-cluster retry budget state.
struct ClusterBudget {
    /// Timestamps of all requests in the window.
    requests: Vec<Instant>,
    /// Timestamps of retry requests in the window.
    retries: Vec<Instant>,
}

/// Retry budget tracker that prevents retry storms across clusters.
pub struct RetryBudgetTracker {
    budgets: RwLock<HashMap<String, ClusterBudget>>,
    /// Maximum ratio of retries to total requests (0.0 to 1.0).
    budget_ratio: f64,
    /// Sliding window duration.
    window: Duration,
}

impl RetryBudgetTracker {
    /// Create a new retry budget tracker with default settings.
    pub fn new() -> Self {
        Self {
            budgets: RwLock::new(HashMap::new()),
            budget_ratio: DEFAULT_BUDGET_RATIO,
            window: DEFAULT_WINDOW,
        }
    }

    /// Record a request (non-retry) for the given cluster.
    pub fn record_request(&self, cluster: &str) {
        let now = Instant::now();
        let mut budgets = self.budgets.write().unwrap();
        let budget = budgets
            .entry(cluster.to_string())
            .or_insert_with(|| ClusterBudget {
                requests: Vec::new(),
                retries: Vec::new(),
            });
        budget.requests.push(now);
        self.prune(budget, now);
    }

    /// Check whether a retry is allowed for the given cluster.
    ///
    /// Returns `true` if the retry budget has capacity. If the budget is
    /// exhausted (retries/total > budget_ratio), returns `false`.
    pub fn allow_retry(&self, cluster: &str) -> bool {
        let budgets = self.budgets.read().unwrap();
        let Some(budget) = budgets.get(cluster) else {
            return true;
        };

        let now = Instant::now();
        let cutoff = now - self.window;

        let total = budget.requests.iter().filter(|t| **t > cutoff).count() as u64;
        let retries = budget.retries.iter().filter(|t| **t > cutoff).count() as u64;

        // Always allow a minimum number of retries.
        if retries < MIN_RETRIES_PER_WINDOW {
            return true;
        }

        if total == 0 {
            return true;
        }

        (retries as f64 / total as f64) < self.budget_ratio
    }

    /// Record a retry for the given cluster (call after deciding to retry).
    pub fn record_retry(&self, cluster: &str) {
        let now = Instant::now();
        let mut budgets = self.budgets.write().unwrap();
        let budget = budgets
            .entry(cluster.to_string())
            .or_insert_with(|| ClusterBudget {
                requests: Vec::new(),
                retries: Vec::new(),
            });
        budget.retries.push(now);
        self.prune(budget, now);
    }

    /// Remove entries outside the sliding window.
    fn prune(&self, budget: &mut ClusterBudget, now: Instant) {
        let cutoff = now - self.window;
        budget.requests.retain(|t| *t > cutoff);
        budget.retries.retain(|t| *t > cutoff);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn allows_retries_under_budget() {
        let tracker = RetryBudgetTracker::new();
        let cluster = "test-cluster";

        // Record 100 requests.
        for _ in 0..100 {
            tracker.record_request(cluster);
        }

        // Should allow retries (well under 20% budget).
        assert!(tracker.allow_retry(cluster));

        // Record a few retries — still under budget.
        for _ in 0..5 {
            tracker.record_retry(cluster);
        }
        assert!(tracker.allow_retry(cluster));
    }

    #[test]
    fn denies_retries_over_budget() {
        let tracker = RetryBudgetTracker::new();
        let cluster = "test-cluster";

        // Record 10 requests.
        for _ in 0..10 {
            tracker.record_request(cluster);
        }

        // Record many retries to exceed 20% budget.
        for _ in 0..10 {
            tracker.record_retry(cluster);
        }

        // Should deny (10 retries / 10 requests = 100%, exceeds 20%).
        assert!(!tracker.allow_retry(cluster));
    }

    #[test]
    fn minimum_retries_always_allowed() {
        let tracker = RetryBudgetTracker::new();
        let cluster = "test-cluster";

        // Record just 1 request.
        tracker.record_request(cluster);

        // Even though ratio would be high, minimum retries allowed.
        assert!(tracker.allow_retry(cluster));
        tracker.record_retry(cluster);
        assert!(tracker.allow_retry(cluster));
        tracker.record_retry(cluster);
        assert!(tracker.allow_retry(cluster));
    }

    #[test]
    fn unknown_cluster_allows_retry() {
        let tracker = RetryBudgetTracker::new();
        assert!(tracker.allow_retry("unknown"));
    }
}
