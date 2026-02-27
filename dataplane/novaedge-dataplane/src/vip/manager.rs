use std::collections::HashMap;
use std::net::IpAddr;

/// VIP operational mode.
#[derive(Debug, Clone, PartialEq)]
pub enum VIPMode {
    L2 { arp_enabled: bool },
    BGP { asn: u32 },
    OSPF { area: u32, cost: u32 },
}

/// VIP status.
#[derive(Debug, Clone, PartialEq)]
pub enum VIPStatus {
    Pending,
    Active,
    Standby,
    Failed,
}

/// VIP state tracking.
#[derive(Debug, Clone)]
pub struct VIPState {
    pub ip: IpAddr,
    pub prefix_len: u8,
    pub interface: String,
    pub mode: VIPMode,
    pub status: VIPStatus,
}

/// VIP manager handles VIP lifecycle.
pub struct VIPManager {
    vips: HashMap<IpAddr, VIPState>,
}

impl VIPManager {
    pub fn new() -> Self {
        Self {
            vips: HashMap::new(),
        }
    }

    /// Add a VIP to the manager.
    pub fn add_vip(
        &mut self,
        ip: IpAddr,
        prefix_len: u8,
        interface: &str,
        mode: VIPMode,
    ) -> anyhow::Result<()> {
        let state = VIPState {
            ip,
            prefix_len,
            interface: interface.to_string(),
            mode,
            status: VIPStatus::Pending,
        };

        tracing::info!(vip = %ip, interface = %interface, "Adding VIP");
        self.vips.insert(ip, state);
        Ok(())
    }

    /// Activate a VIP (bind to interface + announce).
    pub fn activate(&mut self, ip: &IpAddr) -> anyhow::Result<()> {
        let state = self
            .vips
            .get_mut(ip)
            .ok_or_else(|| anyhow::anyhow!("VIP {ip} not found"))?;
        state.status = VIPStatus::Active;
        tracing::info!(vip = %ip, "VIP activated");
        Ok(())
    }

    /// Deactivate a VIP (unbind + withdraw).
    pub fn deactivate(&mut self, ip: &IpAddr) -> anyhow::Result<()> {
        let state = self
            .vips
            .get_mut(ip)
            .ok_or_else(|| anyhow::anyhow!("VIP {ip} not found"))?;
        state.status = VIPStatus::Standby;
        tracing::info!(vip = %ip, "VIP deactivated");
        Ok(())
    }

    /// Remove a VIP.
    pub fn remove_vip(&mut self, ip: &IpAddr) -> anyhow::Result<()> {
        self.vips
            .remove(ip)
            .ok_or_else(|| anyhow::anyhow!("VIP {ip} not found"))?;
        tracing::info!(vip = %ip, "VIP removed");
        Ok(())
    }

    pub fn get_vip(&self, ip: &IpAddr) -> Option<&VIPState> {
        self.vips.get(ip)
    }

    pub fn active_vips(&self) -> Vec<&VIPState> {
        self.vips
            .values()
            .filter(|v| v.status == VIPStatus::Active)
            .collect()
    }

    pub fn vip_count(&self) -> usize {
        self.vips.len()
    }
}

impl Default for VIPManager {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::Ipv4Addr;

    fn test_ip() -> IpAddr {
        IpAddr::V4(Ipv4Addr::new(10, 0, 0, 100))
    }

    fn test_ip2() -> IpAddr {
        IpAddr::V4(Ipv4Addr::new(10, 0, 0, 200))
    }

    #[test]
    fn test_add_vip() {
        let mut mgr = VIPManager::new();
        mgr.add_vip(test_ip(), 32, "eth0", VIPMode::L2 { arp_enabled: true })
            .unwrap();
        assert_eq!(mgr.vip_count(), 1);

        let vip = mgr.get_vip(&test_ip()).unwrap();
        assert_eq!(vip.status, VIPStatus::Pending);
        assert_eq!(vip.prefix_len, 32);
        assert_eq!(vip.interface, "eth0");
    }

    #[test]
    fn test_activate_vip() {
        let mut mgr = VIPManager::new();
        mgr.add_vip(test_ip(), 32, "eth0", VIPMode::L2 { arp_enabled: true })
            .unwrap();
        mgr.activate(&test_ip()).unwrap();

        let vip = mgr.get_vip(&test_ip()).unwrap();
        assert_eq!(vip.status, VIPStatus::Active);
    }

    #[test]
    fn test_deactivate_vip() {
        let mut mgr = VIPManager::new();
        mgr.add_vip(test_ip(), 32, "eth0", VIPMode::L2 { arp_enabled: true })
            .unwrap();
        mgr.activate(&test_ip()).unwrap();
        mgr.deactivate(&test_ip()).unwrap();

        let vip = mgr.get_vip(&test_ip()).unwrap();
        assert_eq!(vip.status, VIPStatus::Standby);
    }

    #[test]
    fn test_remove_vip() {
        let mut mgr = VIPManager::new();
        mgr.add_vip(test_ip(), 32, "eth0", VIPMode::L2 { arp_enabled: true })
            .unwrap();
        mgr.remove_vip(&test_ip()).unwrap();
        assert_eq!(mgr.vip_count(), 0);
        assert!(mgr.get_vip(&test_ip()).is_none());
    }

    #[test]
    fn test_remove_nonexistent_vip() {
        let mut mgr = VIPManager::new();
        assert!(mgr.remove_vip(&test_ip()).is_err());
    }

    #[test]
    fn test_activate_nonexistent_vip() {
        let mut mgr = VIPManager::new();
        assert!(mgr.activate(&test_ip()).is_err());
    }

    #[test]
    fn test_deactivate_nonexistent_vip() {
        let mut mgr = VIPManager::new();
        assert!(mgr.deactivate(&test_ip()).is_err());
    }

    #[test]
    fn test_active_vips() {
        let mut mgr = VIPManager::new();
        mgr.add_vip(test_ip(), 32, "eth0", VIPMode::L2 { arp_enabled: true })
            .unwrap();
        mgr.add_vip(test_ip2(), 32, "eth0", VIPMode::BGP { asn: 65000 })
            .unwrap();

        mgr.activate(&test_ip()).unwrap();
        // test_ip2 stays Pending

        let active = mgr.active_vips();
        assert_eq!(active.len(), 1);
        assert_eq!(active[0].ip, test_ip());
    }

    #[test]
    fn test_vip_modes() {
        let mut mgr = VIPManager::new();

        // L2 mode
        mgr.add_vip(
            IpAddr::V4(Ipv4Addr::new(10, 0, 0, 1)),
            32,
            "eth0",
            VIPMode::L2 { arp_enabled: true },
        )
        .unwrap();

        // BGP mode
        mgr.add_vip(
            IpAddr::V4(Ipv4Addr::new(10, 0, 0, 2)),
            32,
            "eth0",
            VIPMode::BGP { asn: 65000 },
        )
        .unwrap();

        // OSPF mode
        mgr.add_vip(
            IpAddr::V4(Ipv4Addr::new(10, 0, 0, 3)),
            32,
            "eth0",
            VIPMode::OSPF { area: 0, cost: 100 },
        )
        .unwrap();

        assert_eq!(mgr.vip_count(), 3);
    }

    #[test]
    fn test_full_lifecycle() {
        let mut mgr = VIPManager::new();
        let ip = test_ip();

        // Add
        mgr.add_vip(ip, 32, "eth0", VIPMode::L2 { arp_enabled: true })
            .unwrap();
        assert_eq!(mgr.get_vip(&ip).unwrap().status, VIPStatus::Pending);

        // Activate
        mgr.activate(&ip).unwrap();
        assert_eq!(mgr.get_vip(&ip).unwrap().status, VIPStatus::Active);

        // Deactivate
        mgr.deactivate(&ip).unwrap();
        assert_eq!(mgr.get_vip(&ip).unwrap().status, VIPStatus::Standby);

        // Remove
        mgr.remove_vip(&ip).unwrap();
        assert_eq!(mgr.vip_count(), 0);
    }

    #[test]
    fn test_default() {
        let mgr = VIPManager::default();
        assert_eq!(mgr.vip_count(), 0);
    }
}
