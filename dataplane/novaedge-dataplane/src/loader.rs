//! eBPF program loader using aya.
//!
//! Loads compiled eBPF programs from an object file, extracts map handles,
//! and returns a `MapManager` (and optionally a ring buffer handle for flow
//! event streaming).

use crate::maps::MapManager;

/// Result of loading eBPF programs.
#[allow(dead_code)]
pub struct LoadResult {
    /// The map manager wrapping eBPF map handles.
    pub map_manager: MapManager,
    /// Whether real eBPF programs were loaded (vs mock fallback).
    pub is_real: bool,
    /// Ring buffer for flow events (Linux only, None in mock mode).
    #[cfg(target_os = "linux")]
    pub flow_ring_buf: Option<aya::maps::RingBuf<aya::maps::MapData>>,
    /// The loaded eBPF handle, needed for attaching programs to interfaces.
    #[cfg(target_os = "linux")]
    pub bpf: Option<aya::Ebpf>,
}

/// Load eBPF programs from the compiled object file and return a `LoadResult`.
///
/// Extracts all map handles (VIPs, backends, conntrack, rate limits, VIP
/// addresses), attaches the XDP and TC programs, and returns a
/// `MapManager` wrapping the real maps plus the flow-event ring buffer.
#[cfg(target_os = "linux")]
pub fn load_ebpf(path: &str) -> anyhow::Result<LoadResult> {
    use aya::Ebpf;
    use tracing::info;

    use std::cell::UnsafeCell;

    use crate::maps::RealMaps;

    info!(path = %path, "Loading eBPF programs");
    let mut bpf = Ebpf::load_file(path)?;

    // ── Extract map handles ───────────────────────────────────────────
    let vips = aya::maps::HashMap::try_from(
        bpf.take_map("VIPS")
            .ok_or_else(|| anyhow::anyhow!("map VIPS not found"))?,
    )?;
    let backends = aya::maps::HashMap::try_from(
        bpf.take_map("BACKENDS")
            .ok_or_else(|| anyhow::anyhow!("map BACKENDS not found"))?,
    )?;
    let conntrack = aya::maps::HashMap::try_from(
        bpf.take_map("CONNTRACK")
            .ok_or_else(|| anyhow::anyhow!("map CONNTRACK not found"))?,
    )?;
    let rate_limits = aya::maps::HashMap::try_from(
        bpf.take_map("RATE_LIMIT_STATE")
            .ok_or_else(|| anyhow::anyhow!("map RATE_LIMIT_STATE not found"))?,
    )?;
    let rate_limit_cfg = aya::maps::HashMap::try_from(
        bpf.take_map("RATE_LIMIT_CFG")
            .ok_or_else(|| anyhow::anyhow!("map RATE_LIMIT_CFG not found"))?,
    )?;
    let vip_addrs = aya::maps::HashMap::try_from(
        bpf.take_map("VIP_ADDRS")
            .ok_or_else(|| anyhow::anyhow!("map VIP_ADDRS not found"))?,
    )?;
    let flow_ring_buf = aya::maps::RingBuf::try_from(
        bpf.take_map("FLOW_EVENTS")
            .ok_or_else(|| anyhow::anyhow!("map FLOW_EVENTS not found"))?,
    )?;

    info!("eBPF maps extracted successfully");

    let maps = RealMaps {
        vips: UnsafeCell::new(vips),
        backends: UnsafeCell::new(backends),
        conntrack: UnsafeCell::new(conntrack),
        rate_limits: UnsafeCell::new(rate_limits),
        rate_limit_cfg: UnsafeCell::new(rate_limit_cfg),
        vip_addrs: UnsafeCell::new(vip_addrs),
    };

    Ok(LoadResult {
        map_manager: MapManager::new_real(maps),
        is_real: true,
        flow_ring_buf: Some(flow_ring_buf),
        bpf: Some(bpf),
    })
}

/// Attach the XDP programs to the specified network interface.
///
/// This is called separately from `load_ebpf` so the caller can choose
/// when and to which interface to attach.
#[cfg(target_os = "linux")]
pub fn attach_xdp(bpf: &mut aya::Ebpf, program_name: &str, interface: &str) -> anyhow::Result<()> {
    use aya::programs::{Xdp, XdpFlags};
    use tracing::info;

    let prog: &mut Xdp = bpf
        .program_mut(program_name)
        .ok_or_else(|| anyhow::anyhow!("XDP program '{program_name}' not found"))?
        .try_into()?;
    prog.load()?;
    prog.attach(interface, XdpFlags::default())?;
    info!(
        program = program_name,
        interface = interface,
        "XDP program attached"
    );
    Ok(())
}

/// Populate the `local_mac` BPF array map with the interface's hardware MAC address.
///
/// Each XDP program that does MAC rewriting has its own `local_mac` map
/// (named differently per program). This function writes the interface MAC
/// to the map at key 0 so the XDP program can use it as the source MAC.
#[cfg(target_os = "linux")]
pub fn populate_local_mac(
    bpf: &mut aya::Ebpf,
    map_name: &str,
    interface: &str,
) -> anyhow::Result<()> {
    use tracing::info;

    // Read the interface's hardware MAC address from sysfs.
    let mac_path = format!("/sys/class/net/{interface}/address");
    let mac_str = std::fs::read_to_string(&mac_path)
        .map_err(|e| anyhow::anyhow!("read MAC from {mac_path}: {e}"))?;
    let mac_str = mac_str.trim();

    // Parse "aa:bb:cc:dd:ee:ff" into [u8; 6].
    let mac_bytes: Vec<u8> = mac_str
        .split(':')
        .map(|s| u8::from_str_radix(s, 16))
        .collect::<Result<Vec<_>, _>>()
        .map_err(|e| anyhow::anyhow!("parse MAC '{mac_str}': {e}"))?;
    if mac_bytes.len() != 6 {
        anyhow::bail!("unexpected MAC length: {mac_str}");
    }
    let mut mac: [u8; 6] = [0u8; 6];
    mac.copy_from_slice(&mac_bytes);

    // Write to the BPF array map at key 0.
    let mut map: aya::maps::Array<_, [u8; 6]> = aya::maps::Array::try_from(
        bpf.map_mut(map_name)
            .ok_or_else(|| anyhow::anyhow!("map '{map_name}' not found"))?,
    )?;
    map.set(0, mac, 0)?;

    info!(
        interface = interface,
        map = map_name,
        mac = mac_str,
        "Populated local_mac BPF map"
    );
    Ok(())
}

/// Attach the TC classifier program to the specified network interface.
#[cfg(target_os = "linux")]
pub fn attach_tc(bpf: &mut aya::Ebpf, program_name: &str, interface: &str) -> anyhow::Result<()> {
    use aya::programs::{tc, SchedClassifier, TcAttachType};
    use tracing::info;

    // Add clsact qdisc (ignore error if already exists)
    let _ = tc::qdisc_add_clsact(interface);

    let prog: &mut SchedClassifier = bpf
        .program_mut(program_name)
        .ok_or_else(|| anyhow::anyhow!("TC program '{program_name}' not found"))?
        .try_into()?;
    prog.load()?;
    prog.attach(interface, TcAttachType::Ingress)?;
    info!(
        program = program_name,
        interface = interface,
        "TC program attached"
    );
    Ok(())
}

/// Stub for non-Linux platforms.
#[cfg(not(target_os = "linux"))]
#[allow(dead_code)]
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
            #[cfg(target_os = "linux")]
            flow_ring_buf: None,
            #[cfg(target_os = "linux")]
            bpf: None,
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
