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

package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"sync"

	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"go.uber.org/zap"
)
var (
	errSPIFFEWorkloadAPIAddressMustNotBeEmpty = errors.New("SPIFFE workload API address must not be empty")
	errSPIFFETrustDomainMustNotBeEmpty = errors.New("SPIFFE trust domain must not be empty")
	errSPIFFEProviderNotStarted = errors.New("SPIFFE provider not started")
	errNoPeerCertificatePresented = errors.New("no peer certificate presented")
	errSPIFFEID = errors.New("SPIFFE ID")
	errPeerCertificateDoesNotContainAValidSPIFFE = errors.New("peer certificate does not contain a valid SPIFFE ID URI SAN")
)


// SPIFFEConfig holds configuration for the SPIFFE workload identity provider.
type SPIFFEConfig struct {
	// WorkloadAPIAddr is the address of the SPIFFE Workload API socket
	// (e.g., "unix:///run/spire/sockets/agent.sock").
	WorkloadAPIAddr string

	// TrustDomain is the SPIFFE trust domain (e.g., "example.org").
	TrustDomain string

	// AllowedSPIFFEIDs is the list of SPIFFE IDs that are allowed to
	// connect as peers. An empty list means all IDs within the trust
	// domain are accepted.
	AllowedSPIFFEIDs []string
}

// Validate checks that SPIFFEConfig has all required fields.
func (c *SPIFFEConfig) Validate() error {
	if c.WorkloadAPIAddr == "" {
		return errSPIFFEWorkloadAPIAddressMustNotBeEmpty
	}
	if c.TrustDomain == "" {
		return errSPIFFETrustDomainMustNotBeEmpty
	}
	if _, err := spiffeid.TrustDomainFromString(c.TrustDomain); err != nil {
		return fmt.Errorf("invalid SPIFFE trust domain %q: %w", c.TrustDomain, err)
	}
	for _, id := range c.AllowedSPIFFEIDs {
		if _, err := spiffeid.FromString(id); err != nil {
			return fmt.Errorf("invalid allowed SPIFFE ID %q: %w", id, err)
		}
	}
	return nil
}

// X509SVIDSource is an interface for obtaining X.509 SVIDs and trust bundles.
// This abstraction allows mocking the SPIFFE workload API in tests.
type X509SVIDSource interface {
	// GetX509SVID returns the current X.509 SVID.
	GetX509SVID() (*x509svid.SVID, error)

	// GetX509BundleForTrustDomain returns the trust bundle for the given trust domain.
	GetX509BundleForTrustDomain(td spiffeid.TrustDomain) (*x509bundle.Bundle, error)

	// Close releases resources held by the source.
	Close() error
}

// X509SourceFactory creates an X509SVIDSource. This is extracted as a
// function type so tests can inject a mock source without connecting
// to a real SPIRE agent.
type X509SourceFactory func(ctx context.Context, addr string) (X509SVIDSource, error)

// DefaultX509SourceFactory creates a real X509Source connected to the
// SPIFFE Workload API at the given address.
func DefaultX509SourceFactory(ctx context.Context, addr string) (X509SVIDSource, error) {
	source, err := workloadapi.NewX509Source(
		ctx,
		workloadapi.WithClientOptions(workloadapi.WithAddr(addr)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create SPIFFE X509Source: %w", err)
	}
	return source, nil
}

// SPIFFEProvider manages SPIFFE workload identity, including X.509 SVID
// retrieval, automatic renewal, and TLS configuration for mTLS.
type SPIFFEProvider struct {
	config      SPIFFEConfig
	logger      *zap.Logger
	trustDomain spiffeid.TrustDomain
	allowedIDs  map[string]struct{}
	source      X509SVIDSource
	factory     X509SourceFactory
	mu          sync.RWMutex
	cancel      context.CancelFunc
	stopped     chan struct{}
}

// NewSPIFFEProvider creates a new SPIFFEProvider with the given config.
// It validates the configuration but does not connect to the Workload API
// until Start() is called.
func NewSPIFFEProvider(config SPIFFEConfig, logger *zap.Logger) (*SPIFFEProvider, error) {
	return newSPIFFEProviderWithFactory(config, logger, DefaultX509SourceFactory)
}

// newSPIFFEProviderWithFactory creates a SPIFFEProvider with a custom source
// factory, enabling test injection.
func newSPIFFEProviderWithFactory(config SPIFFEConfig, logger *zap.Logger, factory X509SourceFactory) (*SPIFFEProvider, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid SPIFFE config: %w", err)
	}

	td, _ := spiffeid.TrustDomainFromString(config.TrustDomain)

	allowedIDs := make(map[string]struct{}, len(config.AllowedSPIFFEIDs))
	for _, id := range config.AllowedSPIFFEIDs {
		allowedIDs[id] = struct{}{}
	}

	return &SPIFFEProvider{
		config:      config,
		logger:      logger,
		trustDomain: td,
		allowedIDs:  allowedIDs,
		factory:     factory,
		stopped:     make(chan struct{}),
	}, nil
}

// Start connects to the SPIFFE Workload API and begins obtaining SVIDs.
// The context controls the lifetime of the underlying X509Source.
func (p *SPIFFEProvider) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	p.mu.Lock()
	p.cancel = cancel
	p.mu.Unlock()

	source, err := p.factory(ctx, p.config.WorkloadAPIAddr)
	if err != nil {
		cancel()
		return fmt.Errorf("failed to connect to SPIFFE Workload API at %s: %w",
			p.config.WorkloadAPIAddr, err)
	}

	p.mu.Lock()
	p.source = source
	p.mu.Unlock()

	// Verify initial SVID is available
	svid, err := source.GetX509SVID()
	if err != nil {
		if closeErr := source.Close(); closeErr != nil {
			p.logger.Error("failed to close SPIFFE source during cleanup", zap.Error(closeErr))
		}
		cancel()
		return fmt.Errorf("failed to obtain initial X509-SVID: %w", err)
	}

	p.logger.Info("SPIFFE provider started",
		zap.String("spiffe_id", svid.ID.String()),
		zap.String("trust_domain", p.trustDomain.String()),
		zap.Int("allowed_ids", len(p.allowedIDs)),
		zap.String("workload_api", p.config.WorkloadAPIAddr),
	)

	return nil
}

// Stop disconnects from the SPIFFE Workload API and releases resources.
func (p *SPIFFEProvider) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}

	if p.source != nil {
		if err := p.source.Close(); err != nil {
			p.logger.Error("error closing SPIFFE X509Source", zap.Error(err))
		}
		p.source = nil
	}

	p.logger.Info("SPIFFE provider stopped")
}

// GetTLSConfig returns a *tls.Config configured for mTLS using the current
// X.509 SVID and trust bundle. The returned config uses dynamic certificate
// callbacks so that certificate rotation is handled transparently.
func (p *SPIFFEProvider) GetTLSConfig() (*tls.Config, error) {
	p.mu.RLock()
	source := p.source
	p.mu.RUnlock()

	if source == nil {
		return nil, errSPIFFEProviderNotStarted
	}

	// Verify we can obtain a current SVID
	if _, err := source.GetX509SVID(); err != nil {
		return nil, fmt.Errorf("failed to get current SVID for TLS config: %w", err)
	}

	// Build root CA pool from trust bundle
	rootCAs, err := p.getTrustBundleCertPool()
	if err != nil {
		return nil, fmt.Errorf("failed to get trust bundle: %w", err)
	}

	tlsConfig := &tls.Config{
		GetClientCertificate:  p.GetClientCertificate(),
		RootCAs:               rootCAs,
		ClientCAs:             rootCAs,
		ClientAuth:            tls.RequireAndVerifyClientCert,
		MinVersion:            tls.VersionTLS12,
		VerifyPeerCertificate: p.verifyPeerCertificate,
	}

	return tlsConfig, nil
}

// GetClientCertificate returns a callback function suitable for use in
// tls.Config.GetClientCertificate. The callback dynamically retrieves
// the current X.509 SVID so that certificate rotation is seamless.
func (p *SPIFFEProvider) GetClientCertificate() func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
	return func(_ *tls.CertificateRequestInfo) (*tls.Certificate, error) {
		p.mu.RLock()
		source := p.source
		p.mu.RUnlock()

		if source == nil {
			return nil, errSPIFFEProviderNotStarted
		}

		svid, err := source.GetX509SVID()
		if err != nil {
			return nil, fmt.Errorf("failed to get X509-SVID for client certificate: %w", err)
		}

		certChain := make([][]byte, 0, len(svid.Certificates))
		for _, cert := range svid.Certificates {
			certChain = append(certChain, cert.Raw)
		}

		return &tls.Certificate{
			Certificate: certChain,
			PrivateKey:  svid.PrivateKey,
			Leaf:        svid.Certificates[0],
		}, nil
	}
}

// GetTrustBundle returns the X.509 trust bundle for the configured trust domain.
func (p *SPIFFEProvider) GetTrustBundle() (*x509bundle.Bundle, error) {
	p.mu.RLock()
	source := p.source
	p.mu.RUnlock()

	if source == nil {
		return nil, errSPIFFEProviderNotStarted
	}

	bundle, err := source.GetX509BundleForTrustDomain(p.trustDomain)
	if err != nil {
		return nil, fmt.Errorf("failed to get trust bundle for domain %s: %w",
			p.trustDomain.String(), err)
	}

	return bundle, nil
}

// IsAllowedSPIFFEID checks whether the given SPIFFE ID is permitted.
// If no allowed IDs are configured, all IDs within the trust domain are accepted.
func (p *SPIFFEProvider) IsAllowedSPIFFEID(id spiffeid.ID) bool {
	// Check trust domain membership
	if !id.MemberOf(p.trustDomain) {
		return false
	}

	// If no explicit allow list, accept all IDs in the trust domain
	if len(p.allowedIDs) == 0 {
		return true
	}

	_, allowed := p.allowedIDs[id.String()]
	return allowed
}

// getTrustBundleCertPool returns a *x509.CertPool constructed from the
// trust bundle for the configured trust domain.
func (p *SPIFFEProvider) getTrustBundleCertPool() (*x509.CertPool, error) {
	p.mu.RLock()
	source := p.source
	p.mu.RUnlock()

	if source == nil {
		return nil, errSPIFFEProviderNotStarted
	}

	bundle, err := source.GetX509BundleForTrustDomain(p.trustDomain)
	if err != nil {
		return nil, fmt.Errorf("failed to get trust bundle: %w", err)
	}

	pool := x509.NewCertPool()
	for _, authority := range bundle.X509Authorities() {
		pool.AddCert(authority)
	}

	return pool, nil
}

// verifyPeerCertificate is a TLS callback that checks whether the peer's
// SPIFFE ID (extracted from the certificate URI SAN) is in the allowed list.
func (p *SPIFFEProvider) verifyPeerCertificate(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	if len(rawCerts) == 0 {
		return errNoPeerCertificatePresented
	}

	leaf, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return fmt.Errorf("failed to parse peer certificate: %w", err)
	}

	// Extract SPIFFE ID from URI SANs
	for _, uri := range leaf.URIs {
		peerID, parseErr := spiffeIDFromURI(uri)
		if parseErr != nil {
			continue
		}

		if p.IsAllowedSPIFFEID(peerID) {
			p.logger.Debug("peer SPIFFE ID accepted",
				zap.String("peer_id", peerID.String()),
			)
			return nil
		}

		p.logger.Warn("peer SPIFFE ID rejected",
			zap.String("peer_id", peerID.String()),
			zap.String("trust_domain", p.trustDomain.String()),
		)
		return fmt.Errorf("%w: %s is not allowed", errSPIFFEID, peerID.String())
	}

	return errPeerCertificateDoesNotContainAValidSPIFFE
}

// spiffeIDFromURI attempts to parse a URL as a SPIFFE ID.
func spiffeIDFromURI(uri *url.URL) (spiffeid.ID, error) {
	return spiffeid.FromURI(uri)
}
