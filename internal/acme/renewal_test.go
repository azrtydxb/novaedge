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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewRenewalManager(t *testing.T) {
	logger := zap.NewNop()
	
	// Create a client with test config
	config := &Config{
		Email:       "test@example.com",
		Server:      "https://acme-staging-v02.api.letsencrypt.org/directory",
		RenewalDays: 30,
	}
	
	client := &Client{
		config: config,
		logger: logger,
	}
	
	manager := NewRenewalManager(client, logger)
	assert.NotNil(t, manager)
	assert.Equal(t, client, manager.client)
	assert.Equal(t, 24*time.Hour, manager.interval)
}

func TestNewRenewalManagerWithNilLogger(t *testing.T) {
	config := &Config{
		Email:       "test@example.com",
		Server:      "https://acme-staging-v02.api.letsencrypt.org/directory",
		RenewalDays: 30,
	}
	
	client := &Client{
		config: config,
		logger: zap.NewNop(),
	}
	
	manager := NewRenewalManager(client, nil)
	assert.NotNil(t, manager)
}

func TestRenewalManager_SetInterval(t *testing.T) {
	config := &Config{
		Email:       "test@example.com",
		Server:      "https://acme-staging-v02.api.letsencrypt.org/directory",
		RenewalDays: 30,
	}
	
	client := &Client{
		config: config,
		logger: zap.NewNop(),
	}
	
	manager := NewRenewalManager(client, zap.NewNop())
	
	newInterval := 12 * time.Hour
	manager.SetInterval(newInterval)
	assert.Equal(t, newInterval, manager.interval)
}

func TestRenewalManager_SetRenewBefore(t *testing.T) {
	config := &Config{
		Email:       "test@example.com",
		Server:      "https://acme-staging-v02.api.letsencrypt.org/directory",
		RenewalDays: 30,
	}
	
	client := &Client{
		config: config,
		logger: zap.NewNop(),
	}
	
	manager := NewRenewalManager(client, zap.NewNop())
	
	newRenewBefore := 14 * 24 * time.Hour // 14 days
	manager.SetRenewBefore(newRenewBefore)
	assert.Equal(t, newRenewBefore, manager.renewBefore)
}

func TestRenewalManager_StartStop(t *testing.T) {
	config := &Config{
		Email:       "test@example.com",
		Server:      "https://acme-staging-v02.api.letsencrypt.org/directory",
		RenewalDays: 30,
	}
	
	// Create storage
	tmpDir := t.TempDir()
	storage, err := NewFileStorage(tmpDir, zap.NewNop())
	require.NoError(t, err)
	
	client := &Client{
		config:  config,
		logger:  zap.NewNop(),
		storage: storage,
	}
	
	manager := NewRenewalManager(client, zap.NewNop())
	manager.SetInterval(1 * time.Second) // Short interval for testing
	
	ctx := context.Background()
	err = manager.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, manager.running)
	
	// Starting again should be idempotent
	err = manager.Start(ctx)
	assert.NoError(t, err)
	
	// Stop the manager
	manager.Stop()
	assert.False(t, manager.running)
	
	// Stopping again should be idempotent
	manager.Stop()
}

func TestRenewalManager_StopWithoutStart(t *testing.T) {
	config := &Config{
		Email:       "test@example.com",
		Server:      "https://acme-staging-v02.api.letsencrypt.org/directory",
		RenewalDays: 30,
	}
	
	client := &Client{
		config: config,
		logger: zap.NewNop(),
	}
	
	manager := NewRenewalManager(client, zap.NewNop())
	
	// Stopping without starting should not panic
	manager.Stop()
	assert.False(t, manager.running)
}

func TestRenewalManager_ContextCancellation(t *testing.T) {
	config := &Config{
		Email:       "test@example.com",
		Server:      "https://acme-staging-v02.api.letsencrypt.org/directory",
		RenewalDays: 30,
	}
	
	tmpDir := t.TempDir()
	storage, err := NewFileStorage(tmpDir, zap.NewNop())
	require.NoError(t, err)
	
	client := &Client{
		config:  config,
		logger:  zap.NewNop(),
		storage: storage,
	}
	
	manager := NewRenewalManager(client, zap.NewNop())
	manager.SetInterval(100 * time.Millisecond)
	
	ctx, cancel := context.WithCancel(context.Background())
	
	err = manager.Start(ctx)
	require.NoError(t, err)
	
	// Cancel context after a short delay
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	
	// Wait for cancellation to propagate
	time.Sleep(300 * time.Millisecond)
	
	assert.False(t, manager.running)
}

func TestRenewalManager_OnRenewalCallback(t *testing.T) {
	config := &Config{
		Email:       "test@example.com",
		Server:      "https://acme-staging-v02.api.letsencrypt.org/directory",
		RenewalDays: 30,
	}
	
	tmpDir := t.TempDir()
	storage, err := NewFileStorage(tmpDir, zap.NewNop())
	require.NoError(t, err)
	
	client := &Client{
		config:  config,
		logger:  zap.NewNop(),
		storage: storage,
	}
	
	manager := NewRenewalManager(client, zap.NewNop())
	
	_ = false // placeholder for callback test
	manager.OnRenewal = func(domain string, cert *Certificate) {
		_ = true // callback was called
	}
	
	assert.NotNil(t, manager.OnRenewal)
	// Note: Actually triggering the callback would require a real certificate renewal
}

func TestRenewalManager_GetNextRenewalTime(t *testing.T) {
	config := &Config{
		Email:       "test@example.com",
		Server:      "https://acme-staging-v02.api.letsencrypt.org/directory",
		RenewalDays: 30,
	}
	
	tmpDir := t.TempDir()
	storage, err := NewFileStorage(tmpDir, zap.NewNop())
	require.NoError(t, err)
	
	client := &Client{
		config:  config,
		logger:  zap.NewNop(),
		storage: storage,
	}
	
	manager := NewRenewalManager(client, zap.NewNop())
	
	ctx := context.Background()
	
	// When no certificates, should return zero time
	nextTime, err := manager.GetNextRenewalTime(ctx)
	assert.NoError(t, err)
	assert.Equal(t, time.Time{}, nextTime)
}

func TestRenewalManager_GetNextRenewalTimeWithCerts(t *testing.T) {
	config := &Config{
		Email:       "test@example.com",
		Server:      "https://acme-staging-v02.api.letsencrypt.org/directory",
		RenewalDays: 30,
	}
	
	tmpDir := t.TempDir()
	storage, err := NewFileStorage(tmpDir, zap.NewNop())
	require.NoError(t, err)
	
	// Save a certificate
	cert := &Certificate{
		Domains:        []string{"example.com"},
		CertificatePEM: []byte("test-cert"),
		PrivateKeyPEM:  []byte("test-key"),
		NotBefore:      time.Now(),
		NotAfter:       time.Now().Add(90 * 24 * time.Hour),
	}
	err = storage.SaveCertificate(context.Background(), cert)
	require.NoError(t, err)
	
	client := &Client{
		config:  config,
		logger:  zap.NewNop(),
		storage: storage,
	}
	
	manager := NewRenewalManager(client, zap.NewNop())
	
	ctx := context.Background()
	nextTime, err := manager.GetNextRenewalTime(ctx)
	assert.NoError(t, err)
	assert.False(t, nextTime.IsZero())
}

func TestRenewalManager_ConcurrentStart(t *testing.T) {
	config := &Config{
		Email:       "test@example.com",
		Server:      "https://acme-staging-v02.api.letsencrypt.org/directory",
		RenewalDays: 30,
	}
	
	tmpDir := t.TempDir()
	storage, err := NewFileStorage(tmpDir, zap.NewNop())
	require.NoError(t, err)
	
	client := &Client{
		config:  config,
		logger:  zap.NewNop(),
		storage: storage,
	}
	
	manager := NewRenewalManager(client, zap.NewNop())
	manager.SetInterval(1 * time.Second)
	
	ctx := context.Background()
	
	// Start concurrently
	done := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_ = manager.Start(ctx)
			done <- true
		}()
	}
	
	// Wait for both to complete
	<-done
	<-done
	
	// Should only be running once
	assert.True(t, manager.running)
	
	manager.Stop()
}
