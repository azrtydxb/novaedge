//! NovaEdge dataplane daemon — Rust forwarding plane.
//!
//! Receives configuration from the Go agent via gRPC and manages
//! L4/L7 forwarding, eBPF programs, and VIP operations.

use clap::Parser;
use tracing::{info, warn};

#[allow(dead_code)]
mod loader;
mod maps;
mod server;

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

    // Create map manager (mock on non-Linux or standalone mode).
    let map_manager = if args.standalone || cfg!(not(target_os = "linux")) {
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
                Ok(mgr) => mgr,
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

    // Start gRPC server.
    server::run(map_manager, &args.socket).await?;

    info!("novaedge-dataplane shutdown complete");
    Ok(())
}
