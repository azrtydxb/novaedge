//! HTTP response caching middleware.

use std::collections::HashMap;
use std::sync::RwLock;
use std::time::{Duration, Instant};

/// Cache configuration.
#[derive(Debug, Clone)]
pub struct CacheConfig {
    /// Maximum number of entries in the cache.
    pub max_entries: usize,
    /// Default TTL for cache entries when no explicit TTL is given.
    pub default_ttl: Duration,
    /// HTTP status codes that are eligible for caching.
    pub cacheable_statuses: Vec<u16>,
    /// HTTP methods that are eligible for caching.
    pub cacheable_methods: Vec<String>,
}

impl Default for CacheConfig {
    fn default() -> Self {
        Self {
            max_entries: 10_000,
            default_ttl: Duration::from_secs(300),
            cacheable_statuses: vec![200, 301, 404],
            cacheable_methods: vec!["GET".into(), "HEAD".into()],
        }
    }
}

/// A cached response entry.
struct CacheEntry {
    #[allow(dead_code)]
    response: super::Response,
    inserted: Instant,
    #[allow(dead_code)]
    ttl: Duration,
    #[allow(dead_code)]
    etag: Option<String>,
}

/// In-memory HTTP response cache.
pub struct ResponseCache {
    config: CacheConfig,
    entries: RwLock<HashMap<String, CacheEntry>>,
}

#[allow(dead_code)]
impl ResponseCache {
    /// Create a new response cache.
    pub fn new(config: CacheConfig) -> Self {
        Self {
            config,
            entries: RwLock::new(HashMap::new()),
        }
    }

    /// Compute a cache key from method, host, and path.
    pub fn cache_key(method: &str, host: &str, path: &str) -> String {
        format!("{method}:{host}{path}")
    }

    /// Look up a cached response by key, returning `None` if absent or expired.
    pub fn get(&self, key: &str) -> Option<super::Response> {
        let entries = self.entries.read().unwrap_or_else(|e| e.into_inner());
        entries.get(key).and_then(|entry| {
            if entry.inserted.elapsed() < entry.ttl {
                Some(entry.response.clone())
            } else {
                None
            }
        })
    }

    /// Store a response in the cache. If the response status is not cacheable,
    /// the entry is silently discarded.
    pub fn put(&self, key: String, response: super::Response, ttl: Option<Duration>) {
        if !self.is_cacheable(&response) {
            return;
        }

        let ttl = ttl.unwrap_or(self.config.default_ttl);
        let etag = response
            .headers
            .iter()
            .find(|(k, _)| k.eq_ignore_ascii_case("etag"))
            .map(|(_, v)| v.clone());

        let mut entries = self.entries.write().unwrap_or_else(|e| e.into_inner());

        // Evict oldest entry if at capacity.
        if entries.len() >= self.config.max_entries {
            if let Some(oldest_key) = entries
                .iter()
                .min_by_key(|(_, e)| e.inserted)
                .map(|(k, _)| k.clone())
            {
                entries.remove(&oldest_key);
            }
        }

        entries.insert(
            key,
            CacheEntry {
                response,
                inserted: Instant::now(),
                ttl,
                etag,
            },
        );
    }

    /// Check whether a response is eligible for caching based on its status.
    fn is_cacheable(&self, resp: &super::Response) -> bool {
        self.config.cacheable_statuses.contains(&resp.status)
    }

    /// Check whether a request method is cacheable.
    pub fn is_method_cacheable(&self, method: &str) -> bool {
        self.config
            .cacheable_methods
            .iter()
            .any(|m| m.eq_ignore_ascii_case(method))
    }

    /// Remove a specific key from the cache.
    pub fn invalidate(&self, key: &str) {
        self.entries
            .write()
            .unwrap_or_else(|e| e.into_inner())
            .remove(key);
    }

    /// Remove all entries from the cache.
    pub fn clear(&self) {
        self.entries
            .write()
            .unwrap_or_else(|e| e.into_inner())
            .clear();
    }

    /// Return the number of entries currently in the cache.
    pub fn len(&self) -> usize {
        self.entries.read().unwrap_or_else(|e| e.into_inner()).len()
    }

    /// Return whether the cache is empty.
    pub fn is_empty(&self) -> bool {
        self.entries
            .read()
            .unwrap_or_else(|e| e.into_inner())
            .is_empty()
    }

    /// Remove all expired entries from the cache.
    pub fn cleanup_expired(&self) {
        self.entries
            .write()
            .unwrap_or_else(|e| e.into_inner())
            .retain(|_, e| e.inserted.elapsed() < e.ttl);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn ok_response() -> crate::middleware::Response {
        crate::middleware::Response::with_status(200, "cached body")
    }

    fn response_with_status(status: u16) -> crate::middleware::Response {
        crate::middleware::Response::with_status(status, "body")
    }

    fn response_with_etag(etag: &str) -> crate::middleware::Response {
        let mut resp = ok_response();
        resp.headers.push(("ETag".into(), etag.into()));
        resp
    }

    #[test]
    fn cache_key_format() {
        assert_eq!(
            ResponseCache::cache_key("GET", "example.com", "/api/users"),
            "GET:example.com/api/users"
        );
    }

    #[test]
    fn put_and_get() {
        let cache = ResponseCache::new(CacheConfig::default());
        let key = "GET:example.com/test".to_string();
        cache.put(key.clone(), ok_response(), None);

        let cached = cache.get(&key);
        assert!(cached.is_some());
        assert_eq!(cached.unwrap().status, 200);
    }

    #[test]
    fn get_missing_key() {
        let cache = ResponseCache::new(CacheConfig::default());
        assert!(cache.get("nonexistent").is_none());
    }

    #[test]
    fn expired_entry_not_returned() {
        let cache = ResponseCache::new(CacheConfig::default());
        let key = "GET:example.com/expired".to_string();
        cache.put(key.clone(), ok_response(), Some(Duration::from_millis(1)));

        // Wait for expiry.
        std::thread::sleep(Duration::from_millis(10));
        assert!(cache.get(&key).is_none());
    }

    #[test]
    fn non_cacheable_status_not_stored() {
        let cache = ResponseCache::new(CacheConfig::default());
        let key = "GET:example.com/error".to_string();
        cache.put(key.clone(), response_with_status(500), None);

        assert!(cache.get(&key).is_none());
        assert_eq!(cache.len(), 0);
    }

    #[test]
    fn cacheable_301() {
        let cache = ResponseCache::new(CacheConfig::default());
        let key = "GET:example.com/redirect".to_string();
        cache.put(key.clone(), response_with_status(301), None);

        assert!(cache.get(&key).is_some());
    }

    #[test]
    fn cacheable_404() {
        let cache = ResponseCache::new(CacheConfig::default());
        let key = "GET:example.com/missing".to_string();
        cache.put(key.clone(), response_with_status(404), None);

        assert!(cache.get(&key).is_some());
    }

    #[test]
    fn invalidate_removes_entry() {
        let cache = ResponseCache::new(CacheConfig::default());
        let key = "GET:example.com/test".to_string();
        cache.put(key.clone(), ok_response(), None);
        assert_eq!(cache.len(), 1);

        cache.invalidate(&key);
        assert_eq!(cache.len(), 0);
        assert!(cache.get(&key).is_none());
    }

    #[test]
    fn clear_removes_all() {
        let cache = ResponseCache::new(CacheConfig::default());
        cache.put("k1".into(), ok_response(), None);
        cache.put("k2".into(), ok_response(), None);
        cache.put("k3".into(), ok_response(), None);
        assert_eq!(cache.len(), 3);

        cache.clear();
        assert_eq!(cache.len(), 0);
        assert!(cache.is_empty());
    }

    #[test]
    fn eviction_at_capacity() {
        let cache = ResponseCache::new(CacheConfig {
            max_entries: 2,
            ..CacheConfig::default()
        });

        cache.put("k1".into(), ok_response(), None);
        cache.put("k2".into(), ok_response(), None);
        assert_eq!(cache.len(), 2);

        // This should evict the oldest entry.
        cache.put("k3".into(), ok_response(), None);
        assert_eq!(cache.len(), 2);
    }

    #[test]
    fn cleanup_expired_entries() {
        let cache = ResponseCache::new(CacheConfig::default());
        cache.put(
            "short".into(),
            ok_response(),
            Some(Duration::from_millis(1)),
        );
        cache.put(
            "long".into(),
            ok_response(),
            Some(Duration::from_secs(3600)),
        );

        std::thread::sleep(Duration::from_millis(10));
        cache.cleanup_expired();

        assert_eq!(cache.len(), 1);
        assert!(cache.get("long").is_some());
        assert!(cache.get("short").is_none());
    }

    #[test]
    fn is_method_cacheable() {
        let cache = ResponseCache::new(CacheConfig::default());
        assert!(cache.is_method_cacheable("GET"));
        assert!(cache.is_method_cacheable("HEAD"));
        assert!(cache.is_method_cacheable("get")); // case-insensitive
        assert!(!cache.is_method_cacheable("POST"));
        assert!(!cache.is_method_cacheable("DELETE"));
    }

    #[test]
    fn etag_preserved() {
        let cache = ResponseCache::new(CacheConfig::default());
        let key = "GET:example.com/etag".to_string();
        cache.put(key.clone(), response_with_etag("\"abc123\""), None);

        let cached = cache.get(&key).unwrap();
        assert!(cached
            .headers
            .iter()
            .any(|(k, v)| k == "ETag" && v == "\"abc123\""));
    }

    #[test]
    fn default_ttl_used_when_none() {
        let cache = ResponseCache::new(CacheConfig {
            default_ttl: Duration::from_secs(60),
            ..CacheConfig::default()
        });
        let key = "GET:example.com/default-ttl".to_string();
        cache.put(key.clone(), ok_response(), None);

        // Should still be present (well within 60s).
        assert!(cache.get(&key).is_some());
    }

    #[test]
    fn is_empty_check() {
        let cache = ResponseCache::new(CacheConfig::default());
        assert!(cache.is_empty());
        cache.put("k1".into(), ok_response(), None);
        assert!(!cache.is_empty());
    }
}
