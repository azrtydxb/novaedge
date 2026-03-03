//! Forward authentication middleware.
//!
//! Delegates authentication decisions to an external HTTP service.
//! The request is forwarded (with selected headers) to an auth URL;
//! a 2xx response means "allow", anything else means "deny".

use std::time::Duration;

use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;

/// Forward auth configuration.
#[allow(dead_code)] // Forward auth requires async network calls; handled separately from sync pipeline.
#[derive(Debug, Clone)]
pub struct ForwardAuthConfig {
    /// URL of the authentication service (e.g. `http://auth-svc:8080/verify`).
    pub auth_url: String,
    /// Headers from the original request to forward to the auth service.
    pub auth_request_headers: Vec<String>,
    /// Headers from the auth response to propagate back to the client.
    pub auth_response_headers: Vec<String>,
    /// Connection/response timeout for the auth service.
    pub timeout: Duration,
}

/// Forward authentication handler.
#[allow(dead_code)] // Forward auth requires async network calls; handled separately from sync pipeline.
pub struct ForwardAuth {
    config: ForwardAuthConfig,
}

impl ForwardAuth {
    /// Create a new forward auth handler.
    #[allow(dead_code)]
    pub fn new(config: ForwardAuthConfig) -> Self {
        Self { config }
    }

    /// Check a request by forwarding it to the external auth service.
    #[allow(dead_code)]
    pub async fn check(&self, req: &super::super::Request) -> super::AuthResult {
        let (host, port, path) = parse_url(&self.config.auth_url);

        // Build forwarded request with selected headers.
        let mut forward_headers = String::new();
        for header_name in &self.config.auth_request_headers {
            if let Some((_, value)) = req
                .headers
                .iter()
                .find(|(k, _)| k.eq_ignore_ascii_case(header_name))
            {
                forward_headers.push_str(&format!("{header_name}: {value}\r\n"));
            }
        }

        let request = format!(
            "GET {path} HTTP/1.1\r\nHost: {host}\r\nConnection: close\r\n{forward_headers}\r\n",
        );

        let addr = format!("{host}:{port}");
        match tokio::time::timeout(self.config.timeout, async {
            let mut stream = TcpStream::connect(&addr).await?;
            stream.write_all(request.as_bytes()).await?;
            let mut response = vec![0u8; 4096];
            let n = stream.read(&mut response).await?;
            Ok::<_, anyhow::Error>(String::from_utf8_lossy(&response[..n]).to_string())
        })
        .await
        {
            Ok(Ok(response)) => {
                let status: u16 = response
                    .split_whitespace()
                    .nth(1)
                    .and_then(|s| s.parse().ok())
                    .unwrap_or(500);

                if (200..300).contains(&status) {
                    super::AuthResult::Authenticated {
                        user: "forward-auth".into(),
                        claims: vec![],
                    }
                } else {
                    super::AuthResult::Denied {
                        status,
                        message: "Forward auth denied".into(),
                    }
                }
            }
            _ => super::AuthResult::Denied {
                status: 503,
                message: "Auth service unavailable".into(),
            },
        }
    }
}

/// Parse a simple HTTP URL into (host, port, path).
#[allow(dead_code)] // Used by ForwardAuth::check() which requires async.
fn parse_url(url: &str) -> (String, u16, String) {
    let url = url.strip_prefix("http://").unwrap_or(url);
    let (host_port, path) = url.split_once('/').unwrap_or((url, ""));
    let path = format!("/{path}");
    let (host, port) = host_port
        .split_once(':')
        .map(|(h, p)| (h.to_string(), p.parse().unwrap_or(80)))
        .unwrap_or_else(|| (host_port.to_string(), 80));
    (host, port, path)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_url_with_port() {
        let (host, port, path) = parse_url("http://auth-svc:9090/verify");
        assert_eq!(host, "auth-svc");
        assert_eq!(port, 9090);
        assert_eq!(path, "/verify");
    }

    #[test]
    fn parse_url_without_port() {
        let (host, port, path) = parse_url("http://auth-svc/check");
        assert_eq!(host, "auth-svc");
        assert_eq!(port, 80);
        assert_eq!(path, "/check");
    }

    #[test]
    fn parse_url_no_path() {
        let (host, port, path) = parse_url("http://auth-svc:8080");
        assert_eq!(host, "auth-svc");
        assert_eq!(port, 8080);
        assert_eq!(path, "/");
    }

    #[test]
    fn parse_url_no_scheme() {
        let (host, port, path) = parse_url("auth-svc:8080/verify");
        assert_eq!(host, "auth-svc");
        assert_eq!(port, 8080);
        assert_eq!(path, "/verify");
    }

    #[tokio::test]
    async fn forward_auth_service_unavailable() {
        let fa = ForwardAuth::new(ForwardAuthConfig {
            auth_url: "http://127.0.0.1:19999/verify".into(),
            auth_request_headers: vec![],
            auth_response_headers: vec![],
            timeout: Duration::from_millis(100),
        });

        let req = crate::middleware::Request {
            method: "GET".into(),
            path: "/".into(),
            host: "example.com".into(),
            headers: vec![],
            body: None,
            client_ip: "127.0.0.1".into(),
        };

        match fa.check(&req).await {
            super::super::AuthResult::Denied { status, .. } => {
                assert_eq!(status, 503);
            }
            other => panic!("expected Denied/503, got: {other:?}"),
        }
    }
}
