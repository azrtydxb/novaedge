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
	"net/http"
	"net/url"
	"testing"
	"time"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestParseClientAuth(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		expected tls.ClientAuthType
	}{
		{
			name:     "require mode",
			mode:     "require",
			expected: tls.RequireAndVerifyClientCert,
		},
		{
			name:     "REQUIRE uppercase",
			mode:     "REQUIRE",
			expected: tls.RequireAndVerifyClientCert,
		},
		{
			name:     "optional mode",
			mode:     "optional",
			expected: tls.VerifyClientCertIfGiven,
		},
		{
			name:     "OPTIONAL uppercase",
			mode:     "OPTIONAL",
			expected: tls.VerifyClientCertIfGiven,
		},
		{
			name:     "none mode",
			mode:     "none",
			expected: tls.NoClientCert,
		},
		{
			name:     "empty string defaults to NoClientCert",
			mode:     "",
			expected: tls.NoClientCert,
		},
		{
			name:     "unknown mode defaults to NoClientCert",
			mode:     "invalid",
			expected: tls.NoClientCert,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseClientAuth(tt.mode)
			if result != tt.expected {
				t.Errorf("parseClientAuth(%q) = %v, want %v", tt.mode, result, tt.expected)
			}
		})
	}
}

func TestApplyClientAuthConfig_NilConfig(t *testing.T) {
	server := &HTTPServer{
		logger: zap.NewNop(),
	}

	tlsConfig := &tls.Config{}

	err := server.applyClientAuthConfig(tlsConfig, nil)
	if err != nil {
		t.Errorf("applyClientAuthConfig(nil) error = %v, want nil", err)
	}

	if tlsConfig.ClientAuth != tls.NoClientCert {
		t.Errorf("ClientAuth = %v, want %v", tlsConfig.ClientAuth, tls.NoClientCert)
	}
}

func TestApplyClientAuthConfig_NoneMode(t *testing.T) {
	server := &HTTPServer{
		logger: zap.NewNop(),
	}

	tlsConfig := &tls.Config{}
	clientAuth := &pb.ClientAuthConfig{
		Mode: "none",
	}

	err := server.applyClientAuthConfig(tlsConfig, clientAuth)
	if err != nil {
		t.Errorf("applyClientAuthConfig() error = %v, want nil", err)
	}
}

func TestApplyClientAuthConfig_EmptyMode(t *testing.T) {
	server := &HTTPServer{
		logger: zap.NewNop(),
	}

	tlsConfig := &tls.Config{}
	clientAuth := &pb.ClientAuthConfig{
		Mode: "",
	}

	err := server.applyClientAuthConfig(tlsConfig, clientAuth)
	if err != nil {
		t.Errorf("applyClientAuthConfig() error = %v, want nil", err)
	}
}

func TestApplyClientAuthConfig_RequireWithCA(t *testing.T) {
	// Generate CA certificate
	caCert, caPEM := generateCACertificate(t)

	server := &HTTPServer{
		logger: zap.NewNop(),
	}

	tlsConfig := &tls.Config{}
	clientAuth := &pb.ClientAuthConfig{
		Mode:   "require",
		CaCert: caPEM,
	}

	err := server.applyClientAuthConfig(tlsConfig, clientAuth)
	if err != nil {
		t.Fatalf("applyClientAuthConfig() error = %v", err)
	}

	if tlsConfig.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Errorf("ClientAuth = %v, want %v", tlsConfig.ClientAuth, tls.RequireAndVerifyClientCert)
	}

	if tlsConfig.ClientCAs == nil {
		t.Fatal("ClientCAs should be set")
	}

	// Verify CA was loaded correctly
	subjects := tlsConfig.ClientCAs.Subjects()
	if len(subjects) == 0 {
		t.Error("expected CA to be loaded into ClientCAs pool")
	}

	// Verify the loaded CA matches
	if !tlsConfig.ClientCAs.AppendCertsFromPEM(caPEM) {
		// Second append should succeed if CA is valid
		t.Error("CA certificate appears invalid")
	}

	// Verify certificate is in the pool
	opts := x509.VerifyOptions{
		Roots: tlsConfig.ClientCAs,
	}
	if _, err := caCert.Verify(opts); err != nil {
		t.Errorf("CA cert verification failed: %v", err)
	}
}

func TestApplyClientAuthConfig_OptionalWithCA(t *testing.T) {
	_, caPEM := generateCACertificate(t)

	server := &HTTPServer{
		logger: zap.NewNop(),
	}

	tlsConfig := &tls.Config{}
	clientAuth := &pb.ClientAuthConfig{
		Mode:   "optional",
		CaCert: caPEM,
	}

	err := server.applyClientAuthConfig(tlsConfig, clientAuth)
	if err != nil {
		t.Fatalf("applyClientAuthConfig() error = %v", err)
	}

	if tlsConfig.ClientAuth != tls.VerifyClientCertIfGiven {
		t.Errorf("ClientAuth = %v, want %v", tlsConfig.ClientAuth, tls.VerifyClientCertIfGiven)
	}

	if tlsConfig.ClientCAs == nil {
		t.Fatal("ClientCAs should be set")
	}
}

func TestApplyClientAuthConfig_RequireWithoutCA(t *testing.T) {
	server := &HTTPServer{
		logger: zap.NewNop(),
	}

	tlsConfig := &tls.Config{}
	clientAuth := &pb.ClientAuthConfig{
		Mode: "require",
	}

	err := server.applyClientAuthConfig(tlsConfig, clientAuth)
	if err == nil {
		t.Fatal("expected error when requiring mTLS without CA certificate")
	}

	expectedMsg := "mTLS mode 'require' needs a CA certificate bundle but none was provided"
	if err.Error() != expectedMsg {
		t.Errorf("error message = %q, want %q", err.Error(), expectedMsg)
	}
}

func TestApplyClientAuthConfig_InvalidCABundle(t *testing.T) {
	server := &HTTPServer{
		logger: zap.NewNop(),
	}

	tlsConfig := &tls.Config{}
	clientAuth := &pb.ClientAuthConfig{
		Mode:   "require",
		CaCert: []byte("invalid PEM data"),
	}

	err := server.applyClientAuthConfig(tlsConfig, clientAuth)
	if err == nil {
		t.Fatal("expected error for invalid CA certificate bundle")
	}

	expectedMsg := "failed to parse client CA certificate bundle"
	if err.Error() != expectedMsg {
		t.Errorf("error message = %q, want %q", err.Error(), expectedMsg)
	}
}

func TestApplyClientAuthConfig_OptionalWithoutCA(t *testing.T) {
	server := &HTTPServer{
		logger: zap.NewNop(),
	}

	tlsConfig := &tls.Config{}
	clientAuth := &pb.ClientAuthConfig{
		Mode: "optional",
	}

	err := server.applyClientAuthConfig(tlsConfig, clientAuth)
	if err != nil {
		t.Errorf("applyClientAuthConfig() error = %v, want nil", err)
	}

	if tlsConfig.ClientAuth != tls.VerifyClientCertIfGiven {
		t.Errorf("ClientAuth = %v, want %v", tlsConfig.ClientAuth, tls.VerifyClientCertIfGiven)
	}

	// ClientCAs should not be set without CA cert
	if tlsConfig.ClientCAs != nil {
		t.Error("ClientCAs should be nil without CA certificate")
	}
}

func TestInjectClientCertHeaders_NilTLS(t *testing.T) {
	req, err := http.NewRequest("GET", "https://example.com", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	// No TLS state
	req.TLS = nil

	injectClientCertHeaders(req)

	// Should not inject any headers
	for key := range req.Header {
		if len(key) > 14 && key[:14] == "X-Client-Cert-" {
			t.Errorf("unexpected client cert header: %s", key)
		}
	}
}

func TestInjectClientCertHeaders_NoPeerCertificates(t *testing.T) {
	req, err := http.NewRequest("GET", "https://example.com", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	req.TLS = &tls.ConnectionState{
		PeerCertificates: nil,
	}

	injectClientCertHeaders(req)

	// Should not inject any headers
	for key := range req.Header {
		if len(key) > 14 && key[:14] == "X-Client-Cert-" {
			t.Errorf("unexpected client cert header: %s", key)
		}
	}
}

func TestInjectClientCertHeaders_WithClientCert(t *testing.T) {
	// Generate a client certificate
	clientCert := generateClientCertificate(t, "client.example.com", []string{"client.example.com", "api.example.com"})

	req, err := http.NewRequest("GET", "https://example.com", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{clientCert},
	}

	injectClientCertHeaders(req)

	// Verify CN header
	if cn := req.Header.Get("X-Client-Cert-CN"); cn != "client.example.com" {
		t.Errorf("X-Client-Cert-CN = %q, want %q", cn, "client.example.com")
	}

	// Verify SAN DNS header
	sanDNS := req.Header.Get("X-Client-Cert-SAN-DNS")
	if sanDNS != "client.example.com,api.example.com" {
		t.Errorf("X-Client-Cert-SAN-DNS = %q, want %q", sanDNS, "client.example.com,api.example.com")
	}

	// Verify fingerprint header exists
	if fingerprint := req.Header.Get("X-Client-Cert-Fingerprint"); fingerprint == "" {
		t.Error("X-Client-Cert-Fingerprint should be set")
	}

	// Verify serial number header
	if serial := req.Header.Get("X-Client-Cert-Serial"); serial == "" {
		t.Error("X-Client-Cert-Serial should be set")
	}

	// Verify issuer header
	if issuer := req.Header.Get("X-Client-Cert-Issuer"); issuer == "" {
		t.Error("X-Client-Cert-Issuer should be set")
	}

	// Verify subject header
	if subject := req.Header.Get("X-Client-Cert-Subject"); subject == "" {
		t.Error("X-Client-Cert-Subject should be set")
	}
}

func TestInjectClientCertHeaders_WithEmailSAN(t *testing.T) {
	clientCert := generateClientCertWithEmail(t, "user", []string{"user@example.com", "admin@example.com"})

	req, err := http.NewRequest("GET", "https://example.com", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{clientCert},
	}

	injectClientCertHeaders(req)

	sanEmail := req.Header.Get("X-Client-Cert-SAN-Email")
	if sanEmail != "user@example.com,admin@example.com" {
		t.Errorf("X-Client-Cert-SAN-Email = %q, want %q", sanEmail, "user@example.com,admin@example.com")
	}
}

func TestInjectClientCertHeaders_WithURISAN(t *testing.T) {
	clientCert := generateClientCertWithURI(t, "workload", []string{"spiffe://example.org/workload/web"})

	req, err := http.NewRequest("GET", "https://example.com", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{clientCert},
	}

	injectClientCertHeaders(req)

	sanURI := req.Header.Get("X-Client-Cert-SAN-URI")
	if sanURI != "spiffe://example.org/workload/web" {
		t.Errorf("X-Client-Cert-SAN-URI = %q, want %q", sanURI, "spiffe://example.org/workload/web")
	}
}

func TestInjectClientCertHeaders_StripExistingHeaders(t *testing.T) {
	clientCert := generateClientCertificate(t, "client.example.com", []string{"client.example.com"})

	req, err := http.NewRequest("GET", "https://example.com", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	// Pre-populate with spoofed headers
	req.Header.Set("X-Client-Cert-CN", "spoofed.com")
	req.Header.Set("X-Client-Cert-Fingerprint", "spoofed-fingerprint")
	req.Header.Set("X-Client-Cert-Custom", "should-be-removed")

	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{clientCert},
	}

	injectClientCertHeaders(req)

	// Verify spoofed headers were replaced
	if cn := req.Header.Get("X-Client-Cert-CN"); cn != "client.example.com" {
		t.Errorf("X-Client-Cert-CN = %q, want %q (spoofed header not replaced)", cn, "client.example.com")
	}

	// Verify custom client cert header was removed
	if custom := req.Header.Get("X-Client-Cert-Custom"); custom != "" {
		t.Errorf("X-Client-Cert-Custom should be removed, got %q", custom)
	}
}

func TestInjectClientCertHeaders_EmptyCN(t *testing.T) {
	clientCert := generateClientCertificate(t, "", []string{"example.com"})

	req, err := http.NewRequest("GET", "https://example.com", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{clientCert},
	}

	injectClientCertHeaders(req)

	// Should not set CN header for empty CN
	if cn := req.Header.Get("X-Client-Cert-CN"); cn != "" {
		t.Errorf("X-Client-Cert-CN should not be set for empty CN, got %q", cn)
	}

	// Other headers should still be set
	if fingerprint := req.Header.Get("X-Client-Cert-Fingerprint"); fingerprint == "" {
		t.Error("X-Client-Cert-Fingerprint should be set even with empty CN")
	}
}

func TestInjectClientCertHeaders_MultipleSANs(t *testing.T) {
	clientCert := generateClientCertWithAllSANs(t)

	req, err := http.NewRequest("GET", "https://example.com", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{clientCert},
	}

	injectClientCertHeaders(req)

	// Verify all SAN types are present
	if sanDNS := req.Header.Get("X-Client-Cert-SAN-DNS"); sanDNS == "" {
		t.Error("X-Client-Cert-SAN-DNS should be set")
	}

	if sanEmail := req.Header.Get("X-Client-Cert-SAN-Email"); sanEmail == "" {
		t.Error("X-Client-Cert-SAN-Email should be set")
	}

	if sanURI := req.Header.Get("X-Client-Cert-SAN-URI"); sanURI == "" {
		t.Error("X-Client-Cert-SAN-URI should be set")
	}
}

// Helper functions for generating test certificates

func generateCACertificate(t *testing.T) (*x509.Certificate, []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate CA key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
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

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	return cert, certPEM
}

func generateClientCertificate(t *testing.T, cn string, dnsNames []string) *x509.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate client key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().Unix()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
		DNSNames:     dnsNames,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create client certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("failed to parse client certificate: %v", err)
	}

	return cert
}

func generateClientCertWithEmail(t *testing.T, cn string, emails []string) *x509.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate client key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:   big.NewInt(time.Now().Unix()),
		Subject:        pkix.Name{CommonName: cn},
		NotBefore:      time.Now(),
		NotAfter:       time.Now().Add(24 * time.Hour),
		EmailAddresses: emails,
		KeyUsage:       x509.KeyUsageDigitalSignature,
		ExtKeyUsage:    []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create client certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("failed to parse client certificate: %v", err)
	}

	return cert
}

func generateClientCertWithURI(t *testing.T, cn string, uris []string) *x509.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate client key: %v", err)
	}

	uriList := make([]*url.URL, len(uris))
	for i, u := range uris {
		parsed, err := url.Parse(u)
		if err != nil {
			t.Fatalf("failed to parse URI %s: %v", u, err)
		}
		uriList[i] = parsed
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().Unix()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
		URIs:         uriList,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create client certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("failed to parse client certificate: %v", err)
	}

	return cert
}

func generateClientCertWithAllSANs(t *testing.T) *x509.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate client key: %v", err)
	}

	spiffeURI, _ := url.Parse("spiffe://example.org/workload/test")

	template := &x509.Certificate{
		SerialNumber:   big.NewInt(time.Now().Unix()),
		Subject:        pkix.Name{CommonName: "multi-san-client"},
		NotBefore:      time.Now(),
		NotAfter:       time.Now().Add(24 * time.Hour),
		DNSNames:       []string{"client1.example.com", "client2.example.com"},
		EmailAddresses: []string{"user1@example.com", "user2@example.com"},
		URIs:           []*url.URL{spiffeURI},
		KeyUsage:       x509.KeyUsageDigitalSignature,
		ExtKeyUsage:    []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create client certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("failed to parse client certificate: %v", err)
	}

	return cert
}
