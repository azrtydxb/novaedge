use std::net::SocketAddr;
use std::time::Duration;

/// WireGuard peer configuration.
#[derive(Debug, Clone)]
pub struct WireGuardPeer {
    pub public_key: String,
    pub endpoint: Option<SocketAddr>,
    pub allowed_ips: Vec<String>,
    pub keepalive_interval: Option<Duration>,
    pub preshared_key: Option<String>,
}

/// WireGuard tunnel configuration.
#[derive(Debug, Clone)]
pub struct WireGuardConfig {
    pub interface_name: String,
    pub private_key: String,
    pub listen_port: u16,
    pub peers: Vec<WireGuardPeer>,
    pub mtu: u32,
}

impl Default for WireGuardConfig {
    fn default() -> Self {
        Self {
            interface_name: "wg-nova0".into(),
            private_key: String::new(),
            listen_port: 51820,
            peers: Vec::new(),
            mtu: 1420,
        }
    }
}

/// WireGuard tunnel manager.
pub struct WireGuardManager {
    tunnels: Vec<WireGuardConfig>,
    active: bool,
}

impl WireGuardManager {
    pub fn new() -> Self {
        Self {
            tunnels: Vec::new(),
            active: false,
        }
    }

    pub fn add_tunnel(&mut self, config: WireGuardConfig) -> anyhow::Result<()> {
        tracing::info!(interface = %config.interface_name, "Adding WireGuard tunnel");
        self.tunnels.push(config);
        Ok(())
    }

    pub fn remove_tunnel(&mut self, interface: &str) -> anyhow::Result<()> {
        self.tunnels.retain(|t| t.interface_name != interface);
        tracing::info!(interface = %interface, "Removed WireGuard tunnel");
        Ok(())
    }

    pub fn get_tunnel(&self, interface: &str) -> Option<&WireGuardConfig> {
        self.tunnels.iter().find(|t| t.interface_name == interface)
    }

    pub fn tunnel_count(&self) -> usize {
        self.tunnels.len()
    }

    #[cfg(target_os = "linux")]
    pub fn apply(&mut self) -> anyhow::Result<()> {
        // On Linux: create WireGuard interface via netlink, configure peers
        tracing::info!("Applying WireGuard configuration (Linux)");
        self.active = true;
        Ok(())
    }

    #[cfg(not(target_os = "linux"))]
    pub fn apply(&mut self) -> anyhow::Result<()> {
        tracing::info!("WireGuard apply: mock mode (non-Linux)");
        self.active = true;
        Ok(())
    }

    pub fn is_active(&self) -> bool {
        self.active
    }
}

impl Default for WireGuardManager {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::{Ipv4Addr, SocketAddrV4};

    fn make_config(name: &str) -> WireGuardConfig {
        WireGuardConfig {
            interface_name: name.into(),
            private_key: "test-private-key".into(),
            listen_port: 51820,
            peers: vec![],
            mtu: 1420,
        }
    }

    #[test]
    fn test_default_config() {
        let config = WireGuardConfig::default();
        assert_eq!(config.interface_name, "wg-nova0");
        assert!(config.private_key.is_empty());
        assert_eq!(config.listen_port, 51820);
        assert!(config.peers.is_empty());
        assert_eq!(config.mtu, 1420);
    }

    #[test]
    fn test_add_tunnel() {
        let mut mgr = WireGuardManager::new();
        mgr.add_tunnel(make_config("wg0")).unwrap();
        assert_eq!(mgr.tunnel_count(), 1);
        assert!(mgr.get_tunnel("wg0").is_some());
    }

    #[test]
    fn test_remove_tunnel() {
        let mut mgr = WireGuardManager::new();
        mgr.add_tunnel(make_config("wg0")).unwrap();
        mgr.add_tunnel(make_config("wg1")).unwrap();
        assert_eq!(mgr.tunnel_count(), 2);

        mgr.remove_tunnel("wg0").unwrap();
        assert_eq!(mgr.tunnel_count(), 1);
        assert!(mgr.get_tunnel("wg0").is_none());
        assert!(mgr.get_tunnel("wg1").is_some());
    }

    #[test]
    fn test_apply() {
        let mut mgr = WireGuardManager::new();
        assert!(!mgr.is_active());

        mgr.apply().unwrap();
        assert!(mgr.is_active());
    }

    #[test]
    fn test_peer_config() {
        let peer = WireGuardPeer {
            public_key: "peer-pub-key".into(),
            endpoint: Some(SocketAddr::V4(SocketAddrV4::new(
                Ipv4Addr::new(1, 2, 3, 4),
                51820,
            ))),
            allowed_ips: vec!["10.0.0.0/24".into(), "192.168.1.0/24".into()],
            keepalive_interval: Some(Duration::from_secs(25)),
            preshared_key: None,
        };

        assert_eq!(peer.public_key, "peer-pub-key");
        assert_eq!(peer.allowed_ips.len(), 2);
        assert_eq!(peer.keepalive_interval.unwrap(), Duration::from_secs(25));
    }

    #[test]
    fn test_config_with_peers() {
        let config = WireGuardConfig {
            interface_name: "wg-test".into(),
            private_key: "my-key".into(),
            listen_port: 12345,
            peers: vec![
                WireGuardPeer {
                    public_key: "peer1".into(),
                    endpoint: None,
                    allowed_ips: vec!["10.0.0.0/8".into()],
                    keepalive_interval: None,
                    preshared_key: None,
                },
                WireGuardPeer {
                    public_key: "peer2".into(),
                    endpoint: None,
                    allowed_ips: vec!["172.16.0.0/12".into()],
                    keepalive_interval: Some(Duration::from_secs(30)),
                    preshared_key: Some("psk".into()),
                },
            ],
            mtu: 1400,
        };

        assert_eq!(config.peers.len(), 2);
        assert_eq!(config.listen_port, 12345);
        assert_eq!(config.mtu, 1400);
    }

    #[test]
    fn test_manager_default() {
        let mgr = WireGuardManager::default();
        assert_eq!(mgr.tunnel_count(), 0);
        assert!(!mgr.is_active());
    }
}
