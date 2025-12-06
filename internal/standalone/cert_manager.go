/*
Copyright 2024 NovaEdge Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package standalone

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/piwi3910/novaedge/internal/acme"
	"go.uber.org/zap"
)

// CertificateManager manages TLS certificates for standalone mode.
// It handles ACME provisioning, self-signed generation, and certificate hot-reload.
type CertificateManager struct {
	config     *Config
	storagePath string
	logger     *zap.Logger

	mu          sync.RWMutex
	certificates map[string]*ManagedCertificate
	acmeClients  map[string]*acme.Client

	renewalTicker *time.Ticker
	stopCh        chan struct{}
}

// ManagedCertificate represents a managed certificate with hot-reload support.
type ManagedCertificate struct {
	Name       string
	Domains    []string
	IssuerType string

	// Atomic certificate for hot-reload
	certificate atomic.Value // *tls.Certificate

	// Certificate metadata
	NotBefore time.Time
	NotAfter  time.Time

	// Source files (for manual certs)
	certFile string
	keyFile  string
	lastMod  time.Time
}

// NewCertificateManager creates a new certificate manager.
func NewCertificateManager(config *Config, storagePath string, logger *zap.Logger) (*CertificateManager, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}
	if storagePath == "" {
		storagePath = "/var/lib/novaedge/certs"
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	// Ensure storage directory exists
	if err := os.MkdirAll(storagePath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create certificate storage directory: %w", err)
	}

	return &CertificateManager{
		config:       config,
		storagePath:  storagePath,
		logger:       logger,
		certificates: make(map[string]*ManagedCertificate),
		acmeClients:  make(map[string]*acme.Client),
		stopCh:       make(chan struct{}),
	}, nil
}

// Initialize loads or provisions all configured certificates.
func (m *CertificateManager) Initialize(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, certConfig := range m.config.Certificates {
		if err := m.initializeCertificate(ctx, &certConfig); err != nil {
			return fmt.Errorf("failed to initialize certificate %s: %w", certConfig.Name, err)
		}
	}

	return nil
}

// initializeCertificate initializes a single certificate based on its issuer type.
func (m *CertificateManager) initializeCertificate(ctx context.Context, certConfig *CertificateConfig) error {
	m.logger.Info("Initializing certificate",
		zap.String("name", certConfig.Name),
		zap.Strings("domains", certConfig.Domains),
		zap.String("issuer", certConfig.Issuer.Type),
	)

	mc := &ManagedCertificate{
		Name:       certConfig.Name,
		Domains:    certConfig.Domains,
		IssuerType: certConfig.Issuer.Type,
	}

	switch certConfig.Issuer.Type {
	case "acme":
		if err := m.initializeACMECertificate(ctx, certConfig, mc); err != nil {
			return err
		}
	case "manual":
		if err := m.initializeManualCertificate(certConfig, mc); err != nil {
			return err
		}
	case "self-signed":
		if err := m.initializeSelfSignedCertificate(certConfig, mc); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported issuer type: %s", certConfig.Issuer.Type)
	}

	m.certificates[certConfig.Name] = mc
	return nil
}

// initializeACMECertificate provisions a certificate via ACME.
func (m *CertificateManager) initializeACMECertificate(ctx context.Context, certConfig *CertificateConfig, mc *ManagedCertificate) error {
	acmeConfig := certConfig.Issuer.ACME

	// Create ACME client configuration
	config := &acme.Config{
		Email:         acmeConfig.Email,
		Server:        acmeConfig.Server,
		KeyType:       acmeConfig.KeyType,
		ChallengeType: acmeConfig.ChallengeType,
		DNSProvider:   acmeConfig.DNSProvider,
		DNSCredentials: acmeConfig.DNSCredentials,
		AcceptTOS:     acmeConfig.AcceptTOS,
		Storage: acme.StorageConfig{
			Type: "file",
			Path: filepath.Join(m.storagePath, "acme"),
		},
	}

	// Create file storage
	storage, err := acme.NewFileStorage(config.Storage.Path, m.logger.Named("storage"))
	if err != nil {
		return fmt.Errorf("failed to create ACME storage: %w", err)
	}

	// Create ACME client
	client, err := acme.NewClient(config, storage, nil, m.logger.Named("acme"))
	if err != nil {
		return fmt.Errorf("failed to create ACME client: %w", err)
	}

	// Initialize ACME client
	if err := client.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize ACME client: %w", err)
	}

	m.acmeClients[certConfig.Name] = client

	// Try to load existing certificate
	primaryDomain := certConfig.Domains[0]
	cert, err := client.GetCertificate(ctx, primaryDomain)
	if err != nil || cert == nil || cert.ShouldRenew(30*24*time.Hour) {
		// Request new certificate
		cert, err = client.ObtainCertificate(ctx, &acme.CertificateRequest{
			Domains: certConfig.Domains,
		})
		if err != nil {
			return fmt.Errorf("failed to obtain certificate: %w", err)
		}
	}

	// Load certificate into TLS
	tlsCert, err := cert.TLSCertificate()
	if err != nil {
		return fmt.Errorf("failed to load TLS certificate: %w", err)
	}

	mc.certificate.Store(tlsCert)
	mc.NotBefore = cert.NotBefore
	mc.NotAfter = cert.NotAfter

	m.logger.Info("ACME certificate loaded",
		zap.String("name", certConfig.Name),
		zap.Time("expires", cert.NotAfter),
	)

	return nil
}

// initializeManualCertificate loads a manually provided certificate.
func (m *CertificateManager) initializeManualCertificate(certConfig *CertificateConfig, mc *ManagedCertificate) error {
	manualConfig := certConfig.Issuer.Manual

	cert, err := tls.LoadX509KeyPair(manualConfig.CertFile, manualConfig.KeyFile)
	if err != nil {
		return fmt.Errorf("failed to load certificate: %w", err)
	}

	mc.certificate.Store(&cert)
	mc.certFile = manualConfig.CertFile
	mc.keyFile = manualConfig.KeyFile

	// Get modification time for hot-reload
	info, err := os.Stat(manualConfig.CertFile)
	if err == nil {
		mc.lastMod = info.ModTime()
	}

	// Parse certificate to get expiry
	if len(cert.Certificate) > 0 {
		if parsed, err := acme.ParseCertificatePEM(cert.Certificate[0]); err == nil {
			mc.NotBefore = parsed.NotBefore
			mc.NotAfter = parsed.NotAfter
		}
	}

	m.logger.Info("Manual certificate loaded",
		zap.String("name", certConfig.Name),
		zap.String("certFile", manualConfig.CertFile),
	)

	return nil
}

// initializeSelfSignedCertificate generates a self-signed certificate.
func (m *CertificateManager) initializeSelfSignedCertificate(certConfig *CertificateConfig, mc *ManagedCertificate) error {
	selfSignedConfig := certConfig.Issuer.SelfSigned

	// Parse validity duration
	validity := 365 * 24 * time.Hour // Default 1 year
	if selfSignedConfig != nil && selfSignedConfig.Validity != "" {
		parsed, err := time.ParseDuration(selfSignedConfig.Validity)
		if err == nil {
			validity = parsed
		}
	}

	// Get organization
	organization := "NovaEdge"
	if selfSignedConfig != nil && selfSignedConfig.Organization != "" {
		organization = selfSignedConfig.Organization
	}

	// Get key type
	keyType := acme.KeyTypeEC256
	if selfSignedConfig != nil && selfSignedConfig.KeyType != "" {
		keyType = selfSignedConfig.KeyType
	}

	// Generate self-signed certificate
	cert, err := acme.GenerateSelfSigned(&acme.SelfSignedConfig{
		Domains:      certConfig.Domains,
		Organization: organization,
		Validity:     validity,
		KeyType:      keyType,
	})
	if err != nil {
		return fmt.Errorf("failed to generate self-signed certificate: %w", err)
	}

	// Load into TLS certificate
	tlsCert, err := cert.TLSCertificate()
	if err != nil {
		return fmt.Errorf("failed to load self-signed certificate: %w", err)
	}

	mc.certificate.Store(tlsCert)
	mc.NotBefore = cert.NotBefore
	mc.NotAfter = cert.NotAfter

	// Optionally save to disk
	certPath := filepath.Join(m.storagePath, certConfig.Name+".crt")
	keyPath := filepath.Join(m.storagePath, certConfig.Name+".key")

	if err := os.WriteFile(certPath, cert.CertificatePEM, 0644); err != nil {
		m.logger.Warn("Failed to save self-signed certificate", zap.Error(err))
	}
	if err := os.WriteFile(keyPath, cert.PrivateKeyPEM, 0600); err != nil {
		m.logger.Warn("Failed to save self-signed key", zap.Error(err))
	}

	m.logger.Info("Self-signed certificate generated",
		zap.String("name", certConfig.Name),
		zap.Time("expires", mc.NotAfter),
	)

	return nil
}

// GetCertificate returns the TLS certificate for a given name.
func (m *CertificateManager) GetCertificate(name string) (*tls.Certificate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	mc, ok := m.certificates[name]
	if !ok {
		return nil, fmt.Errorf("certificate not found: %s", name)
	}

	cert := mc.certificate.Load()
	if cert == nil {
		return nil, fmt.Errorf("certificate not loaded: %s", name)
	}

	return cert.(*tls.Certificate), nil
}

// GetCertificateFunc returns a function suitable for tls.Config.GetCertificate.
// It performs SNI-based certificate selection.
func (m *CertificateManager) GetCertificateFunc() func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		m.mu.RLock()
		defer m.mu.RUnlock()

		serverName := hello.ServerName

		// Find matching certificate
		for _, mc := range m.certificates {
			for _, domain := range mc.Domains {
				if matchDomain(domain, serverName) {
					cert := mc.certificate.Load()
					if cert != nil {
						return cert.(*tls.Certificate), nil
					}
				}
			}
		}

		// Return first available certificate as fallback
		for _, mc := range m.certificates {
			cert := mc.certificate.Load()
			if cert != nil {
				return cert.(*tls.Certificate), nil
			}
		}

		return nil, fmt.Errorf("no certificate available for %s", serverName)
	}
}

// Start begins background certificate renewal and hot-reload.
func (m *CertificateManager) Start(ctx context.Context) {
	m.renewalTicker = time.NewTicker(1 * time.Hour)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-m.stopCh:
				return
			case <-m.renewalTicker.C:
				m.checkRenewals(ctx)
				m.checkHotReload()
			}
		}
	}()

	m.logger.Info("Certificate manager started")
}

// Stop stops the certificate manager.
func (m *CertificateManager) Stop() {
	close(m.stopCh)
	if m.renewalTicker != nil {
		m.renewalTicker.Stop()
	}
	m.logger.Info("Certificate manager stopped")
}

// checkRenewals checks for certificates needing renewal.
func (m *CertificateManager) checkRenewals(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	renewThreshold := 30 * 24 * time.Hour // 30 days

	for name, mc := range m.certificates {
		if mc.IssuerType != "acme" {
			continue
		}

		if time.Until(mc.NotAfter) < renewThreshold {
			m.logger.Info("Certificate needs renewal",
				zap.String("name", name),
				zap.Time("expires", mc.NotAfter),
			)

			client, ok := m.acmeClients[name]
			if !ok {
				m.logger.Error("ACME client not found for certificate", zap.String("name", name))
				continue
			}

			// Find the certificate config
			var certConfig *CertificateConfig
			for i := range m.config.Certificates {
				if m.config.Certificates[i].Name == name {
					certConfig = &m.config.Certificates[i]
					break
				}
			}

			if certConfig == nil {
				m.logger.Error("Certificate config not found", zap.String("name", name))
				continue
			}

			// Renew certificate
			cert, err := client.ObtainCertificate(ctx, &acme.CertificateRequest{
				Domains: certConfig.Domains,
			})
			if err != nil {
				m.logger.Error("Failed to renew certificate",
					zap.String("name", name),
					zap.Error(err),
				)
				continue
			}

			// Load new certificate
			tlsCert, err := cert.TLSCertificate()
			if err != nil {
				m.logger.Error("Failed to load renewed certificate",
					zap.String("name", name),
					zap.Error(err),
				)
				continue
			}

			mc.certificate.Store(tlsCert)
			mc.NotBefore = cert.NotBefore
			mc.NotAfter = cert.NotAfter

			m.logger.Info("Certificate renewed",
				zap.String("name", name),
				zap.Time("newExpiry", cert.NotAfter),
			)
		}
	}
}

// checkHotReload checks for manual certificate file changes.
func (m *CertificateManager) checkHotReload() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, mc := range m.certificates {
		if mc.IssuerType != "manual" || mc.certFile == "" {
			continue
		}

		info, err := os.Stat(mc.certFile)
		if err != nil {
			continue
		}

		if info.ModTime().After(mc.lastMod) {
			m.logger.Info("Certificate file changed, reloading",
				zap.String("name", name),
				zap.String("file", mc.certFile),
			)

			cert, err := tls.LoadX509KeyPair(mc.certFile, mc.keyFile)
			if err != nil {
				m.logger.Error("Failed to reload certificate",
					zap.String("name", name),
					zap.Error(err),
				)
				continue
			}

			mc.certificate.Store(&cert)
			mc.lastMod = info.ModTime()

			m.logger.Info("Certificate reloaded",
				zap.String("name", name),
			)
		}
	}
}

// GetCertificateInfo returns information about all managed certificates.
func (m *CertificateManager) GetCertificateInfo() []CertificateInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var infos []CertificateInfo
	for name, mc := range m.certificates {
		infos = append(infos, CertificateInfo{
			Name:       name,
			Domains:    mc.Domains,
			IssuerType: mc.IssuerType,
			NotBefore:  mc.NotBefore,
			NotAfter:   mc.NotAfter,
			ExpiresIn:  time.Until(mc.NotAfter),
		})
	}
	return infos
}

// CertificateInfo contains certificate status information.
type CertificateInfo struct {
	Name       string
	Domains    []string
	IssuerType string
	NotBefore  time.Time
	NotAfter   time.Time
	ExpiresIn  time.Duration
}

// matchDomain checks if a domain pattern matches a server name.
func matchDomain(pattern, serverName string) bool {
	if pattern == serverName {
		return true
	}

	// Wildcard matching
	if len(pattern) > 2 && pattern[0] == '*' && pattern[1] == '.' {
		suffix := pattern[1:]
		if len(serverName) > len(suffix) {
			// Check if serverName ends with the suffix
			if serverName[len(serverName)-len(suffix):] == suffix {
				// Check there's exactly one label before the suffix
				prefix := serverName[:len(serverName)-len(suffix)]
				for _, c := range prefix {
					if c == '.' {
						return false
					}
				}
				return true
			}
		}
	}

	return false
}
