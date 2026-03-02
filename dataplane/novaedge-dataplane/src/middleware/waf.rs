//! Web Application Firewall (WAF) middleware.
//!
//! Provides a rule-based engine with OWASP-style regex patterns for detecting
//! common web attacks (XSS, SQL injection, path traversal, etc.).
//!
//! Uses compiled regex patterns (case-insensitive) and URL decoding for
//! evasion resistance.

use regex::RegexSet;

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
    /// Regex pattern to match (case-insensitive).
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

/// WAF rule engine with compiled regex patterns.
pub struct WafEngine {
    config: WafConfig,
    /// Pre-compiled regex sets per target type for fast matching.
    compiled: CompiledRules,
}

/// Pre-compiled regex patterns grouped by target.
struct CompiledRules {
    /// (rule_index, regex) pairs per target type.
    uri_set: Option<(RegexSet, Vec<usize>)>,
    query_set: Option<(RegexSet, Vec<usize>)>,
    headers_set: Option<(RegexSet, Vec<usize>)>,
    user_agent_set: Option<(RegexSet, Vec<usize>)>,
    body_set: Option<(RegexSet, Vec<usize>)>,
}

impl CompiledRules {
    fn compile(rules: &[WafRule]) -> Self {
        fn build_set(rules: &[WafRule], target: &WafTarget) -> Option<(RegexSet, Vec<usize>)> {
            let mut patterns = Vec::new();
            let mut indices = Vec::new();
            for (i, rule) in rules.iter().enumerate() {
                if rule.pattern.is_empty() {
                    continue;
                }
                if std::mem::discriminant(&rule.target) == std::mem::discriminant(target) {
                    // Wrap pattern with case-insensitive flag.
                    patterns.push(format!("(?i){}", rule.pattern));
                    indices.push(i);
                }
            }
            if patterns.is_empty() {
                return None;
            }
            match RegexSet::new(&patterns) {
                Ok(set) => Some((set, indices)),
                Err(_) => None,
            }
        }

        Self {
            uri_set: build_set(rules, &WafTarget::Uri),
            query_set: build_set(rules, &WafTarget::QueryString),
            headers_set: build_set(rules, &WafTarget::Headers),
            user_agent_set: build_set(rules, &WafTarget::UserAgent),
            body_set: build_set(rules, &WafTarget::Body),
        }
    }
}

/// URL-decode a string (handles %XX sequences).
fn url_decode(input: &str) -> String {
    let mut result = String::with_capacity(input.len());
    let mut chars = input.bytes();
    while let Some(b) = chars.next() {
        if b == b'%' {
            let hi = chars.next();
            let lo = chars.next();
            if let (Some(h), Some(l)) = (hi, lo) {
                if let (Some(hv), Some(lv)) = (hex_val(h), hex_val(l)) {
                    result.push((hv << 4 | lv) as char);
                    continue;
                }
            }
            result.push('%');
        } else if b == b'+' {
            result.push(' ');
        } else {
            result.push(b as char);
        }
    }
    result
}

fn hex_val(b: u8) -> Option<u8> {
    match b {
        b'0'..=b'9' => Some(b - b'0'),
        b'a'..=b'f' => Some(b - b'a' + 10),
        b'A'..=b'F' => Some(b - b'A' + 10),
        _ => None,
    }
}

impl WafEngine {
    /// Create a WAF engine with the given configuration.
    pub fn new(config: WafConfig) -> Self {
        let compiled = CompiledRules::compile(&config.rules);
        Self { config, compiled }
    }

    /// Create a WAF engine pre-loaded with basic OWASP-style rules.
    pub fn with_default_rules(mode: WafMode) -> Self {
        let rules = vec![
            WafRule {
                id: 941100,
                description: "XSS Attack: Script Tag".into(),
                target: WafTarget::QueryString,
                pattern: r"<\s*script".into(),
                severity: WafSeverity::Critical,
            },
            WafRule {
                id: 941110,
                description: "XSS Attack: Event Handler".into(),
                target: WafTarget::QueryString,
                pattern: r"on\w+\s*=".into(),
                severity: WafSeverity::Critical,
            },
            WafRule {
                id: 942100,
                description: "SQL Injection: Common Keywords".into(),
                target: WafTarget::QueryString,
                pattern: r"union\s+select".into(),
                severity: WafSeverity::Critical,
            },
            WafRule {
                id: 942110,
                description: "SQL Injection: OR/AND Bypass".into(),
                target: WafTarget::QueryString,
                pattern: r"'\s*or\s+'[^']*'\s*=\s*'".into(),
                severity: WafSeverity::Critical,
            },
            WafRule {
                id: 930100,
                description: "Path Traversal".into(),
                target: WafTarget::Uri,
                pattern: r"\.\.[\\/]".into(),
                severity: WafSeverity::High,
            },
            WafRule {
                id: 913100,
                description: "Scanner/Bot Detection".into(),
                target: WafTarget::UserAgent,
                pattern: r"sqlmap".into(),
                severity: WafSeverity::Medium,
            },
            WafRule {
                id: 913110,
                description: "Scanner/Bot Detection: Nikto".into(),
                target: WafTarget::UserAgent,
                pattern: r"nikto".into(),
                severity: WafSeverity::Medium,
            },
        ];
        Self::new(WafConfig { mode, rules })
    }

    /// Evaluate all rules against a request.
    pub fn check(&self, req: &super::Request) -> WafDecision {
        // Check URI rules (with URL decoding).
        if let Some((ref set, ref indices)) = self.compiled.uri_set {
            let decoded = url_decode(&req.path);
            for idx in set.matches(&decoded) {
                let rule = &self.config.rules[indices[idx]];
                return self.make_decision(rule);
            }
        }

        // Check query string rules (with URL decoding).
        if let Some((ref set, ref indices)) = self.compiled.query_set {
            if let Some(qs) = req.path.split_once('?').map(|(_, q)| q) {
                let decoded = url_decode(qs);
                for idx in set.matches(&decoded) {
                    let rule = &self.config.rules[indices[idx]];
                    return self.make_decision(rule);
                }
            }
        }

        // Check header rules.
        if let Some((ref set, ref indices)) = self.compiled.headers_set {
            for (_, value) in &req.headers {
                for idx in set.matches(value) {
                    let rule = &self.config.rules[indices[idx]];
                    return self.make_decision(rule);
                }
            }
        }

        // Check User-Agent rules.
        if let Some((ref set, ref indices)) = self.compiled.user_agent_set {
            if let Some((_, ua)) = req
                .headers
                .iter()
                .find(|(k, _)| k.eq_ignore_ascii_case("user-agent"))
            {
                for idx in set.matches(ua) {
                    let rule = &self.config.rules[indices[idx]];
                    return self.make_decision(rule);
                }
            }
        }

        // Check body rules.
        if let Some((ref set, ref indices)) = self.compiled.body_set {
            if let Some(body) = &req.body {
                let body_str = String::from_utf8_lossy(body);
                for idx in set.matches(&body_str) {
                    let rule = &self.config.rules[indices[idx]];
                    return self.make_decision(rule);
                }
            }
        }

        WafDecision::Allow
    }

    fn make_decision(&self, rule: &WafRule) -> WafDecision {
        match self.config.mode {
            WafMode::Block => WafDecision::Block {
                rule_id: rule.id,
                description: rule.description.clone(),
            },
            WafMode::Detect => WafDecision::Detect {
                rule_id: rule.id,
                description: rule.description.clone(),
            },
        }
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
        // URL-encoded ../ should be decoded and caught by the same rule.
        let req = make_req("GET", "/files/..%2f..%2fetc/passwd");
        assert!(matches!(
            waf.check(&req),
            WafDecision::Block {
                rule_id: 930100,
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
        assert!(waf.rule_count() >= 7);
    }

    #[test]
    fn url_decode_works() {
        assert_eq!(url_decode("..%2f..%2f"), "../../");
        assert_eq!(url_decode("hello+world"), "hello world");
        assert_eq!(url_decode("%3Cscript%3E"), "<script>");
        assert_eq!(url_decode("normal"), "normal");
    }

    #[test]
    fn no_false_positive_on_dashes() {
        // Removed the `--` rule to prevent false positives on normal URLs.
        let waf = WafEngine::with_default_rules(WafMode::Block);
        let req = make_req("GET", "/api/some-path?key=value--more");
        assert!(matches!(waf.check(&req), WafDecision::Allow));
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
