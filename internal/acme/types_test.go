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

package acme

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func TestConfig_ApplyDefaults(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected Config
	}{
		{
			name:   "empty config",
			config: Config{},
			expected: Config{
				Server:        LetsEncryptProduction,
				KeyType:       KeyTypeEC256,
				ChallengeType: ChallengeHTTP01,
				RenewalDays:   30,
				Storage: StorageConfig{
					Type: "file",
					Path: "/var/lib/novaedge/certs",
				},
			},
		},
		{
			name: "partial config",
			config: Config{
				Email:   "test@example.com",
				KeyType: KeyTypeRSA4096,
			},
			expected: Config{
				Email:         "test@example.com",
				Server:        LetsEncryptProduction,
				KeyType:       KeyTypeRSA4096,
				ChallengeType: ChallengeHTTP01,
				RenewalDays:   30,
				Storage: StorageConfig{
					Type: "file",
					Path: "/var/lib/novaedge/certs",
				},
			},
		},
		{
			name: "full config",
			config: Config{
				Email:          "test@example.com",
				Server:         LetsEncryptStaging,
				KeyType:        KeyTypeRSA2048,
				ChallengeType:  ChallengeDNS01,
				RenewalDays:    14,
				Storage:        StorageConfig{Type: "kubernetes-secret", SecretNamespace: "certs"},
				DNSProvider:    "cloudflare",
				DNSCredentials: map[string]string{"API_KEY": "test"},
			},
			expected: Config{
				Email:          "test@example.com",
				Server:         LetsEncryptStaging,
				KeyType:        KeyTypeRSA2048,
				ChallengeType:  ChallengeDNS01,
				RenewalDays:    14,
				Storage:        StorageConfig{Type: "kubernetes-secret", SecretNamespace: "certs"},
				DNSProvider:    "cloudflare",
				DNSCredentials: map[string]string{"API_KEY": "test"},
			},
		},
		{
			name: "negative renewal days",
			config: Config{
				RenewalDays: -5,
			},
			expected: Config{
				Server:        LetsEncryptProduction,
				KeyType:       KeyTypeEC256,
				ChallengeType: ChallengeHTTP01,
				RenewalDays:   30,
				Storage: StorageConfig{
					Type: "file",
					Path: "/var/lib/novaedge/certs",
				},
			},
		},
		{
			name: "zero renewal days",
			config: Config{
				RenewalDays: 0,
			},
			expected: Config{
				Server:        LetsEncryptProduction,
				KeyType:       KeyTypeEC256,
				ChallengeType: ChallengeHTTP01,
				RenewalDays:   30,
				Storage: StorageConfig{
					Type: "file",
					Path: "/var/lib/novaedge/certs",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.config
			cfg.ApplyDefaults()

			if cfg.Server != tt.expected.Server {
				t.Errorf("Server = %q, want %q", cfg.Server, tt.expected.Server)
			}
			if cfg.KeyType != tt.expected.KeyType {
				t.Errorf("KeyType = %q, want %q", cfg.KeyType, tt.expected.KeyType)
			}
			if cfg.ChallengeType != tt.expected.ChallengeType {
				t.Errorf("ChallengeType = %q, want %q", cfg.ChallengeType, tt.expected.ChallengeType)
			}
			if cfg.RenewalDays != tt.expected.RenewalDays {
				t.Errorf("RenewalDays = %d, want %d", cfg.RenewalDays, tt.expected.RenewalDays)
			}
			if cfg.Storage.Type != tt.expected.Storage.Type {
				t.Errorf("Storage.Type = %q, want %q", cfg.Storage.Type, tt.expected.Storage.Type)
			}
			if cfg.Storage.Path != tt.expected.Storage.Path {
				t.Errorf("Storage.Path = %q, want %q", cfg.Storage.Path, tt.expected.Storage.Path)
			}
		})
	}
}

func TestCertificate_IsExpired(t *testing.T) {
	tests := []struct {
		name     string
		cert     Certificate
		expected bool
	}{
		{
			name: "not expired",
			cert: Certificate{
				NotAfter: time.Now().Add(24 * time.Hour),
			},
			expected: false,
		},
		{
			name: "expired",
			cert: Certificate{
				NotAfter: time.Now().Add(-24 * time.Hour),
			},
			expected: true,
		},
		{
			name: "expires now",
			cert: Certificate{
				NotAfter: time.Now(),
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.cert.IsExpired()
			if result != tt.expected {
				t.Errorf("IsExpired() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCertificate_ShouldRenew(t *testing.T) {
	tests := []struct {
		name        string
		notAfter    time.Time
		renewBefore time.Duration
		expected    bool
	}{
		{
			name:        "no renewal needed",
			notAfter:    time.Now().Add(90 * 24 * time.Hour),
			renewBefore: 30 * 24 * time.Hour,
			expected:    false,
		},
		{
			name:        "renewal needed",
			notAfter:    time.Now().Add(15 * 24 * time.Hour),
			renewBefore: 30 * 24 * time.Hour,
			expected:    true,
		},
		{
			name:        "exactly at threshold",
			notAfter:    time.Now().Add(30 * 24 * time.Hour),
			renewBefore: 30 * 24 * time.Hour,
			expected:    true,
		},
		{
			name:        "already expired",
			notAfter:    time.Now().Add(-1 * time.Hour),
			renewBefore: 30 * 24 * time.Hour,
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cert := Certificate{NotAfter: tt.notAfter}
			result := cert.ShouldRenew(tt.renewBefore)
			if result != tt.expected {
				t.Errorf("ShouldRenew() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCertificate_ExpiresIn(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		notAfter  time.Time
		minDur    time.Duration
		maxDur    time.Duration
	}{
		{
			name:     "1 hour from now",
			notAfter: now.Add(1 * time.Hour),
			minDur:   59 * time.Minute,
			maxDur:   61 * time.Minute,
		},
		{
			name:     "24 hours from now",
			notAfter: now.Add(24 * time.Hour),
			minDur:   23 * time.Hour,
			maxDur:   25 * time.Hour,
		},
		{
			name:     "already expired",
			notAfter: now.Add(-1 * time.Hour),
			minDur:   -2 * time.Hour,
			maxDur:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cert := Certificate{NotAfter: tt.notAfter}
			result := cert.ExpiresIn()
			if result < tt.minDur || result > tt.maxDur {
				t.Errorf("ExpiresIn() = %v, want between %v and %v", result, tt.minDur, tt.maxDur)
			}
		})
	}
}

func generateTestCertificate(t *testing.T) ([]byte, []byte) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "test.example.com",
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(24 * time.Hour),
		KeyUsage:  x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		DNSNames: []string{"test.example.com"},
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

func TestCertificate_TLSCertificate(t *testing.T) {
	certPEM, keyPEM := generateTestCertificate(t)

	cert := Certificate{
		Domains:        []string{"test.example.com"},
		CertificatePEM: certPEM,
		PrivateKeyPEM:  keyPEM,
		NotBefore:      time.Now(),
		NotAfter:       time.Now().Add(24 * time.Hour),
	}

	tlsCert, err := cert.TLSCertificate()
	if err != nil {
		t.Fatalf("TLSCertificate() error = %v", err)
	}

	if tlsCert == nil {
		t.Fatal("TLSCertificate() returned nil")
	}

	if len(tlsCert.Certificate) == 0 {
		t.Error("TLSCertificate() returned empty certificate chain")
	}
}

func TestCertificate_TLSCertificate_InvalidKey(t *testing.T) {
	certPEM, _ := generateTestCertificate(t)

	cert := Certificate{
		Domains:        []string{"test.example.com"},
		CertificatePEM: certPEM,
		PrivateKeyPEM:  []byte("invalid private key"),
	}

	_, err := cert.TLSCertificate()
	if err == nil {
		t.Error("TLSCertificate() should return error for invalid private key")
	}
}

func TestCertificate_TLSCertificate_InvalidCert(t *testing.T) {
	_, keyPEM := generateTestCertificate(t)

	cert := Certificate{
		Domains:        []string{"test.example.com"},
		CertificatePEM: []byte("invalid certificate"),
		PrivateKeyPEM:  keyPEM,
	}

	_, err := cert.TLSCertificate()
	if err == nil {
		t.Error("TLSCertificate() should return error for invalid certificate")
	}
}

func TestConstants(t *testing.T) {
	// Verify ACME server URLs
	if LetsEncryptProduction != "https://acme-v02.api.letsencrypt.org/directory" {
		t.Errorf("LetsEncryptProduction = %q", LetsEncryptProduction)
	}
	if LetsEncryptStaging != "https://acme-staging-v02.api.letsencrypt.org/directory" {
		t.Errorf("LetsEncryptStaging = %q", LetsEncryptStaging)
	}
	if ZeroSSLProduction != "https://acme.zerossl.com/v2/DV90" {
		t.Errorf("ZeroSSLProduction = %q", ZeroSSLProduction)
	}

	// Verify challenge types
	if ChallengeHTTP01 != "http-01" {
		t.Errorf("ChallengeHTTP01 = %q", ChallengeHTTP01)
	}
	if ChallengeDNS01 != "dns-01" {
		t.Errorf("ChallengeDNS01 = %q", ChallengeDNS01)
	}
	if ChallengeTLSALPN01 != "tls-alpn-01" {
		t.Errorf("ChallengeTLSALPN01 = %q", ChallengeTLSALPN01)
	}

	// Verify key types
	if KeyTypeRSA2048 != "RSA2048" {
		t.Errorf("KeyTypeRSA2048 = %q", KeyTypeRSA2048)
	}
	if KeyTypeRSA4096 != "RSA4096" {
		t.Errorf("KeyTypeRSA4096 = %q", KeyTypeRSA4096)
	}
	if KeyTypeEC256 != "EC256" {
		t.Errorf("KeyTypeEC256 = %q", KeyTypeEC256)
	}
	if KeyTypeEC384 != "EC384" {
		t.Errorf("KeyTypeEC384 = %q", KeyTypeEC384)
	}

	// Verify defaults
	if DefaultServer != LetsEncryptProduction {
		t.Errorf("DefaultServer = %q", DefaultServer)
	}
	if DefaultKeyType != KeyTypeEC256 {
		t.Errorf("DefaultKeyType = %q", DefaultKeyType)
	}
	if DefaultChallenge != ChallengeHTTP01 {
		t.Errorf("DefaultChallenge = %q", DefaultChallenge)
	}
	if DefaultRenewalDays != 30 {
		t.Errorf("DefaultRenewalDays = %d", DefaultRenewalDays)
	}
	if DefaultStorageType != "file" {
		t.Errorf("DefaultStorageType = %q", DefaultStorageType)
	}
	if DefaultStoragePath != "/var/lib/novaedge/certs" {
		t.Errorf("DefaultStoragePath = %q", DefaultStoragePath)
	}
}
