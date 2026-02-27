use std::net::SocketAddr;
use std::sync::Arc;

use tokio::sync::RwLock;
use tracing::{debug, warn};

use super::router::{Route, Router};

/// HTTP request representation for proxy handling.
#[derive(Debug)]
pub struct HttpRequest {
    pub method: String,
    pub path: String,
    pub host: String,
    pub headers: Vec<(String, String)>,
    pub client_addr: SocketAddr,
}

/// HTTP response representation from upstream.
#[derive(Debug)]
pub struct HttpResponse {
    pub status: u16,
    pub headers: Vec<(String, String)>,
    pub body: Vec<u8>,
}

/// Proxy handler that routes HTTP requests to backends.
pub struct ProxyHandler {
    router: Arc<RwLock<Router>>,
}

impl ProxyHandler {
    pub fn new(router: Arc<RwLock<Router>>) -> Self {
        Self { router }
    }

    /// Handle an HTTP request by matching route and forwarding.
    pub async fn handle(&self, req: &HttpRequest) -> HttpResponse {
        let router = self.router.read().await;
        let headers_slice: Vec<(String, String)> = req.headers.clone();

        match router.match_request(&req.host, &req.path, &req.method, &headers_slice) {
            Some(route) => {
                debug!(
                    route = %route.name,
                    backend = %route.backend,
                    "Matched route for {} {}",
                    req.method, req.path
                );

                // Apply path rewrite if configured.
                let _target_path = if let Some(rewrite) = &route.rewrite_path {
                    rewrite.clone()
                } else {
                    req.path.clone()
                };

                // In a full implementation, we would forward to the backend
                // here using hyper client. For now, return a placeholder 502
                // since the actual HTTP client forwarding requires hyper
                // integration.
                HttpResponse {
                    status: 502,
                    headers: vec![("X-Route".into(), route.name.clone())],
                    body: b"Backend not yet connected".to_vec(),
                }
            }
            None => {
                warn!("No route matched for {} {}", req.method, req.path);
                HttpResponse {
                    status: 404,
                    headers: vec![],
                    body: b"Not Found".to_vec(),
                }
            }
        }
    }

    /// Update the router's routes.
    pub async fn update_routes(&self, routes: Vec<Route>) {
        self.router.write().await.set_routes(routes);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::proxy::router::{HostMatch, PathMatch};
    use std::collections::HashMap;

    fn test_route(name: &str, priority: i32) -> Route {
        Route {
            name: name.to_string(),
            hostnames: vec![HostMatch::Exact("api.test.com".into())],
            paths: vec![PathMatch::Prefix("/api/".into())],
            methods: Vec::new(),
            headers: Vec::new(),
            backend: format!("{name}-backend"),
            priority,
            rewrite_path: None,
            add_headers: HashMap::new(),
        }
    }

    fn test_request(host: &str, path: &str, method: &str) -> HttpRequest {
        HttpRequest {
            method: method.to_string(),
            path: path.to_string(),
            host: host.to_string(),
            headers: Vec::new(),
            client_addr: "127.0.0.1:9999".parse().unwrap(),
        }
    }

    #[tokio::test]
    async fn test_matched_route_returns_route_header() {
        let router = Arc::new(RwLock::new(Router::new()));
        router
            .write()
            .await
            .set_routes(vec![test_route("my-route", 10)]);

        let handler = ProxyHandler::new(router);
        let req = test_request("api.test.com", "/api/v1", "GET");
        let resp = handler.handle(&req).await;

        assert_eq!(resp.status, 502);
        assert!(resp
            .headers
            .iter()
            .any(|(k, v)| k == "X-Route" && v == "my-route"));
    }

    #[tokio::test]
    async fn test_unmatched_request_returns_404() {
        let router = Arc::new(RwLock::new(Router::new()));
        let handler = ProxyHandler::new(router);
        let req = test_request("unknown.host", "/nothing", "GET");
        let resp = handler.handle(&req).await;

        assert_eq!(resp.status, 404);
        assert_eq!(resp.body, b"Not Found");
    }

    #[tokio::test]
    async fn test_route_update() {
        let router = Arc::new(RwLock::new(Router::new()));
        let handler = ProxyHandler::new(router);

        // Initially no routes.
        let req = test_request("api.test.com", "/api/v1", "GET");
        let resp = handler.handle(&req).await;
        assert_eq!(resp.status, 404);

        // Add routes.
        handler
            .update_routes(vec![test_route("added-route", 10)])
            .await;
        let resp = handler.handle(&req).await;
        assert_eq!(resp.status, 502);
        assert!(resp
            .headers
            .iter()
            .any(|(k, v)| k == "X-Route" && v == "added-route"));
    }
}
