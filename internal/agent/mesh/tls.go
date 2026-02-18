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

package mesh

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"sync"

	"go.uber.org/zap"
)

// TLSProvider manages TLS certificates for mesh connections.
// It supports certificate rotation by using dynamic TLS callbacks
// that read the current certificate under a read lock.
type TLSProvider struct {
	mu          sync.RWMutex
	logger      *zap.Logger
	cert        *tls.Certificate
	caCertPool  *x509.CertPool
	trustDomain string
	spiffeID    string
	fedCfg      *FederationConfig
}

// NewTLSProvider creates a new mesh TLS provider for the given trust domain.
// The fedCfg parameter may be nil when federation is not active.
func NewTLSProvider(logger *zap.Logger, trustDomain string, fedCfg *FederationConfig) *TLSProvider {
	return &TLSProvider{
		logger:      logger,
		trustDomain: trustDomain,
		fedCfg:      fedCfg,
	}
}

// UpdateCertificate updates the workload certificate and CA trust bundle.
// Called when a new MeshTLSConfig is received from the controller.
// The certPEM and keyPEM must contain a valid X.509 certificate and
// private key in PEM format. The caCertPEM must contain one or more
// CA certificates that form the trust bundle.
func (p *TLSProvider) UpdateCertificate(certPEM, keyPEM, caCertPEM []byte, spiffeID string) error {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("failed to parse certificate/key pair: %w", err)
	}

	// Parse the leaf certificate so it's available for inspection.
	if len(cert.Certificate) > 0 {
		leaf, parseErr := x509.ParseCertificate(cert.Certificate[0])
		if parseErr != nil {
			return fmt.Errorf("failed to parse leaf certificate: %w", parseErr)
		}
		cert.Leaf = leaf
	}

	caPool := x509.NewCertPool()
	rest := caCertPEM
	var added int
	for len(rest) > 0 {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		caCert, parseErr := x509.ParseCertificate(block.Bytes)
		if parseErr != nil {
			return fmt.Errorf("failed to parse CA certificate: %w", parseErr)
		}
		caPool.AddCert(caCert)
		added++
	}

	if added == 0 {
		return errors.New("no valid CA certificates found in CA bundle")
	}

	p.mu.Lock()
	p.cert = &cert
	p.caCertPool = caPool
	p.spiffeID = spiffeID
	p.mu.Unlock()

	p.logger.Info("mesh TLS certificate updated",
		zap.String("spiffe_id", spiffeID),
		zap.String("trust_domain", p.trustDomain),
		zap.Int("ca_certs", added),
	)

	return nil
}

// ServerTLSConfig returns a TLS config for the mesh tunnel server.
// It requires client certificates (mTLS) and enforces TLS 1.3 minimum.
// The config uses dynamic callbacks for transparent certificate rotation.
func (p *TLSProvider) ServerTLSConfig() *tls.Config {
	return &tls.Config{
		GetCertificate: func(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
			p.mu.RLock()
			defer p.mu.RUnlock()
			if p.cert == nil {
				return nil, errors.New("no mesh TLS certificate loaded")
			}
			return p.cert, nil
		},
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  p.getClientCAs(),
		GetConfigForClient: func(_ *tls.ClientHelloInfo) (*tls.Config, error) {
			// Return updated ClientCAs on each connection to support CA rotation.
			p.mu.RLock()
			pool := p.caCertPool
			p.mu.RUnlock()
			return &tls.Config{
				GetCertificate: func(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
					p.mu.RLock()
					defer p.mu.RUnlock()
					if p.cert == nil {
						return nil, errors.New("no mesh TLS certificate loaded")
					}
					return p.cert, nil
				},
				ClientAuth: tls.RequireAndVerifyClientCert,
				ClientCAs:  pool,
				MinVersion: tls.VersionTLS13,
				NextProtos: []string{"h2"},
			}, nil
		},
		MinVersion: tls.VersionTLS13,
		NextProtos: []string{"h2"},
	}
}

// ClientTLSConfig returns a TLS config for dialing peer mesh tunnel servers.
// It presents a client certificate and verifies the server certificate
// against the CA trust bundle. TLS 1.3 minimum is enforced.
func (p *TLSProvider) ClientTLSConfig() *tls.Config {
	return &tls.Config{
		GetClientCertificate: func(_ *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			p.mu.RLock()
			defer p.mu.RUnlock()
			if p.cert == nil {
				return nil, errors.New("no mesh TLS certificate loaded")
			}
			return p.cert, nil
		},
		RootCAs:    p.getRootCAs(),
		MinVersion: tls.VersionTLS13,
		NextProtos: []string{"h2"},
	}
}

// HasCertificate returns true if a valid certificate is loaded.
func (p *TLSProvider) HasCertificate() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cert != nil && p.caCertPool != nil
}

// getClientCAs returns the current CA cert pool for client authentication.
func (p *TLSProvider) getClientCAs() *x509.CertPool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.caCertPool
}

// getRootCAs returns the current CA cert pool for server verification.
func (p *TLSProvider) getRootCAs() *x509.CertPool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.caCertPool
}

// PeerSPIFFEID extracts the SPIFFE ID from a peer's TLS certificate.
// It looks for a URI SAN with the "spiffe" scheme in the first peer
// certificate. Returns an empty string if no SPIFFE ID is found.
func PeerSPIFFEID(state *tls.ConnectionState) string {
	if state == nil || len(state.PeerCertificates) == 0 {
		return ""
	}

	for _, uri := range state.PeerCertificates[0].URIs {
		if uri.Scheme == "spiffe" {
			return uri.String()
		}
	}

	return ""
}

// ValidatePeerSPIFFEID checks whether a peer's SPIFFE ID is acceptable.
// When federation is active, cross-cluster SPIFFE IDs are accepted if the
// cluster name is in the allowed list. When federation is not active, only
// SPIFFE IDs with the local trust domain are accepted.
func (p *TLSProvider) ValidatePeerSPIFFEID(spiffeID string) bool {
	if spiffeID == "" {
		return false
	}

	identity := ParseSPIFFEID(spiffeID)
	if identity.SpiffeID == "" {
		return false
	}

	// Cross-cluster federated identity
	if identity.IsFederated() {
		if p.fedCfg == nil || !p.fedCfg.IsActive() {
			// Federation not active — reject cross-cluster identities
			return false
		}
		// Accept if the federation ID matches ours and cluster is allowed
		return identity.FederationID == p.fedCfg.FederationID &&
			p.fedCfg.IsClusterAllowed(identity.ClusterName)
	}

	// Local identity — trust domain must match
	return identity.TrustDomain == p.trustDomain
}
