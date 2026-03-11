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

// Package vault provides optional integration with HashiCorp Vault for
// TLS certificate provisioning and secrets management in NovaEdge.
package vault

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
)

var (
	errVaultConfigIsRequired                 = errors.New("vault config is required")
	errVaultAddressIsRequiredSetVAULTADDROr  = errors.New("vault address is required (set VAULT_ADDR or config.address)")
	errUnsupportedAuthMethod                 = errors.New("unsupported auth method")
	errVaultTokenNotProvidedSetVAULTTOKENOr  = errors.New("vault token not provided (set VAULT_TOKEN or config.token)")
	errKubernetesAuthConfigIsRequired        = errors.New("kubernetes auth config is required")
	errNoClientTokenInKubernetesAuthResponse = errors.New("no client_token in kubernetes auth response")
	errClientTokenIsNotAString               = errors.New("client_token is not a string")
	errApproleAuthConfigIsRequired           = errors.New("approle auth config is required")
	errNoClientTokenInApproleAuthResponse    = errors.New("no client_token in approle auth response")
	errInvalidVaultEnableMode                = errors.New("invalid vault enable mode")
)

// EnableMode defines the Vault enablement mode.
type EnableMode string

const (
	// EnableModeAuto auto-detects Vault availability.
	EnableModeAuto EnableMode = "auto"
	// EnableModeTrue requires Vault to be available.
	EnableModeTrue EnableMode = "true"
	// EnableModeFalse disables Vault integration entirely.
	EnableModeFalse EnableMode = "false"
)

// AuthMethod defines the Vault authentication method.
type AuthMethod string

const (
	// AuthMethodKubernetes uses Kubernetes service account auth.
	AuthMethodKubernetes AuthMethod = "kubernetes"
	// AuthMethodAppRole uses Vault AppRole auth.
	AuthMethodAppRole AuthMethod = "approle"
	// AuthMethodToken uses a static Vault token.
	AuthMethodToken AuthMethod = "token"
)

// Config holds Vault client configuration.
type Config struct {
	// Address is the Vault server address (e.g., "https://vault.example.com:8200")
	Address string `json:"address" yaml:"address"`

	// AuthMethod specifies how to authenticate with Vault
	AuthMethod AuthMethod `json:"authMethod" yaml:"authMethod"`

	// Token for token-based auth (can also be set via VAULT_TOKEN env)
	Token string `json:"token,omitempty" yaml:"token,omitempty"`

	// KubernetesAuth configuration
	KubernetesAuth *KubernetesAuthConfig `json:"kubernetesAuth,omitempty" yaml:"kubernetesAuth,omitempty"`

	// AppRoleAuth configuration
	AppRoleAuth *AppRoleAuthConfig `json:"appRoleAuth,omitempty" yaml:"appRoleAuth,omitempty"`

	// TLSConfig for Vault server TLS
	TLSSkipVerify bool   `json:"tlsSkipVerify,omitempty" yaml:"tlsSkipVerify,omitempty"`
	CACert        string `json:"caCert,omitempty" yaml:"caCert,omitempty"`

	// Namespace for Vault Enterprise namespaces
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
}

// KubernetesAuthConfig configures Kubernetes auth method.
type KubernetesAuthConfig struct {
	// Role is the Vault role to authenticate as
	Role string `json:"role" yaml:"role"`

	// MountPath is the auth mount path (default: "kubernetes")
	MountPath string `json:"mountPath,omitempty" yaml:"mountPath,omitempty"`

	// ServiceAccountTokenPath is the path to the SA token file
	// (default: /var/run/secrets/kubernetes.io/serviceaccount/token)
	ServiceAccountTokenPath string `json:"serviceAccountTokenPath,omitempty" yaml:"serviceAccountTokenPath,omitempty"`
}

// AppRoleAuthConfig configures AppRole auth method.
type AppRoleAuthConfig struct {
	// RoleID is the AppRole role ID
	RoleID string `json:"roleId" yaml:"roleId"`

	// SecretID is the AppRole secret ID
	SecretID string `json:"secretId" yaml:"secretId"`

	// MountPath is the auth mount path (default: "approle")
	MountPath string `json:"mountPath,omitempty" yaml:"mountPath,omitempty"`
}

// Client wraps the Vault API client with NovaEdge-specific functionality.
type Client struct {
	config     *Config
	logger     *zap.Logger
	httpClient *vaultHTTPClient

	mu    sync.RWMutex
	token string

	// tokenExpiry tracks when the token expires for renewal
	tokenExpiry time.Time
}

// vaultHTTPClient is a minimal Vault HTTP API client to avoid
// large dependency on github.com/hashicorp/vault/api.
type vaultHTTPClient struct {
	address   string
	token     string
	namespace string
	caCert    string
	skipTLS   bool
}

// NewClient creates a new Vault client.
func NewClient(config *Config, logger *zap.Logger) (*Client, error) {
	if config == nil {
		return nil, errVaultConfigIsRequired
	}
	if config.Address == "" {
		// Try environment variable
		config.Address = os.Getenv("VAULT_ADDR")
		if config.Address == "" {
			return nil, errVaultAddressIsRequiredSetVAULTADDROr
		}
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	client := &Client{
		config: config,
		logger: logger,
		httpClient: &vaultHTTPClient{
			address:   config.Address,
			namespace: config.Namespace,
			caCert:    config.CACert,
			skipTLS:   config.TLSSkipVerify,
		},
	}

	return client, nil
}

// Authenticate performs authentication with Vault based on the configured method.
func (c *Client) Authenticate(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch c.config.AuthMethod {
	case AuthMethodToken:
		return c.authenticateToken()
	case AuthMethodKubernetes:
		return c.authenticateKubernetes(ctx)
	case AuthMethodAppRole:
		return c.authenticateAppRole(ctx)
	default:
		return fmt.Errorf("%w: %s", errUnsupportedAuthMethod, c.config.AuthMethod)
	}
}

// authenticateToken uses a static token for authentication.
func (c *Client) authenticateToken() error {
	token := c.config.Token
	if token == "" {
		token = os.Getenv("VAULT_TOKEN")
	}
	if token == "" {
		return errVaultTokenNotProvidedSetVAULTTOKENOr
	}

	c.token = token
	c.httpClient.token = token
	c.logger.Info("Authenticated with Vault using token")
	return nil
}

// authenticateKubernetes uses Kubernetes service account auth.
func (c *Client) authenticateKubernetes(ctx context.Context) error {
	config := c.config.KubernetesAuth
	if config == nil {
		return errKubernetesAuthConfigIsRequired
	}

	mountPath := config.MountPath
	if mountPath == "" {
		mountPath = "kubernetes"
	}

	saPath := config.ServiceAccountTokenPath
	if saPath == "" {
		saPath = "/var/run/secrets/kubernetes.io/serviceaccount/token" //nolint:gosec // G101: not a credential, standard Kubernetes service account path
	}

	jwt, err := os.ReadFile(filepath.Clean(saPath))
	if err != nil {
		return fmt.Errorf("failed to read service account token: %w", err)
	}

	// POST to /v1/auth/{mount}/login
	loginPath := fmt.Sprintf("auth/%s/login", mountPath)
	payload := map[string]any{
		"role": config.Role,
		"jwt":  string(jwt),
	}

	resp, err := c.httpClient.Write(ctx, loginPath, payload)
	if err != nil {
		return fmt.Errorf("kubernetes auth login failed: %w", err)
	}

	token, ok := resp.Auth["client_token"]
	if !ok {
		return errNoClientTokenInKubernetesAuthResponse
	}

	tokenStr, ok := token.(string)
	if !ok {
		return errClientTokenIsNotAString
	}

	c.token = tokenStr
	c.httpClient.token = tokenStr

	// Parse token TTL if available
	if leaseDuration, ok := resp.Auth["lease_duration"]; ok {
		if durationFloat, ok := leaseDuration.(float64); ok {
			c.tokenExpiry = time.Now().Add(time.Duration(durationFloat) * time.Second)
		}
	}

	c.logger.Info("Authenticated with Vault using Kubernetes auth",
		zap.String("role", config.Role),
		zap.String("mountPath", mountPath))

	return nil
}

// authenticateAppRole uses AppRole authentication.
func (c *Client) authenticateAppRole(ctx context.Context) error {
	config := c.config.AppRoleAuth
	if config == nil {
		return errApproleAuthConfigIsRequired
	}

	mountPath := config.MountPath
	if mountPath == "" {
		mountPath = "approle"
	}

	loginPath := fmt.Sprintf("auth/%s/login", mountPath)
	payload := map[string]any{
		"role_id":   config.RoleID,
		"secret_id": config.SecretID,
	}

	resp, err := c.httpClient.Write(ctx, loginPath, payload)
	if err != nil {
		return fmt.Errorf("approle auth login failed: %w", err)
	}

	token, ok := resp.Auth["client_token"]
	if !ok {
		return errNoClientTokenInApproleAuthResponse
	}

	tokenStr, ok := token.(string)
	if !ok {
		return errClientTokenIsNotAString
	}

	c.token = tokenStr
	c.httpClient.token = tokenStr

	if leaseDuration, ok := resp.Auth["lease_duration"]; ok {
		if durationFloat, ok := leaseDuration.(float64); ok {
			c.tokenExpiry = time.Now().Add(time.Duration(durationFloat) * time.Second)
		}
	}

	c.logger.Info("Authenticated with Vault using AppRole",
		zap.String("mountPath", mountPath))

	return nil
}

// GetToken returns the current Vault token.
func (c *Client) GetToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

// IsTokenExpiring returns true if the token will expire within the given duration.
func (c *Client) IsTokenExpiring(within time.Duration) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.tokenExpiry.IsZero() {
		return false
	}
	return time.Now().Add(within).After(c.tokenExpiry)
}

// Read reads a secret from Vault.
func (c *Client) Read(ctx context.Context, path string) (*Response, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.httpClient.Read(ctx, path)
}

// Write writes data to Vault.
func (c *Client) Write(ctx context.Context, path string, data map[string]any) (*Response, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.httpClient.Write(ctx, path, data)
}

// Close releases resources held by the client.
// TODO: Close should cancel any in-flight token renewal goroutines and
// clean up the underlying HTTP transport when connection pooling is added.
func (c *Client) Close() error {
	return nil
}

// ShouldEnable determines whether Vault integration should be enabled.
func ShouldEnable(ctx context.Context, config *Config, mode EnableMode, logger *zap.Logger) (bool, error) {
	switch mode {
	case EnableModeFalse:
		return false, nil
	case EnableModeTrue:
		client, err := NewClient(config, logger)
		if err != nil {
			return false, fmt.Errorf("vault is required but client creation failed: %w", err)
		}
		if err := client.Authenticate(ctx); err != nil {
			return false, fmt.Errorf("vault is required but authentication failed: %w", err)
		}
		return true, nil
	case EnableModeAuto:
		if config == nil || config.Address == "" {
			addr := os.Getenv("VAULT_ADDR")
			if addr == "" {
				logger.Info("Vault not configured, disabling")
				return false, nil
			}
		}
		client, err := NewClient(config, logger)
		if err != nil {
			logger.Info("Failed to create Vault client, disabling", zap.Error(err))
			return false, nil
		}
		if err := client.Authenticate(ctx); err != nil {
			logger.Info("Failed to authenticate with Vault, disabling", zap.Error(err))
			return false, nil
		}
		return true, nil
	default:
		return false, fmt.Errorf("%w: %s", errInvalidVaultEnableMode, mode)
	}
}
