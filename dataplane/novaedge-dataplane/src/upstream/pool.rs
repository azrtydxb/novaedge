//! Connection pool for upstream backends.

use std::collections::HashMap;
use std::net::SocketAddr;
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
}

impl Default for PoolConfig {
    fn default() -> Self {
        Self {
            max_connections_per_host: 100,
            max_idle_per_host: 10,
            idle_timeout: Duration::from_secs(90),
            connect_timeout: Duration::from_secs(5),
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
}

struct PooledConn {
    created: Instant,
    last_used: Instant,
}

/// RAII guard returned by [`ConnectionPool::acquire`].
///
/// Tracks that a connection slot has been acquired. The caller should
/// call [`ConnectionPool::release`] when the connection is returned to
/// the pool (or dropped).
pub struct PoolGuard {
    /// The target address this guard was acquired for.
    pub addr: SocketAddr,
    /// When the connection was acquired.
    pub acquired_at: Instant,
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
    pub async fn acquire(&self, addr: SocketAddr) -> Result<PoolGuard, anyhow::Error> {
        let semaphore = {
            let mut pools = self.pools.lock().await;
            let host_pool = pools.entry(addr).or_insert_with(|| HostPool {
                idle: Vec::new(),
                active: 0,
                semaphore: Arc::new(Semaphore::new(
                    self.config.max_connections_per_host as usize,
                )),
            });
            host_pool.semaphore.clone()
        };

        // Acquire a permit (blocks if at max connections).
        let permit = tokio::time::timeout(self.config.connect_timeout, semaphore.acquire_owned())
            .await
            .map_err(|_| anyhow::anyhow!("connection pool timeout for {addr}"))?
            .map_err(|e| anyhow::anyhow!("semaphore closed for {addr}: {e}"))?;

        // We don't hold the permit in the guard — we track it via active count.
        // Forget the permit so the semaphore slot is consumed until release().
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
                    });
                }
                // Expired idle connection — discard it.
            }
            host_pool.active += 1;
        }

        Ok(PoolGuard {
            addr,
            acquired_at: Instant::now(),
        })
    }

    /// Release a connection back to the pool.
    pub async fn release(&self, addr: SocketAddr) {
        let mut pools = self.pools.lock().await;
        if let Some(host_pool) = pools.get_mut(&addr) {
            host_pool.active = host_pool.active.saturating_sub(1);

            // Add to idle pool if below limit.
            if host_pool.idle.len() < self.config.max_idle_per_host as usize {
                host_pool.idle.push(PooledConn {
                    created: Instant::now(),
                    last_used: Instant::now(),
                });
            }

            // Return the semaphore permit.
            host_pool.semaphore.add_permits(1);
        }
    }

    /// Get the number of active connections for the given address.
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
            let removed = before - host_pool.idle.len();
            // Return permits for removed idle connections.
            if removed > 0 {
                host_pool.semaphore.add_permits(removed);
            }
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

        let guard = pool.acquire(addr).await.unwrap();
        assert_eq!(guard.addr, addr);

        // Active count should be 1.
        assert_eq!(pool.active_count(&addr), 1);

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

        let _g = pool.acquire(addr).await.unwrap();
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

        let _g = pool.acquire(addr).await.unwrap();
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
                let _g = pool.acquire(addr).await.unwrap();
                tokio::time::sleep(Duration::from_millis(5)).await;
                pool.release(addr).await;
            }));
        }
        for h in handles {
            h.await.unwrap();
        }
        assert_eq!(pool.active_count(&addr), 0);
    }
}
