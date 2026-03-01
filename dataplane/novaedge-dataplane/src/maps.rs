//! Map manager abstraction for eBPF maps.
//!
//! Provides a unified API over either real eBPF maps (Linux) or mock
//! in-memory maps (macOS / standalone mode / testing).

use std::collections::HashMap;
use std::sync::RwLock;
use std::time::Instant;

#[cfg(target_os = "linux")]
use std::cell::UnsafeCell;

use novaedge_common::{
    BackendKey, BackendValue, ConnTrackKey, ConnTrackValue, RateLimitKey, RateLimitValue, VipKey,
    VipValue,
};

#[cfg(target_os = "linux")]
use novaedge_common::RateLimitCfg;

/// Status information about the map manager.
#[derive(Debug, Clone)]
pub struct MapStatus {
    /// Operating mode: "mock" or "ebpf".
    pub mode: &'static str,
    /// Number of VIP entries.
    pub vip_count: usize,
    /// Number of backend entries.
    pub backend_count: usize,
    /// Number of connection tracking entries.
    pub conntrack_count: usize,
    /// Number of rate limit entries.
    pub rate_limit_count: usize,
}

/// MapManager abstracts over real eBPF maps and mock in-memory maps.
pub struct MapManager {
    inner: MapManagerInner,
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
    vips: RwLock<HashMap<[u8; 8], [u8; 8]>>,
    backends: RwLock<HashMap<[u8; 8], [u8; 8]>>,
    conntrack: RwLock<HashMap<[u8; 16], [u8; 16]>>,
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
    pub vips: UnsafeCell<aya::maps::HashMap<aya::maps::MapData, VipKey, VipValue>>,
    pub backends: UnsafeCell<aya::maps::HashMap<aya::maps::MapData, BackendKey, BackendValue>>,
    pub conntrack: UnsafeCell<aya::maps::HashMap<aya::maps::MapData, ConnTrackKey, ConnTrackValue>>,
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
                vips: RwLock::new(HashMap::new()),
                backends: RwLock::new(HashMap::new()),
                conntrack: RwLock::new(HashMap::new()),
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
    pub fn uptime_seconds(&self) -> u64 {
        self.start_time.elapsed().as_secs()
    }

    // ── VIP operations ─────────────────────────────────────────────────

    /// Upsert a VIP entry.
    pub fn upsert_vip(&self, key: VipKey, value: VipValue) -> anyhow::Result<()> {
        match &self.inner {
            MapManagerInner::Mock(m) => {
                let k = unsafe { core::mem::transmute::<VipKey, [u8; 8]>(key) };
                let v = unsafe { core::mem::transmute::<VipValue, [u8; 8]>(value) };
                m.vips.write().unwrap().insert(k, v);
                Ok(())
            }
            #[cfg(target_os = "linux")]
            MapManagerInner::Real(m) => {
                // SAFETY: External synchronization ensures single-writer access.
                unsafe { &mut *m.vips.get() }
                    .insert(key, value, 0)
                    .map_err(|e| anyhow::anyhow!("vips insert: {e}"))?;
                Ok(())
            }
        }
    }

    /// Delete a VIP entry.
    pub fn delete_vip(&self, key: &VipKey) -> anyhow::Result<()> {
        match &self.inner {
            MapManagerInner::Mock(m) => {
                let k = unsafe { core::mem::transmute::<VipKey, [u8; 8]>(*key) };
                m.vips.write().unwrap().remove(&k);
                Ok(())
            }
            #[cfg(target_os = "linux")]
            MapManagerInner::Real(m) => {
                // SAFETY: External synchronization ensures single-writer access.
                let _ = unsafe { &mut *m.vips.get() }.remove(key);
                Ok(())
            }
        }
    }

    /// Get the number of VIP entries.
    pub fn vip_count(&self) -> usize {
        match &self.inner {
            MapManagerInner::Mock(m) => m.vips.read().unwrap().len(),
            #[cfg(target_os = "linux")]
            MapManagerInner::Real(m) => unsafe { &*m.vips.get() }.keys().count(),
        }
    }

    // ── Backend operations ─────────────────────────────────────────────

    /// Upsert a backend entry.
    pub fn upsert_backend(&self, key: BackendKey, value: BackendValue) -> anyhow::Result<()> {
        match &self.inner {
            MapManagerInner::Mock(m) => {
                let k = unsafe { core::mem::transmute::<BackendKey, [u8; 8]>(key) };
                let v = unsafe { core::mem::transmute::<BackendValue, [u8; 8]>(value) };
                m.backends.write().unwrap().insert(k, v);
                Ok(())
            }
            #[cfg(target_os = "linux")]
            MapManagerInner::Real(m) => {
                // SAFETY: External synchronization ensures single-writer access.
                unsafe { &mut *m.backends.get() }
                    .insert(key, value, 0)
                    .map_err(|e| anyhow::anyhow!("backends insert: {e}"))?;
                Ok(())
            }
        }
    }

    /// Delete a backend entry.
    pub fn delete_backend(&self, key: &BackendKey) -> anyhow::Result<()> {
        match &self.inner {
            MapManagerInner::Mock(m) => {
                let k = unsafe { core::mem::transmute::<BackendKey, [u8; 8]>(*key) };
                m.backends.write().unwrap().remove(&k);
                Ok(())
            }
            #[cfg(target_os = "linux")]
            MapManagerInner::Real(m) => {
                // SAFETY: External synchronization ensures single-writer access.
                let _ = unsafe { &mut *m.backends.get() }.remove(key);
                Ok(())
            }
        }
    }

    /// Bulk replace all backends for a given VIP id.
    ///
    /// This removes all existing backends for the VIP and inserts the new set.
    pub fn sync_backends(&self, vip_id: u32, backends: &[(BackendValue,)]) -> anyhow::Result<()> {
        match &self.inner {
            MapManagerInner::Mock(m) => {
                let mut map = m.backends.write().unwrap();
                // Remove all existing entries for this VIP.
                map.retain(|k, _| {
                    let bk = unsafe { core::mem::transmute::<[u8; 8], BackendKey>(*k) };
                    bk.vip_id != vip_id
                });
                // Insert new backends.
                for (index, (val,)) in backends.iter().enumerate() {
                    let key = BackendKey {
                        vip_id,
                        index: index as u32,
                    };
                    let k = unsafe { core::mem::transmute::<BackendKey, [u8; 8]>(key) };
                    let v = unsafe { core::mem::transmute::<BackendValue, [u8; 8]>(*val) };
                    map.insert(k, v);
                }
                Ok(())
            }
            #[cfg(target_os = "linux")]
            MapManagerInner::Real(m) => {
                let ptr = m.backends.get();

                // Collect keys to remove (cannot remove while iterating).
                // SAFETY: No mutable reference is live during this read.
                let keys_to_remove: Vec<BackendKey> = unsafe { &*ptr }
                    .keys()
                    .flatten()
                    .filter(|k| k.vip_id == vip_id)
                    .collect();

                // SAFETY: External synchronization ensures single-writer access.
                let map_mut = unsafe { &mut *ptr };
                for k in keys_to_remove {
                    let _ = map_mut.remove(&k);
                }

                // Insert new backends.
                for (index, (val,)) in backends.iter().enumerate() {
                    let key = BackendKey {
                        vip_id,
                        index: index as u32,
                    };
                    map_mut
                        .insert(key, *val, 0)
                        .map_err(|e| anyhow::anyhow!("backends sync insert: {e}"))?;
                }
                Ok(())
            }
        }
    }

    /// Get the number of backend entries.
    pub fn backend_count(&self) -> usize {
        match &self.inner {
            MapManagerInner::Mock(m) => m.backends.read().unwrap().len(),
            #[cfg(target_os = "linux")]
            MapManagerInner::Real(m) => unsafe { &*m.backends.get() }.keys().count(),
        }
    }

    // ── Connection tracking operations ─────────────────────────────────

    /// Upsert a connection tracking entry.
    pub fn upsert_conntrack(&self, key: ConnTrackKey, value: ConnTrackValue) -> anyhow::Result<()> {
        match &self.inner {
            MapManagerInner::Mock(m) => {
                let k = unsafe { core::mem::transmute::<ConnTrackKey, [u8; 16]>(key) };
                let v = unsafe { core::mem::transmute::<ConnTrackValue, [u8; 16]>(value) };
                m.conntrack.write().unwrap().insert(k, v);
                Ok(())
            }
            #[cfg(target_os = "linux")]
            MapManagerInner::Real(m) => {
                // SAFETY: External synchronization ensures single-writer access.
                unsafe { &mut *m.conntrack.get() }
                    .insert(key, value, 0)
                    .map_err(|e| anyhow::anyhow!("conntrack insert: {e}"))?;
                Ok(())
            }
        }
    }

    /// Delete a connection tracking entry.
    pub fn delete_conntrack(&self, key: &ConnTrackKey) -> anyhow::Result<()> {
        match &self.inner {
            MapManagerInner::Mock(m) => {
                let k = unsafe { core::mem::transmute::<ConnTrackKey, [u8; 16]>(*key) };
                m.conntrack.write().unwrap().remove(&k);
                Ok(())
            }
            #[cfg(target_os = "linux")]
            MapManagerInner::Real(m) => {
                // SAFETY: External synchronization ensures single-writer access.
                let _ = unsafe { &mut *m.conntrack.get() }.remove(key);
                Ok(())
            }
        }
    }

    /// Get the number of connection tracking entries.
    pub fn conntrack_count(&self) -> usize {
        match &self.inner {
            MapManagerInner::Mock(m) => m.conntrack.read().unwrap().len(),
            #[cfg(target_os = "linux")]
            MapManagerInner::Real(m) => unsafe { &*m.conntrack.get() }.keys().count(),
        }
    }

    // ── Rate limiting operations ───────────────────────────────────────

    /// Upsert a rate limit entry.
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
            vip_count: self.vip_count(),
            backend_count: self.backend_count(),
            conntrack_count: self.conntrack_count(),
            rate_limit_count: self.rate_limit_count(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_mock_vip_upsert_delete() {
        let mgr = MapManager::new_mock();
        let key = VipKey {
            vip: 0x0A000001,
            port: 80,
            protocol: 6,
            _pad: 0,
        };
        let val = VipValue {
            backend_count: 3,
            flags: 0,
        };

        mgr.upsert_vip(key, val).unwrap();
        assert_eq!(mgr.vip_count(), 1);

        mgr.delete_vip(&key).unwrap();
        assert_eq!(mgr.vip_count(), 0);
    }

    #[test]
    fn test_mock_backend_upsert_delete() {
        let mgr = MapManager::new_mock();
        let key = BackendKey {
            vip_id: 1,
            index: 0,
        };
        let val = BackendValue {
            addr: 0x0A000002,
            port: 8080,
            weight: 100,
        };

        mgr.upsert_backend(key, val).unwrap();
        assert_eq!(mgr.backend_count(), 1);

        mgr.delete_backend(&key).unwrap();
        assert_eq!(mgr.backend_count(), 0);
    }

    #[test]
    fn test_mock_conntrack_upsert_delete() {
        let mgr = MapManager::new_mock();
        let key = ConnTrackKey {
            src_ip: 0x0A000001,
            dst_ip: 0x0A600064,
            src_port: 12345,
            dst_port: 80,
            protocol: 6,
            _pad: [0; 3],
        };
        let val = ConnTrackValue {
            backend_ip: 0x0A000002,
            backend_port: 8080,
            state: 1,
            _pad: 0,
            timestamp: 123456789,
        };

        mgr.upsert_conntrack(key, val).unwrap();
        assert_eq!(mgr.conntrack_count(), 1);

        mgr.delete_conntrack(&key).unwrap();
        assert_eq!(mgr.conntrack_count(), 0);
    }

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
    fn test_sync_backends() {
        let mgr = MapManager::new_mock();
        let b1 = BackendValue {
            addr: 0x0A000001,
            port: 8080,
            weight: 50,
        };
        let b2 = BackendValue {
            addr: 0x0A000002,
            port: 8080,
            weight: 50,
        };
        mgr.sync_backends(1, &[(b1,), (b2,)]).unwrap();
        assert_eq!(mgr.backend_count(), 2);

        let b3 = BackendValue {
            addr: 0x0A000003,
            port: 9090,
            weight: 100,
        };
        mgr.sync_backends(1, &[(b3,)]).unwrap();
        assert_eq!(mgr.backend_count(), 1);

        mgr.sync_backends(2, &[(b1,), (b2,)]).unwrap();
        assert_eq!(mgr.backend_count(), 3);
    }

    #[test]
    fn test_get_status() {
        let mgr = MapManager::new_mock();
        let status = mgr.get_status();
        assert_eq!(status.mode, "mock");
        assert_eq!(status.vip_count, 0);
        assert_eq!(status.backend_count, 0);
        assert_eq!(status.conntrack_count, 0);
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
