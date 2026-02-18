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
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestEnableMode_Constants(t *testing.T) {
	assert.Equal(t, EnableMode("auto"), EnableModeAuto)
	assert.Equal(t, EnableMode("true"), EnableModeTrue)
	assert.Equal(t, EnableMode("false"), EnableModeFalse)
}

func TestAuthMethod_Constants(t *testing.T) {
	assert.Equal(t, AuthMethod("kubernetes"), AuthMethodKubernetes)
	assert.Equal(t, AuthMethod("approle"), AuthMethodAppRole)
	assert.Equal(t, AuthMethod("token"), AuthMethodToken)
}

func TestNewClient_NilConfig(t *testing.T) {
	logger := zap.NewNop()
	client, err := NewClient(nil, logger)
	assert.Nil(t, client)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "vault config is required")
}

func TestNewClient_EmptyAddress(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Address: "",
	}
	client, err := NewClient(config, logger)
	assert.Nil(t, client)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "vault address is required")
}

func TestNewClient_AddressFromEnv(t *testing.T) {
	os.Setenv("VAULT_ADDR", "https://vault.example.com:8200")
	defer os.Unsetenv("VAULT_ADDR")

	logger := zap.NewNop()
	config := &Config{
		Address: "", // Will be picked from env
	}
	client, err := NewClient(config, logger)
	assert.NoError(t, err)
	assert.NotNil(t, client)
	assert.Equal(t, "https://vault.example.com:8200", config.Address)
}

func TestNewClient_NilLogger(t *testing.T) {
	config := &Config{
		Address: "https://vault.example.com:8200",
	}
	client, err := NewClient(config, nil)
	assert.NoError(t, err)
	assert.NotNil(t, client)
}

func TestNewClient_Success(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Address:     "https://vault.example.com:8200",
		Namespace:   "test-namespace",
		CACert:      "/path/to/ca.crt",
		TLSSkipVerify: true,
	}
	client, err := NewClient(config, logger)
	assert.NoError(t, err)
	assert.NotNil(t, client)
	assert.NotNil(t, client.httpClient)
	assert.Equal(t, "https://vault.example.com:8200", client.httpClient.address)
	assert.Equal(t, "test-namespace", client.httpClient.namespace)
	assert.Equal(t, "/path/to/ca.crt", client.httpClient.caCert)
	assert.True(t, client.httpClient.skipTLS)
}

func TestClient_Authenticate_Token(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Address:     "https://vault.example.com:8200",
		AuthMethod:  AuthMethodToken,
		Token:       "test-token",
	}
	client, err := NewClient(config, logger)
	assert.NoError(t, err)

	err = client.Authenticate(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "test-token", client.token)
	assert.Equal(t, "test-token", client.httpClient.token)
}

func TestClient_Authenticate_TokenFromEnv(t *testing.T) {
	os.Setenv("VAULT_TOKEN", "env-token")
	defer os.Unsetenv("VAULT_TOKEN")

	logger := zap.NewNop()
	config := &Config{
		Address:    "https://vault.example.com:8200",
		AuthMethod: AuthMethodToken,
	}
	client, err := NewClient(config, logger)
	assert.NoError(t, err)

	err = client.Authenticate(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "env-token", client.token)
}

func TestClient_Authenticate_TokenMissing(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Address:    "https://vault.example.com:8200",
		AuthMethod: AuthMethodToken,
	}
	client, err := NewClient(config, logger)
	assert.NoError(t, err)

	err = client.Authenticate(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "vault token not provided")
}

func TestClient_Authenticate_Kubernetes_MissingConfig(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Address:    "https://vault.example.com:8200",
		AuthMethod: AuthMethodKubernetes,
	}
	client, err := NewClient(config, logger)
	assert.NoError(t, err)

	err = client.Authenticate(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "kubernetes auth config is required")
}

func TestClient_Authenticate_AppRole_MissingConfig(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Address:    "https://vault.example.com:8200",
		AuthMethod: AuthMethodAppRole,
	}
	client, err := NewClient(config, logger)
	assert.NoError(t, err)

	err = client.Authenticate(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "approle auth config is required")
}

func TestClient_Authenticate_UnsupportedMethod(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Address:    "https://vault.example.com:8200",
		AuthMethod: AuthMethod("unsupported"),
	}
	client, err := NewClient(config, logger)
	assert.NoError(t, err)

	err = client.Authenticate(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported auth method")
}

func TestKubernetesAuthConfig_Defaults(t *testing.T) {
	config := &KubernetesAuthConfig{
		Role: "test-role",
	}
	assert.Equal(t, "test-role", config.Role)
	assert.Empty(t, config.MountPath)
	assert.Empty(t, config.ServiceAccountTokenPath)
}

func TestAppRoleAuthConfig_Fields(t *testing.T) {
	config := &AppRoleAuthConfig{
		RoleID:    "test-role-id",
		SecretID:  "test-secret-id",
		MountPath: "custom-approle",
	}
	assert.Equal(t, "test-role-id", config.RoleID)
	assert.Equal(t, "test-secret-id", config.SecretID)
	assert.Equal(t, "custom-approle", config.MountPath)
}

func TestConfig_Fields(t *testing.T) {
	config := &Config{
		Address:       "https://vault.example.com:8200",
		AuthMethod:    AuthMethodKubernetes,
		Token:         "test-token",
		TLSSkipVerify: true,
		CACert:        "/path/to/ca.crt",
		Namespace:     "test-namespace",
		KubernetesAuth: &KubernetesAuthConfig{
			Role: "test-role",
		},
		AppRoleAuth: &AppRoleAuthConfig{
			RoleID:   "role-id",
			SecretID: "secret-id",
		},
	}

	assert.Equal(t, "https://vault.example.com:8200", config.Address)
	assert.Equal(t, AuthMethodKubernetes, config.AuthMethod)
	assert.Equal(t, "test-token", config.Token)
	assert.True(t, config.TLSSkipVerify)
	assert.Equal(t, "/path/to/ca.crt", config.CACert)
	assert.Equal(t, "test-namespace", config.Namespace)
	assert.NotNil(t, config.KubernetesAuth)
	assert.NotNil(t, config.AppRoleAuth)
}
