//! NovaEdge eBPF programs (XDP and TC).
//!
//! These programs run in the kernel and handle fast-path operations:
//! - XDP: L4 load balancing, VIP ARP response
//! - TC: Rate limiting, flow event generation

#![no_std]
#![no_main]

use aya_ebpf::{macros::xdp, programs::XdpContext};

/// Placeholder XDP program — passes all packets through.
///
/// This will be replaced with actual L4 load balancing logic in Phase 2.1.
#[xdp]
pub fn novaedge_xdp(ctx: XdpContext) -> u32 {
    match process_packet(&ctx) {
        Ok(action) => action,
        Err(_) => aya_ebpf::bindings::xdp_action::XDP_PASS,
    }
}

#[inline(always)]
fn process_packet(_ctx: &XdpContext) -> Result<u32, ()> {
    // TODO: Implement L4 load balancing in Phase 2.1.
    Ok(aya_ebpf::bindings::xdp_action::XDP_PASS)
}

#[panic_handler]
fn panic(_info: &core::panic::PanicInfo) -> ! {
    loop {}
}
