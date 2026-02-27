//! Compression negotiation middleware.
//!
//! Determines whether response compression should be applied based on
//! content type, body size, and the client's `Accept-Encoding` header.
//! Actual byte-level compression would require a crate like `flate2`
//! or `async-compression`; this module handles the negotiation and
//! header management.

/// Compression configuration.
#[derive(Debug, Clone)]
pub struct CompressionConfig {
    /// Whether compression is enabled at all.
    pub enabled: bool,
    /// Minimum body size (bytes) before compression is applied.
    pub min_body_size: usize,
    /// MIME types eligible for compression.
    pub compressible_types: Vec<String>,
    /// Preferred compression algorithms in priority order.
    pub algorithms: Vec<CompressionAlgo>,
}

/// Supported compression algorithms.
#[derive(Debug, Clone, PartialEq)]
pub enum CompressionAlgo {
    Gzip,
    Brotli,
    Zstd,
}

impl Default for CompressionConfig {
    fn default() -> Self {
        Self {
            enabled: true,
            min_body_size: 256,
            compressible_types: vec![
                "text/html".into(),
                "text/css".into(),
                "text/plain".into(),
                "text/javascript".into(),
                "application/javascript".into(),
                "application/json".into(),
                "application/xml".into(),
                "image/svg+xml".into(),
            ],
            algorithms: vec![CompressionAlgo::Gzip],
        }
    }
}

/// Compression middleware.
pub struct Compression {
    config: CompressionConfig,
}

impl Compression {
    /// Create a new compression middleware.
    pub fn new(config: CompressionConfig) -> Self {
        Self { config }
    }

    /// Negotiate a compression algorithm based on the `Accept-Encoding` header.
    ///
    /// Returns `None` if compression is disabled or no mutually supported
    /// algorithm is found.
    pub fn negotiate(&self, accept_encoding: &str) -> Option<CompressionAlgo> {
        if !self.config.enabled {
            return None;
        }
        for algo in &self.config.algorithms {
            let name = match algo {
                CompressionAlgo::Gzip => "gzip",
                CompressionAlgo::Brotli => "br",
                CompressionAlgo::Zstd => "zstd",
            };
            if accept_encoding.contains(name) {
                return Some(algo.clone());
            }
        }
        None
    }

    /// Determine whether compression should be applied based on content type
    /// and body size.
    pub fn should_compress(&self, content_type: &str, body_size: usize) -> bool {
        if !self.config.enabled || body_size < self.config.min_body_size {
            return false;
        }
        self.config
            .compressible_types
            .iter()
            .any(|t| content_type.starts_with(t.as_str()))
    }

    /// Add `Content-Encoding` and `Vary` headers to indicate compression.
    pub fn apply_headers(&self, algo: &CompressionAlgo, resp: &mut super::Response) {
        let encoding = match algo {
            CompressionAlgo::Gzip => "gzip",
            CompressionAlgo::Brotli => "br",
            CompressionAlgo::Zstd => "zstd",
        };
        resp.headers
            .push(("Content-Encoding".into(), encoding.into()));
        resp.headers.push(("Vary".into(), "Accept-Encoding".into()));
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn negotiate_gzip() {
        let c = Compression::new(CompressionConfig::default());
        assert_eq!(c.negotiate("gzip, deflate"), Some(CompressionAlgo::Gzip));
    }

    #[test]
    fn negotiate_brotli() {
        let c = Compression::new(CompressionConfig {
            algorithms: vec![CompressionAlgo::Brotli, CompressionAlgo::Gzip],
            ..CompressionConfig::default()
        });
        assert_eq!(c.negotiate("br, gzip"), Some(CompressionAlgo::Brotli));
    }

    #[test]
    fn negotiate_zstd() {
        let c = Compression::new(CompressionConfig {
            algorithms: vec![CompressionAlgo::Zstd],
            ..CompressionConfig::default()
        });
        assert_eq!(c.negotiate("zstd"), Some(CompressionAlgo::Zstd));
    }

    #[test]
    fn negotiate_no_match() {
        let c = Compression::new(CompressionConfig::default());
        assert_eq!(c.negotiate("deflate"), None);
    }

    #[test]
    fn negotiate_disabled() {
        let c = Compression::new(CompressionConfig {
            enabled: false,
            ..CompressionConfig::default()
        });
        assert_eq!(c.negotiate("gzip"), None);
    }

    #[test]
    fn negotiate_priority_order() {
        let c = Compression::new(CompressionConfig {
            algorithms: vec![
                CompressionAlgo::Brotli,
                CompressionAlgo::Zstd,
                CompressionAlgo::Gzip,
            ],
            ..CompressionConfig::default()
        });
        // Client supports all three -- should pick the first in our list.
        assert_eq!(c.negotiate("gzip, zstd, br"), Some(CompressionAlgo::Brotli));
    }

    #[test]
    fn should_compress_text_html() {
        let c = Compression::new(CompressionConfig::default());
        assert!(c.should_compress("text/html; charset=utf-8", 1024));
    }

    #[test]
    fn should_compress_json() {
        let c = Compression::new(CompressionConfig::default());
        assert!(c.should_compress("application/json", 512));
    }

    #[test]
    fn should_not_compress_small_body() {
        let c = Compression::new(CompressionConfig::default());
        assert!(!c.should_compress("text/html", 100));
    }

    #[test]
    fn should_not_compress_binary() {
        let c = Compression::new(CompressionConfig::default());
        assert!(!c.should_compress("image/png", 10_000));
    }

    #[test]
    fn should_not_compress_disabled() {
        let c = Compression::new(CompressionConfig {
            enabled: false,
            ..CompressionConfig::default()
        });
        assert!(!c.should_compress("text/html", 10_000));
    }

    #[test]
    fn apply_headers_gzip() {
        let c = Compression::new(CompressionConfig::default());
        let mut resp = crate::middleware::Response::with_status(200, "ok");
        c.apply_headers(&CompressionAlgo::Gzip, &mut resp);

        assert!(resp
            .headers
            .iter()
            .any(|(k, v)| k == "Content-Encoding" && v == "gzip"));
        assert!(resp
            .headers
            .iter()
            .any(|(k, v)| k == "Vary" && v == "Accept-Encoding"));
    }

    #[test]
    fn apply_headers_brotli() {
        let c = Compression::new(CompressionConfig::default());
        let mut resp = crate::middleware::Response::with_status(200, "ok");
        c.apply_headers(&CompressionAlgo::Brotli, &mut resp);

        assert!(resp
            .headers
            .iter()
            .any(|(k, v)| k == "Content-Encoding" && v == "br"));
    }

    #[test]
    fn apply_headers_zstd() {
        let c = Compression::new(CompressionConfig::default());
        let mut resp = crate::middleware::Response::with_status(200, "ok");
        c.apply_headers(&CompressionAlgo::Zstd, &mut resp);

        assert!(resp
            .headers
            .iter()
            .any(|(k, v)| k == "Content-Encoding" && v == "zstd"));
    }

    #[test]
    fn svg_is_compressible() {
        let c = Compression::new(CompressionConfig::default());
        assert!(c.should_compress("image/svg+xml", 1024));
    }
}
