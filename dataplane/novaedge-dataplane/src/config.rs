//! Runtime configuration state for the dataplane.
//!
//! Stores gateways, routes, and clusters received via gRPC. Written by
//! gRPC handlers, read by proxy handlers and the listener manager.

use dashmap::DashMap;
use std::collections::{HashMap, HashSet};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use tokio::sync::{watch, RwLock};

use crate::lb::{self, LoadBalancer};

/// Gateway listener state.
#[derive(Debug, Clone)]
pub struct GatewayState {
    pub name: String,
    pub bind_address: String,
    pub port: u32,
    pub protocol: String,
    pub tls: Option<TlsState>,
    pub hostnames: Vec<String>,
}

/// TLS configuration for a gateway.
#[derive(Debug, Clone)]
pub struct TlsState {
    pub cert_pem: Vec<u8>,
    pub key_pem: Vec<u8>,
    /// Optional CA certificate for mTLS client verification.
    /// When present, the listener requires clients to present a certificate
    /// signed by this CA.
    pub client_ca_pem: Option<Vec<u8>>,
}

/// Route state mapping requests to backends.
#[derive(Debug, Clone)]
pub struct RouteState {
    pub name: String,
    pub gateway_ref: String,
    pub hostnames: Vec<String>,
    pub path_prefix: String,
    pub path_exact: String,
    pub path_regex: String,
    pub methods: Vec<String>,
    /// Backend references with weights: `(cluster_name, weight)`.
    pub backend_refs: Vec<(String, u32)>,
    pub priority: i32,
    pub rewrite_path: Option<String>,
    pub add_headers: HashMap<String, String>,
    pub middleware_refs: Vec<String>,
}

/// Policy state for rate-limiting, auth, CORS, etc.
#[derive(Debug, Clone)]
pub struct PolicyState {
    pub name: String,
    pub policy_type: String,
    pub target_ref: String,
    pub config_json: String,
}

/// Backend cluster state with endpoints.
#[derive(Debug, Clone)]
pub struct ClusterState {
    pub name: String,
    pub endpoints: Vec<EndpointState>,
    pub lb_algorithm: String,
    pub health_check_path: String,
    /// Session affinity type: "cookie", "header", "source_ip", or empty.
    pub session_affinity_type: String,
    /// Cookie name for cookie-based session affinity.
    pub session_affinity_cookie: String,
    /// Consecutive 5xx threshold for outlier detection (0 = use default).
    #[allow(dead_code)]
    pub outlier_consecutive_5xx: u32,
    /// Base ejection duration in ms for outlier detection (0 = use default).
    #[allow(dead_code)]
    pub outlier_ejection_duration_ms: u64,
    /// Maximum ejection percentage for outlier detection (0 = use default).
    #[allow(dead_code)]
    pub outlier_max_ejection_pct: u32,
    /// Slow start window in ms (0 = disabled).
    #[allow(dead_code)]
    pub slow_start_window_ms: u64,
}

/// A single backend endpoint.
#[derive(Debug, Clone)]
pub struct EndpointState {
    pub address: String,
    pub port: u32,
    pub weight: u32,
    pub healthy: bool,
    /// Locality zone for zone-aware routing.
    pub zone: Option<String>,
    /// Priority group for priority-based failover.
    pub priority: u32,
}

/// Snapshot of all runtime configuration at a point in time.
#[derive(Debug, Clone)]
pub struct ConfigSnapshot {
    pub gateways: HashMap<String, GatewayState>,
    pub routes: HashMap<String, RouteState>,
    pub clusters: HashMap<String, ClusterState>,
    pub policies: HashMap<String, PolicyState>,
    pub version: String,
}

/// Thread-safe runtime configuration store.
///
/// Written by gRPC handlers, read by proxy and listener tasks.
/// Uses `DashMap` for lock-free concurrent access and a `watch` channel
/// to notify listeners of configuration changes.
pub struct RuntimeConfig {
    gateways: DashMap<String, GatewayState>,
    routes: DashMap<String, RouteState>,
    clusters: DashMap<String, ClusterState>,
    policies: DashMap<String, PolicyState>,
    version: RwLock<String>,
    notify: watch::Sender<u64>,
    notify_rx: watch::Receiver<u64>,
    generation: AtomicU64,
    /// Cached load balancer instances keyed by cluster name.
    /// Cleared on config updates so that endpoint changes trigger LB rebuild.
    lb_cache: RwLock<HashMap<String, Arc<dyn LoadBalancer>>>,
}

impl RuntimeConfig {
    /// Create a new empty runtime configuration.
    pub fn new() -> Self {
        let (notify, notify_rx) = watch::channel(0u64);
        Self {
            gateways: DashMap::new(),
            routes: DashMap::new(),
            clusters: DashMap::new(),
            policies: DashMap::new(),
            version: RwLock::new(String::new()),
            notify,
            notify_rx,
            generation: AtomicU64::new(0),
            lb_cache: RwLock::new(HashMap::new()),
        }
    }

    /// Insert or update a gateway.
    pub fn upsert_gateway(&self, gw: GatewayState) {
        self.gateways.insert(gw.name.clone(), gw);
        self.bump();
    }

    /// Remove a gateway by name.
    pub fn delete_gateway(&self, name: &str) {
        self.gateways.remove(name);
        self.bump();
    }

    /// Insert or update a route.
    pub fn upsert_route(&self, route: RouteState) {
        self.routes.insert(route.name.clone(), route);
        self.bump();
    }

    /// Remove a route by name.
    pub fn delete_route(&self, name: &str) {
        self.routes.remove(name);
        self.bump();
    }

    /// Insert or update a cluster.
    ///
    /// Also invalidates the cached LB instance for this cluster so that
    /// endpoint or algorithm changes take effect on the next request.
    pub fn upsert_cluster(&self, cluster: ClusterState) {
        let name = cluster.name.clone();
        self.clusters.insert(name.clone(), cluster);
        // Invalidate the LB cache entry for this cluster (best-effort; uses try_write
        // to avoid blocking the caller if the lock is held by an async task).
        if let Ok(mut cache) = self.lb_cache.try_write() {
            cache.remove(&name);
        }
        self.bump();
    }

    /// Remove a cluster by name.
    pub fn delete_cluster(&self, name: &str) {
        self.clusters.remove(name);
        if let Ok(mut cache) = self.lb_cache.try_write() {
            cache.remove(name);
        }
        self.bump();
    }

    /// Get a cluster by name.
    pub fn get_cluster(&self, name: &str) -> Option<ClusterState> {
        self.clusters.get(name).map(|c| c.value().clone())
    }

    /// Insert or update a policy.
    pub fn upsert_policy(&self, policy: PolicyState) {
        self.policies.insert(policy.name.clone(), policy);
        self.bump();
    }

    /// Remove a policy by name.
    pub fn delete_policy(&self, name: &str) {
        self.policies.remove(name);
        self.bump();
    }

    /// Get a policy by name.
    pub fn get_policy(&self, name: &str) -> Option<PolicyState> {
        self.policies.get(name).map(|p| p.value().clone())
    }

    /// Get or create a cached load balancer for the given cluster and algorithm.
    ///
    /// Reuses an existing instance if one is cached, otherwise creates a new
    /// one via [`lb::new_load_balancer`] and caches it. This ensures stateful
    /// algorithms (RoundRobin counters, EWMA latency history, etc.) persist
    /// across requests.
    pub async fn get_or_create_lb(&self, cluster_name: &str, algo: &str) -> Arc<dyn LoadBalancer> {
        // Fast path: check read lock.
        {
            let cache = self.lb_cache.read().await;
            if let Some(balancer) = cache.get(cluster_name) {
                return balancer.clone();
            }
        }
        // Slow path: acquire write lock and insert.
        let mut cache = self.lb_cache.write().await;
        cache
            .entry(cluster_name.to_string())
            .or_insert_with(|| lb::new_load_balancer(algo))
            .clone()
    }

    /// Clear the load balancer cache.
    ///
    /// Called after config updates so that endpoint/algorithm changes cause
    /// fresh LB instances to be created.
    #[allow(dead_code)]
    pub async fn clear_lb_cache(&self) {
        self.lb_cache.write().await.clear();
    }

    /// Subscribe to configuration change notifications.
    pub fn subscribe(&self) -> watch::Receiver<u64> {
        self.notify_rx.clone()
    }

    /// Take a point-in-time snapshot of all configuration.
    pub fn snapshot(&self) -> ConfigSnapshot {
        ConfigSnapshot {
            gateways: self
                .gateways
                .iter()
                .map(|e| (e.key().clone(), e.value().clone()))
                .collect(),
            routes: self
                .routes
                .iter()
                .map(|e| (e.key().clone(), e.value().clone()))
                .collect(),
            clusters: self
                .clusters
                .iter()
                .map(|e| (e.key().clone(), e.value().clone()))
                .collect(),
            policies: self
                .policies
                .iter()
                .map(|e| (e.key().clone(), e.value().clone()))
                .collect(),
            version: self
                .version
                .try_read()
                .map(|v| v.clone())
                .unwrap_or_default(),
        }
    }

    /// Apply a full configuration snapshot, replacing all existing state.
    ///
    /// Uses `retain()` + insert instead of `clear()` + insert to avoid a race
    /// condition where concurrent readers (e.g. `ListenerManager::reconcile_listeners`)
    /// could observe an empty config between the clear and first insert.
    pub async fn apply_full(
        &self,
        version: String,
        gateways: Vec<GatewayState>,
        routes: Vec<RouteState>,
        clusters: Vec<ClusterState>,
        policies: Vec<PolicyState>,
    ) {
        // Build new key sets
        let gw_keys: HashSet<String> = gateways.iter().map(|g| g.name.clone()).collect();
        let rt_keys: HashSet<String> = routes.iter().map(|r| r.name.clone()).collect();
        let cl_keys: HashSet<String> = clusters.iter().map(|c| c.name.clone()).collect();
        let pl_keys: HashSet<String> = policies.iter().map(|p| p.name.clone()).collect();

        // Remove stale entries (never empties the map if new entries exist)
        self.gateways.retain(|k, _| gw_keys.contains(k));
        self.routes.retain(|k, _| rt_keys.contains(k));
        self.clusters.retain(|k, _| cl_keys.contains(k));
        self.policies.retain(|k, _| pl_keys.contains(k));

        // Insert/update entries
        for gw in gateways {
            self.gateways.insert(gw.name.clone(), gw);
        }
        for r in routes {
            self.routes.insert(r.name.clone(), r);
        }
        for c in clusters {
            self.clusters.insert(c.name.clone(), c);
        }
        for p in policies {
            self.policies.insert(p.name.clone(), p);
        }

        *self.version.write().await = version;
        // Clear LB cache so stateful balancers pick up new endpoints/algorithms.
        self.lb_cache.write().await.clear();
        self.bump();
    }

    /// Find a backend endpoint address for an L4 gateway by name.
    ///
    /// Iterates only the routes DashMap to find the first route referencing
    /// this gateway, then looks up a single cluster—avoiding a full
    /// config snapshot clone on every L4 connection.
    pub fn resolve_l4_backend(&self, gateway_name: &str) -> Option<std::net::SocketAddr> {
        let backend_ref = self
            .routes
            .iter()
            .find(|r| r.gateway_ref == gateway_name)
            .and_then(|r| r.backend_refs.first().map(|(name, _)| name.clone()))?;
        let cluster = self.clusters.get(&backend_ref)?;
        let ep = cluster.endpoints.first()?;
        format!("{}:{}", ep.address, ep.port)
            .parse::<std::net::SocketAddr>()
            .ok()
    }

    /// Get the current config generation number.
    pub fn generation(&self) -> u64 {
        self.generation.load(Ordering::Relaxed)
    }

    fn bump(&self) {
        let gen = self.generation.fetch_add(1, Ordering::Relaxed) + 1;
        let _ = self.notify.send(gen);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Arc;

    fn test_gateway(name: &str, port: u32) -> GatewayState {
        GatewayState {
            name: name.into(),
            bind_address: "0.0.0.0".into(),
            port,
            protocol: "HTTP".into(),
            tls: None,
            hostnames: vec![],
        }
    }

    fn test_route(name: &str, backend: &str) -> RouteState {
        RouteState {
            name: name.into(),
            gateway_ref: "gw-1".into(),
            hostnames: vec!["example.com".into()],
            path_prefix: "/api/".into(),
            path_exact: String::new(),
            path_regex: String::new(),
            methods: vec![],
            backend_refs: vec![(backend.into(), 1)],
            priority: 10,
            rewrite_path: None,
            add_headers: HashMap::new(),
            middleware_refs: Vec::new(),
        }
    }

    fn test_cluster(name: &str) -> ClusterState {
        ClusterState {
            name: name.into(),
            endpoints: vec![EndpointState {
                address: "10.0.0.1".into(),
                port: 8080,
                weight: 1,
                healthy: true,
                zone: None,
                priority: 0,
            }],
            lb_algorithm: "round-robin".into(),
            health_check_path: "/healthz".into(),
            session_affinity_type: String::new(),
            session_affinity_cookie: String::new(),
            outlier_consecutive_5xx: 0,
            outlier_ejection_duration_ms: 0,
            outlier_max_ejection_pct: 0,
            slow_start_window_ms: 0,
        }
    }

    #[test]
    fn test_upsert_and_get_gateway() {
        let cfg = RuntimeConfig::new();
        cfg.upsert_gateway(test_gateway("gw-1", 8080));
        let snap = cfg.snapshot();
        assert_eq!(snap.gateways.len(), 1);
        assert_eq!(snap.gateways["gw-1"].port, 8080);
    }

    #[test]
    fn test_upsert_and_get_route() {
        let cfg = RuntimeConfig::new();
        cfg.upsert_route(test_route("route-1", "backend-1"));
        let snap = cfg.snapshot();
        assert_eq!(snap.routes.len(), 1);
        assert_eq!(snap.routes["route-1"].backend_refs[0].0, "backend-1");
    }

    #[test]
    fn test_upsert_and_get_cluster() {
        let cfg = RuntimeConfig::new();
        cfg.upsert_cluster(test_cluster("cluster-1"));
        let snap = cfg.snapshot();
        assert_eq!(snap.clusters.len(), 1);
        assert_eq!(snap.clusters["cluster-1"].endpoints.len(), 1);
    }

    #[test]
    fn test_get_cluster() {
        let cfg = RuntimeConfig::new();
        assert!(cfg.get_cluster("missing").is_none());
        cfg.upsert_cluster(test_cluster("cluster-1"));
        let c = cfg.get_cluster("cluster-1").unwrap();
        assert_eq!(c.endpoints.len(), 1);
    }

    #[test]
    fn test_delete_removes_entry() {
        let cfg = RuntimeConfig::new();
        cfg.upsert_gateway(test_gateway("gw-1", 8080));
        cfg.upsert_route(test_route("route-1", "b"));
        cfg.upsert_cluster(test_cluster("cluster-1"));

        cfg.delete_gateway("gw-1");
        cfg.delete_route("route-1");
        cfg.delete_cluster("cluster-1");

        let snap = cfg.snapshot();
        assert!(snap.gateways.is_empty());
        assert!(snap.routes.is_empty());
        assert!(snap.clusters.is_empty());
    }

    #[tokio::test]
    async fn test_apply_full_replaces_all() {
        let cfg = RuntimeConfig::new();
        cfg.upsert_gateway(test_gateway("old-gw", 80));
        cfg.upsert_route(test_route("old-route", "b"));

        cfg.apply_full(
            "v2".into(),
            vec![test_gateway("new-gw", 443)],
            vec![test_route("new-route", "new-b")],
            vec![test_cluster("new-cluster")],
            vec![],
        )
        .await;

        let snap = cfg.snapshot();
        assert_eq!(snap.version, "v2");
        assert_eq!(snap.gateways.len(), 1);
        assert!(snap.gateways.contains_key("new-gw"));
        assert_eq!(snap.routes.len(), 1);
        assert!(snap.routes.contains_key("new-route"));
        assert_eq!(snap.clusters.len(), 1);
        assert!(snap.clusters.contains_key("new-cluster"));
    }

    #[test]
    fn test_upsert_and_get_policy() {
        let cfg = RuntimeConfig::new();
        cfg.upsert_policy(PolicyState {
            name: "rate-limit-1".into(),
            policy_type: "rate-limit".into(),
            target_ref: "route-1".into(),
            config_json: r#"{"rps": 100}"#.into(),
        });
        let snap = cfg.snapshot();
        assert_eq!(snap.policies.len(), 1);
        assert_eq!(snap.policies["rate-limit-1"].policy_type, "rate-limit");

        let p = cfg.get_policy("rate-limit-1").unwrap();
        assert_eq!(p.target_ref, "route-1");
        assert!(cfg.get_policy("missing").is_none());

        cfg.delete_policy("rate-limit-1");
        assert!(cfg.get_policy("rate-limit-1").is_none());
    }

    #[test]
    fn test_generation_increments() {
        let cfg = RuntimeConfig::new();
        assert_eq!(cfg.generation(), 0);
        cfg.upsert_gateway(test_gateway("gw", 80));
        assert_eq!(cfg.generation(), 1);
        cfg.upsert_route(test_route("r", "b"));
        assert_eq!(cfg.generation(), 2);
        cfg.delete_gateway("gw");
        assert_eq!(cfg.generation(), 3);
    }

    #[tokio::test]
    async fn test_subscribe_notifies_on_change() {
        let cfg = Arc::new(RuntimeConfig::new());
        let mut rx = cfg.subscribe();

        // Initial value is 0
        assert_eq!(*rx.borrow(), 0);

        cfg.upsert_gateway(test_gateway("gw", 80));

        // Wait for notification
        rx.changed().await.unwrap();
        assert_eq!(*rx.borrow(), 1);
    }

    #[test]
    fn test_upsert_overwrites() {
        let cfg = RuntimeConfig::new();
        cfg.upsert_gateway(test_gateway("gw-1", 80));
        cfg.upsert_gateway(test_gateway("gw-1", 443));
        let snap = cfg.snapshot();
        assert_eq!(snap.gateways.len(), 1);
        assert_eq!(snap.gateways["gw-1"].port, 443);
    }
}
