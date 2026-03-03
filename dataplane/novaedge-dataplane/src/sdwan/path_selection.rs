use super::link::WANLink;

/// SLA requirements for a traffic class.
#[derive(Debug, Clone)]
pub struct SLARequirements {
    pub max_latency_ms: f64,
    pub max_jitter_ms: f64,
    pub max_packet_loss_pct: f64,
    pub min_bandwidth_bps: u64,
}

impl Default for SLARequirements {
    fn default() -> Self {
        Self {
            max_latency_ms: 100.0,
            max_jitter_ms: 30.0,
            max_packet_loss_pct: 1.0,
            min_bandwidth_bps: 1_000_000, // 1 Mbps
        }
    }
}

/// Path selection strategy.
#[derive(Debug, Clone, PartialEq)]
pub enum PathStrategy {
    /// Use best link meeting SLA
    Performance,
    /// Use cheapest link meeting SLA
    #[allow(dead_code)] // Valid strategy selected by WAN policy configuration
    Cost,
    /// Load balance across links meeting SLA
    #[allow(dead_code)] // Valid strategy selected by WAN policy configuration
    LoadBalance,
    /// Failover: primary -> secondary
    #[allow(dead_code)] // Valid strategy selected by WAN policy configuration
    Failover,
}

/// Traffic match criteria for SLA policy.
#[derive(Debug, Clone)]
pub struct TrafficMatch {
    #[allow(dead_code)] // Used by traffic classification before path selection
    pub destination_cidrs: Vec<String>,
    #[allow(dead_code)] // Used by traffic classification before path selection
    pub dscp_values: Vec<u8>,
    #[allow(dead_code)] // Used by traffic classification before path selection
    pub application: Option<String>,
}

/// SLA-based WAN policy.
#[derive(Debug, Clone)]
pub struct WANPolicy {
    #[allow(dead_code)] // Policy identifier for logging and lookup
    pub name: String,
    #[allow(dead_code)] // Traffic matching criteria applied before path selection
    pub match_criteria: TrafficMatch,
    pub sla: SLARequirements,
    pub strategy: PathStrategy,
    pub preferred_links: Vec<String>,
}

/// Path selector evaluates links against SLA policies.
pub struct PathSelector;

impl PathSelector {
    /// Check if a link meets SLA requirements.
    pub fn meets_sla(link: &WANLink, sla: &SLARequirements) -> bool {
        link.is_usable()
            && link.metrics.latency_ms <= sla.max_latency_ms
            && link.metrics.jitter_ms <= sla.max_jitter_ms
            && link.metrics.packet_loss_pct <= sla.max_packet_loss_pct
            && link.metrics.bandwidth_bps >= sla.min_bandwidth_bps
    }

    /// Select the best link for a policy from available links.
    pub fn select<'a>(links: &'a [WANLink], policy: &WANPolicy) -> Option<&'a WANLink> {
        let eligible: Vec<&WANLink> = links
            .iter()
            .filter(|l| Self::meets_sla(l, &policy.sla))
            .collect();

        if eligible.is_empty() {
            // Fallback: return any usable link (SLA not met)
            return links.iter().find(|l| l.is_usable());
        }

        // Check preferred links first
        for pref in &policy.preferred_links {
            if let Some(link) = eligible.iter().find(|l| l.name == *pref) {
                return Some(link);
            }
        }

        match policy.strategy {
            PathStrategy::Performance => eligible.into_iter().min_by(|a, b| {
                a.metrics
                    .latency_ms
                    .partial_cmp(&b.metrics.latency_ms)
                    .unwrap_or(std::cmp::Ordering::Equal)
            }),
            PathStrategy::Cost => {
                // Lower priority = cheaper in our model
                eligible.into_iter().max_by_key(|l| l.priority)
            }
            PathStrategy::LoadBalance => {
                // Select least utilized
                eligible.into_iter().min_by(|a, b| {
                    a.metrics
                        .utilization_pct
                        .partial_cmp(&b.metrics.utilization_pct)
                        .unwrap_or(std::cmp::Ordering::Equal)
                })
            }
            PathStrategy::Failover => {
                // Select highest priority
                eligible.into_iter().min_by_key(|l| l.priority)
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::super::link::{LinkMetrics, LinkState};
    use super::*;
    use std::net::{IpAddr, Ipv4Addr};
    use std::time::Instant;

    fn make_link_with_metrics(
        name: &str,
        latency_ms: f64,
        loss_pct: f64,
        bandwidth_bps: u64,
        utilization_pct: f64,
        priority: u32,
    ) -> WANLink {
        let mut link = WANLink::new(name, "eth0", IpAddr::V4(Ipv4Addr::new(10, 0, 0, 1)));
        link.priority = priority;
        link.metrics = LinkMetrics {
            latency_ms,
            jitter_ms: 1.0,
            packet_loss_pct: loss_pct,
            bandwidth_bps,
            utilization_pct,
            last_updated: Instant::now(),
        };
        // Re-evaluate state based on metrics
        if loss_pct > 50.0 {
            link.state = LinkState::Down;
        } else if loss_pct > 10.0 || latency_ms > 200.0 {
            link.state = LinkState::Degraded;
        } else {
            link.state = LinkState::Active;
        }
        link
    }

    fn make_policy(strategy: PathStrategy) -> WANPolicy {
        WANPolicy {
            name: "test-policy".into(),
            match_criteria: TrafficMatch {
                destination_cidrs: vec!["0.0.0.0/0".into()],
                dscp_values: vec![],
                application: None,
            },
            sla: SLARequirements::default(),
            strategy,
            preferred_links: vec![],
        }
    }

    #[test]
    fn test_default_sla() {
        let sla = SLARequirements::default();
        assert!((sla.max_latency_ms - 100.0).abs() < f64::EPSILON);
        assert!((sla.max_jitter_ms - 30.0).abs() < f64::EPSILON);
        assert!((sla.max_packet_loss_pct - 1.0).abs() < f64::EPSILON);
        assert_eq!(sla.min_bandwidth_bps, 1_000_000);
    }

    #[test]
    fn test_meets_sla_good_link() {
        let link = make_link_with_metrics("wan0", 10.0, 0.1, 10_000_000, 30.0, 0);
        let sla = SLARequirements::default();
        assert!(PathSelector::meets_sla(&link, &sla));
    }

    #[test]
    fn test_meets_sla_high_latency() {
        let link = make_link_with_metrics("wan0", 150.0, 0.1, 10_000_000, 30.0, 0);
        let sla = SLARequirements::default();
        assert!(!PathSelector::meets_sla(&link, &sla));
    }

    #[test]
    fn test_meets_sla_high_loss() {
        let link = make_link_with_metrics("wan0", 10.0, 5.0, 10_000_000, 30.0, 0);
        let sla = SLARequirements::default();
        assert!(!PathSelector::meets_sla(&link, &sla));
    }

    #[test]
    fn test_meets_sla_low_bandwidth() {
        let link = make_link_with_metrics("wan0", 10.0, 0.1, 500_000, 30.0, 0);
        let sla = SLARequirements::default();
        assert!(!PathSelector::meets_sla(&link, &sla));
    }

    #[test]
    fn test_meets_sla_down_link() {
        let link = make_link_with_metrics("wan0", 10.0, 80.0, 10_000_000, 30.0, 0);
        let sla = SLARequirements::default();
        assert!(!PathSelector::meets_sla(&link, &sla));
    }

    #[test]
    fn test_select_performance() {
        let links = vec![
            make_link_with_metrics("slow", 80.0, 0.1, 10_000_000, 30.0, 0),
            make_link_with_metrics("fast", 5.0, 0.1, 10_000_000, 30.0, 0),
            make_link_with_metrics("medium", 40.0, 0.1, 10_000_000, 30.0, 0),
        ];
        let policy = make_policy(PathStrategy::Performance);
        let selected = PathSelector::select(&links, &policy).unwrap();
        assert_eq!(selected.name, "fast");
    }

    #[test]
    fn test_select_cost() {
        let links = vec![
            make_link_with_metrics("expensive", 10.0, 0.1, 10_000_000, 30.0, 0),
            make_link_with_metrics("cheap", 10.0, 0.1, 10_000_000, 30.0, 10),
            make_link_with_metrics("mid", 10.0, 0.1, 10_000_000, 30.0, 5),
        ];
        let policy = make_policy(PathStrategy::Cost);
        let selected = PathSelector::select(&links, &policy).unwrap();
        assert_eq!(selected.name, "cheap"); // highest priority value = cheapest
    }

    #[test]
    fn test_select_load_balance() {
        let links = vec![
            make_link_with_metrics("busy", 10.0, 0.1, 10_000_000, 80.0, 0),
            make_link_with_metrics("idle", 10.0, 0.1, 10_000_000, 10.0, 0),
            make_link_with_metrics("medium", 10.0, 0.1, 10_000_000, 50.0, 0),
        ];
        let policy = make_policy(PathStrategy::LoadBalance);
        let selected = PathSelector::select(&links, &policy).unwrap();
        assert_eq!(selected.name, "idle");
    }

    #[test]
    fn test_select_failover() {
        let links = vec![
            make_link_with_metrics("secondary", 10.0, 0.1, 10_000_000, 30.0, 10),
            make_link_with_metrics("primary", 10.0, 0.1, 10_000_000, 30.0, 0),
        ];
        let policy = make_policy(PathStrategy::Failover);
        let selected = PathSelector::select(&links, &policy).unwrap();
        assert_eq!(selected.name, "primary"); // lowest priority value = highest priority
    }

    #[test]
    fn test_select_preferred_link() {
        let links = vec![
            make_link_with_metrics("wan0", 10.0, 0.1, 10_000_000, 30.0, 0),
            make_link_with_metrics("wan1", 5.0, 0.1, 10_000_000, 30.0, 0),
        ];
        let mut policy = make_policy(PathStrategy::Performance);
        policy.preferred_links = vec!["wan0".into()];

        // Even though wan1 has lower latency, wan0 is preferred
        let selected = PathSelector::select(&links, &policy).unwrap();
        assert_eq!(selected.name, "wan0");
    }

    #[test]
    fn test_select_fallback_when_no_sla_met() {
        let links = vec![
            make_link_with_metrics("degraded", 150.0, 5.0, 500_000, 90.0, 0),
            make_link_with_metrics("down", 10.0, 80.0, 10_000_000, 30.0, 0),
        ];
        let policy = make_policy(PathStrategy::Performance);

        // No link meets SLA, but degraded is still usable
        let selected = PathSelector::select(&links, &policy).unwrap();
        assert_eq!(selected.name, "degraded");
    }

    #[test]
    fn test_select_no_usable_links() {
        let links = vec![make_link_with_metrics(
            "down", 10.0, 80.0, 10_000_000, 30.0, 0,
        )];
        let policy = make_policy(PathStrategy::Performance);
        assert!(PathSelector::select(&links, &policy).is_none());
    }

    #[test]
    fn test_select_empty_links() {
        let links: Vec<WANLink> = vec![];
        let policy = make_policy(PathStrategy::Performance);
        assert!(PathSelector::select(&links, &policy).is_none());
    }
}
