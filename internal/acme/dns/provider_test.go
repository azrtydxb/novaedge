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
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestDefaultPropagationTimeout(t *testing.T) {
	if DefaultPropagationTimeout != 120*time.Second {
		t.Errorf("DefaultPropagationTimeout = %v, want 120s", DefaultPropagationTimeout)
	}
}

func TestDefaultPollingInterval(t *testing.T) {
	if DefaultPollingInterval != 5*time.Second {
		t.Errorf("DefaultPollingInterval = %v, want 5s", DefaultPollingInterval)
	}
}

func TestProviderConfig_ApplyDefaults(t *testing.T) {
	cfg := &ProviderConfig{}
	cfg.ApplyDefaults()

	if cfg.PropagationTimeout != DefaultPropagationTimeout {
		t.Errorf("PropagationTimeout = %v, want %v", cfg.PropagationTimeout, DefaultPropagationTimeout)
	}

	if cfg.PollingInterval != DefaultPollingInterval {
		t.Errorf("PollingInterval = %v, want %v", cfg.PollingInterval, DefaultPollingInterval)
	}

	if cfg.Logger == nil {
		t.Error("Logger should be initialized")
	}
}

func TestProviderConfig_ApplyDefaults_PreservesExisting(t *testing.T) {
	logger := zap.NewNop()
	cfg := &ProviderConfig{
		PropagationTimeout: 60 * time.Second,
		PollingInterval:    2 * time.Second,
		Logger:             logger,
	}
	cfg.ApplyDefaults()

	if cfg.PropagationTimeout != 60*time.Second {
		t.Errorf("PropagationTimeout = %v, want 60s", cfg.PropagationTimeout)
	}

	if cfg.PollingInterval != 2*time.Second {
		t.Errorf("PollingInterval = %v, want 2s", cfg.PollingInterval)
	}

	if cfg.Logger != logger {
		t.Error("Logger should not be changed")
	}
}

func TestNewProvider_UnsupportedProvider(t *testing.T) {
	_, err := NewProvider("unsupported", nil, nil)
	if err == nil {
		t.Error("expected error for unsupported provider")
	}
}

func TestNewProvider_NilConfig(t *testing.T) {
	// This will fail because cloudflare requires credentials, but it tests the nil config path
	_, err := NewProvider("cloudflare", map[string]string{}, nil)
	if err == nil {
		t.Error("expected error for missing credentials")
	}
}

func TestNewProvider_Cloudflare_MissingCredentials(t *testing.T) {
	cfg := &ProviderConfig{Logger: zap.NewNop()}
	_, err := NewProvider("cloudflare", map[string]string{}, cfg)
	if err == nil {
		t.Error("expected error for missing cloudflare credentials")
	}
}

func TestNewProvider_Route53_MissingCredentials(t *testing.T) {
	cfg := &ProviderConfig{Logger: zap.NewNop()}
	_, err := NewProvider("route53", map[string]string{}, cfg)
	if err == nil {
		t.Error("expected error for missing route53 credentials")
	}
}

func TestNewProvider_GoogleDNS_MissingCredentials(t *testing.T) {
	cfg := &ProviderConfig{Logger: zap.NewNop()}
	_, err := NewProvider("googledns", map[string]string{}, cfg)
	if err == nil {
		t.Error("expected error for missing googledns credentials")
	}
}

// mockProvider is a mock implementation of Provider for testing
type mockProvider struct {
	createErr error
	deleteErr error
	waitErr   error
}

func (m *mockProvider) CreateTXTRecord(ctx context.Context, fqdn, value string) error {
	return m.createErr
}

func (m *mockProvider) DeleteTXTRecord(ctx context.Context, fqdn, value string) error {
	return m.deleteErr
}

func (m *mockProvider) WaitForPropagation(ctx context.Context, fqdn, value string) error {
	return m.waitErr
}

func TestMockProvider_CreateTXTRecord(t *testing.T) {
	p := &mockProvider{}
	err := p.CreateTXTRecord(context.Background(), "_acme-challenge.example.com", "test-value")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMockProvider_DeleteTXTRecord(t *testing.T) {
	p := &mockProvider{}
	err := p.DeleteTXTRecord(context.Background(), "_acme-challenge.example.com", "test-value")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMockProvider_WaitForPropagation(t *testing.T) {
	p := &mockProvider{}
	err := p.WaitForPropagation(context.Background(), "_acme-challenge.example.com", "test-value")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDNS01ChallengeProvider_Wrap(t *testing.T) {
	mock := &mockProvider{}
	logger := zap.NewNop()

	provider := &DNS01ChallengeProvider{
		provider: mock,
		logger:   logger,
	}

	if provider.provider != mock {
		t.Error("provider not set correctly")
	}
	if provider.logger != logger {
		t.Error("logger not set correctly")
	}
}
