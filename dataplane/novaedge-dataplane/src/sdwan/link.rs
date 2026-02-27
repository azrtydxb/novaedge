use std::net::IpAddr;
use std::time::{Duration, Instant};

/// WAN link state.
#[derive(Debug, Clone, PartialEq)]
pub enum LinkState {
    Active,
    Degraded,
    Down,
}

/// Measured link metrics.
#[derive(Debug, Clone)]
pub struct LinkMetrics {
    pub latency_ms: f64,
    pub jitter_ms: f64,
    pub packet_loss_pct: f64,
    pub bandwidth_bps: u64,
    pub utilization_pct: f64,
    pub last_updated: Instant,
}

impl Default for LinkMetrics {
    fn default() -> Self {
        Self {
            latency_ms: 0.0,
            jitter_ms: 0.0,
            packet_loss_pct: 0.0,
            bandwidth_bps: 0,
            utilization_pct: 0.0,
            last_updated: Instant::now(),
        }
    }
}

/// WAN link configuration and state.
#[derive(Debug, Clone)]
pub struct WANLink {
    pub name: String,
    pub interface: String,
    pub gateway: IpAddr,
    pub probe_targets: Vec<IpAddr>,
    pub probe_interval: Duration,
    pub priority: u32,
    pub weight: u32,
    pub metrics: LinkMetrics,
    pub state: LinkState,
}

impl WANLink {
    pub fn new(name: &str, interface: &str, gateway: IpAddr) -> Self {
        Self {
            name: name.to_string(),
            interface: interface.to_string(),
            gateway,
            probe_targets: vec![],
            probe_interval: Duration::from_secs(5),
            priority: 0,
            weight: 100,
            metrics: LinkMetrics::default(),
            state: LinkState::Active,
        }
    }

    pub fn update_metrics(&mut self, metrics: LinkMetrics) {
        self.metrics = metrics;
        self.state = self.evaluate_state();
    }

    fn evaluate_state(&self) -> LinkState {
        if self.metrics.packet_loss_pct > 50.0 {
            return LinkState::Down;
        }
        if self.metrics.packet_loss_pct > 10.0 || self.metrics.latency_ms > 200.0 {
            return LinkState::Degraded;
        }
        LinkState::Active
    }

    pub fn is_usable(&self) -> bool {
        self.state != LinkState::Down
    }
}

/// Link manager tracking multiple WAN links.
pub struct LinkManager {
    links: Vec<WANLink>,
}

impl LinkManager {
    pub fn new() -> Self {
        Self { links: Vec::new() }
    }

    pub fn add_link(&mut self, link: WANLink) {
        self.links.push(link);
    }

    pub fn remove_link(&mut self, name: &str) {
        self.links.retain(|l| l.name != name);
    }

    pub fn get_link(&self, name: &str) -> Option<&WANLink> {
        self.links.iter().find(|l| l.name == name)
    }

    pub fn get_link_mut(&mut self, name: &str) -> Option<&mut WANLink> {
        self.links.iter_mut().find(|l| l.name == name)
    }

    pub fn active_links(&self) -> Vec<&WANLink> {
        self.links.iter().filter(|l| l.is_usable()).collect()
    }

    pub fn best_link(&self) -> Option<&WANLink> {
        self.active_links().into_iter().min_by(|a, b| {
            a.metrics
                .latency_ms
                .partial_cmp(&b.metrics.latency_ms)
                .unwrap_or(std::cmp::Ordering::Equal)
        })
    }

    pub fn link_count(&self) -> usize {
        self.links.len()
    }
}

impl Default for LinkManager {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::Ipv4Addr;

    fn make_link(name: &str) -> WANLink {
        WANLink::new(name, "eth0", IpAddr::V4(Ipv4Addr::new(10, 0, 0, 1)))
    }

    #[test]
    fn test_new_link() {
        let link = make_link("wan0");
        assert_eq!(link.name, "wan0");
        assert_eq!(link.interface, "eth0");
        assert_eq!(link.state, LinkState::Active);
        assert!(link.is_usable());
        assert_eq!(link.priority, 0);
        assert_eq!(link.weight, 100);
    }

    #[test]
    fn test_evaluate_state_active() {
        let mut link = make_link("wan0");
        link.update_metrics(LinkMetrics {
            latency_ms: 10.0,
            jitter_ms: 2.0,
            packet_loss_pct: 0.1,
            bandwidth_bps: 1_000_000,
            utilization_pct: 30.0,
            last_updated: Instant::now(),
        });
        assert_eq!(link.state, LinkState::Active);
    }

    #[test]
    fn test_evaluate_state_degraded_loss() {
        let mut link = make_link("wan0");
        link.update_metrics(LinkMetrics {
            packet_loss_pct: 15.0,
            ..LinkMetrics::default()
        });
        assert_eq!(link.state, LinkState::Degraded);
    }

    #[test]
    fn test_evaluate_state_degraded_latency() {
        let mut link = make_link("wan0");
        link.update_metrics(LinkMetrics {
            latency_ms: 250.0,
            ..LinkMetrics::default()
        });
        assert_eq!(link.state, LinkState::Degraded);
    }

    #[test]
    fn test_evaluate_state_down() {
        let mut link = make_link("wan0");
        link.update_metrics(LinkMetrics {
            packet_loss_pct: 60.0,
            ..LinkMetrics::default()
        });
        assert_eq!(link.state, LinkState::Down);
        assert!(!link.is_usable());
    }

    #[test]
    fn test_link_manager_add_remove() {
        let mut mgr = LinkManager::new();
        assert_eq!(mgr.link_count(), 0);

        mgr.add_link(make_link("wan0"));
        mgr.add_link(make_link("wan1"));
        assert_eq!(mgr.link_count(), 2);

        mgr.remove_link("wan0");
        assert_eq!(mgr.link_count(), 1);
        assert!(mgr.get_link("wan0").is_none());
        assert!(mgr.get_link("wan1").is_some());
    }

    #[test]
    fn test_link_manager_active_links() {
        let mut mgr = LinkManager::new();
        mgr.add_link(make_link("wan0"));
        let mut down_link = make_link("wan1");
        down_link.update_metrics(LinkMetrics {
            packet_loss_pct: 100.0,
            ..LinkMetrics::default()
        });
        mgr.add_link(down_link);

        let active = mgr.active_links();
        assert_eq!(active.len(), 1);
        assert_eq!(active[0].name, "wan0");
    }

    #[test]
    fn test_link_manager_best_link() {
        let mut mgr = LinkManager::new();

        let mut fast = make_link("fast");
        fast.update_metrics(LinkMetrics {
            latency_ms: 5.0,
            ..LinkMetrics::default()
        });
        let mut slow = make_link("slow");
        slow.update_metrics(LinkMetrics {
            latency_ms: 50.0,
            ..LinkMetrics::default()
        });

        mgr.add_link(slow);
        mgr.add_link(fast);

        let best = mgr.best_link().unwrap();
        assert_eq!(best.name, "fast");
    }

    #[test]
    fn test_link_manager_best_link_none_when_all_down() {
        let mut mgr = LinkManager::new();
        let mut link = make_link("wan0");
        link.update_metrics(LinkMetrics {
            packet_loss_pct: 100.0,
            ..LinkMetrics::default()
        });
        mgr.add_link(link);

        assert!(mgr.best_link().is_none());
    }

    #[test]
    fn test_link_manager_get_link_mut() {
        let mut mgr = LinkManager::new();
        mgr.add_link(make_link("wan0"));

        let link = mgr.get_link_mut("wan0").unwrap();
        link.priority = 10;

        assert_eq!(mgr.get_link("wan0").unwrap().priority, 10);
    }

    #[test]
    fn test_link_manager_remove_nonexistent() {
        let mut mgr = LinkManager::new();
        mgr.add_link(make_link("wan0"));
        mgr.remove_link("nonexistent");
        assert_eq!(mgr.link_count(), 1);
    }
}
