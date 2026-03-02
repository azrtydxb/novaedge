//! NovaEdge eBPF programs (XDP and TC).
//!
//! These programs run in the kernel and handle fast-path operations:
//! - XDP: L4 load balancing (`novaedge_xdp`), VIP ARP response (`novaedge_arp`)
//! - TC: Rate limiting (`novaedge_ratelimit`)

#![no_std]
#![no_main]
#![allow(clippy::unnecessary_cast)]

use aya_ebpf::{
    bindings::{xdp_action, TC_ACT_OK, TC_ACT_SHOT},
    helpers::bpf_ktime_get_ns,
    macros::{classifier, map, xdp},
    maps::{HashMap, LruHashMap, RingBuf},
    programs::{TcContext, XdpContext},
};
use core::mem;
use network_types::{
    eth::{EthHdr, EtherType},
    ip::{IpProto, Ipv4Hdr},
    tcp::TcpHdr,
    udp::UdpHdr,
};
use novaedge_common::{
    BackendKey, BackendValue, ConnTrackKey, ConnTrackValue, FlowEvent, RateLimitCfg, RateLimitKey,
    RateLimitValue, VipKey, VipValue, VERDICT_FORWARD, VERDICT_RATE_LIMITED,
};

// ── ARP constants and header ──────────────────────────────────────────

const ARP_HTYPE_ETHER: u16 = 1;
const ARP_OP_REQUEST: u16 = 1;
const ARP_OP_REPLY: u16 = 2;
const ETH_P_ARP: u16 = 0x0806;

/// ARP header for IPv4 over Ethernet (28 bytes).
#[repr(C, packed)]
#[derive(Clone, Copy)]
struct ArpHdr {
    /// Hardware type (1 = Ethernet).
    htype: u16,
    /// Protocol type (0x0800 = IPv4).
    ptype: u16,
    /// Hardware address length (6 for Ethernet).
    hlen: u8,
    /// Protocol address length (4 for IPv4).
    plen: u8,
    /// Operation (1 = request, 2 = reply).
    oper: u16,
    /// Sender hardware address (MAC).
    sha: [u8; 6],
    /// Sender protocol address (IP).
    spa: u32,
    /// Target hardware address (MAC).
    tha: [u8; 6],
    /// Target protocol address (IP).
    tpa: u32,
}

// ── eBPF maps ─────────────────────────────────────────────────────────

// Maps for XDP L4 load balancer (#682)
#[map]
static VIPS: HashMap<VipKey, VipValue> = HashMap::with_max_entries(4096, 0);

#[map]
static BACKENDS: HashMap<BackendKey, BackendValue> = HashMap::with_max_entries(65536, 0);

#[map]
static CONNTRACK: LruHashMap<ConnTrackKey, ConnTrackValue> =
    LruHashMap::with_max_entries(1048576, 0);

// Map for XDP ARP responder (#683)
#[map]
static VIP_ADDRS: HashMap<u32, [u8; 6]> = HashMap::with_max_entries(1024, 0);

// Maps for TC rate limiter (#684)
#[map]
static RATE_LIMIT_CFG: HashMap<RateLimitKey, RateLimitCfg> = HashMap::with_max_entries(65536, 0);

#[map]
static RATE_LIMIT_STATE: LruHashMap<RateLimitKey, RateLimitValue> =
    LruHashMap::with_max_entries(1048576, 0);

// Shared flow event ring buffer (#685)
#[map]
static FLOW_EVENTS: RingBuf = RingBuf::with_byte_size(4 * 1024 * 1024, 0);

// ── Helpers ───────────────────────────────────────────────────────────

/// Safely obtain a pointer to a header `T` at `offset` within an XDP packet.
///
/// Returns `Err(())` if the header extends past `data_end`, satisfying
/// the BPF verifier's bounds-check requirements.
#[inline(always)]
unsafe fn ptr_at<T>(ctx: &XdpContext, offset: usize) -> Result<*mut T, ()> {
    let start = ctx.data();
    let end = ctx.data_end();
    let len = mem::size_of::<T>();

    if start + offset + len > end {
        return Err(());
    }

    Ok((start + offset) as *mut T)
}

/// Safely obtain a pointer to a header `T` at `offset` within a TC packet.
#[inline(always)]
unsafe fn tc_ptr_at<T>(ctx: &TcContext, offset: usize) -> Result<*mut T, ()> {
    let start = ctx.data();
    let end = ctx.data_end();
    let len = mem::size_of::<T>();

    if start + offset + len > end {
        return Err(());
    }

    Ok((start + offset) as *mut T)
}

/// Compute a simple hash of a 5-tuple for backend selection.
///
/// Uses a FNV-1a inspired scheme that is cheap in eBPF yet gives
/// reasonable distribution.
#[inline(always)]
fn hash_5tuple(src_ip: u32, dst_ip: u32, src_port: u16, dst_port: u16, proto: u8) -> u32 {
    let mut h: u32 = 2166136261;
    h ^= src_ip;
    h = h.wrapping_mul(16777619);
    h ^= dst_ip;
    h = h.wrapping_mul(16777619);
    h ^= src_port as u32;
    h = h.wrapping_mul(16777619);
    h ^= dst_port as u32;
    h = h.wrapping_mul(16777619);
    h ^= proto as u32;
    h = h.wrapping_mul(16777619);
    h
}

/// Incrementally update an IP checksum when a 32-bit field changes.
///
/// `old` and `new` are in network byte order.
#[inline(always)]
fn csum_update_u32(csum: u16, old: u32, new: u32) -> u16 {
    let mut sum = (!csum as u32) & 0xffff;
    // Subtract old value
    let old_hi = (old >> 16) & 0xffff;
    let old_lo = old & 0xffff;
    sum = sum.wrapping_add((!old_hi) & 0xffff);
    sum = sum.wrapping_add((!old_lo) & 0xffff);
    // Add new value
    let new_hi = (new >> 16) & 0xffff;
    let new_lo = new & 0xffff;
    sum = sum.wrapping_add(new_hi);
    sum = sum.wrapping_add(new_lo);
    // Fold carries
    while sum >> 16 != 0 {
        sum = (sum & 0xffff) + (sum >> 16);
    }
    !(sum as u16)
}

/// Incrementally update a checksum when a 16-bit field changes.
#[inline(always)]
fn csum_update_u16(csum: u16, old: u16, new: u16) -> u16 {
    let mut sum = (!csum as u32) & 0xffff;
    sum = sum.wrapping_add((!old as u32) & 0xffff);
    sum = sum.wrapping_add(new as u32);
    while sum >> 16 != 0 {
        sum = (sum & 0xffff) + (sum >> 16);
    }
    !(sum as u16)
}

/// Emit a FlowEvent to the ring buffer.
#[inline(always)]
fn emit_flow_event(
    src_ip: u32,
    dst_ip: u32,
    src_port: u16,
    dst_port: u16,
    protocol: u8,
    verdict: u8,
    bytes: u64,
) {
    let ts = unsafe { bpf_ktime_get_ns() };
    let event = FlowEvent {
        src_ip,
        dst_ip,
        src_port,
        dst_port,
        protocol,
        verdict,
        _pad: [0; 2],
        bytes,
        timestamp_ns: ts,
    };

    if let Some(mut buf) = FLOW_EVENTS.reserve::<FlowEvent>(0) {
        unsafe {
            core::ptr::write_unaligned(buf.as_mut_ptr() as *mut FlowEvent, event);
        }
        buf.submit(0);
    }
}

// ── XDP L4 Load Balancer (#682) ───────────────────────────────────────

/// XDP program for L4 load balancing.
///
/// Performs VIP lookup, connection tracking, backend selection, DNAT
/// rewrite, and transmits modified packets back out the same interface.
#[xdp]
pub fn novaedge_xdp(ctx: XdpContext) -> u32 {
    match try_novaedge_xdp(&ctx) {
        Ok(action) => action,
        Err(_) => xdp_action::XDP_PASS,
    }
}

#[inline(always)]
fn try_novaedge_xdp(ctx: &XdpContext) -> Result<u32, ()> {
    // 1. Parse Ethernet header
    let eth: *mut EthHdr = unsafe { ptr_at(ctx, 0)? };
    let ether_type = unsafe { (*eth).ether_type };

    // Only handle IPv4
    if ether_type != EtherType::Ipv4 {
        return Ok(xdp_action::XDP_PASS);
    }

    let eth_hdr_len = mem::size_of::<EthHdr>();

    // 2. Parse IPv4 header
    let ipv4: *mut Ipv4Hdr = unsafe { ptr_at(ctx, eth_hdr_len)? };
    let protocol = unsafe { (*ipv4).proto };
    let src_ip = unsafe { (*ipv4).src_addr };
    let dst_ip = unsafe { (*ipv4).dst_addr };
    let total_len = u16::from_be(unsafe { (*ipv4).tot_len });

    // Calculate IPv4 header length (IHL * 4)
    let ihl = unsafe { (*ipv4).ihl() } as usize;
    let ip_hdr_len = ihl * 4;
    let l4_offset = eth_hdr_len + ip_hdr_len;

    // 3. Parse L4 header and extract ports
    let (src_port, dst_port, proto_num): (u16, u16, u8) = match protocol {
        IpProto::Tcp => {
            let tcp: *mut TcpHdr = unsafe { ptr_at(ctx, l4_offset)? };
            let sp = unsafe { (*tcp).source };
            let dp = unsafe { (*tcp).dest };
            (sp, dp, 6u8)
        }
        IpProto::Udp => {
            let udp: *mut UdpHdr = unsafe { ptr_at(ctx, l4_offset)? };
            let sp = unsafe { (*udp).source };
            let dp = unsafe { (*udp).dest };
            (sp, dp, 17u8)
        }
        _ => return Ok(xdp_action::XDP_PASS),
    };

    // 4. VIP lookup
    let vip_key = VipKey {
        vip: dst_ip,
        port: dst_port,
        protocol: proto_num,
        _pad: 0,
    };

    let vip_val = unsafe { VIPS.get(&vip_key) };
    let vip = match vip_val {
        Some(v) => v,
        None => return Ok(xdp_action::XDP_PASS),
    };

    let backend_count = vip.backend_count;
    if backend_count == 0 {
        return Ok(xdp_action::XDP_PASS);
    }

    // 5. Connection tracking lookup
    let ct_key = ConnTrackKey {
        src_ip,
        dst_ip,
        src_port,
        dst_port,
        protocol: proto_num,
        _pad: [0; 3],
    };

    let (backend_ip, backend_port) = if let Some(ct) = unsafe { CONNTRACK.get(&ct_key) } {
        // Existing connection: use cached backend
        (ct.backend_ip, ct.backend_port)
    } else {
        // 6. Select backend via hash
        let h = hash_5tuple(src_ip, dst_ip, src_port, dst_port, proto_num);
        let index = h % backend_count;

        // Build a VIP identifier for backend lookup.
        // Use the raw bits of the VIP key to derive a stable id.
        let vip_id = vip_key.vip ^ ((vip_key.port as u32) << 16) ^ (vip_key.protocol as u32);

        let bk = BackendKey { vip_id, index };

        let backend = match unsafe { BACKENDS.get(&bk) } {
            Some(b) => b,
            None => return Ok(xdp_action::XDP_PASS),
        };

        // 7. Create conntrack entry
        let now = unsafe { bpf_ktime_get_ns() };
        let ct_val = ConnTrackValue {
            backend_ip: backend.addr,
            backend_port: backend.port,
            state: novaedge_common::CONN_STATE_NEW,
            _pad: 0,
            timestamp: now,
        };

        let _ = CONNTRACK.insert(&ct_key, &ct_val, 0);

        (backend.addr, backend.port)
    };

    // 8. DNAT: rewrite destination IP
    let old_dst_ip = unsafe { (*ipv4).dst_addr };
    unsafe {
        (*ipv4).dst_addr = backend_ip;
    }

    // Update IP checksum
    let old_check = unsafe { (*ipv4).check };
    let new_check = csum_update_u32(old_check, old_dst_ip, backend_ip);
    unsafe {
        (*ipv4).check = new_check;
    }

    // 9. DNAT: rewrite destination port and update L4 checksum
    match protocol {
        IpProto::Tcp => {
            let tcp: *mut TcpHdr = unsafe { ptr_at(ctx, l4_offset)? };
            let old_port = unsafe { (*tcp).dest };
            unsafe {
                (*tcp).dest = backend_port;
            }
            // Update TCP checksum for IP change
            let old_csum = unsafe { (*tcp).check };
            let mut new_csum = csum_update_u32(old_csum, old_dst_ip, backend_ip);
            // Update TCP checksum for port change
            new_csum = csum_update_u16(new_csum, old_port, backend_port);
            unsafe {
                (*tcp).check = new_csum;
            }
        }
        IpProto::Udp => {
            let udp: *mut UdpHdr = unsafe { ptr_at(ctx, l4_offset)? };
            let old_port = unsafe { (*udp).dest };
            unsafe {
                (*udp).dest = backend_port;
            }
            let old_csum = unsafe { (*udp).check };
            // UDP checksum is optional (0 means disabled)
            if old_csum != 0 {
                let mut new_csum = csum_update_u32(old_csum, old_dst_ip, backend_ip);
                new_csum = csum_update_u16(new_csum, old_port, backend_port);
                // If result is 0, use 0xFFFF (RFC 768)
                if new_csum == 0 {
                    new_csum = 0xffff;
                }
                unsafe {
                    (*udp).check = new_csum;
                }
            }
        }
        _ => {}
    }

    // 10. Emit flow event
    emit_flow_event(
        src_ip,
        dst_ip,
        src_port,
        dst_port,
        proto_num,
        VERDICT_FORWARD,
        total_len as u64,
    );

    // 11. Transmit via XDP_TX
    Ok(xdp_action::XDP_TX)
}

// ── XDP VIP ARP Responder (#683) ──────────────────────────────────────

/// XDP program that responds to ARP requests for VIP addresses.
///
/// Looks up the target IP in `VIP_ADDRS` and, if found, generates an ARP
/// reply in-place and sends it back out the same interface via `XDP_TX`.
#[xdp]
pub fn novaedge_arp(ctx: XdpContext) -> u32 {
    match try_novaedge_arp(&ctx) {
        Ok(action) => action,
        Err(_) => xdp_action::XDP_PASS,
    }
}

#[inline(always)]
fn try_novaedge_arp(ctx: &XdpContext) -> Result<u32, ()> {
    // 1. Parse Ethernet header
    let eth: *mut EthHdr = unsafe { ptr_at(ctx, 0)? };
    let ether_type = unsafe { (*eth).ether_type };

    // Check for ARP EtherType (0x0806)
    if ether_type != EtherType::Arp {
        return Ok(xdp_action::XDP_PASS);
    }

    let eth_hdr_len = mem::size_of::<EthHdr>();

    // 2. Parse ARP header
    let arp: *mut ArpHdr = unsafe { ptr_at(ctx, eth_hdr_len)? };

    // Verify ARP for IPv4 over Ethernet
    let htype = u16::from_be(unsafe { (*arp).htype });
    let oper = u16::from_be(unsafe { (*arp).oper });

    if htype != ARP_HTYPE_ETHER || oper != ARP_OP_REQUEST {
        return Ok(xdp_action::XDP_PASS);
    }

    // 3. Check if target IP is a VIP
    let target_ip = unsafe { (*arp).tpa };
    let mac = match unsafe { VIP_ADDRS.get(&target_ip) } {
        Some(m) => *m,
        None => return Ok(xdp_action::XDP_PASS),
    };

    // 4. Build ARP reply
    let sender_mac = unsafe { (*arp).sha };
    let sender_ip = unsafe { (*arp).spa };

    unsafe {
        // ARP: set opcode to reply
        (*arp).oper = u16::to_be(ARP_OP_REPLY);

        // ARP: target = original sender
        (*arp).tha = sender_mac;
        (*arp).tpa = sender_ip;

        // ARP: sender = our VIP MAC
        (*arp).sha = mac;
        (*arp).spa = target_ip;

        // Ethernet: set dst to original sender, src to our VIP MAC
        (*eth).dst_addr = sender_mac;
        (*eth).src_addr = mac;
    }

    // 5. Transmit reply
    Ok(xdp_action::XDP_TX)
}

// ── TC Rate Limiter (#684) ────────────────────────────────────────────

/// TC classifier program for per-source-IP token bucket rate limiting.
///
/// Parses the source IP from inbound IPv4 packets, looks up the
/// rate-limit configuration, and applies a token-bucket algorithm.
/// Packets within budget are passed (`TC_ACT_OK`); over-limit packets
/// are dropped (`TC_ACT_SHOT`).
#[classifier]
pub fn novaedge_ratelimit(ctx: TcContext) -> i32 {
    match try_novaedge_ratelimit(&ctx) {
        Ok(action) => action,
        Err(_) => TC_ACT_OK,
    }
}

#[inline(always)]
fn try_novaedge_ratelimit(ctx: &TcContext) -> Result<i32, ()> {
    let eth_hdr_len = mem::size_of::<EthHdr>();

    // 1. Parse Ethernet header and check for IPv4
    let eth: *const EthHdr = unsafe { tc_ptr_at(ctx, 0)? };
    let ether_type = unsafe { (*eth).ether_type };
    if ether_type != EtherType::Ipv4 {
        return Ok(TC_ACT_OK);
    }

    // 2. Parse IPv4 header for source IP
    let ipv4: *const Ipv4Hdr = unsafe { tc_ptr_at(ctx, eth_hdr_len)? };
    let src_ip = unsafe { (*ipv4).src_addr };
    let total_len = u16::from_be(unsafe { (*ipv4).tot_len });

    // Also extract L4 ports for flow events (optional best-effort)
    let protocol = unsafe { (*ipv4).proto };
    let dst_ip = unsafe { (*ipv4).dst_addr };
    let ihl = unsafe { (*ipv4).ihl() } as usize;
    let ip_hdr_len = ihl * 4;
    let l4_offset = eth_hdr_len + ip_hdr_len;

    let (src_port, dst_port, proto_num) = match protocol {
        IpProto::Tcp => {
            if let Ok(tcp) = unsafe { tc_ptr_at::<TcpHdr>(ctx, l4_offset) } {
                let sp = unsafe { (*tcp).source };
                let dp = unsafe { (*tcp).dest };
                (sp, dp, 6u8)
            } else {
                (0u16, 0u16, 6u8)
            }
        }
        IpProto::Udp => {
            if let Ok(udp) = unsafe { tc_ptr_at::<UdpHdr>(ctx, l4_offset) } {
                let sp = unsafe { (*udp).source };
                let dp = unsafe { (*udp).dest };
                (sp, dp, 17u8)
            } else {
                (0u16, 0u16, 17u8)
            }
        }
        _ => (0u16, 0u16, 0u8),
    };

    // 3. Rate limit lookup
    let rl_key = RateLimitKey { src_ip };

    let cfg = match unsafe { RATE_LIMIT_CFG.get(&rl_key) } {
        Some(c) => c,
        None => return Ok(TC_ACT_OK), // No rate limit configured
    };

    // 4. Get or create state
    let now = unsafe { bpf_ktime_get_ns() };

    let state = unsafe { RATE_LIMIT_STATE.get(&rl_key) };

    let (mut tokens, last_refill) = match state {
        Some(s) => (s.tokens, s.last_refill),
        None => {
            // New entry: start with full bucket
            (cfg.burst * 1000, now)
        }
    };

    // 5. Token bucket: refill tokens
    let elapsed_ns = now.wrapping_sub(last_refill);
    // new_tokens = elapsed_ns * rate / 1_000_000_000
    // To avoid overflow, do: (elapsed_ns / 1000) * rate / 1_000_000
    // But for precision let's use: elapsed_ns * rate / 1_000_000_000
    // Split the division to avoid u64 overflow: (elapsed_ns / 1000) * rate / 1_000_000
    let refill = (elapsed_ns / 1000).wrapping_mul(cfg.rate) / 1_000_000;
    tokens = tokens.wrapping_add(refill);
    let max_tokens = cfg.burst * 1000;
    if tokens > max_tokens {
        tokens = max_tokens;
    }

    // 6. Check if we have enough tokens for one packet
    if tokens >= 1000 {
        tokens -= 1000;

        // Update state
        let new_state = RateLimitValue {
            tokens,
            last_refill: now,
        };
        let _ = RATE_LIMIT_STATE.insert(&rl_key, &new_state, 0);

        Ok(TC_ACT_OK)
    } else {
        // Update state (still update last_refill to avoid stale refills)
        let new_state = RateLimitValue {
            tokens,
            last_refill: now,
        };
        let _ = RATE_LIMIT_STATE.insert(&rl_key, &new_state, 0);

        // 7. Emit rate-limited flow event
        emit_flow_event(
            src_ip,
            dst_ip,
            src_port,
            dst_port,
            proto_num,
            VERDICT_RATE_LIMITED,
            total_len as u64,
        );

        Ok(TC_ACT_SHOT)
    }
}

// ── Panic handler (required for #![no_std]) ───────────────────────────

#[panic_handler]
fn panic(_info: &core::panic::PanicInfo) -> ! {
    loop {}
}
