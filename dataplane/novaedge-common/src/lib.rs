//! Shared `#[repr(C)]` types for eBPF map keys and values.
//!
//! These types are used by both the eBPF kernel programs and the userspace
//! dataplane daemon. All types must be `#[repr(C)]` for BPF map compatibility.

#![cfg_attr(not(feature = "userspace"), no_std)]

// Macro for implementing aya::Pod on all shared types (Linux userspace only).
#[cfg(all(feature = "userspace", target_os = "linux"))]
macro_rules! impl_pod {
    ($($t:ty),+ $(,)?) => {
        $(
            // SAFETY: All types are #[repr(C)] with no padding holes or pointers,
            // satisfying the Pod safety requirements.
            unsafe impl aya::Pod for $t {}
        )+
    };
}

/// VIP lookup key: virtual IP + port + protocol.
#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct VipKey {
    /// Virtual IP address (network byte order).
    pub vip: u32,
    /// Destination port (network byte order).
    pub port: u16,
    /// IP protocol number (6 = TCP, 17 = UDP).
    pub protocol: u8,
    /// Padding for alignment.
    pub _pad: u8,
}

/// VIP metadata stored in the VIP map.
#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct VipValue {
    /// Number of backends registered for this VIP.
    pub backend_count: u32,
    /// Flags (e.g., Maglev vs round-robin, session affinity).
    pub flags: u32,
}

/// Backend lookup key: VIP identifier + index in backend list.
#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct BackendKey {
    /// VIP identifier (hash or index).
    pub vip_id: u32,
    /// Backend index within the VIP's backend list.
    pub index: u32,
}

/// Backend endpoint value.
#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct BackendValue {
    /// Backend IP address (network byte order).
    pub addr: u32,
    /// Backend port (network byte order).
    pub port: u16,
    /// Weight for weighted load balancing (0 = equal weight).
    pub weight: u16,
}

/// Connection tracking key (5-tuple).
#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct ConnTrackKey {
    /// Source IP address.
    pub src_ip: u32,
    /// Destination IP address.
    pub dst_ip: u32,
    /// Source port.
    pub src_port: u16,
    /// Destination port.
    pub dst_port: u16,
    /// IP protocol number.
    pub protocol: u8,
    /// Padding.
    pub _pad: [u8; 3],
}

/// Connection tracking value (DNAT target + state).
#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct ConnTrackValue {
    /// Selected backend IP (DNAT target).
    pub backend_ip: u32,
    /// Selected backend port.
    pub backend_port: u16,
    /// Connection state (0 = new, 1 = established, 2 = closing).
    pub state: u8,
    /// Padding.
    pub _pad: u8,
    /// Last seen timestamp (nanoseconds since boot).
    pub timestamp: u64,
}

/// Rate limiting key (per source IP).
#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct RateLimitKey {
    /// Source IP address.
    pub src_ip: u32,
}

/// Rate limiting value (token bucket state).
#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct RateLimitValue {
    /// Current token count (scaled by 1000).
    pub tokens: u64,
    /// Last refill timestamp (nanoseconds since boot).
    pub last_refill: u64,
}

/// Flow event emitted via ring buffer for observability.
#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct FlowEvent {
    /// Source IP address.
    pub src_ip: u32,
    /// Destination IP address.
    pub dst_ip: u32,
    /// Source port.
    pub src_port: u16,
    /// Destination port.
    pub dst_port: u16,
    /// IP protocol number.
    pub protocol: u8,
    /// Verdict (0 = forward, 1 = drop, 2 = rate-limited).
    pub verdict: u8,
    /// Padding for u64 alignment.
    pub _pad: [u8; 2],
    /// Bytes transferred.
    pub bytes: u64,
    /// Timestamp (nanoseconds since boot).
    pub timestamp_ns: u64,
}

/// Flow event verdicts.
pub const VERDICT_FORWARD: u8 = 0;
pub const VERDICT_DROP: u8 = 1;
pub const VERDICT_RATE_LIMITED: u8 = 2;

/// Connection states.
pub const CONN_STATE_NEW: u8 = 0;
pub const CONN_STATE_ESTABLISHED: u8 = 1;
pub const CONN_STATE_CLOSING: u8 = 2;

// Implement aya::Pod for all shared types (Linux userspace only).
#[cfg(all(feature = "userspace", target_os = "linux"))]
impl_pod!(
    VipKey,
    VipValue,
    BackendKey,
    BackendValue,
    ConnTrackKey,
    ConnTrackValue,
    RateLimitKey,
    RateLimitValue,
    FlowEvent,
);

#[cfg(test)]
mod tests {
    use super::*;
    use core::mem;

    #[test]
    fn test_vip_key_size() {
        assert_eq!(mem::size_of::<VipKey>(), 8);
    }

    #[test]
    fn test_vip_value_size() {
        assert_eq!(mem::size_of::<VipValue>(), 8);
    }

    #[test]
    fn test_backend_key_size() {
        assert_eq!(mem::size_of::<BackendKey>(), 8);
    }

    #[test]
    fn test_backend_value_size() {
        assert_eq!(mem::size_of::<BackendValue>(), 8);
    }

    #[test]
    fn test_conn_track_key_size() {
        assert_eq!(mem::size_of::<ConnTrackKey>(), 16);
    }

    #[test]
    fn test_conn_track_value_size() {
        assert_eq!(mem::size_of::<ConnTrackValue>(), 16);
    }

    #[test]
    fn test_rate_limit_key_size() {
        assert_eq!(mem::size_of::<RateLimitKey>(), 4);
    }

    #[test]
    fn test_rate_limit_value_size() {
        assert_eq!(mem::size_of::<RateLimitValue>(), 16);
    }

    #[test]
    fn test_flow_event_size() {
        assert_eq!(mem::size_of::<FlowEvent>(), 32);
    }

    #[test]
    fn test_flow_event_alignment() {
        assert_eq!(mem::align_of::<FlowEvent>(), 8);
    }

    #[test]
    fn test_vip_key_fields() {
        let key = VipKey { vip: 0x0A000001, port: 80, protocol: 6, _pad: 0 };
        assert_eq!(key.vip, 0x0A000001);
        assert_eq!(key.port, 80);
        assert_eq!(key.protocol, 6);
    }

    #[test]
    fn test_backend_value_fields() {
        let val = BackendValue { addr: 0x0A000002, port: 8080, weight: 100 };
        assert_eq!(val.addr, 0x0A000002);
        assert_eq!(val.port, 8080);
        assert_eq!(val.weight, 100);
    }

    #[test]
    fn test_conn_track_key_fields() {
        let key = ConnTrackKey {
            src_ip: 0x0A000001,
            dst_ip: 0x0A600064,
            src_port: 12345,
            dst_port: 80,
            protocol: 6,
            _pad: [0; 3],
        };
        assert_eq!(key.src_ip, 0x0A000001);
        assert_eq!(key.dst_ip, 0x0A600064);
        assert_eq!(key.src_port, 12345);
        assert_eq!(key.dst_port, 80);
        assert_eq!(key.protocol, 6);
    }

    #[test]
    fn test_flow_event_verdicts() {
        assert_eq!(VERDICT_FORWARD, 0);
        assert_eq!(VERDICT_DROP, 1);
        assert_eq!(VERDICT_RATE_LIMITED, 2);
    }

    #[test]
    fn test_default_values() {
        let key = VipKey { vip: 0, port: 0, protocol: 0, _pad: 0 };
        assert_eq!(key.vip, 0);
    }
}
