//! eBPF program loader using aya.
//!
//! Loads compiled eBPF programs from an object file, extracts map handles,
//! and returns a `MapManager` (and optionally a ring buffer handle for flow
//! event streaming).

use crate::maps::MapManager;

/// Result of loading eBPF programs.
pub struct LoadResult {
    /// The map manager wrapping eBPF map handles.
    pub map_manager: MapManager,
    /// Whether real eBPF programs were loaded (vs mock fallback).
    pub is_real: bool,
}

/// Load eBPF programs from the compiled object file and return a `LoadResult`.
#[cfg(target_os = "linux")]
pub fn load_ebpf(path: &str) -> anyhow::Result<LoadResult> {
    use aya::Ebpf;
    use tracing::info;

    info!(path = %path, "Loading eBPF programs");
    let _bpf = Ebpf::load_file(path)?;

    // TODO(Phase 2): Extract map handles and attach XDP/TC programs.
    info!("eBPF programs loaded (maps not yet extracted, using mock)");
    Ok(LoadResult {
        map_manager: MapManager::new_mock(),
        is_real: false,
    })
}

/// Stub for non-Linux platforms.
#[cfg(not(target_os = "linux"))]
pub fn load_ebpf(_path: &str) -> anyhow::Result<LoadResult> {
    anyhow::bail!("eBPF loading is only supported on Linux")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_load_result_mock() {
        let result = LoadResult {
            map_manager: MapManager::new_mock(),
            is_real: false,
        };
        assert!(!result.is_real);
        assert_eq!(result.map_manager.mode(), "mock");
    }

    #[test]
    #[cfg(not(target_os = "linux"))]
    fn test_load_ebpf_fails_on_non_linux() {
        let result = load_ebpf("/nonexistent");
        assert!(result.is_err());
    }
}
