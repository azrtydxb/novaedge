use std::time::{Duration, Instant};

/// Mesh mTLS configuration.
#[derive(Debug, Clone)]
pub struct MeshTlsConfig {
    pub ca_cert_pem: String,
    pub workload_cert_pem: String,
    pub workload_key_pem: String,
    pub spiffe_id: String,
    pub cert_lifetime: Duration,
    pub renewal_threshold: f64,
}

impl Default for MeshTlsConfig {
    fn default() -> Self {
        Self {
            ca_cert_pem: String::new(),
            workload_cert_pem: String::new(),
            workload_key_pem: String::new(),
            spiffe_id: String::new(),
            cert_lifetime: Duration::from_secs(86400), // 24h
            renewal_threshold: 0.8,
        }
    }
}

/// Mesh TLS provider managing certificates and connections.
pub struct MeshTlsProvider {
    config: MeshTlsConfig,
    initialized: bool,
    initialized_at: Option<Instant>,
}

impl MeshTlsProvider {
    pub fn new(config: MeshTlsConfig) -> Self {
        Self {
            config,
            initialized: false,
            initialized_at: None,
        }
    }

    pub fn config(&self) -> &MeshTlsConfig {
        &self.config
    }

    pub fn initialize(&mut self) -> anyhow::Result<()> {
        if self.config.ca_cert_pem.is_empty() {
            anyhow::bail!("CA certificate not configured");
        }
        self.initialized = true;
        self.initialized_at = Some(Instant::now());
        tracing::info!(spiffe_id = %self.config.spiffe_id, "mTLS provider initialized");
        Ok(())
    }

    pub fn is_initialized(&self) -> bool {
        self.initialized
    }

    pub fn needs_renewal(&self) -> bool {
        if !self.initialized {
            return false;
        }
        let Some(init_time) = self.initialized_at else {
            return false;
        };
        let elapsed = init_time.elapsed();
        let threshold = self
            .config
            .cert_lifetime
            .mul_f64(self.config.renewal_threshold);
        elapsed >= threshold
    }

    pub fn update_certificates(&mut self, cert_pem: String, key_pem: String) {
        self.config.workload_cert_pem = cert_pem;
        self.config.workload_key_pem = key_pem;
        self.initialized_at = Some(Instant::now());
        tracing::info!("Mesh certificates updated");
    }

    pub fn spiffe_id(&self) -> &str {
        &self.config.spiffe_id
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_default_config() {
        let config = MeshTlsConfig::default();
        assert!(config.ca_cert_pem.is_empty());
        assert!(config.workload_cert_pem.is_empty());
        assert!(config.workload_key_pem.is_empty());
        assert!(config.spiffe_id.is_empty());
        assert_eq!(config.cert_lifetime, Duration::from_secs(86400));
        assert!((config.renewal_threshold - 0.8).abs() < f64::EPSILON);
    }

    #[test]
    fn test_initialize_without_ca_cert_fails() {
        let config = MeshTlsConfig::default();
        let mut provider = MeshTlsProvider::new(config);
        assert!(!provider.is_initialized());

        let result = provider.initialize();
        assert!(result.is_err());
        assert!(!provider.is_initialized());
    }

    #[test]
    fn test_initialize_with_ca_cert_succeeds() {
        let config = MeshTlsConfig {
            ca_cert_pem: "-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----".into(),
            spiffe_id: "spiffe://example.com/ns/default/sa/test".into(),
            ..MeshTlsConfig::default()
        };
        let mut provider = MeshTlsProvider::new(config);
        provider.initialize().unwrap();
        assert!(provider.is_initialized());
    }

    #[test]
    fn test_update_certificates() {
        let config = MeshTlsConfig::default();
        let mut provider = MeshTlsProvider::new(config);

        provider.update_certificates("new-cert".into(), "new-key".into());
        assert_eq!(provider.config().workload_cert_pem, "new-cert");
        assert_eq!(provider.config().workload_key_pem, "new-key");
    }

    #[test]
    fn test_needs_renewal_not_initialized() {
        let config = MeshTlsConfig::default();
        let provider = MeshTlsProvider::new(config);
        // Not initialized — never needs renewal.
        assert!(!provider.needs_renewal());
    }

    #[test]
    fn test_needs_renewal_after_threshold() {
        let config = MeshTlsConfig {
            ca_cert_pem: "-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----".into(),
            spiffe_id: "spiffe://test/ns/default/sa/test".into(),
            // Use a very short lifetime so the threshold is exceeded immediately.
            cert_lifetime: Duration::from_millis(1),
            renewal_threshold: 0.8,
            ..MeshTlsConfig::default()
        };
        let mut provider = MeshTlsProvider::new(config);
        provider.initialize().unwrap();
        // Sleep briefly to exceed 0.8ms threshold.
        std::thread::sleep(Duration::from_millis(5));
        assert!(provider.needs_renewal());
    }

    #[test]
    fn test_needs_renewal_fresh_cert() {
        let config = MeshTlsConfig {
            ca_cert_pem: "-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----".into(),
            spiffe_id: "spiffe://test/ns/default/sa/test".into(),
            cert_lifetime: Duration::from_secs(86400),
            renewal_threshold: 0.8,
            ..MeshTlsConfig::default()
        };
        let mut provider = MeshTlsProvider::new(config);
        provider.initialize().unwrap();
        // Just initialized — should not need renewal yet.
        assert!(!provider.needs_renewal());
    }

    #[test]
    fn test_spiffe_id_accessor() {
        let config = MeshTlsConfig {
            spiffe_id: "spiffe://cluster.local/ns/prod/sa/web".into(),
            ..MeshTlsConfig::default()
        };
        let provider = MeshTlsProvider::new(config);
        assert_eq!(
            provider.spiffe_id(),
            "spiffe://cluster.local/ns/prod/sa/web"
        );
    }
}
