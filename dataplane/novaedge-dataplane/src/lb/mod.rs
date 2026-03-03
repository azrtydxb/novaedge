//! Load balancing algorithms for backend selection.
//!
//! Provides a [`LoadBalancer`] trait and multiple algorithm implementations
//! including round-robin, random, source-hash, maglev, least-connections,
//! power-of-two-choices, EWMA, sticky sessions, and consistent ring-hash.

use std::collections::HashMap;
use std::net::IpAddr;
use std::sync::Arc;
use std::time::Duration;

pub mod ewma;
pub mod least_conn;
pub mod maglev;
pub mod p2c;
pub mod random;
pub mod ring_hash;
pub mod round_robin;
pub mod slow_start;
pub mod source_hash;
pub mod sticky;
pub mod subsetting;

/// Backend endpoint for load balancer selection.
#[derive(Debug, Clone)]
pub struct Backend {
    pub addr: IpAddr,
    pub port: u16,
    pub weight: u16,
    pub healthy: bool,
    pub zone: Option<String>,
    /// Priority group for failover (0 = highest). Used by priority-based LB.
    pub priority: u32,
}

/// Request context for LB selection.
#[derive(Debug)]
pub struct RequestContext {
    pub src_ip: IpAddr,
    pub src_port: u16,
    pub dst_port: u16,
    pub sticky_cookie: Option<String>,
    pub zone: Option<String>,
}

/// Core load balancer trait.
pub trait LoadBalancer: Send + Sync {
    /// Select a backend index from the given slice.
    fn select(&self, ctx: &RequestContext, backends: &[Backend]) -> Option<usize>;

    /// Report the outcome of a request to the selected backend.
    fn report(&self, _backend_idx: usize, _latency: Duration, _success: bool) {}

    /// Human-readable algorithm name.
    fn name(&self) -> &'static str;
}

/// Create a load balancer by algorithm name.
pub fn new_load_balancer(algo: &str) -> Arc<dyn LoadBalancer> {
    match algo {
        "round-robin" | "" => Arc::new(round_robin::RoundRobin::new()),
        "random" => Arc::new(random::Random),
        "source-hash" => Arc::new(source_hash::SourceHash),
        "maglev" => Arc::new(maglev::MaglevLb::new()),
        "least-conn" => Arc::new(least_conn::LeastConn::new()),
        "p2c" => Arc::new(p2c::PowerOfTwoChoices::new()),
        "ewma" => Arc::new(ewma::Ewma::new()),
        "sticky" => Arc::new(sticky::StickySession::new()),
        "ring-hash" => Arc::new(ring_hash::RingHash::new(150)),
        _ => Arc::new(round_robin::RoundRobin::new()), // fallback
    }
}

/// Collect the indices of healthy backends, repeating by weight.
fn healthy_weighted(backends: &[Backend]) -> Vec<usize> {
    let mut out = Vec::new();
    for (i, b) in backends.iter().enumerate() {
        if b.healthy {
            let w = b.weight.max(1) as usize;
            for _ in 0..w {
                out.push(i);
            }
        }
    }
    out
}

/// Collect the indices of healthy backends (no weight expansion).
fn healthy_indices(backends: &[Backend]) -> Vec<usize> {
    backends
        .iter()
        .enumerate()
        .filter(|(_, b)| b.healthy)
        .map(|(i, _)| i)
        .collect()
}

/// Filter healthy backends to prefer those in the same zone as the request.
///
/// If the request has a zone set and there are healthy backends in that zone,
/// returns only those backends. Otherwise falls back to all healthy backends.
pub fn prefer_same_zone(
    ctx: &RequestContext,
    backends: &[Backend],
    healthy: &[usize],
) -> Vec<usize> {
    if let Some(ref req_zone) = ctx.zone {
        let same_zone: Vec<usize> = healthy
            .iter()
            .copied()
            .filter(|&i| backends[i].zone.as_deref() == Some(req_zone.as_str()))
            .collect();
        if !same_zone.is_empty() {
            return same_zone;
        }
    }
    healthy.to_vec()
}

/// Locality-weighted backend selection.
///
/// Weights traffic proportionally based on per-zone health.
/// The local zone gets `local_healthy / cluster_healthy` share of traffic.
/// Returns indices repeated proportionally by their zone's share.
///
/// For example, if 80% of the local zone's backends are healthy but only 50%
/// of the remote zone's backends are healthy, the local zone gets proportionally
/// more traffic (80/(80+50) ≈ 62%).
pub fn locality_weighted(
    ctx: &RequestContext,
    backends: &[Backend],
    healthy: &[usize],
) -> Vec<usize> {
    let req_zone = match ctx.zone {
        Some(ref z) => z.as_str(),
        None => return healthy.to_vec(),
    };

    if healthy.is_empty() {
        return Vec::new();
    }

    // Compute per-zone stats: (total, healthy_count).
    let mut zone_stats: HashMap<&str, (u32, u32)> = HashMap::new();
    for &i in healthy {
        let zone = backends[i].zone.as_deref().unwrap_or("unknown");
        zone_stats.entry(zone).or_insert((0, 0)).1 += 1;
    }
    for b in backends.iter() {
        let zone = b.zone.as_deref().unwrap_or("unknown");
        zone_stats.entry(zone).or_insert((0, 0)).0 += 1;
    }

    // Compute health ratios per zone.
    let mut zone_weights: Vec<(&str, f64)> = Vec::new();
    for (&zone, &(total, healthy_count)) in &zone_stats {
        if total == 0 || healthy_count == 0 {
            continue;
        }
        let ratio = healthy_count as f64 / total as f64;
        zone_weights.push((zone, ratio));
    }

    if zone_weights.is_empty() {
        return healthy.to_vec();
    }

    // Normalize weights to sum to 1.0.
    let total_weight: f64 = zone_weights.iter().map(|(_, w)| w).sum();
    if total_weight <= 0.0 {
        return healthy.to_vec();
    }

    // Build result: repeat each zone's healthy backends proportionally.
    // Use weight * 100 as repeat count to get proportional distribution.
    let mut result = Vec::new();
    for &(zone, weight) in &zone_weights {
        let normalized = weight / total_weight;
        // Local zone gets a 1.5x boost to prefer local traffic.
        let boosted = if zone == req_zone {
            (normalized * 1.5).min(1.0)
        } else {
            normalized
        };
        let repeats = (boosted * 100.0).round() as usize;
        let zone_backends: Vec<usize> = healthy
            .iter()
            .copied()
            .filter(|&i| backends[i].zone.as_deref().unwrap_or("unknown") == zone)
            .collect();
        for _ in 0..repeats.max(1) {
            result.extend_from_slice(&zone_backends);
        }
    }

    if result.is_empty() {
        healthy.to_vec()
    } else {
        result
    }
}

/// Filter backends to those in the lowest healthy priority group.
///
/// Priority groups are numbered from 0 (highest). Returns indices of healthy
/// backends in the lowest-numbered group. If that group is empty, falls back
/// to the next group. Returns empty vec if no healthy backends exist.
pub fn filter_by_priority(backends: &[Backend]) -> Vec<usize> {
    let mut priorities: Vec<u32> = backends
        .iter()
        .filter(|b| b.healthy)
        .map(|b| b.priority)
        .collect();
    priorities.sort_unstable();
    priorities.dedup();

    for priority in priorities {
        let indices: Vec<usize> = backends
            .iter()
            .enumerate()
            .filter(|(_, b)| b.healthy && b.priority == priority)
            .map(|(i, _)| i)
            .collect();
        if !indices.is_empty() {
            return indices;
        }
    }
    Vec::new()
}

/// Expand a set of backend indices by weight.
///
/// Each index `i` appears `backends[i].weight` times (minimum 1).
fn weighted_expand(indices: &[usize], backends: &[Backend]) -> Vec<usize> {
    let mut out = Vec::new();
    for &i in indices {
        let w = backends[i].weight.max(1) as usize;
        for _ in 0..w {
            out.push(i);
        }
    }
    out
}

#[cfg(test)]
pub(crate) mod test_helpers {
    use super::*;
    use std::net::Ipv4Addr;

    /// Create N healthy backends with weight 1.
    pub fn make_backends(n: usize) -> Vec<Backend> {
        (0..n)
            .map(|i| Backend {
                addr: IpAddr::V4(Ipv4Addr::new(10, 0, 0, (i + 1) as u8)),
                port: 8080,
                weight: 1,
                healthy: true,
                zone: None,
                priority: 0,
            })
            .collect()
    }

    /// Create a default request context.
    pub fn make_ctx() -> RequestContext {
        RequestContext {
            src_ip: IpAddr::V4(Ipv4Addr::new(192, 168, 1, 1)),
            src_port: 12345,
            dst_port: 80,
            sticky_cookie: None,
            zone: None,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::{IpAddr, Ipv4Addr};

    fn backend(ip_last: u8, priority: u32, healthy: bool, weight: u16) -> Backend {
        Backend {
            addr: IpAddr::V4(Ipv4Addr::new(10, 0, 0, ip_last)),
            port: 8080,
            weight,
            healthy,
            zone: None,
            priority,
        }
    }

    #[test]
    fn filter_by_priority_selects_lowest() {
        let backends = vec![
            backend(1, 1, true, 1),
            backend(2, 0, true, 1),
            backend(3, 2, true, 1),
            backend(4, 0, true, 1),
        ];
        let result = filter_by_priority(&backends);
        assert_eq!(result, vec![1, 3]); // priority 0 backends
    }

    #[test]
    fn filter_by_priority_skips_unhealthy() {
        let backends = vec![
            backend(1, 0, false, 1), // priority 0 but unhealthy
            backend(2, 1, true, 1),
            backend(3, 1, true, 1),
        ];
        let result = filter_by_priority(&backends);
        assert_eq!(result, vec![1, 2]); // falls back to priority 1
    }

    #[test]
    fn filter_by_priority_empty_when_all_unhealthy() {
        let backends = vec![backend(1, 0, false, 1), backend(2, 1, false, 1)];
        let result = filter_by_priority(&backends);
        assert!(result.is_empty());
    }

    #[test]
    fn weighted_expand_repeats_by_weight() {
        let backends = vec![
            backend(1, 0, true, 3),
            backend(2, 0, true, 1),
            backend(3, 0, true, 2),
        ];
        let indices = vec![0, 1, 2];
        let expanded = weighted_expand(&indices, &backends);
        assert_eq!(expanded, vec![0, 0, 0, 1, 2, 2]);
    }

    #[test]
    fn prefer_same_zone_filters_correctly() {
        let backends = vec![
            Backend {
                addr: IpAddr::V4(Ipv4Addr::new(10, 0, 0, 1)),
                port: 8080,
                weight: 1,
                healthy: true,
                zone: Some("us-east-1a".to_string()),
                priority: 0,
            },
            Backend {
                addr: IpAddr::V4(Ipv4Addr::new(10, 0, 0, 2)),
                port: 8080,
                weight: 1,
                healthy: true,
                zone: Some("us-east-1b".to_string()),
                priority: 0,
            },
            Backend {
                addr: IpAddr::V4(Ipv4Addr::new(10, 0, 0, 3)),
                port: 8080,
                weight: 1,
                healthy: true,
                zone: Some("us-east-1a".to_string()),
                priority: 0,
            },
        ];
        let healthy = vec![0, 1, 2];
        let ctx = RequestContext {
            src_ip: IpAddr::V4(Ipv4Addr::new(192, 168, 1, 1)),
            src_port: 12345,
            dst_port: 80,
            sticky_cookie: None,
            zone: Some("us-east-1a".to_string()),
        };
        let result = prefer_same_zone(&ctx, &backends, &healthy);
        assert_eq!(result, vec![0, 2]);
    }

    #[test]
    fn prefer_same_zone_falls_back_when_no_match() {
        let backends = vec![Backend {
            addr: IpAddr::V4(Ipv4Addr::new(10, 0, 0, 1)),
            port: 8080,
            weight: 1,
            healthy: true,
            zone: Some("us-west-2a".to_string()),
            priority: 0,
        }];
        let healthy = vec![0];
        let ctx = RequestContext {
            src_ip: IpAddr::V4(Ipv4Addr::new(192, 168, 1, 1)),
            src_port: 12345,
            dst_port: 80,
            sticky_cookie: None,
            zone: Some("us-east-1a".to_string()),
        };
        let result = prefer_same_zone(&ctx, &backends, &healthy);
        assert_eq!(result, vec![0]); // falls back to all healthy
    }

    #[test]
    fn locality_weighted_boosts_local_zone() {
        let backends = vec![
            Backend {
                addr: IpAddr::V4(Ipv4Addr::new(10, 0, 0, 1)),
                port: 8080,
                weight: 1,
                healthy: true,
                zone: Some("us-east-1a".to_string()),
                priority: 0,
            },
            Backend {
                addr: IpAddr::V4(Ipv4Addr::new(10, 0, 0, 2)),
                port: 8080,
                weight: 1,
                healthy: true,
                zone: Some("us-east-1b".to_string()),
                priority: 0,
            },
            Backend {
                addr: IpAddr::V4(Ipv4Addr::new(10, 0, 0, 3)),
                port: 8080,
                weight: 1,
                healthy: true,
                zone: Some("us-east-1a".to_string()),
                priority: 0,
            },
        ];
        let healthy = vec![0, 1, 2];
        let ctx = RequestContext {
            src_ip: IpAddr::V4(Ipv4Addr::new(192, 168, 1, 1)),
            src_port: 12345,
            dst_port: 80,
            sticky_cookie: None,
            zone: Some("us-east-1a".to_string()),
        };
        let result = locality_weighted(&ctx, &backends, &healthy);
        // Local zone (us-east-1a) should appear more than remote (us-east-1b).
        let local_count = result.iter().filter(|&&i| i == 0 || i == 2).count();
        let remote_count = result.iter().filter(|&&i| i == 1).count();
        assert!(
            local_count > remote_count,
            "local_count={local_count}, remote_count={remote_count}"
        );
    }

    #[test]
    fn locality_weighted_no_zone_returns_all() {
        let backends = vec![Backend {
            addr: IpAddr::V4(Ipv4Addr::new(10, 0, 0, 1)),
            port: 8080,
            weight: 1,
            healthy: true,
            zone: Some("us-east-1a".to_string()),
            priority: 0,
        }];
        let healthy = vec![0];
        let ctx = RequestContext {
            src_ip: IpAddr::V4(Ipv4Addr::new(192, 168, 1, 1)),
            src_port: 12345,
            dst_port: 80,
            sticky_cookie: None,
            zone: None,
        };
        let result = locality_weighted(&ctx, &backends, &healthy);
        assert_eq!(result, vec![0]);
    }
}
