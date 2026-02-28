//! NovaEdge dataplane daemon — Rust forwarding plane.
//!
//! Receives configuration from the Go agent via gRPC and manages
//! L4/L7 forwarding, eBPF programs, and VIP operations.

// Modules are scaffolded but not yet fully wired into main().
// Allow dead code until integration is complete.
#![allow(dead_code, unused_imports)]

use std::sync::Arc;

use clap::Parser;
use tracing::{info, warn};

mod flows;
mod health;
mod l4;
mod lb;
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
                            if let Err(e) =
                                loader::attach_xdp(bpf, "novaedge_xdp", &args.interface)
                            {
                                warn!("Failed to attach XDP L4 LB program: {e:#}");
                            }
                            if let Err(e) =
                                loader::attach_xdp(bpf, "novaedge_arp", &args.interface)
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

    // Create flow event broadcast channel.
    let (flow_tx, _flow_rx) = flows::flow_channel();

    // Spawn the flow reader task.
    let flow_tx_clone = flow_tx.clone();
    let flow_reader_handle = tokio::spawn(async move {
        flows::run_flow_reader(flow_tx_clone, use_mock).await;
    });

    // Start gRPC server (blocks until shutdown signal).
    let socket_path = args.socket.clone();
    let server_result = server::run(map_manager, flow_tx, &args.socket).await;

    // Abort the flow reader task on server shutdown.
    flow_reader_handle.abort();
    let _ = flow_reader_handle.await;

    // Clean up socket file.
    let _ = std::fs::remove_file(&socket_path);

    info!("novaedge-dataplane shutdown complete");

    server_result
}
