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
    /// Minimum number of hosts in the cluster for success-rate ejection.
    pub sr_min_hosts: u32,
    /// Minimum number of requests per host for success-rate analysis.
    pub sr_min_requests: u32,
    /// Standard deviation factor: eject backends this many stddevs below mean.
    pub sr_stdev_factor: f64,
}

impl Default for OutlierConfig {
    fn default() -> Self {
        Self {
            consecutive_errors: 5,
            ejection_duration: Duration::from_secs(30),
            max_ejection_percent: 50.0,
            sr_min_hosts: 5,
            sr_min_requests: 100,
            sr_stdev_factor: 1.9,
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

    /// Perform a success-rate analysis sweep.
    ///
    /// Computes per-backend success rate, calculates mean and stddev across
    /// the cluster, and ejects backends that are `sr_stdev_factor` standard
    /// deviations below the mean. Only applies when the cluster has at least
    /// `sr_min_hosts` hosts and each host has at least `sr_min_requests`.
    /// Resets request/error counters after the sweep.
    pub fn sweep_success_rate(&self) {
        let mut stats = self.stats.write().unwrap();
        let now = Instant::now();

        // Collect success rates for hosts with enough requests.
        let mut rates: Vec<(SocketAddr, f64)> = Vec::new();
        for (&addr, s) in stats.iter() {
            // Skip already-ejected backends.
            if s.ejected_until.map(|t| t > now).unwrap_or(false) {
                continue;
            }
            if s.total_requests >= self.config.sr_min_requests as u64 {
                let success_rate = if s.total_requests > 0 {
                    (s.total_requests - s.total_errors) as f64 / s.total_requests as f64
                } else {
                    1.0
                };
                rates.push((addr, success_rate));
            }
        }

        // Only apply if enough hosts.
        if rates.len() >= self.config.sr_min_hosts as usize && !rates.is_empty() {
            let mean: f64 = rates.iter().map(|(_, r)| r).sum::<f64>() / rates.len() as f64;
            let variance: f64 = rates.iter().map(|(_, r)| (r - mean).powi(2)).sum::<f64>()
                / rates.len() as f64;
            let stddev = variance.sqrt();

            let threshold = mean - self.config.sr_stdev_factor * stddev;

            // Count currently ejected for max ejection check.
            let total_backends = stats.len().max(1);
            let currently_ejected = stats
                .values()
                .filter(|s| s.ejected_until.map(|t| t > now).unwrap_or(false))
                .count();
            let max_ejectable =
                ((total_backends as f64) * self.config.max_ejection_percent / 100.0).floor()
                    as usize;

            let mut ejected = currently_ejected;
            for (addr, rate) in &rates {
                if *rate < threshold && ejected < max_ejectable.max(1) {
                    if let Some(entry) = stats.get_mut(addr) {
                        if entry.ejected_until.is_none()
                            || entry.ejected_until.map(|t| t <= now).unwrap_or(true)
                        {
                            entry.ejected_until = Some(now + self.config.ejection_duration);
                            ejected += 1;
                        }
                    }
                }
            }
        }

        // Reset counters for next interval.
        for s in stats.values_mut() {
            s.total_requests = 0;
            s.total_errors = 0;
        }
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
            ..Default::default()
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
            ..Default::default()
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
            ..Default::default()
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
    fn success_rate_ejection() {
        let od = OutlierDetector::new(OutlierConfig {
            consecutive_errors: 100, // high threshold to avoid consecutive ejection
            ejection_duration: Duration::from_secs(60),
            max_ejection_percent: 100.0,
            sr_min_hosts: 3,
            sr_min_requests: 5,
            sr_stdev_factor: 1.0,
        });
        let addr1 = test_addr(8081);
        let addr2 = test_addr(8082);
        let addr3 = test_addr(8083);

        // Backend 1: 90% success
        for _ in 0..9 {
            od.record_success(addr1);
        }
        od.record_failure(addr1);

        // Backend 2: 90% success
        for _ in 0..9 {
            od.record_success(addr2);
        }
        od.record_failure(addr2);

        // Backend 3: 20% success (outlier)
        for _ in 0..2 {
            od.record_success(addr3);
        }
        for _ in 0..8 {
            od.record_failure(addr3);
        }

        // Before sweep — no success-rate ejection.
        assert!(!od.is_ejected(&addr3));

        // Sweep should eject addr3 (significantly below mean).
        od.sweep_success_rate();
        assert!(od.is_ejected(&addr3));
        // Good backends should not be ejected.
        assert!(!od.is_ejected(&addr1));
        assert!(!od.is_ejected(&addr2));
    }

    #[test]
    fn unknown_addr_not_ejected() {
        let od = OutlierDetector::new(OutlierConfig::default());
        let addr = test_addr(9999);
        assert!(!od.is_ejected(&addr));
    }
}
