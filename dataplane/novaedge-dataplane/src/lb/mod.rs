//! Load balancing algorithms for backend selection.
//!
//! Provides a [`LoadBalancer`] trait and multiple algorithm implementations
//! including round-robin, random, source-hash, maglev, least-connections,
//! power-of-two-choices, EWMA, sticky sessions, and consistent ring-hash.

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
pub mod source_hash;
pub mod sticky;

/// Backend endpoint for load balancer selection.
#[derive(Debug, Clone)]
pub struct Backend {
    pub addr: IpAddr,
    pub port: u16,
    pub weight: u16,
    pub healthy: bool,
    pub zone: Option<String>,
    /// Priority group for failover (0 = highest). Used by priority-based LB.
    #[allow(dead_code)]
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
pub fn prefer_same_zone<'a>(
    ctx: &RequestContext,
    backends: &'a [Backend],
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
