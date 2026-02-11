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

package policy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func generateTestCert(cn string, dnsNames []string, ipAddresses []net.IP) (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(1 * time.Hour),
		DNSNames:     dnsNames,
		IPAddresses:  ipAddresses,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		panic(err)
	}

	return cert, key, certDER
}

func TestMTLSValidator_RequiredMode_ValidCert(t *testing.T) {
	config := &pb.ClientAuthConfig{
		Mode:               "require",
		RequiredCnPatterns: []string{`^client\.example\.com$`},
	}

	logger := zap.NewNop()
	validator, err := NewMTLSValidator(config, logger)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	cert, _, _ := generateTestCert("client.example.com", nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
	}

	if err := validator.Validate(req); err != nil {
		t.Errorf("Expected valid cert to pass, got error: %v", err)
	}
}

func TestMTLSValidator_RequiredMode_MissingCert(t *testing.T) {
	config := &pb.ClientAuthConfig{
		Mode: "require",
	}

	logger := zap.NewNop()
	validator, err := NewMTLSValidator(config, logger)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	// Request without TLS
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	err = validator.Validate(req)
	if err == nil {
		t.Error("Expected error for missing cert in require mode")
	}
}

func TestMTLSValidator_RequiredMode_CNMismatch(t *testing.T) {
	config := &pb.ClientAuthConfig{
		Mode:               "require",
		RequiredCnPatterns: []string{`^allowed\.example\.com$`},
	}

	logger := zap.NewNop()
	validator, err := NewMTLSValidator(config, logger)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	cert, _, _ := generateTestCert("wrong.example.com", nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
	}

	err = validator.Validate(req)
	if err == nil {
		t.Error("Expected error for CN mismatch")
	}
}

func TestMTLSValidator_OptionalMode_NoCert(t *testing.T) {
	config := &pb.ClientAuthConfig{
		Mode: "optional",
	}

	logger := zap.NewNop()
	validator, err := NewMTLSValidator(config, logger)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	// Optional mode with no cert should pass
	if err := validator.Validate(req); err != nil {
		t.Errorf("Expected optional mode without cert to pass, got error: %v", err)
	}
}

func TestMTLSValidator_OptionalMode_ValidCert(t *testing.T) {
	config := &pb.ClientAuthConfig{
		Mode:               "optional",
		RequiredCnPatterns: []string{`^client\.example\.com$`},
	}

	logger := zap.NewNop()
	validator, err := NewMTLSValidator(config, logger)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	cert, _, _ := generateTestCert("client.example.com", nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
	}

	if err := validator.Validate(req); err != nil {
		t.Errorf("Expected valid cert in optional mode to pass, got error: %v", err)
	}
}

func TestMTLSValidator_RequiredSANs(t *testing.T) {
	config := &pb.ClientAuthConfig{
		Mode:         "require",
		RequiredSans: []string{"service.example.com"},
	}

	logger := zap.NewNop()
	validator, err := NewMTLSValidator(config, logger)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	// Cert with matching SAN
	cert, _, _ := generateTestCert("client", []string{"service.example.com"}, nil)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
	}

	if err := validator.Validate(req); err != nil {
		t.Errorf("Expected cert with matching SAN to pass, got error: %v", err)
	}

	// Cert without matching SAN
	cert2, _, _ := generateTestCert("client", []string{"other.example.com"}, nil)
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req2.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert2},
	}

	if err := validator.Validate(req2); err == nil {
		t.Error("Expected cert without required SAN to fail")
	}
}

func TestMTLSValidator_Middleware_Forbidden(t *testing.T) {
	config := &pb.ClientAuthConfig{
		Mode: "require",
	}

	logger := zap.NewNop()
	validator, err := NewMTLSValidator(config, logger)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	called := false
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	})

	handler := validator.Middleware(next)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil)

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d", w.Code)
	}
	if called {
		t.Error("Next handler should not have been called")
	}
}

func TestMTLSValidator_NoneMode(t *testing.T) {
	config := &pb.ClientAuthConfig{
		Mode: "none",
	}

	logger := zap.NewNop()
	validator, err := NewMTLSValidator(config, logger)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	if err := validator.Validate(req); err != nil {
		t.Errorf("Expected none mode to always pass, got error: %v", err)
	}
}
