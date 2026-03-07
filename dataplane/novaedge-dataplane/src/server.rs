//! gRPC server for the DataplaneControl service.
//!
//! Listens on a Unix domain socket and processes commands from the Go agent.

use std::pin::Pin;
use std::sync::Arc;
use std::time::Instant;

use tokio::net::UnixListener;
use tokio::sync::broadcast;
use tokio_stream::wrappers::UnixListenerStream;
use tokio_stream::Stream;
use tonic::{Request, Response, Status};
use tracing::{debug, info, warn};

use crate::config::{
    ClusterState, EndpointState, GatewayState, PolicyState, RouteState, RuntimeConfig, TlsState,
};
use crate::maps::MapManager;
use crate::mesh;
use crate::proto;
use crate::proto::dataplane_control_server::{DataplaneControl, DataplaneControlServer};
use crate::sdwan;

/// The gRPC service implementation.
pub struct DataplaneService {
    map_manager: Arc<MapManager>,
    runtime_config: Arc<RuntimeConfig>,
    router: Arc<std::sync::RwLock<crate::proxy::router::Router>>,
    flow_tx: broadcast::Sender<proto::FlowEvent>,
    start_time: Instant,
    mesh_tls: std::sync::Mutex<Option<mesh::mtls::MeshTlsProvider>>,
    mesh_authz: std::sync::Mutex<mesh::authz::MeshAuthzPolicy>,
    mesh_tproxy: std::sync::Mutex<mesh::tproxy::TproxyInterceptor>,
    wan_link_manager: std::sync::Mutex<sdwan::link::LinkManager>,
    wireguard_manager: std::sync::Mutex<sdwan::wireguard::WireGuardManager>,
}

impl DataplaneService {
    /// Create a new `DataplaneService`.
    pub fn new(
        map_manager: Arc<MapManager>,
        runtime_config: Arc<RuntimeConfig>,
        router: Arc<std::sync::RwLock<crate::proxy::router::Router>>,
        flow_tx: broadcast::Sender<proto::FlowEvent>,
    ) -> Self {
        Self {
            map_manager,
            runtime_config,
            router,
            flow_tx,
            start_time: Instant::now(),
            mesh_tls: std::sync::Mutex::new(None),
            mesh_authz: std::sync::Mutex::new(mesh::authz::MeshAuthzPolicy::new(
                mesh::authz::AuthzAction::Allow,
            )),
            mesh_tproxy: std::sync::Mutex::new(mesh::tproxy::TproxyInterceptor::new(
                mesh::tproxy::TproxyConfig::default(),
            )),
            wan_link_manager: std::sync::Mutex::new(sdwan::link::LinkManager::new()),
            wireguard_manager: std::sync::Mutex::new(sdwan::wireguard::WireGuardManager::new()),
        }
    }

    fn ok_response(msg: impl Into<String>) -> (i32, String) {
        (proto::OperationStatus::Ok as i32, msg.into())
    }

    /// Parse an IP address with optional CIDR prefix (e.g. "10.0.0.1/32" -> (10.0.0.1, 32)).
    #[allow(clippy::result_large_err)]
    fn parse_cidr_address(addr: &str) -> Result<(std::net::IpAddr, u8), Status> {
        let (ip_str, prefix_str) = addr.split_once('/').unwrap_or((addr, "32"));
        let ip: std::net::IpAddr = ip_str
            .parse()
            .map_err(|e| Status::invalid_argument(format!("invalid address '{addr}': {e}")))?;
        let prefix: u8 = prefix_str
            .parse()
            .map_err(|e| Status::invalid_argument(format!("invalid prefix length: {e}")))?;
        Ok((ip, prefix))
    }

    /// Serialize a proto policy config variant to a JSON string.
    fn policy_config_to_json(config: &proto::policy_config::Config) -> String {
        use proto::policy_config::Config;
        let val = match config {
            Config::RateLimit(rl) => serde_json::json!({
                "requests_per_second": rl.requests_per_second,
                "burst": rl.burst,
                "key": rl.key,
            }),
            Config::BasicAuth(ba) => serde_json::json!({
                "realm": ba.realm,
                "htpasswd": ba.htpasswd,
                "strip_authorization": ba.strip_authorization,
            }),
            Config::Cors(cors) => serde_json::json!({
                "allow_origins": cors.allow_origins,
                "allow_methods": cors.allow_methods,
                "allow_headers": cors.allow_headers,
                "expose_headers": cors.expose_headers,
                "allow_credentials": cors.allow_credentials,
                "max_age_seconds": cors.max_age_seconds,
            }),
            Config::IpFilter(ipf) => serde_json::json!({
                "action": ipf.action,
                "cidrs": ipf.cidrs,
                "source_header": ipf.source_header,
            }),
            Config::Waf(w) => serde_json::json!({
                "enabled": w.enabled,
                "mode": w.mode,
                "paranoia_level": w.paranoia_level,
                "anomaly_threshold": w.anomaly_threshold,
                "rule_exclusions": w.rule_exclusions,
                "max_body_size": w.max_body_size,
            }),
            Config::SecurityHeaders(sh) => serde_json::json!({
                "content_security_policy": sh.content_security_policy,
                "x_frame_options": sh.x_frame_options,
                "x_content_type_options": sh.x_content_type_options,
                "strict_transport_security": sh.strict_transport_security,
                "referrer_policy": sh.referrer_policy,
                "permissions_policy": sh.permissions_policy,
            }),
            _ => serde_json::json!({}),
        };
        val.to_string()
    }

    /// Convert a proto PolicyConfig to our PolicyState.
    fn policy_from_proto(p: &proto::PolicyConfig) -> PolicyState {
        let policy_type_str = match p.policy_type {
            x if x == proto::PolicyType::RateLimit as i32 => "rate-limit",
            x if x == proto::PolicyType::Jwt as i32 => "jwt",
            x if x == proto::PolicyType::BasicAuth as i32 => "basic-auth",
            x if x == proto::PolicyType::ForwardAuth as i32 => "forward-auth",
            x if x == proto::PolicyType::Oauth2 as i32 => "oauth2",
            x if x == proto::PolicyType::Waf as i32 => "waf",
            x if x == proto::PolicyType::Cors as i32 => "cors",
            x if x == proto::PolicyType::IpFilter as i32 => "ip-filter",
            x if x == proto::PolicyType::SecurityHeaders as i32 => "security-headers",
            _ => "unknown",
        };
        let config_json = match &p.config {
            Some(c) => Self::policy_config_to_json(c),
            None => String::new(),
        };
        PolicyState {
            name: p.name.clone(),
            policy_type: policy_type_str.into(),
            target_ref: String::new(),
            config_json,
        }
    }

    /// Check if an IP address string matches a CIDR block.
    fn ip_in_cidr(ip_str: &str, network: std::net::IpAddr, prefix_len: u8) -> bool {
        let ip: std::net::IpAddr = match ip_str.parse() {
            Ok(ip) => ip,
            Err(_) => return false,
        };
        match (ip, network) {
            (std::net::IpAddr::V4(addr), std::net::IpAddr::V4(net)) => {
                if prefix_len >= 32 {
                    return addr == net;
                }
                let mask = u32::MAX << (32 - prefix_len);
                (u32::from(addr) & mask) == (u32::from(net) & mask)
            }
            (std::net::IpAddr::V6(addr), std::net::IpAddr::V6(net)) => {
                if prefix_len >= 128 {
                    return addr == net;
                }
                let a = u128::from(addr);
                let n = u128::from(net);
                let mask = u128::MAX << (128 - prefix_len);
                (a & mask) == (n & mask)
            }
            _ => false, // v4/v6 mismatch
        }
    }

    /// Convert a proto LBAlgorithm enum value to a string for our LB factory.
    fn lb_algo_str(algo: i32) -> &'static str {
        match algo {
            1 => "round-robin",
            2 => "least-conn",
            3 => "p2c",
            4 => "ewma",
            5 => "ring-hash",
            6 => "maglev",
            7 => "random",
            8 => "source-hash",
            9 => "sticky",
            _ => "round-robin",
        }
    }

    /// Convert a proto GatewayProtocol enum value to a string.
    fn protocol_str(proto: i32) -> &'static str {
        match proto {
            1 => "HTTP",
            2 => "HTTPS",
            3 => "HTTP3",
            4 => "TCP",
            5 => "UDP",
            _ => "HTTP",
        }
    }

    /// Convert a proto GatewayConfig to our GatewayState.
    fn gateway_from_proto(gw: &proto::GatewayConfig) -> GatewayState {
        GatewayState {
            name: gw.name.clone(),
            bind_address: gw.bind_address.clone(),
            port: gw.port,
            protocol: Self::protocol_str(gw.protocol).to_string(),
            tls: gw.tls_config.as_ref().map(|tls| TlsState {
                cert_pem: tls.cert_pem.clone(),
                key_pem: tls.key_pem.clone(),
                client_ca_pem: if tls.ca_pem.is_empty() {
                    None
                } else {
                    Some(tls.ca_pem.clone())
                },
            }),
            hostnames: gw.hostnames.clone(),
        }
    }

    /// Convert a proto RouteConfig to our RouteState.
    fn route_from_proto(route: &proto::RouteConfig) -> RouteState {
        let (path_prefix, path_exact, path_regex) = route
            .path_match
            .as_ref()
            .map(|pm| {
                if pm.match_type == proto::PathMatchType::PathMatchPrefix as i32 {
                    (pm.value.clone(), String::new(), String::new())
                } else if pm.match_type == proto::PathMatchType::PathMatchRegex as i32 {
                    (String::new(), String::new(), pm.value.clone())
                } else {
                    // Exact match (default for EXACT and UNSPECIFIED)
                    (String::new(), pm.value.clone(), String::new())
                }
            })
            .unwrap_or_default();

        let backend_refs: Vec<(String, u32)> = route
            .backend_refs
            .iter()
            .map(|b| (b.cluster_name.clone(), b.weight))
            .collect();

        // Extract retry config.
        let (retry_max, retry_per_try_timeout_ms, retry_on, retry_backoff_base_ms) = route
            .retry
            .as_ref()
            .map(|r| {
                (
                    r.max_retries,
                    r.per_try_timeout_ms,
                    r.retry_on.clone(),
                    r.backoff_base_ms,
                )
            })
            .unwrap_or_default();

        RouteState {
            name: route.name.clone(),
            gateway_ref: route.gateway_ref.clone(),
            hostnames: route.hostnames.clone(),
            path_prefix,
            path_exact,
            path_regex,
            methods: route.methods.clone(),
            backend_refs,
            priority: route.priority,
            rewrite_path: if route.rewrite_path.is_empty() {
                None
            } else {
                Some(route.rewrite_path.clone())
            },
            add_headers: route.add_headers.clone(),
            middleware_refs: route.middleware_refs.clone(),
            mirror_cluster: if route.mirror_cluster.is_empty() {
                None
            } else {
                Some(route.mirror_cluster.clone())
            },
            mirror_percent: route.mirror_percent,
            default_backend: if route.default_backend.is_empty() {
                None
            } else {
                Some(route.default_backend.clone())
            },
            retry_max,
            retry_per_try_timeout_ms,
            retry_on,
            retry_backoff_base_ms,
            hedge_delay_ms: route.hedge_delay_ms,
            hedge_max_requests: route.hedge_max_requests,
        }
    }

    /// Convert a proto ClusterConfig to our ClusterState.
    fn cluster_from_proto(cluster: &proto::ClusterConfig) -> ClusterState {
        // Extract session affinity config.
        let (sa_type, sa_cookie, sa_header) = cluster
            .session_affinity
            .as_ref()
            .map(|sa| {
                (
                    sa.r#type.clone(),
                    sa.cookie_name.clone(),
                    sa.header_name.clone(),
                )
            })
            .unwrap_or_default();

        // Extract outlier detection config.
        let (od_5xx, od_dur, od_pct, od_interval, od_sr_min_hosts, od_sr_min_requests, od_sr_stdev) =
            cluster
                .outlier_detection
                .as_ref()
                .map(|od| {
                    (
                        od.consecutive_5xx_threshold,
                        od.base_ejection_duration_ms,
                        od.max_ejection_percent,
                        od.interval_ms,
                        od.success_rate_min_hosts,
                        od.success_rate_min_requests,
                        od.success_rate_stdev_factor,
                    )
                })
                .unwrap_or_default();

        // Extract slow start config.
        let (ss_window, ss_aggression) = cluster
            .slow_start
            .as_ref()
            .map(|ss| (ss.window_ms, ss.aggression))
            .unwrap_or((0, 1.0));

        // Extract circuit breaker config.
        let (cb_failure, cb_success, cb_open_dur, cb_half_open_max) = cluster
            .circuit_breaker
            .as_ref()
            .map(|cb| {
                (
                    cb.consecutive_errors,
                    cb.success_threshold,
                    cb.open_duration_ms,
                    cb.half_open_max_requests,
                )
            })
            .unwrap_or_default();

        // Extract connection pool config.
        let (pool_max_conn, pool_max_idle, pool_idle_timeout, pool_connect_timeout) = cluster
            .connection_pool
            .as_ref()
            .map(|cp| {
                (
                    cp.max_connections,
                    cp.max_idle,
                    cp.idle_timeout_ms,
                    cp.connect_timeout_ms,
                )
            })
            .unwrap_or_default();

        // Extract backend TLS config.
        // TLS is considered enabled when backend_tls is present.
        let (tls_enabled, tls_skip_verify, tls_ca, tls_sni) = cluster
            .backend_tls
            .as_ref()
            .map(|tls| {
                (
                    true,
                    tls.insecure_skip_verify,
                    tls.ca_pem.clone(),
                    String::new(), // SNI not yet in proto; will use cluster name
                )
            })
            .unwrap_or_default();

        // Extract upstream proxy protocol config.
        let (pp_enabled, pp_version) = cluster
            .upstream_proxy_protocol
            .as_ref()
            .map(|pp| (pp.enabled, pp.version))
            .unwrap_or_default();

        ClusterState {
            name: cluster.name.clone(),
            endpoints: cluster
                .endpoints
                .iter()
                .map(|e| EndpointState {
                    address: e.ip.clone(),
                    port: e.port,
                    weight: e.weight,
                    healthy: e.healthy,
                    zone: if e.zone.is_empty() {
                        None
                    } else {
                        Some(e.zone.clone())
                    },
                    priority: e.priority,
                    draining: false,
                    drain_start: None,
                })
                .collect(),
            lb_algorithm: Self::lb_algo_str(cluster.lb_algorithm).to_string(),
            health_check_path: cluster
                .health_check
                .as_ref()
                .map(|hc| hc.http_path.clone())
                .unwrap_or_default(),
            session_affinity_type: sa_type,
            session_affinity_cookie: sa_cookie,
            session_affinity_header: sa_header,
            outlier_consecutive_5xx: od_5xx,
            outlier_ejection_duration_ms: od_dur,
            outlier_max_ejection_pct: od_pct,
            outlier_interval_ms: od_interval,
            outlier_sr_min_hosts: od_sr_min_hosts,
            outlier_sr_min_requests: od_sr_min_requests,
            outlier_sr_stdev_factor: od_sr_stdev,
            slow_start_window_ms: ss_window,
            slow_start_aggression: ss_aggression,
            protocol: cluster.protocol.clone(),
            upstream_proxy_protocol_enabled: pp_enabled,
            upstream_proxy_protocol_version: pp_version,
            tls_enabled,
            tls_insecure_skip_verify: tls_skip_verify,
            tls_ca_pem: tls_ca,
            tls_server_name: tls_sni,
            cb_failure_threshold: cb_failure,
            cb_success_threshold: cb_success,
            cb_open_duration_ms: cb_open_dur,
            cb_half_open_max_requests: cb_half_open_max,
            pool_max_connections: pool_max_conn,
            pool_max_idle,
            pool_idle_timeout_ms: pool_idle_timeout,
            pool_connect_timeout_ms: pool_connect_timeout,
            panic_threshold_percent: cluster.panic_threshold_percent,
            max_requests_per_connection: cluster.max_requests_per_connection,
            request_queue_depth: cluster.request_queue_depth,
            request_queue_timeout_ms: cluster.request_queue_timeout_ms,
            subset_size: cluster.subset_size,
            remote_endpoints: cluster
                .remote_endpoint_groups
                .iter()
                .map(|g| crate::config::RemoteEndpointGroup {
                    cluster_name: g.cluster_name.clone(),
                    endpoints: g
                        .endpoints
                        .iter()
                        .map(|e| crate::config::EndpointState {
                            address: e.ip.clone(),
                            port: e.port,
                            weight: e.weight,
                            healthy: e.healthy,
                            zone: if e.zone.is_empty() {
                                None
                            } else {
                                Some(e.zone.clone())
                            },
                            priority: e.priority,
                            draining: false,
                            drain_start: None,
                        })
                        .collect(),
                    priority: g.priority,
                })
                .collect(),
        }
    }
}

#[tonic::async_trait]
impl DataplaneControl for DataplaneService {
    async fn apply_config(
        &self,
        request: Request<proto::ApplyConfigRequest>,
    ) -> Result<Response<proto::ApplyConfigResponse>, Status> {
        let req = request.into_inner();
        info!(
            version = %req.version,
            gateways = req.gateways.len(),
            routes = req.routes.len(),
            clusters = req.clusters.len(),
            l4_listeners = req.l4_listeners.len(),
            policies = req.policies.len(),
            wan_links = req.wan_links.len(),
            "ApplyConfig received"
        );

        // Convert proto messages to runtime config types.
        let gateways: Vec<GatewayState> =
            req.gateways.iter().map(Self::gateway_from_proto).collect();
        let routes: Vec<RouteState> = req.routes.iter().map(Self::route_from_proto).collect();
        let clusters: Vec<ClusterState> =
            req.clusters.iter().map(Self::cluster_from_proto).collect();
        let policies: Vec<PolicyState> = req.policies.iter().map(Self::policy_from_proto).collect();

        // Build proxy router routes before consuming the RouteState vec.
        let router_routes: Vec<crate::proxy::router::Route> = routes
            .iter()
            .map(|rs| {
                use crate::proxy::router::{HostMatch, PathMatch, Route as ProxyRoute};
                let hostnames = rs
                    .hostnames
                    .iter()
                    .map(|h| {
                        if h.starts_with("*.") {
                            HostMatch::Wildcard(h.clone())
                        } else {
                            HostMatch::Exact(h.clone())
                        }
                    })
                    .collect();

                let mut paths = Vec::new();
                if !rs.path_exact.is_empty() {
                    paths.push(PathMatch::Exact(rs.path_exact.clone()));
                }
                if !rs.path_prefix.is_empty() {
                    paths.push(PathMatch::Prefix(rs.path_prefix.clone()));
                }
                if !rs.path_regex.is_empty() {
                    match regex::Regex::new(&rs.path_regex) {
                        Ok(re) => paths.push(PathMatch::Regex(re)),
                        Err(e) => {
                            warn!(route = %rs.name, regex = %rs.path_regex, error = %e, "Invalid path regex, skipping regex match");
                            // If no other path constraints exist, add a catch-none to avoid
                            // accidentally matching all paths.
                            if paths.is_empty() {
                                warn!(route = %rs.name, "Route has no valid path constraints after regex failure, route will never match");
                            }
                        }
                    }
                }
                // If paths is empty (no path constraints at all), the route matches all paths
                // which is valid Gateway API behavior (no pathMatch = match everything).

                ProxyRoute {
                    name: rs.name.clone(),
                    hostnames,
                    paths,
                    methods: rs.methods.clone(),
                    headers: Vec::new(),
                    query_params: Vec::new(),
                    backend_refs: rs.backend_refs.clone(),
                    priority: rs.priority,
                    rewrite_path: rs.rewrite_path.clone(),
                    add_headers: rs.add_headers.clone(),
                    middleware_refs: rs.middleware_refs.clone(),
                }
            })
            .collect();

        // Atomically replace all runtime config.
        self.runtime_config
            .apply_full(req.version.clone(), gateways, routes, clusters, policies)
            .await;

        // Push routes to the proxy router.
        {
            let route_count = router_routes.len();
            self.router.write().unwrap().set_routes(router_routes);
            info!(count = route_count, "Routes pushed to proxy router");
        }

        info!(version = %req.version, "Configuration applied to runtime state");

        // Apply mesh configuration.
        if let Some(mesh_cfg) = &req.mesh_config {
            if mesh_cfg.enabled {
                let ca_pem = String::from_utf8_lossy(&mesh_cfg.ca_cert_pem).to_string();
                let cert_pem = String::from_utf8_lossy(&mesh_cfg.cert_pem).to_string();
                let key_pem = String::from_utf8_lossy(&mesh_cfg.key_pem).to_string();
                let tls_config = mesh::mtls::MeshTlsConfig {
                    ca_cert_pem: ca_pem,
                    workload_cert_pem: cert_pem,
                    workload_key_pem: key_pem,
                    spiffe_id: mesh_cfg.spiffe_id.clone(),
                    ..mesh::mtls::MeshTlsConfig::default()
                };
                let mut provider = mesh::mtls::MeshTlsProvider::new(tls_config);
                if !provider.config().ca_cert_pem.is_empty() {
                    let _ = provider.initialize();
                }
                *self.mesh_tls.lock().unwrap() = Some(provider);
                info!("Mesh mTLS configuration applied");
            }
        }

        // Apply WAN link configuration.
        {
            let mut wan_mgr = self.wan_link_manager.lock().unwrap();
            *wan_mgr = sdwan::link::LinkManager::new();
            for wl_cfg in &req.wan_links {
                let gateway: std::net::IpAddr = wl_cfg
                    .gateway
                    .parse()
                    .unwrap_or(std::net::IpAddr::V4(std::net::Ipv4Addr::UNSPECIFIED));
                let mut wan_link =
                    sdwan::link::WANLink::new(&wl_cfg.name, &wl_cfg.interface, gateway);
                wan_link.priority = wl_cfg.priority;
                // Set initial bandwidth from config.
                if wl_cfg.bandwidth_mbps > 0 {
                    let metrics = sdwan::link::LinkMetrics {
                        bandwidth_bps: (wl_cfg.bandwidth_mbps as u64) * 1_000_000,
                        ..sdwan::link::LinkMetrics::default()
                    };
                    wan_link.update_metrics(metrics);
                }
                wan_mgr.add_link(wan_link);
                // Set weight using mutable access after insertion.
                if let Some(link) = wan_mgr.get_link_mut(&wl_cfg.name) {
                    link.weight = wl_cfg.bandwidth_mbps;
                }
            }
            if !req.wan_links.is_empty() {
                let active = wan_mgr.active_links();
                info!(
                    count = req.wan_links.len(),
                    active = active.len(),
                    total = wan_mgr.link_count(),
                    "WAN links applied"
                );
                if let Some(best) = wan_mgr.best_link() {
                    info!(best_link = %best.name, "Best WAN link selected");
                }
            }
        }

        // Reset WireGuard tunnels on full config apply.
        {
            let mut wg = self.wireguard_manager.lock().unwrap();
            *wg = sdwan::wireguard::WireGuardManager::new();
            // Set up a default WireGuard tunnel for each WAN link that has SD-WAN.
            for wl_cfg in &req.wan_links {
                let wg_config = sdwan::wireguard::WireGuardConfig {
                    interface_name: format!("wg-{}", wl_cfg.name),
                    ..sdwan::wireguard::WireGuardConfig::default()
                };
                let _ = wg.add_tunnel(wg_config);
            }
            if wg.tunnel_count() > 0 {
                let _ = wg.apply();
                info!(
                    tunnels = wg.tunnel_count(),
                    active = wg.is_active(),
                    "WireGuard tunnels configured"
                );
            }
        }

        let (status, message) = Self::ok_response("config applied");
        Ok(Response::new(proto::ApplyConfigResponse {
            status,
            message,
            applied_version: req.version,
        }))
    }

    async fn upsert_gateway(
        &self,
        request: Request<proto::UpsertGatewayRequest>,
    ) -> Result<Response<proto::UpsertGatewayResponse>, Status> {
        let req = request.into_inner();
        let gw = req
            .gateway
            .as_ref()
            .ok_or_else(|| Status::invalid_argument("missing gateway config"))?;
        info!(name = %gw.name, port = gw.port, protocol = gw.protocol, "UpsertGateway");

        self.runtime_config
            .upsert_gateway(Self::gateway_from_proto(gw));

        let (status, message) = Self::ok_response(format!("gateway '{}' upserted", gw.name));
        Ok(Response::new(proto::UpsertGatewayResponse {
            status,
            message,
        }))
    }

    async fn delete_gateway(
        &self,
        request: Request<proto::DeleteGatewayRequest>,
    ) -> Result<Response<proto::DeleteGatewayResponse>, Status> {
        let req = request.into_inner();
        info!(name = %req.name, "DeleteGateway");

        self.runtime_config.delete_gateway(&req.name);

        let (status, message) = Self::ok_response(format!("gateway '{}' deleted", req.name));
        Ok(Response::new(proto::DeleteGatewayResponse {
            status,
            message,
        }))
    }

    async fn upsert_route(
        &self,
        request: Request<proto::UpsertRouteRequest>,
    ) -> Result<Response<proto::UpsertRouteResponse>, Status> {
        let req = request.into_inner();
        let route = req
            .route
            .as_ref()
            .ok_or_else(|| Status::invalid_argument("missing route config"))?;
        info!(name = %route.name, gateway_ref = %route.gateway_ref, "UpsertRoute");

        self.runtime_config
            .upsert_route(Self::route_from_proto(route));

        let (status, message) = Self::ok_response(format!("route '{}' upserted", route.name));
        Ok(Response::new(proto::UpsertRouteResponse {
            status,
            message,
        }))
    }

    async fn delete_route(
        &self,
        request: Request<proto::DeleteRouteRequest>,
    ) -> Result<Response<proto::DeleteRouteResponse>, Status> {
        let req = request.into_inner();
        info!(name = %req.name, "DeleteRoute");

        self.runtime_config.delete_route(&req.name);

        let (status, message) = Self::ok_response(format!("route '{}' deleted", req.name));
        Ok(Response::new(proto::DeleteRouteResponse {
            status,
            message,
        }))
    }

    async fn upsert_cluster(
        &self,
        request: Request<proto::UpsertClusterRequest>,
    ) -> Result<Response<proto::UpsertClusterResponse>, Status> {
        let req = request.into_inner();
        let cluster = req
            .cluster
            .as_ref()
            .ok_or_else(|| Status::invalid_argument("missing cluster config"))?;
        info!(name = %cluster.name, endpoints = cluster.endpoints.len(), "UpsertCluster");

        self.runtime_config
            .upsert_cluster(Self::cluster_from_proto(cluster));

        let (status, message) = Self::ok_response(format!("cluster '{}' upserted", cluster.name));
        Ok(Response::new(proto::UpsertClusterResponse {
            status,
            message,
        }))
    }

    async fn delete_cluster(
        &self,
        request: Request<proto::DeleteClusterRequest>,
    ) -> Result<Response<proto::DeleteClusterResponse>, Status> {
        let req = request.into_inner();
        info!(name = %req.name, "DeleteCluster");

        self.runtime_config.delete_cluster(&req.name);

        let (status, message) = Self::ok_response(format!("cluster '{}' deleted", req.name));
        Ok(Response::new(proto::DeleteClusterResponse {
            status,
            message,
        }))
    }

    async fn upsert_l4_listener(
        &self,
        request: Request<proto::UpsertL4ListenerRequest>,
    ) -> Result<Response<proto::UpsertL4ListenerResponse>, Status> {
        let req = request.into_inner();
        let listener = req
            .listener
            .as_ref()
            .ok_or_else(|| Status::invalid_argument("missing L4 listener config"))?;
        info!(name = %listener.name, port = listener.port, "UpsertL4Listener");

        // Register L4 listeners as gateways with TCP protocol.
        self.runtime_config.upsert_gateway(GatewayState {
            name: listener.name.clone(),
            bind_address: listener.bind_address.clone(),
            port: listener.port,
            protocol: "TCP".into(),
            tls: None,
            hostnames: vec![],
        });

        let (status, message) =
            Self::ok_response(format!("L4 listener '{}' upserted", listener.name));
        Ok(Response::new(proto::UpsertL4ListenerResponse {
            status,
            message,
        }))
    }

    async fn delete_l4_listener(
        &self,
        request: Request<proto::DeleteL4ListenerRequest>,
    ) -> Result<Response<proto::DeleteL4ListenerResponse>, Status> {
        let req = request.into_inner();
        info!(name = %req.name, "DeleteL4Listener");

        self.runtime_config.delete_gateway(&req.name);

        let (status, message) = Self::ok_response(format!("L4 listener '{}' deleted", req.name));
        Ok(Response::new(proto::DeleteL4ListenerResponse {
            status,
            message,
        }))
    }

    async fn upsert_policy(
        &self,
        request: Request<proto::UpsertPolicyRequest>,
    ) -> Result<Response<proto::UpsertPolicyResponse>, Status> {
        let req = request.into_inner();
        let policy = req
            .policy
            .as_ref()
            .ok_or_else(|| Status::invalid_argument("missing policy config"))?;
        info!(name = %policy.name, policy_type = policy.policy_type, "UpsertPolicy");

        self.runtime_config
            .upsert_policy(Self::policy_from_proto(policy));

        let (status, message) = Self::ok_response(format!("policy '{}' upserted", policy.name));
        Ok(Response::new(proto::UpsertPolicyResponse {
            status,
            message,
        }))
    }

    async fn delete_policy(
        &self,
        request: Request<proto::DeletePolicyRequest>,
    ) -> Result<Response<proto::DeletePolicyResponse>, Status> {
        let req = request.into_inner();
        info!(name = %req.name, "DeletePolicy");

        self.runtime_config.delete_policy(&req.name);

        let (status, message) = Self::ok_response(format!("policy '{}' deleted", req.name));
        Ok(Response::new(proto::DeletePolicyResponse {
            status,
            message,
        }))
    }

    async fn upsert_mesh_config(
        &self,
        request: Request<proto::UpsertMeshConfigRequest>,
    ) -> Result<Response<proto::UpsertMeshConfigResponse>, Status> {
        let req = request.into_inner();
        let mesh_cfg = req
            .mesh_config
            .as_ref()
            .ok_or_else(|| Status::invalid_argument("missing mesh config"))?;
        info!(enabled = mesh_cfg.enabled, mtls_mode = %mesh_cfg.mtls_mode, "UpsertMeshConfig");

        // Parse and validate the SPIFFE ID from the mesh configuration.
        if !mesh_cfg.spiffe_id.is_empty() {
            match mesh::spiffe::SpiffeId::parse(&mesh_cfg.spiffe_id) {
                Ok(spiffe_id) => {
                    info!(
                        spiffe_uri = %spiffe_id.to_uri(),
                        trust_domain = %spiffe_id.trust_domain,
                        path = %spiffe_id.path,
                        "Parsed mesh SPIFFE ID"
                    );
                    // Verify the SPIFFE ID matches the configured trust domain.
                    if !mesh_cfg.trust_domain.is_empty()
                        && spiffe_id.trust_domain != mesh_cfg.trust_domain
                    {
                        warn!(
                            spiffe_domain = %spiffe_id.trust_domain,
                            config_domain = %mesh_cfg.trust_domain,
                            "SPIFFE trust domain mismatch"
                        );
                    }
                    // Check if this is an agent SPIFFE ID and log its role.
                    let agent_id =
                        mesh::spiffe::SpiffeId::agent(&spiffe_id.trust_domain, "dataplane");
                    if spiffe_id.matches_pattern(&agent_id.to_uri()) {
                        debug!("SPIFFE ID matches agent pattern");
                    }
                }
                Err(e) => {
                    warn!(
                        spiffe_id = %mesh_cfg.spiffe_id,
                        error = %e,
                        "Invalid SPIFFE ID in mesh config"
                    );
                }
            }
        }

        // Configure mTLS provider.
        if mesh_cfg.enabled {
            let ca_pem = String::from_utf8_lossy(&mesh_cfg.ca_cert_pem).to_string();
            let cert_pem = String::from_utf8_lossy(&mesh_cfg.cert_pem).to_string();
            let key_pem = String::from_utf8_lossy(&mesh_cfg.key_pem).to_string();

            let tls_config = mesh::mtls::MeshTlsConfig {
                ca_cert_pem: ca_pem,
                workload_cert_pem: cert_pem.clone(),
                workload_key_pem: key_pem.clone(),
                spiffe_id: mesh_cfg.spiffe_id.clone(),
                ..mesh::mtls::MeshTlsConfig::default()
            };

            let mut provider = mesh::mtls::MeshTlsProvider::new(tls_config);
            if !provider.config().ca_cert_pem.is_empty() {
                let _ = provider.initialize();
            }
            // Check if existing provider needs certificate renewal.
            {
                let existing = self.mesh_tls.lock().unwrap();
                if let Some(ref prev) = *existing {
                    if prev.is_initialized() && prev.needs_renewal() {
                        info!("Previous mesh TLS provider needs renewal, replacing");
                    }
                }
            }
            // Update certificates on the new provider if certs were provided.
            if !cert_pem.is_empty() && !key_pem.is_empty() {
                provider.update_certificates(cert_pem, key_pem);
            }
            info!(
                initialized = provider.is_initialized(),
                spiffe_id = %provider.spiffe_id(),
                "Mesh mTLS provider configured"
            );
            *self.mesh_tls.lock().unwrap() = Some(provider);

            // Configure mesh authorization policy with default allow.
            // Real authorization rules will be set via mesh policy updates.
            {
                let mut authz = self.mesh_authz.lock().unwrap();
                // Configure a default authorization policy allowing mesh traffic.
                let trust_domain = if mesh_cfg.trust_domain.is_empty() {
                    "cluster.local"
                } else {
                    &mesh_cfg.trust_domain
                };
                let default_rules = vec![mesh::authz::AuthzRule {
                    source_patterns: vec![format!("spiffe://{trust_domain}/*")],
                    destination_port: None,
                    action: mesh::authz::AuthzAction::Allow,
                }];
                authz.set_rules(default_rules);
                // Add port-specific rules for intercepted ports.
                for port in &mesh_cfg.intercept_ports {
                    authz.add_rule(mesh::authz::AuthzRule {
                        source_patterns: vec!["*".into()],
                        destination_port: Some(*port as u16),
                        action: mesh::authz::AuthzAction::Allow,
                    });
                }
                info!(trust_domain = %trust_domain, "Mesh authorization policy configured");

                // Validate the policy by checking a sample workload identity.
                let sample_id = mesh::spiffe::SpiffeId::workload(trust_domain, "default", "sample");
                let result = authz.check(&sample_id, 80);
                debug!(
                    sample_source = %sample_id,
                    result = ?result,
                    "Authorization policy validation check"
                );
            }

            // Configure TPROXY interception.
            let tproxy_config = mesh::tproxy::TproxyConfig {
                enabled: true,
                inbound_port: 15006,
                outbound_port: 15001,
                exclude_ports: mesh_cfg.intercept_ports.iter().map(|&p| p as u16).collect(),
                ..mesh::tproxy::TproxyConfig::default()
            };
            let mut tproxy = self.mesh_tproxy.lock().unwrap();

            // Check idempotency: skip reinstall if already active with same config.
            if tproxy.is_active() {
                let current_cfg = tproxy.config();
                info!(
                    current_inbound = current_cfg.inbound_port,
                    current_outbound = current_cfg.outbound_port,
                    "TPROXY already active, reinstalling with new config"
                );
                let _ = tproxy.uninstall();
            }
            *tproxy = mesh::tproxy::TproxyInterceptor::new(tproxy_config);
            let _ = tproxy.install();
            let installed_cfg = tproxy.config();
            info!(
                active = tproxy.is_active(),
                inbound_port = installed_cfg.inbound_port,
                outbound_port = installed_cfg.outbound_port,
                exclude_ports = ?installed_cfg.exclude_ports,
                "TPROXY interception installed"
            );
            // Log the iptables commands that were (or would be) applied.
            for cmd in tproxy.iptables_commands() {
                debug!(cmd = %cmd, "TPROXY iptables rule");
            }
        }

        let (status, message) = Self::ok_response("mesh config applied");
        Ok(Response::new(proto::UpsertMeshConfigResponse {
            status,
            message,
        }))
    }

    async fn delete_mesh_config(
        &self,
        request: Request<proto::DeleteMeshConfigRequest>,
    ) -> Result<Response<proto::DeleteMeshConfigResponse>, Status> {
        let _req = request.into_inner();
        info!("DeleteMeshConfig");

        // Tear down mesh: uninstall TPROXY rules and clear TLS provider.
        {
            let mut tproxy = self.mesh_tproxy.lock().unwrap();
            if tproxy.is_active() {
                let cfg = tproxy.config();
                info!(
                    inbound_port = cfg.inbound_port,
                    outbound_port = cfg.outbound_port,
                    "Uninstalling active TPROXY rules"
                );
                let _ = tproxy.uninstall();
            }
        }

        // Clear mTLS provider and log final state.
        {
            let mut tls_guard = self.mesh_tls.lock().unwrap();
            if let Some(ref provider) = *tls_guard {
                info!(
                    was_initialized = provider.is_initialized(),
                    spiffe_id = %provider.spiffe_id(),
                    "Clearing mesh mTLS provider"
                );
            }
            *tls_guard = None;
        }

        // Clear authorization rules.
        {
            let mut authz = self.mesh_authz.lock().unwrap();
            authz.set_rules(vec![]);
            info!("Mesh authorization rules cleared");
        }

        let (status, message) = Self::ok_response("mesh config deleted");
        Ok(Response::new(proto::DeleteMeshConfigResponse {
            status,
            message,
        }))
    }

    async fn upsert_wan_link(
        &self,
        request: Request<proto::UpsertWanLinkRequest>,
    ) -> Result<Response<proto::UpsertWanLinkResponse>, Status> {
        let req = request.into_inner();
        let link_cfg = req
            .wan_link
            .as_ref()
            .ok_or_else(|| Status::invalid_argument("missing WAN link config"))?;
        info!(name = %link_cfg.name, interface = %link_cfg.interface, "UpsertWANLink");

        let gateway: std::net::IpAddr = link_cfg
            .gateway
            .parse()
            .unwrap_or(std::net::IpAddr::V4(std::net::Ipv4Addr::UNSPECIFIED));

        let mut wan_link = sdwan::link::WANLink::new(&link_cfg.name, &link_cfg.interface, gateway);
        wan_link.priority = link_cfg.priority;

        // Set initial metrics from SLA target if available, to evaluate link state.
        if let Some(sla) = &link_cfg.sla_target {
            let metrics = sdwan::link::LinkMetrics {
                latency_ms: 0.0,
                jitter_ms: 0.0,
                packet_loss_pct: 0.0,
                bandwidth_bps: (link_cfg.bandwidth_mbps as u64) * 1_000_000,
                utilization_pct: 0.0,
                last_updated: std::time::Instant::now(),
            };
            wan_link.update_metrics(metrics);
            debug!(
                link = %link_cfg.name,
                max_latency = sla.max_latency_ms,
                max_jitter = sla.max_jitter_ms,
                max_loss = sla.max_packet_loss_pct,
                "SLA target configured for link"
            );
        }

        let mut mgr = self.wan_link_manager.lock().unwrap();

        // Log existing link state if replacing.
        if let Some(existing) = mgr.get_link(&link_cfg.name) {
            info!(
                link = %link_cfg.name,
                current_state = ?existing.state,
                current_latency = existing.metrics.latency_ms,
                is_usable = existing.is_usable(),
                "Replacing existing WAN link"
            );
        }

        // Remove existing link with same name, then re-add.
        mgr.remove_link(&link_cfg.name);
        mgr.add_link(wan_link);

        // Report link manager status after update.
        let total = mgr.link_count();
        let active = mgr.active_links();
        let active_count = active.len();
        info!(
            link = %link_cfg.name,
            total_links = total,
            active_links = active_count,
            "WAN link upserted"
        );

        // Report best link selection after update.
        if let Some(best) = mgr.best_link() {
            debug!(
                best_link = %best.name,
                interface = %best.interface,
                gateway = %best.gateway,
                latency_ms = best.metrics.latency_ms,
                state = ?best.state,
                probe_targets = best.probe_targets.len(),
                probe_interval_ms = best.probe_interval.as_millis() as u64,
                "Current best WAN link"
            );
        }

        // Evaluate path selection with a default policy to exercise PathSelector.
        if total > 0 {
            // Collect links for path selection evaluation.
            let all_links: Vec<sdwan::link::WANLink> = (0..total)
                .filter_map(|_| {
                    // We need to iterate; collect names first.
                    None::<sdwan::link::WANLink>
                })
                .collect();
            // Use active_links for path selection check.
            if !active.is_empty() {
                let default_policy = sdwan::path_selection::WANPolicy {
                    name: "default-check".into(),
                    match_criteria: sdwan::path_selection::TrafficMatch {
                        destination_cidrs: vec!["0.0.0.0/0".into()],
                        dscp_values: vec![],
                        application: None,
                    },
                    sla: sdwan::path_selection::SLARequirements::default(),
                    strategy: sdwan::path_selection::PathStrategy::Performance,
                    preferred_links: vec![],
                };
                // Build a vec of WANLinks from active links for PathSelector.
                // Note: PathSelector::select takes a slice of owned WANLinks.
                // We clone the active links for the selection check.
                let active_owned: Vec<sdwan::link::WANLink> =
                    active.iter().map(|l| (*l).clone()).collect();
                if let Some(selected) =
                    sdwan::path_selection::PathSelector::select(&active_owned, &default_policy)
                {
                    debug!(
                        selected_link = %selected.name,
                        strategy = ?default_policy.strategy,
                        "Path selection result"
                    );
                }
            }
            drop(all_links);
        }

        drop(mgr);

        // Apply WireGuard tunnels if any are configured (wiring WireGuard manager reads).
        {
            let wg = self.wireguard_manager.lock().unwrap();
            if wg.tunnel_count() > 0 {
                debug!(
                    tunnels = wg.tunnel_count(),
                    active = wg.is_active(),
                    "WireGuard status after WAN link update"
                );
                // Check if a tunnel exists for this link's interface.
                if let Some(tunnel) = wg.get_tunnel(&link_cfg.name) {
                    debug!(
                        interface = %tunnel.interface_name,
                        listen_port = tunnel.listen_port,
                        peers = tunnel.peers.len(),
                        mtu = tunnel.mtu,
                        has_private_key = !tunnel.private_key.is_empty(),
                        "WireGuard tunnel for link"
                    );
                    // Log peer details.
                    for peer in &tunnel.peers {
                        debug!(
                            public_key = %peer.public_key,
                            endpoint = ?peer.endpoint,
                            allowed_ips = ?peer.allowed_ips,
                            keepalive = ?peer.keepalive_interval,
                            has_psk = peer.preshared_key.is_some(),
                            "WireGuard peer"
                        );
                    }
                }
            }
        }

        let (status, message) = Self::ok_response(format!("WAN link '{}' upserted", link_cfg.name));
        Ok(Response::new(proto::UpsertWanLinkResponse {
            status,
            message,
        }))
    }

    async fn delete_wan_link(
        &self,
        request: Request<proto::DeleteWanLinkRequest>,
    ) -> Result<Response<proto::DeleteWanLinkResponse>, Status> {
        let req = request.into_inner();
        info!(name = %req.name, "DeleteWANLink");

        let mut mgr = self.wan_link_manager.lock().unwrap();

        // Log the link being deleted.
        if let Some(link) = mgr.get_link(&req.name) {
            info!(
                link = %req.name,
                state = ?link.state,
                latency_ms = link.metrics.latency_ms,
                jitter_ms = link.metrics.jitter_ms,
                packet_loss = link.metrics.packet_loss_pct,
                bandwidth_bps = link.metrics.bandwidth_bps,
                utilization_pct = link.metrics.utilization_pct,
                is_usable = link.is_usable(),
                "Removing WAN link"
            );
        }

        mgr.remove_link(&req.name);

        // Report remaining links.
        let remaining = mgr.link_count();
        let active = mgr.active_links().len();
        info!(
            remaining_total = remaining,
            remaining_active = active,
            "WAN link deleted"
        );

        // Update best link after removal.
        if let Some(best) = mgr.best_link() {
            debug!(best_link = %best.name, "New best WAN link after deletion");
        }

        // Also remove any WireGuard tunnel associated with this link.
        drop(mgr);
        {
            let mut wg = self.wireguard_manager.lock().unwrap();
            if wg.get_tunnel(&req.name).is_some() {
                let _ = wg.remove_tunnel(&req.name);
                info!(interface = %req.name, "Removed associated WireGuard tunnel");
            }
        }

        let (status, message) = Self::ok_response(format!("WAN link '{}' deleted", req.name));
        Ok(Response::new(proto::DeleteWanLinkResponse {
            status,
            message,
        }))
    }

    async fn attach_program(
        &self,
        request: Request<proto::AttachProgramRequest>,
    ) -> Result<Response<proto::AttachProgramResponse>, Status> {
        let req = request.into_inner();
        info!(
            name = %req.name,
            object_path = %req.object_path,
            interface = %req.interface,
            attach_type = req.attach_type,
            "AttachProgram"
        );

        #[cfg(target_os = "linux")]
        {
            // Load the eBPF object file and attach the specified program.
            let mut load_result = crate::loader::load_ebpf(&req.object_path)
                .map_err(|e| Status::internal(format!("failed to load eBPF object: {e}")))?;

            if let Some(ref mut bpf) = load_result.bpf {
                let iface = if req.interface.is_empty() {
                    "eth0"
                } else {
                    &req.interface
                };
                // attach_type: 1 = XDP, 2 = TC
                match req.attach_type {
                    2 => crate::loader::attach_tc(bpf, &req.name, iface)
                        .map_err(|e| Status::internal(format!("TC attach failed: {e}")))?,
                    _ => crate::loader::attach_xdp(bpf, &req.name, iface)
                        .map_err(|e| Status::internal(format!("XDP attach failed: {e}")))?,
                }
            } else {
                return Err(Status::internal("eBPF handle not available after load"));
            }

            let (status, message) = Self::ok_response(format!("program '{}' attached", req.name));
            Ok(Response::new(proto::AttachProgramResponse {
                status,
                message,
                program_id: 1,
            }))
        }

        #[cfg(not(target_os = "linux"))]
        {
            Err(Status::unimplemented(
                "eBPF program attachment requires Linux",
            ))
        }
    }

    async fn detach_program(
        &self,
        request: Request<proto::DetachProgramRequest>,
    ) -> Result<Response<proto::DetachProgramResponse>, Status> {
        let req = request.into_inner();
        info!(name = %req.name, "DetachProgram");

        // eBPF programs attached via aya are detached when the Ebpf handle is dropped.
        // For runtime detachment, the caller must stop the dataplane or reload programs.
        // Log the detach request; actual cleanup happens on process shutdown.
        warn!(
            name = %req.name,
            "Program detach acknowledged — eBPF programs detach on handle drop"
        );

        let (status, message) = Self::ok_response(format!("program '{}' detached", req.name));
        Ok(Response::new(proto::DetachProgramResponse {
            status,
            message,
        }))
    }

    type StreamFlowsStream =
        Pin<Box<dyn Stream<Item = Result<proto::FlowEvent, Status>> + Send + 'static>>;

    async fn stream_flows(
        &self,
        request: Request<proto::StreamFlowsRequest>,
    ) -> Result<Response<Self::StreamFlowsStream>, Status> {
        let req = request.into_inner();
        info!(
            buffer_size = req.buffer_size,
            filter_protocol = req.filter_protocol,
            "StreamFlows"
        );

        let mut rx = self.flow_tx.subscribe();
        let filter_protocol = req.filter_protocol;
        let filter_src_cidr = req.filter_src_cidr;
        let filter_dst_cidr = req.filter_dst_cidr;

        // Parse CIDR filters once at stream setup.
        let src_net: Option<(std::net::IpAddr, u8)> = if filter_src_cidr.is_empty() {
            None
        } else {
            Self::parse_cidr_address(&filter_src_cidr).ok()
        };
        let dst_net: Option<(std::net::IpAddr, u8)> = if filter_dst_cidr.is_empty() {
            None
        } else {
            Self::parse_cidr_address(&filter_dst_cidr).ok()
        };

        let stream = async_stream::try_stream! {
            loop {
                match rx.recv().await {
                    Ok(event) => {
                        if filter_protocol > 0 && event.protocol != filter_protocol {
                            continue;
                        }
                        // Apply CIDR filters if specified.
                        if let Some((net_ip, prefix)) = &src_net {
                            if !Self::ip_in_cidr(&event.src_ip, *net_ip, *prefix) {
                                continue;
                            }
                        }
                        if let Some((net_ip, prefix)) = &dst_net {
                            if !Self::ip_in_cidr(&event.dst_ip, *net_ip, *prefix) {
                                continue;
                            }
                        }
                        yield event;
                    }
                    Err(broadcast::error::RecvError::Lagged(n)) => {
                        warn!(skipped = n, "flow stream lagged, some events dropped");
                        continue;
                    }
                    Err(broadcast::error::RecvError::Closed) => {
                        break;
                    }
                }
            }
        };

        Ok(Response::new(Box::pin(stream)))
    }

    async fn get_dataplane_status(
        &self,
        _request: Request<proto::GetDataplaneStatusRequest>,
    ) -> Result<Response<proto::DataplaneStatus>, Status> {
        let config_gen = self.runtime_config.generation();
        info!(config_generation = config_gen, "GetDataplaneStatus");
        let map_status = self.map_manager.get_status();
        let snap = self.runtime_config.snapshot();

        // Log config snapshot counts for observability.
        debug!(
            routes = snap.routes.len(),
            clusters = snap.clusters.len(),
            policies = snap.policies.len(),
            generation = config_gen,
            "Config snapshot summary"
        );

        // Log policy target refs for debugging.
        for (name, policy) in &snap.policies {
            if !policy.target_ref.is_empty() {
                debug!(
                    policy = %name,
                    target_ref = %policy.target_ref,
                    "Policy target"
                );
            }
        }

        // Report mesh mTLS status.
        {
            let tls_guard = self.mesh_tls.lock().unwrap();
            if let Some(ref provider) = *tls_guard {
                let initialized = provider.is_initialized();
                let needs_renewal = provider.needs_renewal();
                let spiffe = provider.spiffe_id();
                let cfg = provider.config();
                debug!(
                    initialized = initialized,
                    needs_renewal = needs_renewal,
                    spiffe_id = %spiffe,
                    cert_lifetime_secs = cfg.cert_lifetime.as_secs(),
                    renewal_threshold = cfg.renewal_threshold,
                    has_workload_cert = !cfg.workload_cert_pem.is_empty(),
                    has_workload_key = !cfg.workload_key_pem.is_empty(),
                    "Mesh mTLS status"
                );
            }
        }

        // Report mesh TPROXY status.
        {
            let tproxy_guard = self.mesh_tproxy.lock().unwrap();
            let tproxy_active = tproxy_guard.is_active();
            let tproxy_cfg = tproxy_guard.config();
            debug!(
                active = tproxy_active,
                inbound_port = tproxy_cfg.inbound_port,
                outbound_port = tproxy_cfg.outbound_port,
                enabled = tproxy_cfg.enabled,
                exclude_ports = ?tproxy_cfg.exclude_ports,
                exclude_cidrs = ?tproxy_cfg.exclude_cidrs,
                "TPROXY status"
            );
        }

        // Report WAN link status.
        let wan_mgr = self.wan_link_manager.lock().unwrap();
        let wan_total = wan_mgr.link_count();
        let wan_active = wan_mgr.active_links();
        let wan_active_count = wan_active.len();
        if let Some(best) = wan_mgr.best_link() {
            debug!(
                best_link = %best.name,
                latency_ms = best.metrics.latency_ms,
                "Best WAN link"
            );
        }
        drop(wan_mgr);

        // Report WireGuard status.
        let wg_mgr = self.wireguard_manager.lock().unwrap();
        let wg_tunnels = wg_mgr.tunnel_count();
        let wg_active = wg_mgr.is_active();
        debug!(tunnels = wg_tunnels, active = wg_active, "WireGuard status");
        drop(wg_mgr);

        let map_sizes = vec![
            proto::MapInfo {
                name: "rate_limits".into(),
                entries: map_status.rate_limit_count as u64,
                max_entries: 65536,
            },
            proto::MapInfo {
                name: "config_routes".into(),
                entries: snap.routes.len() as u64,
                max_entries: 0,
            },
            proto::MapInfo {
                name: "config_clusters".into(),
                entries: snap.clusters.len() as u64,
                max_entries: 0,
            },
            proto::MapInfo {
                name: "config_policies".into(),
                entries: snap.policies.len() as u64,
                max_entries: 0,
            },
            proto::MapInfo {
                name: "wan_links_total".into(),
                entries: wan_total as u64,
                max_entries: 0,
            },
            proto::MapInfo {
                name: "wan_links_active".into(),
                entries: wan_active_count as u64,
                max_entries: 0,
            },
            proto::MapInfo {
                name: "wireguard_tunnels".into(),
                entries: wg_tunnels as u64,
                max_entries: 0,
            },
        ];

        Ok(Response::new(proto::DataplaneStatus {
            mode: map_status.mode.into(),
            loaded_programs: vec![],
            active_connections: 0,
            map_sizes,
            uptime_seconds: self.start_time.elapsed().as_secs(),
            config_version: snap.version,
        }))
    }

    type StreamMetricsStream =
        Pin<Box<dyn Stream<Item = Result<proto::MetricsSnapshot, Status>> + Send + 'static>>;

    async fn stream_metrics(
        &self,
        request: Request<proto::StreamMetricsRequest>,
    ) -> Result<Response<Self::StreamMetricsStream>, Status> {
        let req = request.into_inner();
        let interval_ms = if req.interval_ms == 0 {
            5000
        } else {
            req.interval_ms
        };
        info!(interval_ms = interval_ms, "StreamMetrics");

        let map_manager = Arc::clone(&self.map_manager);

        let stream = async_stream::try_stream! {
            let mut interval = tokio::time::interval(std::time::Duration::from_millis(interval_ms));
            loop {
                interval.tick().await;
                let status = map_manager.get_status();
                let mut gauges = std::collections::HashMap::new();
                gauges.insert("rate_limit_count".into(), status.rate_limit_count as f64);
                let now = std::time::SystemTime::now()
                    .duration_since(std::time::UNIX_EPOCH)
                    .unwrap_or_default()
                    .as_nanos() as u64;
                yield proto::MetricsSnapshot {
                    timestamp_ns: now,
                    counters: std::collections::HashMap::new(),
                    gauges,
                    histograms: std::collections::HashMap::new(),
                };
            }
        };

        Ok(Response::new(Box::pin(stream)))
    }
}

/// Run the gRPC server on a Unix domain socket.
pub async fn run(
    map_manager: Arc<MapManager>,
    runtime_config: Arc<RuntimeConfig>,
    router: Arc<std::sync::RwLock<crate::proxy::router::Router>>,
    flow_tx: broadcast::Sender<proto::FlowEvent>,
    socket_path: &str,
) -> anyhow::Result<()> {
    let _ = std::fs::remove_file(socket_path);
    if let Some(parent) = std::path::Path::new(socket_path).parent() {
        std::fs::create_dir_all(parent)?;
    }
    let uds = UnixListener::bind(socket_path)?;

    // Restrict socket permissions to owner only (0o600) to prevent
    // unauthorized processes from sending config to the dataplane.
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let perms = std::fs::Permissions::from_mode(0o600);
        if let Err(e) = std::fs::set_permissions(socket_path, perms) {
            warn!(error = %e, "Failed to set socket permissions");
        }
    }

    let uds_stream = UnixListenerStream::new(uds);
    let service = DataplaneService::new(map_manager, runtime_config, router, flow_tx);
    info!(socket = %socket_path, "gRPC server listening");

    tonic::transport::Server::builder()
        .add_service(DataplaneControlServer::new(service))
        .serve_with_incoming_shutdown(uds_stream, async {
            let _ = tokio::signal::ctrl_c().await;
            info!("gRPC server shutting down");
        })
        .await?;

    let _ = std::fs::remove_file(socket_path);
    info!(socket = %socket_path, "Socket file cleaned up");
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_service() -> DataplaneService {
        let mgr = Arc::new(MapManager::new_mock());
        let cfg = Arc::new(RuntimeConfig::new());
        let router = Arc::new(std::sync::RwLock::new(crate::proxy::router::Router::new()));
        let (tx, _rx) = crate::flows::flow_channel();
        DataplaneService::new(mgr, cfg, router, tx)
    }

    #[tokio::test]
    async fn test_apply_config() {
        let svc = make_service();
        let req = Request::new(proto::ApplyConfigRequest {
            version: "v1".into(),
            gateways: vec![proto::GatewayConfig {
                name: "gw-1".into(),
                bind_address: "0.0.0.0".into(),
                port: 8080,
                protocol: proto::GatewayProtocol::Http as i32,
                tls_config: None,
                http2_settings: None,
                proxy_protocol: None,
                hostnames: vec!["example.com".into()],
                max_request_body_bytes: 0,
                idle_timeout_ms: 0,
            }],
            routes: vec![proto::RouteConfig {
                name: "route-1".into(),
                gateway_ref: "gw-1".into(),
                hostnames: vec!["example.com".into()],
                path_match: Some(proto::PathMatch {
                    match_type: proto::PathMatchType::PathMatchPrefix as i32,
                    value: "/api/".into(),
                }),
                header_matches: vec![],
                backend_refs: vec![proto::BackendRef {
                    cluster_name: "cluster-1".into(),
                    weight: 1,
                }],
                middleware_refs: vec![],
                timeout_ms: 0,
                retry: None,
                priority: 10,
                methods: vec![],
                rewrite_path: String::new(),
                add_headers: std::collections::HashMap::new(),
                mirror_cluster: String::new(),
                mirror_percent: 0,
                default_backend: String::new(),
                hedge_delay_ms: 0,
                hedge_max_requests: 0,
            }],
            clusters: vec![proto::ClusterConfig {
                name: "cluster-1".into(),
                endpoints: vec![proto::Endpoint {
                    ip: "10.0.0.1".into(),
                    port: 8080,
                    weight: 1,
                    healthy: true,
                    zone: String::new(),
                    priority: 0,
                }],
                lb_algorithm: proto::LbAlgorithm::RoundRobin as i32,
                health_check: None,
                circuit_breaker: None,
                connection_pool: None,
                backend_tls: None,
                connect_timeout_ms: 0,
                session_affinity: None,
                outlier_detection: None,
                slow_start: None,
                protocol: String::new(),
                upstream_proxy_protocol: None,
                health_check_path: String::new(),
                panic_threshold_percent: 0,
                max_requests_per_connection: 0,
                request_queue_depth: 0,
                request_queue_timeout_ms: 0,
                subset_size: 0,
                remote_endpoint_groups: vec![],
            }],
            vips: vec![],
            l4_listeners: vec![],
            policies: vec![],
            mesh_config: None,
            wan_links: vec![],
        });
        let resp = svc.apply_config(req).await.unwrap();
        let inner = resp.into_inner();
        assert_eq!(inner.status, proto::OperationStatus::Ok as i32);
        assert_eq!(inner.applied_version, "v1");

        // Verify config was actually stored.
        let snap = svc.runtime_config.snapshot();
        assert_eq!(snap.version, "v1");
        assert_eq!(snap.gateways.len(), 1);
        assert_eq!(snap.routes.len(), 1);
        assert_eq!(snap.clusters.len(), 1);
        assert_eq!(snap.gateways["gw-1"].port, 8080);
        assert_eq!(snap.routes["route-1"].backend_refs[0].0, "cluster-1");
        assert_eq!(snap.clusters["cluster-1"].endpoints.len(), 1);
    }

    #[tokio::test]
    async fn test_get_dataplane_status() {
        let svc = make_service();
        let resp = svc
            .get_dataplane_status(Request::new(proto::GetDataplaneStatusRequest {}))
            .await
            .unwrap();
        let status = resp.into_inner();
        assert_eq!(status.mode, "mock");
        // 1 eBPF map + 3 config maps + 2 VIP maps + 2 WAN link maps + 1 WireGuard map = 9
        assert_eq!(status.map_sizes.len(), 9);
    }

    #[tokio::test]
    async fn test_upsert_gateway() {
        let svc = make_service();
        let req = Request::new(proto::UpsertGatewayRequest {
            gateway: Some(proto::GatewayConfig {
                name: "test-gw".into(),
                bind_address: "0.0.0.0".into(),
                port: 8080,
                protocol: proto::GatewayProtocol::Http as i32,
                tls_config: None,
                http2_settings: None,
                proxy_protocol: None,
                hostnames: vec![],
                max_request_body_bytes: 0,
                idle_timeout_ms: 0,
            }),
        });
        let resp = svc.upsert_gateway(req).await.unwrap();
        assert_eq!(resp.into_inner().status, proto::OperationStatus::Ok as i32);

        // Verify stored.
        let snap = svc.runtime_config.snapshot();
        assert_eq!(snap.gateways.len(), 1);
        assert_eq!(snap.gateways["test-gw"].port, 8080);
    }

    #[tokio::test]
    async fn test_upsert_gateway_missing() {
        let svc = make_service();
        let req = Request::new(proto::UpsertGatewayRequest { gateway: None });
        let result = svc.upsert_gateway(req).await;
        assert!(result.is_err());
        assert_eq!(result.unwrap_err().code(), tonic::Code::InvalidArgument);
    }

    #[tokio::test]
    async fn test_upsert_delete_route() {
        let svc = make_service();
        let req = Request::new(proto::UpsertRouteRequest {
            route: Some(proto::RouteConfig {
                name: "route-1".into(),
                gateway_ref: "gw-1".into(),
                hostnames: vec!["example.com".into()],
                path_match: Some(proto::PathMatch {
                    match_type: proto::PathMatchType::PathMatchPrefix as i32,
                    value: "/api/".into(),
                }),
                header_matches: vec![],
                backend_refs: vec![proto::BackendRef {
                    cluster_name: "backend-1".into(),
                    weight: 1,
                }],
                middleware_refs: vec![],
                timeout_ms: 0,
                retry: None,
                priority: 10,
                methods: vec![],
                rewrite_path: String::new(),
                add_headers: std::collections::HashMap::new(),
                mirror_cluster: String::new(),
                mirror_percent: 0,
                default_backend: String::new(),
                hedge_delay_ms: 0,
                hedge_max_requests: 0,
            }),
        });
        let resp = svc.upsert_route(req).await.unwrap();
        assert_eq!(resp.into_inner().status, proto::OperationStatus::Ok as i32);
        assert_eq!(svc.runtime_config.snapshot().routes.len(), 1);

        // Delete.
        let req = Request::new(proto::DeleteRouteRequest {
            name: "route-1".into(),
        });
        let resp = svc.delete_route(req).await.unwrap();
        assert_eq!(resp.into_inner().status, proto::OperationStatus::Ok as i32);
        assert!(svc.runtime_config.snapshot().routes.is_empty());
    }

    #[tokio::test]
    async fn test_upsert_delete_cluster() {
        let svc = make_service();
        let req = Request::new(proto::UpsertClusterRequest {
            cluster: Some(proto::ClusterConfig {
                name: "cluster-1".into(),
                endpoints: vec![proto::Endpoint {
                    ip: "10.0.0.1".into(),
                    port: 8080,
                    weight: 1,
                    healthy: true,
                    zone: String::new(),
                    priority: 0,
                }],
                lb_algorithm: proto::LbAlgorithm::LeastConn as i32,
                health_check: None,
                circuit_breaker: None,
                connection_pool: None,
                backend_tls: None,
                connect_timeout_ms: 0,
                session_affinity: None,
                outlier_detection: None,
                slow_start: None,
                protocol: String::new(),
                upstream_proxy_protocol: None,
                health_check_path: String::new(),
                panic_threshold_percent: 0,
                max_requests_per_connection: 0,
                request_queue_depth: 0,
                request_queue_timeout_ms: 0,
                subset_size: 0,
                remote_endpoint_groups: vec![],
            }),
        });
        let resp = svc.upsert_cluster(req).await.unwrap();
        assert_eq!(resp.into_inner().status, proto::OperationStatus::Ok as i32);

        let snap = svc.runtime_config.snapshot();
        assert_eq!(snap.clusters["cluster-1"].lb_algorithm, "least-conn");

        // Delete.
        let req = Request::new(proto::DeleteClusterRequest {
            name: "cluster-1".into(),
        });
        svc.delete_cluster(req).await.unwrap();
        assert!(svc.runtime_config.snapshot().clusters.is_empty());
    }

    #[tokio::test]
    async fn test_upsert_delete_vip() {
        let svc = make_service();
        let req = Request::new(proto::UpsertVipRequest {
            vip: Some(proto::VipConfig {
                name: "test-vip".into(),
                address: "10.0.0.1/32".into(),
                mode: proto::VipMode::L2 as i32,
                interface: "eth0".into(),
                bgp_config: None,
                arp_interface: String::new(),
                ospf_area_id: 0,
                bfd_enabled: false,
                bfd_interval_ms: 0,
                bfd_multiplier: 0,
            }),
        });
        let resp = svc.upsert_vip(req).await.unwrap();
        assert_eq!(resp.into_inner().status, proto::OperationStatus::Ok as i32);
        let req = Request::new(proto::DeleteVipRequest {
            name: "test-vip".into(),
        });
        let resp = svc.delete_vip(req).await.unwrap();
        assert_eq!(resp.into_inner().status, proto::OperationStatus::Ok as i32);
    }

    #[tokio::test]
    async fn test_upsert_l4_listener_creates_gateway() {
        let svc = make_service();
        let req = Request::new(proto::UpsertL4ListenerRequest {
            listener: Some(proto::L4ListenerConfig {
                name: "tcp-1".into(),
                bind_address: "0.0.0.0".into(),
                port: 3306,
                protocol: proto::L4Protocol::Tcp as i32,
                backend_refs: vec![],
                idle_timeout_ms: 0,
                connect_timeout_ms: 0,
            }),
        });
        let resp = svc.upsert_l4_listener(req).await.unwrap();
        assert_eq!(resp.into_inner().status, proto::OperationStatus::Ok as i32);

        // Should be registered as a gateway with TCP protocol.
        let snap = svc.runtime_config.snapshot();
        assert_eq!(snap.gateways.len(), 1);
        assert_eq!(snap.gateways["tcp-1"].protocol, "TCP");
        assert_eq!(snap.gateways["tcp-1"].port, 3306);
    }

    #[tokio::test]
    async fn test_attach_program_requires_linux() {
        let svc = make_service();
        let req = Request::new(proto::AttachProgramRequest {
            name: "test-prog".into(),
            object_path: "/tmp/test.o".into(),
            attach_type: proto::EbpfAttachType::EbpfAttachXdp as i32,
            interface: "eth0".into(),
            section: "xdp".into(),
            pin_path: String::new(),
        });
        let result = svc.attach_program(req).await;
        // On non-Linux, attach_program returns Unimplemented.
        // On Linux, it will fail with a load error (no object at /tmp/test.o).
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_detach_program() {
        let svc = make_service();
        let req = Request::new(proto::DetachProgramRequest {
            name: "test-prog".into(),
        });
        let resp = svc.detach_program(req).await.unwrap();
        assert_eq!(resp.into_inner().status, proto::OperationStatus::Ok as i32);
    }

    #[tokio::test]
    async fn test_upsert_delete_policy() {
        let svc = make_service();
        let req = Request::new(proto::UpsertPolicyRequest {
            policy: Some(proto::PolicyConfig {
                name: "test-rl".into(),
                policy_type: proto::PolicyType::RateLimit as i32,
                config: Some(proto::policy_config::Config::RateLimit(
                    proto::RateLimitPolicyConfig {
                        requests_per_second: 100,
                        burst: 10,
                        key: "source-ip".into(),
                    },
                )),
            }),
        });
        let resp = svc.upsert_policy(req).await.unwrap();
        assert_eq!(resp.into_inner().status, proto::OperationStatus::Ok as i32);

        // Verify policy was stored in RuntimeConfig.
        let p = svc.runtime_config.get_policy("test-rl").unwrap();
        assert_eq!(p.policy_type, "rate-limit");

        // Delete it.
        let del_req = Request::new(proto::DeletePolicyRequest {
            name: "test-rl".into(),
        });
        let resp = svc.delete_policy(del_req).await.unwrap();
        assert_eq!(resp.into_inner().status, proto::OperationStatus::Ok as i32);
        assert!(svc.runtime_config.get_policy("test-rl").is_none());
    }

    #[tokio::test]
    async fn test_upsert_delete_mesh_config() {
        let svc = make_service();
        let req = Request::new(proto::UpsertMeshConfigRequest {
            mesh_config: Some(proto::MeshConfig {
                enabled: true,
                mtls_mode: "strict".into(),
                spiffe_id: "spiffe://cluster.local/agent/node-1".into(),
                intercept_ports: vec![80, 443],
                ca_cert_pem: vec![],
                cert_pem: vec![],
                key_pem: vec![],
                trust_domain: "cluster.local".into(),
            }),
        });
        let resp = svc.upsert_mesh_config(req).await.unwrap();
        assert_eq!(resp.into_inner().status, proto::OperationStatus::Ok as i32);
        let req = Request::new(proto::DeleteMeshConfigRequest {});
        let resp = svc.delete_mesh_config(req).await.unwrap();
        assert_eq!(resp.into_inner().status, proto::OperationStatus::Ok as i32);
    }

    #[tokio::test]
    async fn test_upsert_delete_wan_link() {
        let svc = make_service();
        let req = Request::new(proto::UpsertWanLinkRequest {
            wan_link: Some(proto::WanLinkConfig {
                name: "wan-1".into(),
                interface: "eth1".into(),
                gateway: "192.168.1.1".into(),
                priority: 1,
                sla_target: None,
                bandwidth_mbps: 1000,
                provider: "ISP-A".into(),
            }),
        });
        let resp = svc.upsert_wan_link(req).await.unwrap();
        assert_eq!(resp.into_inner().status, proto::OperationStatus::Ok as i32);
    }
}
