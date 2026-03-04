//! Upstream connection management.
//!
//! Provides connection pooling, circuit breaking, and outlier detection
//! for backend connections.

#[allow(dead_code)]
pub mod adaptive;
pub mod circuit_breaker;
pub mod outlier;
pub mod pool;
pub mod retry_budget;
