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
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

var (
	errVaultReturnedStatus = errors.New("vault returned status")
	errFailedToParseCACert = errors.New("failed to parse CA cert")
)

const (
	defaultHTTPTimeout = 30 * time.Second
	maxResponseSize    = 10 * 1024 * 1024 // 10MB
)

// Response represents a response from the Vault HTTP API.
type Response struct {
	// Auth contains authentication info (for login responses)
	Auth map[string]any `json:"auth,omitempty"`

	// Data contains the secret data
	Data map[string]any `json:"data,omitempty"`

	// LeaseDuration is the lease duration in seconds
	LeaseDuration int `json:"lease_duration,omitempty"`

	// LeaseID is the lease identifier
	LeaseID string `json:"lease_id,omitempty"`

	// Renewable indicates if the lease is renewable
	Renewable bool `json:"renewable,omitempty"`

	// Warnings from the Vault server
	Warnings []string `json:"warnings,omitempty"`
}

// Read reads from the Vault API (GET request).
func (c *vaultHTTPClient) Read(ctx context.Context, path string) (*Response, error) {
	url := c.buildURL(path)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	return c.doRequest(req)
}

// Write writes to the Vault API (POST/PUT request).
func (c *vaultHTTPClient) Write(ctx context.Context, path string, data map[string]any) (*Response, error) {
	url := c.buildURL(path)

	var body io.Reader
	if data != nil {
		jsonData, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request data: %w", err)
		}
		body = bytes.NewReader(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.doRequest(req)
}

// Delete sends a DELETE request to the Vault API.
func (c *vaultHTTPClient) Delete(ctx context.Context, path string) (*Response, error) {
	url := c.buildURL(path)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	return c.doRequest(req)
}

// buildURL constructs the full Vault API URL and validates it.
func (c *vaultHTTPClient) buildURL(vaultPath string) string {
	// Ensure path doesn't start with /v1/ (we'll add it)
	vaultPath = strings.TrimPrefix(vaultPath, "/")
	vaultPath = strings.TrimPrefix(vaultPath, "v1/")

	// Reject path traversal attempts
	cleaned := path.Clean(vaultPath)
	if strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, "/../") {
		return ""
	}

	result := fmt.Sprintf("%s/v1/%s", strings.TrimRight(c.address, "/"), cleaned)
	// Validate the URL to satisfy SSRF taint analysis
	if _, err := url.ParseRequestURI(result); err != nil {
		return ""
	}
	return result
}

// doRequest executes an HTTP request against the Vault API.
func (c *vaultHTTPClient) doRequest(req *http.Request) (*Response, error) {
	// Set Vault token header
	if c.token != "" {
		req.Header.Set("X-Vault-Token", c.token)
	}

	// Set namespace header for Vault Enterprise
	if c.namespace != "" {
		req.Header.Set("X-Vault-Namespace", c.namespace)
	}

	httpClient, err := c.cachedHTTPClient()
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req) //nolint:gosec // G704: URL validated via buildURL which calls url.ParseRequestURI
	if err != nil {
		return nil, fmt.Errorf("vault request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response body with size limit
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: %d: %s", errVaultReturnedStatus, resp.StatusCode, string(body))
	}

	// Parse response
	var vaultResp Response
	if len(body) > 0 {
		if err := json.Unmarshal(body, &vaultResp); err != nil {
			return nil, fmt.Errorf("failed to parse vault response: %w", err)
		}
	}

	return &vaultResp, nil
}

// cachedHTTPClient returns the shared HTTP client, initializing it once.
// The client is safe for concurrent use and reuses connections across requests.
func (c *vaultHTTPClient) cachedHTTPClient() (*http.Client, error) {
	var initErr error
	c.clientOnce.Do(func() {
		client, err := c.newHTTPClient()
		if err != nil {
			initErr = err
			return
		}
		c.client = client
	})
	if initErr != nil {
		return nil, initErr
	}
	return c.client, nil
}

// newHTTPClient creates an HTTP client with optional TLS configuration.
// Call cachedHTTPClient() instead to reuse the client across requests.
func (c *vaultHTTPClient) newHTTPClient() (*http.Client, error) {
	transport := &http.Transport{}

	if c.skipTLS {
		c.logger.Warn("Vault TLS verification is disabled (InsecureSkipVerify=true) -- this is insecure and should only be used in dev/test environments",
			zap.String("vault_address", c.address))
	}

	//nolint:gosec // G402: InsecureSkipVerify is user-configurable via ProxyVaultConfig CRD for dev/test environments
	tlsConfig := &tls.Config{
		InsecureSkipVerify: c.skipTLS,
	}

	if c.caCert != "" {
		caCert, err := os.ReadFile(filepath.Clean(c.caCert))
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, errFailedToParseCACert
		}
		tlsConfig.RootCAs = pool
	}

	transport.TLSClientConfig = tlsConfig

	return &http.Client{
		Transport: transport,
		Timeout:   defaultHTTPTimeout,
	}, nil
}

// HealthCheck performs a Vault health check.
func (c *vaultHTTPClient) HealthCheck(ctx context.Context) (*HealthStatus, error) {
	healthURL := fmt.Sprintf("%s/v1/sys/health", strings.TrimRight(c.address, "/"))
	if _, parseErr := url.ParseRequestURI(healthURL); parseErr != nil {
		return nil, fmt.Errorf("invalid vault health check URL: %w", parseErr)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create health check request: %w", err)
	}

	httpClient, err := c.cachedHTTPClient()
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req) //nolint:gosec // G704: URL validated via url.ParseRequestURI above
	if err != nil {
		return nil, fmt.Errorf("vault health check failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read health response: %w", err)
	}

	var health HealthStatus
	if err := json.Unmarshal(body, &health); err != nil {
		return nil, fmt.Errorf("failed to parse health response: %w", err)
	}

	health.StatusCode = resp.StatusCode
	return &health, nil
}

// HealthStatus represents the Vault health check response.
type HealthStatus struct {
	Initialized   bool   `json:"initialized"`
	Sealed        bool   `json:"sealed"`
	Standby       bool   `json:"standby"`
	ServerTimeUTC int64  `json:"server_time_utc"`
	Version       string `json:"version"`
	ClusterName   string `json:"cluster_name"`
	ClusterID     string `json:"cluster_id"`
	StatusCode    int    `json:"-"`
}

// IsHealthy returns true if Vault is initialized, unsealed, and active.
func (h *HealthStatus) IsHealthy() bool {
	return h.Initialized && !h.Sealed && !h.Standby
}
