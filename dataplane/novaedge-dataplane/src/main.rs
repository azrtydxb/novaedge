//! NovaEdge dataplane daemon — Rust forwarding plane.
//!
//! Receives configuration from the Go agent via gRPC and manages
//! L4/L7 forwarding, eBPF programs, VIP management, service mesh,
//! and SD-WAN operations.

use std::sync::Arc;

use clap::Parser;
use tracing::{info, warn};

mod config;
mod flows;
mod health;
mod l4;
mod lb;
mod listener;
mod loader;
mod maps;
mod mesh;
mod middleware;
mod proto;
mod proxy;
mod sdwan;
mod server;
mod upstream;
mod vip;

/// NovaEdge Rust dataplane daemon.
#[derive(Parser, Debug)]
#[command(name = "novaedge-dataplane", about = "NovaEdge Rust forwarding plane")]
struct Args {
    /// Unix socket path for gRPC server.
    #[arg(long, default_value = "/run/novaedge/dataplane.sock")]
    socket: String,

    /// Run in standalone mode (mock eBPF maps, no kernel interaction).
    #[arg(long)]
    standalone: bool,

    /// Path to compiled eBPF object file.
    #[arg(long, default_value = "/opt/novaedge/novaedge-ebpf")]
    ebpf_path: String,

    /// Network interface for eBPF program attachment.
    #[arg(long, default_value = "eth0")]
    interface: String,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    // Initialize tracing.
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info")),
        )
        .init();

    let args = Args::parse();
    info!(socket = %args.socket, standalone = args.standalone, "Starting novaedge-dataplane");

    // Determine whether to use mock or real eBPF maps.
    let use_mock = args.standalone || cfg!(not(target_os = "linux"));

    // Create map manager.
    let map_manager = if use_mock {
        if cfg!(not(target_os = "linux")) {
            warn!("Non-Linux platform detected, using mock maps");
        } else {
            info!("Standalone mode: using mock maps");
        }
        maps::MapManager::new_mock()
    } else {
        #[cfg(target_os = "linux")]
        {
            match loader::load_ebpf(&args.ebpf_path) {
                Ok(mut result) => {
                    if result.is_real {
                        info!("eBPF programs loaded successfully");

                        // Attach eBPF programs to the network interface.
                        if let Some(ref mut bpf) = result.bpf {
                            if let Err(e) = loader::attach_xdp(bpf, "novaedge_xdp", &args.interface)
                            {
                                warn!("Failed to attach XDP program: {e:#}");
                            }
                            if let Err(e) = loader::attach_xdp(bpf, "novaedge_arp", &args.interface)
                            {
                                warn!("Failed to attach XDP ARP responder: {e:#}");
                            }
                            if let Err(e) =
                                loader::attach_tc(bpf, "novaedge_ratelimit", &args.interface)
                            {
                                warn!("Failed to attach TC rate limiter: {e:#}");
                            }
                        }
                    } else {
                        info!("eBPF loader returned mock maps (programs not yet available)");
                    }
                    result.map_manager
                }
                Err(e) => {
                    warn!("Failed to load eBPF programs: {e:#}, falling back to mock maps");
                    maps::MapManager::new_mock()
                }
            }
        }
        #[cfg(not(target_os = "linux"))]
        {
            unreachable!()
        }
    };

    info!("Map manager initialized: {}", map_manager.mode());

    let map_manager = Arc::new(map_manager);

    // Create runtime config store (shared between gRPC handlers and proxy).
    let runtime_config = Arc::new(config::RuntimeConfig::new());

    // Create upstream resilience components.
    let connection_pool = Arc::new(upstream::pool::ConnectionPool::new(
        upstream::pool::PoolConfig::default(),
    ));

    // Create the HTTP proxy handler.
    let router = Arc::new(std::sync::RwLock::new(proxy::router::Router::new()));
    let pool_for_cleanup = connection_pool.clone();
    let proxy_handler = Arc::new(proxy::handler::ProxyHandler::new(
        router.clone(),
        runtime_config.clone(),
        connection_pool,
    ));

    // Spawn periodic connection pool cleanup task.
    let pool_cleanup_handle = tokio::spawn(async move {
        let mut interval = tokio::time::interval(std::time::Duration::from_secs(60));
        loop {
            interval.tick().await;
            pool_for_cleanup.cleanup_idle().await;
        }
    });

    // Create the listener manager.
    let listener_mgr = Arc::new(listener::ListenerManager::new(
        runtime_config.clone(),
        proxy_handler,
    ));

    // Create shutdown signal for listener manager.
    let (shutdown_tx, shutdown_rx) = tokio::sync::watch::channel(false);

    // Spawn listener manager (watches config and starts/stops listeners).
    let listener_mgr_clone = listener_mgr.clone();
    let listener_handle = tokio::spawn(async move {
        listener_mgr_clone.run(shutdown_rx).await;
    });

    // Create the health checker with default TCP health check config.
    // It runs as a background task and periodically probes backend endpoints
    // discovered from the runtime config's cluster health check paths.
    // Health check results are fed back into RuntimeConfig to update endpoint health.
    let health_checker = std::sync::Arc::new(health::checker::HealthChecker::new(
        health::types::HealthCheckConfig::default(),
    ));
    let health_shutdown_rx = shutdown_tx.subscribe();
    let health_config = runtime_config.clone();
    let health_checker_clone = health_checker.clone();
    let health_handle = tokio::spawn(async move {
        let mut interval = tokio::time::interval(std::time::Duration::from_secs(30));
        let mut cancel = health_shutdown_rx;
        loop {
            tokio::select! {
                _ = interval.tick() => {
                    let snapshot = health_config.snapshot();
                    for (cluster_name, cluster) in &snapshot.clusters {
                        if cluster.health_check_path.is_empty() {
                            continue;
                        }
                        for ep in &cluster.endpoints {
                            if let Ok(addr) = ep.address.parse::<std::net::IpAddr>() {
                                let sock = std::net::SocketAddr::new(addr, ep.port as u16);
                                health_checker_clone.check(sock).await;
                                let healthy = health_checker_clone.is_healthy(&sock).await;
                                health_config.update_endpoint_health(
                                    cluster_name,
                                    &ep.address,
                                    ep.port,
                                    healthy,
                                );
                            }
                        }
                    }
                }
                _ = cancel.changed() => {
                    info!("Health checker shutting down");
                    break;
                }
            }
        }
    });
    let _health_checker_ref = health_checker;

    // Create flow event broadcast channel.
    let (flow_tx, _flow_rx) = flows::flow_channel();

    // Spawn the flow reader task.
    let flow_tx_clone = flow_tx.clone();
    let flow_reader_handle = tokio::spawn(async move {
        flows::run_flow_reader(flow_tx_clone, use_mock).await;
    });

    // Start gRPC server (blocks until shutdown signal).
    let socket_path = args.socket.clone();
    let server_result =
        server::run(map_manager, runtime_config, router, flow_tx, &args.socket).await;

    // Signal listener manager to shut down.
    let _ = shutdown_tx.send(true);
    listener_handle.abort();
    let _ = listener_handle.await;

    // Abort the flow reader, pool cleanup, and health checker tasks on server shutdown.
    flow_reader_handle.abort();
    let _ = flow_reader_handle.await;
    pool_cleanup_handle.abort();
    let _ = pool_cleanup_handle.await;
    health_handle.abort();
    let _ = health_handle.await;

    // Clean up socket file.
    let _ = std::fs::remove_file(&socket_path);

    info!("novaedge-dataplane shutdown complete");

    server_result
}
