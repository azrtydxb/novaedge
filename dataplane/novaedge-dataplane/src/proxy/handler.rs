//! HTTP proxy handler that routes requests to backend endpoints via hyper.

use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::Arc;

use bytes::Bytes;
use http_body_util::{BodyExt, Full};
use hyper::body::Incoming;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tracing::{debug, info, warn};

use super::response::{apply_header_actions, find_error_page, ErrorPage, HeaderAction};
use super::router::Router;
use crate::config::RuntimeConfig;
use crate::lb;
use crate::upstream::circuit_breaker::CircuitBreaker;
use crate::upstream::outlier::OutlierDetector;
use crate::upstream::pool::ConnectionPool;

/// Proxy handler that routes incoming HTTP requests to upstream backends.
pub struct ProxyHandler {
    router: Arc<std::sync::RwLock<Router>>,
    config: Arc<RuntimeConfig>,
    client: hyper_util::client::legacy::Client<
        hyper_util::client::legacy::connect::HttpConnector,
        Full<Bytes>,
    >,
    /// Per-cluster circuit breakers.
    circuit_breakers: Arc<tokio::sync::RwLock<HashMap<String, Arc<CircuitBreaker>>>>,
    /// Outlier detector for passive failure tracking.
    outlier_detector: Arc<OutlierDetector>,
    /// Connection pool for concurrency limiting.
    connection_pool: Arc<ConnectionPool>,
    /// Custom error pages for specific HTTP status codes.
    error_pages: Vec<ErrorPage>,
    /// Response header actions applied to all upstream responses.
    response_header_actions: Vec<HeaderAction>,
}

impl ProxyHandler {
    /// Create a new proxy handler.
    pub fn new(
        router: Arc<std::sync::RwLock<Router>>,
        config: Arc<RuntimeConfig>,
        outlier_detector: Arc<OutlierDetector>,
        connection_pool: Arc<ConnectionPool>,
    ) -> Self {
        let client =
            hyper_util::client::legacy::Client::builder(hyper_util::rt::TokioExecutor::new())
                .build_http();

        Self {
            router,
            config,
            client,
            circuit_breakers: Arc::new(tokio::sync::RwLock::new(HashMap::new())),
            outlier_detector,
            connection_pool,
            error_pages: Vec::new(),
            response_header_actions: Vec::new(),
        }
    }

    /// Get or create a circuit breaker for the given cluster.
    async fn get_circuit_breaker(&self, cluster_name: &str) -> Arc<CircuitBreaker> {
        // Fast path: check read lock.
        {
            let cbs = self.circuit_breakers.read().await;
            if let Some(cb) = cbs.get(cluster_name) {
                return cb.clone();
            }
        }
        // Slow path: acquire write lock and insert.
        let mut cbs = self.circuit_breakers.write().await;
        cbs.entry(cluster_name.to_string())
            .or_insert_with(|| {
                Arc::new(CircuitBreaker::new(
                    crate::upstream::circuit_breaker::CircuitBreakerConfig::default(),
                ))
            })
            .clone()
    }

    /// Handle an incoming HTTP request: match route, select backend, forward.
    pub async fn handle_request(
        &self,
        req: hyper::Request<Incoming>,
        client_addr: SocketAddr,
    ) -> Result<hyper::Response<Full<Bytes>>, hyper::Error> {
        let method = req.method().to_string();
        let path = req.uri().path().to_string();
        let query_string = req.uri().query().map(String::from);
        let host = req
            .headers()
            .get("host")
            .and_then(|v| v.to_str().ok())
            .unwrap_or("")
            .to_string();

        let headers: Vec<(String, String)> = req
            .headers()
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_str().unwrap_or("").to_string()))
            .collect();

        // Detect WebSocket upgrade request.
        let is_websocket = headers
            .iter()
            .any(|(k, v)| k.eq_ignore_ascii_case("upgrade") && v.eq_ignore_ascii_case("websocket"));

        // For WebSocket, we need the original hyper request for upgrade.
        // Do early route/LB resolution so we can branch.
        if is_websocket {
            let (route, backend_addr, upstream_uri) = match self.resolve_route_and_backend(
                &method, &path, &host, &headers, query_string.as_deref(), client_addr,
            ).await {
                Ok(resolved) => resolved,
                Err(resp) => return Ok(resp),
            };
            return self
                .handle_websocket_upgrade(req, &headers, &route, backend_addr, &upstream_uri)
                .await;
        }

        // --- Normal HTTP forwarding ---

        // Check Content-Length against max body size (default 10 MiB).
        const MAX_BODY_SIZE: u64 = 10 * 1024 * 1024;

        // For methods that don't carry a body, skip collection entirely.
        let skip_body = matches!(
            method.as_str(),
            "GET" | "HEAD" | "DELETE" | "OPTIONS"
        );

        let body_bytes = if skip_body {
            drop(req);
            Bytes::new()
        } else {
            if let Some(cl) = headers
                .iter()
                .find(|(k, _)| k.eq_ignore_ascii_case("content-length"))
                .and_then(|(_, v)| v.parse::<u64>().ok())
            {
                if cl > MAX_BODY_SIZE {
                    return Ok(hyper::Response::builder()
                        .status(413)
                        .body(Full::new(Bytes::from("Payload Too Large")))
                        .unwrap());
                }
            }

            match http_body_util::Limited::new(req.into_body(), MAX_BODY_SIZE as usize)
                .collect()
                .await
            {
                Ok(collected) => collected.to_bytes(),
                Err(e) => {
                    warn!("Failed to read request body: {e}");
                    return Ok(hyper::Response::builder()
                        .status(413)
                        .body(Full::new(Bytes::from("Payload Too Large")))
                        .unwrap());
                }
            }
        };

        Ok(self
            .handle_request_inner(
                &method,
                &path,
                query_string.as_deref(),
                &host,
                &headers,
                body_bytes,
                client_addr,
            )
            .await)
    }

    /// Core proxy logic shared by HTTP/1.1 (hyper) and HTTP/3 (h3) paths.
    ///
    /// Takes pre-parsed request fields and an already-collected body.
    /// Returns a response with `Full<Bytes>` body. WebSocket upgrades are
    /// NOT handled here — they require the original hyper request for the
    /// upgrade handshake.
    pub async fn handle_request_inner(
        &self,
        method: &str,
        path: &str,
        query_string: Option<&str>,
        host: &str,
        headers: &[(String, String)],
        body_bytes: Bytes,
        client_addr: SocketAddr,
    ) -> hyper::Response<Full<Bytes>> {
        let ejected = self.outlier_detector.ejected_count();
        if ejected > 0 {
            debug!(ejected_backends = ejected, "Outlier detector: backends currently ejected");
        }

        // Match route (with query param support).
        let matched_route = {
            let router = self.router.read().unwrap();
            router
                .match_request_with_query(host, path, method, headers, query_string)
                .cloned()
        };

        let route = match matched_route {
            Some(r) => r,
            None => {
                let default_backend = {
                    let router = self.router.read().unwrap();
                    router.default_backend().map(String::from)
                };
                if let Some(backend) = default_backend {
                    debug!("No route matched for {method} {host}{path}, using default backend");
                    super::router::Route {
                        name: "__default__".to_string(),
                        hostnames: Vec::new(),
                        paths: Vec::new(),
                        methods: Vec::new(),
                        headers: Vec::new(),
                        query_params: Vec::new(),
                        backend,
                        priority: 0,
                        rewrite_path: None,
                        add_headers: HashMap::new(),
                        middleware_refs: Vec::new(),
                    }
                } else {
                    debug!("No route matched for {method} {host}{path}");
                    return hyper::Response::builder()
                        .status(404)
                        .body(Full::new(Bytes::from("Not Found")))
                        .unwrap();
                }
            }
        };

        debug!(
            route = %route.name,
            backend = %route.backend,
            "Matched route for {method} {path}"
        );

        // Build middleware request for pipeline evaluation.
        let mw_request = crate::middleware::Request {
            method: method.to_string(),
            path: path.to_string(),
            host: host.to_string(),
            headers: headers.to_vec(),
            body: None,
            client_ip: client_addr.ip().to_string(),
        };

        // Run request-phase middleware pipeline.
        if !route.middleware_refs.is_empty() {
            match crate::middleware::pipeline::run_pipeline(
                &self.config,
                &route.middleware_refs,
                mw_request.clone(),
            ) {
                crate::middleware::MiddlewareResult::Respond(resp) => {
                    let mut builder = hyper::Response::builder().status(resp.status);
                    for (k, v) in &resp.headers {
                        if let Ok(name) = hyper::header::HeaderName::from_bytes(k.as_bytes()) {
                            if let Ok(val) = hyper::header::HeaderValue::from_str(v) {
                                builder = builder.header(name, val);
                            }
                        }
                    }
                    return builder.body(Full::new(Bytes::from(resp.body))).unwrap();
                }
                crate::middleware::MiddlewareResult::Continue(_) => {
                    // Proceed with normal request handling.
                }
            }
        }

        // Look up cluster endpoints.
        let cluster = match self.config.get_cluster(&route.backend) {
            Some(c) => c,
            None => {
                warn!(
                    backend = %route.backend,
                    "No cluster found for route '{}'", route.name
                );
                return hyper::Response::builder()
                    .status(502)
                    .header("X-Route", route.name.as_str())
                    .body(Full::new(Bytes::from("No upstream cluster")))
                    .unwrap();
            }
        };

        if !cluster.health_check_path.is_empty() {
            debug!(
                cluster = %cluster.name,
                health_check_path = %cluster.health_check_path,
                "Cluster has health check path configured"
            );
        }

        let backends: Vec<lb::Backend> = cluster
            .endpoints
            .iter()
            .map(|e| lb::Backend {
                addr: e
                    .address
                    .parse()
                    .unwrap_or(std::net::IpAddr::V4(std::net::Ipv4Addr::LOCALHOST)),
                port: e.port as u16,
                weight: e.weight as u16,
                healthy: e.healthy,
                zone: e.zone.clone(),
                priority: e.priority,
            })
            .collect();

        let ctx = lb::RequestContext {
            src_ip: client_addr.ip(),
            src_port: client_addr.port(),
            dst_port: 0,
            sticky_cookie: None,
            zone: None,
        };

        let cb = self.get_circuit_breaker(&cluster.name).await;
        if !cb.allow_request() {
            debug!(
                cluster = %cluster.name,
                "Circuit breaker open, rejecting request"
            );
            return hyper::Response::builder()
                .status(503)
                .header("X-Route", route.name.as_str())
                .body(Full::new(Bytes::from("Circuit breaker open")))
                .unwrap();
        }

        let healthy_backends: Vec<&lb::Backend> = backends
            .iter()
            .filter(|b| {
                let addr = SocketAddr::new(b.addr, b.port);
                !self.outlier_detector.is_ejected(&addr)
            })
            .collect();

        let balancer = lb::new_load_balancer(&cluster.lb_algorithm);
        debug!(
            cluster = %cluster.name,
            algorithm = balancer.name(),
            "Using load balancer"
        );

        let (backend_idx, use_all) = if healthy_backends.is_empty() {
            match balancer.select(&ctx, &backends) {
                Some(idx) => (idx, true),
                None => {
                    warn!(
                        cluster = %cluster.name,
                        "No healthy endpoints for cluster"
                    );
                    cb.record_failure();
                    return hyper::Response::builder()
                        .status(503)
                        .header("X-Route", route.name.as_str())
                        .body(Full::new(Bytes::from("No healthy upstream")))
                        .unwrap();
                }
            }
        } else {
            let filtered: Vec<lb::Backend> =
                healthy_backends.iter().map(|b| (*b).clone()).collect();
            match balancer.select(&ctx, &filtered) {
                Some(idx) => {
                    let selected_backend = &filtered[idx];
                    let original_idx = backends
                        .iter()
                        .position(|b| {
                            b.addr == selected_backend.addr && b.port == selected_backend.port
                        })
                        .unwrap_or(idx);
                    (original_idx, false)
                }
                None => {
                    warn!(
                        cluster = %cluster.name,
                        "No healthy endpoints for cluster"
                    );
                    cb.record_failure();
                    return hyper::Response::builder()
                        .status(503)
                        .header("X-Route", route.name.as_str())
                        .body(Full::new(Bytes::from("No healthy upstream")))
                        .unwrap();
                }
            }
        };
        if use_all {
            warn!(
                cluster = %cluster.name,
                "Panic mode: all backends ejected, using unfiltered pool"
            );
        }

        let selected = &backends[backend_idx];
        let backend_addr = SocketAddr::new(selected.addr, selected.port);

        let active = self.connection_pool.active_count(&backend_addr);
        if active > 0 {
            debug!(
                backend = %backend_addr,
                active_connections = active,
                "Connection pool status before acquire"
            );
        }

        let pool_guard = match self.connection_pool.acquire(backend_addr).await {
            Ok(guard) => Some(guard),
            Err(e) => {
                warn!(
                    backend = %backend_addr,
                    error = %e,
                    "Connection pool limit reached"
                );
                return hyper::Response::builder()
                    .status(503)
                    .header("X-Route", route.name.as_str())
                    .body(Full::new(Bytes::from("Connection pool exhausted")))
                    .unwrap();
            }
        };

        let target_path = if let Some(ref rewrite) = route.rewrite_path {
            rewrite.clone()
        } else {
            path.to_string()
        };

        let query_suffix = query_string
            .map(|q| format!("?{q}"))
            .unwrap_or_default();
        let upstream_uri = format!("http://{backend_addr}{target_path}{query_suffix}");

        // Build upstream request.
        let mut upstream_req = hyper::Request::builder()
            .method(method)
            .uri(&upstream_uri);

        for (key, value) in headers {
            if key.eq_ignore_ascii_case("host") {
                continue;
            }
            if let Ok(name) = hyper::header::HeaderName::from_bytes(key.as_bytes()) {
                if let Ok(val) = hyper::header::HeaderValue::from_str(value) {
                    upstream_req = upstream_req.header(name, val);
                }
            }
        }

        upstream_req = upstream_req.header("Host", backend_addr.to_string());

        for (key, value) in &route.add_headers {
            if let Ok(name) = hyper::header::HeaderName::from_bytes(key.as_bytes()) {
                if let Ok(val) = hyper::header::HeaderValue::from_str(value) {
                    upstream_req = upstream_req.header(name, val);
                }
            }
        }

        let upstream_req = match upstream_req.body(Full::new(body_bytes)) {
            Ok(r) => r,
            Err(e) => {
                warn!("Failed to build upstream request: {e}");
                return hyper::Response::builder()
                    .status(500)
                    .body(Full::new(Bytes::from("Internal error")))
                    .unwrap();
            }
        };

        if let Some(ref guard) = pool_guard {
            let wait_time = guard.acquired_at.elapsed();
            if wait_time > std::time::Duration::from_millis(100) {
                debug!(
                    backend = %guard.addr,
                    wait_ms = wait_time.as_millis(),
                    "Slow pool acquisition"
                );
            }
        }

        let upstream_start = std::time::Instant::now();
        let result = match self.client.request(upstream_req).await {
            Ok(upstream_resp) => {
                let status = upstream_resp.status();
                let upstream_latency = upstream_start.elapsed();

                if status.is_server_error() {
                    cb.record_failure();
                    self.outlier_detector.record_failure(backend_addr);
                    balancer.report(backend_idx, upstream_latency, false);
                } else {
                    cb.record_success();
                    self.outlier_detector.record_success(backend_addr);
                    balancer.report(backend_idx, upstream_latency, true);
                }

                let resp_headers: Vec<(String, String)> = upstream_resp
                    .headers()
                    .iter()
                    .map(|(k, v)| (k.to_string(), v.to_str().unwrap_or("").to_string()))
                    .collect();

                let body = upstream_resp
                    .into_body()
                    .collect()
                    .await
                    .map(|c| c.to_bytes())
                    .unwrap_or_default();

                let mut resp = hyper::Response::builder()
                    .status(status)
                    .header("X-Route", route.name.as_str());

                for (key, value) in &resp_headers {
                    if key.eq_ignore_ascii_case("transfer-encoding")
                        || key.eq_ignore_ascii_case("connection")
                    {
                        continue;
                    }
                    if let Ok(name) = hyper::header::HeaderName::from_bytes(key.as_bytes()) {
                        if let Ok(val) = hyper::header::HeaderValue::from_str(value) {
                            resp = resp.header(name, val);
                        }
                    }
                }

                for (key, value) in &route.add_headers {
                    if let Ok(name) = hyper::header::HeaderName::from_bytes(key.as_bytes()) {
                        if let Ok(val) = hyper::header::HeaderValue::from_str(value) {
                            resp = resp.header(name, val);
                        }
                    }
                }

                if !route.middleware_refs.is_empty() {
                    let mut mw_resp = crate::middleware::Response {
                        status: status.as_u16(),
                        headers: resp_headers.clone(),
                        body: body.to_vec(),
                    };
                    crate::middleware::pipeline::run_response_pipeline(
                        &self.config,
                        &route.middleware_refs,
                        &mw_request,
                        &mut mw_resp,
                    );
                    for (k, v) in &mw_resp.headers {
                        if resp_headers.iter().any(|(rk, rv)| rk == k && rv == v) {
                            continue;
                        }
                        if let Ok(name) = hyper::header::HeaderName::from_bytes(k.as_bytes()) {
                            if let Ok(val) = hyper::header::HeaderValue::from_str(v) {
                                resp = resp.header(name, val);
                            }
                        }
                    }
                }

                if !self.response_header_actions.is_empty() {
                    let mut action_headers: Vec<(String, String)> = resp_headers.clone();
                    apply_header_actions(&mut action_headers, &self.response_header_actions);
                    for (k, v) in &action_headers {
                        if resp_headers.iter().any(|(rk, rv)| rk == k && rv == v) {
                            continue;
                        }
                        if let Ok(name) = hyper::header::HeaderName::from_bytes(k.as_bytes()) {
                            if let Ok(val) = hyper::header::HeaderValue::from_str(v) {
                                resp = resp.header(name, val);
                            }
                        }
                    }
                }

                if client_addr.port() == 443 || client_addr.port() == 8443 {
                    let alt_svc = super::http3::alt_svc_header(client_addr.port());
                    resp = resp.header("Alt-Svc", alt_svc);
                }

                if (status.is_client_error() || status.is_server_error())
                    && !self.error_pages.is_empty()
                {
                    if let Some(error_page) = find_error_page(status.as_u16(), &self.error_pages) {
                        debug!(
                            status = status.as_u16(),
                            "Serving custom error page"
                        );
                        resp = resp.header("Content-Type", error_page.content_type.as_str());
                        return resp
                            .body(Full::new(Bytes::from(error_page.body.clone())))
                            .unwrap();
                    }
                }

                resp.body(Full::new(body)).unwrap()
            }
            Err(e) => {
                let upstream_latency = upstream_start.elapsed();
                cb.record_failure();
                self.outlier_detector.record_failure(backend_addr);
                balancer.report(backend_idx, upstream_latency, false);

                warn!(
                    backend = %backend_addr,
                    error = %e,
                    "Failed to forward request to upstream"
                );
                hyper::Response::builder()
                    .status(502)
                    .header("X-Route", route.name.as_str())
                    .body(Full::new(Bytes::from("Bad Gateway")))
                    .unwrap()
            }
        };

        // Release the connection pool slot.
        self.connection_pool.release(backend_addr).await;

        result
    }

    /// Resolve route and backend for WebSocket upgrade handling.
    ///
    /// Returns (route, backend_addr, upstream_uri) or an error response.
    async fn resolve_route_and_backend(
        &self,
        method: &str,
        path: &str,
        host: &str,
        headers: &[(String, String)],
        query_string: Option<&str>,
        client_addr: SocketAddr,
    ) -> Result<(super::router::Route, SocketAddr, String), hyper::Response<Full<Bytes>>> {
        let matched_route = {
            let router = self.router.read().unwrap();
            router
                .match_request_with_query(host, path, method, headers, query_string)
                .cloned()
        };

        let route = match matched_route {
            Some(r) => r,
            None => {
                let default_backend = {
                    let router = self.router.read().unwrap();
                    router.default_backend().map(String::from)
                };
                if let Some(backend) = default_backend {
                    super::router::Route {
                        name: "__default__".to_string(),
                        hostnames: Vec::new(),
                        paths: Vec::new(),
                        methods: Vec::new(),
                        headers: Vec::new(),
                        query_params: Vec::new(),
                        backend,
                        priority: 0,
                        rewrite_path: None,
                        add_headers: HashMap::new(),
                        middleware_refs: Vec::new(),
                    }
                } else {
                    return Err(hyper::Response::builder()
                        .status(404)
                        .body(Full::new(Bytes::from("Not Found")))
                        .unwrap());
                }
            }
        };

        let cluster = match self.config.get_cluster(&route.backend) {
            Some(c) => c,
            None => {
                return Err(hyper::Response::builder()
                    .status(502)
                    .header("X-Route", route.name.as_str())
                    .body(Full::new(Bytes::from("No upstream cluster")))
                    .unwrap());
            }
        };

        let backends: Vec<lb::Backend> = cluster
            .endpoints
            .iter()
            .map(|e| lb::Backend {
                addr: e.address.parse().unwrap_or(std::net::IpAddr::V4(std::net::Ipv4Addr::LOCALHOST)),
                port: e.port as u16,
                weight: e.weight as u16,
                healthy: e.healthy,
                zone: e.zone.clone(),
                priority: e.priority,
            })
            .collect();

        let ctx = lb::RequestContext {
            src_ip: client_addr.ip(),
            src_port: client_addr.port(),
            dst_port: 0,
            sticky_cookie: None,
            zone: None,
        };

        let balancer = lb::new_load_balancer(&cluster.lb_algorithm);
        let backend_idx = match balancer.select(&ctx, &backends) {
            Some(idx) => idx,
            None => {
                return Err(hyper::Response::builder()
                    .status(503)
                    .header("X-Route", route.name.as_str())
                    .body(Full::new(Bytes::from("No healthy upstream")))
                    .unwrap());
            }
        };

        let selected = &backends[backend_idx];
        let backend_addr = SocketAddr::new(selected.addr, selected.port);

        let target_path = if let Some(ref rewrite) = route.rewrite_path {
            rewrite.clone()
        } else {
            path.to_string()
        };
        let query_suffix = query_string
            .map(|q| format!("?{q}"))
            .unwrap_or_default();
        let upstream_uri = format!("http://{backend_addr}{target_path}{query_suffix}");

        Ok((route, backend_addr, upstream_uri))
    }

    /// Handle a WebSocket upgrade by establishing a bidirectional TCP tunnel
    /// between the client and the upstream backend.
    async fn handle_websocket_upgrade(
        &self,
        req: hyper::Request<Incoming>,
        headers: &[(String, String)],
        route: &super::router::Route,
        backend_addr: SocketAddr,
        _upstream_uri: &str,
    ) -> Result<hyper::Response<Full<Bytes>>, hyper::Error> {
        info!(
            route = %route.name,
            backend = %backend_addr,
            "WebSocket upgrade detected, establishing tunnel"
        );

        // Connect to upstream via raw TCP.
        let upstream_stream = match tokio::net::TcpStream::connect(backend_addr).await {
            Ok(s) => s,
            Err(e) => {
                warn!(error = %e, "Failed to connect to WebSocket upstream");
                return Ok(hyper::Response::builder()
                    .status(502)
                    .body(Full::new(Bytes::from("WebSocket upstream connect failed")))
                    .unwrap());
            }
        };

        // Parse the URI path for the HTTP upgrade request.
        let path = req
            .uri()
            .path_and_query()
            .map(|pq| pq.to_string())
            .unwrap_or_else(|| "/".to_string());

        // Build the HTTP upgrade request to send to upstream.
        let mut upgrade_request = format!("GET {path} HTTP/1.1\r\nHost: {backend_addr}\r\n");
        for (key, value) in headers {
            if key.eq_ignore_ascii_case("host") {
                continue; // We already set Host above.
            }
            upgrade_request.push_str(&format!("{key}: {value}\r\n"));
        }
        // Add route-specific headers.
        for (key, value) in &route.add_headers {
            upgrade_request.push_str(&format!("{key}: {value}\r\n"));
        }
        upgrade_request.push_str("\r\n");

        let (mut upstream_read, mut upstream_write) = upstream_stream.into_split();

        // Send the upgrade request to upstream.
        if let Err(e) = upstream_write.write_all(upgrade_request.as_bytes()).await {
            warn!(error = %e, "Failed to send WebSocket upgrade to upstream");
            return Ok(hyper::Response::builder()
                .status(502)
                .body(Full::new(Bytes::from("WebSocket upgrade failed")))
                .unwrap());
        }

        // Read the upstream's 101 response headers.
        let mut response_buf = Vec::with_capacity(4096);
        let mut byte_buf = [0u8; 1];
        loop {
            match upstream_read.read(&mut byte_buf).await {
                Ok(0) => {
                    return Ok(hyper::Response::builder()
                        .status(502)
                        .body(Full::new(Bytes::from("WebSocket upstream closed")))
                        .unwrap());
                }
                Ok(_) => {
                    response_buf.push(byte_buf[0]);
                    if response_buf.len() >= 4
                        && &response_buf[response_buf.len() - 4..] == b"\r\n\r\n"
                    {
                        break;
                    }
                    if response_buf.len() > 8192 {
                        return Ok(hyper::Response::builder()
                            .status(502)
                            .body(Full::new(Bytes::from(
                                "WebSocket upgrade response too large",
                            )))
                            .unwrap());
                    }
                }
                Err(e) => {
                    warn!(error = %e, "Failed reading WebSocket upgrade response");
                    return Ok(hyper::Response::builder()
                        .status(502)
                        .body(Full::new(Bytes::from("WebSocket upgrade read failed")))
                        .unwrap());
                }
            }
        }

        let response_str = String::from_utf8_lossy(&response_buf);

        // Parse the status line (e.g. "HTTP/1.1 101 Switching Protocols").
        let status_line = response_str.lines().next().unwrap_or("");
        let status_code: u16 = status_line
            .split_whitespace()
            .nth(1)
            .and_then(|s| s.parse().ok())
            .unwrap_or(502);

        if status_code != 101 {
            return Ok(hyper::Response::builder()
                .status(status_code)
                .body(Full::new(Bytes::from(
                    "WebSocket upgrade rejected by upstream",
                )))
                .unwrap());
        }

        // Parse upstream response headers.
        let mut resp_builder = hyper::Response::builder()
            .status(101)
            .header("X-Route", route.name.as_str());

        for line in response_str.lines().skip(1) {
            let line = line.trim();
            if line.is_empty() {
                break;
            }
            if let Some((k, v)) = line.split_once(':') {
                let k = k.trim();
                let v = v.trim();
                if let Ok(name) = hyper::header::HeaderName::from_bytes(k.as_bytes()) {
                    if let Ok(val) = hyper::header::HeaderValue::from_str(v) {
                        resp_builder = resp_builder.header(name, val);
                    }
                }
            }
        }

        // Upgrade the client connection and start bidirectional copy.
        let on_upgrade = hyper::upgrade::on(req);

        // Spawn the bidirectional copy task.
        tokio::spawn(async move {
            match on_upgrade.await {
                Ok(upgraded) => {
                    let mut client_io = hyper_util::rt::TokioIo::new(upgraded);

                    // Reunite the upstream halves into a single TcpStream.
                    let upstream_tcp = upstream_read.reunite(upstream_write).unwrap();
                    let (mut upstream_r, mut upstream_w) = tokio::io::split(upstream_tcp);
                    let (mut client_r, mut client_w) = tokio::io::split(&mut client_io);

                    // Bidirectional copy using two tasks.
                    let c2u = tokio::io::copy(&mut client_r, &mut upstream_w);
                    let u2c = tokio::io::copy(&mut upstream_r, &mut client_w);

                    let _ = tokio::try_join!(c2u, u2c);
                }
                Err(e) => {
                    warn!(error = %e, "WebSocket upgrade failed on client side");
                }
            }
        });

        Ok(resp_builder.body(Full::new(Bytes::new())).unwrap())
    }

    /// Reset the circuit breaker for a named cluster.
    ///
    /// Called when a cluster is updated to clear any tripped state,
    /// giving the refreshed endpoints a clean slate.
    #[allow(dead_code)]
    pub async fn reset_circuit_breaker(&self, cluster_name: &str) {
        let cbs = self.circuit_breakers.read().await;
        if let Some(cb) = cbs.get(cluster_name) {
            debug!(cluster = %cluster_name, "Resetting circuit breaker for updated cluster");
            cb.reset();
        }
    }

    /// Update the router's routes.
    ///
    /// This is a public API intended for use by server.rs when route
    /// configuration is updated. Currently server.rs accesses the router
    /// directly, but this method provides a higher-level alternative.
    #[allow(dead_code)]
    pub async fn update_routes(&self, routes: Vec<super::router::Route>) {
        self.router.write().unwrap().set_routes(routes);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config;
    use crate::proxy::router::{HostMatch, PathMatch, Route};
    use crate::upstream::outlier::{OutlierConfig, OutlierDetector};
    use crate::upstream::pool::{ConnectionPool, PoolConfig};

    fn test_route(name: &str, backend: &str) -> Route {
        Route {
            name: name.to_string(),
            hostnames: vec![HostMatch::Exact("api.test.com".into())],
            paths: vec![PathMatch::Prefix("/api/".into())],
            methods: Vec::new(),
            headers: Vec::new(),
            query_params: Vec::new(),
            backend: backend.to_string(),
            priority: 10,
            rewrite_path: None,
            add_headers: HashMap::new(),
            middleware_refs: Vec::new(),
        }
    }

    fn test_handler(router: Arc<std::sync::RwLock<Router>>, cfg: Arc<RuntimeConfig>) -> ProxyHandler {
        ProxyHandler::new(
            router,
            cfg,
            Arc::new(OutlierDetector::new(OutlierConfig::default())),
            Arc::new(ConnectionPool::new(PoolConfig::default())),
        )
    }

    #[tokio::test]
    async fn test_handler_construction() {
        let router = Arc::new(std::sync::RwLock::new(Router::new()));
        let cfg = Arc::new(RuntimeConfig::new());
        let _handler = test_handler(router, cfg.clone());
        // Verify config is accessible.
        assert!(cfg.get_cluster("nonexistent").is_none());
    }

    #[tokio::test]
    async fn test_cluster_lookup() {
        let router = Arc::new(std::sync::RwLock::new(Router::new()));
        router
            .write()
            .unwrap()
            .set_routes(vec![test_route("my-route", "test-cluster")]);

        let cfg = Arc::new(RuntimeConfig::new());
        cfg.upsert_cluster(config::ClusterState {
            name: "test-cluster".into(),
            endpoints: vec![config::EndpointState {
                address: "10.0.0.1".into(),
                port: 8080,
                weight: 1,
                healthy: true,
                zone: None,
                priority: 0,
            }],
            lb_algorithm: "round-robin".into(),
            health_check_path: String::new(),
        });

        let _handler = test_handler(router, cfg.clone());
        let cluster = cfg.get_cluster("test-cluster").unwrap();
        assert_eq!(cluster.endpoints.len(), 1);
        assert_eq!(cluster.endpoints[0].port, 8080);
    }

    #[tokio::test]
    async fn test_forwarding_to_local_backend() {
        use hyper::service::service_fn;
        use hyper_util::rt::TokioIo;
        use tokio::net::TcpListener;

        // Start a simple HTTP backend.
        let backend_listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let backend_addr = backend_listener.local_addr().unwrap();

        tokio::spawn(async move {
            loop {
                let (stream, _) = backend_listener.accept().await.unwrap();
                let io = TokioIo::new(stream);
                tokio::spawn(async move {
                    let _ = hyper::server::conn::http1::Builder::new()
                        .serve_connection(
                            io,
                            service_fn(|_req| async {
                                Ok::<_, hyper::Error>(hyper::Response::new(Full::new(Bytes::from(
                                    "Hello Rust!",
                                ))))
                            }),
                        )
                        .await;
                });
            }
        });

        // Set up router and config with a route pointing to the backend.
        let router = Arc::new(std::sync::RwLock::new(Router::new()));
        router
            .write()
            .unwrap()
            .set_routes(vec![test_route("perf-route", "perf-cluster")]);

        let cfg = Arc::new(RuntimeConfig::new());
        cfg.upsert_cluster(config::ClusterState {
            name: "perf-cluster".into(),
            endpoints: vec![config::EndpointState {
                address: backend_addr.ip().to_string(),
                port: backend_addr.port() as u32,
                weight: 1,
                healthy: true,
                zone: None,
                priority: 0,
            }],
            lb_algorithm: "round-robin".into(),
            health_check_path: String::new(),
        });

        let handler = Arc::new(test_handler(router, cfg));

        // Start a proxy listener.
        let proxy_listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let proxy_addr = proxy_listener.local_addr().unwrap();

        let handler_clone = handler.clone();
        tokio::spawn(async move {
            let (stream, client_addr) = proxy_listener.accept().await.unwrap();
            let io = TokioIo::new(stream);
            let handler = handler_clone.clone();
            let _ = hyper::server::conn::http1::Builder::new()
                .serve_connection(
                    io,
                    service_fn(move |req| {
                        let handler = handler.clone();
                        async move { handler.handle_request(req, client_addr).await }
                    }),
                )
                .await;
        });

        // Give the listener a moment to start.
        tokio::time::sleep(std::time::Duration::from_millis(10)).await;

        // Send request through the proxy.
        let client =
            hyper_util::client::legacy::Client::builder(hyper_util::rt::TokioExecutor::new())
                .build_http();

        let req = hyper::Request::builder()
            .method("GET")
            .uri(format!("http://127.0.0.1:{}/api/v1", proxy_addr.port()))
            .header("host", "api.test.com")
            .body(Full::new(Bytes::new()))
            .unwrap();

        let resp = client.request(req).await.unwrap();
        assert_eq!(resp.status(), 200);

        let body = resp.into_body().collect().await.unwrap().to_bytes();
        assert_eq!(&body[..], b"Hello Rust!");
    }
}
