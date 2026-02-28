//! Upstream connection management.
//!
//! Provides connection pooling, circuit breaking, and outlier detection
//! for backend connections.

pub mod circuit_breaker;
pub mod outlier;
pub mod pool;
