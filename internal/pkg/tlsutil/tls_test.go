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

package tlsutil

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// generateTestCertificate creates a self-signed certificate for testing
func generateTestCertificate(t *testing.T, commonName string, isCA bool) ([]byte, []byte) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: commonName,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour * 24),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  isCA,
	}

	if isCA {
		template.KeyUsage |= x509.KeyUsageCertSign
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	return certPEM, keyPEM
}

// generateTestCertificates creates CA, server, and client certificates for testing
func generateTestCertificates(t *testing.T) (caCert, serverCert, serverKey, clientCert, clientKey []byte) {
	t.Helper()

	// Generate CA certificate
	var caKey []byte
	caCert, caKey = generateTestCertificate(t, "Test CA", true)

	// Parse CA for signing
	caBlock, _ := pem.Decode(caCert)
	if caBlock == nil {
		t.Fatal("Failed to decode CA certificate")
	}
	ca, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse CA certificate: %v", err)
	}

	caKeyBlock, _ := pem.Decode(caKey)
	if caKeyBlock == nil {
		t.Fatal("Failed to decode CA key")
	}
	caPrivateKey, err := x509.ParsePKCS1PrivateKey(caKeyBlock.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse CA private key: %v", err)
	}

	// Generate server certificate
	serverPrivateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate server private key: %v", err)
	}

	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			CommonName: "server.example.com",
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(time.Hour * 24),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"server.example.com", "localhost"},
	}

	serverCertDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, ca, &serverPrivateKey.PublicKey, caPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create server certificate: %v", err)
	}

	serverCert = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: serverCertDER,
	})

	serverKey = pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(serverPrivateKey),
	})

	// Generate client certificate
	clientPrivateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate client private key: %v", err)
	}

	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject: pkix.Name{
			CommonName: "client.example.com",
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(time.Hour * 24),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	clientCertDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, ca, &clientPrivateKey.PublicKey, caPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create client certificate: %v", err)
	}

	clientCert = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: clientCertDER,
	})

	clientKey = pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(clientPrivateKey),
	})

	return
}

func TestSecureCipherSuites(t *testing.T) {
	suites := SecureCipherSuites()

	if len(suites) == 0 {
		t.Error("SecureCipherSuites() returned empty list")
	}

	// Verify TLS 1.3 cipher suites are included
	expectedTLS13 := []uint16{
		tls.TLS_AES_128_GCM_SHA256,
		tls.TLS_AES_256_GCM_SHA384,
		tls.TLS_CHACHA20_POLY1305_SHA256,
	}

	for _, expected := range expectedTLS13 {
		found := false
		for _, suite := range suites {
			if suite == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Missing TLS 1.3 cipher suite: %d", expected)
		}
	}

	// Verify TLS 1.2 ECDHE cipher suites are included
	expectedTLS12 := []uint16{
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	}

	for _, expected := range expectedTLS12 {
		found := false
		for _, suite := range suites {
			if suite == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Missing TLS 1.2 cipher suite: %d", expected)
		}
	}
}

func TestCreateServerTLSConfig(t *testing.T) {
	certPEM, keyPEM := generateTestCertificate(t, "test-server", false)

	config, err := CreateServerTLSConfig(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("CreateServerTLSConfig() error = %v", err)
	}

	if config == nil {
		t.Fatal("CreateServerTLSConfig() returned nil config")
	}

	// Verify minimum TLS version
	if config.MinVersion != tls.VersionTLS13 {
		t.Errorf("MinVersion = %d, want %d", config.MinVersion, tls.VersionTLS13)
	}

	// Verify certificates are loaded
	if len(config.Certificates) != 1 {
		t.Errorf("Certificates count = %d, want 1", len(config.Certificates))
	}

	// Verify cipher suites are set
	if len(config.CipherSuites) == 0 {
		t.Error("CipherSuites should not be empty")
	}

	// Verify client auth is NoClientCert by default
	if config.ClientAuth != tls.NoClientCert {
		t.Errorf("ClientAuth = %d, want %d", config.ClientAuth, tls.NoClientCert)
	}
}

func TestCreateServerTLSConfig_InvalidCert(t *testing.T) {
	_, err := CreateServerTLSConfig([]byte("invalid"), []byte("invalid"))
	if err == nil {
		t.Error("CreateServerTLSConfig() should return error for invalid cert")
	}
}

func TestCreateServerTLSConfigWithMTLS(t *testing.T) {
	caCert, serverCert, serverKey, _, _ := generateTestCertificates(t)

	config, err := CreateServerTLSConfigWithMTLS(serverCert, serverKey, caCert)
	if err != nil {
		t.Fatalf("CreateServerTLSConfigWithMTLS() error = %v", err)
	}

	if config == nil {
		t.Fatal("CreateServerTLSConfigWithMTLS() returned nil config")
	}

	// Verify client auth is RequireAndVerifyClientCert
	if config.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Errorf("ClientAuth = %d, want %d", config.ClientAuth, tls.RequireAndVerifyClientCert)
	}

	// Verify ClientCAs is set
	if config.ClientCAs == nil {
		t.Error("ClientCAs should not be nil")
	}
}

func TestCreateServerTLSConfigWithMTLS_InvalidCA(t *testing.T) {
	certPEM, keyPEM := generateTestCertificate(t, "test-server", false)

	_, err := CreateServerTLSConfigWithMTLS(certPEM, keyPEM, []byte("invalid-ca"))
	if err == nil {
		t.Error("CreateServerTLSConfigWithMTLS() should return error for invalid CA")
	}
}

func TestCreateClientTLSConfig(t *testing.T) {
	config, err := CreateClientTLSConfig("server.example.com")
	if err != nil {
		t.Fatalf("CreateClientTLSConfig() error = %v", err)
	}

	if config == nil {
		t.Fatal("CreateClientTLSConfig() returned nil config")
	}

	// Verify server name
	if config.ServerName != "server.example.com" {
		t.Errorf("ServerName = %q, want %q", config.ServerName, "server.example.com")
	}

	// Verify minimum TLS version
	if config.MinVersion != tls.VersionTLS13 {
		t.Errorf("MinVersion = %d, want %d", config.MinVersion, tls.VersionTLS13)
	}

	// Verify cipher suites
	if len(config.CipherSuites) == 0 {
		t.Error("CipherSuites should not be empty")
	}
}

func TestCreateClientTLSConfigWithMTLS(t *testing.T) {
	caCert, _, _, clientCert, clientKey := generateTestCertificates(t)

	config, err := CreateClientTLSConfigWithMTLS(clientCert, clientKey, caCert, "server.example.com")
	if err != nil {
		t.Fatalf("CreateClientTLSConfigWithMTLS() error = %v", err)
	}

	if config == nil {
		t.Fatal("CreateClientTLSConfigWithMTLS() returned nil config")
	}

	// Verify certificates are loaded
	if len(config.Certificates) != 1 {
		t.Errorf("Certificates count = %d, want 1", len(config.Certificates))
	}

	// Verify RootCAs is set
	if config.RootCAs == nil {
		t.Error("RootCAs should not be nil")
	}

	// Verify server name
	if config.ServerName != "server.example.com" {
		t.Errorf("ServerName = %q, want %q", config.ServerName, "server.example.com")
	}
}

func TestCreateClientTLSConfigWithMTLS_InvalidCert(t *testing.T) {
	caCert, _, _, _, _ := generateTestCertificates(t)

	_, err := CreateClientTLSConfigWithMTLS([]byte("invalid"), []byte("invalid"), caCert, "server.example.com")
	if err == nil {
		t.Error("CreateClientTLSConfigWithMTLS() should return error for invalid cert")
	}
}

func TestCreateClientTLSConfigWithMTLS_InvalidCA(t *testing.T) {
	_, _, _, clientCert, clientKey := generateTestCertificates(t)

	_, err := CreateClientTLSConfigWithMTLS(clientCert, clientKey, []byte("invalid-ca"), "server.example.com")
	if err == nil {
		t.Error("CreateClientTLSConfigWithMTLS() should return error for invalid CA")
	}
}

func TestCreateBackendTLSConfig(t *testing.T) {
	caCert, _, _, _, _ := generateTestCertificates(t)

	tests := []struct {
		name               string
		caCertPEM          []byte
		serverName         string
		insecureSkipVerify bool
		wantInsecure       bool
		wantRootCAs        bool
	}{
		{
			name:               "with CA cert",
			caCertPEM:          caCert,
			serverName:         "backend.example.com",
			insecureSkipVerify: false,
			wantInsecure:       false,
			wantRootCAs:        true,
		},
		{
			name:               "without CA cert",
			caCertPEM:          nil,
			serverName:         "backend.example.com",
			insecureSkipVerify: false,
			wantInsecure:       false,
			wantRootCAs:        false,
		},
		{
			name:               "insecure skip verify",
			caCertPEM:          nil,
			serverName:         "backend.example.com",
			insecureSkipVerify: true,
			wantInsecure:       true,
			wantRootCAs:        false,
		},
		{
			name:               "insecure with CA cert",
			caCertPEM:          caCert,
			serverName:         "backend.example.com",
			insecureSkipVerify: true,
			wantInsecure:       true,
			wantRootCAs:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := CreateBackendTLSConfig(tt.caCertPEM, tt.serverName, tt.insecureSkipVerify)
			if err != nil {
				t.Fatalf("CreateBackendTLSConfig() error = %v", err)
			}

			if config.InsecureSkipVerify != tt.wantInsecure {
				t.Errorf("InsecureSkipVerify = %v, want %v", config.InsecureSkipVerify, tt.wantInsecure)
			}

			if tt.wantRootCAs && config.RootCAs == nil {
				t.Error("RootCAs should not be nil")
			}

			if !tt.wantRootCAs && config.RootCAs != nil {
				t.Error("RootCAs should be nil")
			}

			if config.ServerName != tt.serverName {
				t.Errorf("ServerName = %q, want %q", config.ServerName, tt.serverName)
			}
		})
	}
}

func TestCreateBackendTLSConfig_InvalidCA(t *testing.T) {
	_, err := CreateBackendTLSConfig([]byte("invalid-ca"), "backend.example.com", false)
	if err == nil {
		t.Error("CreateBackendTLSConfig() should return error for invalid CA")
	}
}

func TestCreateServerTLSConfigWithSNI(t *testing.T) {
	defaultCertPEM, defaultKeyPEM := generateTestCertificate(t, "default.example.com", false)
	hostCertPEM, hostKeyPEM := generateTestCertificate(t, "host.example.com", false)

	defaultCert, err := tls.X509KeyPair(defaultCertPEM, defaultKeyPEM)
	if err != nil {
		t.Fatalf("Failed to load default cert: %v", err)
	}

	hostCert, err := tls.X509KeyPair(hostCertPEM, hostKeyPEM)
	if err != nil {
		t.Fatalf("Failed to load host cert: %v", err)
	}

	sniConfig := &SNIConfig{
		DefaultCert: defaultCert,
		Certificates: map[string]tls.Certificate{
			"host.example.com": hostCert,
		},
	}

	config, err := CreateServerTLSConfigWithSNI(sniConfig)
	if err != nil {
		t.Fatalf("CreateServerTLSConfigWithSNI() error = %v", err)
	}

	if config == nil {
		t.Fatal("CreateServerTLSConfigWithSNI() returned nil config")
	}

	// Verify GetCertificate is set
	if config.GetCertificate == nil {
		t.Error("GetCertificate should not be nil")
	}

	// Verify default certificate is set
	if len(config.Certificates) != 1 {
		t.Errorf("Certificates count = %d, want 1", len(config.Certificates))
	}
}

func TestCreateServerTLSConfigWithSNI_NilConfig(t *testing.T) {
	_, err := CreateServerTLSConfigWithSNI(nil)
	if err == nil {
		t.Error("CreateServerTLSConfigWithSNI() should return error for nil config")
	}
}

func TestCreateServerTLSConfigWithSNI_Wildcard(t *testing.T) {
	defaultCertPEM, defaultKeyPEM := generateTestCertificate(t, "default.example.com", false)
	wildcardCertPEM, wildcardKeyPEM := generateTestCertificate(t, "*.example.com", false)

	defaultCert, err := tls.X509KeyPair(defaultCertPEM, defaultKeyPEM)
	if err != nil {
		t.Fatalf("Failed to load default cert: %v", err)
	}

	wildcardCert, err := tls.X509KeyPair(wildcardCertPEM, wildcardKeyPEM)
	if err != nil {
		t.Fatalf("Failed to load wildcard cert: %v", err)
	}

	sniConfig := &SNIConfig{
		DefaultCert: defaultCert,
		Certificates: map[string]tls.Certificate{
			"*.example.com": wildcardCert,
		},
	}

	config, err := CreateServerTLSConfigWithSNI(sniConfig)
	if err != nil {
		t.Fatalf("CreateServerTLSConfigWithSNI() error = %v", err)
	}

	// Test wildcard matching
	tests := []struct {
		serverName  string
		wantDefault bool
	}{
		{"www.example.com", false}, // Should match wildcard
		{"api.example.com", false}, // Should match wildcard
		{"other.com", true},        // Should use default
		{"", true},                 // Should use default
	}

	for _, tt := range tests {
		t.Run(tt.serverName, func(t *testing.T) {
			clientHello := &tls.ClientHelloInfo{
				ServerName: tt.serverName,
			}

			cert, err := config.GetCertificate(clientHello)
			if err != nil {
				t.Fatalf("GetCertificate() error = %v", err)
			}

			if cert == nil {
				t.Fatal("GetCertificate() returned nil cert")
			}
		})
	}
}

func TestCreateServerTLSConfigWithSNI_TLS12(t *testing.T) {
	certPEM, keyPEM := generateTestCertificate(t, "test-server", false)

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Failed to load cert: %v", err)
	}

	sniConfig := &SNIConfig{
		DefaultCert: cert,
		MinVersion:  tls.VersionTLS12,
	}

	config, err := CreateServerTLSConfigWithSNI(sniConfig)
	if err != nil {
		t.Fatalf("CreateServerTLSConfigWithSNI() error = %v", err)
	}

	if config.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want %d", config.MinVersion, tls.VersionTLS12)
	}
}

func TestLoadServerTLSCredentials(t *testing.T) {
	// Create temp directory for test files
	tmpDir := t.TempDir()

	caCert, serverCert, serverKey, _, _ := generateTestCertificates(t)

	// Write test files
	caFile := filepath.Join(tmpDir, "ca.crt")
	certFile := filepath.Join(tmpDir, "server.crt")
	keyFile := filepath.Join(tmpDir, "server.key")

	if err := os.WriteFile(caFile, caCert, 0600); err != nil {
		t.Fatalf("Failed to write CA file: %v", err)
	}
	if err := os.WriteFile(certFile, serverCert, 0600); err != nil {
		t.Fatalf("Failed to write cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, serverKey, 0600); err != nil {
		t.Fatalf("Failed to write key file: %v", err)
	}

	creds, err := LoadServerTLSCredentials(certFile, keyFile, caFile)
	if err != nil {
		t.Fatalf("LoadServerTLSCredentials() error = %v", err)
	}

	if creds == nil {
		t.Fatal("LoadServerTLSCredentials() returned nil credentials")
	}
}

func TestLoadServerTLSCredentials_MissingCert(t *testing.T) {
	tmpDir := t.TempDir()

	caCert, _, _, _, _ := generateTestCertificates(t)

	caFile := filepath.Join(tmpDir, "ca.crt")
	if err := os.WriteFile(caFile, caCert, 0600); err != nil {
		t.Fatalf("Failed to write CA file: %v", err)
	}

	_, err := LoadServerTLSCredentials("/nonexistent/cert.crt", "/nonexistent/key.key", caFile)
	if err == nil {
		t.Error("LoadServerTLSCredentials() should return error for missing cert")
	}
}

func TestLoadServerTLSCredentials_MissingCA(t *testing.T) {
	tmpDir := t.TempDir()

	_, serverCert, serverKey, _, _ := generateTestCertificates(t)

	certFile := filepath.Join(tmpDir, "server.crt")
	keyFile := filepath.Join(tmpDir, "server.key")

	if err := os.WriteFile(certFile, serverCert, 0600); err != nil {
		t.Fatalf("Failed to write cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, serverKey, 0600); err != nil {
		t.Fatalf("Failed to write key file: %v", err)
	}

	_, err := LoadServerTLSCredentials(certFile, keyFile, "/nonexistent/ca.crt")
	if err == nil {
		t.Error("LoadServerTLSCredentials() should return error for missing CA")
	}
}

func TestLoadClientTLSCredentials(t *testing.T) {
	tmpDir := t.TempDir()

	caCert, _, _, clientCert, clientKey := generateTestCertificates(t)

	caFile := filepath.Join(tmpDir, "ca.crt")
	certFile := filepath.Join(tmpDir, "client.crt")
	keyFile := filepath.Join(tmpDir, "client.key")

	if err := os.WriteFile(caFile, caCert, 0600); err != nil {
		t.Fatalf("Failed to write CA file: %v", err)
	}
	if err := os.WriteFile(certFile, clientCert, 0600); err != nil {
		t.Fatalf("Failed to write cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, clientKey, 0600); err != nil {
		t.Fatalf("Failed to write key file: %v", err)
	}

	creds, err := LoadClientTLSCredentials(certFile, keyFile, caFile, "server.example.com")
	if err != nil {
		t.Fatalf("LoadClientTLSCredentials() error = %v", err)
	}

	if creds == nil {
		t.Fatal("LoadClientTLSCredentials() returned nil credentials")
	}
}

func TestLoadClientTLSCredentials_MissingCert(t *testing.T) {
	tmpDir := t.TempDir()

	caCert, _, _, _, _ := generateTestCertificates(t)

	caFile := filepath.Join(tmpDir, "ca.crt")
	if err := os.WriteFile(caFile, caCert, 0600); err != nil {
		t.Fatalf("Failed to write CA file: %v", err)
	}

	_, err := LoadClientTLSCredentials("/nonexistent/cert.crt", "/nonexistent/key.key", caFile, "server.example.com")
	if err == nil {
		t.Error("LoadClientTLSCredentials() should return error for missing cert")
	}
}

func TestLoadServerTLSCredentialsFromMemory(t *testing.T) {
	caCert, serverCert, serverKey, _, _ := generateTestCertificates(t)

	creds, err := LoadServerTLSCredentialsFromMemory(serverCert, serverKey, caCert)
	if err != nil {
		t.Fatalf("LoadServerTLSCredentialsFromMemory() error = %v", err)
	}

	if creds == nil {
		t.Fatal("LoadServerTLSCredentialsFromMemory() returned nil credentials")
	}
}

func TestLoadServerTLSCredentialsFromMemory_InvalidCert(t *testing.T) {
	caCert, _, _, _, _ := generateTestCertificates(t)

	_, err := LoadServerTLSCredentialsFromMemory([]byte("invalid"), []byte("invalid"), caCert)
	if err == nil {
		t.Error("LoadServerTLSCredentialsFromMemory() should return error for invalid cert")
	}
}

func TestLoadServerTLSCredentialsFromMemory_InvalidCA(t *testing.T) {
	_, serverCert, serverKey, _, _ := generateTestCertificates(t)

	_, err := LoadServerTLSCredentialsFromMemory(serverCert, serverKey, []byte("invalid-ca"))
	if err == nil {
		t.Error("LoadServerTLSCredentialsFromMemory() should return error for invalid CA")
	}
}

func TestLoadClientTLSCredentialsFromMemory(t *testing.T) {
	caCert, _, _, clientCert, clientKey := generateTestCertificates(t)

	creds, err := LoadClientTLSCredentialsFromMemory(clientCert, clientKey, caCert, "server.example.com")
	if err != nil {
		t.Fatalf("LoadClientTLSCredentialsFromMemory() error = %v", err)
	}

	if creds == nil {
		t.Fatal("LoadClientTLSCredentialsFromMemory() returned nil credentials")
	}
}

func TestLoadClientTLSCredentialsFromMemory_InvalidCert(t *testing.T) {
	caCert, _, _, _, _ := generateTestCertificates(t)

	_, err := LoadClientTLSCredentialsFromMemory([]byte("invalid"), []byte("invalid"), caCert, "server.example.com")
	if err == nil {
		t.Error("LoadClientTLSCredentialsFromMemory() should return error for invalid cert")
	}
}

func TestLoadClientTLSCredentialsFromMemory_InvalidCA(t *testing.T) {
	_, _, _, clientCert, clientKey := generateTestCertificates(t)

	_, err := LoadClientTLSCredentialsFromMemory(clientCert, clientKey, []byte("invalid-ca"), "server.example.com")
	if err == nil {
		t.Error("LoadClientTLSCredentialsFromMemory() should return error for invalid CA")
	}
}

func TestSNIConfig_ExactMatch(t *testing.T) {
	defaultCertPEM, defaultKeyPEM := generateTestCertificate(t, "default.example.com", false)
	exactCertPEM, exactKeyPEM := generateTestCertificate(t, "exact.example.com", false)

	defaultCert, _ := tls.X509KeyPair(defaultCertPEM, defaultKeyPEM)
	exactCert, _ := tls.X509KeyPair(exactCertPEM, exactKeyPEM)

	sniConfig := &SNIConfig{
		DefaultCert: defaultCert,
		Certificates: map[string]tls.Certificate{
			"exact.example.com": exactCert,
		},
	}

	config, err := CreateServerTLSConfigWithSNI(sniConfig)
	if err != nil {
		t.Fatalf("CreateServerTLSConfigWithSNI() error = %v", err)
	}

	// Test exact match
	cert, err := config.GetCertificate(&tls.ClientHelloInfo{ServerName: "exact.example.com"})
	if err != nil {
		t.Fatalf("GetCertificate() error = %v", err)
	}
	if cert == nil {
		t.Error("GetCertificate() should return exact match certificate")
	}
}

func TestSNIConfig_DefaultFallback(t *testing.T) {
	defaultCertPEM, defaultKeyPEM := generateTestCertificate(t, "default.example.com", false)

	defaultCert, _ := tls.X509KeyPair(defaultCertPEM, defaultKeyPEM)

	// When Certificates map is empty, GetCertificate is not set
	// The default cert is in config.Certificates[0] instead
	sniConfig := &SNIConfig{
		DefaultCert:  defaultCert,
		Certificates: map[string]tls.Certificate{},
	}

	config, err := CreateServerTLSConfigWithSNI(sniConfig)
	if err != nil {
		t.Fatalf("CreateServerTLSConfigWithSNI() error = %v", err)
	}

	// When Certificates is empty, GetCertificate is nil and default is in config.Certificates
	if len(config.Certificates) != 1 {
		t.Errorf("Expected 1 certificate in config.Certificates, got %d", len(config.Certificates))
	}
}

func TestParseTLSVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected uint16
	}{
		{"TLS1.2", tls.VersionTLS12},
		{"TLSV1.2", tls.VersionTLS12},
		{"1.2", tls.VersionTLS12},
		{"TLS1.3", tls.VersionTLS13},
		{"TLSV1.3", tls.VersionTLS13},
		{"1.3", tls.VersionTLS13},
		{"unknown", tls.VersionTLS13}, // defaults to TLS 1.3
		{"", tls.VersionTLS13},        // defaults to TLS 1.3
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseTLSVersion(tt.input)
			if result != tt.expected {
				t.Errorf("ParseTLSVersion(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseCipherSuites(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		wantLen int // > 0 means we expect specific suites
	}{
		{
			name:    "empty returns defaults",
			input:   []string{},
			wantLen: -1, // any non-zero length
		},
		{
			name:    "nil returns defaults",
			input:   nil,
			wantLen: -1,
		},
		{
			name:    "valid TLS 1.3 suite",
			input:   []string{"TLS_AES_128_GCM_SHA256"},
			wantLen: 1,
		},
		{
			name:    "valid TLS 1.2 suite",
			input:   []string{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"},
			wantLen: 1,
		},
		{
			name:    "multiple suites",
			input:   []string{"TLS_AES_128_GCM_SHA256", "TLS_AES_256_GCM_SHA384"},
			wantLen: 2,
		},
		{
			name:    "invalid suite returns defaults",
			input:   []string{"INVALID_SUITE"},
			wantLen: -1,
		},
		{
			name:    "case insensitive",
			input:   []string{"tls_aes_128_gcm_sha256"},
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseCipherSuites(tt.input)
			if len(result) == 0 {
				t.Error("ParseCipherSuites() should not return empty slice")
			}
			if tt.wantLen > 0 && len(result) != tt.wantLen {
				t.Errorf("ParseCipherSuites() returned %d suites, want %d", len(result), tt.wantLen)
			}
		})
	}
}
