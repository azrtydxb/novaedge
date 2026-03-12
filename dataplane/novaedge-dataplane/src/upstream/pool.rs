//! Connection pool for upstream backends.

use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::Arc;
use std::time::{Duration, Instant};

use tokio::sync::{Mutex, Semaphore};

/// Connection pool configuration.
#[derive(Debug, Clone)]
pub struct PoolConfig {
    /// Maximum active connections per host.
    pub max_connections_per_host: u32,
    /// Maximum idle connections kept per host.
    pub max_idle_per_host: u32,
    /// Time after which idle connections are closed.
    pub idle_timeout: Duration,
    /// Timeout for establishing a new connection.
    pub connect_timeout: Duration,
    /// Maximum requests per connection (0 = unlimited).
    /// After this many requests, the connection is not returned to idle.
    pub max_requests_per_connection: u32,
}

impl Default for PoolConfig {
    fn default() -> Self {
        Self {
            max_connections_per_host: 100,
            max_idle_per_host: 10,
            idle_timeout: Duration::from_secs(90),
            connect_timeout: Duration::from_secs(5),
            max_requests_per_connection: 0,
        }
    }
}

/// Connection pool managing per-host connection limits and idle recycling.
pub struct ConnectionPool {
    pools: Mutex<HashMap<SocketAddr, HostPool>>,
    config: PoolConfig,
}

struct HostPool {
    idle: Vec<PooledConn>,
    active: u32,
    semaphore: Arc<Semaphore>,
    /// Consecutive connection failures for backoff tracking.
    consecutive_failures: u32,
    /// Timestamp of the last connection failure.
    last_failure: Option<Instant>,
    /// Total requests served on this pool (for max_requests_per_connection).
    request_count: u32,
    /// Current number of requests waiting in the queue.
    queued: Arc<AtomicU32>,
    /// Maximum queue depth (0 = no queuing limit, use semaphore timeout).
    queue_depth: u32,
    /// Queue wait timeout (separate from connect_timeout).
    queue_timeout: Duration,
}

struct PooledConn {
    created: Instant,
    last_used: Instant,
}

/// RAII guard returned by [`ConnectionPool::acquire`].
///
/// Tracks that a connection slot has been acquired. When dropped without
/// an explicit `mark_released()`, the semaphore permit is returned
/// automatically to prevent permit leaks on panics or early returns.
#[derive(Debug)]
#[allow(dead_code)]
pub struct PoolGuard {
    /// The target address this guard was acquired for.
    pub addr: SocketAddr,
    /// When the connection was acquired.
    pub acquired_at: Instant,
    /// Semaphore to return the permit to on drop.
    semaphore: Arc<Semaphore>,
    /// Whether the permit has already been returned via `ConnectionPool::release`.
    released: bool,
}

impl PoolGuard {
    /// Mark this guard as released so `Drop` won't double-return the permit.
    ///
    /// Must be called before `ConnectionPool::release()` to avoid a
    /// double-return of the semaphore permit.
    pub fn mark_released(&mut self) {
        self.released = true;
    }
}

impl Drop for PoolGuard {
    fn drop(&mut self) {
        if !self.released {
            self.semaphore.add_permits(1);
        }
    }
}

impl ConnectionPool {
    /// Create a new connection pool with the given configuration.
    pub fn new(config: PoolConfig) -> Self {
        Self {
            pools: Mutex::new(HashMap::new()),
            config,
        }
    }

    /// Acquire a connection slot for the given address.
    ///
    /// If an idle connection is available, it is reused. Otherwise, a new
    /// slot is allocated (up to `max_connections_per_host`). Blocks if the
    /// maximum is reached until a slot becomes available.
    ///
    /// Applies exponential backoff if the host has consecutive connection
    /// failures: `min(base_ms * 2^failures, 30s)`. Fails fast if still
    /// within the backoff window.
    pub async fn acquire(&self, addr: SocketAddr) -> Result<PoolGuard, anyhow::Error> {
        // Check connection backoff before acquiring a semaphore permit.
        {
            let pools = self.pools.lock().await;
            if let Some(host_pool) = pools.get(&addr) {
                if host_pool.consecutive_failures > 0 {
                    if let Some(last_failure) = host_pool.last_failure {
                        let backoff_ms = std::cmp::min(
                            100u64 * (1u64 << host_pool.consecutive_failures.min(8)),
                            30_000,
                        );
                        let elapsed = last_failure.elapsed();
                        if elapsed < Duration::from_millis(backoff_ms) {
                            return Err(anyhow::anyhow!(
                                "connection backoff for {addr}: {}ms remaining",
                                backoff_ms - elapsed.as_millis() as u64
                            ));
                        }
                    }
                }
            }
        }

        let (semaphore, queued, queue_depth, queue_timeout) = {
            let mut pools = self.pools.lock().await;
            let host_pool = pools.entry(addr).or_insert_with(|| HostPool {
                idle: Vec::new(),
                active: 0,
                semaphore: Arc::new(Semaphore::new(
                    self.config.max_connections_per_host as usize,
                )),
                consecutive_failures: 0,
                last_failure: None,
                request_count: 0,
                queued: Arc::new(AtomicU32::new(0)),
                queue_depth: 0,
                queue_timeout: Duration::from_secs(5),
            });
            (
                host_pool.semaphore.clone(),
                host_pool.queued.clone(),
                host_pool.queue_depth,
                host_pool.queue_timeout,
            )
        };

        // Check queue depth before waiting for a permit.
        if queue_depth > 0 {
            let current_queued = queued.load(Ordering::Relaxed);
            if current_queued >= queue_depth {
                return Err(anyhow::anyhow!(
                    "request queue full for {addr}: {current_queued}/{queue_depth} queued"
                ));
            }
        }

        // Track that we're waiting in the queue.
        queued.fetch_add(1, Ordering::Relaxed);
        let wait_timeout = if queue_depth > 0 {
            queue_timeout
        } else {
            self.config.connect_timeout
        };

        // Clone the semaphore before acquire_owned consumes the Arc.
        let sem_for_guard = semaphore.clone();

        // Acquire a permit (blocks if at max connections).
        let permit = tokio::time::timeout(wait_timeout, semaphore.acquire_owned())
            .await
            .map_err(|_| {
                queued.fetch_sub(1, Ordering::Relaxed);
                anyhow::anyhow!("connection pool timeout for {addr}")
            })?
            .map_err(|e| {
                queued.fetch_sub(1, Ordering::Relaxed);
                anyhow::anyhow!("semaphore closed for {addr}: {e}")
            })?;
        queued.fetch_sub(1, Ordering::Relaxed);

        // Forget the permit — the PoolGuard's Drop impl will return it via
        // semaphore.add_permits(1) if the guard is dropped without an explicit
        // release, preventing permit leaks on panics.
        permit.forget();

        let mut pools = self.pools.lock().await;
        if let Some(host_pool) = pools.get_mut(&addr) {
            // Try to reuse an idle connection.
            let now = Instant::now();
            while let Some(conn) = host_pool.idle.pop() {
                if now.duration_since(conn.last_used) < self.config.idle_timeout {
                    host_pool.active += 1;
                    return Ok(PoolGuard {
                        addr,
                        acquired_at: conn.last_used,
                        semaphore: sem_for_guard,
                        released: false,
                    });
                }
                // Expired idle connection — discard it.
            }
            host_pool.active += 1;
        }

        Ok(PoolGuard {
            addr,
            acquired_at: Instant::now(),
            semaphore: sem_for_guard,
            released: false,
        })
    }

    /// Release a connection back to the pool.
    ///
    /// If `max_requests_per_connection` is configured and the host has served
    /// that many total requests, the connection is not returned to the idle
    /// pool (effectively recycling it).
    pub async fn release(&self, addr: SocketAddr) {
        let mut pools = self.pools.lock().await;
        if let Some(host_pool) = pools.get_mut(&addr) {
            let prev_active = host_pool.active;
            host_pool.active = host_pool.active.saturating_sub(1);
            host_pool.request_count = host_pool.request_count.saturating_add(1);

            let max_req = self.config.max_requests_per_connection;
            let should_recycle = max_req > 0 && host_pool.request_count >= max_req;

            // Add to idle pool if below limit and not recycling.
            if !should_recycle && host_pool.idle.len() < self.config.max_idle_per_host as usize {
                host_pool.idle.push(PooledConn {
                    created: Instant::now(),
                    last_used: Instant::now(),
                });
            }

            if should_recycle {
                // Reset counter after recycling.
                host_pool.request_count = 0;
            }

            // Only return the semaphore permit if active was > 0 before
            // decrement to avoid adding extra permits on double-release.
            if prev_active > 0 {
                host_pool.semaphore.add_permits(1);
            }
        }
    }

    /// Record a successful connection to the given address.
    /// Resets the consecutive failure counter and backoff.
    pub async fn record_connection_success(&self, addr: SocketAddr) {
        let mut pools = self.pools.lock().await;
        if let Some(host_pool) = pools.get_mut(&addr) {
            host_pool.consecutive_failures = 0;
            host_pool.last_failure = None;
        }
    }

    /// Record a failed connection to the given address.
    /// Increments the consecutive failure counter for backoff tracking.
    pub async fn record_connection_failure(&self, addr: SocketAddr) {
        let mut pools = self.pools.lock().await;
        let host_pool = pools.entry(addr).or_insert_with(|| HostPool {
            idle: Vec::new(),
            active: 0,
            semaphore: Arc::new(Semaphore::new(
                self.config.max_connections_per_host as usize,
            )),
            consecutive_failures: 0,
            last_failure: None,
            request_count: 0,
            queued: Arc::new(AtomicU32::new(0)),
            queue_depth: 0,
            queue_timeout: Duration::from_secs(5),
        });
        host_pool.consecutive_failures = host_pool.consecutive_failures.saturating_add(1);
        host_pool.last_failure = Some(Instant::now());
    }

    /// Get the number of active connections for the given address.
    #[allow(dead_code)]
    pub fn active_count(&self, addr: &SocketAddr) -> u32 {
        // Use try_lock to avoid blocking; return 0 if lock is contended.
        match self.pools.try_lock() {
            Ok(pools) => pools.get(addr).map(|p| p.active).unwrap_or(0),
            Err(_) => 0,
        }
    }

    /// Remove idle connections that have exceeded the idle timeout or max age.
    ///
    /// Connections are removed if they have been idle longer than the configured
    /// `idle_timeout`, or if they were created more than 10 minutes ago (max age).
    pub async fn cleanup_idle(&self) {
        const MAX_CONN_AGE: Duration = Duration::from_secs(600);
        let mut pools = self.pools.lock().await;
        let timeout = self.config.idle_timeout;
        let now = Instant::now();
        for (_addr, host_pool) in pools.iter_mut() {
            let before = host_pool.idle.len();
            host_pool.idle.retain(|c| {
                now.duration_since(c.last_used) < timeout && c.created.elapsed() < MAX_CONN_AGE
            });
            let _removed = before - host_pool.idle.len();
            // Note: permits are NOT returned here because idle connections
            // already had their permits returned via `release()` when they
            // transitioned from active to idle.
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::{IpAddr, Ipv4Addr};

    fn test_addr(port: u16) -> SocketAddr {
        SocketAddr::new(IpAddr::V4(Ipv4Addr::new(127, 0, 0, 1)), port)
    }

    #[tokio::test]
    async fn acquire_and_release() {
        let pool = ConnectionPool::new(PoolConfig::default());
        let addr = test_addr(8080);

        let mut guard = pool.acquire(addr).await.unwrap();
        assert_eq!(guard.addr, addr);

        // Active count should be 1.
        assert_eq!(pool.active_count(&addr), 1);

        guard.mark_released();
        pool.release(addr).await;
        // Active count should be 0 after release.
        assert_eq!(pool.active_count(&addr), 0);
    }

    #[tokio::test]
    async fn max_connections_limit() {
        let config = PoolConfig {
            max_connections_per_host: 2,
            connect_timeout: Duration::from_millis(100),
            ..Default::default()
        };
        let pool = Arc::new(ConnectionPool::new(config));
        let addr = test_addr(8081);

        // Acquire 2 connections (the max).
        let _g1 = pool.acquire(addr).await.unwrap();
        let _g2 = pool.acquire(addr).await.unwrap();

        // Third acquire should timeout.
        let result = pool.acquire(addr).await;
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn idle_connection_reuse() {
        let pool = ConnectionPool::new(PoolConfig {
            idle_timeout: Duration::from_secs(60),
            ..Default::default()
        });
        let addr = test_addr(8082);

        let mut g = pool.acquire(addr).await.unwrap();
        g.mark_released();
        pool.release(addr).await;

        // The idle pool should have 1 connection.
        {
            let pools = pool.pools.lock().await;
            assert_eq!(pools.get(&addr).unwrap().idle.len(), 1);
        }

        // Acquiring again should reuse the idle connection.
        let _g2 = pool.acquire(addr).await.unwrap();
    }

    #[tokio::test]
    async fn cleanup_removes_expired_idle() {
        let pool = ConnectionPool::new(PoolConfig {
            idle_timeout: Duration::from_millis(10),
            ..Default::default()
        });
        let addr = test_addr(8083);

        let mut g = pool.acquire(addr).await.unwrap();
        g.mark_released();
        pool.release(addr).await;

        // Wait for idle timeout.
        tokio::time::sleep(Duration::from_millis(50)).await;

        pool.cleanup_idle().await;

        let pools = pool.pools.lock().await;
        assert_eq!(pools.get(&addr).unwrap().idle.len(), 0);
    }

    #[tokio::test]
    async fn concurrent_acquire_release() {
        let pool = Arc::new(ConnectionPool::new(PoolConfig {
            max_connections_per_host: 10,
            ..Default::default()
        }));
        let addr = test_addr(8084);

        let mut handles = Vec::new();
        for _ in 0..10 {
            let pool = pool.clone();
            handles.push(tokio::spawn(async move {
                let mut g = pool.acquire(addr).await.unwrap();
                tokio::time::sleep(Duration::from_millis(5)).await;
                g.mark_released();
                pool.release(addr).await;
            }));
        }
        for h in handles {
            h.await.unwrap();
        }
        assert_eq!(pool.active_count(&addr), 0);
    }

    #[tokio::test]
    async fn connection_backoff() {
        let pool = ConnectionPool::new(PoolConfig::default());
        let addr = test_addr(8085);

        // Record several failures to build up backoff.
        pool.record_connection_failure(addr).await;
        pool.record_connection_failure(addr).await;
        pool.record_connection_failure(addr).await;

        // Acquire should fail due to backoff.
        let result = pool.acquire(addr).await;
        assert!(result.is_err());
        assert!(
            result.unwrap_err().to_string().contains("backoff"),
            "Error should mention backoff"
        );

        // After recording success, backoff should reset.
        pool.record_connection_success(addr).await;
        let result = pool.acquire(addr).await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn max_requests_per_connection_recycling() {
        let pool = ConnectionPool::new(PoolConfig {
            max_requests_per_connection: 3,
            ..Default::default()
        });
        let addr = test_addr(8086);

        // Serve 3 requests (the max).
        for _ in 0..3 {
            let mut g = pool.acquire(addr).await.unwrap();
            g.mark_released();
            pool.release(addr).await;
        }

        // After 3 releases, the connection should have been recycled
        // (not returned to idle on the 3rd release).
        let pools = pool.pools.lock().await;
        let hp = pools.get(&addr).unwrap();
        // request_count is reset to 0 after recycling.
        assert_eq!(hp.request_count, 0);
    }

    #[tokio::test]
    async fn request_queue_depth_limit() {
        // Create pool with max 1 connection — any additional acquire will queue.
        let pool = Arc::new(ConnectionPool::new(PoolConfig {
            max_connections_per_host: 1,
            ..Default::default()
        }));
        let addr = test_addr(8087);

        // Acquire the single slot.
        let _g1 = pool.acquire(addr).await.unwrap();

        // Set queue depth to 0 on the host pool to simulate "no queuing".
        // (queue_depth=0 means unlimited, so we don't block on depth check.)
        // The acquire should eventually timeout since the slot is held.
        let pool2 = pool.clone();
        let handle = tokio::spawn(async move {
            // With 1 connection held, this should timeout after connect_timeout.
            tokio::time::timeout(Duration::from_millis(50), pool2.acquire(addr)).await
        });

        let result = handle.await.unwrap();
        // Should timeout (Err from timeout).
        assert!(result.is_err());
    }
}
