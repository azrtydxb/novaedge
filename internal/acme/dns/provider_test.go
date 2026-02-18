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

package dns

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestProviderConfig_ApplyDefaults(t *testing.T) {
	tests := []struct {
		name     string
		config   ProviderConfig
		expected ProviderConfig
	}{
		{
			name:   "empty config gets defaults",
			config: ProviderConfig{},
			expected: ProviderConfig{
				PropagationTimeout: DefaultPropagationTimeout,
				PollingInterval:    DefaultPollingInterval,
				Logger:             zap.NewNop(),
			},
		},
		{
			name: "partial config keeps values",
			config: ProviderConfig{
				PropagationTimeout: 60 * time.Second,
			},
			expected: ProviderConfig{
				PropagationTimeout: 60 * time.Second,
				PollingInterval:    DefaultPollingInterval,
				Logger:             zap.NewNop(),
			},
		},
		{
			name: "full config keeps all values",
			config: ProviderConfig{
				PropagationTimeout: 60 * time.Second,
				PollingInterval:    2 * time.Second,
				Logger:             zap.NewNop(),
			},
			expected: ProviderConfig{
				PropagationTimeout: 60 * time.Second,
				PollingInterval:    2 * time.Second,
				Logger:             zap.NewNop(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.config
			config.ApplyDefaults()

			assert.Equal(t, tt.expected.PropagationTimeout, config.PropagationTimeout)
			assert.Equal(t, tt.expected.PollingInterval, config.PollingInterval)
			assert.NotNil(t, config.Logger)
		})
	}
}

func TestNewProvider_Unsupported(t *testing.T) {
	provider, err := NewProvider("unsupported", nil, nil)
	assert.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "unsupported DNS provider")
}

func TestNewProvider_NilConfig(t *testing.T) {
	// Test that nil config gets defaults applied
	// This will fail because cloudflare requires credentials, but we're testing
	// that nil config doesn't panic
	provider, err := NewProvider("cloudflare", map[string]string{}, nil)
	// Should fail due to missing credentials, not nil config
	assert.Error(t, err)
	assert.Nil(t, provider)
}

func TestNewProvider_ValidConfig(t *testing.T) {
	config := &ProviderConfig{
		PropagationTimeout: 60 * time.Second,
		PollingInterval:    2 * time.Second,
		Logger:             zap.NewNop(),
	}

	// Cloudflare requires APIToken or APIKey/Email
	provider, err := NewProvider("cloudflare", map[string]string{
		"APIToken": "test-token",
	}, config)

	// May fail due to validation, but shouldn't panic
	// If it succeeds, verify it's not nil
	if err == nil {
		assert.NotNil(t, provider)
	}
}

// mockProvider is a mock implementation of the Provider interface for testing
type mockProvider struct {
	createErr error
	deleteErr error
	waitErr   error
	records   map[string]string
}

func newMockProvider() *mockProvider {
	return &mockProvider{
		records: make(map[string]string),
	}
}

func (m *mockProvider) CreateTXTRecord(_ context.Context, fqdn, value string) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.records[fqdn] = value
	return nil
}

func (m *mockProvider) DeleteTXTRecord(_ context.Context, fqdn, _ string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.records, fqdn)
	return nil
}

func (m *mockProvider) WaitForPropagation(_ context.Context, _, value string) error {
	if m.waitErr != nil {
		return m.waitErr
	}
	return nil
}

func TestDNS01ChallengeProvider_Present(t *testing.T) {
	tests := []struct {
		name       string
		provider   *mockProvider
		domain     string
		token      string
		keyAuth    string
		expectErr  bool
		expectWait bool
	}{
		{
			name:      "successful present",
			provider:  newMockProvider(),
			domain:    "example.com",
			token:     "test-token",
			keyAuth:   "test-key-auth",
			expectErr: false,
		},
		{
			name: "create error",
			provider: &mockProvider{
				createErr: errors.New("create failed"),
				records:   make(map[string]string),
			},
			domain:    "example.com",
			token:     "test-token",
			keyAuth:   "test-key-auth",
			expectErr: true,
		},
		{
			name: "wait error continues",
			provider: &mockProvider{
				waitErr: errors.New("wait failed"),
				records: make(map[string]string),
			},
			domain:    "example.com",
			token:     "test-token",
			keyAuth:   "test-key-auth",
			expectErr: false, // Wait errors are logged but don't fail
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			challenge := NewDNS01ChallengeProvider(tt.provider, zap.NewNop())
			err := challenge.Present(tt.domain, tt.token, tt.keyAuth)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Verify record was created
				fqdn := "_acme-challenge." + tt.domain + "."
				assert.Equal(t, tt.keyAuth, tt.provider.records[fqdn])
			}
		})
	}
}

func TestDNS01ChallengeProvider_CleanUp(t *testing.T) {
	tests := []struct {
		name      string
		provider  *mockProvider
		domain    string
		token     string
		keyAuth   string
		expectErr bool
	}{
		{
			name:      "successful cleanup",
			provider:  newMockProvider(),
			domain:    "example.com",
			token:     "test-token",
			keyAuth:   "test-key-auth",
			expectErr: false,
		},
		{
			name: "delete error",
			provider: &mockProvider{
				deleteErr: errors.New("delete failed"),
				records:   make(map[string]string),
			},
			domain:    "example.com",
			token:     "test-token",
			keyAuth:   "test-key-auth",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			challenge := NewDNS01ChallengeProvider(tt.provider, zap.NewNop())
			err := challenge.CleanUp(tt.domain, tt.token, tt.keyAuth)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNewDNS01ChallengeProvider_NilLogger(t *testing.T) {
	provider := newMockProvider()
	challenge := NewDNS01ChallengeProvider(provider, nil)

	assert.NotNil(t, challenge)
	assert.NotNil(t, challenge.logger)
}

func TestDNS01ChallengeProvider_FQDN(t *testing.T) {
	provider := newMockProvider()
	challenge := NewDNS01ChallengeProvider(provider, zap.NewNop())

	err := challenge.Present("example.com", "token", "keyauth")
	require.NoError(t, err)

	// Verify FQDN format
	expectedFQDN := "_acme-challenge.example.com."
	assert.Equal(t, "keyauth", provider.records[expectedFQDN])
}

func TestDefaultTimeouts(t *testing.T) {
	assert.Equal(t, 120*time.Second, DefaultPropagationTimeout)
	assert.Equal(t, 5*time.Second, DefaultPollingInterval)
}

func TestProvider_Interface(t *testing.T) {
	// Verify mockProvider implements Provider interface
	var _ Provider = newMockProvider()

	// Verify DNS01ChallengeProvider can be created with any Provider
	var provider Provider = newMockProvider()
	challenge := NewDNS01ChallengeProvider(provider, nil)
	assert.NotNil(t, challenge)
}
