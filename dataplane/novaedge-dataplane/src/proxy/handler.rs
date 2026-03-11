//! HTTP proxy handler that routes requests to backend endpoints via hyper.

use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::atomic::{AtomicU32, Ordering};
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
use crate::upstream::circuit_breaker::{CircuitBreaker, CircuitBreakerConfig};
use crate::upstream::outlier::{OutlierConfig, OutlierDetector};
use crate::upstream::pool::ConnectionPool;
use crate::upstream::retry_budget::RetryBudgetTracker;

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
    /// Per-cluster outlier detectors for passive failure tracking.
    outlier_detectors: Arc<tokio::sync::RwLock<HashMap<String, Arc<OutlierDetector>>>>,
    /// Connection pool for concurrency limiting.
    connection_pool: Arc<ConnectionPool>,
    /// Slow start tracker for newly added/recovered endpoints.
    slow_start_tracker: Arc<lb::slow_start::SlowStartTracker>,
    /// Retry budget tracker to prevent retry storms.
    retry_budget: Arc<RetryBudgetTracker>,
    /// Global active request counter for load shedding.
    active_requests: Arc<AtomicU32>,
    /// Maximum concurrent active requests before shedding (0 = unlimited).
    max_active_requests: u32,
    /// Custom error pages for specific HTTP status codes.
    error_pages: Vec<ErrorPage>,
    /// Response header actions applied to all upstream responses.
    response_header_actions: Vec<HeaderAction>,
    /// When true, include X-Route debug headers in responses.
    debug_headers: bool,
}

impl ProxyHandler {
    /// Create a new proxy handler.
    pub fn new(
        router: Arc<std::sync::RwLock<Router>>,
        config: Arc<RuntimeConfig>,
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
            outlier_detectors: Arc::new(tokio::sync::RwLock::new(HashMap::new())),
            connection_pool,
            slow_start_tracker: Arc::new(lb::slow_start::SlowStartTracker::new()),
            retry_budget: Arc::new(RetryBudgetTracker::new()),
            active_requests: Arc::new(AtomicU32::new(0)),
            max_active_requests: 0,
            error_pages: Vec::new(),
            response_header_actions: Vec::new(),
            debug_headers: false,
        }
    }

    /// Get or create a circuit breaker for the given cluster, using per-cluster
    /// config from the ClusterState (failure threshold, success threshold,
    /// open duration).
    async fn get_circuit_breaker(&self, cluster_name: &str) -> Arc<CircuitBreaker> {
        // Fast path: check read lock.
        {
            let cbs = self.circuit_breakers.read().await;
            if let Some(cb) = cbs.get(cluster_name) {
                return cb.clone();
            }
        }
        // Slow path: acquire write lock and insert with cluster-specific config.
        let cb_config = self
            .config
            .get_cluster(cluster_name)
            .map(|c| CircuitBreakerConfig {
                failure_threshold: if c.cb_failure_threshold > 0 {
                    c.cb_failure_threshold
                } else {
                    5
                },
                success_threshold: if c.cb_success_threshold > 0 {
                    c.cb_success_threshold
                } else {
                    3
                },
                open_duration: if c.cb_open_duration_ms > 0 {
                    std::time::Duration::from_millis(c.cb_open_duration_ms)
                } else {
                    std::time::Duration::from_secs(30)
                },
                half_open_max_requests: if c.cb_half_open_max_requests > 0 {
                    c.cb_half_open_max_requests
                } else {
                    1
                },
            })
            .unwrap_or_default();

        let mut cbs = self.circuit_breakers.write().await;
        cbs.entry(cluster_name.to_string())
            .or_insert_with(|| Arc::new(CircuitBreaker::new(cb_config)))
            .clone()
    }

    /// Get or create a per-cluster outlier detector, using config from ClusterState.
    async fn get_outlier_detector(&self, cluster_name: &str) -> Arc<OutlierDetector> {
        // Fast path: check read lock.
        {
            let ods = self.outlier_detectors.read().await;
            if let Some(od) = ods.get(cluster_name) {
                return od.clone();
            }
        }
        // Slow path: create with cluster-specific config.
        let od_config = self
            .config
            .get_cluster(cluster_name)
            .map(|c| OutlierConfig {
                consecutive_errors: if c.outlier_consecutive_5xx > 0 {
                    c.outlier_consecutive_5xx
                } else {
                    5
                },
                ejection_duration: if c.outlier_ejection_duration_ms > 0 {
                    std::time::Duration::from_millis(c.outlier_ejection_duration_ms)
                } else {
                    std::time::Duration::from_secs(30)
                },
                max_ejection_percent: if c.outlier_max_ejection_pct > 0 {
                    c.outlier_max_ejection_pct as f64
                } else {
                    50.0
                },
                sr_min_hosts: if c.outlier_sr_min_hosts > 0 {
                    c.outlier_sr_min_hosts
                } else {
                    5
                },
                sr_min_requests: if c.outlier_sr_min_requests > 0 {
                    c.outlier_sr_min_requests
                } else {
                    100
                },
                sr_stdev_factor: if c.outlier_sr_stdev_factor > 0.0 {
                    c.outlier_sr_stdev_factor
                } else {
                    1.9
                },
            })
            .unwrap_or_default();

        let mut ods = self.outlier_detectors.write().await;
        ods.entry(cluster_name.to_string())
            .or_insert_with(|| Arc::new(OutlierDetector::new(od_config)))
            .clone()
    }

    /// Handle an incoming HTTP request: match route, select backend, forward.
    pub async fn handle_request(
        &self,
        req: hyper::Request<Incoming>,
        client_addr: SocketAddr,
        server_port: u16,
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
            let (route, backend_addr, upstream_uri) = match self
                .resolve_route_and_backend(
                    &method,
                    &path,
                    &host,
                    &headers,
                    query_string.as_deref(),
                    client_addr,
                )
                .await
            {
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
        let skip_body = matches!(method.as_str(), "GET" | "HEAD" | "DELETE" | "OPTIONS");

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
                server_port,
            )
            .await)
    }

    /// Core proxy logic shared by HTTP/1.1 (hyper) and HTTP/3 (h3) paths.
    ///
    /// Takes pre-parsed request fields and an already-collected body.
    /// Returns a response with `Full<Bytes>` body. WebSocket upgrades are
    /// NOT handled here — they require the original hyper request for the
    /// upgrade handshake.
    #[allow(clippy::too_many_arguments)]
    pub async fn handle_request_inner(
        &self,
        method: &str,
        path: &str,
        query_string: Option<&str>,
        host: &str,
        headers: &[(String, String)],
        body_bytes: Bytes,
        client_addr: SocketAddr,
        server_port: u16,
    ) -> hyper::Response<Full<Bytes>> {
        // Load shedding: reject requests if active count exceeds limit.
        if self.max_active_requests > 0 {
            let current = self.active_requests.fetch_add(1, Ordering::Relaxed);
            if current >= self.max_active_requests {
                self.active_requests.fetch_sub(1, Ordering::Relaxed);
                warn!(
                    active = current,
                    max = self.max_active_requests,
                    "Load shedding: rejecting request"
                );
                return hyper::Response::builder()
                    .status(503)
                    .header("Retry-After", "1")
                    .body(Full::new(Bytes::from("Service Overloaded")))
                    .unwrap();
            }
        } else {
            self.active_requests.fetch_add(1, Ordering::Relaxed);
        }

        let _load_shed_guard = LoadShedGuard {
            counter: &self.active_requests,
        };

        // Match route (with query param support).
        let matched_route = {
            let router = self.router.read().unwrap_or_else(|e| e.into_inner());
            router
                .match_request_with_query(host, path, method, headers, query_string)
                .cloned()
        };

        let route = match matched_route {
            Some(r) => r,
            None => {
                let default_backend = {
                    let router = self.router.read().unwrap_or_else(|e| e.into_inner());
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
                        backend_refs: vec![(backend, 1)],
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

        // Select a backend cluster using weighted selection.
        let selected_backend = match route.select_backend() {
            Some(b) => b.to_string(),
            None => {
                warn!(route = %route.name, "Route has no backend_refs");
                return hyper::Response::builder()
                    .status(502)
                    .body(Full::new(Bytes::from("No upstream cluster")))
                    .unwrap();
            }
        };

        debug!(
            route = %route.name,
            backend = %selected_backend,
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
        let cluster = match self.config.get_cluster(&selected_backend) {
            Some(c) => c,
            None => {
                warn!(
                    backend = %selected_backend,
                    "No cluster found for route '{}'", route.name
                );
                return hyper::Response::builder()
                    .status(502)
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
            .filter(|e| !e.draining)
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

        // Session affinity: extract sticky key and optionally override LB algorithm.
        let (sticky_cookie, effective_lb_algo) = match cluster.session_affinity_type.as_str() {
            "cookie" if !cluster.session_affinity_cookie.is_empty() => {
                let cookie = extract_cookie(headers, &cluster.session_affinity_cookie);
                // Cookie affinity forces sticky LB algorithm.
                (cookie, "sticky".to_string())
            }
            "header" if !cluster.session_affinity_header.is_empty() => {
                // Use header value as sticky key via source-hash.
                let header_val = headers
                    .iter()
                    .find(|(k, _)| k.eq_ignore_ascii_case(&cluster.session_affinity_header))
                    .map(|(_, v)| v.clone());
                (header_val, "source-hash".to_string())
            }
            "source_ip" => {
                // Source IP affinity uses source-hash (already hashes by src_ip).
                (None, "source-hash".to_string())
            }
            _ => (None, cluster.lb_algorithm.clone()),
        };

        // Apply slow start weight adjustment to backends.
        let backends: Vec<lb::Backend> = if cluster.slow_start_window_ms > 0 {
            let window = std::time::Duration::from_millis(cluster.slow_start_window_ms);
            let aggression = if cluster.slow_start_aggression > 0.0 {
                cluster.slow_start_aggression
            } else {
                1.0
            };
            backends
                .into_iter()
                .map(|mut b| {
                    let key = format!("{}:{}", b.addr, b.port);
                    self.slow_start_tracker.register(&key);
                    let multiplier = self
                        .slow_start_tracker
                        .weight_multiplier(&key, window, aggression);
                    b.weight = ((b.weight as f64) * multiplier).max(1.0) as u16;
                    b
                })
                .collect()
        } else {
            backends
        };

        // Set zone from config for zone-aware routing.
        let local_zone = self.config.local_zone().await;
        let ctx = lb::RequestContext {
            src_ip: client_addr.ip(),
            src_port: client_addr.port(),
            dst_port: 0,
            sticky_cookie,
            zone: if local_zone.is_empty() {
                None
            } else {
                Some(local_zone)
            },
        };

        let cb = self.get_circuit_breaker(&cluster.name).await;
        if !cb.allow_request() {
            debug!(
                cluster = %cluster.name,
                "Circuit breaker open, rejecting request"
            );
            return hyper::Response::builder()
                .status(503)
                .body(Full::new(Bytes::from("Circuit breaker open")))
                .unwrap();
        }

        // Apply priority-based failover: only consider healthy backends in
        // the lowest priority group.
        let priority_filtered: Vec<lb::Backend> = {
            let priority_indices = lb::filter_by_priority(&backends);
            if priority_indices.is_empty() {
                backends.clone()
            } else {
                priority_indices
                    .iter()
                    .map(|&i| backends[i].clone())
                    .collect()
            }
        };

        // Per-cluster outlier detection.
        let outlier_detector = self.get_outlier_detector(&cluster.name).await;
        let healthy_backends: Vec<&lb::Backend> = priority_filtered
            .iter()
            .filter(|b| {
                let addr = SocketAddr::new(b.addr, b.port);
                !outlier_detector.is_ejected(&addr)
            })
            .collect();

        // Panic mode threshold: if healthy percentage drops below threshold,
        // ignore health/outlier status and use all backends in the priority group.
        let total_in_priority = priority_filtered.len();
        let healthy_count = healthy_backends.len();
        let panic_threshold = cluster.panic_threshold_percent as usize;
        let in_panic_mode = panic_threshold > 0
            && total_in_priority > 0
            && (healthy_count * 100 / total_in_priority) < panic_threshold;

        let effective_backends = if in_panic_mode {
            warn!(
                cluster = %cluster.name,
                healthy = healthy_count,
                total = total_in_priority,
                threshold = panic_threshold,
                "Panic mode: healthy% below threshold, using all backends"
            );
            priority_filtered.clone()
        } else if healthy_backends.is_empty() {
            // All ejected — fall back to full backend pool as last resort.
            priority_filtered.clone()
        } else {
            healthy_backends.iter().map(|b| (*b).clone()).collect()
        };

        let balancer = self
            .config
            .get_or_create_lb(&cluster.name, &effective_lb_algo)
            .await;
        debug!(
            cluster = %cluster.name,
            algorithm = balancer.name(),
            "Using load balancer"
        );

        // Retry configuration from RouteState.
        let route_state = self.config.get_route(&route.name);
        let retry_max = route_state.as_ref().map(|r| r.retry_max).unwrap_or(0) as usize;
        let retry_on: Vec<String> = route_state
            .as_ref()
            .map(|r| r.retry_on.clone())
            .unwrap_or_default();
        let retry_backoff_base_ms = route_state
            .as_ref()
            .map(|r| {
                if r.retry_backoff_base_ms > 0 {
                    r.retry_backoff_base_ms
                } else {
                    25
                }
            })
            .unwrap_or(25);

        // Hedging configuration from RouteState.
        let hedge_delay_ms = route_state.as_ref().map(|r| r.hedge_delay_ms).unwrap_or(0);
        let _hedge_max_requests = route_state
            .as_ref()
            .map(|r| r.hedge_max_requests)
            .unwrap_or(0);

        let target_path = if let Some(ref rewrite) = route.rewrite_path {
            rewrite.clone()
        } else {
            path.to_string()
        };
        let query_suffix = query_string.map(|q| format!("?{q}")).unwrap_or_default();

        // Attempt loop: initial attempt + up to retry_max retries.
        let max_attempts = retry_max + 1;
        let mut final_status = hyper::StatusCode::BAD_GATEWAY;
        let mut final_resp_headers: Vec<(String, String)> = Vec::new();
        let mut final_body = Bytes::from("Bad Gateway");
        let mut final_backend_addr =
            SocketAddr::new(std::net::IpAddr::V4(std::net::Ipv4Addr::LOCALHOST), 0);
        let mut final_backend_idx: usize = 0;
        let mut got_response = false;

        for attempt in 0..max_attempts {
            if attempt == 0 {
                // Record the initial request for retry budget tracking.
                self.retry_budget.record_request(&cluster.name);
            } else {
                // Check retry budget before retrying.
                if !self.retry_budget.allow_retry(&cluster.name) {
                    debug!(
                        cluster = %cluster.name,
                        attempt,
                        "Retry budget exhausted, using last response"
                    );
                    break;
                }
                self.retry_budget.record_retry(&cluster.name);

                let backoff_ms = retry_backoff_base_ms.saturating_mul(1u64 << (attempt - 1).min(8));
                debug!(
                    attempt,
                    backoff_ms,
                    cluster = %cluster.name,
                    "Retrying upstream request"
                );
                tokio::time::sleep(std::time::Duration::from_millis(backoff_ms)).await;
            }

            // Select backend.
            let backend_idx = match balancer.select(&ctx, &effective_backends) {
                Some(idx) => {
                    let selected = &effective_backends[idx];
                    backends
                        .iter()
                        .position(|b| b.addr == selected.addr && b.port == selected.port)
                        .unwrap_or(idx)
                }
                None => {
                    if !got_response {
                        warn!(
                            cluster = %cluster.name,
                            "No healthy endpoints for cluster"
                        );
                        cb.record_failure();
                        return hyper::Response::builder()
                            .status(503)
                            .body(Full::new(Bytes::from("No healthy upstream")))
                            .unwrap();
                    }
                    break;
                }
            };

            let selected = &backends[backend_idx];
            let backend_addr = SocketAddr::new(selected.addr, selected.port);

            // Pool acquire.
            let _pool_guard = match self.connection_pool.acquire(backend_addr).await {
                Ok(guard) => guard,
                Err(e) => {
                    warn!(
                        backend = %backend_addr,
                        error = %e,
                        attempt,
                        "Connection pool limit reached"
                    );
                    self.connection_pool
                        .record_connection_failure(backend_addr)
                        .await;
                    if attempt < max_attempts - 1
                        && is_retryable_condition("connect-failure", &retry_on)
                    {
                        continue;
                    }
                    if !got_response {
                        return hyper::Response::builder()
                            .status(503)
                            .body(Full::new(Bytes::from("Connection pool exhausted")))
                            .unwrap();
                    }
                    break;
                }
            };

            let upstream_uri = format!("http://{backend_addr}{target_path}{query_suffix}");

            // Build upstream request.
            let mut upstream_req_builder =
                hyper::Request::builder().method(method).uri(&upstream_uri);

            for (key, value) in headers {
                if key.eq_ignore_ascii_case("host") {
                    continue;
                }
                if let Ok(name) = hyper::header::HeaderName::from_bytes(key.as_bytes()) {
                    if let Ok(val) = hyper::header::HeaderValue::from_str(value) {
                        upstream_req_builder = upstream_req_builder.header(name, val);
                    }
                }
            }

            upstream_req_builder = upstream_req_builder.header("Host", backend_addr.to_string());

            for (key, value) in &route.add_headers {
                if let Ok(name) = hyper::header::HeaderName::from_bytes(key.as_bytes()) {
                    if let Ok(val) = hyper::header::HeaderValue::from_str(value) {
                        upstream_req_builder = upstream_req_builder.header(name, val);
                    }
                }
            }

            let upstream_req = match upstream_req_builder.body(Full::new(body_bytes.clone())) {
                Ok(r) => r,
                Err(e) => {
                    warn!("Failed to build upstream request: {e}");
                    return hyper::Response::builder()
                        .status(500)
                        .body(Full::new(Bytes::from("Internal error")))
                        .unwrap();
                }
            };

            let upstream_start = std::time::Instant::now();

            // Hedging: on first attempt, if configured, race primary vs hedged request.
            let upstream_result =
                if attempt == 0 && hedge_delay_ms > 0 && effective_backends.len() > 1 {
                    let hedge_delay = std::time::Duration::from_millis(hedge_delay_ms);
                    // Build hedge request to a different backend.
                    let hedge_idx = (backend_idx + 1) % backends.len();
                    let hedge_backend = &backends[hedge_idx];
                    let hedge_addr = SocketAddr::new(hedge_backend.addr, hedge_backend.port);
                    let hedge_uri = format!("http://{hedge_addr}{target_path}{query_suffix}");
                    let mut hedge_builder =
                        hyper::Request::builder().method(method).uri(&hedge_uri);
                    for (key, value) in headers {
                        if key.eq_ignore_ascii_case("host") {
                            continue;
                        }
                        if let Ok(name) = hyper::header::HeaderName::from_bytes(key.as_bytes()) {
                            if let Ok(val) = hyper::header::HeaderValue::from_str(value) {
                                hedge_builder = hedge_builder.header(name, val);
                            }
                        }
                    }
                    hedge_builder = hedge_builder.header("Host", hedge_addr.to_string());
                    let hedge_req = hedge_builder.body(Full::new(body_bytes.clone())).ok();

                    let primary_fut = self.client.request(upstream_req);
                    let client = &self.client;
                    let pool = &self.connection_pool;

                    tokio::select! {
                        result = primary_fut => result,
                        result = async {
                            tokio::time::sleep(hedge_delay).await;
                            if let Some(req) = hedge_req {
                                // Acquire a pool guard for the hedge backend before sending.
                                let _hedge_pool_guard = match pool.acquire(hedge_addr).await {
                                    Ok(guard) => guard,
                                    Err(e) => {
                                        debug!(
                                            hedge_backend = %hedge_addr,
                                            error = %e,
                                            "Hedge request pool limit reached, waiting for primary"
                                        );
                                        return std::future::pending().await;
                                    }
                                };
                                debug!(hedge_backend = %hedge_addr, "Sending hedged request");
                                client.request(req).await
                            } else {
                                // Fallback: wait for primary forever (unreachable if hedge_req is Some).
                                std::future::pending().await
                            }
                        } => result,
                    }
                } else {
                    self.client.request(upstream_req).await
                };

            match upstream_result {
                Ok(upstream_resp) => {
                    let status = upstream_resp.status();
                    let upstream_latency = upstream_start.elapsed();

                    if status.is_server_error() {
                        cb.record_failure();
                        outlier_detector.record_failure(backend_addr);
                        balancer.report(backend_idx, upstream_latency, false);
                    } else {
                        cb.record_success();
                        outlier_detector.record_success(backend_addr);
                        balancer.report(backend_idx, upstream_latency, true);
                        self.connection_pool
                            .record_connection_success(backend_addr)
                            .await;
                    }

                    let resp_headers: Vec<(String, String)> = upstream_resp
                        .headers()
                        .iter()
                        .map(|(k, v)| (k.to_string(), v.to_str().unwrap_or("").to_string()))
                        .collect();

                    // Limit upstream response body to 100 MiB to prevent OOM.
                    const MAX_RESPONSE_BODY: usize = 100 * 1024 * 1024;
                    let limited =
                        http_body_util::Limited::new(upstream_resp.into_body(), MAX_RESPONSE_BODY);
                    let body = match limited.collect().await {
                        Ok(collected) => collected.to_bytes(),
                        Err(e) => {
                            warn!(backend = %backend_addr, error = %e, "Failed to read upstream response body");
                            cb.record_failure();
                            outlier_detector.record_failure(backend_addr);
                            balancer.report(backend_idx, upstream_start.elapsed(), false);
                            self.connection_pool.release(backend_addr).await;
                            if attempt < max_attempts - 1
                                && is_retryable_condition("reset", &retry_on)
                            {
                                continue;
                            }
                            if !got_response {
                                return hyper::Response::builder()
                                    .status(502)
                                    .body(Full::new(Bytes::from("Bad Gateway")))
                                    .unwrap();
                            }
                            break;
                        }
                    };

                    self.connection_pool.release(backend_addr).await;

                    final_status = status;
                    final_resp_headers = resp_headers;
                    final_body = body;
                    final_backend_addr = backend_addr;
                    final_backend_idx = backend_idx;
                    got_response = true;

                    // Check if retryable.
                    if status.is_server_error()
                        && attempt < max_attempts - 1
                        && is_retryable_status(status.as_u16(), &retry_on)
                    {
                        continue;
                    }

                    break;
                }
                Err(e) => {
                    let upstream_latency = upstream_start.elapsed();
                    cb.record_failure();
                    outlier_detector.record_failure(backend_addr);
                    balancer.report(backend_idx, upstream_latency, false);
                    self.connection_pool.release(backend_addr).await;
                    self.connection_pool
                        .record_connection_failure(backend_addr)
                        .await;

                    warn!(
                        backend = %backend_addr,
                        error = %e,
                        attempt,
                        "Failed to forward request to upstream"
                    );

                    final_status = hyper::StatusCode::BAD_GATEWAY;
                    final_resp_headers = Vec::new();
                    final_body = Bytes::from("Bad Gateway");
                    final_backend_addr = backend_addr;
                    final_backend_idx = backend_idx;
                    got_response = true;

                    if attempt < max_attempts - 1
                        && is_retryable_condition("connect-failure", &retry_on)
                    {
                        continue;
                    }

                    break;
                }
            }
        }

        // Build the final response from the last attempt result.
        let mut resp = hyper::Response::builder().status(final_status);
        if self.debug_headers {
            resp = resp.header("X-Route", route.name.as_str());
        }

        for (key, value) in &final_resp_headers {
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
                status: final_status.as_u16(),
                headers: final_resp_headers.clone(),
                body: final_body.to_vec(),
            };
            crate::middleware::pipeline::run_response_pipeline(
                &self.config,
                &route.middleware_refs,
                &mw_request,
                &mut mw_resp,
            );
            for (k, v) in &mw_resp.headers {
                if final_resp_headers.iter().any(|(rk, rv)| rk == k && rv == v) {
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
            let mut action_headers: Vec<(String, String)> = final_resp_headers.clone();
            apply_header_actions(&mut action_headers, &self.response_header_actions);
            for (k, v) in &action_headers {
                if final_resp_headers.iter().any(|(rk, rv)| rk == k && rv == v) {
                    continue;
                }
                if let Ok(name) = hyper::header::HeaderName::from_bytes(k.as_bytes()) {
                    if let Ok(val) = hyper::header::HeaderValue::from_str(v) {
                        resp = resp.header(name, val);
                    }
                }
            }
        }

        if server_port == 443 || server_port == 8443 {
            let alt_svc = super::http3::alt_svc_header(server_port);
            resp = resp.header("Alt-Svc", alt_svc);
        }

        // Set cookie for cookie-based session affinity if not already present.
        if cluster.session_affinity_type == "cookie"
            && !cluster.session_affinity_cookie.is_empty()
            && extract_cookie(headers, &cluster.session_affinity_cookie).is_none()
        {
            // Always set Secure: the client's ephemeral port cannot reliably indicate
            // whether the *listener* is TLS. In Kubernetes ingress the external connection
            // is virtually always TLS-terminated. Secure cookies are safe to send over
            // localhost HTTP in development environments.
            let cookie_value = format!(
                "{}={}; Path=/; HttpOnly; Secure; SameSite=Lax",
                cluster.session_affinity_cookie, final_backend_addr,
            );
            resp = resp.header("Set-Cookie", cookie_value);
        }

        if (final_status.is_client_error() || final_status.is_server_error())
            && !self.error_pages.is_empty()
        {
            if let Some(error_page) = find_error_page(final_status.as_u16(), &self.error_pages) {
                debug!(status = final_status.as_u16(), "Serving custom error page");
                resp = resp.header("Content-Type", error_page.content_type.as_str());
                return resp
                    .body(Full::new(Bytes::from(error_page.body.clone())))
                    .unwrap();
            }
        }

        // Suppress unused variable warnings for fields used in response building.
        let _ = final_backend_idx;

        // Request mirroring: fire-and-forget copy of request to mirror cluster.
        if let Some(ref mirror_cluster_name) =
            route_state.as_ref().and_then(|r| r.mirror_cluster.clone())
        {
            let mirror_percent = route_state
                .as_ref()
                .map(|r| r.mirror_percent)
                .unwrap_or(100);
            let should_mirror = if mirror_percent >= 100 {
                true
            } else {
                rand::random::<u32>() % 100 < mirror_percent
            };

            if should_mirror {
                if let Some(mirror_cluster) = self.config.get_cluster(mirror_cluster_name) {
                    let mirror_endpoints: Vec<_> = mirror_cluster
                        .endpoints
                        .iter()
                        .filter(|e| e.healthy && !e.draining)
                        .collect();

                    if let Some(ep) = mirror_endpoints.first() {
                        let mirror_addr = format!("{}:{}", ep.address, ep.port);
                        let mirror_uri = format!("http://{mirror_addr}{target_path}{query_suffix}");
                        let client = self.client.clone();
                        let method_clone = method.to_string();
                        let headers_clone: Vec<(String, String)> = headers.to_vec();
                        let body_clone = body_bytes;
                        let mirror_addr_clone = mirror_addr.clone();

                        // Fire-and-forget: don't await, don't care about result.
                        tokio::spawn(async move {
                            let mut req_builder = hyper::Request::builder()
                                .method(method_clone.as_str())
                                .uri(&mirror_uri);

                            for (key, value) in &headers_clone {
                                if key.eq_ignore_ascii_case("host") {
                                    continue;
                                }
                                if let Ok(name) =
                                    hyper::header::HeaderName::from_bytes(key.as_bytes())
                                {
                                    if let Ok(val) = hyper::header::HeaderValue::from_str(value) {
                                        req_builder = req_builder.header(name, val);
                                    }
                                }
                            }

                            req_builder = req_builder.header("Host", mirror_addr_clone);

                            if let Ok(req) = req_builder.body(Full::new(body_clone)) {
                                let _ = client.request(req).await;
                            }
                        });
                    }
                }
            }
        }

        resp.body(Full::new(final_body)).unwrap()
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
            let router = self.router.read().unwrap_or_else(|e| e.into_inner());
            router
                .match_request_with_query(host, path, method, headers, query_string)
                .cloned()
        };

        let route = match matched_route {
            Some(r) => r,
            None => {
                let default_backend = {
                    let router = self.router.read().unwrap_or_else(|e| e.into_inner());
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
                        backend_refs: vec![(backend, 1)],
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

        let selected_backend = match route.select_backend() {
            Some(b) => b.to_string(),
            None => {
                return Err(hyper::Response::builder()
                    .status(502)
                    .body(Full::new(Bytes::from("No upstream cluster")))
                    .unwrap());
            }
        };

        let cluster = match self.config.get_cluster(&selected_backend) {
            Some(c) => c,
            None => {
                return Err(hyper::Response::builder()
                    .status(502)
                    .body(Full::new(Bytes::from("No upstream cluster")))
                    .unwrap());
            }
        };

        let backends: Vec<lb::Backend> = cluster
            .endpoints
            .iter()
            .filter(|e| !e.draining)
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

        // Set zone from config for zone-aware routing.
        let local_zone = self.config.local_zone().await;
        let ctx = lb::RequestContext {
            src_ip: client_addr.ip(),
            src_port: client_addr.port(),
            dst_port: 0,
            sticky_cookie: None,
            zone: if local_zone.is_empty() {
                None
            } else {
                Some(local_zone)
            },
        };

        // Check circuit breaker before selecting backend.
        let cb = self.get_circuit_breaker(&cluster.name).await;
        if !cb.allow_request() {
            return Err(hyper::Response::builder()
                .status(503)
                .body(Full::new(Bytes::from("Circuit breaker open")))
                .unwrap());
        }

        // Apply priority-based failover.
        let priority_filtered: Vec<lb::Backend> = {
            let priority_indices = lb::filter_by_priority(&backends);
            if priority_indices.is_empty() {
                backends.clone()
            } else {
                priority_indices
                    .iter()
                    .map(|&i| backends[i].clone())
                    .collect()
            }
        };

        // Filter out outlier-ejected backends.
        let outlier_detector = self.get_outlier_detector(&cluster.name).await;
        let healthy_backends: Vec<lb::Backend> = priority_filtered
            .iter()
            .filter(|b| {
                let addr = SocketAddr::new(b.addr, b.port);
                !outlier_detector.is_ejected(&addr)
            })
            .cloned()
            .collect();

        // Panic mode threshold check.
        let total_in_priority = priority_filtered.len();
        let healthy_count = healthy_backends.len();
        let panic_threshold = cluster.panic_threshold_percent as usize;
        let in_panic_mode = panic_threshold > 0
            && total_in_priority > 0
            && (healthy_count * 100 / total_in_priority) < panic_threshold;

        let effective_backends = if in_panic_mode {
            warn!(
                cluster = %cluster.name,
                "Panic mode: healthy% below threshold, using all backends"
            );
            priority_filtered
        } else if healthy_backends.is_empty() {
            backends.clone()
        } else {
            healthy_backends
        };

        let balancer = self
            .config
            .get_or_create_lb(&cluster.name, &cluster.lb_algorithm)
            .await;

        let backend_idx = match balancer.select(&ctx, &effective_backends) {
            Some(idx) => idx,
            None => {
                return Err(hyper::Response::builder()
                    .status(503)
                    .body(Full::new(Bytes::from("No healthy upstream")))
                    .unwrap());
            }
        };

        let selected = &effective_backends[backend_idx];
        let backend_addr = SocketAddr::new(selected.addr, selected.port);

        let target_path = if let Some(ref rewrite) = route.rewrite_path {
            rewrite.clone()
        } else {
            path.to_string()
        };
        let query_suffix = query_string.map(|q| format!("?{q}")).unwrap_or_default();
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
            // Sanitize against header injection (CR/LF in key or value).
            if key.bytes().any(|b| b == b'\r' || b == b'\n')
                || value.bytes().any(|b| b == b'\r' || b == b'\n')
            {
                warn!(header = %key, "Dropping WebSocket header with CR/LF to prevent request smuggling");
                continue;
            }
            upgrade_request.push_str(&format!("{key}: {value}\r\n"));
        }
        // Add route-specific headers.
        for (key, value) in &route.add_headers {
            if key.bytes().any(|b| b == b'\r' || b == b'\n')
                || value.bytes().any(|b| b == b'\r' || b == b'\n')
            {
                warn!(header = %key, "Dropping route header with CR/LF to prevent request smuggling");
                continue;
            }
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
        let mut resp_builder = hyper::Response::builder().status(101);

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
        self.router
            .write()
            .unwrap_or_else(|e| e.into_inner())
            .set_routes(routes);
    }
}

/// Extract a named cookie value from request headers.
/// RAII guard that decrements the active request counter when dropped.
struct LoadShedGuard<'a> {
    counter: &'a AtomicU32,
}

impl<'a> Drop for LoadShedGuard<'a> {
    fn drop(&mut self) {
        self.counter.fetch_sub(1, Ordering::Relaxed);
    }
}

fn extract_cookie(headers: &[(String, String)], cookie_name: &str) -> Option<String> {
    for (key, value) in headers {
        if key.eq_ignore_ascii_case("cookie") {
            for part in value.split(';') {
                let part = part.trim();
                if let Some((name, val)) = part.split_once('=') {
                    if name.trim() == cookie_name {
                        return Some(val.trim().to_string());
                    }
                }
            }
        }
    }
    None
}

/// Check if an HTTP status code is retryable according to the retry_on conditions.
fn is_retryable_status(status: u16, retry_on: &[String]) -> bool {
    if retry_on.is_empty() {
        // Default: retry on any 5xx.
        return status >= 500;
    }
    for condition in retry_on {
        match condition.as_str() {
            "5xx" if (500..600).contains(&status) => return true,
            "gateway-error" if status == 502 || status == 503 || status == 504 => return true,
            "retriable-4xx" if status == 409 => return true,
            _ => {
                // Check for exact status code match (e.g. "503").
                if let Ok(code) = condition.parse::<u16>() {
                    if status == code {
                        return true;
                    }
                }
            }
        }
    }
    false
}

/// Check if a given error condition is in the retry_on list.
fn is_retryable_condition(condition: &str, retry_on: &[String]) -> bool {
    if retry_on.is_empty() {
        // Default: retry on connect failures and resets.
        return condition == "connect-failure" || condition == "reset";
    }
    retry_on.iter().any(|c| c == condition)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config;
    use crate::proxy::router::{HostMatch, PathMatch, Route};
    use crate::upstream::pool::{ConnectionPool, PoolConfig};

    fn test_route(name: &str, backend: &str) -> Route {
        Route {
            name: name.to_string(),
            hostnames: vec![HostMatch::Exact("api.test.com".into())],
            paths: vec![PathMatch::Prefix("/api/".into())],
            methods: Vec::new(),
            headers: Vec::new(),
            query_params: Vec::new(),
            backend_refs: vec![(backend.to_string(), 1)],
            priority: 10,
            rewrite_path: None,
            add_headers: HashMap::new(),
            middleware_refs: Vec::new(),
        }
    }

    fn test_handler(
        router: Arc<std::sync::RwLock<Router>>,
        cfg: Arc<RuntimeConfig>,
    ) -> ProxyHandler {
        ProxyHandler::new(
            router,
            cfg,
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
                draining: false,
                drain_start: None,
            }],
            lb_algorithm: "round-robin".into(),
            health_check_path: String::new(),
            session_affinity_type: String::new(),
            session_affinity_cookie: String::new(),
            session_affinity_header: String::new(),
            outlier_consecutive_5xx: 0,
            outlier_ejection_duration_ms: 0,
            outlier_max_ejection_pct: 0,
            outlier_interval_ms: 0,
            outlier_sr_min_hosts: 0,
            outlier_sr_min_requests: 0,
            outlier_sr_stdev_factor: 0.0,
            slow_start_window_ms: 0,
            slow_start_aggression: 1.0,
            protocol: String::new(),
            upstream_proxy_protocol_enabled: false,
            upstream_proxy_protocol_version: 0,
            tls_enabled: false,
            tls_insecure_skip_verify: false,
            tls_ca_pem: Vec::new(),
            tls_server_name: String::new(),
            cb_failure_threshold: 0,
            cb_success_threshold: 0,
            cb_open_duration_ms: 0,
            cb_half_open_max_requests: 0,
            pool_max_connections: 0,
            pool_max_idle: 0,
            pool_idle_timeout_ms: 0,
            pool_connect_timeout_ms: 0,
            panic_threshold_percent: 0,
            max_requests_per_connection: 0,
            request_queue_depth: 0,
            request_queue_timeout_ms: 0,
            subset_size: 0,
            remote_endpoints: Vec::new(),
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
                draining: false,
                drain_start: None,
            }],
            lb_algorithm: "round-robin".into(),
            health_check_path: String::new(),
            session_affinity_type: String::new(),
            session_affinity_cookie: String::new(),
            session_affinity_header: String::new(),
            outlier_consecutive_5xx: 0,
            outlier_ejection_duration_ms: 0,
            outlier_max_ejection_pct: 0,
            outlier_interval_ms: 0,
            outlier_sr_min_hosts: 0,
            outlier_sr_min_requests: 0,
            outlier_sr_stdev_factor: 0.0,
            slow_start_window_ms: 0,
            slow_start_aggression: 1.0,
            protocol: String::new(),
            upstream_proxy_protocol_enabled: false,
            upstream_proxy_protocol_version: 0,
            tls_enabled: false,
            tls_insecure_skip_verify: false,
            tls_ca_pem: Vec::new(),
            tls_server_name: String::new(),
            cb_failure_threshold: 0,
            cb_success_threshold: 0,
            cb_open_duration_ms: 0,
            cb_half_open_max_requests: 0,
            pool_max_connections: 0,
            pool_max_idle: 0,
            pool_idle_timeout_ms: 0,
            pool_connect_timeout_ms: 0,
            panic_threshold_percent: 0,
            max_requests_per_connection: 0,
            request_queue_depth: 0,
            request_queue_timeout_ms: 0,
            subset_size: 0,
            remote_endpoints: Vec::new(),
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
                        async move {
                            handler
                                .handle_request(req, client_addr, proxy_addr.port())
                                .await
                        }
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
