use std::collections::HashMap;

/// A route configuration for the HTTP proxy.
#[derive(Debug, Clone)]
pub struct Route {
    pub name: String,
    pub hostnames: Vec<HostMatch>,
    pub paths: Vec<PathMatch>,
    pub methods: Vec<String>,
    pub headers: Vec<HeaderMatch>,
    pub backend: String,
    pub priority: i32,
    pub rewrite_path: Option<String>,
    pub add_headers: HashMap<String, String>,
}

/// Hostname matching strategy.
#[derive(Debug, Clone)]
pub enum HostMatch {
    Exact(String),
    /// Wildcard match, e.g. `*.example.com`.
    Wildcard(String),
}

/// Path matching strategy.
#[derive(Debug, Clone)]
pub enum PathMatch {
    Exact(String),
    Prefix(String),
}

/// Header matching criterion.
#[derive(Debug, Clone)]
pub struct HeaderMatch {
    pub name: String,
    pub value: HeaderMatchValue,
}

/// How a header value should be matched.
#[derive(Debug, Clone)]
pub enum HeaderMatchValue {
    Exact(String),
    Present,
}

/// Route matching engine that evaluates HTTP requests against a prioritised
/// list of routes.
pub struct Router {
    routes: Vec<Route>,
    default_backend: Option<String>,
}

impl Router {
    pub fn new() -> Self {
        Self {
            routes: Vec::new(),
            default_backend: None,
        }
    }

    /// Replace the current route list (sorted by descending priority).
    pub fn set_routes(&mut self, routes: Vec<Route>) {
        let mut routes = routes;
        routes.sort_by(|a, b| b.priority.cmp(&a.priority));
        self.routes = routes;
    }

    /// Set the default backend (used when no route matches).
    pub fn set_default_backend(&mut self, backend: String) {
        self.default_backend = Some(backend);
    }

    /// Return the default backend name, if configured.
    pub fn default_backend(&self) -> Option<&str> {
        self.default_backend.as_deref()
    }

    /// Find the first route that matches the given request parameters.
    pub fn match_request(
        &self,
        host: &str,
        path: &str,
        method: &str,
        headers: &[(String, String)],
    ) -> Option<&Route> {
        for route in &self.routes {
            if self.matches_route(route, host, path, method, headers) {
                return Some(route);
            }
        }
        None
    }

    fn matches_route(
        &self,
        route: &Route,
        host: &str,
        path: &str,
        method: &str,
        headers: &[(String, String)],
    ) -> bool {
        // Check hostname.
        if !route.hostnames.is_empty() && !route.hostnames.iter().any(|h| match_host(h, host)) {
            return false;
        }
        // Check path.
        if !route.paths.is_empty() && !route.paths.iter().any(|p| match_path(p, path)) {
            return false;
        }
        // Check method.
        if !route.methods.is_empty()
            && !route.methods.iter().any(|m| m.eq_ignore_ascii_case(method))
        {
            return false;
        }
        // Check headers.
        for header_match in &route.headers {
            if !match_header(header_match, headers) {
                return false;
            }
        }
        true
    }
}

fn match_host(host_match: &HostMatch, host: &str) -> bool {
    match host_match {
        HostMatch::Exact(h) => h.eq_ignore_ascii_case(host),
        HostMatch::Wildcard(pattern) => {
            // *.example.com matches sub.example.com but not example.com
            if let Some(suffix) = pattern.strip_prefix("*.") {
                host.ends_with(suffix) && host.len() > suffix.len() + 1
            } else {
                pattern.eq_ignore_ascii_case(host)
            }
        }
    }
}

fn match_path(path_match: &PathMatch, path: &str) -> bool {
    match path_match {
        PathMatch::Exact(p) => p == path,
        PathMatch::Prefix(p) => path.starts_with(p.as_str()),
    }
}

fn match_header(header_match: &HeaderMatch, headers: &[(String, String)]) -> bool {
    headers.iter().any(|(name, value)| {
        name.eq_ignore_ascii_case(&header_match.name)
            && match &header_match.value {
                HeaderMatchValue::Exact(v) => v == value,
                HeaderMatchValue::Present => true,
            }
    })
}

impl Default for Router {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_route(name: &str, priority: i32) -> Route {
        Route {
            name: name.to_string(),
            hostnames: Vec::new(),
            paths: Vec::new(),
            methods: Vec::new(),
            headers: Vec::new(),
            backend: format!("{name}-backend"),
            priority,
            rewrite_path: None,
            add_headers: HashMap::new(),
        }
    }

    #[test]
    fn test_exact_hostname_match() {
        let mut router = Router::new();
        let mut route = make_route("host-route", 10);
        route.hostnames = vec![HostMatch::Exact("api.example.com".into())];
        router.set_routes(vec![route]);

        assert!(router
            .match_request("api.example.com", "/", "GET", &[])
            .is_some());
        assert!(router
            .match_request("other.example.com", "/", "GET", &[])
            .is_none());
    }

    #[test]
    fn test_exact_hostname_case_insensitive() {
        let mut router = Router::new();
        let mut route = make_route("host-route", 10);
        route.hostnames = vec![HostMatch::Exact("API.Example.COM".into())];
        router.set_routes(vec![route]);

        assert!(router
            .match_request("api.example.com", "/", "GET", &[])
            .is_some());
    }

    #[test]
    fn test_wildcard_hostname_match() {
        let mut router = Router::new();
        let mut route = make_route("wildcard-route", 10);
        route.hostnames = vec![HostMatch::Wildcard("*.example.com".into())];
        router.set_routes(vec![route]);

        assert!(router
            .match_request("sub.example.com", "/", "GET", &[])
            .is_some());
        assert!(router
            .match_request("deep.sub.example.com", "/", "GET", &[])
            .is_some());
        // Must not match the bare domain.
        assert!(router
            .match_request("example.com", "/", "GET", &[])
            .is_none());
    }

    #[test]
    fn test_exact_path_match() {
        let mut router = Router::new();
        let mut route = make_route("exact-path", 10);
        route.paths = vec![PathMatch::Exact("/health".into())];
        router.set_routes(vec![route]);

        assert!(router.match_request("", "/health", "GET", &[]).is_some());
        assert!(router.match_request("", "/healthz", "GET", &[]).is_none());
    }

    #[test]
    fn test_prefix_path_match() {
        let mut router = Router::new();
        let mut route = make_route("prefix-path", 10);
        route.paths = vec![PathMatch::Prefix("/api/".into())];
        router.set_routes(vec![route]);

        assert!(router
            .match_request("", "/api/v1/users", "GET", &[])
            .is_some());
        assert!(router.match_request("", "/other", "GET", &[]).is_none());
    }

    #[test]
    fn test_method_filtering() {
        let mut router = Router::new();
        let mut route = make_route("post-only", 10);
        route.methods = vec!["POST".into()];
        router.set_routes(vec![route]);

        assert!(router.match_request("", "/", "POST", &[]).is_some());
        assert!(router.match_request("", "/", "GET", &[]).is_none());
    }

    #[test]
    fn test_method_case_insensitive() {
        let mut router = Router::new();
        let mut route = make_route("post-only", 10);
        route.methods = vec!["POST".into()];
        router.set_routes(vec![route]);

        assert!(router.match_request("", "/", "post", &[]).is_some());
    }

    #[test]
    fn test_header_match_exact() {
        let mut router = Router::new();
        let mut route = make_route("header-route", 10);
        route.headers = vec![HeaderMatch {
            name: "X-Version".into(),
            value: HeaderMatchValue::Exact("v2".into()),
        }];
        router.set_routes(vec![route]);

        let headers = vec![("X-Version".into(), "v2".into())];
        assert!(router.match_request("", "/", "GET", &headers).is_some());

        let headers = vec![("X-Version".into(), "v1".into())];
        assert!(router.match_request("", "/", "GET", &headers).is_none());
    }

    #[test]
    fn test_header_match_present() {
        let mut router = Router::new();
        let mut route = make_route("header-present", 10);
        route.headers = vec![HeaderMatch {
            name: "Authorization".into(),
            value: HeaderMatchValue::Present,
        }];
        router.set_routes(vec![route]);

        let headers = vec![("Authorization".into(), "Bearer xyz".into())];
        assert!(router.match_request("", "/", "GET", &headers).is_some());

        assert!(router.match_request("", "/", "GET", &[]).is_none());
    }

    #[test]
    fn test_route_priority_ordering() {
        let mut router = Router::new();
        let mut low = make_route("low", 1);
        low.paths = vec![PathMatch::Prefix("/".into())];
        let mut high = make_route("high", 100);
        high.paths = vec![PathMatch::Prefix("/".into())];

        // Insert low first, high second -- router should sort by priority.
        router.set_routes(vec![low, high]);

        let matched = router.match_request("", "/anything", "GET", &[]).unwrap();
        assert_eq!(matched.name, "high");
    }

    #[test]
    fn test_no_match_returns_none() {
        let router = Router::new();
        assert!(router.match_request("", "/", "GET", &[]).is_none());
    }

    #[test]
    fn test_empty_criteria_matches_all() {
        let mut router = Router::new();
        let route = make_route("catch-all", 1);
        router.set_routes(vec![route]);

        // A route with no hostname/path/method/header constraints matches
        // everything.
        assert!(router
            .match_request("any.host", "/any/path", "DELETE", &[])
            .is_some());
    }

    #[test]
    fn test_default_backend() {
        let mut router = Router::new();
        assert!(router.default_backend().is_none());
        router.set_default_backend("fallback".into());
        assert_eq!(router.default_backend(), Some("fallback"));
    }
}
