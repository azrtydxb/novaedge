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

package vault

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewRenewalManager(t *testing.T) {
	t.Run("with logger", func(t *testing.T) {
		logger := zap.NewNop()
		client := &Client{}
		pkiManager := &PKIManager{}

		rm := NewRenewalManager(client, pkiManager, logger)

		require.NotNil(t, rm)
		assert.Equal(t, client, rm.client)
		assert.Equal(t, pkiManager, rm.pkiManager)
		assert.Equal(t, logger, rm.logger)
		assert.Equal(t, defaultTokenRenewalThreshold, rm.tokenRenewalThreshold)
		assert.Equal(t, defaultCertRenewalThreshold, rm.certRenewalThreshold)
		assert.Equal(t, defaultCheckInterval, rm.checkInterval)
		assert.NotNil(t, rm.trackedCerts)
		assert.NotNil(t, rm.stopCh)
	})

	t.Run("with nil logger", func(t *testing.T) {
		client := &Client{}
		pkiManager := &PKIManager{}

		rm := NewRenewalManager(client, pkiManager, nil)

		require.NotNil(t, rm)
		assert.NotNil(t, rm.logger) // Should be set to nop logger
	})
}

func TestRenewalManager_SetTokenRenewalThreshold(t *testing.T) {
	rm := NewRenewalManager(&Client{}, &PKIManager{}, zap.NewNop())

	newThreshold := 10 * time.Minute
	rm.SetTokenRenewalThreshold(newThreshold)

	assert.Equal(t, newThreshold, rm.tokenRenewalThreshold)
}

func TestRenewalManager_SetCertRenewalThreshold(t *testing.T) {
	rm := NewRenewalManager(&Client{}, &PKIManager{}, zap.NewNop())

	newThreshold := 48 * time.Hour
	rm.SetCertRenewalThreshold(newThreshold)

	assert.Equal(t, newThreshold, rm.certRenewalThreshold)
}

func TestRenewalManager_SetCheckInterval(t *testing.T) {
	rm := NewRenewalManager(&Client{}, &PKIManager{}, zap.NewNop())

	newInterval := 5 * time.Minute
	rm.SetCheckInterval(newInterval)

	assert.Equal(t, newInterval, rm.checkInterval)
}

func TestRenewalManager_Callbacks(t *testing.T) {
	rm := NewRenewalManager(&Client{}, &PKIManager{}, zap.NewNop())

	t.Run("OnTokenRenewed", func(t *testing.T) {
		called := false
		rm.OnTokenRenewed(func() {
			called = true
		})
		require.NotNil(t, rm.onTokenRenewed)
		rm.onTokenRenewed()
		assert.True(t, called)
	})

	t.Run("OnCertRenewed", func(t *testing.T) {
		var receivedName string
		var receivedCert *PKICertificate
		rm.OnCertRenewed(func(name string, cert *PKICertificate) {
			receivedName = name
			receivedCert = cert
		})
		require.NotNil(t, rm.onCertRenewed)
		testCert := &PKICertificate{Certificate: "test"}
		rm.onCertRenewed("test-cert", testCert)
		assert.Equal(t, "test-cert", receivedName)
		assert.Equal(t, testCert, receivedCert)
	})

	t.Run("OnRenewalError", func(t *testing.T) {
		var receivedName string
		var receivedErr error
		rm.OnRenewalError(func(name string, err error) {
			receivedName = name
			receivedErr = err
		})
		require.NotNil(t, rm.onRenewalError)
		testErr := assert.AnError
		rm.onRenewalError("test-cert", testErr)
		assert.Equal(t, "test-cert", receivedName)
		assert.Equal(t, testErr, receivedErr)
	})
}

func TestRenewalManager_TrackCertificate(t *testing.T) {
	rm := NewRenewalManager(&Client{}, &PKIManager{}, zap.NewNop())

	cert := &PKICertificate{Certificate: "test"}
	req := &PKIRequest{CommonName: "test.example.com"}

	rm.TrackCertificate("test-cert", cert, req)

	rm.mu.Lock()
	tracked, exists := rm.trackedCerts["test-cert"]
	rm.mu.Unlock()

	assert.True(t, exists)
	assert.Equal(t, "test-cert", tracked.name)
	assert.Equal(t, cert, tracked.cert)
	assert.Equal(t, req, tracked.request)
}

func TestRenewalManager_UntrackCertificate(t *testing.T) {
	rm := NewRenewalManager(&Client{}, &PKIManager{}, zap.NewNop())

	// First track a certificate
	cert := &PKICertificate{Certificate: "test"}
	req := &PKIRequest{CommonName: "test.example.com"}
	rm.TrackCertificate("test-cert", cert, req)

	// Then untrack it
	rm.UntrackCertificate("test-cert")

	rm.mu.Lock()
	_, exists := rm.trackedCerts["test-cert"]
	rm.mu.Unlock()

	assert.False(t, exists)
}

func TestRenewalManager_UntrackCertificate_NotExists(t *testing.T) {
	rm := NewRenewalManager(&Client{}, &PKIManager{}, zap.NewNop())

	// Untracking a non-existent certificate should not panic
	rm.UntrackCertificate("non-existent")
}

func TestRenewalConstants(t *testing.T) {
	// Verify constants are set correctly
	assert.Equal(t, 5*time.Minute, defaultTokenRenewalThreshold)
	assert.Equal(t, 24*time.Hour, defaultCertRenewalThreshold)
	assert.Equal(t, 1*time.Minute, defaultCheckInterval)
}

func TestTrackedCertificate(t *testing.T) {
	cert := &PKICertificate{
		Certificate: "test-cert",
		PrivateKey:  "test-key",
	}
	req := &PKIRequest{
		CommonName: "test.example.com",
		TTL:        "24h",
	}

	tracked := &trackedCertificate{
		name:    "test-cert",
		cert:    cert,
		request: req,
	}

	assert.Equal(t, "test-cert", tracked.name)
	assert.Equal(t, cert, tracked.cert)
	assert.Equal(t, req, tracked.request)
}

func TestPKICertificate(t *testing.T) {
	t.Run("with all fields", func(t *testing.T) {
		cert := PKICertificate{
			Certificate:    "-----BEGIN CERTIFICATE-----",
			IssuingCA:      "-----BEGIN CA CERTIFICATE-----",
			CAChain:        []string{"ca1", "ca2"},
			PrivateKey:     "-----BEGIN PRIVATE KEY-----",
			PrivateKeyType: "rsa",
			SerialNumber:   "12345",
			Expiration:     1234567890,
		}

		assert.Equal(t, "-----BEGIN CERTIFICATE-----", cert.Certificate)
		assert.Equal(t, "-----BEGIN CA CERTIFICATE-----", cert.IssuingCA)
		assert.Len(t, cert.CAChain, 2)
		assert.Equal(t, "-----BEGIN PRIVATE KEY-----", cert.PrivateKey)
		assert.Equal(t, "rsa", cert.PrivateKeyType)
		assert.Equal(t, "12345", cert.SerialNumber)
		assert.Equal(t, int64(1234567890), cert.Expiration)
	})

	t.Run("empty", func(t *testing.T) {
		cert := PKICertificate{}
		assert.Empty(t, cert.Certificate)
		assert.Empty(t, cert.IssuingCA)
		assert.Nil(t, cert.CAChain)
		assert.Empty(t, cert.PrivateKey)
	})
}

func TestPKIRequest(t *testing.T) {
	t.Run("with all fields", func(t *testing.T) {
		req := PKIRequest{
			MountPath:  "pki",
			Role:       "example-com",
			CommonName: "test.example.com",
			AltNames:   []string{"www.example.com"},
			IPSANs:     []string{"192.168.1.1"},
			TTL:        "24h",
		}

		assert.Equal(t, "pki", req.MountPath)
		assert.Equal(t, "example-com", req.Role)
		assert.Equal(t, "test.example.com", req.CommonName)
		assert.Equal(t, []string{"www.example.com"}, req.AltNames)
		assert.Equal(t, []string{"192.168.1.1"}, req.IPSANs)
		assert.Equal(t, "24h", req.TTL)
	})

	t.Run("empty", func(t *testing.T) {
		req := PKIRequest{}
		assert.Empty(t, req.MountPath)
		assert.Empty(t, req.Role)
		assert.Empty(t, req.CommonName)
		assert.Nil(t, req.AltNames)
		assert.Nil(t, req.IPSANs)
	})
}
