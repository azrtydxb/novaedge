//! Middleware pipeline runner.
//!
//! Evaluates a route's middleware chain against an incoming request.
//! Each middleware can either allow the request to continue (possibly modified)
//! or short-circuit with an immediate response.

use std::sync::Arc;

use dashmap::DashMap;
use tracing::warn;

use super::{MiddlewareResult, Request, Response};
use crate::config::RuntimeConfig;

/// Shared cache of rate-limiter instances keyed by policy name.
/// This ensures rate-limit state is preserved across requests.
static RATE_LIMITERS: std::sync::LazyLock<DashMap<String, super::ratelimit::TokenBucket>> =
    std::sync::LazyLock::new(DashMap::new);

/// Run the middleware pipeline for a route's `middleware_refs`.
///
/// Iterates through the named policies in order.  Returns
/// `MiddlewareResult::Continue(req)` if all middlewares pass, or
/// `MiddlewareResult::Respond(resp)` to short-circuit with an early response.
pub fn run_pipeline(
    config: &Arc<RuntimeConfig>,
    middleware_refs: &[String],
    mut request: Request,
) -> MiddlewareResult {
    for policy_name in middleware_refs {
        let policy = match config.get_policy(policy_name) {
            Some(p) => p,
            None => {
                warn!(policy = %policy_name, "policy not found, skipping");
                continue;
            }
        };

        let result = match policy.policy_type.as_str() {
            "rate-limit" => run_rate_limit(policy_name, &policy.config_json, &request),
            "basic-auth" => run_basic_auth(policy_name, &policy.config_json, &request),
            "cors" => run_cors(&policy.config_json, &request),
            "ip-filter" => run_ip_filter(policy_name, &policy.config_json, &request),
            "waf" => run_waf(policy_name, &policy.config_json, &request),
            "security-headers" => {
                // Security headers are applied on the response path; skip in request phase.
                MiddlewareResult::Continue(request.clone())
            }
            other => {
                warn!(
                    policy_type = %other,
                    policy = %policy_name,
                    "unsupported middleware type, skipping"
                );
                MiddlewareResult::Continue(request.clone())
            }
        };

        match result {
            MiddlewareResult::Respond(resp) => return MiddlewareResult::Respond(resp),
            MiddlewareResult::Continue(req) => request = req,
        }
    }
    MiddlewareResult::Continue(request)
}

// ---------------------------------------------------------------------------
// Per-middleware helpers
// ---------------------------------------------------------------------------

/// Run the rate-limit middleware.
///
/// Uses a shared `TokenBucket` cache (keyed by policy name) so rate-limit
/// state is preserved across requests.
fn run_rate_limit(policy_name: &str, config_json: &str, req: &Request) -> MiddlewareResult {
    let config: serde_json::Value = match serde_json::from_str(config_json) {
        Ok(v) => v,
        Err(e) => {
            warn!(policy = %policy_name, error = %e, "malformed rate-limit config JSON, skipping");
            return MiddlewareResult::Continue(req.clone());
        }
    };

    let rps = config["requests_per_second"]
        .as_f64()
        .unwrap_or(100.0);
    let burst = config["burst"].as_u64().unwrap_or(10) as u32;

    // Get or create a cached TokenBucket for this policy.
    let limiter = RATE_LIMITERS
        .entry(policy_name.to_string())
        .or_insert_with(|| {
            super::ratelimit::TokenBucket::new(super::ratelimit::RateLimitConfig {
                requests_per_second: rps,
                burst,
                key_type: super::ratelimit::RateLimitKeyType::SourceIP,
            })
        });

    let key = &req.client_ip;
    match limiter.check(key) {
        super::ratelimit::RateLimitResult::Allowed { .. } => {
            MiddlewareResult::Continue(req.clone())
        }
        super::ratelimit::RateLimitResult::Denied { retry_after } => {
            MiddlewareResult::Respond(Response {
                status: 429,
                headers: vec![
                    ("Content-Type".into(), "text/plain".into()),
                    (
                        "Retry-After".into(),
                        retry_after.as_secs().to_string(),
                    ),
                ],
                body: b"Too Many Requests".to_vec(),
            })
        }
    }
}

/// Run the basic-auth middleware.
///
/// Fails closed: malformed config returns 500 rather than allowing the request.
fn run_basic_auth(policy_name: &str, config_json: &str, req: &Request) -> MiddlewareResult {
    let config: serde_json::Value = match serde_json::from_str(config_json) {
        Ok(v) => v,
        Err(e) => {
            warn!(policy = %policy_name, error = %e, "malformed basic-auth config JSON, denying request");
            return MiddlewareResult::Respond(Response::with_status(500, "Internal Server Error"));
        }
    };

    let realm = config["realm"].as_str().unwrap_or("Restricted");

    // Parse htpasswd-style credentials (user:password lines).
    let mut users = std::collections::HashMap::new();
    if let Some(htpasswd) = config["htpasswd"].as_str() {
        for line in htpasswd.lines() {
            let line = line.trim();
            if line.is_empty() || line.starts_with('#') {
                continue;
            }
            if let Some((user, pass)) = line.split_once(':') {
                users.insert(user.to_string(), pass.to_string());
            }
        }
    }

    let auth = super::auth::basic::BasicAuth::new(realm, users);
    match auth.check(req) {
        super::auth::AuthResult::Authenticated { .. } => {
            MiddlewareResult::Continue(req.clone())
        }
        super::auth::AuthResult::Denied { status, message } => {
            MiddlewareResult::Respond(Response {
                status,
                headers: vec![
                    ("Content-Type".into(), "text/plain".into()),
                    (
                        "WWW-Authenticate".into(),
                        format!("Basic realm=\"{realm}\""),
                    ),
                ],
                body: message.into_bytes(),
            })
        }
    }
}

/// Run the CORS middleware.
fn run_cors(config_json: &str, req: &Request) -> MiddlewareResult {
    let config: serde_json::Value = match serde_json::from_str(config_json) {
        Ok(v) => v,
        Err(e) => {
            warn!(error = %e, "malformed CORS config JSON, skipping");
            return MiddlewareResult::Continue(req.clone());
        }
    };

    let allowed_origins: Vec<String> = config["allow_origins"]
        .as_array()
        .map(|a| {
            a.iter()
                .filter_map(|v| v.as_str().map(String::from))
                .collect()
        })
        .unwrap_or_else(|| vec!["*".into()]);

    let allowed_methods: Vec<String> = config["allow_methods"]
        .as_array()
        .map(|a| {
            a.iter()
                .filter_map(|v| v.as_str().map(String::from))
                .collect()
        })
        .unwrap_or_default();

    let allowed_headers: Vec<String> = config["allow_headers"]
        .as_array()
        .map(|a| {
            a.iter()
                .filter_map(|v| v.as_str().map(String::from))
                .collect()
        })
        .unwrap_or_default();

    let exposed_headers: Vec<String> = config["expose_headers"]
        .as_array()
        .map(|a| {
            a.iter()
                .filter_map(|v| v.as_str().map(String::from))
                .collect()
        })
        .unwrap_or_default();

    let allow_credentials = config["allow_credentials"].as_bool().unwrap_or(false);
    let max_age = config["max_age_seconds"].as_u64().unwrap_or(86400) as u32;

    let cors = super::cors::CorsMiddleware::new(super::cors::CorsConfig {
        allowed_origins,
        allowed_methods,
        allowed_headers,
        exposed_headers,
        allow_credentials,
        max_age,
    });

    if cors.is_preflight(req) {
        let resp = cors.handle_preflight(req);
        return MiddlewareResult::Respond(resp);
    }

    // For non-preflight requests, CORS headers are added on the response path.
    // Allow the request to continue.
    MiddlewareResult::Continue(req.clone())
}

/// Run the IP filter middleware.
///
/// Fails closed: malformed config returns 500 rather than allowing the request.
fn run_ip_filter(policy_name: &str, config_json: &str, req: &Request) -> MiddlewareResult {
    let config: serde_json::Value = match serde_json::from_str(config_json) {
        Ok(v) => v,
        Err(e) => {
            warn!(policy = %policy_name, error = %e, "malformed ip-filter config JSON, denying request");
            return MiddlewareResult::Respond(Response::with_status(500, "Internal Server Error"));
        }
    };

    let action = config["action"].as_str().unwrap_or("allow");
    let cidrs: Vec<String> = config["cidrs"]
        .as_array()
        .map(|a| {
            a.iter()
                .filter_map(|v| v.as_str().map(String::from))
                .collect()
        })
        .unwrap_or_default();

    // Build CIDR ranges.
    let mut ranges = Vec::new();
    for cidr_str in &cidrs {
        if let Ok(range) = super::ip_filter::CidrRange::parse(cidr_str) {
            ranges.push(range);
        }
    }

    // Determine allowlist/denylist based on action.
    let (allowlist, denylist) = if action == "deny" {
        (Vec::new(), ranges)
    } else {
        (ranges, Vec::new())
    };

    let source_header = config["source_header"].as_str().unwrap_or("");
    let trust_xff = !source_header.is_empty();

    let filter = super::ip_filter::IpFilter::new(super::ip_filter::IpFilterConfig {
        allowlist,
        denylist,
        trust_xff,
        xff_depth: 1,
    });

    if filter.check(req) {
        MiddlewareResult::Continue(req.clone())
    } else {
        MiddlewareResult::Respond(Response {
            status: 403,
            headers: vec![("Content-Type".into(), "text/plain".into())],
            body: b"Forbidden".to_vec(),
        })
    }
}

/// Run the WAF middleware.
///
/// Fails closed: malformed config returns 500 rather than allowing the request.
fn run_waf(policy_name: &str, config_json: &str, req: &Request) -> MiddlewareResult {
    let config: serde_json::Value = match serde_json::from_str(config_json) {
        Ok(v) => v,
        Err(e) => {
            warn!(policy = %policy_name, error = %e, "malformed WAF config JSON, denying request");
            return MiddlewareResult::Respond(Response::with_status(500, "Internal Server Error"));
        }
    };

    let enabled = config["enabled"].as_bool().unwrap_or(true);
    if !enabled {
        return MiddlewareResult::Continue(req.clone());
    }

    let mode_str = config["mode"].as_str().unwrap_or("prevention");
    let mode = if mode_str == "detection" {
        super::waf::WafMode::Detect
    } else {
        super::waf::WafMode::Block
    };

    let waf = super::waf::WafEngine::with_default_rules(mode);
    match waf.check(req) {
        super::waf::WafDecision::Allow => MiddlewareResult::Continue(req.clone()),
        super::waf::WafDecision::Block {
            rule_id,
            description,
        } => {
            warn!(
                rule_id,
                description = %description,
                "WAF blocked request"
            );
            MiddlewareResult::Respond(Response {
                status: 403,
                headers: vec![("Content-Type".into(), "text/plain".into())],
                body: b"Forbidden".to_vec(),
            })
        }
        super::waf::WafDecision::Detect {
            rule_id,
            description,
        } => {
            warn!(
                rule_id,
                description = %description,
                "WAF detected suspicious request (detect-only mode)"
            );
            MiddlewareResult::Continue(req.clone())
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config::{PolicyState, RuntimeConfig};

    fn make_request(method: &str, path: &str, client_ip: &str) -> Request {
        Request {
            method: method.into(),
            path: path.into(),
            host: "example.com".into(),
            headers: vec![("Host".into(), "example.com".into())],
            body: None,
            client_ip: client_ip.into(),
        }
    }

    #[test]
    fn empty_refs_passes_through() {
        let config = Arc::new(RuntimeConfig::new());
        let req = make_request("GET", "/", "127.0.0.1");
        match run_pipeline(&config, &[], req) {
            MiddlewareResult::Continue(_) => {}
            MiddlewareResult::Respond(_) => panic!("expected Continue"),
        }
    }

    #[test]
    fn missing_policy_skipped() {
        let config = Arc::new(RuntimeConfig::new());
        let req = make_request("GET", "/", "127.0.0.1");
        match run_pipeline(&config, &["nonexistent".into()], req) {
            MiddlewareResult::Continue(_) => {}
            MiddlewareResult::Respond(_) => panic!("expected Continue"),
        }
    }

    #[test]
    fn waf_blocks_xss() {
        let config = Arc::new(RuntimeConfig::new());
        config.upsert_policy(PolicyState {
            name: "waf-1".into(),
            policy_type: "waf".into(),
            target_ref: String::new(),
            config_json: r#"{"enabled":true,"mode":"prevention"}"#.into(),
        });

        let req = make_request("GET", "/search?q=<script>alert(1)</script>", "127.0.0.1");
        match run_pipeline(&config, &["waf-1".into()], req) {
            MiddlewareResult::Respond(resp) => {
                assert_eq!(resp.status, 403);
            }
            MiddlewareResult::Continue(_) => panic!("expected Respond"),
        }
    }

    #[test]
    fn ip_filter_blocks_denied() {
        let config = Arc::new(RuntimeConfig::new());
        config.upsert_policy(PolicyState {
            name: "ip-filter-1".into(),
            policy_type: "ip-filter".into(),
            target_ref: String::new(),
            config_json: r#"{"action":"deny","cidrs":["10.0.0.0/8"]}"#.into(),
        });

        let req = make_request("GET", "/", "10.0.0.1");
        match run_pipeline(&config, &["ip-filter-1".into()], req) {
            MiddlewareResult::Respond(resp) => {
                assert_eq!(resp.status, 403);
            }
            MiddlewareResult::Continue(_) => panic!("expected Respond"),
        }
    }

    #[test]
    fn cors_preflight_handled() {
        let config = Arc::new(RuntimeConfig::new());
        config.upsert_policy(PolicyState {
            name: "cors-1".into(),
            policy_type: "cors".into(),
            target_ref: String::new(),
            config_json: r#"{"allow_origins":["*"],"allow_methods":["GET","POST"]}"#.into(),
        });

        let req = Request {
            method: "OPTIONS".into(),
            path: "/".into(),
            host: "example.com".into(),
            headers: vec![
                ("Origin".into(), "http://example.com".into()),
                (
                    "Access-Control-Request-Method".into(),
                    "POST".into(),
                ),
            ],
            body: None,
            client_ip: "127.0.0.1".into(),
        };

        match run_pipeline(&config, &["cors-1".into()], req) {
            MiddlewareResult::Respond(resp) => {
                assert_eq!(resp.status, 204);
            }
            MiddlewareResult::Continue(_) => panic!("expected Respond for preflight"),
        }
    }

    #[test]
    fn unsupported_type_skipped() {
        let config = Arc::new(RuntimeConfig::new());
        config.upsert_policy(PolicyState {
            name: "custom-1".into(),
            policy_type: "custom-thing".into(),
            target_ref: String::new(),
            config_json: "{}".into(),
        });

        let req = make_request("GET", "/", "127.0.0.1");
        match run_pipeline(&config, &["custom-1".into()], req) {
            MiddlewareResult::Continue(_) => {}
            MiddlewareResult::Respond(_) => panic!("expected Continue"),
        }
    }
}
