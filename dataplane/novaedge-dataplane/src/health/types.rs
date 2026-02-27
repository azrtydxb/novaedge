//! Health check types and configuration.

use std::time::Duration;

/// Type of health check to perform.
#[derive(Debug, Clone)]
pub enum HealthCheckType {
    /// HTTP health check.
    Http(HttpHealthCheck),
    /// TCP connect health check.
    Tcp(TcpHealthCheck),
    /// gRPC health check (currently falls back to TCP connect).
    Grpc(GrpcHealthCheck),
}

/// HTTP health check configuration.
#[derive(Debug, Clone)]
pub struct HttpHealthCheck {
    /// Path to request (e.g., "/healthz").
    pub path: String,
    /// Optional Host header override.
    pub host: Option<String>,
    /// HTTP status codes considered healthy.
    pub expected_statuses: Vec<u16>,
    /// HTTP method (GET, HEAD, etc.).
    pub method: String,
}

impl Default for HttpHealthCheck {
    fn default() -> Self {
        Self {
            path: "/healthz".into(),
            host: None,
            expected_statuses: vec![200],
            method: "GET".into(),
        }
    }
}

/// TCP health check configuration.
#[derive(Debug, Clone)]
pub struct TcpHealthCheck {
    /// Optional bytes to send after connecting.
    pub send: Option<Vec<u8>>,
    /// Optional bytes expected in response.
    pub receive: Option<Vec<u8>>,
}

/// gRPC health check configuration.
#[derive(Debug, Clone)]
pub struct GrpcHealthCheck {
    /// Service name for the gRPC health check.
    pub service: String,
}

/// Health check configuration.
#[derive(Debug, Clone)]
pub struct HealthCheckConfig {
    /// Interval between health checks.
    pub interval: Duration,
    /// Timeout for each individual check.
    pub timeout: Duration,
    /// Number of consecutive successes to consider a backend healthy.
    pub healthy_threshold: u32,
    /// Number of consecutive failures to consider a backend unhealthy.
    pub unhealthy_threshold: u32,
    /// The health check type and its configuration.
    pub check: HealthCheckType,
}

impl Default for HealthCheckConfig {
    fn default() -> Self {
        Self {
            interval: Duration::from_secs(10),
            timeout: Duration::from_secs(5),
            healthy_threshold: 2,
            unhealthy_threshold: 3,
            check: HealthCheckType::Tcp(TcpHealthCheck {
                send: None,
                receive: None,
            }),
        }
    }
}

/// Health state of a backend.
#[derive(Debug, Clone)]
pub enum HealthState {
    /// Backend is healthy.
    Healthy {
        /// Number of consecutive successful checks.
        consecutive_successes: u32,
    },
    /// Backend is unhealthy.
    Unhealthy {
        /// Number of consecutive failed checks.
        consecutive_failures: u32,
        /// Error message from the last failed check.
        last_error: String,
    },
    /// Health state has not been determined yet.
    Unknown,
}

impl HealthState {
    /// Returns `true` if the backend is in the healthy state.
    pub fn is_healthy(&self) -> bool {
        matches!(self, HealthState::Healthy { .. })
    }
}
