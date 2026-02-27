//! Security headers middleware.
//!
//! Adds standard security headers to HTTP responses (HSTS, X-Frame-Options,
//! CSP, etc.).

/// Security headers configuration.
#[derive(Debug, Clone)]
pub struct SecurityHeadersConfig {
    /// HSTS `max-age` in seconds (`None` to omit the header).
    pub hsts_max_age: Option<u64>,
    /// Whether to include `includeSubDomains` in the HSTS header.
    pub hsts_include_subdomains: bool,
    /// Whether to add `X-Content-Type-Options: nosniff`.
    pub content_type_nosniff: bool,
    /// `X-Frame-Options` value (`"DENY"` or `"SAMEORIGIN"`), or `None` to omit.
    pub frame_options: Option<String>,
    /// Whether to add the legacy `X-XSS-Protection` header.
    pub xss_protection: bool,
    /// `Referrer-Policy` value, or `None` to omit.
    pub referrer_policy: Option<String>,
    /// `Content-Security-Policy` value, or `None` to omit.
    pub csp: Option<String>,
    /// `Permissions-Policy` value, or `None` to omit.
    pub permissions_policy: Option<String>,
    /// Additional custom headers to inject.
    pub custom_headers: Vec<(String, String)>,
}

impl Default for SecurityHeadersConfig {
    fn default() -> Self {
        Self {
            hsts_max_age: Some(31_536_000), // 1 year
            hsts_include_subdomains: true,
            content_type_nosniff: true,
            frame_options: Some("DENY".into()),
            xss_protection: true,
            referrer_policy: Some("strict-origin-when-cross-origin".into()),
            csp: None,
            permissions_policy: None,
            custom_headers: vec![],
        }
    }
}

/// Security headers middleware.
pub struct SecurityHeaders {
    config: SecurityHeadersConfig,
}

impl SecurityHeaders {
    /// Create a new security headers middleware.
    pub fn new(config: SecurityHeadersConfig) -> Self {
        Self { config }
    }

    /// Apply security headers to a response.
    pub fn apply(&self, resp: &mut super::Response) {
        if let Some(max_age) = self.config.hsts_max_age {
            let value = if self.config.hsts_include_subdomains {
                format!("max-age={max_age}; includeSubDomains")
            } else {
                format!("max-age={max_age}")
            };
            resp.headers
                .push(("Strict-Transport-Security".into(), value));
        }

        if self.config.content_type_nosniff {
            resp.headers
                .push(("X-Content-Type-Options".into(), "nosniff".into()));
        }

        if let Some(fo) = &self.config.frame_options {
            resp.headers.push(("X-Frame-Options".into(), fo.clone()));
        }

        if self.config.xss_protection {
            resp.headers
                .push(("X-XSS-Protection".into(), "1; mode=block".into()));
        }

        if let Some(rp) = &self.config.referrer_policy {
            resp.headers.push(("Referrer-Policy".into(), rp.clone()));
        }

        if let Some(csp) = &self.config.csp {
            resp.headers
                .push(("Content-Security-Policy".into(), csp.clone()));
        }

        if let Some(pp) = &self.config.permissions_policy {
            resp.headers.push(("Permissions-Policy".into(), pp.clone()));
        }

        for (k, v) in &self.config.custom_headers {
            resp.headers.push((k.clone(), v.clone()));
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn empty_resp() -> crate::middleware::Response {
        crate::middleware::Response::with_status(200, "ok")
    }

    fn has_header(resp: &crate::middleware::Response, name: &str) -> Option<String> {
        resp.headers
            .iter()
            .find(|(k, _)| k.eq_ignore_ascii_case(name))
            .map(|(_, v)| v.clone())
    }

    #[test]
    fn default_headers_applied() {
        let sh = SecurityHeaders::new(SecurityHeadersConfig::default());
        let mut resp = empty_resp();
        sh.apply(&mut resp);

        assert!(has_header(&resp, "Strict-Transport-Security")
            .unwrap()
            .contains("max-age=31536000"));
        assert!(has_header(&resp, "Strict-Transport-Security")
            .unwrap()
            .contains("includeSubDomains"));
        assert_eq!(
            has_header(&resp, "X-Content-Type-Options"),
            Some("nosniff".into())
        );
        assert_eq!(has_header(&resp, "X-Frame-Options"), Some("DENY".into()));
        assert_eq!(
            has_header(&resp, "X-XSS-Protection"),
            Some("1; mode=block".into())
        );
        assert_eq!(
            has_header(&resp, "Referrer-Policy"),
            Some("strict-origin-when-cross-origin".into())
        );
    }

    #[test]
    fn hsts_without_subdomains() {
        let sh = SecurityHeaders::new(SecurityHeadersConfig {
            hsts_include_subdomains: false,
            ..SecurityHeadersConfig::default()
        });
        let mut resp = empty_resp();
        sh.apply(&mut resp);

        let hsts = has_header(&resp, "Strict-Transport-Security").unwrap();
        assert!(hsts.contains("max-age=31536000"));
        assert!(!hsts.contains("includeSubDomains"));
    }

    #[test]
    fn hsts_disabled() {
        let sh = SecurityHeaders::new(SecurityHeadersConfig {
            hsts_max_age: None,
            ..SecurityHeadersConfig::default()
        });
        let mut resp = empty_resp();
        sh.apply(&mut resp);

        assert!(has_header(&resp, "Strict-Transport-Security").is_none());
    }

    #[test]
    fn frame_options_sameorigin() {
        let sh = SecurityHeaders::new(SecurityHeadersConfig {
            frame_options: Some("SAMEORIGIN".into()),
            ..SecurityHeadersConfig::default()
        });
        let mut resp = empty_resp();
        sh.apply(&mut resp);

        assert_eq!(
            has_header(&resp, "X-Frame-Options"),
            Some("SAMEORIGIN".into())
        );
    }

    #[test]
    fn frame_options_disabled() {
        let sh = SecurityHeaders::new(SecurityHeadersConfig {
            frame_options: None,
            ..SecurityHeadersConfig::default()
        });
        let mut resp = empty_resp();
        sh.apply(&mut resp);

        assert!(has_header(&resp, "X-Frame-Options").is_none());
    }

    #[test]
    fn csp_header() {
        let sh = SecurityHeaders::new(SecurityHeadersConfig {
            csp: Some("default-src 'self'".into()),
            ..SecurityHeadersConfig::default()
        });
        let mut resp = empty_resp();
        sh.apply(&mut resp);

        assert_eq!(
            has_header(&resp, "Content-Security-Policy"),
            Some("default-src 'self'".into())
        );
    }

    #[test]
    fn permissions_policy_header() {
        let sh = SecurityHeaders::new(SecurityHeadersConfig {
            permissions_policy: Some("geolocation=(), camera=()".into()),
            ..SecurityHeadersConfig::default()
        });
        let mut resp = empty_resp();
        sh.apply(&mut resp);

        assert_eq!(
            has_header(&resp, "Permissions-Policy"),
            Some("geolocation=(), camera=()".into())
        );
    }

    #[test]
    fn custom_headers() {
        let sh = SecurityHeaders::new(SecurityHeadersConfig {
            custom_headers: vec![
                ("X-Custom-One".into(), "value1".into()),
                ("X-Custom-Two".into(), "value2".into()),
            ],
            ..SecurityHeadersConfig::default()
        });
        let mut resp = empty_resp();
        sh.apply(&mut resp);

        assert_eq!(has_header(&resp, "X-Custom-One"), Some("value1".into()));
        assert_eq!(has_header(&resp, "X-Custom-Two"), Some("value2".into()));
    }

    #[test]
    fn xss_protection_disabled() {
        let sh = SecurityHeaders::new(SecurityHeadersConfig {
            xss_protection: false,
            ..SecurityHeadersConfig::default()
        });
        let mut resp = empty_resp();
        sh.apply(&mut resp);

        assert!(has_header(&resp, "X-XSS-Protection").is_none());
    }

    #[test]
    fn nosniff_disabled() {
        let sh = SecurityHeaders::new(SecurityHeadersConfig {
            content_type_nosniff: false,
            ..SecurityHeadersConfig::default()
        });
        let mut resp = empty_resp();
        sh.apply(&mut resp);

        assert!(has_header(&resp, "X-Content-Type-Options").is_none());
    }

    #[test]
    fn referrer_policy_disabled() {
        let sh = SecurityHeaders::new(SecurityHeadersConfig {
            referrer_policy: None,
            ..SecurityHeadersConfig::default()
        });
        let mut resp = empty_resp();
        sh.apply(&mut resp);

        assert!(has_header(&resp, "Referrer-Policy").is_none());
    }
}
