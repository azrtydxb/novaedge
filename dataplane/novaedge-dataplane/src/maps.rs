//! Map manager abstraction for eBPF maps.
//!
//! Provides a unified API over either real eBPF maps (Linux) or mock
//! in-memory maps (macOS / standalone mode / testing).

use std::collections::HashMap;
use std::sync::RwLock;
use std::time::Instant;

#[cfg(target_os = "linux")]
use std::cell::UnsafeCell;

use novaedge_common::{RateLimitKey, RateLimitValue};

#[cfg(target_os = "linux")]
use novaedge_common::RateLimitCfg;

/// Status information about the map manager.
#[derive(Debug, Clone)]
pub struct MapStatus {
    /// Operating mode: "mock" or "ebpf".
    pub mode: &'static str,
    /// Number of rate limit entries.
    pub rate_limit_count: usize,
}

/// MapManager abstracts over real eBPF maps and mock in-memory maps.
pub struct MapManager {
    inner: MapManagerInner,
    /// Used by `uptime_seconds()` for status reporting.
    #[allow(dead_code)]
    start_time: Instant,
}

#[allow(clippy::large_enum_variant)]
enum MapManagerInner {
    Mock(MockMaps),
    #[cfg(target_os = "linux")]
    Real(Box<RealMaps>),
}

/// Mock map implementation using in-memory HashMaps.
struct MockMaps {
    rate_limits: RwLock<HashMap<[u8; 4], [u8; 16]>>,
}

/// Real eBPF map handles (Linux only).
///
/// Uses `UnsafeCell` for interior mutability because aya's `HashMap::insert`
/// and `HashMap::remove` require `&mut self`, but `MapManager` exposes `&self`
/// methods for ergonomic usage behind `Arc`. External synchronization (single
/// writer from the gRPC thread) ensures safety.
#[cfg(target_os = "linux")]
pub struct RealMaps {
    pub rate_limits:
        UnsafeCell<aya::maps::HashMap<aya::maps::MapData, RateLimitKey, RateLimitValue>>,
    pub rate_limit_cfg:
        UnsafeCell<aya::maps::HashMap<aya::maps::MapData, RateLimitKey, RateLimitCfg>>,
    pub vip_addrs: UnsafeCell<aya::maps::HashMap<aya::maps::MapData, u32, [u8; 6]>>,
}

// SAFETY: RealMaps is safe to send between threads because the aya map
// handles are just file descriptors. External synchronization (single
// writer from gRPC thread) ensures no data races.
#[cfg(target_os = "linux")]
unsafe impl Send for RealMaps {}
#[cfg(target_os = "linux")]
unsafe impl Sync for RealMaps {}

impl MapManager {
    /// Create a new mock MapManager (for macOS, standalone, or testing).
    pub fn new_mock() -> Self {
        Self {
            inner: MapManagerInner::Mock(MockMaps {
                rate_limits: RwLock::new(HashMap::new()),
            }),
            start_time: Instant::now(),
        }
    }

    /// Create a new MapManager wrapping real eBPF maps (Linux only).
    #[cfg(target_os = "linux")]
    pub fn new_real(maps: RealMaps) -> Self {
        Self {
            inner: MapManagerInner::Real(Box::new(maps)),
            start_time: Instant::now(),
        }
    }

    /// Return the mode string for status reporting.
    pub fn mode(&self) -> &'static str {
        match &self.inner {
            MapManagerInner::Mock(_) => "mock",
            #[cfg(target_os = "linux")]
            MapManagerInner::Real(_) => "ebpf",
        }
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
        match &self.inner {
            MapManagerInner::Mock(m) => {
                let k = unsafe { core::mem::transmute::<RateLimitKey, [u8; 4]>(key) };
                let v = unsafe { core::mem::transmute::<RateLimitValue, [u8; 16]>(value) };
                m.rate_limits.write().unwrap().insert(k, v);
                Ok(())
            }
            #[cfg(target_os = "linux")]
            MapManagerInner::Real(m) => {
                // SAFETY: External synchronization ensures single-writer access.
                unsafe { &mut *m.rate_limits.get() }
                    .insert(key, value, 0)
                    .map_err(|e| anyhow::anyhow!("rate_limits insert: {e}"))?;
                Ok(())
            }
        }
    }

    /// Delete a rate limit entry.
    #[allow(dead_code)]
    pub fn delete_rate_limit(&self, key: &RateLimitKey) -> anyhow::Result<()> {
        match &self.inner {
            MapManagerInner::Mock(m) => {
                let k = unsafe { core::mem::transmute::<RateLimitKey, [u8; 4]>(*key) };
                m.rate_limits.write().unwrap().remove(&k);
                Ok(())
            }
            #[cfg(target_os = "linux")]
            MapManagerInner::Real(m) => {
                // SAFETY: External synchronization ensures single-writer access.
                let _ = unsafe { &mut *m.rate_limits.get() }.remove(key);
                Ok(())
            }
        }
    }

    /// Get the number of rate limit entries.
    pub fn rate_limit_count(&self) -> usize {
        match &self.inner {
            MapManagerInner::Mock(m) => m.rate_limits.read().unwrap().len(),
            #[cfg(target_os = "linux")]
            MapManagerInner::Real(m) => unsafe { &*m.rate_limits.get() }.keys().count(),
        }
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
        let mgr = MapManager::new_mock();
        let status = mgr.get_status();
        assert_eq!(status.mode, "mock");
        assert_eq!(status.rate_limit_count, 0);
    }

    #[test]
    fn test_mock_mode() {
        let mgr = MapManager::new_mock();
        assert_eq!(mgr.mode(), "mock");
    }

    #[test]
    fn test_uptime() {
        let mgr = MapManager::new_mock();
        assert!(mgr.uptime_seconds() < 2);
    }
}
