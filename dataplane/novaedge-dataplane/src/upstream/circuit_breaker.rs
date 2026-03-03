//! Circuit breaker for upstream connections.
//!
//! Implements the standard closed → open → half-open state machine
//! to protect against cascading failures.

use std::sync::atomic::{AtomicU32, AtomicU64, AtomicU8, Ordering};
use std::time::Duration;

/// Circuit breaker state.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CircuitState {
    /// Normal operation — requests are allowed.
    Closed = 0,
    /// Too many failures — requests are rejected.
    Open = 1,
    /// Trial period — limited requests allowed to probe recovery.
    HalfOpen = 2,
}

impl From<u8> for CircuitState {
    fn from(v: u8) -> Self {
        match v {
            0 => CircuitState::Closed,
            1 => CircuitState::Open,
            2 => CircuitState::HalfOpen,
            _ => CircuitState::Closed,
        }
    }
}

/// Circuit breaker configuration.
#[derive(Debug, Clone)]
pub struct CircuitBreakerConfig {
    /// Number of consecutive failures to trip the circuit.
    pub failure_threshold: u32,
    /// Number of consecutive successes in half-open to close the circuit.
    pub success_threshold: u32,
    /// How long to stay open before transitioning to half-open.
    pub open_duration: Duration,
    /// Maximum number of requests allowed through in half-open state.
    /// Once exhausted, further requests are rejected until the circuit closes.
    pub half_open_max_requests: u32,
}

impl Default for CircuitBreakerConfig {
    fn default() -> Self {
        Self {
            failure_threshold: 5,
            success_threshold: 3,
            open_duration: Duration::from_secs(30),
            half_open_max_requests: 1,
        }
    }
}

/// Circuit breaker using atomic operations for lock-free state management.
pub struct CircuitBreaker {
    state: AtomicU8,
    failures: AtomicU32,
    successes: AtomicU32,
    /// Timestamp (milliseconds since UNIX epoch) of the last failure that
    /// caused the circuit to open.
    last_failure_ms: AtomicU64,
    /// Remaining probe permits in half-open state.
    half_open_permits: AtomicU32,
    config: CircuitBreakerConfig,
}

impl CircuitBreaker {
    /// Create a new circuit breaker in the closed state.
    pub fn new(config: CircuitBreakerConfig) -> Self {
        let half_open = config.half_open_max_requests.max(1);
        Self {
            state: AtomicU8::new(CircuitState::Closed as u8),
            failures: AtomicU32::new(0),
            successes: AtomicU32::new(0),
            last_failure_ms: AtomicU64::new(0),
            half_open_permits: AtomicU32::new(half_open),
            config,
        }
    }

    /// Check whether a request should be allowed through.
    pub fn allow_request(&self) -> bool {
        let state = self.state();
        match state {
            CircuitState::Closed => true,
            CircuitState::Open => {
                // Check if enough time has passed to try half-open.
                let now_ms = current_time_ms();
                let last = self.last_failure_ms.load(Ordering::Relaxed);
                if now_ms.saturating_sub(last) >= self.config.open_duration.as_millis() as u64 {
                    // Transition to half-open with limited probe permits.
                    // The transition itself counts as the first probe, so set
                    // remaining permits to (max - 1).
                    self.state
                        .store(CircuitState::HalfOpen as u8, Ordering::Release);
                    self.successes.store(0, Ordering::Relaxed);
                    self.half_open_permits.store(
                        self.config.half_open_max_requests.max(1).saturating_sub(1),
                        Ordering::Relaxed,
                    );
                    true
                } else {
                    false
                }
            }
            CircuitState::HalfOpen => {
                // Limit probes: decrement permits atomically.
                self.half_open_permits
                    .fetch_update(Ordering::Relaxed, Ordering::Relaxed, |p| {
                        if p > 0 {
                            Some(p - 1)
                        } else {
                            None
                        }
                    })
                    .is_ok()
            }
        }
    }

    /// Record a successful request.
    pub fn record_success(&self) {
        let state = self.state();
        match state {
            CircuitState::Closed => {
                // Reset failure counter on success.
                self.failures.store(0, Ordering::Relaxed);
            }
            CircuitState::HalfOpen => {
                let s = self.successes.fetch_add(1, Ordering::Relaxed) + 1;
                if s >= self.config.success_threshold {
                    // Enough successes — close the circuit.
                    self.state
                        .store(CircuitState::Closed as u8, Ordering::Release);
                    self.failures.store(0, Ordering::Relaxed);
                    self.successes.store(0, Ordering::Relaxed);
                }
            }
            CircuitState::Open => {
                // Shouldn't happen (requests blocked), but reset anyway.
            }
        }
    }

    /// Record a failed request.
    pub fn record_failure(&self) {
        let state = self.state();
        match state {
            CircuitState::Closed => {
                let f = self.failures.fetch_add(1, Ordering::Relaxed) + 1;
                if f >= self.config.failure_threshold {
                    // Trip the circuit.
                    self.state
                        .store(CircuitState::Open as u8, Ordering::Release);
                    self.last_failure_ms
                        .store(current_time_ms(), Ordering::Relaxed);
                }
            }
            CircuitState::HalfOpen => {
                // Any failure in half-open goes back to open.
                self.state
                    .store(CircuitState::Open as u8, Ordering::Release);
                self.last_failure_ms
                    .store(current_time_ms(), Ordering::Relaxed);
                self.successes.store(0, Ordering::Relaxed);
            }
            CircuitState::Open => {
                // Update the timestamp to extend the open period.
                self.last_failure_ms
                    .store(current_time_ms(), Ordering::Relaxed);
            }
        }
    }

    /// Get the current circuit state.
    pub fn state(&self) -> CircuitState {
        CircuitState::from(self.state.load(Ordering::Acquire))
    }

    /// Reset the circuit breaker to closed state.
    pub fn reset(&self) {
        self.state
            .store(CircuitState::Closed as u8, Ordering::Release);
        self.failures.store(0, Ordering::Relaxed);
        self.successes.store(0, Ordering::Relaxed);
        self.last_failure_ms.store(0, Ordering::Relaxed);
        self.half_open_permits
            .store(self.config.half_open_max_requests.max(1), Ordering::Relaxed);
    }
}

/// Get the current time in milliseconds since UNIX epoch.
fn current_time_ms() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis() as u64
}

#[cfg(test)]
mod tests {
    use super::*;

    fn default_cb() -> CircuitBreaker {
        CircuitBreaker::new(CircuitBreakerConfig {
            failure_threshold: 3,
            success_threshold: 2,
            open_duration: Duration::from_millis(100),
            half_open_max_requests: 1,
        })
    }

    #[test]
    fn starts_closed() {
        let cb = default_cb();
        assert_eq!(cb.state(), CircuitState::Closed);
        assert!(cb.allow_request());
    }

    #[test]
    fn closed_to_open_on_failures() {
        let cb = default_cb();
        cb.record_failure();
        assert_eq!(cb.state(), CircuitState::Closed);
        cb.record_failure();
        assert_eq!(cb.state(), CircuitState::Closed);
        cb.record_failure();
        assert_eq!(cb.state(), CircuitState::Open);
        assert!(!cb.allow_request());
    }

    #[test]
    fn open_to_half_open_after_timeout() {
        let cb = default_cb();
        // Trip the circuit.
        for _ in 0..3 {
            cb.record_failure();
        }
        assert_eq!(cb.state(), CircuitState::Open);
        assert!(!cb.allow_request());

        // Wait for the open duration.
        std::thread::sleep(Duration::from_millis(150));

        // Should transition to half-open.
        assert!(cb.allow_request());
        assert_eq!(cb.state(), CircuitState::HalfOpen);
    }

    #[test]
    fn half_open_to_closed_on_successes() {
        let cb = default_cb();
        // Trip the circuit.
        for _ in 0..3 {
            cb.record_failure();
        }
        std::thread::sleep(Duration::from_millis(150));
        cb.allow_request(); // Transition to half-open.

        cb.record_success();
        assert_eq!(cb.state(), CircuitState::HalfOpen);
        cb.record_success();
        assert_eq!(cb.state(), CircuitState::Closed);
    }

    #[test]
    fn half_open_to_open_on_failure() {
        let cb = default_cb();
        for _ in 0..3 {
            cb.record_failure();
        }
        std::thread::sleep(Duration::from_millis(150));
        cb.allow_request(); // Transition to half-open.

        cb.record_failure();
        assert_eq!(cb.state(), CircuitState::Open);
    }

    #[test]
    fn reset() {
        let cb = default_cb();
        for _ in 0..3 {
            cb.record_failure();
        }
        assert_eq!(cb.state(), CircuitState::Open);

        cb.reset();
        assert_eq!(cb.state(), CircuitState::Closed);
        assert!(cb.allow_request());
    }

    #[test]
    fn half_open_probe_limiting() {
        let cb = CircuitBreaker::new(CircuitBreakerConfig {
            failure_threshold: 2,
            success_threshold: 2,
            open_duration: Duration::from_millis(50),
            half_open_max_requests: 2,
        });
        // Trip the circuit.
        cb.record_failure();
        cb.record_failure();
        assert_eq!(cb.state(), CircuitState::Open);

        // Wait for open duration.
        std::thread::sleep(Duration::from_millis(80));

        // First probe should be allowed (transitions to half-open).
        assert!(cb.allow_request());
        assert_eq!(cb.state(), CircuitState::HalfOpen);

        // Second probe should be allowed (2 permits configured).
        assert!(cb.allow_request());

        // Third probe should be rejected (permits exhausted).
        assert!(!cb.allow_request());
    }

    #[test]
    fn success_resets_failure_count() {
        let cb = default_cb();
        cb.record_failure();
        cb.record_failure();
        // One success should reset the counter.
        cb.record_success();
        // Two more failures should not trip (counter was reset).
        cb.record_failure();
        cb.record_failure();
        assert_eq!(cb.state(), CircuitState::Closed);
        // Third failure now trips it.
        cb.record_failure();
        assert_eq!(cb.state(), CircuitState::Open);
    }
}
