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
)

func TestProviderConfig_ApplyDefaults(t *testing.T) {
	config := &ProviderConfig{}
	config.ApplyDefaults()

	if config.PropagationTimeout != DefaultPropagationTimeout {
		t.Errorf("expected default propagation timeout %v, got %v",
			DefaultPropagationTimeout, config.PropagationTimeout)
	}
	if config.PollingInterval != DefaultPollingInterval {
		t.Errorf("expected default polling interval %v, got %v",
			DefaultPollingInterval, config.PollingInterval)
	}
	if config.Logger == nil {
		t.Error("expected non-nil logger")
	}
}

func TestProviderConfig_CustomValues(t *testing.T) {
	config := &ProviderConfig{
		PropagationTimeout: 60 * time.Second,
		PollingInterval:    10 * time.Second,
	}
	config.ApplyDefaults()

	if config.PropagationTimeout != 60*time.Second {
		t.Errorf("expected custom propagation timeout 60s, got %v", config.PropagationTimeout)
	}
	if config.PollingInterval != 10*time.Second {
		t.Errorf("expected custom polling interval 10s, got %v", config.PollingInterval)
	}
}

func TestNewProvider_Cloudflare(t *testing.T) {
	creds := map[string]string{"api_token": "test-token"}
	provider, err := NewProvider("cloudflare", creds, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Error("expected non-nil provider")
	}
}

func TestNewProvider_Cloudflare_MissingToken(t *testing.T) {
	creds := map[string]string{}
	_, err := NewProvider("cloudflare", creds, nil)
	if err == nil {
		t.Error("expected error for missing API token")
	}
}

func TestNewProvider_Route53(t *testing.T) {
	creds := map[string]string{
		"access_key_id":     "AKID",
		"secret_access_key": "secret",
		"hosted_zone_id":    "Z123",
	}
	provider, err := NewProvider("route53", creds, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Error("expected non-nil provider")
	}
}

func TestNewProvider_Route53_MissingCreds(t *testing.T) {
	creds := map[string]string{"access_key_id": "AKID"}
	_, err := NewProvider("route53", creds, nil)
	if err == nil {
		t.Error("expected error for missing credentials")
	}
}

func TestNewProvider_GoogleDNS(t *testing.T) {
	creds := map[string]string{
		"project":      "my-project",
		"managed_zone": "my-zone",
		"access_token": "ya29.xxx",
	}
	provider, err := NewProvider("googledns", creds, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Error("expected non-nil provider")
	}
}

func TestNewProvider_GoogleDNS_MissingProject(t *testing.T) {
	creds := map[string]string{
		"managed_zone": "my-zone",
		"access_token": "ya29.xxx",
	}
	_, err := NewProvider("googledns", creds, nil)
	if err == nil {
		t.Error("expected error for missing project")
	}
}

func TestNewProvider_Unknown(t *testing.T) {
	_, err := NewProvider("unknown-provider", map[string]string{}, nil)
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestDNS01ChallengeProvider_CleanUp(t *testing.T) {
	// Test with a mock provider
	mock := &mockDNSProvider{}
	provider := NewDNS01ChallengeProvider(mock, nil)

	err := provider.CleanUp("example.com", "token", "keyauth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.deleteCallCount != 1 {
		t.Errorf("expected 1 delete call, got %d", mock.deleteCallCount)
	}
}

// mockDNSProvider is a mock DNS provider for testing.
type mockDNSProvider struct {
	createCallCount int
	deleteCallCount int
}

func (m *mockDNSProvider) CreateTXTRecord(_ context.Context, _, _ string) error {
	m.createCallCount++
	return nil
}

func (m *mockDNSProvider) DeleteTXTRecord(_ context.Context, _, _ string) error {
	m.deleteCallCount++
	return nil
}

func (m *mockDNSProvider) WaitForPropagation(_ context.Context, _, _ string) error {
	return nil
}
