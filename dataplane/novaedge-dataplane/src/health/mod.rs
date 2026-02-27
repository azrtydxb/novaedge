//! Active health checking for upstream backends.
//!
//! Supports TCP, HTTP, and gRPC health check types with configurable
//! intervals, thresholds, and timeouts.

pub mod checker;
pub mod types;

pub use checker::HealthChecker;
pub use types::*;
