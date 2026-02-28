//! HTTP/3 QUIC support — pending quinn + h3 + h3-quinn dependencies.
//!
//! This module defines the HTTP/3 configuration types and server interface.
//! The configuration is accepted and stored so that Alt-Svc headers can be
//! generated and advertised on HTTP/1.1 and HTTP/2 responses. The actual
//! QUIC listener will be wired once the `quinn`, `h3`, and `h3-quinn` crates
//! are added to `Cargo.toml`.
//!
//! Tracking: the `start()` method logs the config but does not bind a UDP
//! socket. When quinn is added, `start()` should create a `quinn::Endpoint`
//! and accept connections.

use std::net::SocketAddr;

/// HTTP/3 server configuration.
#[derive(Debug, Clone)]
pub struct Http3Config {
    /// UDP listen address for QUIC.
    pub listen_addr: SocketAddr,
    /// Whether to enable 0-RTT (early data).
    pub enable_0rtt: bool,
    /// Maximum concurrent bidirectional streams per connection.
    pub max_streams: u64,
    /// Connection idle timeout in milliseconds.
    pub idle_timeout_ms: u64,
}

impl Default for Http3Config {
    fn default() -> Self {
        Self {
            listen_addr: "0.0.0.0:443".parse().unwrap(),
            enable_0rtt: true,
            max_streams: 100,
            idle_timeout_ms: 30000,
        }
    }
}

/// Generate the `Alt-Svc` header value to advertise HTTP/3 availability.
pub fn alt_svc_header(port: u16) -> String {
    format!("h3=\":{port}\"; ma=86400")
}

/// HTTP/3 server state.
pub struct Http3Server {
    config: Http3Config,
    running: bool,
}

impl Http3Server {
    pub fn new(config: Http3Config) -> Self {
        Self {
            config,
            running: false,
        }
    }

    pub fn config(&self) -> &Http3Config {
        &self.config
    }

    pub fn is_running(&self) -> bool {
        self.running
    }

    /// Start the HTTP/3 server.
    ///
    /// Requires quinn + h3 + h3-quinn crates in Cargo.toml. When those
    /// dependencies are added, this method should create a `quinn::Endpoint`,
    /// configure TLS with rustls, and accept incoming QUIC connections.
    /// Until then, the server logs its configuration and marks itself as
    /// running so that Alt-Svc headers are emitted on HTTP/1.1 and HTTP/2.
    pub async fn start(&mut self) -> anyhow::Result<()> {
        tracing::info!(
            listen = %self.config.listen_addr,
            zero_rtt = self.config.enable_0rtt,
            max_streams = self.config.max_streams,
            "HTTP/3 QUIC server configured (pending quinn integration)"
        );
        self.running = true;
        Ok(())
    }

    /// Stop the HTTP/3 server.
    pub async fn stop(&mut self) {
        self.running = false;
        tracing::info!("HTTP/3 server stopped");
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_config_defaults() {
        let config = Http3Config::default();
        assert_eq!(
            config.listen_addr,
            "0.0.0.0:443".parse::<SocketAddr>().unwrap()
        );
        assert!(config.enable_0rtt);
        assert_eq!(config.max_streams, 100);
        assert_eq!(config.idle_timeout_ms, 30000);
    }

    #[test]
    fn test_alt_svc_header() {
        let header = alt_svc_header(443);
        assert_eq!(header, "h3=\":443\"; ma=86400");

        let header = alt_svc_header(8443);
        assert_eq!(header, "h3=\":8443\"; ma=86400");
    }

    #[tokio::test]
    async fn test_server_start_stop() {
        let mut server = Http3Server::new(Http3Config::default());
        assert!(!server.is_running());

        server.start().await.unwrap();
        assert!(server.is_running());

        server.stop().await;
        assert!(!server.is_running());
    }

    #[test]
    fn test_server_config_access() {
        let config = Http3Config {
            listen_addr: "0.0.0.0:8443".parse().unwrap(),
            enable_0rtt: false,
            max_streams: 200,
            idle_timeout_ms: 60000,
        };
        let server = Http3Server::new(config);
        assert_eq!(
            server.config().listen_addr,
            "0.0.0.0:8443".parse::<SocketAddr>().unwrap()
        );
        assert!(!server.config().enable_0rtt);
        assert_eq!(server.config().max_streams, 200);
        assert_eq!(server.config().idle_timeout_ms, 60000);
    }
}
