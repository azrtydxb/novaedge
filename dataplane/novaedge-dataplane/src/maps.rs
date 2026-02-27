//! Map manager abstraction for eBPF maps.
//!
//! Provides a unified API over either real eBPF maps (Linux) or mock
//! in-memory maps (macOS / standalone mode / testing).

use std::collections::HashMap;
use std::sync::RwLock;

#[allow(unused_imports)]
use novaedge_common::{
    BackendKey, BackendValue, ConnTrackKey, ConnTrackValue, RateLimitKey, RateLimitValue, VipKey,
    VipValue,
};

/// MapManager abstracts over real eBPF maps and mock in-memory maps.
#[allow(dead_code)]
pub struct MapManager {
    inner: MapManagerInner,
}

#[allow(dead_code)]
enum MapManagerInner {
    Mock(MockMaps),
    #[cfg(target_os = "linux")]
    #[allow(dead_code)]
    Real(RealMaps),
}

/// Mock map implementation using in-memory HashMaps.
#[allow(dead_code)]
struct MockMaps {
    vips: RwLock<HashMap<[u8; 8], [u8; 8]>>,
    backends: RwLock<HashMap<[u8; 8], [u8; 8]>>,
    conntrack: RwLock<HashMap<[u8; 16], [u8; 16]>>,
    rate_limits: RwLock<HashMap<[u8; 4], [u8; 16]>>,
}

/// Real eBPF map handles (Linux only).
#[cfg(target_os = "linux")]
struct RealMaps {
    // TODO: Add aya::maps::HashMap handles in Phase 2.
}

#[allow(dead_code)]
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
            MapManagerInner::Real(_m) => {
                // TODO: Implement in Phase 2.
                let _ = (key, value);
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
            MapManagerInner::Real(_m) => {
                let _ = key;
                Ok(())
            }
        }
    }

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
            MapManagerInner::Real(_m) => {
                let _ = (key, value);
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
            MapManagerInner::Real(_m) => {
                let _ = key;
                Ok(())
            }
        }
    }

    /// Get the number of VIP entries.
    pub fn vip_count(&self) -> usize {
        match &self.inner {
            MapManagerInner::Mock(m) => m.vips.read().unwrap().len(),
            #[cfg(target_os = "linux")]
            MapManagerInner::Real(_m) => 0, // TODO
        }
    }

    /// Get the number of backend entries.
    pub fn backend_count(&self) -> usize {
        match &self.inner {
            MapManagerInner::Mock(m) => m.backends.read().unwrap().len(),
            #[cfg(target_os = "linux")]
            MapManagerInner::Real(_m) => 0, // TODO
        }
    }

    /// Get the number of connection tracking entries.
    pub fn conntrack_count(&self) -> usize {
        match &self.inner {
            MapManagerInner::Mock(m) => m.conntrack.read().unwrap().len(),
            #[cfg(target_os = "linux")]
            MapManagerInner::Real(_m) => 0, // TODO
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_mock_vip_upsert_delete() {
        let mgr = MapManager::new_mock();
        let key = VipKey { vip: 0x0A000001, port: 80, protocol: 6, _pad: 0 };
        let val = VipValue { backend_count: 3, flags: 0 };

        mgr.upsert_vip(key, val).unwrap();
        assert_eq!(mgr.vip_count(), 1);

        mgr.delete_vip(&key).unwrap();
        assert_eq!(mgr.vip_count(), 0);
    }

    #[test]
    fn test_mock_backend_upsert_delete() {
        let mgr = MapManager::new_mock();
        let key = BackendKey { vip_id: 1, index: 0 };
        let val = BackendValue { addr: 0x0A000002, port: 8080, weight: 100 };

        mgr.upsert_backend(key, val).unwrap();
        assert_eq!(mgr.backend_count(), 1);

        mgr.delete_backend(&key).unwrap();
        assert_eq!(mgr.backend_count(), 0);
    }

    #[test]
    fn test_mock_mode() {
        let mgr = MapManager::new_mock();
        assert_eq!(mgr.mode(), "mock");
    }
}
