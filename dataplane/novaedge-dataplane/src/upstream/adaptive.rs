//! Adaptive concurrency limiter using gradient-based algorithm.
//!
//! Tracks minimum RTT and current RTT to dynamically adjust the concurrency
//! limit. When latency rises (gradient < 1.0), the limit decreases. When
//! latency is stable/improving, the limit increases.
//!
//! Based on the Netflix/Envoy gradient limiter approach.

use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::Mutex;
use std::time::Duration;

/// Default minimum concurrency limit.
const MIN_LIMIT: u32 = 1;

/// Default maximum concurrency limit.
const MAX_LIMIT: u32 = 1000;

/// Smoothing factor for EWMA RTT calculation.
const SMOOTHING: f64 = 0.2;

/// Adaptive concurrency limiter.
pub struct AdaptiveLimiter {
    /// Current concurrency limit.
    limit: AtomicU32,
    /// Current number of active requests.
    active: AtomicU32,
    /// Internal state protected by mutex (updated less frequently).
    state: Mutex<LimiterState>,
    /// Configuration.
    config: AdaptiveConfig,
}

/// Configuration for the adaptive limiter.
#[derive(Debug, Clone)]
pub struct AdaptiveConfig {
    /// Minimum concurrency limit.
    pub min_limit: u32,
    /// Maximum concurrency limit.
    pub max_limit: u32,
    /// Minimum number of samples before adjusting the limit.
    pub min_samples: u32,
}

impl Default for AdaptiveConfig {
    fn default() -> Self {
        Self {
            min_limit: MIN_LIMIT,
            max_limit: MAX_LIMIT,
            min_samples: 10,
        }
    }
}

struct LimiterState {
    /// Minimum observed RTT (the "ideal" latency).
    min_rtt_ns: f64,
    /// Exponentially weighted moving average of recent RTT.
    ewma_rtt_ns: f64,
    /// Number of samples since last limit adjustment.
    sample_count: u32,
}

impl AdaptiveLimiter {
    /// Create a new adaptive limiter with the given configuration.
    pub fn new(config: AdaptiveConfig) -> Self {
        let initial_limit = config.min_limit.max(10);
        Self {
            limit: AtomicU32::new(initial_limit),
            active: AtomicU32::new(0),
            state: Mutex::new(LimiterState {
                min_rtt_ns: f64::MAX,
                ewma_rtt_ns: 0.0,
                sample_count: 0,
            }),
            config,
        }
    }

    /// Atomically try to acquire a concurrency slot.
    ///
    /// Increments the active count and checks against the limit in one step
    /// to avoid a TOCTOU race between checking and incrementing. Returns
    /// `true` if the slot was acquired; `false` (and reverted) if over limit.
    pub fn try_acquire(&self) -> bool {
        let prev = self.active.fetch_add(1, Ordering::Relaxed);
        let limit = self.limit.load(Ordering::Relaxed);
        if prev >= limit {
            // Over limit — revert the increment.
            self.active.fetch_sub(1, Ordering::Relaxed);
            return false;
        }
        true
    }

    /// Check if a new request should be allowed (non-acquiring check).
    ///
    /// Returns `true` if current active requests are below the limit.
    /// Prefer [`try_acquire`] to avoid TOCTOU races.
    pub fn allow_request(&self) -> bool {
        let active = self.active.load(Ordering::Relaxed);
        let limit = self.limit.load(Ordering::Relaxed);
        active < limit
    }

    /// Record that a request is starting.
    pub fn on_request_start(&self) {
        self.active.fetch_add(1, Ordering::Relaxed);
    }

    /// Record that a request has completed with the given RTT.
    pub fn on_request_complete(&self, rtt: Duration) {
        self.active.fetch_sub(1, Ordering::Relaxed);

        let rtt_ns = rtt.as_nanos() as f64;
        let mut state = self.state.lock().unwrap();

        // Update min RTT.
        if rtt_ns < state.min_rtt_ns {
            state.min_rtt_ns = rtt_ns;
        }

        // Update EWMA.
        if state.ewma_rtt_ns == 0.0 {
            state.ewma_rtt_ns = rtt_ns;
        } else {
            state.ewma_rtt_ns = SMOOTHING * rtt_ns + (1.0 - SMOOTHING) * state.ewma_rtt_ns;
        }

        state.sample_count += 1;

        // Adjust limit periodically.
        if state.sample_count >= self.config.min_samples {
            self.adjust_limit(&state);
            state.sample_count = 0;
        }
    }

    /// Adjust the concurrency limit based on the gradient.
    fn adjust_limit(&self, state: &LimiterState) {
        if state.min_rtt_ns <= 0.0 || state.min_rtt_ns == f64::MAX {
            return;
        }

        // Gradient: ratio of min_rtt to current_rtt.
        // gradient = 1.0 when latency is at minimum (no queueing).
        // gradient < 1.0 when latency is elevated (queueing delay).
        let gradient = state.min_rtt_ns / state.ewma_rtt_ns;

        let current_limit = self.limit.load(Ordering::Relaxed) as f64;

        // New limit: current_limit * gradient + queue_use.
        // The queue_use term allows the limit to grow when there's headroom.
        let queue_use = (current_limit - self.active.load(Ordering::Relaxed) as f64).max(0.0);
        let headroom = (queue_use * 0.1).sqrt();

        let new_limit = (current_limit * gradient + headroom)
            .round()
            .clamp(self.config.min_limit as f64, self.config.max_limit as f64)
            as u32;

        self.limit.store(new_limit, Ordering::Relaxed);
    }

    /// Get the current concurrency limit.
    pub fn current_limit(&self) -> u32 {
        self.limit.load(Ordering::Relaxed)
    }

    /// Get the current number of active requests.
    pub fn current_active(&self) -> u32 {
        self.active.load(Ordering::Relaxed)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn allows_requests_under_limit() {
        let limiter = AdaptiveLimiter::new(AdaptiveConfig::default());
        assert!(limiter.allow_request());
    }

    #[test]
    fn try_acquire_atomic() {
        let limiter = AdaptiveLimiter::new(AdaptiveConfig {
            min_limit: 2,
            max_limit: 2,
            min_samples: 10,
        });
        // Force limit to 2 for the test.
        limiter.limit.store(2, Ordering::Relaxed);

        assert!(limiter.try_acquire());
        assert_eq!(limiter.current_active(), 1);
        assert!(limiter.try_acquire());
        assert_eq!(limiter.current_active(), 2);
        // Third acquire must fail and not inflate active.
        assert!(!limiter.try_acquire());
        assert_eq!(limiter.current_active(), 2);
    }

    #[test]
    fn tracks_active_requests() {
        let limiter = AdaptiveLimiter::new(AdaptiveConfig::default());
        limiter.on_request_start();
        assert_eq!(limiter.current_active(), 1);
        limiter.on_request_complete(Duration::from_millis(10));
        assert_eq!(limiter.current_active(), 0);
    }

    #[test]
    fn limit_decreases_under_load() {
        let limiter = AdaptiveLimiter::new(AdaptiveConfig {
            min_limit: 1,
            max_limit: 100,
            min_samples: 5,
        });

        // First: establish low min_rtt with fast responses.
        for _ in 0..5 {
            limiter.on_request_start();
            limiter.on_request_complete(Duration::from_millis(1));
        }
        let limit_after_fast = limiter.current_limit();

        // Now: simulate increased latency (10x slower).
        for _ in 0..10 {
            limiter.on_request_start();
            limiter.on_request_complete(Duration::from_millis(100));
        }
        let limit_after_slow = limiter.current_limit();

        // Limit should have decreased due to gradient < 1.
        assert!(
            limit_after_slow <= limit_after_fast,
            "Expected limit to decrease: fast={limit_after_fast}, slow={limit_after_slow}"
        );
    }

    #[test]
    fn min_rtt_tracks_minimum() {
        let limiter = AdaptiveLimiter::new(AdaptiveConfig {
            min_samples: 2,
            ..Default::default()
        });

        limiter.on_request_start();
        limiter.on_request_complete(Duration::from_millis(10));
        limiter.on_request_start();
        limiter.on_request_complete(Duration::from_millis(5));
        limiter.on_request_start();
        limiter.on_request_complete(Duration::from_millis(8));

        let state = limiter.state.lock().unwrap();
        // min_rtt should be 5ms.
        assert!((state.min_rtt_ns - 5_000_000.0).abs() < 1000.0);
    }
}
