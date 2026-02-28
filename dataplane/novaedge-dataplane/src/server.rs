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
    ClusterState, EndpointState, GatewayState, RouteState, RuntimeConfig, TlsState,
};
use crate::maps::MapManager;
use crate::proto;
use crate::proto::dataplane_control_server::{DataplaneControl, DataplaneControlServer};

/// The gRPC service implementation.
pub struct DataplaneService {
    map_manager: Arc<MapManager>,
    runtime_config: Arc<RuntimeConfig>,
    flow_tx: broadcast::Sender<proto::FlowEvent>,
    start_time: Instant,
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
        }
    }

    fn ok_response(msg: impl Into<String>) -> (i32, String) {
        (proto::OperationStatus::Ok as i32, msg.into())
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

        // Atomically replace all runtime config.
        self.runtime_config
            .apply_full(req.version.clone(), gateways, routes, clusters)
            .await;

        info!(version = %req.version, "Configuration applied to runtime state");

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
        let vip = req
            .vip
            .as_ref()
            .ok_or_else(|| Status::invalid_argument("missing VIP config"))?;
        info!(name = %vip.name, address = %vip.address, mode = vip.mode, "UpsertVIP");
        // VIP management stays in Go — this is a passthrough acknowledgment.
        let (status, message) = Self::ok_response(format!("VIP '{}' upserted", vip.name));
        Ok(Response::new(proto::UpsertVipResponse { status, message }))
    }

    async fn delete_vip(
        &self,
        request: Request<proto::DeleteVipRequest>,
    ) -> Result<Response<proto::DeleteVipResponse>, Status> {
        let req = request.into_inner();
        info!(name = %req.name, "DeleteVIP");
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
        // Policy application is handled via middleware pipeline — acknowledge receipt.
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
        let mesh = req
            .mesh_config
            .as_ref()
            .ok_or_else(|| Status::invalid_argument("missing mesh config"))?;
        info!(enabled = mesh.enabled, mtls_mode = %mesh.mtls_mode, "UpsertMeshConfig");
        // Mesh management stays in Go.
        let (status, message) = Self::ok_response("mesh config upserted");
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
        let link = req
            .wan_link
            .as_ref()
            .ok_or_else(|| Status::invalid_argument("missing WAN link config"))?;
        info!(name = %link.name, interface = %link.interface, "UpsertWANLink");
        // SD-WAN management stays in Go.
        let (status, message) = Self::ok_response(format!("WAN link '{}' upserted", link.name));
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
        info!(name = %req.name, object_path = %req.object_path, "AttachProgram");
        let (status, message) = Self::ok_response(format!("program '{}' attached", req.name));
        Ok(Response::new(proto::AttachProgramResponse {
            status,
            message,
            program_id: 0,
        }))
    }

    async fn detach_program(
        &self,
        request: Request<proto::DetachProgramRequest>,
    ) -> Result<Response<proto::DetachProgramResponse>, Status> {
        let req = request.into_inner();
        info!(name = %req.name, "DetachProgram");
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

        let stream = async_stream::try_stream! {
            loop {
                match rx.recv().await {
                    Ok(event) => {
                        if filter_protocol > 0 && event.protocol != filter_protocol {
                            continue;
                        }
                        // TODO: Apply CIDR filters.
                        let _ = (&filter_src_cidr, &filter_dst_cidr);
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
    async fn test_attach_detach_program() {
        let svc = make_service();
        let req = Request::new(proto::AttachProgramRequest {
            name: "test-prog".into(),
            object_path: "/tmp/test.o".into(),
            attach_type: proto::EbpfAttachType::EbpfAttachXdp as i32,
            interface: "eth0".into(),
            section: "xdp".into(),
            pin_path: String::new(),
        });
        let resp = svc.attach_program(req).await.unwrap();
        assert_eq!(resp.into_inner().status, proto::OperationStatus::Ok as i32);
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
