//! Health checker implementation.

use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::Arc;

use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;
use tokio::sync::RwLock;
use tokio::time::timeout;
use tracing::{debug, warn};

use super::types::*;

/// Active health checker that periodically probes backend endpoints.
pub struct HealthChecker {
    config: HealthCheckConfig,
    states: Arc<RwLock<HashMap<SocketAddr, HealthState>>>,
    /// Tracks consecutive failure count before the unhealthy threshold is met.
    /// Once threshold is reached, the count moves into `HealthState::Unhealthy`.
    failure_counts: Arc<RwLock<HashMap<SocketAddr, u32>>>,
}

impl HealthChecker {
    /// Create a new health checker with the given configuration.
    pub fn new(config: HealthCheckConfig) -> Self {
        Self {
            config,
            states: Arc::new(RwLock::new(HashMap::new())),
            failure_counts: Arc::new(RwLock::new(HashMap::new())),
        }
    }

    /// Perform a single health check against the given address.
    ///
    /// Returns `true` if the backend transitioned to (or remains in)
    /// the healthy state.
    pub async fn check(&self, addr: SocketAddr) -> bool {
        let result = match &self.config.check {
            HealthCheckType::Tcp(tcp) => self.check_tcp(addr, tcp).await,
            HealthCheckType::Http(http) => self.check_http(addr, http).await,
            HealthCheckType::Grpc(grpc) => self.check_grpc(addr, grpc).await,
        };

        match &result {
            Ok(()) => debug!(%addr, "health check passed"),
            Err(e) => debug!(%addr, error = %e, "health check failed"),
        }

        self.update_state(addr, result).await
    }

    async fn check_tcp(&self, addr: SocketAddr, _cfg: &TcpHealthCheck) -> Result<(), String> {
        timeout(self.config.timeout, TcpStream::connect(addr))
            .await
            .map_err(|_| "timeout".to_string())?
            .map_err(|e| e.to_string())?;
        Ok(())
    }

    async fn check_http(&self, addr: SocketAddr, cfg: &HttpHealthCheck) -> Result<(), String> {
        let mut stream = timeout(self.config.timeout, TcpStream::connect(addr))
            .await
            .map_err(|_| "timeout".to_string())?
            .map_err(|e| e.to_string())?;

        let host = cfg.host.as_deref().unwrap_or("localhost");
        let req = format!(
            "{} {} HTTP/1.1\r\nHost: {}\r\nConnection: close\r\n\r\n",
            cfg.method, cfg.path, host
        );
        stream
            .write_all(req.as_bytes())
            .await
            .map_err(|e| e.to_string())?;

        let mut buf = vec![0u8; 1024];
        let n = stream.read(&mut buf).await.map_err(|e| e.to_string())?;
        let response = String::from_utf8_lossy(&buf[..n]);

        // Parse status code from "HTTP/1.1 200 OK".
        let status: u16 = response
            .split_whitespace()
            .nth(1)
            .and_then(|s| s.parse().ok())
            .unwrap_or(0);

        if cfg.expected_statuses.contains(&status) {
            Ok(())
        } else {
            Err(format!("unexpected status: {status}"))
        }
    }

    async fn check_grpc(&self, addr: SocketAddr, _cfg: &GrpcHealthCheck) -> Result<(), String> {
        // TCP connect check for gRPC backends. A full grpc.health.v1.Health
        // probe would require a tonic channel per backend which is expensive
        // for periodic health checks. TCP connect detects port-level failures
        // and is the standard fallback used by Envoy and other proxies.
        timeout(self.config.timeout, TcpStream::connect(addr))
            .await
            .map_err(|_| "timeout".to_string())?
            .map_err(|e| e.to_string())?;
        Ok(())
    }

    async fn update_state(&self, addr: SocketAddr, result: Result<(), String>) -> bool {
        let mut states = self.states.write().await;
        let state = states.entry(addr).or_insert(HealthState::Unknown);
        let mut failure_counts = self.failure_counts.write().await;

        match result {
            Ok(()) => {
                // Reset consecutive failure counter on success.
                failure_counts.remove(&addr);

                let count = match state {
                    HealthState::Healthy {
                        consecutive_successes,
                    } => *consecutive_successes + 1,
                    _ => 1,
                };
                let is_healthy = count >= self.config.healthy_threshold;
                *state = HealthState::Healthy {
                    consecutive_successes: count,
                };
                is_healthy
            }
            Err(err) => {
                let count = failure_counts.entry(addr).or_insert(0);
                *count += 1;
                let consecutive = *count;

                warn!(%addr, error = %err, consecutive = consecutive, "backend unhealthy");

                // Only transition to Unhealthy once the threshold is met.
                if consecutive >= self.config.unhealthy_threshold {
                    *state = HealthState::Unhealthy {
                        consecutive_failures: consecutive,
                        last_error: err,
                    };
                    false
                } else {
                    // Below threshold — keep previous state, backend still considered OK.
                    !matches!(state, HealthState::Unhealthy { .. })
                }
            }
        }
    }

    /// Get the current health state of a backend.
    #[allow(dead_code)]
    pub async fn get_state(&self, addr: &SocketAddr) -> HealthState {
        self.states
            .read()
            .await
            .get(addr)
            .cloned()
            .unwrap_or(HealthState::Unknown)
    }

    /// Check whether a backend is currently healthy.
    ///
    /// Can be used by the proxy handler to filter unhealthy backends
    /// before load balancing selection.
    #[allow(dead_code)]
    pub async fn is_healthy(&self, addr: &SocketAddr) -> bool {
        self.states
            .read()
            .await
            .get(addr)
            .map(|s| s.is_healthy())
            .unwrap_or(false)
    }

    /// Run periodic health checks for a list of backends.
    ///
    /// Runs until the `cancel` signal is received. An alternative to
    /// the manual check loop in main.rs for static backend lists.
    #[allow(dead_code)]
    pub async fn run(
        &self,
        backends: Vec<SocketAddr>,
        mut cancel: tokio::sync::watch::Receiver<bool>,
    ) {
        let mut interval = tokio::time::interval(self.config.interval);
        loop {
            tokio::select! {
                _ = interval.tick() => {
                    for &addr in &backends {
                        self.check(addr).await;
                    }
                }
                _ = cancel.changed() => {
                    debug!("health checker cancelled");
                    return;
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::{IpAddr, Ipv4Addr};
    use std::time::Duration;
    use tokio::net::TcpListener;

    fn test_addr(port: u16) -> SocketAddr {
        SocketAddr::new(IpAddr::V4(Ipv4Addr::new(127, 0, 0, 1)), port)
    }

    #[tokio::test]
    async fn tcp_check_with_listener() {
        // Start a TCP listener.
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();

        let checker = HealthChecker::new(HealthCheckConfig {
            timeout: Duration::from_secs(1),
            healthy_threshold: 1,
            unhealthy_threshold: 1,
            check: HealthCheckType::Tcp(TcpHealthCheck {
                send: None,
                receive: None,
            }),
            ..Default::default()
        });

        assert!(checker.check(addr).await);
        assert!(checker.is_healthy(&addr).await);
    }

    #[tokio::test]
    async fn tcp_check_fails_no_listener() {
        // Use a port that is very unlikely to be in use.
        let addr = test_addr(19999);

        let checker = HealthChecker::new(HealthCheckConfig {
            timeout: Duration::from_millis(100),
            healthy_threshold: 1,
            unhealthy_threshold: 1,
            check: HealthCheckType::Tcp(TcpHealthCheck {
                send: None,
                receive: None,
            }),
            ..Default::default()
        });

        assert!(!checker.check(addr).await);
        assert!(!checker.is_healthy(&addr).await);

        match checker.get_state(&addr).await {
            HealthState::Unhealthy {
                consecutive_failures,
                ..
            } => {
                assert_eq!(consecutive_failures, 1);
            }
            other => panic!("expected Unhealthy, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn health_state_transitions() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();

        let checker = HealthChecker::new(HealthCheckConfig {
            timeout: Duration::from_secs(1),
            healthy_threshold: 2,
            unhealthy_threshold: 2,
            check: HealthCheckType::Tcp(TcpHealthCheck {
                send: None,
                receive: None,
            }),
            ..Default::default()
        });

        // First check: healthy but below threshold.
        assert!(!checker.check(addr).await);
        match checker.get_state(&addr).await {
            HealthState::Healthy {
                consecutive_successes,
            } => assert_eq!(consecutive_successes, 1),
            other => panic!("expected Healthy with 1 success, got {other:?}"),
        }

        // Second check: now above threshold.
        assert!(checker.check(addr).await);
        match checker.get_state(&addr).await {
            HealthState::Healthy {
                consecutive_successes,
            } => assert_eq!(consecutive_successes, 2),
            other => panic!("expected Healthy with 2 successes, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn http_check_parses_status() {
        // Start a simple HTTP server.
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();

        tokio::spawn(async move {
            loop {
                let (mut stream, _) = listener.accept().await.unwrap();
                let mut buf = vec![0u8; 1024];
                let _ = stream.read(&mut buf).await;
                let response = "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok";
                stream.write_all(response.as_bytes()).await.unwrap();
            }
        });

        let checker = HealthChecker::new(HealthCheckConfig {
            timeout: Duration::from_secs(1),
            healthy_threshold: 1,
            check: HealthCheckType::Http(HttpHealthCheck {
                path: "/healthz".into(),
                host: None,
                expected_statuses: vec![200],
                method: "GET".into(),
            }),
            ..Default::default()
        });

        assert!(checker.check(addr).await);
    }

    #[tokio::test]
    async fn http_check_rejects_unexpected_status() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();

        tokio::spawn(async move {
            loop {
                let (mut stream, _) = listener.accept().await.unwrap();
                let mut buf = vec![0u8; 1024];
                let _ = stream.read(&mut buf).await;
                let response = "HTTP/1.1 503 Service Unavailable\r\nContent-Length: 0\r\n\r\n";
                stream.write_all(response.as_bytes()).await.unwrap();
            }
        });

        let checker = HealthChecker::new(HealthCheckConfig {
            timeout: Duration::from_secs(1),
            healthy_threshold: 1,
            check: HealthCheckType::Http(HttpHealthCheck {
                path: "/healthz".into(),
                host: None,
                expected_statuses: vec![200],
                method: "GET".into(),
            }),
            ..Default::default()
        });

        assert!(!checker.check(addr).await);
    }

    #[tokio::test]
    async fn unknown_state_by_default() {
        let checker = HealthChecker::new(HealthCheckConfig::default());
        let addr = test_addr(12345);
        match checker.get_state(&addr).await {
            HealthState::Unknown => {}
            other => panic!("expected Unknown, got {other:?}"),
        }
        assert!(!checker.is_healthy(&addr).await);
    }

    #[tokio::test]
    async fn periodic_checking_with_cancellation() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();

        let checker = Arc::new(HealthChecker::new(HealthCheckConfig {
            interval: Duration::from_millis(50),
            timeout: Duration::from_secs(1),
            healthy_threshold: 1,
            check: HealthCheckType::Tcp(TcpHealthCheck {
                send: None,
                receive: None,
            }),
            ..Default::default()
        }));

        let (cancel_tx, cancel_rx) = tokio::sync::watch::channel(false);

        let checker_clone = checker.clone();
        let handle = tokio::spawn(async move {
            checker_clone.run(vec![addr], cancel_rx).await;
        });

        // Wait for a few check cycles.
        tokio::time::sleep(Duration::from_millis(200)).await;

        // Backend should be healthy.
        assert!(checker.is_healthy(&addr).await);

        // Cancel the checker.
        cancel_tx.send(true).unwrap();
        handle.await.unwrap();
    }
}
