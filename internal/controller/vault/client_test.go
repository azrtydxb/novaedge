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
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNewClient_MissingAddress(t *testing.T) {
	// Unset VAULT_ADDR for this test
	t.Setenv("VAULT_ADDR", "")

	_, err := NewClient(&Config{}, zap.NewNop())
	if err == nil {
		t.Error("expected error for missing address")
	}
}

func TestNewClient_AddressFromEnv(t *testing.T) {
	t.Setenv("VAULT_ADDR", "https://vault.test:8200")

	client, err := NewClient(&Config{
		AuthMethod: AuthMethodToken,
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.config.Address != "https://vault.test:8200" {
		t.Errorf("expected address from env, got %s", client.config.Address)
	}
}

func TestClient_AuthenticateToken(t *testing.T) {
	t.Setenv("VAULT_TOKEN", "test-token")

	client, err := NewClient(&Config{
		Address:    "https://vault.test:8200",
		AuthMethod: AuthMethodToken,
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Token auth should succeed with env var
	err = client.authenticateToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.GetToken() != "test-token" {
		t.Errorf("expected token 'test-token', got '%s'", client.GetToken())
	}
}

func TestClient_AuthenticateToken_MissingToken(t *testing.T) {
	t.Setenv("VAULT_TOKEN", "")

	client, err := NewClient(&Config{
		Address:    "https://vault.test:8200",
		AuthMethod: AuthMethodToken,
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = client.authenticateToken()
	if err == nil {
		t.Error("expected error for missing token")
	}
}

func TestClient_IsTokenExpiring(t *testing.T) {
	client := &Client{
		tokenExpiry: time.Now().Add(3 * time.Minute),
	}

	// Should not be expiring within 2 minutes
	if client.IsTokenExpiring(2 * time.Minute) {
		t.Error("token should not be expiring within 2 minutes")
	}

	// Should be expiring within 5 minutes
	if !client.IsTokenExpiring(5 * time.Minute) {
		t.Error("token should be expiring within 5 minutes")
	}
}

func TestClient_IsTokenExpiring_ZeroExpiry(t *testing.T) {
	client := &Client{}

	// Zero expiry should not be considered expiring
	if client.IsTokenExpiring(5 * time.Minute) {
		t.Error("zero expiry should not be considered expiring")
	}
}

func TestShouldEnable_False(t *testing.T) {
	enabled, err := ShouldEnable(context.TODO(), nil, EnableModeFalse, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enabled {
		t.Error("expected disabled when mode is false")
	}
}

func TestShouldEnable_Auto_NoConfig(t *testing.T) {
	t.Setenv("VAULT_ADDR", "")

	enabled, err := ShouldEnable(context.TODO(), nil, EnableModeAuto, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enabled {
		t.Error("expected disabled when no config and no env")
	}
}

func TestBuildURL(t *testing.T) {
	c := &vaultHTTPClient{address: "https://vault.test:8200"}

	tests := []struct {
		path     string
		expected string
	}{
		{"secret/data/myapp", "https://vault.test:8200/v1/secret/data/myapp"},
		{"/v1/secret/data/myapp", "https://vault.test:8200/v1/secret/data/myapp"},
		{"auth/kubernetes/login", "https://vault.test:8200/v1/auth/kubernetes/login"},
	}

	for _, tt := range tests {
		result := c.buildURL(tt.path)
		if result != tt.expected {
			t.Errorf("buildURL(%q) = %q, want %q", tt.path, result, tt.expected)
		}
	}
}
