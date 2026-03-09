//! Map manager for in-memory rate-limit state.
//!
//! With eBPF removed, this uses in-memory HashMaps for all platforms.

use std::collections::HashMap;
use std::sync::RwLock;
use std::time::Instant;

use novaedge_common::{RateLimitKey, RateLimitValue};

/// Status information about the map manager.
#[derive(Debug, Clone)]
pub struct MapStatus {
    /// Operating mode (always "in-memory" now).
    pub mode: &'static str,
    /// Number of rate limit entries.
    pub rate_limit_count: usize,
}

/// MapManager provides in-memory rate-limit state storage.
pub struct MapManager {
    rate_limits: RwLock<HashMap<[u8; 4], [u8; 16]>>,
    /// Used by `uptime_seconds()` for status reporting.
    #[allow(dead_code)]
    start_time: Instant,
}

impl MapManager {
    /// Create a new MapManager with in-memory storage.
    pub fn new() -> Self {
        Self {
            rate_limits: RwLock::new(HashMap::new()),
            start_time: Instant::now(),
        }
    }

    /// Backwards-compatible alias for `new()` (used by tests).
    #[allow(dead_code)]
    pub fn new_mock() -> Self {
        Self::new()
    }

    /// Return the mode string for status reporting.
    pub fn mode(&self) -> &'static str {
        "in-memory"
    }

    /// Return uptime in seconds since the manager was created.
    #[allow(dead_code)]
    pub fn uptime_seconds(&self) -> u64 {
        self.start_time.elapsed().as_secs()
    }

    // ── Rate limiting operations ───────────────────────────────────────

    /// Upsert a rate limit entry.
    #[allow(dead_code)]
    pub fn upsert_rate_limit(
        &self,
        key: RateLimitKey,
        value: RateLimitValue,
    ) -> anyhow::Result<()> {
        let k = unsafe { core::mem::transmute::<RateLimitKey, [u8; 4]>(key) };
        let v = unsafe { core::mem::transmute::<RateLimitValue, [u8; 16]>(value) };
        self.rate_limits.write().unwrap().insert(k, v);
        Ok(())
    }

    /// Delete a rate limit entry.
    #[allow(dead_code)]
    pub fn delete_rate_limit(&self, key: &RateLimitKey) -> anyhow::Result<()> {
        let k = unsafe { core::mem::transmute::<RateLimitKey, [u8; 4]>(*key) };
        self.rate_limits.write().unwrap().remove(&k);
        Ok(())
    }

    /// Get the number of rate limit entries.
    pub fn rate_limit_count(&self) -> usize {
        self.rate_limits.read().unwrap().len()
    }

    // ── Status ─────────────────────────────────────────────────────────

    /// Get a status snapshot of all map sizes and mode.
    pub fn get_status(&self) -> MapStatus {
        MapStatus {
            mode: self.mode(),
            rate_limit_count: self.rate_limit_count(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_mock_rate_limit_upsert_delete() {
        let mgr = MapManager::new_mock();
        let key = RateLimitKey { src_ip: 0x0A000001 };
        let val = RateLimitValue {
            tokens: 1000,
            last_refill: 123456789,
        };

        mgr.upsert_rate_limit(key, val).unwrap();
        assert_eq!(mgr.rate_limit_count(), 1);

        mgr.delete_rate_limit(&key).unwrap();
        assert_eq!(mgr.rate_limit_count(), 0);
    }

    #[test]
    fn test_get_status() {
        let mgr = MapManager::new();
        let status = mgr.get_status();
        assert_eq!(status.mode, "in-memory");
        assert_eq!(status.rate_limit_count, 0);
    }

    #[test]
    fn test_mode() {
        let mgr = MapManager::new();
        assert_eq!(mgr.mode(), "in-memory");
    }

    #[test]
    fn test_uptime() {
        let mgr = MapManager::new_mock();
        assert!(mgr.uptime_seconds() < 2);
    }
}
