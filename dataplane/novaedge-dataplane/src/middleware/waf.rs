//! Web Application Firewall (WAF) middleware.
//!
//! Provides a rule-based engine with OWASP-style patterns for detecting
//! common web attacks (XSS, SQL injection, path traversal, etc.).

/// WAF configuration.
#[derive(Debug, Clone)]
pub struct WafConfig {
    /// Operating mode (block or detect-only).
    pub mode: WafMode,
    /// List of rules to evaluate.
    pub rules: Vec<WafRule>,
}

/// WAF operating mode.
#[derive(Debug, Clone)]
pub enum WafMode {
    /// Block matching requests (return 403).
    Block,
    /// Log matching requests but allow them through.
    Detect,
}

/// A single WAF rule.
#[derive(Debug, Clone)]
pub struct WafRule {
    /// Unique rule identifier (modeled after OWASP CRS rule IDs).
    pub id: u32,
    /// Human-readable description.
    pub description: String,
    /// Which part of the request to inspect.
    pub target: WafTarget,
    /// Substring pattern to match (case-insensitive).
    pub pattern: String,
    /// Severity classification.
    pub severity: WafSeverity,
}

/// Part of the request that a WAF rule inspects.
#[derive(Debug, Clone)]
pub enum WafTarget {
    /// Request URI / path.
    Uri,
    /// Query string portion of the URI.
    QueryString,
    /// All header values.
    Headers,
    /// Request body.
    Body,
    /// User-Agent header specifically.
    UserAgent,
}

/// WAF rule severity.
#[derive(Debug, Clone, PartialEq)]
pub enum WafSeverity {
    Critical,
    High,
    Medium,
    Low,
}

/// WAF evaluation result.
#[derive(Debug)]
pub enum WafDecision {
    /// Request passed all rules.
    Allow,
    /// Request matched a rule and should be blocked.
    Block { rule_id: u32, description: String },
    /// Request matched a rule in detect-only mode.
    Detect { rule_id: u32, description: String },
}

/// WAF rule engine.
pub struct WafEngine {
    config: WafConfig,
}

impl WafEngine {
    /// Create a WAF engine with the given configuration.
    pub fn new(config: WafConfig) -> Self {
        Self { config }
    }

    /// Create a WAF engine pre-loaded with basic OWASP-style rules.
    pub fn with_default_rules(mode: WafMode) -> Self {
        let rules = vec![
            WafRule {
                id: 941100,
                description: "XSS Attack: Script Tag".into(),
                target: WafTarget::QueryString,
                pattern: "<script".into(),
                severity: WafSeverity::Critical,
            },
            WafRule {
                id: 941110,
                description: "XSS Attack: Event Handler".into(),
                target: WafTarget::QueryString,
                pattern: "onerror=".into(),
                severity: WafSeverity::Critical,
            },
            WafRule {
                id: 942100,
                description: "SQL Injection: Common Keywords".into(),
                target: WafTarget::QueryString,
                pattern: "union select".into(),
                severity: WafSeverity::Critical,
            },
            WafRule {
                id: 942110,
                description: "SQL Injection: OR/AND Bypass".into(),
                target: WafTarget::QueryString,
                pattern: "' or '1'='1".into(),
                severity: WafSeverity::Critical,
            },
            WafRule {
                id: 942120,
                description: "SQL Injection: Comment Sequence".into(),
                target: WafTarget::QueryString,
                pattern: "--".into(),
                severity: WafSeverity::High,
            },
            WafRule {
                id: 930100,
                description: "Path Traversal".into(),
                target: WafTarget::Uri,
                pattern: "../".into(),
                severity: WafSeverity::High,
            },
            WafRule {
                id: 930110,
                description: "Path Traversal: Encoded".into(),
                target: WafTarget::Uri,
                pattern: "..%2f".into(),
                severity: WafSeverity::High,
            },
            WafRule {
                id: 913100,
                description: "Scanner/Bot Detection".into(),
                target: WafTarget::UserAgent,
                pattern: "sqlmap".into(),
                severity: WafSeverity::Medium,
            },
            WafRule {
                id: 913110,
                description: "Scanner/Bot Detection: Nikto".into(),
                target: WafTarget::UserAgent,
                pattern: "nikto".into(),
                severity: WafSeverity::Medium,
            },
        ];
        Self::new(WafConfig { mode, rules })
    }

    /// Evaluate all rules against a request.
    pub fn check(&self, req: &super::Request) -> WafDecision {
        for rule in &self.config.rules {
            if rule.pattern.is_empty() {
                continue;
            }

            let matched = match &rule.target {
                WafTarget::Uri => req
                    .path
                    .to_lowercase()
                    .contains(&rule.pattern.to_lowercase()),
                WafTarget::QueryString => {
                    if let Some(qs) = req.path.split_once('?').map(|(_, q)| q) {
                        qs.to_lowercase().contains(&rule.pattern.to_lowercase())
                    } else {
                        false
                    }
                }
                WafTarget::Headers => req
                    .headers
                    .iter()
                    .any(|(_, value)| value.to_lowercase().contains(&rule.pattern.to_lowercase())),
                WafTarget::UserAgent => req
                    .headers
                    .iter()
                    .find(|(k, _)| k.eq_ignore_ascii_case("user-agent"))
                    .map(|(_, ua)| ua.to_lowercase().contains(&rule.pattern.to_lowercase()))
                    .unwrap_or(false),
                WafTarget::Body => {
                    if let Some(body) = &req.body {
                        let body_str = String::from_utf8_lossy(body);
                        body_str
                            .to_lowercase()
                            .contains(&rule.pattern.to_lowercase())
                    } else {
                        false
                    }
                }
            };

            if matched {
                return match self.config.mode {
                    WafMode::Block => WafDecision::Block {
                        rule_id: rule.id,
                        description: rule.description.clone(),
                    },
                    WafMode::Detect => WafDecision::Detect {
                        rule_id: rule.id,
                        description: rule.description.clone(),
                    },
                };
            }
        }

        WafDecision::Allow
    }

    /// Return the number of rules loaded.
    pub fn rule_count(&self) -> usize {
        self.config.rules.len()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_req(method: &str, path: &str) -> crate::middleware::Request {
        crate::middleware::Request {
            method: method.into(),
            path: path.into(),
            host: "example.com".into(),
            headers: vec![],
            body: None,
            client_ip: "127.0.0.1".into(),
        }
    }

    fn make_req_with_ua(path: &str, ua: &str) -> crate::middleware::Request {
        crate::middleware::Request {
            method: "GET".into(),
            path: path.into(),
            host: "example.com".into(),
            headers: vec![("User-Agent".into(), ua.into())],
            body: None,
            client_ip: "127.0.0.1".into(),
        }
    }

    fn make_req_with_body(path: &str, body: &[u8]) -> crate::middleware::Request {
        crate::middleware::Request {
            method: "POST".into(),
            path: path.into(),
            host: "example.com".into(),
            headers: vec![],
            body: Some(body.to_vec()),
            client_ip: "127.0.0.1".into(),
        }
    }

    #[test]
    fn clean_request_allowed() {
        let waf = WafEngine::with_default_rules(WafMode::Block);
        let req = make_req("GET", "/api/users");
        assert!(matches!(waf.check(&req), WafDecision::Allow));
    }

    #[test]
    fn xss_script_tag_blocked() {
        let waf = WafEngine::with_default_rules(WafMode::Block);
        let req = make_req("GET", "/search?q=<script>alert(1)</script>");
        match waf.check(&req) {
            WafDecision::Block {
                rule_id,
                description,
            } => {
                assert_eq!(rule_id, 941100);
                assert!(description.contains("XSS"));
            }
            other => panic!("expected Block, got: {other:?}"),
        }
    }

    #[test]
    fn xss_case_insensitive() {
        let waf = WafEngine::with_default_rules(WafMode::Block);
        let req = make_req("GET", "/search?q=<SCRIPT>alert(1)</SCRIPT>");
        assert!(matches!(waf.check(&req), WafDecision::Block { .. }));
    }

    #[test]
    fn sql_injection_union_select() {
        let waf = WafEngine::with_default_rules(WafMode::Block);
        let req = make_req("GET", "/api?id=1 UNION SELECT * FROM users");
        assert!(matches!(
            waf.check(&req),
            WafDecision::Block {
                rule_id: 942100,
                ..
            }
        ));
    }

    #[test]
    fn sql_injection_or_bypass() {
        let waf = WafEngine::with_default_rules(WafMode::Block);
        let req = make_req("GET", "/login?user=' OR '1'='1");
        assert!(matches!(
            waf.check(&req),
            WafDecision::Block {
                rule_id: 942110,
                ..
            }
        ));
    }

    #[test]
    fn path_traversal_blocked() {
        let waf = WafEngine::with_default_rules(WafMode::Block);
        let req = make_req("GET", "/files/../../etc/passwd");
        match waf.check(&req) {
            WafDecision::Block { rule_id, .. } => {
                assert_eq!(rule_id, 930100);
            }
            other => panic!("expected Block, got: {other:?}"),
        }
    }

    #[test]
    fn path_traversal_encoded() {
        let waf = WafEngine::with_default_rules(WafMode::Block);
        let req = make_req("GET", "/files/..%2f..%2fetc/passwd");
        assert!(matches!(
            waf.check(&req),
            WafDecision::Block {
                rule_id: 930110,
                ..
            }
        ));
    }

    #[test]
    fn detect_mode_allows_but_reports() {
        let waf = WafEngine::with_default_rules(WafMode::Detect);
        let req = make_req("GET", "/search?q=<script>alert(1)</script>");
        match waf.check(&req) {
            WafDecision::Detect {
                rule_id,
                description,
            } => {
                assert_eq!(rule_id, 941100);
                assert!(description.contains("XSS"));
            }
            other => panic!("expected Detect, got: {other:?}"),
        }
    }

    #[test]
    fn scanner_user_agent_detected() {
        let waf = WafEngine::with_default_rules(WafMode::Block);
        let req = make_req_with_ua("/", "sqlmap/1.0");
        assert!(matches!(
            waf.check(&req),
            WafDecision::Block {
                rule_id: 913100,
                ..
            }
        ));
    }

    #[test]
    fn nikto_user_agent_detected() {
        let waf = WafEngine::with_default_rules(WafMode::Block);
        let req = make_req_with_ua("/", "Mozilla/5.0 Nikto/2.1");
        assert!(matches!(
            waf.check(&req),
            WafDecision::Block {
                rule_id: 913110,
                ..
            }
        ));
    }

    #[test]
    fn normal_user_agent_allowed() {
        let waf = WafEngine::with_default_rules(WafMode::Block);
        let req = make_req_with_ua("/", "Mozilla/5.0 (X11; Linux x86_64) Chrome/120");
        assert!(matches!(waf.check(&req), WafDecision::Allow));
    }

    #[test]
    fn body_inspection() {
        let waf = WafEngine::new(WafConfig {
            mode: WafMode::Block,
            rules: vec![WafRule {
                id: 1,
                description: "Body XSS".into(),
                target: WafTarget::Body,
                pattern: "<script".into(),
                severity: WafSeverity::Critical,
            }],
        });

        let req = make_req_with_body("/submit", b"<script>alert(1)</script>");
        assert!(matches!(
            waf.check(&req),
            WafDecision::Block { rule_id: 1, .. }
        ));

        let clean_req = make_req_with_body("/submit", b"Hello world");
        assert!(matches!(waf.check(&clean_req), WafDecision::Allow));
    }

    #[test]
    fn header_inspection() {
        let waf = WafEngine::new(WafConfig {
            mode: WafMode::Block,
            rules: vec![WafRule {
                id: 2,
                description: "Header injection".into(),
                target: WafTarget::Headers,
                pattern: "evil-value".into(),
                severity: WafSeverity::High,
            }],
        });

        let mut req = make_req("GET", "/");
        req.headers
            .push(("X-Custom".into(), "evil-value-here".into()));
        assert!(matches!(
            waf.check(&req),
            WafDecision::Block { rule_id: 2, .. }
        ));
    }

    #[test]
    fn empty_pattern_skipped() {
        let waf = WafEngine::new(WafConfig {
            mode: WafMode::Block,
            rules: vec![WafRule {
                id: 3,
                description: "Empty pattern".into(),
                target: WafTarget::Uri,
                pattern: "".into(),
                severity: WafSeverity::Low,
            }],
        });

        let req = make_req("GET", "/anything");
        assert!(matches!(waf.check(&req), WafDecision::Allow));
    }

    #[test]
    fn no_query_string_no_match() {
        let waf = WafEngine::with_default_rules(WafMode::Block);
        let req = make_req("GET", "/api/clean-path");
        assert!(matches!(waf.check(&req), WafDecision::Allow));
    }

    #[test]
    fn rule_count() {
        let waf = WafEngine::with_default_rules(WafMode::Block);
        assert!(waf.rule_count() >= 9);
    }

    #[test]
    fn no_body_no_match() {
        let waf = WafEngine::new(WafConfig {
            mode: WafMode::Block,
            rules: vec![WafRule {
                id: 4,
                description: "Body check".into(),
                target: WafTarget::Body,
                pattern: "attack".into(),
                severity: WafSeverity::High,
            }],
        });

        let req = make_req("GET", "/"); // no body
        assert!(matches!(waf.check(&req), WafDecision::Allow));
    }
}
