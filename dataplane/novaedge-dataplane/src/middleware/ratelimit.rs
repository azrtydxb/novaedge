//! Rate limiting middleware using token bucket and sliding window algorithms.

use std::collections::HashMap;
use std::sync::RwLock;
use std::time::{Duration, Instant};

/// Rate limit key extraction strategy.
#[derive(Debug, Clone)]
pub enum RateLimitKeyType {
    /// Key by source IP address.
    SourceIP,
    /// Key by a specific header value.
    Header(String),
    /// Key by request path.
    Path,
}

/// Rate limiter configuration for token bucket.
#[derive(Debug, Clone)]
pub struct RateLimitConfig {
    /// Sustained rate in requests per second.
    pub requests_per_second: f64,
    /// Maximum burst size (bucket capacity).
    pub burst: u32,
    /// How to extract the rate limit key from a request.
    pub key_type: RateLimitKeyType,
}

/// Token bucket rate limiter for L7 requests.
pub struct TokenBucket {
    config: RateLimitConfig,
    buckets: RwLock<HashMap<String, BucketState>>,
}

struct BucketState {
    tokens: f64,
    last_refill: Instant,
}

impl TokenBucket {
    /// Create a new token bucket rate limiter.
    pub fn new(config: RateLimitConfig) -> Self {
        Self {
            config,
            buckets: RwLock::new(HashMap::new()),
        }
    }

    /// Extract the rate limit key from a request.
    pub fn extract_key(&self, req: &super::Request) -> String {
        match &self.config.key_type {
            RateLimitKeyType::SourceIP => req.client_ip.clone(),
            RateLimitKeyType::Header(name) => req
                .headers
                .iter()
                .find(|(k, _)| k.eq_ignore_ascii_case(name))
                .map(|(_, v)| v.clone())
                .unwrap_or_else(|| "default".into()),
            RateLimitKeyType::Path => req.path.clone(),
        }
    }

    /// Check whether a request identified by `key` is allowed.
    pub fn check(&self, key: &str) -> RateLimitResult {
        let now = Instant::now();
        let mut buckets = self.buckets.write().unwrap();
        let state = buckets.entry(key.to_string()).or_insert(BucketState {
            tokens: self.config.burst as f64,
            last_refill: now,
        });

        // Refill tokens based on elapsed time.
        let elapsed = now.duration_since(state.last_refill).as_secs_f64();
        state.tokens = (state.tokens + elapsed * self.config.requests_per_second)
            .min(self.config.burst as f64);
        state.last_refill = now;

        if state.tokens >= 1.0 {
            state.tokens -= 1.0;
            RateLimitResult::Allowed {
                remaining: state.tokens as u32,
                limit: self.config.burst,
            }
        } else {
            let retry_after =
                ((1.0 - state.tokens) / self.config.requests_per_second).ceil() as u64;
            RateLimitResult::Denied {
                retry_after: Duration::from_secs(retry_after.max(1)),
            }
        }
    }

    /// Remove stale bucket entries older than `max_age`.
    pub fn cleanup(&self, max_age: Duration) {
        let cutoff = Instant::now() - max_age;
        self.buckets
            .write()
            .unwrap()
            .retain(|_, s| s.last_refill > cutoff);
    }

    /// Return the number of active (tracked) keys.
    pub fn active_count(&self) -> usize {
        self.buckets.read().unwrap().len()
    }
}

/// Result of a rate limit check.
#[derive(Debug)]
pub enum RateLimitResult {
    /// Request is allowed.
    Allowed {
        /// Remaining tokens in the bucket.
        remaining: u32,
        /// Total bucket capacity.
        limit: u32,
    },
    /// Request is denied (rate limited).
    Denied {
        /// Recommended time to wait before retrying.
        retry_after: Duration,
    },
}

// ---------------------------------------------------------------------------
// Sliding window rate limiter
// ---------------------------------------------------------------------------

/// Sliding window rate limiter configuration.
#[derive(Debug, Clone)]
pub struct SlidingWindowConfig {
    /// Maximum number of requests in the window.
    pub limit: u64,
    /// Window duration.
    pub window: Duration,
    /// How to extract the rate limit key from a request.
    pub key_type: RateLimitKeyType,
}

/// Sliding window rate limiter — provides more accurate rate limiting than
/// a simple fixed-window counter by interpolating between the current and
/// previous window.
pub struct SlidingWindow {
    config: SlidingWindowConfig,
    windows: RwLock<HashMap<String, WindowState>>,
}

struct WindowState {
    current_count: u64,
    previous_count: u64,
    current_start: Instant,
    window: Duration,
}

impl SlidingWindow {
    /// Create a new sliding window rate limiter.
    pub fn new(config: SlidingWindowConfig) -> Self {
        Self {
            config,
            windows: RwLock::new(HashMap::new()),
        }
    }

    /// Extract the rate limit key from a request.
    pub fn extract_key(&self, req: &super::Request) -> String {
        match &self.config.key_type {
            RateLimitKeyType::SourceIP => req.client_ip.clone(),
            RateLimitKeyType::Header(name) => req
                .headers
                .iter()
                .find(|(k, _)| k.eq_ignore_ascii_case(name))
                .map(|(_, v)| v.clone())
                .unwrap_or_else(|| "default".into()),
            RateLimitKeyType::Path => req.path.clone(),
        }
    }

    /// Check whether a request identified by `key` is allowed.
    ///
    /// The sliding window estimate is:
    ///   `previous_count * (1 - elapsed / window) + current_count`
    ///
    /// If the estimate is >= limit, the request is denied.
    pub fn check(&self, key: &str) -> RateLimitResult {
        let now = Instant::now();
        let mut windows = self.windows.write().unwrap();
        let state = windows.entry(key.to_string()).or_insert(WindowState {
            current_count: 0,
            previous_count: 0,
            current_start: now,
            window: self.config.window,
        });

        let elapsed = now.duration_since(state.current_start);

        // If we've moved past the current window, rotate.
        if elapsed >= state.window {
            state.previous_count = state.current_count;
            state.current_count = 0;
            state.current_start = now;
        }

        // Compute sliding window estimate.
        let window_secs = state.window.as_secs_f64();
        let elapsed_secs = now
            .duration_since(state.current_start)
            .as_secs_f64()
            .min(window_secs);
        let weight = 1.0 - (elapsed_secs / window_secs);
        let estimate = (state.previous_count as f64 * weight) + state.current_count as f64;

        if estimate >= self.config.limit as f64 {
            let retry_after_secs = (window_secs - elapsed_secs).ceil() as u64;
            RateLimitResult::Denied {
                retry_after: Duration::from_secs(retry_after_secs.max(1)),
            }
        } else {
            state.current_count += 1;
            let remaining = (self.config.limit as f64 - estimate - 1.0).max(0.0) as u32;
            RateLimitResult::Allowed {
                remaining,
                limit: self.config.limit as u32,
            }
        }
    }

    /// Return the number of active (tracked) keys.
    pub fn active_count(&self) -> usize {
        self.windows.read().unwrap().len()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn token_bucket_allows_burst() {
        let rl = TokenBucket::new(RateLimitConfig {
            requests_per_second: 10.0,
            burst: 5,
            key_type: RateLimitKeyType::SourceIP,
        });

        // Should allow `burst` requests immediately.
        for _ in 0..5 {
            assert!(matches!(
                rl.check("1.2.3.4"),
                RateLimitResult::Allowed { .. }
            ));
        }
        // Next request should be denied.
        assert!(matches!(
            rl.check("1.2.3.4"),
            RateLimitResult::Denied { .. }
        ));
    }

    #[test]
    fn token_bucket_different_keys_independent() {
        let rl = TokenBucket::new(RateLimitConfig {
            requests_per_second: 1.0,
            burst: 1,
            key_type: RateLimitKeyType::SourceIP,
        });

        assert!(matches!(rl.check("a"), RateLimitResult::Allowed { .. }));
        assert!(matches!(rl.check("b"), RateLimitResult::Allowed { .. }));
        assert!(matches!(rl.check("a"), RateLimitResult::Denied { .. }));
        assert!(matches!(rl.check("b"), RateLimitResult::Denied { .. }));
    }

    #[test]
    fn token_bucket_cleanup() {
        let rl = TokenBucket::new(RateLimitConfig {
            requests_per_second: 10.0,
            burst: 10,
            key_type: RateLimitKeyType::SourceIP,
        });

        rl.check("x");
        rl.check("y");
        assert_eq!(rl.active_count(), 2);

        // Cleanup with zero max_age should remove everything.
        rl.cleanup(Duration::from_secs(0));
        assert_eq!(rl.active_count(), 0);
    }

    #[test]
    fn token_bucket_extract_key_source_ip() {
        let rl = TokenBucket::new(RateLimitConfig {
            requests_per_second: 10.0,
            burst: 10,
            key_type: RateLimitKeyType::SourceIP,
        });

        let req = super::super::Request {
            method: "GET".into(),
            path: "/test".into(),
            host: "example.com".into(),
            headers: vec![],
            body: None,
            client_ip: "10.0.0.1".into(),
        };

        assert_eq!(rl.extract_key(&req), "10.0.0.1");
    }

    #[test]
    fn token_bucket_extract_key_header() {
        let rl = TokenBucket::new(RateLimitConfig {
            requests_per_second: 10.0,
            burst: 10,
            key_type: RateLimitKeyType::Header("X-Api-Key".into()),
        });

        let req = super::super::Request {
            method: "GET".into(),
            path: "/test".into(),
            host: "example.com".into(),
            headers: vec![("x-api-key".into(), "key123".into())],
            body: None,
            client_ip: "10.0.0.1".into(),
        };

        assert_eq!(rl.extract_key(&req), "key123");
    }

    #[test]
    fn token_bucket_extract_key_header_missing() {
        let rl = TokenBucket::new(RateLimitConfig {
            requests_per_second: 10.0,
            burst: 10,
            key_type: RateLimitKeyType::Header("X-Api-Key".into()),
        });

        let req = super::super::Request {
            method: "GET".into(),
            path: "/test".into(),
            host: "example.com".into(),
            headers: vec![],
            body: None,
            client_ip: "10.0.0.1".into(),
        };

        assert_eq!(rl.extract_key(&req), "default");
    }

    #[test]
    fn token_bucket_extract_key_path() {
        let rl = TokenBucket::new(RateLimitConfig {
            requests_per_second: 10.0,
            burst: 10,
            key_type: RateLimitKeyType::Path,
        });

        let req = super::super::Request {
            method: "GET".into(),
            path: "/api/users".into(),
            host: "example.com".into(),
            headers: vec![],
            body: None,
            client_ip: "10.0.0.1".into(),
        };

        assert_eq!(rl.extract_key(&req), "/api/users");
    }

    #[test]
    fn token_bucket_denied_has_retry_after() {
        let rl = TokenBucket::new(RateLimitConfig {
            requests_per_second: 1.0,
            burst: 1,
            key_type: RateLimitKeyType::SourceIP,
        });

        rl.check("x"); // consume the single token
        match rl.check("x") {
            RateLimitResult::Denied { retry_after } => {
                assert!(retry_after.as_secs() >= 1);
            }
            _ => panic!("expected Denied"),
        }
    }

    #[test]
    fn sliding_window_basic() {
        let sw = SlidingWindow::new(SlidingWindowConfig {
            limit: 3,
            window: Duration::from_secs(60),
            key_type: RateLimitKeyType::SourceIP,
        });

        assert!(matches!(sw.check("a"), RateLimitResult::Allowed { .. }));
        assert!(matches!(sw.check("a"), RateLimitResult::Allowed { .. }));
        assert!(matches!(sw.check("a"), RateLimitResult::Allowed { .. }));
        assert!(matches!(sw.check("a"), RateLimitResult::Denied { .. }));
    }

    #[test]
    fn sliding_window_independent_keys() {
        let sw = SlidingWindow::new(SlidingWindowConfig {
            limit: 1,
            window: Duration::from_secs(60),
            key_type: RateLimitKeyType::SourceIP,
        });

        assert!(matches!(sw.check("a"), RateLimitResult::Allowed { .. }));
        assert!(matches!(sw.check("b"), RateLimitResult::Allowed { .. }));
        assert!(matches!(sw.check("a"), RateLimitResult::Denied { .. }));
    }

    #[test]
    fn sliding_window_extract_key() {
        let sw = SlidingWindow::new(SlidingWindowConfig {
            limit: 100,
            window: Duration::from_secs(60),
            key_type: RateLimitKeyType::Path,
        });

        let req = super::super::Request {
            method: "GET".into(),
            path: "/api/v1".into(),
            host: "example.com".into(),
            headers: vec![],
            body: None,
            client_ip: "10.0.0.1".into(),
        };

        assert_eq!(sw.extract_key(&req), "/api/v1");
    }

    #[test]
    fn sliding_window_active_count() {
        let sw = SlidingWindow::new(SlidingWindowConfig {
            limit: 100,
            window: Duration::from_secs(60),
            key_type: RateLimitKeyType::SourceIP,
        });

        sw.check("a");
        sw.check("b");
        sw.check("c");
        assert_eq!(sw.active_count(), 3);
    }
}
