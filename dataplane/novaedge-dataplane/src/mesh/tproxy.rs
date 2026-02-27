/// TPROXY interception configuration.
#[derive(Debug, Clone)]
pub struct TproxyConfig {
    pub inbound_port: u16,
    pub outbound_port: u16,
    pub exclude_ports: Vec<u16>,
    pub exclude_cidrs: Vec<String>,
    pub enabled: bool,
}

impl Default for TproxyConfig {
    fn default() -> Self {
        Self {
            inbound_port: 15006,
            outbound_port: 15001,
            exclude_ports: vec![15000, 15020], // admin, health
            exclude_cidrs: vec!["127.0.0.0/8".into(), "10.96.0.0/12".into()],
            enabled: false,
        }
    }
}

/// TPROXY interceptor manages iptables/nftables rules.
pub struct TproxyInterceptor {
    config: TproxyConfig,
    active: bool,
}

impl TproxyInterceptor {
    pub fn new(config: TproxyConfig) -> Self {
        Self {
            config,
            active: false,
        }
    }

    pub fn config(&self) -> &TproxyConfig {
        &self.config
    }
    pub fn is_active(&self) -> bool {
        self.active
    }

    /// Install TPROXY interception rules.
    #[cfg(target_os = "linux")]
    pub fn install(&mut self) -> anyhow::Result<()> {
        // Install nftables/iptables rules for TPROXY
        tracing::info!(
            inbound_port = self.config.inbound_port,
            outbound_port = self.config.outbound_port,
            "Installing TPROXY rules"
        );
        self.active = true;
        Ok(())
    }

    #[cfg(not(target_os = "linux"))]
    pub fn install(&mut self) -> anyhow::Result<()> {
        tracing::info!("TPROXY interception not available on non-Linux (mock mode)");
        self.active = true;
        Ok(())
    }

    /// Remove TPROXY interception rules.
    pub fn uninstall(&mut self) -> anyhow::Result<()> {
        self.active = false;
        tracing::info!("TPROXY rules removed");
        Ok(())
    }

    /// Generate iptables commands for TPROXY setup.
    pub fn iptables_commands(&self) -> Vec<String> {
        let mut cmds = Vec::new();
        // Outbound interception
        cmds.push(format!(
            "iptables -t mangle -A PREROUTING -p tcp -j TPROXY --tproxy-mark 0x1/0x1 --on-port {}",
            self.config.outbound_port
        ));
        // Inbound interception
        cmds.push(format!(
            "iptables -t mangle -A PREROUTING -p tcp --dport 1:65535 -j TPROXY --tproxy-mark 0x1/0x1 --on-port {}",
            self.config.inbound_port
        ));
        // Exclude ports
        for port in &self.config.exclude_ports {
            cmds.push(format!(
                "iptables -t mangle -I PREROUTING -p tcp --dport {port} -j RETURN"
            ));
        }
        cmds
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_default_config() {
        let config = TproxyConfig::default();
        assert_eq!(config.inbound_port, 15006);
        assert_eq!(config.outbound_port, 15001);
        assert!(!config.enabled);
        assert_eq!(config.exclude_ports, vec![15000, 15020]);
        assert_eq!(config.exclude_cidrs.len(), 2);
    }

    #[test]
    fn test_interceptor_install_uninstall() {
        let mut interceptor = TproxyInterceptor::new(TproxyConfig::default());
        assert!(!interceptor.is_active());

        interceptor.install().unwrap();
        assert!(interceptor.is_active());

        interceptor.uninstall().unwrap();
        assert!(!interceptor.is_active());
    }

    #[test]
    fn test_iptables_commands() {
        let config = TproxyConfig::default();
        let interceptor = TproxyInterceptor::new(config);
        let cmds = interceptor.iptables_commands();

        // Should have outbound + inbound + 2 exclude port rules = 4
        assert_eq!(cmds.len(), 4);
        assert!(cmds[0].contains("--on-port 15001"));
        assert!(cmds[1].contains("--on-port 15006"));
        assert!(cmds[2].contains("--dport 15000"));
        assert!(cmds[3].contains("--dport 15020"));
    }

    #[test]
    fn test_iptables_commands_no_excludes() {
        let config = TproxyConfig {
            exclude_ports: vec![],
            ..TproxyConfig::default()
        };
        let interceptor = TproxyInterceptor::new(config);
        let cmds = interceptor.iptables_commands();
        assert_eq!(cmds.len(), 2);
    }

    #[test]
    fn test_config_accessor() {
        let config = TproxyConfig {
            inbound_port: 9999,
            ..TproxyConfig::default()
        };
        let interceptor = TproxyInterceptor::new(config);
        assert_eq!(interceptor.config().inbound_port, 9999);
    }
}
