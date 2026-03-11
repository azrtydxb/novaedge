use std::collections::HashMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;

/// A route configuration for the HTTP proxy.
#[derive(Clone)]
pub struct Route {
    pub name: String,
    pub hostnames: Vec<HostMatch>,
    pub paths: Vec<PathMatch>,
    pub methods: Vec<String>,
    pub headers: Vec<HeaderMatch>,
    pub query_params: Vec<QueryParamMatch>,
    /// Backend references with weights: `(cluster_name, weight)`.
    pub backend_refs: Vec<(String, u32)>,
    pub priority: i32,
    pub rewrite_path: Option<String>,
    pub add_headers: HashMap<String, String>,
    pub middleware_refs: Vec<String>,
    /// Per-route counter for deterministic weighted round-robin backend selection.
    pub rr_counter: Arc<AtomicU64>,
}

impl std::fmt::Debug for Route {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Route")
            .field("name", &self.name)
            .field("hostnames", &self.hostnames)
            .field("paths", &self.paths)
            .field("methods", &self.methods)
            .field("headers", &self.headers)
            .field("query_params", &self.query_params)
            .field("backend_refs", &self.backend_refs)
            .field("priority", &self.priority)
            .field("rewrite_path", &self.rewrite_path)
            .field("add_headers", &self.add_headers)
            .field("middleware_refs", &self.middleware_refs)
            .field("rr_counter", &self.rr_counter.load(Ordering::Relaxed))
            .finish()
    }
}

impl Route {
    /// Select a backend cluster name using deterministic weighted round-robin.
    ///
    /// If there is only one backend, returns it directly. If multiple, uses
    /// a per-route atomic counter to cycle through backends proportional to
    /// their weights. This is deterministic and thread-safe.
    pub fn select_backend(&self) -> Option<&str> {
        match self.backend_refs.len() {
            0 => None,
            1 => Some(&self.backend_refs[0].0),
            _ => {
                let total_weight: u32 = self.backend_refs.iter().map(|(_, w)| *w).sum();
                if total_weight == 0 {
                    return Some(&self.backend_refs[0].0);
                }
                let slot = self.rr_counter.fetch_add(1, Ordering::Relaxed) % total_weight as u64;
                let mut remaining = slot as u32;
                for (name, weight) in &self.backend_refs {
                    if remaining < *weight {
                        return Some(name);
                    }
                    remaining -= weight;
                }
                // Fallback (shouldn't reach here).
                Some(&self.backend_refs[0].0)
            }
        }
    }
}

/// Query parameter matching criterion.
#[derive(Debug, Clone)]
pub struct QueryParamMatch {
    pub name: String,
    pub value: String,
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
    Regex(regex::Regex),
}

/// Header matching criterion.
#[derive(Debug, Clone)]
pub struct HeaderMatch {
    pub name: String,
    pub value: HeaderMatchValue,
}

/// How a header value should be matched.
///
/// Variants are constructed by the gRPC config translation layer
/// when routes with header-based matching are pushed from the Go agent.
#[derive(Debug, Clone)]
#[allow(dead_code)]
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
        routes.sort_by_key(|r| std::cmp::Reverse(r.priority));
        self.routes = routes;
    }

    /// Set the default backend (used when no route matches).
    ///
    /// Called by the config translation layer when a gateway specifies a
    /// default backend. The handler falls back to this when no route matches.
    #[allow(dead_code)]
    pub fn set_default_backend(&mut self, backend: String) {
        self.default_backend = Some(backend);
    }

    /// Return the default backend name, if configured.
    pub fn default_backend(&self) -> Option<&str> {
        self.default_backend.as_deref()
    }

    /// Find the first route that matches the given request parameters.
    ///
    /// Convenience wrapper over [`match_request_with_query`] for callers
    /// that do not need query parameter matching.
    #[allow(dead_code)]
    pub fn match_request(
        &self,
        host: &str,
        path: &str,
        method: &str,
        headers: &[(String, String)],
    ) -> Option<&Route> {
        self.match_request_with_query(host, path, method, headers, None)
    }

    /// Find the first route that matches, including query parameters.
    pub fn match_request_with_query(
        &self,
        host: &str,
        path: &str,
        method: &str,
        headers: &[(String, String)],
        query: Option<&str>,
    ) -> Option<&Route> {
        self.routes
            .iter()
            .find(|route| self.matches_route(route, host, path, method, headers, query))
    }

    fn matches_route(
        &self,
        route: &Route,
        host: &str,
        path: &str,
        method: &str,
        headers: &[(String, String)],
        query: Option<&str>,
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
        // Check query parameters.
        if !route.query_params.is_empty() {
            let query_str = query.unwrap_or("");
            for qp in &route.query_params {
                if !match_query_param(&qp.name, &qp.value, query_str) {
                    return false;
                }
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
        PathMatch::Regex(re) => re.is_match(path),
    }
}

fn match_query_param(name: &str, value: &str, query_str: &str) -> bool {
    for pair in query_str.split('&') {
        if let Some((k, v)) = pair.split_once('=') {
            let decoded_k = percent_decode(k);
            let decoded_v = percent_decode(v);
            if decoded_k == name && decoded_v == value {
                return true;
            }
        }
    }
    false
}

/// Simple percent-decoding for query parameter keys/values.
fn percent_decode(input: &str) -> String {
    let mut result = Vec::with_capacity(input.len());
    let bytes = input.as_bytes();
    let mut i = 0;
    while i < bytes.len() {
        if bytes[i] == b'%' && i + 2 < bytes.len() {
            if let (Some(hi), Some(lo)) = (hex_val(bytes[i + 1]), hex_val(bytes[i + 2])) {
                result.push(hi << 4 | lo);
                i += 3;
                continue;
            }
        }
        // Also decode '+' as space (form encoding).
        if bytes[i] == b'+' {
            result.push(b' ');
        } else {
            result.push(bytes[i]);
        }
        i += 1;
    }
    String::from_utf8_lossy(&result).into_owned()
}

fn hex_val(b: u8) -> Option<u8> {
    match b {
        b'0'..=b'9' => Some(b - b'0'),
        b'a'..=b'f' => Some(b - b'a' + 10),
        b'A'..=b'F' => Some(b - b'A' + 10),
        _ => None,
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
            query_params: Vec::new(),
            backend_refs: vec![(format!("{name}-backend"), 1)],
            priority,
            rewrite_path: None,
            add_headers: HashMap::new(),
            middleware_refs: Vec::new(),
            rr_counter: Arc::new(AtomicU64::new(0)),
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

    #[test]
    fn test_regex_path_match() {
        let mut router = Router::new();
        let mut route = make_route("regex-path", 10);
        let re = regex::Regex::new(r"^/api/v\d+/users$").unwrap();
        route.paths = vec![PathMatch::Regex(re)];
        router.set_routes(vec![route]);

        assert!(router
            .match_request("", "/api/v1/users", "GET", &[])
            .is_some());
        assert!(router
            .match_request("", "/api/v2/users", "GET", &[])
            .is_some());
        assert!(router
            .match_request("", "/api/v1/posts", "GET", &[])
            .is_none());
        assert!(router.match_request("", "/other", "GET", &[]).is_none());
    }

    #[test]
    fn test_query_param_match() {
        let mut router = Router::new();
        let mut route = make_route("query-route", 10);
        route.paths = vec![PathMatch::Prefix("/search".into())];
        route.query_params = vec![QueryParamMatch {
            name: "type".into(),
            value: "users".into(),
        }];
        router.set_routes(vec![route]);

        assert!(router
            .match_request_with_query("", "/search", "GET", &[], Some("type=users"))
            .is_some());
        assert!(router
            .match_request_with_query("", "/search", "GET", &[], Some("type=posts"))
            .is_none());
        assert!(router
            .match_request_with_query("", "/search", "GET", &[], None)
            .is_none());
    }

    #[test]
    fn test_query_param_multiple() {
        let mut router = Router::new();
        let mut route = make_route("multi-query", 10);
        route.query_params = vec![
            QueryParamMatch {
                name: "a".into(),
                value: "1".into(),
            },
            QueryParamMatch {
                name: "b".into(),
                value: "2".into(),
            },
        ];
        router.set_routes(vec![route]);

        assert!(router
            .match_request_with_query("", "/", "GET", &[], Some("a=1&b=2"))
            .is_some());
        assert!(router
            .match_request_with_query("", "/", "GET", &[], Some("a=1&b=3"))
            .is_none());
    }
}
