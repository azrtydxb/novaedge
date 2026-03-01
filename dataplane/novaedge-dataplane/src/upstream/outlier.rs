//! Outlier detection and ejection for upstream backends.
//!
//! Tracks per-backend error rates and ejects backends that exceed
//! a consecutive error threshold, with configurable ejection duration
//! and maximum ejection percentage.

use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::RwLock;
use std::time::{Duration, Instant};

/// Outlier detection configuration.
#[derive(Debug, Clone)]
pub struct OutlierConfig {
    /// Number of consecutive errors before ejecting a backend.
    pub consecutive_errors: u32,
    /// How long a backend stays ejected.
    pub ejection_duration: Duration,
    /// Maximum percentage of backends that can be ejected simultaneously.
    pub max_ejection_percent: f64,
}

impl Default for OutlierConfig {
    fn default() -> Self {
        Self {
            consecutive_errors: 5,
            ejection_duration: Duration::from_secs(30),
            max_ejection_percent: 50.0,
        }
    }
}

#[derive(Default)]
struct BackendStats {
    consecutive_errors: u32,
    ejected_until: Option<Instant>,
    total_requests: u64,
    total_errors: u64,
}

/// Outlier detector that ejects misbehaving backends.
pub struct OutlierDetector {
    stats: RwLock<HashMap<SocketAddr, BackendStats>>,
    config: OutlierConfig,
}

impl OutlierDetector {
    /// Create a new outlier detector with the given configuration.
    pub fn new(config: OutlierConfig) -> Self {
        Self {
            stats: RwLock::new(HashMap::new()),
            config,
        }
    }

    /// Record a successful request to the given backend.
    pub fn record_success(&self, addr: SocketAddr) {
        let mut stats = self.stats.write().unwrap();
        let entry = stats.entry(addr).or_default();
        entry.consecutive_errors = 0;
        entry.total_requests += 1;
    }

    /// Record a failed request to the given backend.
    pub fn record_failure(&self, addr: SocketAddr) {
        let mut stats = self.stats.write().unwrap();
        let total_backends = stats.len().max(1);

        // Update counters first.
        let entry = stats.entry(addr).or_default();
        entry.consecutive_errors += 1;
        entry.total_requests += 1;
        entry.total_errors += 1;

        // Read the values we need before releasing the mutable borrow on entry.
        let should_consider_ejection = entry.consecutive_errors >= self.config.consecutive_errors
            && entry.ejected_until.is_none();

        if should_consider_ejection {
            // Count currently ejected backends (iterate over all entries).
            let now = Instant::now();
            let currently_ejected = stats
                .values()
                .filter(|s| s.ejected_until.map(|t| t > now).unwrap_or(false))
                .count();

            let max_ejectable = ((total_backends as f64) * self.config.max_ejection_percent / 100.0)
                .floor() as usize;

            if currently_ejected < max_ejectable.max(1) {
                // Re-borrow the specific entry to set ejection time.
                if let Some(entry) = stats.get_mut(&addr) {
                    entry.ejected_until = Some(now + self.config.ejection_duration);
                }
            }
        }
    }

    /// Check whether a backend is currently ejected.
    pub fn is_ejected(&self, addr: &SocketAddr) -> bool {
        let stats = self.stats.read().unwrap();
        stats
            .get(addr)
            .and_then(|s| s.ejected_until)
            .map(|t| t > Instant::now())
            .unwrap_or(false)
    }

    /// Get the number of currently ejected backends.
    pub fn ejected_count(&self) -> usize {
        let stats = self.stats.read().unwrap();
        let now = Instant::now();
        stats
            .values()
            .filter(|s| s.ejected_until.map(|t| t > now).unwrap_or(false))
            .count()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::{IpAddr, Ipv4Addr};

    fn test_addr(port: u16) -> SocketAddr {
        SocketAddr::new(IpAddr::V4(Ipv4Addr::new(10, 0, 0, 1)), port)
    }

    #[test]
    fn no_ejection_below_threshold() {
        let od = OutlierDetector::new(OutlierConfig {
            consecutive_errors: 5,
            ..Default::default()
        });
        let addr = test_addr(8080);

        for _ in 0..4 {
            od.record_failure(addr);
        }
        assert!(!od.is_ejected(&addr));
    }

    #[test]
    fn ejection_at_threshold() {
        let od = OutlierDetector::new(OutlierConfig {
            consecutive_errors: 3,
            ejection_duration: Duration::from_secs(60),
            max_ejection_percent: 100.0,
        });
        let addr = test_addr(8080);

        for _ in 0..3 {
            od.record_failure(addr);
        }
        assert!(od.is_ejected(&addr));
        assert_eq!(od.ejected_count(), 1);
    }

    #[test]
    fn ejection_expires() {
        let od = OutlierDetector::new(OutlierConfig {
            consecutive_errors: 2,
            ejection_duration: Duration::from_millis(50),
            max_ejection_percent: 100.0,
        });
        let addr = test_addr(8080);

        od.record_failure(addr);
        od.record_failure(addr);
        assert!(od.is_ejected(&addr));

        std::thread::sleep(Duration::from_millis(100));
        assert!(!od.is_ejected(&addr));
    }

    #[test]
    fn success_resets_consecutive_errors() {
        let od = OutlierDetector::new(OutlierConfig {
            consecutive_errors: 3,
            max_ejection_percent: 100.0,
            ..Default::default()
        });
        let addr = test_addr(8080);

        od.record_failure(addr);
        od.record_failure(addr);
        od.record_success(addr);
        // Counter reset — two more failures should not eject.
        od.record_failure(addr);
        od.record_failure(addr);
        assert!(!od.is_ejected(&addr));
    }

    #[test]
    fn max_ejection_percent() {
        let od = OutlierDetector::new(OutlierConfig {
            consecutive_errors: 1,
            ejection_duration: Duration::from_secs(60),
            max_ejection_percent: 50.0,
        });

        let addr1 = test_addr(8081);
        let addr2 = test_addr(8082);

        // Register both backends first with a success.
        od.record_success(addr1);
        od.record_success(addr2);

        // Now fail addr1 — should be ejected (1/2 = 50%).
        od.record_failure(addr1);
        assert!(od.is_ejected(&addr1));

        // Fail addr2 — should NOT be ejected (would exceed 50% of 2 backends).
        od.record_failure(addr2);
        assert!(!od.is_ejected(&addr2));
    }

    #[test]
    fn unknown_addr_not_ejected() {
        let od = OutlierDetector::new(OutlierConfig::default());
        let addr = test_addr(9999);
        assert!(!od.is_ejected(&addr));
    }
}
