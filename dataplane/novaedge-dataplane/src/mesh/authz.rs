use super::spiffe::SpiffeId;

/// Authorization action.
#[derive(Debug, Clone, PartialEq)]
pub enum AuthzAction {
    Allow,
    Deny,
}

/// Authorization rule.
#[derive(Debug, Clone)]
pub struct AuthzRule {
    pub source_patterns: Vec<String>,
    pub destination_port: Option<u16>,
    pub action: AuthzAction,
}

/// Mesh authorization policy.
pub struct MeshAuthzPolicy {
    rules: Vec<AuthzRule>,
    default_action: AuthzAction,
}

impl MeshAuthzPolicy {
    pub fn new(default_action: AuthzAction) -> Self {
        Self {
            rules: Vec::new(),
            default_action,
        }
    }

    pub fn add_rule(&mut self, rule: AuthzRule) {
        self.rules.push(rule);
    }

    pub fn set_rules(&mut self, rules: Vec<AuthzRule>) {
        self.rules = rules;
    }

    /// Check if a request from source to destination port is allowed.
    pub fn check(&self, source: &SpiffeId, dest_port: u16) -> AuthzAction {
        for rule in &self.rules {
            let port_matches = rule.destination_port.is_none_or(|p| p == dest_port);
            let source_matches = rule
                .source_patterns
                .iter()
                .any(|pattern| source.matches_pattern(pattern));

            if port_matches && source_matches {
                return rule.action.clone();
            }
        }
        self.default_action.clone()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn test_source() -> SpiffeId {
        SpiffeId::workload("cluster.local", "default", "web")
    }

    #[test]
    fn test_default_allow() {
        let policy = MeshAuthzPolicy::new(AuthzAction::Allow);
        assert_eq!(policy.check(&test_source(), 8080), AuthzAction::Allow);
    }

    #[test]
    fn test_default_deny() {
        let policy = MeshAuthzPolicy::new(AuthzAction::Deny);
        assert_eq!(policy.check(&test_source(), 8080), AuthzAction::Deny);
    }

    #[test]
    fn test_allow_rule() {
        let mut policy = MeshAuthzPolicy::new(AuthzAction::Deny);
        policy.add_rule(AuthzRule {
            source_patterns: vec!["spiffe://cluster.local/ns/default/*".into()],
            destination_port: Some(8080),
            action: AuthzAction::Allow,
        });

        assert_eq!(policy.check(&test_source(), 8080), AuthzAction::Allow);
        // Wrong port -> default deny
        assert_eq!(policy.check(&test_source(), 9090), AuthzAction::Deny);
    }

    #[test]
    fn test_deny_rule() {
        let mut policy = MeshAuthzPolicy::new(AuthzAction::Allow);
        policy.add_rule(AuthzRule {
            source_patterns: vec!["spiffe://cluster.local/ns/default/sa/web".into()],
            destination_port: None, // any port
            action: AuthzAction::Deny,
        });

        assert_eq!(policy.check(&test_source(), 8080), AuthzAction::Deny);
        assert_eq!(policy.check(&test_source(), 443), AuthzAction::Deny);

        // Different source -> default allow
        let other = SpiffeId::workload("cluster.local", "prod", "api");
        assert_eq!(policy.check(&other, 8080), AuthzAction::Allow);
    }

    #[test]
    fn test_wildcard_source() {
        let mut policy = MeshAuthzPolicy::new(AuthzAction::Deny);
        policy.add_rule(AuthzRule {
            source_patterns: vec!["*".into()],
            destination_port: Some(80),
            action: AuthzAction::Allow,
        });

        assert_eq!(policy.check(&test_source(), 80), AuthzAction::Allow);
        assert_eq!(policy.check(&test_source(), 443), AuthzAction::Deny);
    }

    #[test]
    fn test_multiple_source_patterns() {
        let mut policy = MeshAuthzPolicy::new(AuthzAction::Deny);
        policy.add_rule(AuthzRule {
            source_patterns: vec![
                "spiffe://cluster.local/ns/prod/*".into(),
                "spiffe://cluster.local/ns/default/*".into(),
            ],
            destination_port: None,
            action: AuthzAction::Allow,
        });

        assert_eq!(policy.check(&test_source(), 8080), AuthzAction::Allow);
        let prod = SpiffeId::workload("cluster.local", "prod", "api");
        assert_eq!(policy.check(&prod, 8080), AuthzAction::Allow);
        let staging = SpiffeId::workload("cluster.local", "staging", "api");
        assert_eq!(policy.check(&staging, 8080), AuthzAction::Deny);
    }

    #[test]
    fn test_set_rules() {
        let mut policy = MeshAuthzPolicy::new(AuthzAction::Deny);
        policy.set_rules(vec![
            AuthzRule {
                source_patterns: vec!["*".into()],
                destination_port: Some(443),
                action: AuthzAction::Allow,
            },
            AuthzRule {
                source_patterns: vec!["*".into()],
                destination_port: Some(80),
                action: AuthzAction::Allow,
            },
        ]);

        assert_eq!(policy.check(&test_source(), 443), AuthzAction::Allow);
        assert_eq!(policy.check(&test_source(), 80), AuthzAction::Allow);
        assert_eq!(policy.check(&test_source(), 8080), AuthzAction::Deny);
    }

    #[test]
    fn test_first_matching_rule_wins() {
        let mut policy = MeshAuthzPolicy::new(AuthzAction::Allow);
        policy.add_rule(AuthzRule {
            source_patterns: vec!["*".into()],
            destination_port: Some(8080),
            action: AuthzAction::Deny,
        });
        policy.add_rule(AuthzRule {
            source_patterns: vec!["*".into()],
            destination_port: Some(8080),
            action: AuthzAction::Allow,
        });

        // First rule wins
        assert_eq!(policy.check(&test_source(), 8080), AuthzAction::Deny);
    }

    #[test]
    fn test_port_none_matches_any() {
        let mut policy = MeshAuthzPolicy::new(AuthzAction::Deny);
        policy.add_rule(AuthzRule {
            source_patterns: vec!["spiffe://cluster.local/ns/default/sa/web".into()],
            destination_port: None,
            action: AuthzAction::Allow,
        });

        assert_eq!(policy.check(&test_source(), 1), AuthzAction::Allow);
        assert_eq!(policy.check(&test_source(), 65535), AuthzAction::Allow);
    }
}
