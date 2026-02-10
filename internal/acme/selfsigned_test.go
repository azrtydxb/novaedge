package acme

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"testing"
	"time"
)

func TestGenerateSelfSigned_WithDomains(t *testing.T) {
	config := &SelfSignedConfig{
		Domains:      []string{"example.com", "www.example.com"},
		Organization: "Test Org",
		Validity:     24 * time.Hour,
		KeyType:      KeyTypeEC256,
	}

	cert, err := GenerateSelfSigned(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cert == nil {
		t.Fatal("expected non-nil certificate")
	}
	if len(cert.CertificatePEM) == 0 {
		t.Error("expected non-empty certificate PEM")
	}
	if len(cert.PrivateKeyPEM) == 0 {
		t.Error("expected non-empty private key PEM")
	}
	if cert.Issuer != "Self-Signed" {
		t.Errorf("expected issuer 'Self-Signed', got %q", cert.Issuer)
	}

	// Parse and verify the certificate
	block, _ := pem.Decode(cert.CertificatePEM)
	if block == nil {
		t.Fatal("failed to decode certificate PEM")
	}
	x509Cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}

	if x509Cert.Subject.CommonName != "example.com" {
		t.Errorf("expected CN 'example.com', got %q", x509Cert.Subject.CommonName)
	}
	if len(x509Cert.DNSNames) != 2 {
		t.Errorf("expected 2 DNS names, got %d", len(x509Cert.DNSNames))
	}
}

func TestGenerateSelfSigned_WithIPs(t *testing.T) {
	config := &SelfSignedConfig{
		IPs:     []net.IP{net.ParseIP("10.0.0.1"), net.ParseIP("::1")},
		KeyType: KeyTypeEC256,
	}

	cert, err := GenerateSelfSigned(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	block, _ := pem.Decode(cert.CertificatePEM)
	x509Cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}

	if len(x509Cert.IPAddresses) != 2 {
		t.Errorf("expected 2 IP addresses, got %d", len(x509Cert.IPAddresses))
	}
}

func TestGenerateSelfSigned_NoDomainsOrIPs(t *testing.T) {
	config := &SelfSignedConfig{}

	_, err := GenerateSelfSigned(config)
	if err == nil {
		t.Fatal("expected error for empty domains and IPs")
	}
}

func TestGenerateSelfSigned_RSA2048(t *testing.T) {
	config := &SelfSignedConfig{
		Domains: []string{"rsa.example.com"},
		KeyType: KeyTypeRSA2048,
	}

	cert, err := GenerateSelfSigned(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify key type by checking the PEM block type
	block, _ := pem.Decode(cert.PrivateKeyPEM)
	if block == nil {
		t.Fatal("failed to decode private key PEM")
	}
	if block.Type != "RSA PRIVATE KEY" {
		t.Errorf("expected RSA PRIVATE KEY, got %q", block.Type)
	}
}

func TestGenerateSelfSigned_RSA4096(t *testing.T) {
	config := &SelfSignedConfig{
		Domains: []string{"rsa4096.example.com"},
		KeyType: KeyTypeRSA4096,
	}

	cert, err := GenerateSelfSigned(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	block, _ := pem.Decode(cert.PrivateKeyPEM)
	if block == nil {
		t.Fatal("failed to decode private key PEM")
	}
	if block.Type != "RSA PRIVATE KEY" {
		t.Errorf("expected RSA PRIVATE KEY, got %q", block.Type)
	}
}

func TestGenerateSelfSigned_EC384(t *testing.T) {
	config := &SelfSignedConfig{
		Domains: []string{"ec384.example.com"},
		KeyType: KeyTypeEC384,
	}

	cert, err := GenerateSelfSigned(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	block, _ := pem.Decode(cert.PrivateKeyPEM)
	if block == nil {
		t.Fatal("failed to decode private key PEM")
	}
	if block.Type != "EC PRIVATE KEY" {
		t.Errorf("expected EC PRIVATE KEY, got %q", block.Type)
	}
}

func TestGenerateSelfSigned_UnsupportedKeyType(t *testing.T) {
	config := &SelfSignedConfig{
		Domains: []string{"bad.example.com"},
		KeyType: "INVALID_KEY_TYPE",
	}

	_, err := GenerateSelfSigned(config)
	if err == nil {
		t.Fatal("expected error for unsupported key type")
	}
}

func TestGenerateSelfSigned_DefaultValues(t *testing.T) {
	config := &SelfSignedConfig{
		Domains: []string{"defaults.example.com"},
	}

	cert, err := GenerateSelfSigned(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Defaults should be applied: 1 year validity, EC256
	expectedExpiry := time.Now().Add(365 * 24 * time.Hour)
	diff := cert.NotAfter.Sub(expectedExpiry)
	if diff > 5*time.Second || diff < -5*time.Second {
		t.Errorf("expected ~1 year validity, got NotAfter=%v", cert.NotAfter)
	}

	block, _ := pem.Decode(cert.PrivateKeyPEM)
	if block.Type != "EC PRIVATE KEY" {
		t.Errorf("expected default EC key, got %q", block.Type)
	}
}

func TestGenerateSelfSigned_CACertificate(t *testing.T) {
	config := &SelfSignedConfig{
		Domains:      []string{"CA"},
		Organization: "Test CA",
		IsCA:         true,
		KeyType:      KeyTypeEC256,
	}

	cert, err := GenerateSelfSigned(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	block, _ := pem.Decode(cert.CertificatePEM)
	x509Cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}

	if !x509Cert.IsCA {
		t.Error("expected CA certificate")
	}
	if x509Cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Error("expected cert signing key usage for CA")
	}
}

func TestGenerateCA(t *testing.T) {
	cert, err := GenerateCA("Test Organization", 365*24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	block, _ := pem.Decode(cert.CertificatePEM)
	x509Cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse CA certificate: %v", err)
	}

	if !x509Cert.IsCA {
		t.Error("expected CA certificate from GenerateCA")
	}
}

func TestSignCertificate(t *testing.T) {
	// Generate CA
	caCert, err := GenerateCA("Test CA", 365*24*time.Hour)
	if err != nil {
		t.Fatalf("failed to generate CA: %v", err)
	}

	// Sign a certificate with the CA
	config := &SelfSignedConfig{
		Domains:      []string{"signed.example.com"},
		Organization: "Test Org",
		Validity:     24 * time.Hour,
		KeyType:      KeyTypeEC256,
	}

	cert, err := SignCertificate(caCert, config)
	if err != nil {
		t.Fatalf("failed to sign certificate: %v", err)
	}

	if cert == nil {
		t.Fatal("expected non-nil certificate")
	}

	// Verify the certificate was signed by the CA
	block, _ := pem.Decode(cert.CertificatePEM)
	x509Cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse signed certificate: %v", err)
	}

	caBlock, _ := pem.Decode(caCert.CertificatePEM)
	caX509, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		t.Fatalf("failed to parse CA certificate: %v", err)
	}

	// Verify chain
	pool := x509.NewCertPool()
	pool.AddCert(caX509)
	_, err = x509Cert.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	if err != nil {
		t.Fatalf("certificate verification failed: %v", err)
	}
}

func TestSignCertificate_NoDomainsOrIPs(t *testing.T) {
	caCert, err := GenerateCA("Test CA", 365*24*time.Hour)
	if err != nil {
		t.Fatalf("failed to generate CA: %v", err)
	}

	config := &SelfSignedConfig{}
	_, err = SignCertificate(caCert, config)
	if err == nil {
		t.Fatal("expected error for empty domains and IPs")
	}
}

func TestQuickSelfSigned_Default(t *testing.T) {
	cert, err := QuickSelfSigned()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	block, _ := pem.Decode(cert.CertificatePEM)
	x509Cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}

	if x509Cert.Subject.CommonName != "localhost" {
		t.Errorf("expected CN 'localhost', got %q", x509Cert.Subject.CommonName)
	}
}

func TestQuickSelfSigned_CustomDomains(t *testing.T) {
	cert, err := QuickSelfSigned("myapp.local", "api.myapp.local")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	block, _ := pem.Decode(cert.CertificatePEM)
	x509Cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}

	if x509Cert.Subject.CommonName != "myapp.local" {
		t.Errorf("expected CN 'myapp.local', got %q", x509Cert.Subject.CommonName)
	}
	if len(x509Cert.DNSNames) != 2 {
		t.Errorf("expected 2 DNS names, got %d", len(x509Cert.DNSNames))
	}
}

func TestGenerateSelfSigned_TLSCertificate(t *testing.T) {
	cert, err := QuickSelfSigned("tls.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the certificate can be used as tls.Certificate
	tlsCert, err := cert.TLSCertificate()
	if err != nil {
		t.Fatalf("failed to create TLS certificate: %v", err)
	}
	if tlsCert == nil {
		t.Fatal("expected non-nil TLS certificate")
	}
}
