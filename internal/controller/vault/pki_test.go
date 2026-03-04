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

	"go.uber.org/zap"
)

func TestPKIManager_ShouldRenew(t *testing.T) {
	pki := NewPKIManager(nil, zap.NewNop())

	tests := []struct {
		name        string
		expiresAt   time.Time
		renewBefore time.Duration
		expected    bool
	}{
		{
			name:        "expires in 1 hour, renew before 24h",
			expiresAt:   time.Now().Add(1 * time.Hour),
			renewBefore: 24 * time.Hour,
			expected:    true,
		},
		{
			name:        "expires in 48 hours, renew before 24h",
			expiresAt:   time.Now().Add(48 * time.Hour),
			renewBefore: 24 * time.Hour,
			expected:    false,
		},
		{
			name:        "already expired",
			expiresAt:   time.Now().Add(-1 * time.Hour),
			renewBefore: 24 * time.Hour,
			expected:    true,
		},
		{
			name:        "zero expiry",
			expiresAt:   time.Time{},
			renewBefore: 24 * time.Hour,
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cert := &PKICertificate{ExpiresAt: tt.expiresAt}
			result := pki.ShouldRenew(cert, tt.renewBefore)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestPKICertificate_CertToPEM(t *testing.T) {
	cert := &PKICertificate{ //nolint:gosec // G101: test fixture data, not real credentials
		Certificate: "-----BEGIN CERTIFICATE-----\ntest-cert\n-----END CERTIFICATE-----",
		IssuingCA:   "-----BEGIN CERTIFICATE-----\ntest-ca\n-----END CERTIFICATE-----",
		PrivateKey:  "-----BEGIN RSA PRIVATE KEY-----\ntest-key\n-----END RSA PRIVATE KEY-----",
	}

	certPEM, keyPEM := cert.CertToPEM()

	if len(certPEM) == 0 {
		t.Error("expected non-empty cert PEM")
	}
	if len(keyPEM) == 0 {
		t.Error("expected non-empty key PEM")
	}

	certStr := string(certPEM)
	if certStr != "-----BEGIN CERTIFICATE-----\ntest-cert\n-----END CERTIFICATE-----\n-----BEGIN CERTIFICATE-----\ntest-ca\n-----END CERTIFICATE-----" {
		t.Errorf("unexpected cert PEM: %s", certStr)
	}
}

func TestPKICertificate_CertToPEM_NoCA(t *testing.T) {
	cert := &PKICertificate{ //nolint:gosec // G101: test fixture data, not real credentials
		Certificate: "-----BEGIN CERTIFICATE-----\ntest-cert\n-----END CERTIFICATE-----",
		PrivateKey:  "-----BEGIN RSA PRIVATE KEY-----\ntest-key\n-----END RSA PRIVATE KEY-----",
	}

	certPEM, keyPEM := cert.CertToPEM()

	if string(certPEM) != "-----BEGIN CERTIFICATE-----\ntest-cert\n-----END CERTIFICATE-----" {
		t.Errorf("unexpected cert PEM: %s", string(certPEM))
	}
	if string(keyPEM) != "-----BEGIN RSA PRIVATE KEY-----\ntest-key\n-----END RSA PRIVATE KEY-----" { //nolint:gosec // G101: test data, not actual credentials
		t.Errorf("unexpected key PEM: %s", string(keyPEM))
	}
}
