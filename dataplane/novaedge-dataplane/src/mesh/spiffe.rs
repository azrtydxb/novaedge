/// SPIFFE ID components.
#[derive(Debug, Clone, PartialEq)]
pub struct SpiffeId {
    pub trust_domain: String,
    pub path: String,
}

impl SpiffeId {
    /// Parse a SPIFFE ID string.
    /// Format: spiffe://trust-domain/path
    pub fn parse(id: &str) -> Result<Self, String> {
        let id = id
            .strip_prefix("spiffe://")
            .ok_or_else(|| "SPIFFE ID must start with spiffe://".to_string())?;
        let (domain, path) = id
            .split_once('/')
            .ok_or_else(|| "SPIFFE ID must have a path component".to_string())?;
        if domain.is_empty() {
            return Err("trust domain cannot be empty".into());
        }
        Ok(Self {
            trust_domain: domain.to_string(),
            path: format!("/{path}"),
        })
    }

    /// Build a workload SPIFFE ID.
    pub fn workload(trust_domain: &str, namespace: &str, service_account: &str) -> Self {
        Self {
            trust_domain: trust_domain.to_string(),
            path: format!("/ns/{namespace}/sa/{service_account}"),
        }
    }

    /// Build an agent SPIFFE ID.
    pub fn agent(trust_domain: &str, node_name: &str) -> Self {
        Self {
            trust_domain: trust_domain.to_string(),
            path: format!("/agent/{node_name}"),
        }
    }

    pub fn to_uri(&self) -> String {
        format!("spiffe://{}{}", self.trust_domain, self.path)
    }

    pub fn matches_pattern(&self, pattern: &str) -> bool {
        if pattern == "*" {
            return true;
        }
        let pattern_id = match SpiffeId::parse(pattern) {
            Ok(id) => id,
            Err(_) => return false,
        };
        if self.trust_domain != pattern_id.trust_domain {
            return false;
        }
        if pattern_id.path.ends_with("/*") {
            let prefix = &pattern_id.path[..pattern_id.path.len() - 1];
            self.path.starts_with(prefix)
        } else {
            self.path == pattern_id.path
        }
    }
}

impl std::fmt::Display for SpiffeId {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "spiffe://{}{}", self.trust_domain, self.path)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_valid() {
        let id = SpiffeId::parse("spiffe://cluster.local/ns/default/sa/web").unwrap();
        assert_eq!(id.trust_domain, "cluster.local");
        assert_eq!(id.path, "/ns/default/sa/web");
    }

    #[test]
    fn test_parse_missing_prefix() {
        let result = SpiffeId::parse("https://cluster.local/ns/default/sa/web");
        assert!(result.is_err());
        assert!(result.unwrap_err().contains("must start with spiffe://"));
    }

    #[test]
    fn test_parse_missing_path() {
        let result = SpiffeId::parse("spiffe://cluster.local");
        assert!(result.is_err());
        assert!(result.unwrap_err().contains("must have a path component"));
    }

    #[test]
    fn test_parse_empty_domain() {
        let result = SpiffeId::parse("spiffe:///some/path");
        assert!(result.is_err());
        assert!(result.unwrap_err().contains("trust domain cannot be empty"));
    }

    #[test]
    fn test_workload() {
        let id = SpiffeId::workload("cluster.local", "prod", "frontend");
        assert_eq!(id.trust_domain, "cluster.local");
        assert_eq!(id.path, "/ns/prod/sa/frontend");
        assert_eq!(id.to_uri(), "spiffe://cluster.local/ns/prod/sa/frontend");
    }

    #[test]
    fn test_agent() {
        let id = SpiffeId::agent("cluster.local", "node-1");
        assert_eq!(id.trust_domain, "cluster.local");
        assert_eq!(id.path, "/agent/node-1");
    }

    #[test]
    fn test_to_uri() {
        let id = SpiffeId::parse("spiffe://example.com/workload").unwrap();
        assert_eq!(id.to_uri(), "spiffe://example.com/workload");
    }

    #[test]
    fn test_display() {
        let id = SpiffeId::workload("cluster.local", "default", "api");
        assert_eq!(format!("{id}"), "spiffe://cluster.local/ns/default/sa/api");
    }

    #[test]
    fn test_matches_wildcard() {
        let id = SpiffeId::workload("cluster.local", "default", "web");
        assert!(id.matches_pattern("*"));
    }

    #[test]
    fn test_matches_exact() {
        let id = SpiffeId::workload("cluster.local", "default", "web");
        assert!(id.matches_pattern("spiffe://cluster.local/ns/default/sa/web"));
        assert!(!id.matches_pattern("spiffe://cluster.local/ns/default/sa/api"));
    }

    #[test]
    fn test_matches_prefix_wildcard() {
        let id = SpiffeId::workload("cluster.local", "prod", "web");
        assert!(id.matches_pattern("spiffe://cluster.local/ns/prod/*"));
        assert!(!id.matches_pattern("spiffe://cluster.local/ns/staging/*"));
    }

    #[test]
    fn test_matches_different_domain() {
        let id = SpiffeId::workload("cluster.local", "default", "web");
        assert!(!id.matches_pattern("spiffe://other.domain/ns/default/sa/web"));
    }

    #[test]
    fn test_matches_invalid_pattern() {
        let id = SpiffeId::workload("cluster.local", "default", "web");
        assert!(!id.matches_pattern("not-a-spiffe-id"));
    }
}
