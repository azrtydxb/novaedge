//! CORS (Cross-Origin Resource Sharing) middleware.

/// CORS configuration.
#[derive(Debug, Clone)]
pub struct CorsConfig {
    /// Allowed origins (`*` for wildcard, `*.example.com` for suffix match).
    pub allowed_origins: Vec<String>,
    /// Allowed HTTP methods.
    pub allowed_methods: Vec<String>,
    /// Allowed request headers.
    pub allowed_headers: Vec<String>,
    /// Response headers to expose to the browser.
    pub exposed_headers: Vec<String>,
    /// Whether to include `Access-Control-Allow-Credentials`.
    pub allow_credentials: bool,
    /// Max age (seconds) for preflight cache.
    pub max_age: u32,
}

impl Default for CorsConfig {
    fn default() -> Self {
        Self {
            allowed_origins: vec!["*".into()],
            allowed_methods: vec![
                "GET".into(),
                "POST".into(),
                "PUT".into(),
                "DELETE".into(),
                "OPTIONS".into(),
            ],
            allowed_headers: vec!["Content-Type".into(), "Authorization".into()],
            exposed_headers: vec![],
            allow_credentials: false,
            max_age: 86400,
        }
    }
}

/// CORS middleware handler.
pub struct CorsMiddleware {
    config: CorsConfig,
}

impl CorsMiddleware {
    /// Create a new CORS middleware with the given configuration.
    pub fn new(config: CorsConfig) -> Self {
        Self { config }
    }

    /// Check whether a request is a CORS preflight (OPTIONS with
    /// `Access-Control-Request-Method`).
    pub fn is_preflight(&self, req: &super::Request) -> bool {
        req.method == "OPTIONS"
            && req
                .headers
                .iter()
                .any(|(k, _)| k.eq_ignore_ascii_case("access-control-request-method"))
    }

    /// Generate a preflight response.
    pub fn handle_preflight(&self, req: &super::Request) -> super::Response {
        let origin = req
            .headers
            .iter()
            .find(|(k, _)| k.eq_ignore_ascii_case("origin"))
            .map(|(_, v)| v.as_str())
            .unwrap_or("*");

        let mut headers = vec![];

        if self.is_origin_allowed(origin) {
            let allowed_origin = if self.config.allow_credentials {
                origin.to_string()
            } else if self.config.allowed_origins.contains(&"*".to_string()) {
                "*".to_string()
            } else {
                origin.to_string()
            };
            headers.push(("Access-Control-Allow-Origin".into(), allowed_origin));
        }

        headers.push((
            "Access-Control-Allow-Methods".into(),
            self.config.allowed_methods.join(", "),
        ));
        headers.push((
            "Access-Control-Allow-Headers".into(),
            self.config.allowed_headers.join(", "),
        ));
        headers.push((
            "Access-Control-Max-Age".into(),
            self.config.max_age.to_string(),
        ));

        if self.config.allow_credentials {
            headers.push(("Access-Control-Allow-Credentials".into(), "true".into()));
        }

        super::Response {
            status: 204,
            headers,
            body: vec![],
        }
    }

    /// Add CORS headers to an existing response.
    pub fn add_cors_headers(&self, req: &super::Request, resp: &mut super::Response) {
        let origin = req
            .headers
            .iter()
            .find(|(k, _)| k.eq_ignore_ascii_case("origin"))
            .map(|(_, v)| v.as_str())
            .unwrap_or("*");

        if self.is_origin_allowed(origin) {
            let allowed_origin = if self.config.allow_credentials {
                origin.to_string()
            } else if self.config.allowed_origins.contains(&"*".to_string()) {
                "*".to_string()
            } else {
                origin.to_string()
            };
            resp.headers
                .push(("Access-Control-Allow-Origin".into(), allowed_origin));
        }

        if !self.config.exposed_headers.is_empty() {
            resp.headers.push((
                "Access-Control-Expose-Headers".into(),
                self.config.exposed_headers.join(", "),
            ));
        }

        if self.config.allow_credentials {
            resp.headers
                .push(("Access-Control-Allow-Credentials".into(), "true".into()));
        }

        // Vary on Origin so caches key by origin.
        resp.headers.push(("Vary".into(), "Origin".into()));
    }

    /// Check whether a given origin is allowed by the configuration.
    fn is_origin_allowed(&self, origin: &str) -> bool {
        self.config
            .allowed_origins
            .iter()
            .any(|o| o == "*" || o == origin || (o.starts_with("*.") && origin.ends_with(&o[1..])))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_req(method: &str, origin: Option<&str>) -> crate::middleware::Request {
        let mut headers = vec![];
        if let Some(o) = origin {
            headers.push(("Origin".into(), o.to_string()));
        }
        if method == "OPTIONS" {
            headers.push(("Access-Control-Request-Method".into(), "POST".into()));
        }
        crate::middleware::Request {
            method: method.into(),
            path: "/".into(),
            host: "example.com".into(),
            headers,
            body: None,
            client_ip: "127.0.0.1".into(),
        }
    }

    #[test]
    fn is_preflight_true() {
        let cors = CorsMiddleware::new(CorsConfig::default());
        let req = make_req("OPTIONS", Some("http://example.com"));
        assert!(cors.is_preflight(&req));
    }

    #[test]
    fn is_preflight_false_for_get() {
        let cors = CorsMiddleware::new(CorsConfig::default());
        let req = make_req("GET", Some("http://example.com"));
        assert!(!cors.is_preflight(&req));
    }

    #[test]
    fn is_preflight_false_without_acr_method() {
        let cors = CorsMiddleware::new(CorsConfig::default());
        let req = crate::middleware::Request {
            method: "OPTIONS".into(),
            path: "/".into(),
            host: "example.com".into(),
            headers: vec![],
            body: None,
            client_ip: "127.0.0.1".into(),
        };
        assert!(!cors.is_preflight(&req));
    }

    #[test]
    fn preflight_response_has_cors_headers() {
        let cors = CorsMiddleware::new(CorsConfig::default());
        let req = make_req("OPTIONS", Some("http://example.com"));
        let resp = cors.handle_preflight(&req);

        assert_eq!(resp.status, 204);
        assert!(resp
            .headers
            .iter()
            .any(|(k, _)| k == "Access-Control-Allow-Origin"));
        assert!(resp
            .headers
            .iter()
            .any(|(k, _)| k == "Access-Control-Allow-Methods"));
        assert!(resp
            .headers
            .iter()
            .any(|(k, _)| k == "Access-Control-Max-Age"));
    }

    #[test]
    fn wildcard_origin_allowed() {
        let cors = CorsMiddleware::new(CorsConfig::default());
        let req = make_req("GET", Some("http://anything.com"));
        let mut resp = crate::middleware::Response::with_status(200, "ok");
        cors.add_cors_headers(&req, &mut resp);

        let origin_header = resp
            .headers
            .iter()
            .find(|(k, _)| k == "Access-Control-Allow-Origin");
        assert_eq!(origin_header.unwrap().1, "*");
    }

    #[test]
    fn specific_origin_allowed() {
        let cors = CorsMiddleware::new(CorsConfig {
            allowed_origins: vec!["http://app.example.com".into()],
            ..CorsConfig::default()
        });

        let req = make_req("GET", Some("http://app.example.com"));
        let mut resp = crate::middleware::Response::with_status(200, "ok");
        cors.add_cors_headers(&req, &mut resp);

        let origin_header = resp
            .headers
            .iter()
            .find(|(k, _)| k == "Access-Control-Allow-Origin");
        assert_eq!(origin_header.unwrap().1, "http://app.example.com");
    }

    #[test]
    fn suffix_wildcard_origin() {
        let cors = CorsMiddleware::new(CorsConfig {
            allowed_origins: vec!["*.example.com".into()],
            ..CorsConfig::default()
        });

        // sub.example.com should match *.example.com
        assert!(cors.is_origin_allowed("sub.example.com"));
        assert!(!cors.is_origin_allowed("other.com"));
    }

    #[test]
    fn credentials_uses_specific_origin() {
        let cors = CorsMiddleware::new(CorsConfig {
            allowed_origins: vec!["*".into()],
            allow_credentials: true,
            ..CorsConfig::default()
        });

        let req = make_req("GET", Some("http://app.example.com"));
        let mut resp = crate::middleware::Response::with_status(200, "ok");
        cors.add_cors_headers(&req, &mut resp);

        // With credentials, should echo origin instead of "*".
        let origin_header = resp
            .headers
            .iter()
            .find(|(k, _)| k == "Access-Control-Allow-Origin");
        assert_eq!(origin_header.unwrap().1, "http://app.example.com");

        let creds_header = resp
            .headers
            .iter()
            .find(|(k, _)| k == "Access-Control-Allow-Credentials");
        assert_eq!(creds_header.unwrap().1, "true");
    }

    #[test]
    fn exposed_headers_added() {
        let cors = CorsMiddleware::new(CorsConfig {
            exposed_headers: vec!["X-Request-Id".into(), "X-Total-Count".into()],
            ..CorsConfig::default()
        });

        let req = make_req("GET", Some("http://example.com"));
        let mut resp = crate::middleware::Response::with_status(200, "ok");
        cors.add_cors_headers(&req, &mut resp);

        let exposed = resp
            .headers
            .iter()
            .find(|(k, _)| k == "Access-Control-Expose-Headers");
        assert_eq!(exposed.unwrap().1, "X-Request-Id, X-Total-Count");
    }

    #[test]
    fn vary_origin_always_added() {
        let cors = CorsMiddleware::new(CorsConfig::default());
        let req = make_req("GET", Some("http://example.com"));
        let mut resp = crate::middleware::Response::with_status(200, "ok");
        cors.add_cors_headers(&req, &mut resp);

        assert!(resp
            .headers
            .iter()
            .any(|(k, v)| k == "Vary" && v == "Origin"));
    }
}
