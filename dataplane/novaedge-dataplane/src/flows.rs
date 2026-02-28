//! Flow event streaming from eBPF ring buffer.
//!
//! On Linux this reads from the eBPF ring buffer using `AsyncFd`.
//! In mock mode a `MockFlowInjector` allows tests to push events.

use std::net::Ipv4Addr;

use tokio::sync::broadcast;
use tracing::{debug, info};

#[cfg(target_os = "linux")]
use tracing::warn;

use crate::proto;

/// Capacity of the broadcast channel for flow events.
const FLOW_CHANNEL_CAPACITY: usize = 4096;

/// Create a new flow event broadcast channel.
///
/// Returns `(sender, receiver)`. The sender is cloneable and should be passed
/// to the flow reader task and the gRPC service. Additional receivers can be
/// created via `sender.subscribe()`.
pub fn flow_channel() -> (
    broadcast::Sender<proto::FlowEvent>,
    broadcast::Receiver<proto::FlowEvent>,
) {
    broadcast::channel(FLOW_CHANNEL_CAPACITY)
}

/// Convert a raw `novaedge_common::FlowEvent` into a proto `FlowEvent`.
pub fn raw_to_proto(raw: &novaedge_common::FlowEvent) -> proto::FlowEvent {
    let src_ip = Ipv4Addr::from(raw.src_ip.to_be()).to_string();
    let dst_ip = Ipv4Addr::from(raw.dst_ip.to_be()).to_string();

    let verdict = match raw.verdict {
        novaedge_common::VERDICT_FORWARD => proto::FlowVerdict::Forwarded as i32,
        novaedge_common::VERDICT_DROP => proto::FlowVerdict::Dropped as i32,
        novaedge_common::VERDICT_RATE_LIMITED => proto::FlowVerdict::Rejected as i32,
        _ => proto::FlowVerdict::Unspecified as i32,
    };

    proto::FlowEvent {
        src_ip,
        dst_ip,
        src_port: raw.src_port as u32,
        dst_port: raw.dst_port as u32,
        protocol: raw.protocol as u32,
        verdict,
        latency_ns: 0,
        bytes: raw.bytes,
        backend_selected: String::new(),
        lb_algorithm: String::new(),
        timestamp_ns: raw.timestamp_ns,
    }
}

/// Mock flow injector for testing and non-Linux platforms.
///
/// Allows tests and mock mode to inject flow events into the broadcast channel.
pub struct MockFlowInjector {
    tx: broadcast::Sender<proto::FlowEvent>,
}

impl MockFlowInjector {
    /// Create a new mock flow injector wrapping the given sender.
    pub fn new(tx: broadcast::Sender<proto::FlowEvent>) -> Self {
        Self { tx }
    }

    /// Inject a raw flow event (converts to proto and broadcasts).
    pub fn inject_raw(&self, raw: &novaedge_common::FlowEvent) -> Result<(), String> {
        let event = raw_to_proto(raw);
        self.tx
            .send(event)
            .map(|_| ())
            .map_err(|e| format!("no active subscribers: {e}"))
    }

    /// Inject a proto flow event directly.
    pub fn inject(&self, event: proto::FlowEvent) -> Result<(), String> {
        self.tx
            .send(event)
            .map(|_| ())
            .map_err(|e| format!("no active subscribers: {e}"))
    }
}

/// Start the real flow reader on Linux, reading from the eBPF ring buffer.
///
/// This uses `AsyncFd` to efficiently wait for ring buffer data availability,
/// then reads and broadcasts each event.
#[cfg(target_os = "linux")]
pub async fn run_flow_reader_real(
    mut ring_buf: aya::maps::RingBuf<aya::maps::MapData>,
    tx: broadcast::Sender<proto::FlowEvent>,
) {
    use std::os::fd::AsRawFd;
    use tokio::io::unix::AsyncFd;

    info!("Flow reader: starting eBPF ring buffer reader");

    let fd = ring_buf.as_raw_fd();
    let async_fd = match AsyncFd::new(fd) {
        Ok(afd) => afd,
        Err(e) => {
            warn!("Flow reader: failed to create AsyncFd: {e}");
            return;
        }
    };

    loop {
        // Wait for the ring buffer to become readable.
        let mut guard = match async_fd.readable().await {
            Ok(g) => g,
            Err(e) => {
                warn!("Flow reader: readable() error: {e}");
                break;
            }
        };

        // Drain all available events.
        while let Some(event_data) = ring_buf.next() {
            if event_data.len() < core::mem::size_of::<novaedge_common::FlowEvent>() {
                continue;
            }
            let raw: &novaedge_common::FlowEvent =
                unsafe { &*(event_data.as_ptr() as *const novaedge_common::FlowEvent) };
            let proto_event = raw_to_proto(raw);
            let _ = tx.send(proto_event);
        }

        guard.clear_ready();
    }
}

/// Start the flow reader task.
///
/// On Linux with a real ring buffer, this would read from the eBPF ring buffer
/// using `AsyncFd`. In mock mode, this simply logs that mock mode is active
/// and waits for the cancellation token.
///
/// Flow events are broadcast via the sender. The gRPC `StreamFlows` RPC
/// subscribes to this channel to deliver events to clients.
pub async fn run_flow_reader(tx: broadcast::Sender<proto::FlowEvent>, mock_mode: bool) {
    if mock_mode {
        info!("Flow reader running in mock mode (no eBPF ring buffer)");
        // In mock mode, we just keep the task alive. Events can be injected
        // via `MockFlowInjector` in tests.
        // This task will be cancelled when the main runtime shuts down.
        let _ = std::future::pending::<()>().await;
        return;
    }

    // On Linux with real eBPF, the ring buffer reader is started via
    // `run_flow_reader_real()` with the actual RingBuf handle.
    // If we reach here in non-mock mode without a ring buffer, just wait.
    #[cfg(target_os = "linux")]
    {
        info!("Flow reader: waiting for ring buffer (use run_flow_reader_real for eBPF)");
        let _ = std::future::pending::<()>().await;
    }

    #[cfg(not(target_os = "linux"))]
    {
        debug!("Flow reader: non-Linux platform, no ring buffer available");
        let _ = &tx;
        let _ = std::future::pending::<()>().await;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_raw_to_proto_forward() {
        let raw = novaedge_common::FlowEvent {
            src_ip: u32::from(Ipv4Addr::new(10, 0, 0, 1)).to_be(),
            dst_ip: u32::from(Ipv4Addr::new(10, 0, 0, 2)).to_be(),
            src_port: 12345,
            dst_port: 80,
            protocol: 6, // TCP
            verdict: novaedge_common::VERDICT_FORWARD,
            _pad: [0; 2],
            bytes: 1500,
            timestamp_ns: 1000000,
        };

        let proto_ev = raw_to_proto(&raw);
        assert_eq!(proto_ev.src_ip, "10.0.0.1");
        assert_eq!(proto_ev.dst_ip, "10.0.0.2");
        assert_eq!(proto_ev.src_port, 12345);
        assert_eq!(proto_ev.dst_port, 80);
        assert_eq!(proto_ev.protocol, 6);
        assert_eq!(proto_ev.verdict, proto::FlowVerdict::Forwarded as i32);
        assert_eq!(proto_ev.bytes, 1500);
        assert_eq!(proto_ev.timestamp_ns, 1000000);
    }

    #[test]
    fn test_raw_to_proto_drop() {
        let raw = novaedge_common::FlowEvent {
            src_ip: 0,
            dst_ip: 0,
            src_port: 0,
            dst_port: 0,
            protocol: 17,
            verdict: novaedge_common::VERDICT_DROP,
            _pad: [0; 2],
            bytes: 0,
            timestamp_ns: 0,
        };

        let proto_ev = raw_to_proto(&raw);
        assert_eq!(proto_ev.verdict, proto::FlowVerdict::Dropped as i32);
    }

    #[test]
    fn test_raw_to_proto_rate_limited() {
        let raw = novaedge_common::FlowEvent {
            src_ip: 0,
            dst_ip: 0,
            src_port: 0,
            dst_port: 0,
            protocol: 6,
            verdict: novaedge_common::VERDICT_RATE_LIMITED,
            _pad: [0; 2],
            bytes: 0,
            timestamp_ns: 0,
        };

        let proto_ev = raw_to_proto(&raw);
        assert_eq!(proto_ev.verdict, proto::FlowVerdict::Rejected as i32);
    }

    #[test]
    fn test_flow_channel_creation() {
        let (tx, _rx) = flow_channel();
        // Channel should be created successfully.
        assert_eq!(tx.receiver_count(), 1);
    }

    #[test]
    fn test_mock_flow_injector() {
        let (tx, mut rx) = flow_channel();
        let injector = MockFlowInjector::new(tx);

        let event = proto::FlowEvent {
            src_ip: "10.0.0.1".into(),
            dst_ip: "10.0.0.2".into(),
            src_port: 12345,
            dst_port: 80,
            protocol: 6,
            verdict: proto::FlowVerdict::Forwarded as i32,
            latency_ns: 0,
            bytes: 1500,
            backend_selected: String::new(),
            lb_algorithm: String::new(),
            timestamp_ns: 1000000,
        };

        injector.inject(event.clone()).unwrap();
        let received = rx.try_recv().unwrap();
        assert_eq!(received.src_ip, "10.0.0.1");
        assert_eq!(received.dst_port, 80);
    }

    #[test]
    fn test_mock_flow_injector_raw() {
        let (tx, mut rx) = flow_channel();
        let injector = MockFlowInjector::new(tx);

        let raw = novaedge_common::FlowEvent {
            src_ip: u32::from(Ipv4Addr::new(192, 168, 1, 1)).to_be(),
            dst_ip: u32::from(Ipv4Addr::new(192, 168, 1, 2)).to_be(),
            src_port: 54321,
            dst_port: 443,
            protocol: 6,
            verdict: novaedge_common::VERDICT_FORWARD,
            _pad: [0; 2],
            bytes: 2048,
            timestamp_ns: 999999,
        };

        injector.inject_raw(&raw).unwrap();
        let received = rx.try_recv().unwrap();
        assert_eq!(received.src_ip, "192.168.1.1");
        assert_eq!(received.dst_ip, "192.168.1.2");
        assert_eq!(received.src_port, 54321);
        assert_eq!(received.dst_port, 443);
    }
}
