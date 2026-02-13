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
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
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
		return nil, fmt.Errorf("cloudflare credentials must include 'api_token'")
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

	url := fmt.Sprintf("%s/zones/%s/dns_records", cloudflareAPIBase, zoneID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
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
		return fmt.Errorf("cloudflare API error: %v", cfResp.Errors)
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
	url := fmt.Sprintf("%s/zones/%s/dns_records/%s", cloudflareAPIBase, zoneID, recordID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
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
	timeout := time.After(p.config.PropagationTimeout)
	ticker := time.NewTicker(p.config.PollingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("DNS propagation timeout for %s after %s", fqdn, p.config.PropagationTimeout)
		case <-ticker.C:
			resolver := &net.Resolver{}
			records, err := resolver.LookupTXT(ctx, strings.TrimSuffix(fqdn, "."))
			if err != nil {
				p.logger.Debug("DNS lookup not yet propagated",
					zap.String("fqdn", fqdn),
					zap.Error(err))
				continue
			}
			for _, record := range records {
				if record == value {
					p.logger.Info("DNS propagation confirmed",
						zap.String("fqdn", fqdn))
					return nil
				}
			}
		}
	}
}

// findZoneID finds the Cloudflare zone ID for a given record name.
func (p *CloudflareProvider) findZoneID(ctx context.Context, recordName string) (string, error) {
	// Extract the root domain (last two labels for basic domains)
	parts := strings.Split(recordName, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid domain: %s", recordName)
	}

	// Try progressively shorter domain parts to find the zone
	for i := range len(parts) - 1 {
		zoneName := strings.Join(parts[i:], ".")

		url := fmt.Sprintf("%s/zones?name=%s", cloudflareAPIBase, zoneName)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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

	return "", fmt.Errorf("no Cloudflare zone found for %s", recordName)
}

// findRecord finds a specific TXT record in Cloudflare.
func (p *CloudflareProvider) findRecord(ctx context.Context, zoneID, name, content string) (string, error) {
	url := fmt.Sprintf("%s/zones/%s/dns_records?type=TXT&name=%s", cloudflareAPIBase, zoneID, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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

	return "", fmt.Errorf("TXT record not found for %s with content %s", name, content)
}
