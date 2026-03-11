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

        // Reject CR/LF in host or path to prevent HTTP request smuggling via
        // a maliciously crafted auth_url config value.
        if contains_cr_lf(&host) || contains_cr_lf(&path) {
            return super::AuthResult::Denied {
                status: 500,
                message: "Auth URL contains invalid characters".into(),
            };
        }

        // Build forwarded request with selected headers (sanitised against injection).
        let mut forward_headers = String::new();
        for header_name in &self.config.auth_request_headers {
            if let Some((_, value)) = req
                .headers
                .iter()
                .find(|(k, _)| k.eq_ignore_ascii_case(header_name))
            {
                // Reject header names/values containing CR or LF to prevent request smuggling.
                if contains_cr_lf(header_name) || contains_cr_lf(value) {
                    tracing::warn!(
                        header = %header_name,
                        "Dropping header with CR/LF characters to prevent request smuggling"
                    );
                    continue;
                }
                forward_headers.push_str(&format!("{header_name}: {value}\r\n"));
            }
        }

        let request = format!(
            "GET {path} HTTP/1.1\r\nHost: {host}\r\nConnection: close\r\n{forward_headers}\r\n",
        );

        let addr = format!("{host}:{port}");

        // SSRF protection: resolve DNS and check against denylist before connecting.
        let resolved = match tokio::net::lookup_host(&addr).await {
            Ok(addrs) => addrs.collect::<Vec<_>>(),
            Err(_) => {
                return super::AuthResult::Denied {
                    status: 503,
                    message: "Auth service DNS resolution failed".into(),
                };
            }
        };
        for sock_addr in &resolved {
            if is_denied_ip(&sock_addr.ip()) {
                tracing::warn!(
                    addr = %sock_addr,
                    auth_url = %self.config.auth_url,
                    "Forward auth URL resolves to denied internal IP — blocking SSRF"
                );
                return super::AuthResult::Denied {
                    status: 403,
                    message: "Auth URL resolves to a denied internal address".into(),
                };
            }
        }

        // Connect directly to the first resolved address to prevent DNS rebinding:
        // a second DNS lookup after the denylist check could return a different IP.
        let first_addr = resolved[0];
        match tokio::time::timeout(self.config.timeout, async move {
            let mut stream = TcpStream::connect(first_addr).await?;
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

/// Returns `true` if the string contains any CR (`\r`) or LF (`\n`) characters.
fn contains_cr_lf(s: &str) -> bool {
    s.bytes().any(|b| b == b'\r' || b == b'\n')
}

/// Check whether an IP address belongs to a denied range (internal/metadata).
fn is_denied_ip(ip: &std::net::IpAddr) -> bool {
    match ip {
        std::net::IpAddr::V4(v4) => {
            v4.is_loopback()                              // 127.0.0.0/8
                || v4.octets()[0] == 10                   // 10.0.0.0/8
                || (v4.octets()[0] == 172 && (v4.octets()[1] & 0xf0) == 16)  // 172.16.0.0/12
                || (v4.octets()[0] == 192 && v4.octets()[1] == 168)          // 192.168.0.0/16
                || (v4.octets()[0] == 169 && v4.octets()[1] == 254) // 169.254.0.0/16 (link-local / cloud metadata)
        }
        std::net::IpAddr::V6(v6) => {
            // Detect IPv4-mapped IPv6 (::ffff:x.x.x.x) and check the inner v4 address.
            if let Some(v4) = v6.to_ipv4_mapped() {
                return is_denied_ip(&std::net::IpAddr::V4(v4));
            }
            v6.is_loopback()                              // ::1
                || (v6.octets()[0] & 0xfe) == 0xfc        // fc00::/7 (ULA)
                || v6.octets()[0] == 0xfe && (v6.octets()[1] & 0xc0) == 0x80 // fe80::/10 (link-local)
        }
    }
}

/// Parse an HTTP or HTTPS URL into (host, port, path).
#[allow(dead_code)] // Used by ForwardAuth::check() which requires async.
fn parse_url(url: &str) -> (String, u16, String) {
    let (rest, default_port) = if let Some(stripped) = url.strip_prefix("https://") {
        (stripped, 443u16)
    } else if let Some(stripped) = url.strip_prefix("http://") {
        (stripped, 80u16)
    } else {
        (url, 80u16)
    };
    let (host_port, path) = rest.split_once('/').unwrap_or((rest, ""));
    let path = format!("/{path}");
    let (host, port) = host_port
        .split_once(':')
        .map(|(h, p)| (h.to_string(), p.parse().unwrap_or(default_port)))
        .unwrap_or_else(|| (host_port.to_string(), default_port));
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
    fn parse_url_https_with_port() {
        let (host, port, path) = parse_url("https://auth-svc:9443/verify");
        assert_eq!(host, "auth-svc");
        assert_eq!(port, 9443);
        assert_eq!(path, "/verify");
    }

    #[test]
    fn parse_url_https_default_port() {
        let (host, port, path) = parse_url("https://auth-svc/check");
        assert_eq!(host, "auth-svc");
        assert_eq!(port, 443);
        assert_eq!(path, "/check");
    }

    #[test]
    fn parse_url_no_scheme() {
        let (host, port, path) = parse_url("auth-svc:8080/verify");
        assert_eq!(host, "auth-svc");
        assert_eq!(port, 8080);
        assert_eq!(path, "/verify");
    }

    #[tokio::test]
    async fn forward_auth_loopback_blocked() {
        // 127.0.0.1 is a denied internal address; the SSRF denylist must block it
        // before any connection attempt, returning 403 rather than a timeout 503.
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
            super::super::AuthResult::Denied { status, message } => {
                assert_eq!(status, 403);
                assert!(message.contains("denied"), "got: {message}");
            }
            other => panic!("expected Denied/403, got: {other:?}"),
        }
    }

    #[test]
    fn cr_lf_detection() {
        assert!(contains_cr_lf("value\r\nEvil: injected"));
        assert!(contains_cr_lf("value\revil"));
        assert!(contains_cr_lf("value\nevil"));
        assert!(!contains_cr_lf("clean-value"));
        assert!(!contains_cr_lf(""));
    }

    #[test]
    fn ssrf_denylist() {
        use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};

        // Denied ranges
        assert!(is_denied_ip(&IpAddr::V4(Ipv4Addr::new(127, 0, 0, 1))));
        assert!(is_denied_ip(&IpAddr::V4(Ipv4Addr::new(10, 0, 0, 1))));
        assert!(is_denied_ip(&IpAddr::V4(Ipv4Addr::new(172, 16, 0, 1))));
        assert!(is_denied_ip(&IpAddr::V4(Ipv4Addr::new(172, 31, 255, 255))));
        assert!(is_denied_ip(&IpAddr::V4(Ipv4Addr::new(192, 168, 1, 1))));
        assert!(is_denied_ip(&IpAddr::V4(Ipv4Addr::new(169, 254, 169, 254))));
        assert!(is_denied_ip(&IpAddr::V6(Ipv6Addr::LOCALHOST)));
        assert!(is_denied_ip(&IpAddr::V6(Ipv6Addr::new(
            0xfd00, 0, 0, 0, 0, 0, 0, 1
        ))));
        // fc00::/8 (ULA — the other half of fc00::/7)
        assert!(is_denied_ip(&IpAddr::V6(Ipv6Addr::new(
            0xfc00, 0, 0, 0, 0, 0, 0, 1
        ))));
        // IPv4-mapped IPv6 addresses must be checked against the v4 denylist
        assert!(is_denied_ip(&IpAddr::V6(Ipv6Addr::new(
            0, 0, 0, 0, 0, 0xffff, 0x7f00, 0x0001 // ::ffff:127.0.0.1
        ))));
        assert!(is_denied_ip(&IpAddr::V6(Ipv6Addr::new(
            0, 0, 0, 0, 0, 0xffff, 0xa9fe, 0xa9fe // ::ffff:169.254.169.254
        ))));

        // Allowed ranges
        assert!(!is_denied_ip(&IpAddr::V4(Ipv4Addr::new(8, 8, 8, 8))));
        assert!(!is_denied_ip(&IpAddr::V4(Ipv4Addr::new(172, 32, 0, 1))));
        assert!(!is_denied_ip(&IpAddr::V6(Ipv6Addr::new(
            0x2001, 0xdb8, 0, 0, 0, 0, 0, 1
        ))));
    }

    #[tokio::test]
    async fn forward_auth_ssrf_blocked() {
        let fa = ForwardAuth::new(ForwardAuthConfig {
            auth_url: "http://169.254.169.254/latest/meta-data".into(),
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
            super::super::AuthResult::Denied { status, message } => {
                assert_eq!(status, 403);
                assert!(message.contains("denied"), "got: {message}");
            }
            other => panic!("expected Denied/403, got: {other:?}"),
        }
    }
}
