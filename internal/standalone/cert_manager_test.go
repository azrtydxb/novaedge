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

package standalone

import (
	"context"
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewCertificateManager(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("with valid config", func(t *testing.T) {
		config := &Config{
			Version: "v1",
		}
		logger := zap.NewNop()

		manager, err := NewCertificateManager(config, tempDir, logger)
		assert.NoError(t, err)
		assert.NotNil(t, manager)
		assert.Equal(t, config, manager.config)
		assert.Equal(t, tempDir, manager.storagePath)
		assert.NotNil(t, manager.certificates)
		assert.NotNil(t, manager.acmeClients)
	})

	t.Run("with nil config", func(t *testing.T) {
		manager, err := NewCertificateManager(nil, tempDir, zap.NewNop())
		assert.Error(t, err)
		assert.Nil(t, manager)
		assert.Contains(t, err.Error(), "config is required")
	})

	t.Run("with empty storage path uses default", func(t *testing.T) {
		config := &Config{Version: "v1"}
		// Note: This test may fail if /var/lib/novaedge/certs cannot be created
		// In CI/CD, use a temp directory or skip this test
		manager, err := NewCertificateManager(config, "", zap.NewNop())
		if err != nil {
			// Permission denied is expected in restricted environments
			assert.Contains(t, err.Error(), "permission denied")
			return
		}
		assert.NotNil(t, manager)
		assert.Equal(t, "/var/lib/novaedge/certs", manager.storagePath)
	})

	t.Run("with nil logger", func(t *testing.T) {
		config := &Config{Version: "v1"}
		manager, err := NewCertificateManager(config, tempDir, nil)
		assert.NoError(t, err)
		assert.NotNil(t, manager)
		assert.NotNil(t, manager.logger)
	})

	t.Run("creates storage directory", func(t *testing.T) {
		config := &Config{Version: "v1"}
		newDir := filepath.Join(tempDir, "subdir", "certs")
		manager, err := NewCertificateManager(config, newDir, zap.NewNop())
		assert.NoError(t, err)
		assert.NotNil(t, manager)

		// Check directory was created
		_, err = os.Stat(newDir)
		assert.NoError(t, err)
	})
}

func TestCertificateManager_Initialize(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("with no certificates", func(t *testing.T) {
		config := &Config{
			Version:      "v1",
			Certificates: []CertificateConfig{},
		}
		manager, err := NewCertificateManager(config, tempDir, zap.NewNop())
		require.NoError(t, err)

		err = manager.Initialize(context.Background())
		assert.NoError(t, err)
	})

	t.Run("with self-signed certificate", func(t *testing.T) {
		config := &Config{
			Version: "v1",
			Certificates: []CertificateConfig{
				{
					Name:    "test-cert",
					Domains: []string{"localhost", "example.com"},
					Issuer: CertificateIssuerConfig{
						Type: "self-signed",
						SelfSigned: &SelfSignedIssuerConfig{
							Organization: "Test Org",
							Validity:     "24h",
						},
					},
				},
			},
		}
		manager, err := NewCertificateManager(config, tempDir, zap.NewNop())
		require.NoError(t, err)

		err = manager.Initialize(context.Background())
		assert.NoError(t, err)

		// Verify certificate was stored
		manager.mu.RLock()
		cert, exists := manager.certificates["test-cert"]
		manager.mu.RUnlock()

		assert.True(t, exists)
		assert.NotNil(t, cert)
		assert.Equal(t, "test-cert", cert.Name)
		assert.Equal(t, "self-signed", cert.IssuerType)
		assert.NotZero(t, cert.NotBefore)
		assert.NotZero(t, cert.NotAfter)
	})
}

func TestCertificateManager_GetCertificate(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("existing certificate", func(t *testing.T) {
		config := &Config{
			Version: "v1",
			Certificates: []CertificateConfig{
				{
					Name:    "test-cert",
					Domains: []string{"localhost"},
					Issuer: CertificateIssuerConfig{
						Type: "self-signed",
					},
				},
			},
		}
		manager, err := NewCertificateManager(config, tempDir, zap.NewNop())
		require.NoError(t, err)
		err = manager.Initialize(context.Background())
		require.NoError(t, err)

		cert, err := manager.GetCertificate("test-cert")
		assert.NoError(t, err)
		assert.NotNil(t, cert)
	})

	t.Run("non-existing certificate", func(t *testing.T) {
		config := &Config{Version: "v1"}
		manager, err := NewCertificateManager(config, tempDir, zap.NewNop())
		require.NoError(t, err)

		cert, err := manager.GetCertificate("non-existing")
		assert.Error(t, err)
		assert.Nil(t, cert)
		assert.Contains(t, err.Error(), "certificate not found")
	})
}

func TestCertificateManager_GetCertificateFunc(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("with matching domain", func(t *testing.T) {
		config := &Config{
			Version: "v1",
			Certificates: []CertificateConfig{
				{
					Name:    "test-cert",
					Domains: []string{"localhost", "example.com"},
					Issuer: CertificateIssuerConfig{
						Type: "self-signed",
					},
				},
			},
		}
		manager, err := NewCertificateManager(config, tempDir, zap.NewNop())
		require.NoError(t, err)
		err = manager.Initialize(context.Background())
		require.NoError(t, err)

		getCert := manager.GetCertificateFunc()
		require.NotNil(t, getCert)

		// Test with matching domain
		tlsCert, err := getCert(&tls.ClientHelloInfo{
			ServerName: "localhost",
		})
		assert.NoError(t, err)
		assert.NotNil(t, tlsCert)

		// Test with another matching domain
		tlsCert, err = getCert(&tls.ClientHelloInfo{
			ServerName: "example.com",
		})
		assert.NoError(t, err)
		assert.NotNil(t, tlsCert)
	})

	t.Run("with non-matching domain returns fallback", func(t *testing.T) {
		config := &Config{
			Version: "v1",
			Certificates: []CertificateConfig{
				{
					Name:    "test-cert",
					Domains: []string{"localhost"},
					Issuer: CertificateIssuerConfig{
						Type: "self-signed",
					},
				},
			},
		}
		manager, err := NewCertificateManager(config, tempDir, zap.NewNop())
		require.NoError(t, err)
		err = manager.Initialize(context.Background())
		require.NoError(t, err)

		getCert := manager.GetCertificateFunc()

		// Test with non-matching domain (should return fallback cert)
		tlsCert, err := getCert(&tls.ClientHelloInfo{
			ServerName: "unknown.com",
		})
		assert.NoError(t, err)
		assert.NotNil(t, tlsCert)
	})

	t.Run("with no certificates", func(t *testing.T) {
		config := &Config{Version: "v1"}
		manager, err := NewCertificateManager(config, tempDir, zap.NewNop())
		require.NoError(t, err)

		getCert := manager.GetCertificateFunc()

		tlsCert, err := getCert(&tls.ClientHelloInfo{
			ServerName: "example.com",
		})
		assert.Error(t, err)
		assert.Nil(t, tlsCert)
		assert.Contains(t, err.Error(), "no certificate available")
	})
}

func TestCertificateManager_StartStop(t *testing.T) {
	tempDir := t.TempDir()

	config := &Config{
		Version: "v1",
		Certificates: []CertificateConfig{
			{
				Name:    "test-cert",
				Domains: []string{"localhost"},
				Issuer: CertificateIssuerConfig{
					Type: "self-signed",
				},
			},
		},
	}
	manager, err := NewCertificateManager(config, tempDir, zap.NewNop())
	require.NoError(t, err)
	err = manager.Initialize(context.Background())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the manager
	manager.Start(ctx)

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	// Stop the manager
	manager.Stop()
}

func TestCertificateManager_Stop_WithoutStart(t *testing.T) {
	tempDir := t.TempDir()

	config := &Config{Version: "v1"}
	manager, err := NewCertificateManager(config, tempDir, zap.NewNop())
	require.NoError(t, err)

	// Stop should not panic even if not started
	manager.Stop()
}

func TestCertificateManager_Initialize_UnsupportedIssuer(t *testing.T) {
	tempDir := t.TempDir()

	config := &Config{
		Version: "v1",
		Certificates: []CertificateConfig{
			{
				Name:    "test-cert",
				Domains: []string{"localhost"},
				Issuer: CertificateIssuerConfig{
					Type: "unsupported",
				},
			},
		},
	}
	manager, err := NewCertificateManager(config, tempDir, zap.NewNop())
	require.NoError(t, err)

	err = manager.Initialize(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported issuer type")
}

func TestCertificateManager_ConcurrentAccess(t *testing.T) {
	tempDir := t.TempDir()

	config := &Config{
		Version: "v1",
		Certificates: []CertificateConfig{
			{
				Name:    "test-cert",
				Domains: []string{"localhost"},
				Issuer: CertificateIssuerConfig{
					Type: "self-signed",
				},
			},
		},
	}
	manager, err := NewCertificateManager(config, tempDir, zap.NewNop())
	require.NoError(t, err)
	err = manager.Initialize(context.Background())
	require.NoError(t, err)

	done := make(chan bool)

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_, _ = manager.GetCertificate("test-cert")
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}


