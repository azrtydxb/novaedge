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
	googleDNSAPIBase = "https://dns.googleapis.com/dns/v1"
)

// GoogleDNSProvider implements DNS-01 challenges using Google Cloud DNS.
// Uses service account JSON credentials and direct HTTP API calls.
type GoogleDNSProvider struct {
	project     string
	managedZone string
	accessToken string
	saJSON      string
	config      *ProviderConfig
	logger      *zap.Logger
	client      *http.Client
}

// NewGoogleDNSProvider creates a new Google Cloud DNS provider.
// Credentials must include "project", "managed_zone", and either "access_token"
// or "service_account_json".
func NewGoogleDNSProvider(credentials map[string]string, config *ProviderConfig) (*GoogleDNSProvider, error) {
	project := credentials["project"]
	managedZone := credentials["managed_zone"]

	if project == "" {
		return nil, fmt.Errorf("googledns credentials must include 'project'")
	}
	if managedZone == "" {
		return nil, fmt.Errorf("googledns credentials must include 'managed_zone'")
	}

	accessToken := credentials["access_token"]
	saJSON := credentials["service_account_json"]

	if accessToken == "" && saJSON == "" {
		return nil, fmt.Errorf("googledns credentials must include 'access_token' or 'service_account_json'")
	}

	return &GoogleDNSProvider{
		project:     project,
		managedZone: managedZone,
		accessToken: accessToken,
		saJSON:      saJSON,
		config:      config,
		logger:      config.Logger.Named("googledns"),
		client:      &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// googleDNSChange represents a Google Cloud DNS change request.
type googleDNSChange struct {
	Additions []googleDNSResourceRecordSet `json:"additions,omitempty"`
	Deletions []googleDNSResourceRecordSet `json:"deletions,omitempty"`
}

// googleDNSResourceRecordSet represents a DNS resource record set.
type googleDNSResourceRecordSet struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	TTL     int      `json:"ttl"`
	Rrdatas []string `json:"rrdatas"`
}

// CreateTXTRecord creates a DNS TXT record in Google Cloud DNS.
func (p *GoogleDNSProvider) CreateTXTRecord(ctx context.Context, fqdn, value string) error {
	recordName := fqdn
	if !strings.HasSuffix(recordName, ".") {
		recordName += "."
	}

	change := googleDNSChange{
		Additions: []googleDNSResourceRecordSet{
			{
				Name:    recordName,
				Type:    "TXT",
				TTL:     60,
				Rrdatas: []string{fmt.Sprintf(`"%s"`, value)},
			},
		},
	}

	return p.submitChange(ctx, &change)
}

// DeleteTXTRecord removes a DNS TXT record from Google Cloud DNS.
func (p *GoogleDNSProvider) DeleteTXTRecord(ctx context.Context, fqdn, value string) error {
	recordName := fqdn
	if !strings.HasSuffix(recordName, ".") {
		recordName += "."
	}

	change := googleDNSChange{
		Deletions: []googleDNSResourceRecordSet{
			{
				Name:    recordName,
				Type:    "TXT",
				TTL:     60,
				Rrdatas: []string{fmt.Sprintf(`"%s"`, value)},
			},
		},
	}

	return p.submitChange(ctx, &change)
}

// WaitForPropagation waits for the TXT record to be visible in DNS.
func (p *GoogleDNSProvider) WaitForPropagation(ctx context.Context, fqdn, value string) error {
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
					p.logger.Info("DNS propagation confirmed", zap.String("fqdn", fqdn))
					return nil
				}
			}
		}
	}
}

// submitChange submits a DNS change request to Google Cloud DNS.
func (p *GoogleDNSProvider) submitChange(ctx context.Context, change *googleDNSChange) error {
	jsonData, err := json.Marshal(change)
	if err != nil {
		return fmt.Errorf("failed to marshal change: %w", err)
	}

	url := fmt.Sprintf("%s/projects/%s/managedZones/%s/changes",
		googleDNSAPIBase, p.project, p.managedZone)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Set authorization
	if p.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.accessToken)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("google cloud DNS API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("google cloud DNS API error (status %d): %s",
			resp.StatusCode, string(body))
	}

	p.logger.Info("Google Cloud DNS change submitted",
		zap.String("project", p.project),
		zap.String("zone", p.managedZone))

	return nil
}
