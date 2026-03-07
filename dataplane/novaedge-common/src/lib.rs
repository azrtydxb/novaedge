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

/// Rate limiting key (per source IP).
#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct RateLimitKey {
    /// Source IP address.
    pub src_ip: u32,
}

/// Rate limiting configuration (per source IP).
#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct RateLimitCfg {
    /// Tokens per second (refill rate).
    pub rate: u64,
    /// Maximum tokens (bucket capacity).
    pub burst: u64,
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

// Implement aya::Pod for all shared types (Linux userspace only).
#[cfg(all(feature = "userspace", target_os = "linux"))]
impl_pod!(RateLimitKey, RateLimitCfg, RateLimitValue, FlowEvent,);

#[cfg(test)]
mod tests {
    use super::*;
    use core::mem;

    #[test]
    fn test_rate_limit_key_size() {
        assert_eq!(mem::size_of::<RateLimitKey>(), 4);
    }

    #[test]
    fn test_rate_limit_cfg_size() {
        assert_eq!(mem::size_of::<RateLimitCfg>(), 16);
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
    fn test_flow_event_verdicts() {
        assert_eq!(VERDICT_FORWARD, 0);
        assert_eq!(VERDICT_DROP, 1);
        assert_eq!(VERDICT_RATE_LIMITED, 2);
    }
}
