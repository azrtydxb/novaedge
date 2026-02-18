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
	t.Run("creates storage with default path", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage, err := NewFileStorage(tmpDir, nil)
		require.NoError(t, err)
		assert.NotNil(t, storage)
		assert.Equal(t, tmpDir, storage.basePath)
	})

	t.Run("creates storage with logger", func(t *testing.T) {
		tmpDir := t.TempDir()
		logger := zap.NewNop()
		storage, err := NewFileStorage(tmpDir, logger)
		require.NoError(t, err)
		assert.NotNil(t, storage)
		assert.Equal(t, logger, storage.logger)
	})

	t.Run("creates directory if it doesn't exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		newDir := filepath.Join(tmpDir, "new", "nested", "dir")
		storage, err := NewFileStorage(newDir, nil)
		require.NoError(t, err)
		assert.NotNil(t, storage)

		// Check directory was created
		info, err := os.Stat(newDir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})
}

func TestFileStorage_SaveAndLoadCertificate(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewFileStorage(tmpDir, zap.NewNop())
	require.NoError(t, err)

	ctx := context.Background()

	cert := &Certificate{
		Domains:              []string{"example.com", "www.example.com"},
		CertificatePEM:       []byte("test-cert-pem"),
		PrivateKeyPEM:        []byte("test-key-pem"),
		IssuerCertificatePEM: []byte("test-issuer-pem"),
		NotBefore:            time.Now().Add(-24 * time.Hour),
		NotAfter:             time.Now().Add(30 * 24 * time.Hour),
		SerialNumber:         "123456",
		Issuer:               "Test CA",
	}

	t.Run("saves certificate successfully", func(t *testing.T) {
		err := storage.SaveCertificate(ctx, cert)
		assert.NoError(t, err)

		// Check files exist
		domain := cert.Domains[0]
		dir := storage.certPath(domain)
		assert.FileExists(t, filepath.Join(dir, "cert.pem"))
		assert.FileExists(t, filepath.Join(dir, "key.pem"))
		assert.FileExists(t, filepath.Join(dir, "issuer.pem"))
		assert.FileExists(t, filepath.Join(dir, "meta.json"))
	})

	t.Run("loads certificate successfully", func(t *testing.T) {
		loaded, err := storage.LoadCertificate(ctx, cert.Domains[0])
		require.NoError(t, err)
		assert.Equal(t, cert.Domains, loaded.Domains)
		assert.Equal(t, cert.CertificatePEM, loaded.CertificatePEM)
		assert.Equal(t, cert.PrivateKeyPEM, loaded.PrivateKeyPEM)
		assert.Equal(t, cert.IssuerCertificatePEM, loaded.IssuerCertificatePEM)
		assert.Equal(t, cert.SerialNumber, loaded.SerialNumber)
		assert.Equal(t, cert.Issuer, loaded.Issuer)
		assert.WithinDuration(t, cert.NotBefore, loaded.NotBefore, time.Second)
		assert.WithinDuration(t, cert.NotAfter, loaded.NotAfter, time.Second)
	})

	t.Run("fails to save certificate without domains", func(t *testing.T) {
		invalidCert := &Certificate{
			Domains:        []string{},
			CertificatePEM: []byte("test"),
			PrivateKeyPEM:  []byte("test"),
		}
		err := storage.SaveCertificate(ctx, invalidCert)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no domains")
	})

	t.Run("fails to load non-existent certificate", func(t *testing.T) {
		_, err := storage.LoadCertificate(ctx, "nonexistent.com")
		assert.Error(t, err)
	})
}

func TestFileStorage_DeleteCertificate(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewFileStorage(tmpDir, zap.NewNop())
	require.NoError(t, err)

	ctx := context.Background()

	cert := &Certificate{
		Domains:        []string{"delete.example.com"},
		CertificatePEM: []byte("test-cert-pem"),
		PrivateKeyPEM:  []byte("test-key-pem"),
		NotBefore:      time.Now(),
		NotAfter:       time.Now().Add(30 * 24 * time.Hour),
	}

	// Save certificate
	err = storage.SaveCertificate(ctx, cert)
	require.NoError(t, err)

	// Verify it exists
	_, err = storage.LoadCertificate(ctx, cert.Domains[0])
	require.NoError(t, err)

	// Delete certificate
	err = storage.DeleteCertificate(ctx, cert.Domains[0])
	assert.NoError(t, err)

	// Verify it's gone
	_, err = storage.LoadCertificate(ctx, cert.Domains[0])
	assert.Error(t, err)
}

func TestFileStorage_ListCertificates(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewFileStorage(tmpDir, zap.NewNop())
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("returns empty list when no certificates", func(t *testing.T) {
		certs, err := storage.ListCertificates(ctx)
		assert.NoError(t, err)
		assert.Empty(t, certs)
	})

	t.Run("lists multiple certificates", func(t *testing.T) {
		// Save multiple certificates
		domains := []string{"example1.com", "example2.com", "example3.com"}
		for _, domain := range domains {
			cert := &Certificate{
				Domains:        []string{domain},
				CertificatePEM: []byte("test-cert-" + domain),
				PrivateKeyPEM:  []byte("test-key-" + domain),
				NotBefore:      time.Now(),
				NotAfter:       time.Now().Add(30 * 24 * time.Hour),
			}
			err := storage.SaveCertificate(ctx, cert)
			require.NoError(t, err)
		}

		// List certificates
		certs, err := storage.ListCertificates(ctx)
		assert.NoError(t, err)
		assert.Len(t, certs, 3)

		// Verify domains
		foundDomains := make(map[string]bool)
		for _, cert := range certs {
			if len(cert.Domains) > 0 {
				foundDomains[cert.Domains[0]] = true
			}
		}
		for _, domain := range domains {
			assert.True(t, foundDomains[domain], "Domain %s not found", domain)
		}
	})

	t.Run("handles wildcard certificates", func(t *testing.T) {
		cert := &Certificate{
			Domains:        []string{"*.example.com"},
			CertificatePEM: []byte("test-wildcard-cert"),
			PrivateKeyPEM:  []byte("test-wildcard-key"),
			NotBefore:      time.Now(),
			NotAfter:       time.Now().Add(30 * 24 * time.Hour),
		}
		err := storage.SaveCertificate(ctx, cert)
		require.NoError(t, err)

		// Verify it can be loaded
		loaded, err := storage.LoadCertificate(ctx, "*.example.com")
		assert.NoError(t, err)
		assert.Equal(t, cert.Domains, loaded.Domains)
	})
}

func TestFileStorage_SaveAndLoadAccount(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewFileStorage(tmpDir, zap.NewNop())
	require.NoError(t, err)

	ctx := context.Background()

	account := &AccountInfo{
		Email:         "test@example.com",
		URI:           "https://acme.example.com/account/123",
		PrivateKeyPEM: []byte("test-account-key"),
		Registration:  time.Now(),
	}

	t.Run("saves account successfully", func(t *testing.T) {
		err := storage.SaveAccount(ctx, account)
		assert.NoError(t, err)

		// Check files exist
		accountDir := filepath.Join(tmpDir, "account")
		assert.FileExists(t, filepath.Join(accountDir, "key.pem"))
		assert.FileExists(t, filepath.Join(accountDir, "account.json"))
	})

	t.Run("loads account successfully", func(t *testing.T) {
		loaded, err := storage.LoadAccount(ctx)
		require.NoError(t, err)
		assert.Equal(t, account.Email, loaded.Email)
		assert.Equal(t, account.URI, loaded.URI)
		assert.Equal(t, account.PrivateKeyPEM, loaded.PrivateKeyPEM)
		assert.WithinDuration(t, account.Registration, loaded.Registration, time.Second)
	})

	t.Run("fails to load non-existent account", func(t *testing.T) {
		tmpDir2 := t.TempDir()
		storage2, err := NewFileStorage(tmpDir2, zap.NewNop())
		require.NoError(t, err)

		_, err = storage2.LoadAccount(ctx)
		assert.Error(t, err)
	})
}

func TestFileStorage_CertPath(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewFileStorage(tmpDir, nil)
	require.NoError(t, err)

	tests := []struct {
		domain   string
		expected string
	}{
		{
			domain:   "example.com",
			expected: "example_com",
		},
		{
			domain:   "*.example.com",
			expected: "wildcard_example_com",
		},
		{
			domain:   "sub.domain.example.com",
			expected: "sub_domain_example_com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			path := storage.certPath(tt.domain)
			assert.Contains(t, path, tt.expected)
		})
	}
}

func TestFileStorage_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewFileStorage(tmpDir, zap.NewNop())
	require.NoError(t, err)

	ctx := context.Background()

	// Save a certificate
	cert := &Certificate{
		Domains:        []string{"concurrent.example.com"},
		CertificatePEM: []byte("test-cert"),
		PrivateKeyPEM:  []byte("test-key"),
		NotBefore:      time.Now(),
		NotAfter:       time.Now().Add(30 * 24 * time.Hour),
	}
	err = storage.SaveCertificate(ctx, cert)
	require.NoError(t, err)

	// Concurrent reads
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_, err := storage.LoadCertificate(ctx, "concurrent.example.com")
			assert.NoError(t, err)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}
