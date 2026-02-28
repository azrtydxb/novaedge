//! Map manager abstraction for eBPF maps.
//!
//! Provides a unified API over either real eBPF maps (Linux) or mock
//! in-memory maps (macOS / standalone mode / testing).

use std::collections::HashMap;
use std::sync::RwLock;
use std::time::Instant;

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

enum MapManagerInner {
    Mock(MockMaps),
    #[cfg(target_os = "linux")]
    Real(RealMaps),
}

/// Mock map implementation using in-memory HashMaps.
struct MockMaps {
    vips: RwLock<HashMap<[u8; 8], [u8; 8]>>,
    backends: RwLock<HashMap<[u8; 8], [u8; 8]>>,
    conntrack: RwLock<HashMap<[u8; 16], [u8; 16]>>,
    rate_limits: RwLock<HashMap<[u8; 4], [u8; 16]>>,
}

/// Real eBPF map handles (Linux only).
#[cfg(target_os = "linux")]
pub struct RealMaps {
    pub vips: aya::maps::HashMap<aya::maps::MapData, VipKey, VipValue>,
    pub backends: aya::maps::HashMap<aya::maps::MapData, BackendKey, BackendValue>,
    pub conntrack: aya::maps::HashMap<aya::maps::MapData, ConnTrackKey, ConnTrackValue>,
    pub rate_limits: aya::maps::HashMap<aya::maps::MapData, RateLimitKey, RateLimitValue>,
    pub rate_limit_cfg: aya::maps::HashMap<aya::maps::MapData, RateLimitKey, RateLimitCfg>,
    pub vip_addrs: aya::maps::HashMap<aya::maps::MapData, u32, [u8; 6]>,
}

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
            inner: MapManagerInner::Real(maps),
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
                // SAFETY: RealMaps requires &self but aya HashMap::insert needs &mut.
                // We rely on external synchronization (single writer from gRPC thread).
                let map_ptr = &m.vips
                    as *const aya::maps::HashMap<aya::maps::MapData, VipKey, VipValue>
                    as *mut aya::maps::HashMap<aya::maps::MapData, VipKey, VipValue>;
                unsafe { &mut *map_ptr }
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
                let map_ptr = &m.vips
                    as *const aya::maps::HashMap<aya::maps::MapData, VipKey, VipValue>
                    as *mut aya::maps::HashMap<aya::maps::MapData, VipKey, VipValue>;
                let _ = unsafe { &mut *map_ptr }.remove(key);
                Ok(())
            }
        }
    }

    /// Get the number of VIP entries.
    pub fn vip_count(&self) -> usize {
        match &self.inner {
            MapManagerInner::Mock(m) => m.vips.read().unwrap().len(),
            #[cfg(target_os = "linux")]
            MapManagerInner::Real(m) => m.vips.keys().count(),
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
                let map_ptr = &m.backends
                    as *const aya::maps::HashMap<aya::maps::MapData, BackendKey, BackendValue>
                    as *mut aya::maps::HashMap<aya::maps::MapData, BackendKey, BackendValue>;
                unsafe { &mut *map_ptr }
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
                let map_ptr = &m.backends
                    as *const aya::maps::HashMap<aya::maps::MapData, BackendKey, BackendValue>
                    as *mut aya::maps::HashMap<aya::maps::MapData, BackendKey, BackendValue>;
                let _ = unsafe { &mut *map_ptr }.remove(key);
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
                let map_ptr = &m.backends
                    as *const aya::maps::HashMap<aya::maps::MapData, BackendKey, BackendValue>
                    as *mut aya::maps::HashMap<aya::maps::MapData, BackendKey, BackendValue>;
                let map_mut = unsafe { &mut *map_ptr };

                // Collect keys to remove (cannot remove while iterating).
                let keys_to_remove: Vec<BackendKey> = m
                    .backends
                    .keys()
                    .into_iter()
                    .flatten()
                    .filter(|k| k.vip_id == vip_id)
                    .collect();

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
            MapManagerInner::Real(m) => m.backends.keys().count(),
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
                let map_ptr = &m.conntrack
                    as *const aya::maps::HashMap<aya::maps::MapData, ConnTrackKey, ConnTrackValue>
                    as *mut aya::maps::HashMap<aya::maps::MapData, ConnTrackKey, ConnTrackValue>;
                unsafe { &mut *map_ptr }
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
                let map_ptr = &m.conntrack
                    as *const aya::maps::HashMap<aya::maps::MapData, ConnTrackKey, ConnTrackValue>
                    as *mut aya::maps::HashMap<aya::maps::MapData, ConnTrackKey, ConnTrackValue>;
                let _ = unsafe { &mut *map_ptr }.remove(key);
                Ok(())
            }
        }
    }

    /// Get the number of connection tracking entries.
    pub fn conntrack_count(&self) -> usize {
        match &self.inner {
            MapManagerInner::Mock(m) => m.conntrack.read().unwrap().len(),
            #[cfg(target_os = "linux")]
            MapManagerInner::Real(m) => m.conntrack.keys().count(),
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
                let map_ptr = &m.rate_limits
                    as *const aya::maps::HashMap<aya::maps::MapData, RateLimitKey, RateLimitValue>
                    as *mut aya::maps::HashMap<aya::maps::MapData, RateLimitKey, RateLimitValue>;
                unsafe { &mut *map_ptr }
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
                let map_ptr = &m.rate_limits
                    as *const aya::maps::HashMap<aya::maps::MapData, RateLimitKey, RateLimitValue>
                    as *mut aya::maps::HashMap<aya::maps::MapData, RateLimitKey, RateLimitValue>;
                let _ = unsafe { &mut *map_ptr }.remove(key);
                Ok(())
            }
        }
    }

    /// Get the number of rate limit entries.
    pub fn rate_limit_count(&self) -> usize {
        match &self.inner {
            MapManagerInner::Mock(m) => m.rate_limits.read().unwrap().len(),
            #[cfg(target_os = "linux")]
            MapManagerInner::Real(m) => m.rate_limits.keys().count(),
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
