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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
)

var (
	errCloudflareCredentialsMustIncludeAPIToken = errors.New("cloudflare credentials must include 'api_token'")
	errCloudflareAPIError                       = errors.New("cloudflare API error")
	errInvalidDomain                            = errors.New("invalid domain")
	errNoCloudflareZoneFoundFor                 = errors.New("no Cloudflare zone found for")
	errTXTRecordNotFoundFor                     = errors.New("TXT record not found for")
)

const (
	cloudflareAPIBase = "https://api.cloudflare.com/client/v4"
	maxResponseBody   = 1024 * 1024 // 1MB
)

// CloudflareProvider implements DNS-01 challenges using the Cloudflare API.
type CloudflareProvider struct {
	apiToken string
	config   *ProviderConfig
	logger   *zap.Logger
	client   *http.Client
}

// NewCloudflareProvider creates a new Cloudflare DNS provider.
// Credentials must include "api_token".
func NewCloudflareProvider(credentials map[string]string, config *ProviderConfig) (*CloudflareProvider, error) {
	apiToken := credentials["api_token"]
	if apiToken == "" {
		return nil, errCloudflareCredentialsMustIncludeAPIToken
	}

	return &CloudflareProvider{
		apiToken: apiToken,
		config:   config,
		logger:   config.Logger.Named("cloudflare"),
		client:   &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// cloudflareZone represents a Cloudflare DNS zone.
type cloudflareZone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// cloudflareRecord represents a Cloudflare DNS record.
type cloudflareRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
}

type cloudflareResponse struct {
	Success  bool                     `json:"success"`
	Errors   []cloudflareError        `json:"errors"`
	Result   json.RawMessage          `json:"result"`
	Messages []map[string]interface{} `json:"messages"`
}

type cloudflareError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// CreateTXTRecord creates a DNS TXT record in Cloudflare.
func (p *CloudflareProvider) CreateTXTRecord(ctx context.Context, fqdn, value string) error {
	recordName := strings.TrimSuffix(fqdn, ".")

	zoneID, err := p.findZoneID(ctx, recordName)
	if err != nil {
		return fmt.Errorf("failed to find zone for %s: %w", recordName, err)
	}

	payload := map[string]interface{}{
		"type":    "TXT",
		"name":    recordName,
		"content": value,
		"ttl":     60,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	apiURL := fmt.Sprintf("%s/zones/%s/dns_records", cloudflareAPIBase, zoneID)
	if _, parseErr := url.ParseRequestURI(apiURL); parseErr != nil {
		return fmt.Errorf("invalid cloudflare API URL: %w", parseErr)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var cfResp cloudflareResponse
	if err := json.Unmarshal(body, &cfResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !cfResp.Success {
		return fmt.Errorf("%w: %v", errCloudflareAPIError, cfResp.Errors)
	}

	p.logger.Info("Created Cloudflare TXT record",
		zap.String("name", recordName),
		zap.String("zone", zoneID))

	return nil
}

// DeleteTXTRecord removes a DNS TXT record from Cloudflare.
func (p *CloudflareProvider) DeleteTXTRecord(ctx context.Context, fqdn, value string) error {
	recordName := strings.TrimSuffix(fqdn, ".")

	zoneID, err := p.findZoneID(ctx, recordName)
	if err != nil {
		return fmt.Errorf("failed to find zone for %s: %w", recordName, err)
	}

	// Find the record
	recordID, err := p.findRecord(ctx, zoneID, recordName, value)
	if err != nil {
		return fmt.Errorf("failed to find record: %w", err)
	}

	// Delete the record
	deleteURL := fmt.Sprintf("%s/zones/%s/dns_records/%s", cloudflareAPIBase, zoneID, recordID)
	if _, parseErr := url.ParseRequestURI(deleteURL); parseErr != nil {
		return fmt.Errorf("invalid cloudflare API URL: %w", parseErr)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, deleteURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiToken)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	p.logger.Info("Deleted Cloudflare TXT record",
		zap.String("name", recordName),
		zap.String("zone", zoneID))

	return nil
}

// WaitForPropagation waits for the TXT record to be visible in DNS.
func (p *CloudflareProvider) WaitForPropagation(ctx context.Context, fqdn, value string) error {
	return waitForDNSPropagation(ctx, fqdn, value, p.config.PropagationTimeout, p.config.PollingInterval, p.logger)
}

// findZoneID finds the Cloudflare zone ID for a given record name.
func (p *CloudflareProvider) findZoneID(ctx context.Context, recordName string) (string, error) {
	// Extract the root domain (last two labels for basic domains)
	parts := strings.Split(recordName, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("%w: %s", errInvalidDomain, recordName)
	}

	// Try progressively shorter domain parts to find the zone
	for i := range len(parts) - 1 {
		zoneName := strings.Join(parts[i:], ".")

		zoneURL := fmt.Sprintf("%s/zones?name=%s", cloudflareAPIBase, url.QueryEscape(zoneName))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, zoneURL, nil)
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+p.apiToken)

		resp, err := p.client.Do(req)
		if err != nil {
			return "", fmt.Errorf("cloudflare API request failed: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
		if err != nil {
			return "", fmt.Errorf("failed to read response: %w", err)
		}

		var cfResp cloudflareResponse
		if err := json.Unmarshal(body, &cfResp); err != nil {
			return "", fmt.Errorf("failed to parse response: %w", err)
		}

		var zones []cloudflareZone
		if err := json.Unmarshal(cfResp.Result, &zones); err != nil {
			continue
		}

		if len(zones) > 0 {
			return zones[0].ID, nil
		}
	}

	return "", fmt.Errorf("%w: %s", errNoCloudflareZoneFoundFor, recordName)
}

// findRecord finds a specific TXT record in Cloudflare.
func (p *CloudflareProvider) findRecord(ctx context.Context, zoneID, name, content string) (string, error) {
	recordURL := fmt.Sprintf("%s/zones/%s/dns_records?type=TXT&name=%s", cloudflareAPIBase, zoneID, url.QueryEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, recordURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiToken)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("cloudflare API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var cfResp cloudflareResponse
	if err := json.Unmarshal(body, &cfResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	var records []cloudflareRecord
	if err := json.Unmarshal(cfResp.Result, &records); err != nil {
		return "", fmt.Errorf("failed to parse records: %w", err)
	}

	for _, r := range records {
		if r.Content == content {
			return r.ID, nil
		}
	}

	return "", fmt.Errorf("%w: %s with content %s", errTXTRecordNotFoundFor, name, content)
}
