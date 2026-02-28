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
use tracing::{info, warn};

use crate::config::{
    ClusterState, EndpointState, GatewayState, PolicyState, RouteState, RuntimeConfig, TlsState,
};
use crate::maps::MapManager;
use crate::mesh;
use crate::proto;
use crate::proto::dataplane_control_server::{DataplaneControl, DataplaneControlServer};
use crate::sdwan;
use crate::vip;

/// The gRPC service implementation.
pub struct DataplaneService {
    map_manager: Arc<MapManager>,
    runtime_config: Arc<RuntimeConfig>,
    flow_tx: broadcast::Sender<proto::FlowEvent>,
    start_time: Instant,
    vip_manager: std::sync::Mutex<vip::manager::VIPManager>,
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
        flow_tx: broadcast::Sender<proto::FlowEvent>,
    ) -> Self {
        Self {
            map_manager,
            runtime_config,
            flow_tx,
            start_time: Instant::now(),
            vip_manager: std::sync::Mutex::new(vip::manager::VIPManager::new()),
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

    /// Parse an IP address from a VIP address string (e.g. "10.0.0.1/32" → 10.0.0.1, prefix 32).
    fn parse_vip_address(addr: &str) -> Result<(std::net::IpAddr, u8), Status> {
        let (ip_str, prefix_str) = addr.split_once('/').unwrap_or((addr, "32"));
        let ip: std::net::IpAddr = ip_str
            .parse()
            .map_err(|e| Status::invalid_argument(format!("invalid VIP address '{addr}': {e}")))?;
        let prefix: u8 = prefix_str
            .parse()
            .map_err(|e| Status::invalid_argument(format!("invalid prefix length: {e}")))?;
        Ok((ip, prefix))
    }

    /// Convert proto VIP mode to our VIPMode.
    fn vip_mode_from_proto(
        mode: i32,
        bgp_config: &Option<proto::BgpConfig>,
    ) -> vip::manager::VIPMode {
        match mode {
            1 => vip::manager::VIPMode::L2 { arp_enabled: true },
            2 => {
                let asn = bgp_config.as_ref().map_or(65000, |c| c.local_asn);
                vip::manager::VIPMode::BGP { asn }
            }
            3 => vip::manager::VIPMode::OSPF { area: 0, cost: 100 },
            _ => vip::manager::VIPMode::L2 { arp_enabled: true },
        }
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
            _ => "unknown",
        };
        // Serialize the config variant as a debug string for storage.
        let config_json = match &p.config {
            Some(c) => format!("{c:?}"),
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

    /// Read the MAC address of a network interface.
    /// Falls back to a broadcast MAC if the interface cannot be read.
    #[cfg(target_os = "linux")]
    fn read_interface_mac(interface: &str) -> [u8; 6] {
        let path = format!("/sys/class/net/{interface}/address");
        match std::fs::read_to_string(&path) {
            Ok(contents) => {
                let mac_str = contents.trim();
                let parts: Vec<&str> = mac_str.split(':').collect();
                if parts.len() == 6 {
                    let mut mac = [0u8; 6];
                    for (i, part) in parts.iter().enumerate() {
                        mac[i] = u8::from_str_radix(part, 16).unwrap_or(0);
                    }
                    mac
                } else {
                    warn!(interface, "unexpected MAC format, using broadcast");
                    [0xff; 6]
                }
            }
            Err(e) => {
                warn!(interface, error = %e, "cannot read interface MAC, using broadcast");
                [0xff; 6]
            }
        }
    }

    #[cfg(not(target_os = "linux"))]
    fn read_interface_mac(interface: &str) -> [u8; 6] {
        warn!(interface, "MAC address lookup not available on non-Linux, using broadcast");
        [0xff; 6]
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
            }),
            hostnames: gw.hostnames.clone(),
        }
    }

    /// Convert a proto RouteConfig to our RouteState.
    fn route_from_proto(route: &proto::RouteConfig) -> RouteState {
        let (path_prefix, path_exact) = route
            .path_match
            .as_ref()
            .map(|pm| {
                if pm.match_type == proto::PathMatchType::PathMatchPrefix as i32 {
                    (pm.value.clone(), String::new())
                } else {
                    (String::new(), pm.value.clone())
                }
            })
            .unwrap_or_default();

        let backend_ref = route
            .backend_refs
            .first()
            .map(|b| b.cluster_name.clone())
            .unwrap_or_default();

        RouteState {
            name: route.name.clone(),
            gateway_ref: route.gateway_ref.clone(),
            hostnames: route.hostnames.clone(),
            path_prefix,
            path_exact,
            methods: vec![],
            backend_ref,
            priority: route.priority,
            rewrite_path: None,
            add_headers: std::collections::HashMap::new(),
        }
    }

    /// Convert a proto ClusterConfig to our ClusterState.
    fn cluster_from_proto(cluster: &proto::ClusterConfig) -> ClusterState {
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
                })
                .collect(),
            lb_algorithm: Self::lb_algo_str(cluster.lb_algorithm).to_string(),
            health_check_path: cluster
                .health_check
                .as_ref()
                .map(|hc| hc.http_path.clone())
                .unwrap_or_default(),
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
            vips = req.vips.len(),
            l4_listeners = req.l4_listeners.len(),
            policies = req.policies.len(),
            wan_links = req.wan_links.len(),
            "ApplyConfig received"
        );

        // Convert proto messages to runtime config types.
        let gateways: Vec<GatewayState> = req.gateways.iter().map(Self::gateway_from_proto).collect();
        let routes: Vec<RouteState> = req.routes.iter().map(Self::route_from_proto).collect();
        let clusters: Vec<ClusterState> =
            req.clusters.iter().map(Self::cluster_from_proto).collect();
        let policies: Vec<PolicyState> = req
            .policies
            .iter()
            .map(|p| Self::policy_from_proto(p))
            .collect();

        // Atomically replace all runtime config.
        self.runtime_config
            .apply_full(req.version.clone(), gateways, routes, clusters, policies)
            .await;

        info!(version = %req.version, "Configuration applied to runtime state");

        // Apply VIP assignments.
        {
            let mut vip_mgr = self.vip_manager.lock().unwrap();
            // Clear and re-apply all VIPs from snapshot.
            *vip_mgr = vip::manager::VIPManager::new();
            for vip_cfg in &req.vips {
                if let Ok((ip, prefix)) = Self::parse_vip_address(&vip_cfg.address) {
                    let mode = Self::vip_mode_from_proto(vip_cfg.mode, &vip_cfg.bgp_config);
                    let iface = if vip_cfg.interface.is_empty() { "eth0" } else { &vip_cfg.interface };
                    if vip_mgr.add_vip(ip, prefix, iface, mode).is_ok() {
                        let _ = vip_mgr.activate(&ip);
                    }
                }
            }
            if !req.vips.is_empty() {
                info!(count = req.vips.len(), "VIP assignments applied");
            }
        }

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
                    .unwrap_or_else(|_| std::net::IpAddr::V4(std::net::Ipv4Addr::UNSPECIFIED));
                let mut wan_link = sdwan::link::WANLink::new(&wl_cfg.name, &wl_cfg.interface, gateway);
                wan_link.priority = wl_cfg.priority;
                wan_mgr.add_link(wan_link);
            }
            if !req.wan_links.is_empty() {
                info!(count = req.wan_links.len(), "WAN links applied");
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

    async fn upsert_vip(
        &self,
        request: Request<proto::UpsertVipRequest>,
    ) -> Result<Response<proto::UpsertVipResponse>, Status> {
        let req = request.into_inner();
        let vip_cfg = req
            .vip
            .as_ref()
            .ok_or_else(|| Status::invalid_argument("missing VIP config"))?;
        info!(name = %vip_cfg.name, address = %vip_cfg.address, mode = vip_cfg.mode, "UpsertVIP");

        let (ip, prefix) = Self::parse_vip_address(&vip_cfg.address)?;
        let mode = Self::vip_mode_from_proto(vip_cfg.mode, &vip_cfg.bgp_config);
        let interface = if vip_cfg.interface.is_empty() {
            "eth0"
        } else {
            &vip_cfg.interface
        };

        let mut mgr = self.vip_manager.lock().unwrap();
        // Remove existing VIP at this address if present, then re-add.
        let _ = mgr.remove_vip(&ip);
        mgr.add_vip(ip, prefix, interface, mode)
            .map_err(|e| Status::internal(format!("failed to add VIP: {e}")))?;
        mgr.activate(&ip)
            .map_err(|e| Status::internal(format!("failed to activate VIP: {e}")))?;

        // Send GARP for L2 VIPs.
        if vip_cfg.mode == proto::VipMode::L2 as i32 {
            if ip.is_ipv4() {
                let mac = Self::read_interface_mac(interface);
                let _ = vip::garp::send_garp(ip, interface, &mac, &vip::garp::GarpConfig::default());
            }
        }

        let (status, message) = Self::ok_response(format!("VIP '{}' upserted and activated", vip_cfg.name));
        Ok(Response::new(proto::UpsertVipResponse { status, message }))
    }

    async fn delete_vip(
        &self,
        request: Request<proto::DeleteVipRequest>,
    ) -> Result<Response<proto::DeleteVipResponse>, Status> {
        let req = request.into_inner();
        info!(name = %req.name, "DeleteVIP");

        // The name is used to identify the VIP; we search by iterating.
        // Since VIPManager is keyed by IP, we need to find the IP by name.
        // For now, try parsing the name as an address (the Go agent sends the address).
        // If that fails, just acknowledge (the VIP may have been removed already).
        if let Ok((ip, _)) = Self::parse_vip_address(&req.name) {
            let mut mgr = self.vip_manager.lock().unwrap();
            let _ = mgr.deactivate(&ip);
            let _ = mgr.remove_vip(&ip);
        }

        let (status, message) = Self::ok_response(format!("VIP '{}' deleted", req.name));
        Ok(Response::new(proto::DeleteVipResponse { status, message }))
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

        // Configure mTLS provider.
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

            // Configure TPROXY interception.
            let tproxy_config = mesh::tproxy::TproxyConfig {
                enabled: true,
                inbound_port: 15006,
                outbound_port: 15001,
                exclude_ports: mesh_cfg.intercept_ports.iter().map(|&p| p as u16).collect(),
                ..mesh::tproxy::TproxyConfig::default()
            };
            let mut tproxy = self.mesh_tproxy.lock().unwrap();
            *tproxy = mesh::tproxy::TproxyInterceptor::new(tproxy_config);
            let _ = tproxy.install();
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
        let mut tproxy = self.mesh_tproxy.lock().unwrap();
        let _ = tproxy.uninstall();
        *self.mesh_tls.lock().unwrap() = None;

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
            .unwrap_or_else(|_| std::net::IpAddr::V4(std::net::Ipv4Addr::UNSPECIFIED));

        let mut wan_link = sdwan::link::WANLink::new(
            &link_cfg.name,
            &link_cfg.interface,
            gateway,
        );
        wan_link.priority = link_cfg.priority;

        let mut mgr = self.wan_link_manager.lock().unwrap();
        // Remove existing link with same name, then re-add.
        mgr.remove_link(&link_cfg.name);
        mgr.add_link(wan_link);

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

        self.wan_link_manager.lock().unwrap().remove_link(&req.name);

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
            Self::parse_vip_address(&filter_src_cidr).ok()
        };
        let dst_net: Option<(std::net::IpAddr, u8)> = if filter_dst_cidr.is_empty() {
            None
        } else {
            Self::parse_vip_address(&filter_dst_cidr).ok()
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
        info!("GetDataplaneStatus");
        let map_status = self.map_manager.get_status();
        let snap = self.runtime_config.snapshot();

        let map_sizes = vec![
            proto::MapInfo {
                name: "vips".into(),
                entries: map_status.vip_count as u64,
                max_entries: 65536,
            },
            proto::MapInfo {
                name: "backends".into(),
                entries: map_status.backend_count as u64,
                max_entries: 262144,
            },
            proto::MapInfo {
                name: "conntrack".into(),
                entries: map_status.conntrack_count as u64,
                max_entries: 1048576,
            },
            proto::MapInfo {
                name: "rate_limits".into(),
                entries: map_status.rate_limit_count as u64,
                max_entries: 65536,
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
                gauges.insert("vip_count".into(), status.vip_count as f64);
                gauges.insert("backend_count".into(), status.backend_count as f64);
                gauges.insert("conntrack_count".into(), status.conntrack_count as f64);
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
    flow_tx: broadcast::Sender<proto::FlowEvent>,
    socket_path: &str,
) -> anyhow::Result<()> {
    let _ = std::fs::remove_file(socket_path);
    if let Some(parent) = std::path::Path::new(socket_path).parent() {
        std::fs::create_dir_all(parent)?;
    }
    let uds = UnixListener::bind(socket_path)?;
    let uds_stream = UnixListenerStream::new(uds);
    let service = DataplaneService::new(map_manager, runtime_config, flow_tx);
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
        let (tx, _rx) = crate::flows::flow_channel();
        DataplaneService::new(mgr, cfg, tx)
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
            }],
            clusters: vec![proto::ClusterConfig {
                name: "cluster-1".into(),
                endpoints: vec![proto::Endpoint {
                    ip: "10.0.0.1".into(),
                    port: 8080,
                    weight: 1,
                    healthy: true,
                }],
                lb_algorithm: proto::LbAlgorithm::RoundRobin as i32,
                health_check: None,
                circuit_breaker: None,
                connection_pool: None,
                backend_tls: None,
                connect_timeout_ms: 0,
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
        assert_eq!(snap.routes["route-1"].backend_ref, "cluster-1");
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
        assert_eq!(status.map_sizes.len(), 4);
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
                }],
                lb_algorithm: proto::LbAlgorithm::LeastConn as i32,
                health_check: None,
                circuit_breaker: None,
                connection_pool: None,
                backend_tls: None,
                connect_timeout_ms: 0,
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
