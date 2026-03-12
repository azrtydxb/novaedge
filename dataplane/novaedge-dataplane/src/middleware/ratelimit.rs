//! Rate limiting middleware using token bucket algorithm.

use std::collections::HashMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::RwLock;
use std::time::{Duration, Instant};

/// Maximum number of tracked rate-limit keys before triggering automatic cleanup.
const MAX_ENTRIES: usize = 100_000;

/// Stale entries older than this are removed during automatic cleanup.
const AUTO_CLEANUP_MAX_AGE: Duration = Duration::from_secs(300);

/// Rate limit key extraction strategy.
#[derive(Debug, Clone)]
pub enum RateLimitKeyType {
    /// Key by source IP address.
    SourceIP,
    /// Key by a specific header value.
    #[allow(dead_code)]
    Header(String),
    /// Key by request path.
    #[allow(dead_code)]
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
    #[allow(dead_code)]
    // Read by TokenBucket::extract_key(); pipeline uses client_ip directly.
    pub key_type: RateLimitKeyType,
}

/// Token bucket rate limiter for L7 requests.
pub struct TokenBucket {
    config: RateLimitConfig,
    buckets: RwLock<HashMap<String, BucketState>>,
    check_count: AtomicU64,
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
            check_count: AtomicU64::new(0),
        }
    }

    /// Extract the rate limit key from a request.
    #[allow(dead_code)]
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
        // Periodically clean up stale entries to bound memory usage.
        let count = self.check_count.fetch_add(1, Ordering::Relaxed);
        if count.is_multiple_of(1000) {
            let len = self.buckets.read().unwrap_or_else(|e| e.into_inner()).len();
            if len > MAX_ENTRIES {
                self.cleanup(AUTO_CLEANUP_MAX_AGE);
            }
        }

        let now = Instant::now();
        let mut buckets = self.buckets.write().unwrap_or_else(|e| e.into_inner());
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
            .unwrap_or_else(|e| e.into_inner())
            .retain(|_, s| s.last_refill > cutoff);
    }

    /// Return the number of active (tracked) keys.
    #[allow(dead_code)]
    pub fn active_count(&self) -> usize {
        self.buckets.read().unwrap_or_else(|e| e.into_inner()).len()
    }
}

/// Result of a rate limit check.
#[derive(Debug)]
pub enum RateLimitResult {
    /// Request is allowed.
    Allowed {
        /// Remaining tokens in the bucket.
        #[allow(dead_code)] // Informational; available for rate-limit response headers.
        remaining: u32,
        /// Total bucket capacity.
        #[allow(dead_code)] // Informational; available for rate-limit response headers.
        limit: u32,
    },
    /// Request is denied (rate limited).
    Denied {
        /// Recommended time to wait before retrying.
        retry_after: Duration,
    },
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
}
