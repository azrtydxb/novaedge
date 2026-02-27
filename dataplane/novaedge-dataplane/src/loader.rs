//! eBPF program loader using aya.
//!
//! Loads compiled eBPF programs from an object file and extracts map handles.

use crate::maps::MapManager;

/// Load eBPF programs from the compiled object file and return a MapManager.
#[cfg(target_os = "linux")]
pub fn load_ebpf(path: &str) -> anyhow::Result<MapManager> {
    use aya::Ebpf;
    use tracing::info;

    info!(path = %path, "Loading eBPF programs");

    let _bpf = Ebpf::load_file(path)?;

    // TODO: Extract map handles and attach programs (Phase 2).
    // For now, return mock maps as the eBPF programs aren't written yet.
    Ok(MapManager::new_mock())
}

/// Stub for non-Linux platforms.
#[cfg(not(target_os = "linux"))]
pub fn load_ebpf(_path: &str) -> anyhow::Result<MapManager> {
    anyhow::bail!("eBPF loading is only supported on Linux")
}
