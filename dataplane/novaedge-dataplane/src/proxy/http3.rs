//! HTTP/3 QUIC support using quinn + h3 + h3-quinn.
//!
//! This module provides an HTTP/3 server that accepts QUIC connections,
//! negotiates HTTP/3 via the h3 crate, and serves basic HTTP responses.
//! TLS is mandatory for QUIC and is configured from PEM-encoded certificates.

use std::net::SocketAddr;
use std::sync::Arc;

use bytes::Bytes;
use tokio::sync::watch;
use tracing::{debug, info, warn};

use crate::config::TlsState;
use crate::proxy::handler::ProxyHandler;

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

/// HTTP/3 server that accepts QUIC connections and serves HTTP/3 requests.
pub struct Http3Server {
    config: Http3Config,
    handler: Arc<ProxyHandler>,
}

impl Http3Server {
    /// Create a new HTTP/3 server with the given configuration and proxy handler.
    pub fn new(config: Http3Config, handler: Arc<ProxyHandler>) -> Self {
        Self { config, handler }
    }

    /// Return a reference to the server configuration.
    pub fn config(&self) -> &Http3Config {
        &self.config
    }

    /// Start the HTTP/3 QUIC listener.
    ///
    /// Loads TLS certificates from the provided [`TlsState`], builds a quinn
    /// `Endpoint` bound to the configured UDP address, and accepts incoming
    /// QUIC connections. Each connection is handled in a spawned task that
    /// negotiates HTTP/3 and serves requests.
    ///
    /// The server runs until a shutdown signal is received via the `shutdown`
    /// watch channel.
    pub async fn start(
        &self,
        tls: &TlsState,
        mut shutdown: watch::Receiver<bool>,
    ) -> Result<(), String> {
        // Parse TLS certificates and private key from PEM.
        let certs = rustls_pemfile::certs(&mut std::io::BufReader::new(&tls.cert_pem[..]))
            .collect::<Result<Vec<_>, _>>()
            .map_err(|e| format!("parse certs: {e}"))?;

        let key = rustls_pemfile::private_key(&mut std::io::BufReader::new(&tls.key_pem[..]))
            .map_err(|e| format!("parse key: {e}"))?
            .ok_or_else(|| "no private key found".to_string())?;

        // Build rustls ServerConfig with h3 ALPN.
        let mut tls_config = rustls::ServerConfig::builder()
            .with_no_client_auth()
            .with_single_cert(certs, key)
            .map_err(|e| format!("TLS config: {e}"))?;
        tls_config.alpn_protocols = vec![b"h3".to_vec()];

        // Convert to quinn-compatible QUIC server config.
        let quic_server_config = quinn::crypto::rustls::QuicServerConfig::try_from(tls_config)
            .map_err(|e| format!("QUIC TLS config: {e}"))?;

        let mut server_config = quinn::ServerConfig::with_crypto(Arc::new(quic_server_config));

        // Apply transport parameters from our config.
        let mut transport = quinn::TransportConfig::default();
        transport.max_concurrent_bidi_streams(
            quinn::VarInt::from_u64(self.config.max_streams)
                .map_err(|e| format!("max_streams too large: {e}"))?,
        );
        transport.max_idle_timeout(Some(
            quinn::IdleTimeout::try_from(std::time::Duration::from_millis(
                self.config.idle_timeout_ms,
            ))
            .map_err(|e| format!("idle timeout: {e}"))?,
        ));
        server_config.transport_config(Arc::new(transport));

        // Bind the QUIC endpoint to the configured UDP address.
        let endpoint = quinn::Endpoint::server(server_config, self.config.listen_addr)
            .map_err(|e| format!("bind QUIC endpoint: {e}"))?;

        let actual_addr = endpoint
            .local_addr()
            .map_err(|e| format!("local addr: {e}"))?;
        info!(
            addr = %actual_addr,
            zero_rtt = self.config.enable_0rtt,
            max_streams = self.config.max_streams,
            "HTTP/3 QUIC listener started"
        );

        loop {
            tokio::select! {
                conn = endpoint.accept() => {
                    match conn {
                        Some(incoming) => {
                            let handler = self.handler.clone();
                            tokio::spawn(async move {
                                if let Err(e) = handle_h3_connection(incoming, handler).await {
                                    debug!(error = %e, "HTTP/3 connection ended");
                                }
                            });
                        }
                        None => {
                            info!("QUIC endpoint closed");
                            break;
                        }
                    }
                }
                _ = shutdown.changed() => {
                    info!("HTTP/3 listener shutting down");
                    endpoint.close(0u32.into(), b"shutdown");
                    break;
                }
            }
        }

        Ok(())
    }
}

/// Handle a single QUIC connection by negotiating HTTP/3 and serving requests.
async fn handle_h3_connection(
    incoming: quinn::Incoming,
    _handler: Arc<ProxyHandler>,
) -> Result<(), Box<dyn std::error::Error>> {
    let conn = incoming.await?;
    debug!(
        remote = %conn.remote_address(),
        "Accepted QUIC connection"
    );

    let mut h3_conn = h3::server::builder()
        .build(h3_quinn::Connection::new(conn))
        .await?;

    loop {
        match h3_conn.accept().await {
            Ok(Some(resolver)) => {
                // Spawn a task to handle the request concurrently.
                tokio::spawn(async move {
                    // Resolve the request from the resolver.
                    let (req, mut stream) = match resolver.resolve_request().await {
                        Ok(pair) => pair,
                        Err(e) => {
                            debug!(error = %e, "HTTP/3 resolve request failed");
                            return;
                        }
                    };

                    debug!(
                        method = %req.method(),
                        uri = %req.uri(),
                        "HTTP/3 request received"
                    );

                    // Build and send the HTTP response.
                    // NOTE: This is a basic implementation that returns 200 OK.
                    // Full proxy handler integration (converting h3 requests to
                    // hyper requests and forwarding to backends) is a future
                    // enhancement.
                    let resp = http::Response::builder().status(200).body(()).unwrap();

                    if let Err(e) = stream.send_response(resp).await {
                        debug!(error = %e, "HTTP/3 send response failed");
                        return;
                    }

                    if let Err(e) = stream.send_data(Bytes::from("HTTP/3 OK")).await {
                        debug!(error = %e, "HTTP/3 send data failed");
                        return;
                    }

                    if let Err(e) = stream.finish().await {
                        debug!(error = %e, "HTTP/3 finish stream failed");
                    }
                });
            }
            Ok(None) => {
                // Connection closed gracefully.
                debug!("HTTP/3 connection closed");
                break;
            }
            Err(e) => {
                warn!(error = %e, "HTTP/3 accept error");
                break;
            }
        }
    }

    Ok(())
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

    #[test]
    fn test_custom_config() {
        let config = Http3Config {
            listen_addr: "0.0.0.0:8443".parse().unwrap(),
            enable_0rtt: false,
            max_streams: 200,
            idle_timeout_ms: 60000,
        };
        assert_eq!(
            config.listen_addr,
            "0.0.0.0:8443".parse::<SocketAddr>().unwrap()
        );
        assert!(!config.enable_0rtt);
        assert_eq!(config.max_streams, 200);
        assert_eq!(config.idle_timeout_ms, 60000);
    }
}
