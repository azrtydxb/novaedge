//! L4 proxy modules provide advanced TCP/UDP proxying with connection limits,
//! session affinity, metrics, and PROXY protocol support. Currently the
//! listener manager uses inline copy_bidirectional for L4 gateways; these
//! modules will be integrated once per-gateway connection limits and byte
//! counters are wired through config.
#[allow(dead_code)]
pub mod proxy_protocol;
#[allow(dead_code)]
pub mod tcp;
#[allow(dead_code)]
pub mod udp;
