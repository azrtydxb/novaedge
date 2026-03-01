//! Dynamic listener manager for HTTP and L4 gateways.
//!
//! Watches the [`RuntimeConfig`] for gateway changes and starts/stops
//! TCP listeners dynamically. HTTP gateways are served by [`ProxyHandler`],
//! while TCP gateways use bidirectional copy.

use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::Arc;

use bytes::Bytes;
use http_body_util::Full;
use hyper::service::service_fn;
use hyper_util::rt::TokioIo;
use tokio::net::TcpListener;
use tokio::sync::Mutex;
use tracing::{info, warn};

use crate::config::RuntimeConfig;
use crate::proxy::handler::ProxyHandler;

/// Handle for a running listener task.
struct ListenerHandle {
    port: u32,
    protocol: String,
    cancel: tokio::sync::watch::Sender<bool>,
    task: tokio::task::JoinHandle<()>,
}

/// Manages dynamic listeners that are started/stopped based on gateway config.
pub struct ListenerManager {
    config: Arc<RuntimeConfig>,
    proxy_handler: Arc<ProxyHandler>,
    listeners: Mutex<HashMap<String, ListenerHandle>>,
}

impl ListenerManager {
    /// Create a new listener manager.
    pub fn new(config: Arc<RuntimeConfig>, proxy_handler: Arc<ProxyHandler>) -> Self {
        Self {
            config,
            proxy_handler,
            listeners: Mutex::new(HashMap::new()),
        }
    }

    /// Run the listener reconciliation loop until shutdown.
    pub async fn run(&self, mut shutdown: tokio::sync::watch::Receiver<bool>) {
        let mut config_rx = self.config.subscribe();

        // Do an initial reconciliation.
        self.reconcile_listeners().await;

        loop {
            tokio::select! {
                result = config_rx.changed() => {
                    if result.is_err() {
                        break; // Channel closed
                    }
                    self.reconcile_listeners().await;
                }
                _ = shutdown.changed() => {
                    info!("Listener manager shutting down");
                    self.stop_all().await;
                    return;
                }
            }
        }
    }

    /// Reconcile running listeners against current gateway config.
    async fn reconcile_listeners(&self) {
        let snapshot = self.config.snapshot();
        let mut listeners = self.listeners.lock().await;

        // Start new listeners for gateways not yet running.
        for (name, gw) in &snapshot.gateways {
            if !listeners.contains_key(name) {
                let bind = if gw.bind_address.is_empty() {
                    "0.0.0.0"
                } else {
                    &gw.bind_address
                };

                let addr_str = format!("{bind}:{}", gw.port);
                let addr: SocketAddr = match addr_str.parse() {
                    Ok(a) => a,
                    Err(e) => {
                        warn!(gateway = %name, addr = %addr_str, "Invalid bind address: {e}");
                        continue;
                    }
                };

                match gw.protocol.as_str() {
                    "HTTP" | "HTTPS" | "HTTP3" => {
                        let handle =
                            self.start_http_listener(name, addr).await;
                        if let Some(h) = handle {
                            listeners.insert(name.clone(), h);
                        }
                    }
                    "TCP" | "UDP" => {
                        let handle =
                            self.start_l4_listener(name, addr).await;
                        if let Some(h) = handle {
                            listeners.insert(name.clone(), h);
                        }
                    }
                    other => {
                        warn!(gateway = %name, protocol = %other, "Unsupported protocol");
                    }
                }
            }
        }

        // Stop listeners for removed gateways.
        let to_remove: Vec<String> = listeners
            .keys()
            .filter(|name| !snapshot.gateways.contains_key(*name))
            .cloned()
            .collect();
        for name in to_remove {
            if let Some(handle) = listeners.remove(&name) {
                info!(
                    gateway = %name,
                    port = handle.port,
                    "Stopping listener for removed gateway"
                );
                let _ = handle.cancel.send(true);
                handle.task.abort();
            }
        }
    }

    /// Start an HTTP listener using hyper.
    async fn start_http_listener(
        &self,
        name: &str,
        addr: SocketAddr,
    ) -> Option<ListenerHandle> {
        let listener = match TcpListener::bind(addr).await {
            Ok(l) => l,
            Err(e) => {
                warn!(gateway = %name, addr = %addr, "Failed to bind HTTP listener: {e}");
                return None;
            }
        };

        let actual_addr = listener.local_addr().unwrap_or(addr);
        info!(gateway = %name, addr = %actual_addr, "HTTP listener started");

        let handler = self.proxy_handler.clone();
        let (cancel_tx, mut cancel_rx) = tokio::sync::watch::channel(false);
        let gw_name = name.to_string();

        let task = tokio::spawn(async move {
            loop {
                tokio::select! {
                    result = listener.accept() => {
                        match result {
                            Ok((stream, client_addr)) => {
                                let io = TokioIo::new(stream);
                                let handler = handler.clone();
                                tokio::spawn(async move {
                                    if let Err(e) = hyper::server::conn::http1::Builder::new()
                                        .serve_connection(
                                            io,
                                            service_fn(move |req| {
                                                let handler = handler.clone();
                                                async move {
                                                    handler.handle_request(req, client_addr).await
                                                }
                                            }),
                                        )
                                        .await
                                    {
                                        // Connection-level errors are normal (client disconnects, etc.)
                                        tracing::debug!(error = %e, "HTTP connection ended");
                                    }
                                });
                            }
                            Err(e) => {
                                warn!(error = %e, "Failed to accept connection");
                            }
                        }
                    }
                    _ = cancel_rx.changed() => {
                        info!(gateway = %gw_name, "HTTP listener shutting down");
                        return;
                    }
                }
            }
        });

        Some(ListenerHandle {
            port: actual_addr.port() as u32,
            protocol: "HTTP".into(),
            cancel: cancel_tx,
            task,
        })
    }

    /// Start an L4 TCP listener using bidirectional copy.
    async fn start_l4_listener(
        &self,
        name: &str,
        addr: SocketAddr,
    ) -> Option<ListenerHandle> {
        let listener = match TcpListener::bind(addr).await {
            Ok(l) => l,
            Err(e) => {
                warn!(gateway = %name, addr = %addr, "Failed to bind L4 listener: {e}");
                return None;
            }
        };

        let actual_addr = listener.local_addr().unwrap_or(addr);
        info!(gateway = %name, addr = %actual_addr, "L4/TCP listener started");

        let config = self.config.clone();
        let (cancel_tx, mut cancel_rx) = tokio::sync::watch::channel(false);
        let gw_name = name.to_string();

        let task = tokio::spawn(async move {
            loop {
                tokio::select! {
                    result = listener.accept() => {
                        match result {
                            Ok((mut client, _client_addr)) => {
                                // For L4, we need to select a backend from the config.
                                // Use the gateway name to find associated routes/clusters.
                                let snap = config.snapshot();
                                let backend_addr = snap.routes.values()
                                    .find(|r| r.gateway_ref == gw_name)
                                    .and_then(|r| snap.clusters.get(&r.backend_ref))
                                    .and_then(|c| c.endpoints.first())
                                    .and_then(|e| format!("{}:{}", e.address, e.port).parse::<SocketAddr>().ok());

                                if let Some(backend) = backend_addr {
                                    tokio::spawn(async move {
                                        match tokio::net::TcpStream::connect(backend).await {
                                            Ok(mut upstream) => {
                                                let _ = tokio::io::copy_bidirectional(&mut client, &mut upstream).await;
                                            }
                                            Err(e) => {
                                                tracing::debug!(backend = %backend, error = %e, "L4 backend connection failed");
                                            }
                                        }
                                    });
                                } else {
                                    tracing::debug!(gateway = %gw_name, "No backend for L4 connection");
                                    drop(client);
                                }
                            }
                            Err(e) => {
                                warn!(error = %e, "Failed to accept L4 connection");
                            }
                        }
                    }
                    _ = cancel_rx.changed() => {
                        info!(gateway = %gw_name, "L4 listener shutting down");
                        return;
                    }
                }
            }
        });

        Some(ListenerHandle {
            port: actual_addr.port() as u32,
            protocol: "TCP".into(),
            cancel: cancel_tx,
            task,
        })
    }

    /// Stop all running listeners.
    async fn stop_all(&self) {
        let mut listeners = self.listeners.lock().await;
        for (name, handle) in listeners.drain() {
            info!(
                gateway = %name,
                port = handle.port,
                "Stopping listener"
            );
            let _ = handle.cancel.send(true);
            handle.task.abort();
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config::{ClusterState, EndpointState, GatewayState, RouteState, RuntimeConfig};
    use crate::proxy::router::Router;
    use tokio::io::{AsyncReadExt, AsyncWriteExt};
    use tokio::sync::RwLock;

    fn make_handler(config: Arc<RuntimeConfig>) -> Arc<ProxyHandler> {
        let router = Arc::new(RwLock::new(Router::new()));
        Arc::new(ProxyHandler::new(router, config))
    }

    #[tokio::test]
    async fn test_http_listener_starts_and_responds() {
        let config = Arc::new(RuntimeConfig::new());

        // Add a gateway on a random port.
        config.upsert_gateway(GatewayState {
            name: "test-gw".into(),
            bind_address: "127.0.0.1".into(),
            port: 0, // Will be resolved by OS
            protocol: "HTTP".into(),
            tls: None,
            hostnames: vec![],
        });

        let handler = make_handler(config.clone());
        let mgr = ListenerManager::new(config.clone(), handler);

        // Start a listener manually.
        let handle = mgr
            .start_http_listener("test-gw", "127.0.0.1:0".parse().unwrap())
            .await
            .expect("listener should start");

        assert_eq!(handle.protocol, "HTTP");
        assert!(handle.port > 0);

        // Send an HTTP request — should get 404 (no routes configured).
        let client =
            hyper_util::client::legacy::Client::builder(hyper_util::rt::TokioExecutor::new())
                .build_http();

        let req = hyper::Request::builder()
            .uri(format!("http://127.0.0.1:{}/test", handle.port))
            .body(Full::new(Bytes::new()))
            .unwrap();

        let resp = client.request(req).await.unwrap();
        assert_eq!(resp.status(), 404);

        // Shutdown.
        let _ = handle.cancel.send(true);
        handle.task.abort();
    }

    #[tokio::test]
    async fn test_reconcile_starts_and_stops_listeners() {
        let config = Arc::new(RuntimeConfig::new());
        let handler = make_handler(config.clone());
        let mgr = ListenerManager::new(config.clone(), handler);

        // Initially no listeners.
        assert!(mgr.listeners.lock().await.is_empty());

        // Add a gateway.
        config.upsert_gateway(GatewayState {
            name: "gw-1".into(),
            bind_address: "127.0.0.1".into(),
            port: 0,
            protocol: "HTTP".into(),
            tls: None,
            hostnames: vec![],
        });

        mgr.reconcile_listeners().await;
        assert_eq!(mgr.listeners.lock().await.len(), 1);

        // Remove the gateway.
        config.delete_gateway("gw-1");
        mgr.reconcile_listeners().await;
        assert!(mgr.listeners.lock().await.is_empty());
    }

    #[tokio::test]
    async fn test_l4_listener_forwards_tcp() {
        // Start a backend echo server.
        let backend = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let backend_addr = backend.local_addr().unwrap();

        tokio::spawn(async move {
            let (mut stream, _) = backend.accept().await.unwrap();
            let mut buf = [0u8; 1024];
            let n = stream.read(&mut buf).await.unwrap();
            stream.write_all(&buf[..n]).await.unwrap();
        });

        let config = Arc::new(RuntimeConfig::new());

        // Configure gateway, route, and cluster.
        config.upsert_gateway(GatewayState {
            name: "tcp-gw".into(),
            bind_address: "127.0.0.1".into(),
            port: 0,
            protocol: "TCP".into(),
            tls: None,
            hostnames: vec![],
        });
        config.upsert_route(RouteState {
            name: "tcp-route".into(),
            gateway_ref: "tcp-gw".into(),
            hostnames: vec![],
            path_prefix: String::new(),
            path_exact: String::new(),
            methods: vec![],
            backend_ref: "tcp-cluster".into(),
            priority: 0,
            rewrite_path: None,
            add_headers: HashMap::new(),
            middleware_refs: Vec::new(),
        });
        config.upsert_cluster(ClusterState {
            name: "tcp-cluster".into(),
            endpoints: vec![EndpointState {
                address: backend_addr.ip().to_string(),
                port: backend_addr.port() as u32,
                weight: 1,
                healthy: true,
            }],
            lb_algorithm: "round-robin".into(),
            health_check_path: String::new(),
        });

        let handler = make_handler(config.clone());
        let mgr = ListenerManager::new(config.clone(), handler);

        let handle = mgr
            .start_l4_listener("tcp-gw", "127.0.0.1:0".parse().unwrap())
            .await
            .expect("L4 listener should start");

        // Give listener time to start accepting.
        tokio::time::sleep(std::time::Duration::from_millis(10)).await;

        // Connect through the L4 proxy.
        let mut client = tokio::net::TcpStream::connect(format!("127.0.0.1:{}", handle.port))
            .await
            .unwrap();
        client.write_all(b"hello l4").await.unwrap();

        let mut buf = [0u8; 64];
        let n = client.read(&mut buf).await.unwrap();
        assert_eq!(&buf[..n], b"hello l4");

        // Cleanup.
        let _ = handle.cancel.send(true);
        handle.task.abort();
    }
}
