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
	tmpDir := t.TempDir()
	storage, err := NewFileStorage(tmpDir, zap.NewNop())
	require.NoError(t, err)

	config := &Config{
		Email:       "test@example.com",
		RenewalDays: 30,
	}
	client, err := NewClient(config, storage, nil, zap.NewNop())
	require.NoError(t, err)

	t.Run("creates manager with default values", func(t *testing.T) {
		manager := NewRenewalManager(client, nil)
		assert.NotNil(t, manager)
		assert.Equal(t, client, manager.client)
		assert.NotNil(t, manager.logger)
		assert.Equal(t, 24*time.Hour, manager.interval)
		assert.Equal(t, time.Duration(config.RenewalDays)*24*time.Hour, manager.renewBefore)
	})

	t.Run("creates manager with logger", func(t *testing.T) {
		logger := zap.NewNop()
		manager := NewRenewalManager(client, logger)
		assert.Equal(t, logger, manager.logger)
	})
}

func TestRenewalManager_SetInterval(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewFileStorage(tmpDir, zap.NewNop())
	require.NoError(t, err)

	config := &Config{
		Email:       "test@example.com",
		RenewalDays: 30,
	}
	client, err := NewClient(config, storage, nil, zap.NewNop())
	require.NoError(t, err)

	manager := NewRenewalManager(client, zap.NewNop())

	newInterval := 12 * time.Hour
	manager.SetInterval(newInterval)
	assert.Equal(t, newInterval, manager.interval)
}

func TestRenewalManager_SetRenewBefore(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewFileStorage(tmpDir, zap.NewNop())
	require.NoError(t, err)

	config := &Config{
		Email:       "test@example.com",
		RenewalDays: 30,
	}
	client, err := NewClient(config, storage, nil, zap.NewNop())
	require.NoError(t, err)

	manager := NewRenewalManager(client, zap.NewNop())

	newRenewBefore := 15 * 24 * time.Hour
	manager.SetRenewBefore(newRenewBefore)
	assert.Equal(t, newRenewBefore, manager.renewBefore)
}

func TestRenewalManager_StartStop(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewFileStorage(tmpDir, zap.NewNop())
	require.NoError(t, err)

	config := &Config{
		Email:       "test@example.com",
		RenewalDays: 30,
	}
	client, err := NewClient(config, storage, nil, zap.NewNop())
	require.NoError(t, err)

	manager := NewRenewalManager(client, zap.NewNop())
	// Set a shorter interval for testing
	manager.SetInterval(100 * time.Millisecond)

	ctx := context.Background()

	t.Run("starts manager successfully", func(t *testing.T) {
		err := manager.Start(ctx)
		assert.NoError(t, err)
		assert.True(t, manager.running)

		// Wait a bit to ensure the goroutine is running
		time.Sleep(50 * time.Millisecond)
	})

	t.Run("start is idempotent", func(t *testing.T) {
		// Second start should not cause an error
		err := manager.Start(ctx)
		assert.NoError(t, err)
	})

	t.Run("stops manager successfully", func(t *testing.T) {
		manager.Stop()
		assert.False(t, manager.running)
	})

	t.Run("stop is idempotent", func(t *testing.T) {
		// Second stop should not cause an error
		manager.Stop()
		assert.False(t, manager.running)
	})
}

func TestRenewalManager_GetNextRenewalTime(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewFileStorage(tmpDir, zap.NewNop())
	require.NoError(t, err)

	config := &Config{
		Email:       "test@example.com",
		RenewalDays: 30,
	}
	client, err := NewClient(config, storage, nil, zap.NewNop())
	require.NoError(t, err)

	manager := NewRenewalManager(client, zap.NewNop())
	ctx := context.Background()

	t.Run("returns zero time when no certificates", func(t *testing.T) {
		nextTime, err := manager.GetNextRenewalTime(ctx)
		assert.NoError(t, err)
		assert.True(t, nextTime.IsZero())
	})

	t.Run("returns earliest renewal time", func(t *testing.T) {
		// Save certificates with different expiry times
		now := time.Now()
		certs := []*Certificate{
			{
				Domains:        []string{"cert1.example.com"},
				CertificatePEM: []byte("cert1"),
				PrivateKeyPEM:  []byte("key1"),
				NotBefore:      now,
				NotAfter:       now.Add(60 * 24 * time.Hour), // Expires in 60 days
			},
			{
				Domains:        []string{"cert2.example.com"},
				CertificatePEM: []byte("cert2"),
				PrivateKeyPEM:  []byte("key2"),
				NotBefore:      now,
				NotAfter:       now.Add(40 * 24 * time.Hour), // Expires in 40 days (earliest)
			},
			{
				Domains:        []string{"cert3.example.com"},
				CertificatePEM: []byte("cert3"),
				PrivateKeyPEM:  []byte("key3"),
				NotBefore:      now,
				NotAfter:       now.Add(90 * 24 * time.Hour), // Expires in 90 days
			},
		}

		for _, cert := range certs {
			err := storage.SaveCertificate(ctx, cert)
			require.NoError(t, err)
		}

		nextTime, err := manager.GetNextRenewalTime(ctx)
		assert.NoError(t, err)
		assert.False(t, nextTime.IsZero())

		// Should be 40 days - 30 days renewal period = 10 days from now
		expectedTime := certs[1].NotAfter.Add(-manager.renewBefore)
		assert.WithinDuration(t, expectedTime, nextTime, time.Second)
	})
}

func TestRenewalManager_OnRenewal(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewFileStorage(tmpDir, zap.NewNop())
	require.NoError(t, err)

	config := &Config{
		Email:       "test@example.com",
		RenewalDays: 30,
	}
	client, err := NewClient(config, storage, nil, zap.NewNop())
	require.NoError(t, err)

	manager := NewRenewalManager(client, zap.NewNop())

	// Set up a callback
	callbackCalled := false
	var callbackDomain string
	var callbackCert *Certificate

	manager.OnRenewal = func(domain string, cert *Certificate) {
		callbackCalled = true
		callbackDomain = domain
		callbackCert = cert
	}

	// Test that the callback is configured
	assert.NotNil(t, manager.OnRenewal)

	// Simulate calling the callback
	testDomain := "test.example.com"
	testCert := &Certificate{
		Domains:        []string{testDomain},
		CertificatePEM: []byte("test-cert"),
		PrivateKeyPEM:  []byte("test-key"),
		NotBefore:      time.Now(),
		NotAfter:       time.Now().Add(90 * 24 * time.Hour),
	}

	manager.OnRenewal(testDomain, testCert)

	assert.True(t, callbackCalled)
	assert.Equal(t, testDomain, callbackDomain)
	assert.Equal(t, testCert, callbackCert)
}

func TestRenewalManager_CheckAndRenewLogic(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewFileStorage(tmpDir, zap.NewNop())
	require.NoError(t, err)

	config := &Config{
		Email:       "test@example.com",
		RenewalDays: 30,
	}
	client, err := NewClient(config, storage, nil, zap.NewNop())
	require.NoError(t, err)

	ctx := context.Background()

	// Save a certificate that doesn't need renewal yet
	now := time.Now()
	cert := &Certificate{
		Domains:        []string{"future.example.com"},
		CertificatePEM: []byte("test-cert"),
		PrivateKeyPEM:  []byte("test-key"),
		NotBefore:      now,
		NotAfter:       now.Add(90 * 24 * time.Hour), // Expires in 90 days
	}
	err = storage.SaveCertificate(ctx, cert)
	require.NoError(t, err)

	// Verify that GetCertificatesNeedingRenewal works
	needsRenewal, err := client.GetCertificatesNeedingRenewal(ctx)
	assert.NoError(t, err)
	assert.Empty(t, needsRenewal) // Should not need renewal yet
}

func TestRenewalManager_ConcurrentStartStop(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewFileStorage(tmpDir, zap.NewNop())
	require.NoError(t, err)

	config := &Config{
		Email:       "test@example.com",
		RenewalDays: 30,
	}
	client, err := NewClient(config, storage, nil, zap.NewNop())
	require.NoError(t, err)

	manager := NewRenewalManager(client, zap.NewNop())
	manager.SetInterval(10 * time.Millisecond)

	ctx := context.Background()

	// Start and stop multiple times concurrently
	done := make(chan bool)
	
	for i := 0; i < 5; i++ {
		go func() {
			_ = manager.Start(ctx)
			time.Sleep(5 * time.Millisecond)
			manager.Stop()
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 5; i++ {
		<-done
	}

	// Final state should be stopped
	assert.False(t, manager.running)
}
