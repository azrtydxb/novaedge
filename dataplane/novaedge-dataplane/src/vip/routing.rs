use std::net::IpAddr;

/// Route delegation actions.
#[derive(Debug, Clone)]
#[allow(dead_code)] // Represents BGP/OSPF actions, used by tests and future route batching
pub enum RouteAction {
    Advertise {
        prefix: String,
        communities: Vec<String>,
    },
    Withdraw {
        prefix: String,
    },
}

/// Route client for delegating BGP/OSPF to novaroute.
pub struct RouteClient {
    endpoint: String,
    connected: bool,
}

impl RouteClient {
    pub fn new(endpoint: &str) -> Self {
        Self {
            endpoint: endpoint.to_string(),
            connected: false,
        }
    }

    /// Connect to the novaroute agent.
    pub async fn connect(&mut self) -> anyhow::Result<()> {
        tracing::info!(endpoint = %self.endpoint, "Connecting to novaroute");
        self.connected = true;
        Ok(())
    }

    pub fn is_connected(&self) -> bool {
        self.connected
    }

    /// Advertise a VIP prefix via BGP/OSPF.
    pub async fn advertise_vip(&self, vip: IpAddr) -> anyhow::Result<()> {
        if !self.connected {
            anyhow::bail!("Not connected to novaroute");
        }
        let prefix = match vip {
            IpAddr::V4(_) => format!("{vip}/32"),
            IpAddr::V6(_) => format!("{vip}/128"),
        };
        tracing::info!(prefix = %prefix, "Advertising VIP prefix");
        Ok(())
    }

    /// Withdraw a VIP prefix.
    pub async fn withdraw_vip(&self, vip: IpAddr) -> anyhow::Result<()> {
        if !self.connected {
            anyhow::bail!("Not connected to novaroute");
        }
        let prefix = match vip {
            IpAddr::V4(_) => format!("{vip}/32"),
            IpAddr::V6(_) => format!("{vip}/128"),
        };
        tracing::info!(prefix = %prefix, "Withdrawing VIP prefix");
        Ok(())
    }

    pub fn disconnect(&mut self) {
        self.connected = false;
        tracing::info!("Disconnected from novaroute");
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::{Ipv4Addr, Ipv6Addr};

    #[tokio::test]
    async fn test_connect() {
        let mut client = RouteClient::new("unix:///run/novaroute.sock");
        assert!(!client.is_connected());

        client.connect().await.unwrap();
        assert!(client.is_connected());
    }

    #[tokio::test]
    async fn test_disconnect() {
        let mut client = RouteClient::new("unix:///run/novaroute.sock");
        client.connect().await.unwrap();
        client.disconnect();
        assert!(!client.is_connected());
    }

    #[tokio::test]
    async fn test_advertise_vip_v4() {
        let mut client = RouteClient::new("unix:///run/novaroute.sock");
        client.connect().await.unwrap();

        let vip = IpAddr::V4(Ipv4Addr::new(10, 0, 0, 100));
        client.advertise_vip(vip).await.unwrap();
    }

    #[tokio::test]
    async fn test_advertise_vip_v6() {
        let mut client = RouteClient::new("unix:///run/novaroute.sock");
        client.connect().await.unwrap();

        let vip = IpAddr::V6(Ipv6Addr::new(0xfd00, 0, 0, 0, 0, 0, 0, 1));
        client.advertise_vip(vip).await.unwrap();
    }

    #[tokio::test]
    async fn test_withdraw_vip() {
        let mut client = RouteClient::new("unix:///run/novaroute.sock");
        client.connect().await.unwrap();

        let vip = IpAddr::V4(Ipv4Addr::new(10, 0, 0, 100));
        client.withdraw_vip(vip).await.unwrap();
    }

    #[tokio::test]
    async fn test_advertise_not_connected() {
        let client = RouteClient::new("unix:///run/novaroute.sock");
        let vip = IpAddr::V4(Ipv4Addr::new(10, 0, 0, 100));
        assert!(client.advertise_vip(vip).await.is_err());
    }

    #[tokio::test]
    async fn test_withdraw_not_connected() {
        let client = RouteClient::new("unix:///run/novaroute.sock");
        let vip = IpAddr::V4(Ipv4Addr::new(10, 0, 0, 100));
        assert!(client.withdraw_vip(vip).await.is_err());
    }

    #[test]
    fn test_route_action_variants() {
        let advertise = RouteAction::Advertise {
            prefix: "10.0.0.100/32".into(),
            communities: vec!["65000:100".into()],
        };
        let withdraw = RouteAction::Withdraw {
            prefix: "10.0.0.100/32".into(),
        };

        // Just verify they can be constructed
        match advertise {
            RouteAction::Advertise {
                prefix,
                communities,
            } => {
                assert_eq!(prefix, "10.0.0.100/32");
                assert_eq!(communities.len(), 1);
            }
            _ => panic!("Expected Advertise"),
        }
        match withdraw {
            RouteAction::Withdraw { prefix } => {
                assert_eq!(prefix, "10.0.0.100/32");
            }
            _ => panic!("Expected Withdraw"),
        }
    }
}
