//! HTTP middleware pipeline for the NovaEdge dataplane.
//!
//! Provides composable middleware components for request/response processing
//! including rate limiting, authentication, WAF, CORS, IP filtering,
//! security headers, compression, and caching.

pub mod auth;
pub mod cache;
pub mod compression;
pub mod cors;
pub mod ip_filter;
pub mod pipeline;
pub mod ratelimit;
pub mod security_headers;
pub mod waf;

/// HTTP request for middleware processing.
#[derive(Debug, Clone)]
pub struct Request {
    pub method: String,
    pub path: String,
    pub host: String,
    pub headers: Vec<(String, String)>,
    pub body: Option<Vec<u8>>,
    pub client_ip: String,
}

/// HTTP response from middleware/backend.
#[derive(Debug, Clone)]
pub struct Response {
    pub status: u16,
    pub headers: Vec<(String, String)>,
    pub body: Vec<u8>,
}

impl Response {
    /// Create a simple text response with the given status code.
    pub fn with_status(status: u16, body: &str) -> Self {
        Self {
            status,
            headers: vec![("Content-Type".into(), "text/plain".into())],
            body: body.as_bytes().to_vec(),
        }
    }
}

/// Middleware decision: continue processing or return early.
pub enum MiddlewareResult {
    /// Continue to the next middleware / backend with the (possibly modified) request.
    Continue(Request),
    /// Short-circuit: return this response immediately.
    Respond(Response),
}
