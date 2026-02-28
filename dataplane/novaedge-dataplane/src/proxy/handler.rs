//! HTTP proxy handler that routes requests to backend endpoints via hyper.

use std::net::SocketAddr;
use std::sync::Arc;

use bytes::Bytes;
use http_body_util::{BodyExt, Full};
use hyper::body::Incoming;
use tokio::sync::RwLock;
use tracing::{debug, warn};

use super::router::Router;
use crate::config::RuntimeConfig;
use crate::lb;

/// Proxy handler that routes incoming HTTP requests to upstream backends.
pub struct ProxyHandler {
    router: Arc<RwLock<Router>>,
    config: Arc<RuntimeConfig>,
    client: hyper_util::client::legacy::Client<
        hyper_util::client::legacy::connect::HttpConnector,
        Full<Bytes>,
    >,
}

impl ProxyHandler {
    /// Create a new proxy handler.
    pub fn new(router: Arc<RwLock<Router>>, config: Arc<RuntimeConfig>) -> Self {
        let client = hyper_util::client::legacy::Client::builder(
            hyper_util::rt::TokioExecutor::new(),
        )
        .build_http();

        Self {
            router,
            config,
            client,
        }
    }

    /// Handle an incoming HTTP request: match route, select backend, forward.
    pub async fn handle_request(
        &self,
        req: hyper::Request<Incoming>,
        client_addr: SocketAddr,
    ) -> Result<hyper::Response<Full<Bytes>>, hyper::Error> {
        let method = req.method().to_string();
        let path = req.uri().path().to_string();
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

        // Match route.
        let matched_route = {
            let router = self.router.read().await;
            router
                .match_request(&host, &path, &method, &headers)
                .cloned()
        };

        let route = match matched_route {
            Some(r) => r,
            None => {
                debug!("No route matched for {method} {host}{path}");
                return Ok(hyper::Response::builder()
                    .status(404)
                    .body(Full::new(Bytes::from("Not Found")))
                    .unwrap());
            }
        };

        debug!(
            route = %route.name,
            backend = %route.backend,
            "Matched route for {method} {path}"
        );

        // Look up cluster endpoints.
        let cluster = match self.config.get_cluster(&route.backend) {
            Some(c) => c,
            None => {
                warn!(
                    backend = %route.backend,
                    "No cluster found for route '{}'", route.name
                );
                return Ok(hyper::Response::builder()
                    .status(502)
                    .header("X-Route", route.name.as_str())
                    .body(Full::new(Bytes::from("No upstream cluster")))
                    .unwrap());
            }
        };

        // Convert endpoints to LB Backend type.
        let backends: Vec<lb::Backend> = cluster
            .endpoints
            .iter()
            .map(|e| lb::Backend {
                addr: e.address.parse().unwrap_or(std::net::IpAddr::V4(std::net::Ipv4Addr::LOCALHOST)),
                port: e.port as u16,
                weight: e.weight as u16,
                healthy: e.healthy,
                zone: None,
                priority: 0,
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
                warn!(
                    cluster = %cluster.name,
                    "No healthy endpoints for cluster"
                );
                return Ok(hyper::Response::builder()
                    .status(503)
                    .header("X-Route", route.name.as_str())
                    .body(Full::new(Bytes::from("No healthy upstream")))
                    .unwrap());
            }
        };

        let selected = &backends[backend_idx];
        let backend_addr = SocketAddr::new(selected.addr, selected.port);

        // Apply path rewrite if configured.
        let target_path = if let Some(ref rewrite) = route.rewrite_path {
            rewrite.clone()
        } else {
            path.clone()
        };

        // Build the upstream URI.
        let query = req
            .uri()
            .query()
            .map(|q| format!("?{q}"))
            .unwrap_or_default();
        let upstream_uri = format!("http://{backend_addr}{target_path}{query}");

        // Collect the incoming body.
        let body_bytes = match req.into_body().collect().await {
            Ok(collected) => collected.to_bytes(),
            Err(e) => {
                warn!("Failed to read request body: {e}");
                return Ok(hyper::Response::builder()
                    .status(400)
                    .body(Full::new(Bytes::from("Bad request body")))
                    .unwrap());
            }
        };

        // Build upstream request.
        let mut upstream_req = hyper::Request::builder()
            .method(method.as_str())
            .uri(&upstream_uri);

        // Forward original headers (except Host which we set to upstream).
        for (key, value) in &headers {
            if key.eq_ignore_ascii_case("host") {
                continue;
            }
            if let Ok(name) = hyper::header::HeaderName::from_bytes(key.as_bytes()) {
                if let Ok(val) = hyper::header::HeaderValue::from_str(value) {
                    upstream_req = upstream_req.header(name, val);
                }
            }
        }

        // Set the Host header to the upstream address.
        upstream_req = upstream_req.header("Host", backend_addr.to_string());

        // Add route-specific headers.
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
                return Ok(hyper::Response::builder()
                    .status(500)
                    .body(Full::new(Bytes::from("Internal error")))
                    .unwrap());
            }
        };

        // Send the request to the upstream.
        match self.client.request(upstream_req).await {
            Ok(upstream_resp) => {
                let status = upstream_resp.status();
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
                    // Skip hop-by-hop headers.
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

                // Apply response header actions.
                // (response::HeaderAction is available but requires converting to our types)

                Ok(resp.body(Full::new(body)).unwrap())
            }
            Err(e) => {
                warn!(
                    backend = %backend_addr,
                    error = %e,
                    "Failed to forward request to upstream"
                );
                Ok(hyper::Response::builder()
                    .status(502)
                    .header("X-Route", route.name.as_str())
                    .body(Full::new(Bytes::from(format!("Upstream error: {e}"))))
                    .unwrap())
            }
        }
    }

    /// Update the router's routes.
    pub async fn update_routes(&self, routes: Vec<super::router::Route>) {
        self.router.write().await.set_routes(routes);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config;
    use crate::proxy::router::{HostMatch, PathMatch, Route};
    use std::collections::HashMap;

    fn test_route(name: &str, backend: &str) -> Route {
        Route {
            name: name.to_string(),
            hostnames: vec![HostMatch::Exact("api.test.com".into())],
            paths: vec![PathMatch::Prefix("/api/".into())],
            methods: Vec::new(),
            headers: Vec::new(),
            backend: backend.to_string(),
            priority: 10,
            rewrite_path: None,
            add_headers: HashMap::new(),
        }
    }

    #[tokio::test]
    async fn test_handler_construction() {
        let router = Arc::new(RwLock::new(Router::new()));
        let cfg = Arc::new(RuntimeConfig::new());
        let _handler = ProxyHandler::new(router, cfg.clone());
        // Verify config is accessible.
        assert!(cfg.get_cluster("nonexistent").is_none());
    }

    #[tokio::test]
    async fn test_cluster_lookup() {
        let router = Arc::new(RwLock::new(Router::new()));
        router
            .write()
            .await
            .set_routes(vec![test_route("my-route", "test-cluster")]);

        let cfg = Arc::new(RuntimeConfig::new());
        cfg.upsert_cluster(config::ClusterState {
            name: "test-cluster".into(),
            endpoints: vec![config::EndpointState {
                address: "10.0.0.1".into(),
                port: 8080,
                weight: 1,
                healthy: true,
            }],
            lb_algorithm: "round-robin".into(),
            health_check_path: String::new(),
        });

        let _handler = ProxyHandler::new(router, cfg.clone());
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
                                Ok::<_, hyper::Error>(hyper::Response::new(Full::new(
                                    Bytes::from("Hello Rust!"),
                                )))
                            }),
                        )
                        .await;
                });
            }
        });

        // Set up router and config with a route pointing to the backend.
        let router = Arc::new(RwLock::new(Router::new()));
        router
            .write()
            .await
            .set_routes(vec![test_route("perf-route", "perf-cluster")]);

        let cfg = Arc::new(RuntimeConfig::new());
        cfg.upsert_cluster(config::ClusterState {
            name: "perf-cluster".into(),
            endpoints: vec![config::EndpointState {
                address: backend_addr.ip().to_string(),
                port: backend_addr.port() as u32,
                weight: 1,
                healthy: true,
            }],
            lb_algorithm: "round-robin".into(),
            health_check_path: String::new(),
        });

        let handler = Arc::new(ProxyHandler::new(router, cfg));

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
