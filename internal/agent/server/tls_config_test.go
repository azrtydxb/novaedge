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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// generateTestCertPEM generates a test certificate and key in PEM format
func generateTestCertPEM(t *testing.T, commonName string, sans ...string) (certPEM, keyPEM []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().Unix()),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(1 * time.Hour),
		DNSNames:     sans,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("failed to marshal key: %v", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	return certPEM, keyPEM
}

func TestParseTLSVersion(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		expected uint16
	}{
		{
			name:     "TLS 1.2",
			version:  "TLS1.2",
			expected: tls.VersionTLS12,
		},
		{
			name:     "TLS 1.3",
			version:  "TLS1.3",
			expected: tls.VersionTLS13,
		},
		{
			name:     "empty string defaults to TLS 1.3",
			version:  "",
			expected: tls.VersionTLS13,
		},
		{
			name:     "unknown version defaults to TLS 1.3",
			version:  "TLS1.0",
			expected: tls.VersionTLS13,
		},
		{
			name:     "invalid version defaults to TLS 1.3",
			version:  "invalid",
			expected: tls.VersionTLS13,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := &HTTPServer{
				logger: zap.NewNop(),
			}
			result := server.parseTLSVersion(tt.version)
			if result != tt.expected {
				t.Errorf("parseTLSVersion(%q) = %d, want %d", tt.version, result, tt.expected)
			}
		})
	}
}

func TestParseCipherSuites(t *testing.T) {
	tests := []struct {
		name     string
		suites   []string
		expected []uint16
	}{
		{
			name:     "empty list returns nil",
			suites:   []string{},
			expected: nil,
		},
		{
			name:     "nil list returns nil",
			suites:   nil,
			expected: nil,
		},
		{
			name:     "single TLS 1.3 cipher suite",
			suites:   []string{"TLS_AES_128_GCM_SHA256"},
			expected: []uint16{tls.TLS_AES_128_GCM_SHA256},
		},
		{
			name:     "multiple TLS 1.3 cipher suites",
			suites:   []string{"TLS_AES_128_GCM_SHA256", "TLS_AES_256_GCM_SHA384", "TLS_CHACHA20_POLY1305_SHA256"},
			expected: []uint16{tls.TLS_AES_128_GCM_SHA256, tls.TLS_AES_256_GCM_SHA384, tls.TLS_CHACHA20_POLY1305_SHA256},
		},
		{
			name:     "TLS 1.2 ECDHE cipher suites",
			suites:   []string{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", "TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384"},
			expected: []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384},
		},
		{
			name:     "ChaCha20 cipher suites",
			suites:   []string{"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256", "TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256"},
			expected: []uint16{tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256, tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256},
		},
		{
			name:     "unknown cipher suite ignored",
			suites:   []string{"UNKNOWN_CIPHER"},
			expected: []uint16{},
		},
		{
			name:     "mixed valid and invalid cipher suites",
			suites:   []string{"TLS_AES_128_GCM_SHA256", "UNKNOWN_CIPHER", "TLS_AES_256_GCM_SHA384"},
			expected: []uint16{tls.TLS_AES_128_GCM_SHA256, tls.TLS_AES_256_GCM_SHA384},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := &HTTPServer{
				logger: zap.NewNop(),
			}
			result := server.parseCipherSuites(tt.suites)
			if len(result) != len(tt.expected) {
				t.Errorf("parseCipherSuites() returned %d suites, want %d", len(result), len(tt.expected))
				return
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("parseCipherSuites()[%d] = %d, want %d", i, result[i], tt.expected[i])
				}
			}
		})
	}
}

func TestAeadOnlyCipherSuites(t *testing.T) {
	suites := aeadOnlyCipherSuites()

	expectedSuites := []uint16{
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
		tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
	}

	if len(suites) != len(expectedSuites) {
		t.Errorf("aeadOnlyCipherSuites() returned %d suites, want %d", len(suites), len(expectedSuites))
		return
	}

	for i, expected := range expectedSuites {
		if suites[i] != expected {
			t.Errorf("aeadOnlyCipherSuites()[%d] = %d, want %d", i, suites[i], expected)
		}
	}
}

func TestCreateTLSConfigWithSNI_SingleCertificate(t *testing.T) {
	certPEM, keyPEM := generateTestCertPEM(t, "example.com", "example.com")

	listener := &pb.Listener{
		Name: "test-listener",
		Tls: &pb.TLSConfig{
			Cert: certPEM,
			Key:  keyPEM,
		},
	}

	server := &HTTPServer{
		logger: zap.NewNop(),
	}

	tlsConfig, err := server.createTLSConfigWithSNI(listener)
	if err != nil {
		t.Fatalf("createTLSConfigWithSNI() error = %v", err)
	}

	if tlsConfig == nil {
		t.Fatal("expected non-nil tls.Config")
	}

	if tlsConfig.MinVersion != tls.VersionTLS13 {
		t.Errorf("MinVersion = %d, want %d", tlsConfig.MinVersion, tls.VersionTLS13)
	}

	if len(tlsConfig.Certificates) != 1 {
		t.Errorf("len(Certificates) = %d, want 1", len(tlsConfig.Certificates))
	}
}

func TestCreateTLSConfigWithSNI_MultipleCertificates(t *testing.T) {
	cert1PEM, key1PEM := generateTestCertPEM(t, "example.com", "example.com")
	cert2PEM, key2PEM := generateTestCertPEM(t, "api.example.com", "api.example.com")
	cert3PEM, key3PEM := generateTestCertPEM(t, "*.example.com", "*.example.com")

	listener := &pb.Listener{
		Name: "test-listener",
		TlsCertificates: map[string]*pb.TLSConfig{
			"example.com": {
				Cert: cert1PEM,
				Key:  key1PEM,
			},
			"api.example.com": {
				Cert: cert2PEM,
				Key:  key2PEM,
			},
			"*.example.com": {
				Cert: cert3PEM,
				Key:  key3PEM,
			},
		},
	}

	server := &HTTPServer{
		logger: zap.NewNop(),
	}

	tlsConfig, err := server.createTLSConfigWithSNI(listener)
	if err != nil {
		t.Fatalf("createTLSConfigWithSNI() error = %v", err)
	}

	if tlsConfig.GetCertificate == nil {
		t.Fatal("expected GetCertificate callback to be set")
	}

	// Test exact match
	clientHello := &tls.ClientHelloInfo{ServerName: "api.example.com"}
	cert, err := tlsConfig.GetCertificate(clientHello)
	if err != nil {
		t.Errorf("GetCertificate(api.example.com) error = %v", err)
	}
	if cert == nil {
		t.Error("expected certificate for api.example.com")
	}

	// Test wildcard match
	clientHello = &tls.ClientHelloInfo{ServerName: "test.example.com"}
	cert, err = tlsConfig.GetCertificate(clientHello)
	if err != nil {
		t.Errorf("GetCertificate(test.example.com) error = %v", err)
	}
	if cert == nil {
		t.Error("expected wildcard certificate for test.example.com")
	}

	// Test default certificate fallback
	clientHello = &tls.ClientHelloInfo{ServerName: "unknown.com"}
	cert, err = tlsConfig.GetCertificate(clientHello)
	if err != nil {
		t.Errorf("GetCertificate(unknown.com) error = %v", err)
	}
	if cert == nil {
		t.Error("expected default certificate for unknown.com")
	}
}

func TestCreateTLSConfigWithSNI_WildcardCertificate(t *testing.T) {
	certPEM, keyPEM := generateTestCertPEM(t, "*.example.com", "*.example.com")

	listener := &pb.Listener{
		Name: "test-listener",
		TlsCertificates: map[string]*pb.TLSConfig{
			"*.example.com": {
				Cert: certPEM,
				Key:  keyPEM,
			},
		},
	}

	server := &HTTPServer{
		logger: zap.NewNop(),
	}

	tlsConfig, err := server.createTLSConfigWithSNI(listener)
	if err != nil {
		t.Fatalf("createTLSConfigWithSNI() error = %v", err)
	}

	// Test subdomain matches wildcard
	clientHello := &tls.ClientHelloInfo{ServerName: "api.example.com"}
	cert, err := tlsConfig.GetCertificate(clientHello)
	if err != nil {
		t.Errorf("GetCertificate(api.example.com) error = %v", err)
	}
	if cert == nil {
		t.Error("expected wildcard certificate for api.example.com")
	}
}

func TestCreateTLSConfigWithSNI_TLS12Legacy(t *testing.T) {
	certPEM, keyPEM := generateTestCertPEM(t, "example.com", "example.com")

	listener := &pb.Listener{
		Name: "test-listener",
		Tls: &pb.TLSConfig{
			Cert:       certPEM,
			Key:        keyPEM,
			MinVersion: "TLS1.2",
		},
	}

	server := &HTTPServer{
		logger: zap.NewNop(),
	}

	tlsConfig, err := server.createTLSConfigWithSNI(listener)
	if err != nil {
		t.Fatalf("createTLSConfigWithSNI() error = %v", err)
	}

	if tlsConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want %d", tlsConfig.MinVersion, tls.VersionTLS12)
	}

	// Should have AEAD-only cipher suites for TLS 1.2
	if tlsConfig.CipherSuites == nil {
		t.Error("expected cipher suites to be set for TLS 1.2")
	}

	expectedSuites := aeadOnlyCipherSuites()
	if len(tlsConfig.CipherSuites) != len(expectedSuites) {
		t.Errorf("len(CipherSuites) = %d, want %d", len(tlsConfig.CipherSuites), len(expectedSuites))
	}
}

func TestCreateTLSConfigWithSNI_CustomCipherSuites(t *testing.T) {
	certPEM, keyPEM := generateTestCertPEM(t, "example.com", "example.com")

	listener := &pb.Listener{
		Name: "test-listener",
		Tls: &pb.TLSConfig{
			Cert:         certPEM,
			Key:          keyPEM,
			CipherSuites: []string{"TLS_AES_128_GCM_SHA256", "TLS_AES_256_GCM_SHA384"},
		},
	}

	server := &HTTPServer{
		logger: zap.NewNop(),
	}

	tlsConfig, err := server.createTLSConfigWithSNI(listener)
	if err != nil {
		t.Fatalf("createTLSConfigWithSNI() error = %v", err)
	}

	expectedSuites := []uint16{tls.TLS_AES_128_GCM_SHA256, tls.TLS_AES_256_GCM_SHA384}
	if len(tlsConfig.CipherSuites) != len(expectedSuites) {
		t.Errorf("len(CipherSuites) = %d, want %d", len(tlsConfig.CipherSuites), len(expectedSuites))
	}
}

func TestCreateTLSConfigWithSNI_NoCertificates(t *testing.T) {
	listener := &pb.Listener{
		Name: "test-listener",
	}

	server := &HTTPServer{
		logger: zap.NewNop(),
	}

	_, err := server.createTLSConfigWithSNI(listener)
	if err == nil {
		t.Fatal("expected error for listener with no certificates")
	}

	expectedMsg := "no TLS certificates configured"
	if err.Error() != expectedMsg {
		t.Errorf("error message = %q, want %q", err.Error(), expectedMsg)
	}
}

func TestCreateTLSConfigWithSNI_InvalidCertificate(t *testing.T) {
	listener := &pb.Listener{
		Name: "test-listener",
		Tls: &pb.TLSConfig{
			Cert: []byte("invalid cert"),
			Key:  []byte("invalid key"),
		},
	}

	server := &HTTPServer{
		logger: zap.NewNop(),
	}

	_, err := server.createTLSConfigWithSNI(listener)
	if err == nil {
		t.Fatal("expected error for invalid certificate")
	}
}

func TestCreateTLSConfigWithSNI_PartiallyInvalidSNICerts(t *testing.T) {
	cert1PEM, key1PEM := generateTestCertPEM(t, "example.com", "example.com")

	listener := &pb.Listener{
		Name: "test-listener",
		TlsCertificates: map[string]*pb.TLSConfig{
			"example.com": {
				Cert: cert1PEM,
				Key:  key1PEM,
			},
			"invalid.com": {
				Cert: []byte("invalid"),
				Key:  []byte("invalid"),
			},
		},
	}

	server := &HTTPServer{
		logger: zap.NewNop(),
	}

	tlsConfig, err := server.createTLSConfigWithSNI(listener)
	if err != nil {
		t.Fatalf("createTLSConfigWithSNI() error = %v", err)
	}

	// Should still work with valid certificate
	if tlsConfig == nil {
		t.Fatal("expected non-nil tls.Config")
	}
}

func TestCreateTLSConfigWithSNI_EmptyServerName(t *testing.T) {
	certPEM, keyPEM := generateTestCertPEM(t, "example.com", "example.com")

	listener := &pb.Listener{
		Name: "test-listener",
		TlsCertificates: map[string]*pb.TLSConfig{
			"example.com": {
				Cert: certPEM,
				Key:  keyPEM,
			},
		},
	}

	server := &HTTPServer{
		logger: zap.NewNop(),
	}

	tlsConfig, err := server.createTLSConfigWithSNI(listener)
	if err != nil {
		t.Fatalf("createTLSConfigWithSNI() error = %v", err)
	}

	// Test with empty server name (should return default cert)
	clientHello := &tls.ClientHelloInfo{ServerName: ""}
	cert, err := tlsConfig.GetCertificate(clientHello)
	if err != nil {
		t.Errorf("GetCertificate() error = %v", err)
	}
	if cert == nil {
		t.Error("expected default certificate for empty ServerName")
	}
}

func TestCreateTLSConfigWithMTLS_NoClientAuth(t *testing.T) {
	certPEM, keyPEM := generateTestCertPEM(t, "example.com", "example.com")

	listener := &pb.Listener{
		Name: "test-listener",
		Tls: &pb.TLSConfig{
			Cert: certPEM,
			Key:  keyPEM,
		},
	}

	server := &HTTPServer{
		logger: zap.NewNop(),
	}

	tlsConfig, err := server.createTLSConfigWithMTLS(listener, nil, false)
	if err != nil {
		t.Fatalf("createTLSConfigWithMTLS() error = %v", err)
	}

	if tlsConfig == nil {
		t.Fatal("expected non-nil tls.Config")
	}
}

func TestCreateTLSConfigWithMTLS_WithOCSP(t *testing.T) {
	certPEM, keyPEM := generateTestCertPEM(t, "example.com", "example.com")

	listener := &pb.Listener{
		Name: "test-listener",
		Tls: &pb.TLSConfig{
			Cert: certPEM,
			Key:  keyPEM,
		},
	}

	server := &HTTPServer{
		logger:      zap.NewNop(),
		ocspStapler: NewOCSPStapler(zap.NewNop()),
	}

	tlsConfig, err := server.createTLSConfigWithMTLS(listener, nil, true)
	if err != nil {
		t.Fatalf("createTLSConfigWithMTLS() error = %v", err)
	}

	// OCSP stapling should set GetCertificate callback
	if tlsConfig.GetCertificate == nil {
		t.Error("expected GetCertificate callback to be set for OCSP stapling")
	}
}

func TestCreateTLSConfigWithMTLS_InvalidBaseTLS(t *testing.T) {
	listener := &pb.Listener{
		Name: "test-listener",
	}

	server := &HTTPServer{
		logger: zap.NewNop(),
	}

	_, err := server.createTLSConfigWithMTLS(listener, nil, false)
	if err == nil {
		t.Fatal("expected error for invalid TLS config")
	}
}

func TestCreateTLSConfigWithMTLS_InvalidClientAuth(t *testing.T) {
	certPEM, keyPEM := generateTestCertPEM(t, "example.com", "example.com")

	listener := &pb.Listener{
		Name: "test-listener",
		Tls: &pb.TLSConfig{
			Cert: certPEM,
			Key:  keyPEM,
		},
	}

	clientAuth := &pb.ClientAuthConfig{
		Mode:   "require",
		CaCert: []byte("invalid CA"),
	}

	server := &HTTPServer{
		logger: zap.NewNop(),
	}

	_, err := server.createTLSConfigWithMTLS(listener, clientAuth, false)
	if err == nil {
		t.Fatal("expected error for invalid client auth config")
	}
}

func TestCreateTLSConfigWithSNI_SNICertsWithMinVersionAndCipherSuites(t *testing.T) {
	cert1PEM, key1PEM := generateTestCertPEM(t, "example.com", "example.com")

	listener := &pb.Listener{
		Name: "test-listener",
		TlsCertificates: map[string]*pb.TLSConfig{
			"example.com": {
				Cert:         cert1PEM,
				Key:          key1PEM,
				MinVersion:   "TLS1.2",
				CipherSuites: []string{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"},
			},
		},
	}

	server := &HTTPServer{
		logger: zap.NewNop(),
	}

	tlsConfig, err := server.createTLSConfigWithSNI(listener)
	if err != nil {
		t.Fatalf("createTLSConfigWithSNI() error = %v", err)
	}

	// Should use settings from SNI certificate when no default TLS config
	if tlsConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want %d", tlsConfig.MinVersion, tls.VersionTLS12)
	}
}

func TestCreateTLSConfigWithSNI_SingleLabelDomain(t *testing.T) {
	certPEM, keyPEM := generateTestCertPEM(t, "*.example.com", "*.example.com")

	listener := &pb.Listener{
		Name: "test-listener",
		TlsCertificates: map[string]*pb.TLSConfig{
			"*.example.com": {
				Cert: certPEM,
				Key:  keyPEM,
			},
		},
	}

	server := &HTTPServer{
		logger: zap.NewNop(),
	}

	tlsConfig, err := server.createTLSConfigWithSNI(listener)
	if err != nil {
		t.Fatalf("createTLSConfigWithSNI() error = %v", err)
	}

	// Test single-label domain (no wildcard match)
	clientHello := &tls.ClientHelloInfo{ServerName: "localhost"}
	cert, err := tlsConfig.GetCertificate(clientHello)
	if err != nil {
		t.Errorf("GetCertificate(localhost) error = %v", err)
	}
	// Should return default cert since localhost is single-label
	if cert == nil {
		t.Error("expected default certificate for single-label domain")
	}
}
