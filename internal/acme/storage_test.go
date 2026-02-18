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
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewFileStorage(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
		wantErr  bool
	}{
		{
			name:     "valid path",
			basePath: filepath.Join(t.TempDir(), "acme-storage"),
			wantErr:  false,
		},
		{
			name:     "empty path uses default",
			basePath: "",
			wantErr:  true, // Will fail because default path requires root permissions
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zap.NewNop()
			storage, err := NewFileStorage(tt.basePath, logger)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, storage)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, storage)
			}
		})
	}
}

func TestNewFileStorageWithNilLogger(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewFileStorage(tmpDir, nil)

	assert.NoError(t, err)
	assert.NotNil(t, storage)
}

func TestSaveCertificate(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zap.NewNop()
	storage, err := NewFileStorage(tmpDir, logger)
	require.NoError(t, err)

	cert := &Certificate{
		Domains:              []string{"example.com", "www.example.com"},
		CertificatePEM:       []byte("-----BEGIN CERTIFICATE-----\ntest-cert\n-----END CERTIFICATE-----"),
		PrivateKeyPEM:        []byte("-----BEGIN PRIVATE KEY-----\ntest-key\n-----END PRIVATE KEY-----"),
		IssuerCertificatePEM: []byte("-----BEGIN CERTIFICATE-----\nissuer-cert\n-----END CERTIFICATE-----"),
		NotBefore:            time.Now(),
		NotAfter:             time.Now().Add(90 * 24 * time.Hour),
		SerialNumber:         "1234567890",
		Issuer:               "Let's Encrypt",
	}

	ctx := context.Background()
	err = storage.SaveCertificate(ctx, cert)
	assert.NoError(t, err)

	// Verify files were created
	certPath := filepath.Join(tmpDir, "certs", "example_com", "cert.pem")
	_, err = os.Stat(certPath)
	assert.NoError(t, err)

	keyPath := filepath.Join(tmpDir, "certs", "example_com", "key.pem")
	_, err = os.Stat(keyPath)
	assert.NoError(t, err)

	issuerPath := filepath.Join(tmpDir, "certs", "example_com", "issuer.pem")
	_, err = os.Stat(issuerPath)
	assert.NoError(t, err)

	metaPath := filepath.Join(tmpDir, "certs", "example_com", "meta.json")
	_, err = os.Stat(metaPath)
	assert.NoError(t, err)
}

func TestSaveCertificateNoDomains(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zap.NewNop()
	storage, err := NewFileStorage(tmpDir, logger)
	require.NoError(t, err)

	cert := &Certificate{
		Domains: []string{},
	}

	ctx := context.Background()
	err = storage.SaveCertificate(ctx, cert)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no domains")
}

func TestLoadCertificate(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zap.NewNop()
	storage, err := NewFileStorage(tmpDir, logger)
	require.NoError(t, err)

	// First save a certificate
	cert := &Certificate{
		Domains:        []string{"example.com"},
		CertificatePEM: []byte("-----BEGIN CERTIFICATE-----\ntest-cert\n-----END CERTIFICATE-----"),
		PrivateKeyPEM:  []byte("-----BEGIN PRIVATE KEY-----\ntest-key\n-----END PRIVATE KEY-----"),
		NotBefore:      time.Now(),
		NotAfter:       time.Now().Add(90 * 24 * time.Hour),
		SerialNumber:   "1234567890",
		Issuer:         "Let's Encrypt",
	}

	ctx := context.Background()
	err = storage.SaveCertificate(ctx, cert)
	require.NoError(t, err)

	// Now load it
	loadedCert, err := storage.LoadCertificate(ctx, "example.com")
	assert.NoError(t, err)
	assert.NotNil(t, loadedCert)
	assert.Equal(t, cert.Domains, loadedCert.Domains)
	assert.Equal(t, cert.SerialNumber, loadedCert.SerialNumber)
	assert.Equal(t, cert.Issuer, loadedCert.Issuer)
}

func TestLoadCertificateNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zap.NewNop()
	storage, err := NewFileStorage(tmpDir, logger)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = storage.LoadCertificate(ctx, "nonexistent.com")
	assert.Error(t, err)
}

func TestDeleteCertificate(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zap.NewNop()
	storage, err := NewFileStorage(tmpDir, logger)
	require.NoError(t, err)

	// First save a certificate
	cert := &Certificate{
		Domains:        []string{"delete-test.com"},
		CertificatePEM: []byte("test-cert"),
		PrivateKeyPEM:  []byte("test-key"),
		NotBefore:      time.Now(),
		NotAfter:       time.Now().Add(90 * 24 * time.Hour),
	}

	ctx := context.Background()
	err = storage.SaveCertificate(ctx, cert)
	require.NoError(t, err)

	// Delete it
	err = storage.DeleteCertificate(ctx, "delete-test.com")
	assert.NoError(t, err)

	// Verify it's gone
	certPath := filepath.Join(tmpDir, "certs", "delete-test_com")
	_, err = os.Stat(certPath)
	assert.True(t, os.IsNotExist(err))
}

func TestDeleteCertificateNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zap.NewNop()
	storage, err := NewFileStorage(tmpDir, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = storage.DeleteCertificate(ctx, "nonexistent.com")
	assert.NoError(t, err) // Deleting non-existent should not error
}

func TestListCertificates(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zap.NewNop()
	storage, err := NewFileStorage(tmpDir, logger)
	require.NoError(t, err)

	// Save multiple certificates
	domains := []string{"example1.com", "example2.com", "example3.com"}
	for _, domain := range domains {
		cert := &Certificate{
			Domains:        []string{domain},
			CertificatePEM: []byte("test-cert"),
			PrivateKeyPEM:  []byte("test-key"),
			NotBefore:      time.Now(),
			NotAfter:       time.Now().Add(90 * 24 * time.Hour),
		}
		err = storage.SaveCertificate(context.Background(), cert)
		require.NoError(t, err)
	}

	// List certificates
	ctx := context.Background()
	certs, err := storage.ListCertificates(ctx)
	assert.NoError(t, err)
	assert.Len(t, certs, 3)

	// Verify all domains are present
	certDomains := make(map[string]bool)
	for _, cert := range certs {
		certDomains[cert.Domains[0]] = true
	}
	for _, domain := range domains {
		assert.True(t, certDomains[domain], "domain %s should be in list", domain)
	}
}

func TestListCertificatesEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zap.NewNop()
	storage, err := NewFileStorage(tmpDir, logger)
	require.NoError(t, err)

	ctx := context.Background()
	certs, err := storage.ListCertificates(ctx)
	assert.NoError(t, err)
	assert.Empty(t, certs)
}

func TestSaveAccount(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zap.NewNop()
	storage, err := NewFileStorage(tmpDir, logger)
	require.NoError(t, err)

	account := &AccountInfo{
		Email:         "test@example.com",
		URI:           "https://acme-v02.api.letsencrypt.org/acme/acct/12345",
		PrivateKeyPEM: []byte("test-private-key"),
		Registration:  time.Now(),
	}

	ctx := context.Background()
	err = storage.SaveAccount(ctx, account)
	assert.NoError(t, err)

	// Verify file was created
	accountPath := filepath.Join(tmpDir, "account", "account.json")
	_, err = os.Stat(accountPath)
	assert.NoError(t, err)
}

func TestLoadAccount(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zap.NewNop()
	storage, err := NewFileStorage(tmpDir, logger)
	require.NoError(t, err)

	// First save an account
	account := &AccountInfo{
		Email:         "load-test@example.com",
		URI:           "https://acme-v02.api.letsencrypt.org/acme/acct/12345",
		PrivateKeyPEM: []byte("test-private-key"),
		Registration:  time.Now(),
	}

	ctx := context.Background()
	err = storage.SaveAccount(ctx, account)
	require.NoError(t, err)

	// Now load it
	loadedAccount, err := storage.LoadAccount(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, loadedAccount)
	assert.Equal(t, account.Email, loadedAccount.Email)
	assert.Equal(t, account.URI, loadedAccount.URI)
}

func TestLoadAccountNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zap.NewNop()
	storage, err := NewFileStorage(tmpDir, logger)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = storage.LoadAccount(ctx)
	assert.Error(t, err)
}

func TestCertPath(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zap.NewNop()
	storage, err := NewFileStorage(tmpDir, logger)
	require.NoError(t, err)

	path := storage.certPath("example.com")
	expectedPath := filepath.Join(tmpDir, "certs", "example_com")
	assert.Equal(t, expectedPath, path)
}

func TestCertPathWithWildcard(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zap.NewNop()
	storage, err := NewFileStorage(tmpDir, logger)
	require.NoError(t, err)

	path := storage.certPath("*.example.com")
	expectedPath := filepath.Join(tmpDir, "certs", "wildcard_example_com")
	assert.Equal(t, expectedPath, path)
}

func TestSaveCertificateWithIssuer(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zap.NewNop()
	storage, err := NewFileStorage(tmpDir, logger)
	require.NoError(t, err)

	cert := &Certificate{
		Domains:              []string{"with-issuer.com"},
		CertificatePEM:       []byte("test-cert"),
		PrivateKeyPEM:        []byte("test-key"),
		IssuerCertificatePEM: []byte("issuer-cert"),
		NotBefore:            time.Now(),
		NotAfter:             time.Now().Add(90 * 24 * time.Hour),
		SerialNumber:         "ABC123",
		Issuer:               "Test Issuer",
	}

	ctx := context.Background()
	err = storage.SaveCertificate(ctx, cert)
	assert.NoError(t, err)

	// Load and verify issuer is preserved
	loadedCert, err := storage.LoadCertificate(ctx, "with-issuer.com")
	assert.NoError(t, err)
	assert.Equal(t, cert.SerialNumber, loadedCert.SerialNumber)
	assert.Equal(t, cert.Issuer, loadedCert.Issuer)
}

func TestConcurrentSaveCertificate(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zap.NewNop()
	storage, err := NewFileStorage(tmpDir, logger)
	require.NoError(t, err)

	// Run concurrent saves for different domains
	done := make(chan bool)
	domains := []string{"concurrent1.com", "concurrent2.com", "concurrent3.com"}

	for _, domain := range domains {
		go func(d string) {
			cert := &Certificate{
				Domains:        []string{d},
				CertificatePEM: []byte("test-cert-" + d),
				PrivateKeyPEM:  []byte("test-key-" + d),
				NotBefore:      time.Now(),
				NotAfter:       time.Now().Add(90 * 24 * time.Hour),
			}
			err := storage.SaveCertificate(context.Background(), cert)
			assert.NoError(t, err)
			done <- true
		}(domain)
	}

	// Wait for all goroutines
	for range domains {
		<-done
	}

	// Verify all certificates were saved
	certs, err := storage.ListCertificates(context.Background())
	assert.NoError(t, err)
	assert.Len(t, certs, 3)
}
