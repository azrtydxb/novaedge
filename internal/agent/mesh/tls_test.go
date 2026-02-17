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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/url"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

// testPKI holds generated test certificates for mesh TLS tests.
type testPKI struct {
	caCertPEM      []byte
	caKeyPEM       []byte
	workloadCert   []byte
	workloadKey    []byte
	spiffeID       string
	caCert         *x509.Certificate
	caKey          *ecdsa.PrivateKey
	workloadParsed *x509.Certificate
}

// generateTestPKI creates a self-signed CA and a workload certificate
// with a SPIFFE ID URI SAN for testing.
func generateTestPKI(t *testing.T, spiffeID string) *testPKI {
	t.Helper()

	// Generate CA key pair.
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate CA key: %v", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   "Test Mesh CA",
			Organization: []string{"NovaEdge Test"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("failed to create CA certificate: %v", err)
	}

	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		t.Fatalf("failed to parse CA certificate: %v", err)
	}

	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})
	caKeyDER, err := x509.MarshalECPrivateKey(caKey)
	if err != nil {
		t.Fatalf("failed to marshal CA key: %v", err)
	}
	caKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: caKeyDER})

	// Generate workload key pair.
	workloadKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate workload key: %v", err)
	}

	spiffeURL, err := url.Parse(spiffeID)
	if err != nil {
		t.Fatalf("failed to parse SPIFFE ID URL: %v", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("failed to generate serial number: %v", err)
	}

	workloadTemplate := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   "test-workload",
			Organization: []string{"NovaEdge Test"},
		},
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter:  time.Now().Add(24 * time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
		URIs: []*url.URL{spiffeURL},
	}

	workloadCertDER, err := x509.CreateCertificate(rand.Reader, workloadTemplate, caCert, &workloadKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("failed to create workload certificate: %v", err)
	}

	workloadParsed, err := x509.ParseCertificate(workloadCertDER)
	if err != nil {
		t.Fatalf("failed to parse workload certificate: %v", err)
	}

	workloadCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: workloadCertDER})
	workloadKeyDER, err := x509.MarshalECPrivateKey(workloadKey)
	if err != nil {
		t.Fatalf("failed to marshal workload key: %v", err)
	}
	workloadKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: workloadKeyDER})

	return &testPKI{
		caCertPEM:      caCertPEM,
		caKeyPEM:       caKeyPEM,
		workloadCert:   workloadCertPEM,
		workloadKey:    workloadKeyPEM,
		spiffeID:       spiffeID,
		caCert:         caCert,
		caKey:          caKey,
		workloadParsed: workloadParsed,
	}
}

func TestTLSProviderUpdateCertificate(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pki := generateTestPKI(t, "spiffe://cluster.local/ns/default/sa/test-agent")

	provider := NewTLSProvider(logger, "cluster.local", nil)

	// Before loading, HasCertificate should return false.
	if provider.HasCertificate() {
		t.Fatal("expected HasCertificate to return false before loading")
	}

	// Load valid certificate.
	err := provider.UpdateCertificate(pki.workloadCert, pki.workloadKey, pki.caCertPEM, pki.spiffeID)
	if err != nil {
		t.Fatalf("UpdateCertificate failed: %v", err)
	}

	// After loading, HasCertificate should return true.
	if !provider.HasCertificate() {
		t.Fatal("expected HasCertificate to return true after loading")
	}

	// Verify the stored SPIFFE ID.
	provider.mu.RLock()
	storedID := provider.spiffeID
	provider.mu.RUnlock()
	if storedID != pki.spiffeID {
		t.Fatalf("expected spiffeID %q, got %q", pki.spiffeID, storedID)
	}
}

func TestTLSProviderUpdateCertificateErrors(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pki := generateTestPKI(t, "spiffe://cluster.local/ns/default/sa/test-agent")

	provider := NewTLSProvider(logger, "cluster.local", nil)

	// Invalid key PEM should fail.
	err := provider.UpdateCertificate(pki.workloadCert, []byte("not-a-key"), pki.caCertPEM, pki.spiffeID)
	if err == nil {
		t.Fatal("expected error for invalid key PEM")
	}

	// Invalid cert PEM should fail.
	err = provider.UpdateCertificate([]byte("not-a-cert"), pki.workloadKey, pki.caCertPEM, pki.spiffeID)
	if err == nil {
		t.Fatal("expected error for invalid cert PEM")
	}

	// Empty CA bundle should fail.
	err = provider.UpdateCertificate(pki.workloadCert, pki.workloadKey, []byte("not-ca-pem"), pki.spiffeID)
	if err == nil {
		t.Fatal("expected error for empty CA bundle")
	}

	// Provider should still not have a certificate.
	if provider.HasCertificate() {
		t.Fatal("expected HasCertificate to return false after failed updates")
	}
}

func TestTLSProviderServerConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pki := generateTestPKI(t, "spiffe://cluster.local/ns/default/sa/test-agent")

	provider := NewTLSProvider(logger, "cluster.local", nil)
	err := provider.UpdateCertificate(pki.workloadCert, pki.workloadKey, pki.caCertPEM, pki.spiffeID)
	if err != nil {
		t.Fatalf("UpdateCertificate failed: %v", err)
	}

	cfg := provider.ServerTLSConfig()

	// Verify TLS 1.3 minimum.
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Fatalf("expected MinVersion TLS 1.3 (0x%04x), got 0x%04x", tls.VersionTLS13, cfg.MinVersion)
	}

	// Verify client auth is required.
	if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("expected ClientAuth RequireAndVerifyClientCert, got %v", cfg.ClientAuth)
	}

	// Verify NextProtos includes h2.
	if len(cfg.NextProtos) == 0 || cfg.NextProtos[0] != "h2" {
		t.Fatalf("expected NextProtos [h2], got %v", cfg.NextProtos)
	}

	// Verify GetCertificate callback works.
	if cfg.GetCertificate == nil {
		t.Fatal("expected GetCertificate callback to be set")
	}
	cert, err := cfg.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("GetCertificate failed: %v", err)
	}
	if cert == nil {
		t.Fatal("expected non-nil certificate from GetCertificate")
	}
}

func TestTLSProviderClientConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pki := generateTestPKI(t, "spiffe://cluster.local/ns/default/sa/test-agent")

	provider := NewTLSProvider(logger, "cluster.local", nil)
	err := provider.UpdateCertificate(pki.workloadCert, pki.workloadKey, pki.caCertPEM, pki.spiffeID)
	if err != nil {
		t.Fatalf("UpdateCertificate failed: %v", err)
	}

	cfg := provider.ClientTLSConfig()

	// Verify TLS 1.3 minimum.
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Fatalf("expected MinVersion TLS 1.3 (0x%04x), got 0x%04x", tls.VersionTLS13, cfg.MinVersion)
	}

	// Verify NextProtos includes h2.
	if len(cfg.NextProtos) == 0 || cfg.NextProtos[0] != "h2" {
		t.Fatalf("expected NextProtos [h2], got %v", cfg.NextProtos)
	}

	// Verify RootCAs is set.
	if cfg.RootCAs == nil {
		t.Fatal("expected RootCAs to be set")
	}

	// Verify GetClientCertificate callback works.
	if cfg.GetClientCertificate == nil {
		t.Fatal("expected GetClientCertificate callback to be set")
	}
	cert, err := cfg.GetClientCertificate(&tls.CertificateRequestInfo{})
	if err != nil {
		t.Fatalf("GetClientCertificate failed: %v", err)
	}
	if cert == nil {
		t.Fatal("expected non-nil certificate from GetClientCertificate")
	}
}

func TestTLSProviderNoCertificateCallbacks(t *testing.T) {
	logger := zaptest.NewLogger(t)
	provider := NewTLSProvider(logger, "cluster.local", nil)

	// Server config callback should return error when no cert is loaded.
	serverCfg := provider.ServerTLSConfig()
	_, err := serverCfg.GetCertificate(&tls.ClientHelloInfo{})
	if err == nil {
		t.Fatal("expected error from GetCertificate with no cert loaded")
	}

	// Client config callback should return error when no cert is loaded.
	clientCfg := provider.ClientTLSConfig()
	_, err = clientCfg.GetClientCertificate(&tls.CertificateRequestInfo{})
	if err == nil {
		t.Fatal("expected error from GetClientCertificate with no cert loaded")
	}
}

func TestPeerSPIFFEID(t *testing.T) {
	spiffeIDStr := "spiffe://cluster.local/ns/kube-system/sa/novaedge-agent"
	spiffeURL, err := url.Parse(spiffeIDStr)
	if err != nil {
		t.Fatalf("failed to parse SPIFFE URL: %v", err)
	}

	t.Run("valid SPIFFE ID", func(t *testing.T) {
		state := &tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{
				{
					URIs: []*url.URL{spiffeURL},
				},
			},
		}

		got := PeerSPIFFEID(state)
		if got != spiffeIDStr {
			t.Fatalf("expected %q, got %q", spiffeIDStr, got)
		}
	})

	t.Run("no SPIFFE ID in URIs", func(t *testing.T) {
		httpURL, _ := url.Parse("https://example.com/identity")
		state := &tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{
				{
					URIs: []*url.URL{httpURL},
				},
			},
		}

		got := PeerSPIFFEID(state)
		if got != "" {
			t.Fatalf("expected empty string, got %q", got)
		}
	})

	t.Run("no peer certificates", func(t *testing.T) {
		state := &tls.ConnectionState{}
		got := PeerSPIFFEID(state)
		if got != "" {
			t.Fatalf("expected empty string, got %q", got)
		}
	})

	t.Run("nil connection state", func(t *testing.T) {
		got := PeerSPIFFEID(nil)
		if got != "" {
			t.Fatalf("expected empty string, got %q", got)
		}
	})

	t.Run("multiple URIs with SPIFFE second", func(t *testing.T) {
		httpURL, _ := url.Parse("https://example.com/other")
		state := &tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{
				{
					URIs: []*url.URL{httpURL, spiffeURL},
				},
			},
		}

		got := PeerSPIFFEID(state)
		if got != spiffeIDStr {
			t.Fatalf("expected %q, got %q", spiffeIDStr, got)
		}
	})
}

func TestTLSProviderCertificateRotation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	provider := NewTLSProvider(logger, "cluster.local", nil)

	// Load initial certificate.
	pki1 := generateTestPKI(t, "spiffe://cluster.local/ns/default/sa/agent-v1")
	err := provider.UpdateCertificate(pki1.workloadCert, pki1.workloadKey, pki1.caCertPEM, pki1.spiffeID)
	if err != nil {
		t.Fatalf("first UpdateCertificate failed: %v", err)
	}

	clientCfg := provider.ClientTLSConfig()
	cert1, err := clientCfg.GetClientCertificate(&tls.CertificateRequestInfo{})
	if err != nil {
		t.Fatalf("GetClientCertificate failed: %v", err)
	}

	// Rotate to a new certificate.
	pki2 := generateTestPKI(t, "spiffe://cluster.local/ns/default/sa/agent-v2")
	err = provider.UpdateCertificate(pki2.workloadCert, pki2.workloadKey, pki2.caCertPEM, pki2.spiffeID)
	if err != nil {
		t.Fatalf("second UpdateCertificate failed: %v", err)
	}

	// The same config's callback should now return the new certificate.
	cert2, err := clientCfg.GetClientCertificate(&tls.CertificateRequestInfo{})
	if err != nil {
		t.Fatalf("GetClientCertificate after rotation failed: %v", err)
	}

	// Certificates should be different (different serial numbers).
	if cert1.Leaf != nil && cert2.Leaf != nil {
		if cert1.Leaf.SerialNumber.Cmp(cert2.Leaf.SerialNumber) == 0 {
			t.Fatal("expected different certificates after rotation")
		}
	}

	// Verify the SPIFFE ID was updated.
	provider.mu.RLock()
	storedID := provider.spiffeID
	provider.mu.RUnlock()
	if storedID != pki2.spiffeID {
		t.Fatalf("expected spiffeID %q after rotation, got %q", pki2.spiffeID, storedID)
	}
}

func TestValidatePeerSPIFFEIDLocalOnly(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// No federation config — only local trust domain accepted.
	provider := NewTLSProvider(logger, "cluster.local", nil)

	tests := []struct {
		name     string
		spiffeID string
		want     bool
	}{
		{"local agent", "spiffe://cluster.local/agent/node-1", true},
		{"local workload", "spiffe://cluster.local/ns/default/sa/svc", true},
		{"wrong trust domain", "spiffe://other.domain/agent/node-1", false},
		{"federated identity rejected", "spiffe://my-fed/cluster/remote/agent/node-1", false},
		{"empty string", "", false},
		{"invalid", "not-a-spiffe-id", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := provider.ValidatePeerSPIFFEID(tt.spiffeID); got != tt.want {
				t.Errorf("ValidatePeerSPIFFEID(%q) = %v, want %v", tt.spiffeID, got, tt.want)
			}
		})
	}
}

func TestValidatePeerSPIFFEIDWithFederation(t *testing.T) {
	logger := zaptest.NewLogger(t)

	fedCfg := &FederationConfig{
		FederationID:    "my-federation",
		ClusterName:     "cluster-a",
		AllowedClusters: []string{"cluster-b", "cluster-c"},
	}
	provider := NewTLSProvider(logger, "cluster.local", fedCfg)

	tests := []struct {
		name     string
		spiffeID string
		want     bool
	}{
		{"local agent", "spiffe://cluster.local/agent/node-1", true},
		{"federated allowed cluster-b", "spiffe://my-federation/cluster/cluster-b/agent/node-2", true},
		{"federated allowed cluster-c", "spiffe://my-federation/cluster/cluster-c/agent/node-3", true},
		{"federated disallowed cluster-d", "spiffe://my-federation/cluster/cluster-d/agent/node-4", false},
		{"wrong federation ID", "spiffe://other-fed/cluster/cluster-b/agent/node-5", false},
		{"wrong trust domain", "spiffe://evil.domain/agent/node-6", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := provider.ValidatePeerSPIFFEID(tt.spiffeID); got != tt.want {
				t.Errorf("ValidatePeerSPIFFEID(%q) = %v, want %v", tt.spiffeID, got, tt.want)
			}
		})
	}
}
