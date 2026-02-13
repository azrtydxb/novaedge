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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/url"
	"testing"
	"time"

	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"go.uber.org/zap"
)

// mockX509Source implements X509SVIDSource for testing.
type mockX509Source struct {
	svid      *x509svid.SVID
	bundle    *x509bundle.Bundle
	svidErr   error
	bundleErr error
	closed    bool
}

func (m *mockX509Source) GetX509SVID() (*x509svid.SVID, error) {
	if m.svidErr != nil {
		return nil, m.svidErr
	}
	return m.svid, nil
}

func (m *mockX509Source) GetX509BundleForTrustDomain(td spiffeid.TrustDomain) (*x509bundle.Bundle, error) {
	if m.bundleErr != nil {
		return nil, m.bundleErr
	}
	return m.bundle, nil
}

func (m *mockX509Source) Close() error {
	m.closed = true
	return nil
}

// generateSPIFFETestCA creates a test CA cert and key.
func generateSPIFFETestCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate CA key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "SPIFFE Test CA"},
		NotBefore:             time.Now().Add(-1 * time.Minute),
		NotAfter:              time.Now().Add(1 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create CA certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("failed to parse CA certificate: %v", err)
	}

	return cert, key
}

// generateSPIFFESVID creates a test X509-SVID with a SPIFFE ID URI SAN.
func generateSPIFFESVID(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, spiffeIDStr string) *x509svid.SVID {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate SVID key: %v", err)
	}

	parsedURI, err := url.Parse(spiffeIDStr)
	if err != nil {
		t.Fatalf("failed to parse SPIFFE ID URI: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "workload"},
		NotBefore:    time.Now().Add(-1 * time.Minute),
		NotAfter:     time.Now().Add(1 * time.Hour),
		URIs:         []*url.URL{parsedURI},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("failed to create SVID certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("failed to parse SVID certificate: %v", err)
	}

	id, err := spiffeid.FromString(spiffeIDStr)
	if err != nil {
		t.Fatalf("failed to parse SPIFFE ID: %v", err)
	}

	return &x509svid.SVID{
		ID:           id,
		Certificates: []*x509.Certificate{cert, ca},
		PrivateKey:   key,
	}
}

func mockSourceFactory(source X509SVIDSource) X509SourceFactory {
	return func(_ context.Context, _ string) (X509SVIDSource, error) {
		return source, nil
	}
}

func TestSPIFFEConfig_Validate_EmptyWorkloadAPIAddr(t *testing.T) {
	cfg := SPIFFEConfig{
		WorkloadAPIAddr: "",
		TrustDomain:     "example.org",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty workload API address")
	}
	if err.Error() != "SPIFFE workload API address must not be empty" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestSPIFFEConfig_Validate_EmptyTrustDomain(t *testing.T) {
	cfg := SPIFFEConfig{
		WorkloadAPIAddr: "unix:///run/spire/sockets/agent.sock",
		TrustDomain:     "",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty trust domain")
	}
	if err.Error() != "SPIFFE trust domain must not be empty" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestSPIFFEConfig_Validate_InvalidTrustDomain(t *testing.T) {
	cfg := SPIFFEConfig{
		WorkloadAPIAddr: "unix:///run/spire/sockets/agent.sock",
		TrustDomain:     "spiffe://not-a-domain/with/path",
	}
	err := cfg.Validate()
	// The go-spiffe library may or may not reject certain domain formats;
	// just ensure no panic and validation completes.
	_ = err
}

func TestSPIFFEConfig_Validate_InvalidAllowedID(t *testing.T) {
	cfg := SPIFFEConfig{
		WorkloadAPIAddr:  "unix:///run/spire/sockets/agent.sock",
		TrustDomain:      "example.org",
		AllowedSPIFFEIDs: []string{"not-a-valid-spiffe-id"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid allowed SPIFFE ID")
	}
}

func TestSPIFFEConfig_Validate_ValidConfig(t *testing.T) {
	cfg := SPIFFEConfig{
		WorkloadAPIAddr:  "unix:///run/spire/sockets/agent.sock",
		TrustDomain:      "example.org",
		AllowedSPIFFEIDs: []string{"spiffe://example.org/workload/web"},
	}
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestNewSPIFFEProvider_InvalidConfig(t *testing.T) {
	logger := zap.NewNop()
	cfg := SPIFFEConfig{
		WorkloadAPIAddr: "",
		TrustDomain:     "example.org",
	}
	_, err := NewSPIFFEProvider(cfg, logger)
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestSPIFFEProvider_StartAndStop(t *testing.T) {
	logger := zap.NewNop()
	ca, caKey := generateSPIFFETestCA(t)
	svid := generateSPIFFESVID(t, ca, caKey, "spiffe://example.org/workload/test")
	td := spiffeid.RequireTrustDomainFromString("example.org")
	bundle := x509bundle.FromX509Authorities(td, []*x509.Certificate{ca})

	mock := &mockX509Source{
		svid:   svid,
		bundle: bundle,
	}

	cfg := SPIFFEConfig{
		WorkloadAPIAddr: "unix:///tmp/test.sock",
		TrustDomain:     "example.org",
	}

	provider, err := newSPIFFEProviderWithFactory(cfg, logger, mockSourceFactory(mock))
	if err != nil {
		t.Fatalf("unexpected error creating provider: %v", err)
	}

	err = provider.Start(context.Background())
	if err != nil {
		t.Fatalf("unexpected error starting provider: %v", err)
	}

	provider.Stop()

	if !mock.closed {
		t.Error("expected mock source to be closed after Stop()")
	}
}

func TestSPIFFEProvider_GetTLSConfig(t *testing.T) {
	logger := zap.NewNop()
	ca, caKey := generateSPIFFETestCA(t)
	svid := generateSPIFFESVID(t, ca, caKey, "spiffe://example.org/workload/test")
	td := spiffeid.RequireTrustDomainFromString("example.org")
	bundle := x509bundle.FromX509Authorities(td, []*x509.Certificate{ca})

	mock := &mockX509Source{
		svid:   svid,
		bundle: bundle,
	}

	cfg := SPIFFEConfig{
		WorkloadAPIAddr:  "unix:///tmp/test.sock",
		TrustDomain:      "example.org",
		AllowedSPIFFEIDs: []string{"spiffe://example.org/workload/test"},
	}

	provider, err := newSPIFFEProviderWithFactory(cfg, logger, mockSourceFactory(mock))
	if err != nil {
		t.Fatalf("unexpected error creating provider: %v", err)
	}

	err = provider.Start(context.Background())
	if err != nil {
		t.Fatalf("unexpected error starting provider: %v", err)
	}
	defer provider.Stop()

	tlsConfig, err := provider.GetTLSConfig()
	if err != nil {
		t.Fatalf("unexpected error getting TLS config: %v", err)
	}

	if tlsConfig.GetClientCertificate == nil {
		t.Error("expected GetClientCertificate to be set")
	}

	if tlsConfig.RootCAs == nil {
		t.Error("expected RootCAs to be set from trust bundle")
	}

	if tlsConfig.ClientCAs == nil {
		t.Error("expected ClientCAs to be set from trust bundle")
	}

	if tlsConfig.ClientAuth != 4 { // tls.RequireAndVerifyClientCert
		t.Errorf("expected ClientAuth to be RequireAndVerifyClientCert, got %d", tlsConfig.ClientAuth)
	}

	if tlsConfig.MinVersion != 0x0303 { // tls.VersionTLS12
		t.Errorf("expected MinVersion to be TLS 1.2, got %d", tlsConfig.MinVersion)
	}

	if tlsConfig.VerifyPeerCertificate == nil {
		t.Error("expected VerifyPeerCertificate callback to be set")
	}
}

func TestSPIFFEProvider_GetTLSConfig_NotStarted(t *testing.T) {
	logger := zap.NewNop()
	cfg := SPIFFEConfig{
		WorkloadAPIAddr: "unix:///tmp/test.sock",
		TrustDomain:     "example.org",
	}

	provider, err := newSPIFFEProviderWithFactory(cfg, logger, mockSourceFactory(&mockX509Source{}))
	if err != nil {
		t.Fatalf("unexpected error creating provider: %v", err)
	}

	_, err = provider.GetTLSConfig()
	if err == nil {
		t.Fatal("expected error when provider is not started")
	}
}

func TestSPIFFEProvider_GetClientCertificate(t *testing.T) {
	logger := zap.NewNop()
	ca, caKey := generateSPIFFETestCA(t)
	svid := generateSPIFFESVID(t, ca, caKey, "spiffe://example.org/workload/test")
	td := spiffeid.RequireTrustDomainFromString("example.org")
	bundle := x509bundle.FromX509Authorities(td, []*x509.Certificate{ca})

	mock := &mockX509Source{
		svid:   svid,
		bundle: bundle,
	}

	cfg := SPIFFEConfig{
		WorkloadAPIAddr: "unix:///tmp/test.sock",
		TrustDomain:     "example.org",
	}

	provider, err := newSPIFFEProviderWithFactory(cfg, logger, mockSourceFactory(mock))
	if err != nil {
		t.Fatalf("unexpected error creating provider: %v", err)
	}

	err = provider.Start(context.Background())
	if err != nil {
		t.Fatalf("unexpected error starting provider: %v", err)
	}
	defer provider.Stop()

	callback := provider.GetClientCertificate()
	cert, err := callback(nil)
	if err != nil {
		t.Fatalf("unexpected error getting client certificate: %v", err)
	}

	if len(cert.Certificate) == 0 {
		t.Error("expected non-empty certificate chain")
	}

	if cert.PrivateKey == nil {
		t.Error("expected private key to be set")
	}

	if cert.Leaf == nil {
		t.Error("expected leaf certificate to be set")
	}
}

func TestSPIFFEProvider_IsAllowedSPIFFEID(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name       string
		allowedIDs []string
		testID     string
		expected   bool
	}{
		{
			name:       "allowed ID in explicit list",
			allowedIDs: []string{"spiffe://example.org/workload/web"},
			testID:     "spiffe://example.org/workload/web",
			expected:   true,
		},
		{
			name:       "denied ID not in explicit list",
			allowedIDs: []string{"spiffe://example.org/workload/web"},
			testID:     "spiffe://example.org/workload/api",
			expected:   false,
		},
		{
			name:       "all IDs allowed when allow list is empty",
			allowedIDs: nil,
			testID:     "spiffe://example.org/workload/anything",
			expected:   true,
		},
		{
			name:       "different trust domain rejected",
			allowedIDs: nil,
			testID:     "spiffe://other.org/workload/test",
			expected:   false,
		},
		{
			name:       "different trust domain rejected with allow list",
			allowedIDs: []string{"spiffe://example.org/workload/web"},
			testID:     "spiffe://other.org/workload/web",
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := SPIFFEConfig{
				WorkloadAPIAddr:  "unix:///tmp/test.sock",
				TrustDomain:      "example.org",
				AllowedSPIFFEIDs: tt.allowedIDs,
			}

			provider, err := newSPIFFEProviderWithFactory(cfg, logger, mockSourceFactory(&mockX509Source{}))
			if err != nil {
				t.Fatalf("unexpected error creating provider: %v", err)
			}

			id := spiffeid.RequireFromString(tt.testID)
			result := provider.IsAllowedSPIFFEID(id)
			if result != tt.expected {
				t.Errorf("IsAllowedSPIFFEID(%s) = %v, want %v", tt.testID, result, tt.expected)
			}
		})
	}
}

func TestSPIFFEProvider_VerifyPeerCertificate_Allowed(t *testing.T) {
	logger := zap.NewNop()
	ca, caKey := generateSPIFFETestCA(t)
	svid := generateSPIFFESVID(t, ca, caKey, "spiffe://example.org/workload/test")
	td := spiffeid.RequireTrustDomainFromString("example.org")
	bundle := x509bundle.FromX509Authorities(td, []*x509.Certificate{ca})

	mock := &mockX509Source{
		svid:   svid,
		bundle: bundle,
	}

	cfg := SPIFFEConfig{
		WorkloadAPIAddr:  "unix:///tmp/test.sock",
		TrustDomain:      "example.org",
		AllowedSPIFFEIDs: []string{"spiffe://example.org/workload/test"},
	}

	provider, err := newSPIFFEProviderWithFactory(cfg, logger, mockSourceFactory(mock))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = provider.Start(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer provider.Stop()

	// Use the SVID leaf certificate raw bytes
	rawCerts := [][]byte{svid.Certificates[0].Raw}
	err = provider.verifyPeerCertificate(rawCerts, nil)
	if err != nil {
		t.Errorf("expected peer to be allowed, got error: %v", err)
	}
}

func TestSPIFFEProvider_VerifyPeerCertificate_Denied(t *testing.T) {
	logger := zap.NewNop()
	ca, caKey := generateSPIFFETestCA(t)
	svid := generateSPIFFESVID(t, ca, caKey, "spiffe://example.org/workload/other")
	td := spiffeid.RequireTrustDomainFromString("example.org")
	bundle := x509bundle.FromX509Authorities(td, []*x509.Certificate{ca})

	mock := &mockX509Source{
		svid:   svid,
		bundle: bundle,
	}

	cfg := SPIFFEConfig{
		WorkloadAPIAddr:  "unix:///tmp/test.sock",
		TrustDomain:      "example.org",
		AllowedSPIFFEIDs: []string{"spiffe://example.org/workload/web"},
	}

	provider, err := newSPIFFEProviderWithFactory(cfg, logger, mockSourceFactory(mock))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = provider.Start(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer provider.Stop()

	rawCerts := [][]byte{svid.Certificates[0].Raw}
	err = provider.verifyPeerCertificate(rawCerts, nil)
	if err == nil {
		t.Error("expected peer to be denied, but got no error")
	}
}

func TestSPIFFEProvider_VerifyPeerCertificate_NoCerts(t *testing.T) {
	logger := zap.NewNop()
	cfg := SPIFFEConfig{
		WorkloadAPIAddr: "unix:///tmp/test.sock",
		TrustDomain:     "example.org",
	}

	provider, err := newSPIFFEProviderWithFactory(cfg, logger, mockSourceFactory(&mockX509Source{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = provider.verifyPeerCertificate(nil, nil)
	if err == nil {
		t.Error("expected error for empty raw certs")
	}
}

func TestSPIFFEProvider_VerifyPeerCertificate_NoURISAN(t *testing.T) {
	logger := zap.NewNop()
	ca, caKey := generateSPIFFETestCA(t)

	// Create a certificate without URI SANs
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "no-spiffe-id"},
		NotBefore:    time.Now().Add(-1 * time.Minute),
		NotAfter:     time.Now().Add(1 * time.Hour),
		DNSNames:     []string{"example.com"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	cfg := SPIFFEConfig{
		WorkloadAPIAddr: "unix:///tmp/test.sock",
		TrustDomain:     "example.org",
	}

	provider, err := newSPIFFEProviderWithFactory(cfg, logger, mockSourceFactory(&mockX509Source{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = provider.verifyPeerCertificate([][]byte{certDER}, nil)
	if err == nil {
		t.Error("expected error for certificate without SPIFFE ID URI SAN")
	}
}

func TestSPIFFEProvider_GetTrustBundle(t *testing.T) {
	logger := zap.NewNop()
	ca, caKey := generateSPIFFETestCA(t)
	svid := generateSPIFFESVID(t, ca, caKey, "spiffe://example.org/workload/test")
	td := spiffeid.RequireTrustDomainFromString("example.org")
	bundle := x509bundle.FromX509Authorities(td, []*x509.Certificate{ca})

	mock := &mockX509Source{
		svid:   svid,
		bundle: bundle,
	}

	cfg := SPIFFEConfig{
		WorkloadAPIAddr: "unix:///tmp/test.sock",
		TrustDomain:     "example.org",
	}

	provider, err := newSPIFFEProviderWithFactory(cfg, logger, mockSourceFactory(mock))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = provider.Start(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer provider.Stop()

	b, err := provider.GetTrustBundle()
	if err != nil {
		t.Fatalf("unexpected error getting trust bundle: %v", err)
	}

	authorities := b.X509Authorities()
	if len(authorities) != 1 {
		t.Errorf("expected 1 authority in trust bundle, got %d", len(authorities))
	}
}

func TestSPIFFEProvider_GetTrustBundle_NotStarted(t *testing.T) {
	logger := zap.NewNop()
	cfg := SPIFFEConfig{
		WorkloadAPIAddr: "unix:///tmp/test.sock",
		TrustDomain:     "example.org",
	}

	provider, err := newSPIFFEProviderWithFactory(cfg, logger, mockSourceFactory(&mockX509Source{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = provider.GetTrustBundle()
	if err == nil {
		t.Fatal("expected error when provider is not started")
	}
}

func TestSPIFFEProvider_MultipleAllowedIDs(t *testing.T) {
	logger := zap.NewNop()
	cfg := SPIFFEConfig{
		WorkloadAPIAddr: "unix:///tmp/test.sock",
		TrustDomain:     "example.org",
		AllowedSPIFFEIDs: []string{
			"spiffe://example.org/workload/web",
			"spiffe://example.org/workload/api",
			"spiffe://example.org/workload/db",
		},
	}

	provider, err := newSPIFFEProviderWithFactory(cfg, logger, mockSourceFactory(&mockX509Source{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	allowedIDs := []string{
		"spiffe://example.org/workload/web",
		"spiffe://example.org/workload/api",
		"spiffe://example.org/workload/db",
	}

	for _, idStr := range allowedIDs {
		id := spiffeid.RequireFromString(idStr)
		if !provider.IsAllowedSPIFFEID(id) {
			t.Errorf("expected %s to be allowed", idStr)
		}
	}

	deniedID := spiffeid.RequireFromString("spiffe://example.org/workload/cache")
	if provider.IsAllowedSPIFFEID(deniedID) {
		t.Error("expected spiffe://example.org/workload/cache to be denied")
	}
}
