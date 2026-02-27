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
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

var (
	errRoute53CredentialsMustIncludeAccessKeyIDAnd = errors.New("route53 credentials must include 'access_key_id' and 'secret_access_key'")
	errRoute53CredentialsMustIncludeHostedZoneID   = errors.New("route53 credentials must include 'hosted_zone_id'")
	errRoute53APIErrorStatus                       = errors.New("route53 API error (status")
)

const (
	route53APIBase = "https://route53.amazonaws.com"
)

// Route53Provider implements DNS-01 challenges using AWS Route 53.
// Uses lightweight HTTP calls instead of the full AWS SDK.
type Route53Provider struct {
	accessKeyID     string
	secretAccessKey string
	hostedZoneID    string
	region          string
	config          *ProviderConfig
	logger          *zap.Logger
	client          *http.Client
}

// NewRoute53Provider creates a new Route53 DNS provider.
// Credentials must include "access_key_id", "secret_access_key", and "hosted_zone_id".
// Optional: "region" (defaults to "us-east-1").
func NewRoute53Provider(credentials map[string]string, config *ProviderConfig) (*Route53Provider, error) {
	accessKey := credentials["access_key_id"]
	secretKey := credentials["secret_access_key"]
	hostedZone := credentials["hosted_zone_id"]

	if accessKey == "" || secretKey == "" {
		return nil, errRoute53CredentialsMustIncludeAccessKeyIDAnd
	}
	if hostedZone == "" {
		return nil, errRoute53CredentialsMustIncludeHostedZoneID
	}

	region := credentials["region"]
	if region == "" {
		region = "us-east-1"
	}

	return &Route53Provider{
		accessKeyID:     accessKey,
		secretAccessKey: secretKey,
		hostedZoneID:    hostedZone,
		region:          region,
		config:          config,
		logger:          config.Logger.Named("route53"),
		client:          &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// CreateTXTRecord creates a DNS TXT record in Route 53.
func (p *Route53Provider) CreateTXTRecord(ctx context.Context, fqdn, value string) error {
	return p.changeRecord(ctx, "UPSERT", fqdn, value)
}

// DeleteTXTRecord removes a DNS TXT record from Route 53.
func (p *Route53Provider) DeleteTXTRecord(ctx context.Context, fqdn, value string) error {
	return p.changeRecord(ctx, "DELETE", fqdn, value)
}

// WaitForPropagation waits for the TXT record to be visible in DNS.
func (p *Route53Provider) WaitForPropagation(ctx context.Context, fqdn, value string) error {
	return waitForDNSPropagation(ctx, fqdn, value, p.config.PropagationTimeout, p.config.PollingInterval, p.logger)
}

// changeRecord performs a Route 53 change resource record set operation.
func (p *Route53Provider) changeRecord(ctx context.Context, action, fqdn, value string) error {
	recordName := fqdn
	if !strings.HasSuffix(recordName, ".") {
		recordName += "."
	}

	// Build Route53 XML payload
	payload := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<ChangeResourceRecordSetsRequest xmlns="https://route53.amazonaws.com/doc/2013-04-01/">
  <ChangeBatch>
    <Comment>NovaEdge ACME DNS-01 challenge</Comment>
    <Changes>
      <Change>
        <Action>%s</Action>
        <ResourceRecordSet>
          <Name>%s</Name>
          <Type>TXT</Type>
          <TTL>60</TTL>
          <ResourceRecords>
            <ResourceRecord>
              <Value>"%s"</Value>
            </ResourceRecord>
          </ResourceRecords>
        </ResourceRecordSet>
      </Change>
    </Changes>
  </ChangeBatch>
</ChangeResourceRecordSetsRequest>`, action, recordName, value)

	url := fmt.Sprintf("%s/2013-04-01/hostedzone/%s/rrset",
		route53APIBase, p.hostedZoneID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url,
		bytes.NewBufferString(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/xml")

	// Sign the request with AWS Signature V4
	p.signRequest(req, []byte(payload))

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("route53 API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: %d): %s", errRoute53APIErrorStatus, resp.StatusCode, string(body))
	}

	p.logger.Info("Route53 record change successful",
		zap.String("action", action),
		zap.String("name", recordName),
		zap.String("zone", p.hostedZoneID))

	return nil
}

// signRequest adds AWS Signature Version 4 headers to the request.
func (p *Route53Provider) signRequest(req *http.Request, payload []byte) {
	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")

	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("Host", req.Host)

	// Compute payload hash
	payloadHash := sha256Hex(payload)

	// Canonical request
	canonicalHeaders := fmt.Sprintf("content-type:%s\nhost:%s\nx-amz-date:%s\n",
		req.Header.Get("Content-Type"), "route53.amazonaws.com", amzDate)
	signedHeaders := "content-type;host;x-amz-date"

	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		req.Method,
		req.URL.Path,
		req.URL.RawQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash)

	// String to sign
	credentialScope := fmt.Sprintf("%s/%s/route53/aws4_request", dateStamp, p.region)
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s",
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)))

	// Signing key
	signingKey := getSignatureKey(p.secretAccessKey, dateStamp, p.region, "route53")

	// Signature
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	// Authorization header
	authHeader := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		p.accessKeyID, credentialScope, signedHeaders, signature)

	req.Header.Set("Authorization", authHeader)
}

// sha256Hex returns the hex-encoded SHA256 hash of the data.
func sha256Hex(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// hmacSHA256 computes HMAC-SHA256.
func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

// getSignatureKey derives the AWS signing key.
func getSignatureKey(secret, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}

// Ensure xml import is used by referencing it.
var _ = xml.Header
