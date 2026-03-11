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

// Package dns provides DNS-01 ACME challenge providers for various DNS services.
package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"go.uber.org/zap"
)

var (
	errUnsupportedDNSProvider   = errors.New("unsupported DNS provider")
	errDNSPropagationTimeoutFor = errors.New("DNS propagation timeout for")
)

const (
	// DefaultPropagationTimeout is the default time to wait for DNS propagation.
	DefaultPropagationTimeout = 120 * time.Second

	// DefaultPollingInterval is the default time between DNS checks.
	DefaultPollingInterval = 5 * time.Second
)

// Provider defines the interface for DNS-01 challenge providers.
// Implementations handle creating and removing DNS TXT records
// for ACME DNS-01 challenges.
type Provider interface {
	// CreateTXTRecord creates a DNS TXT record for ACME challenge validation.
	CreateTXTRecord(ctx context.Context, fqdn, value string) error

	// DeleteTXTRecord removes the DNS TXT record after validation.
	DeleteTXTRecord(ctx context.Context, fqdn, value string) error

	// WaitForPropagation waits until the TXT record is visible via DNS.
	WaitForPropagation(ctx context.Context, fqdn, value string) error
}

// ProviderConfig holds configuration common to all DNS providers.
type ProviderConfig struct {
	// PropagationTimeout is the maximum time to wait for DNS propagation.
	PropagationTimeout time.Duration

	// PollingInterval is the time between DNS propagation checks.
	PollingInterval time.Duration

	// Logger for the provider.
	Logger *zap.Logger
}

// ApplyDefaults fills in default values for empty fields.
func (c *ProviderConfig) ApplyDefaults() {
	if c.PropagationTimeout == 0 {
		c.PropagationTimeout = DefaultPropagationTimeout
	}
	if c.PollingInterval == 0 {
		c.PollingInterval = DefaultPollingInterval
	}
	if c.Logger == nil {
		c.Logger = zap.NewNop()
	}
}

// NewProvider creates a DNS provider based on the provider name and credentials.
func NewProvider(name string, credentials map[string]string, config *ProviderConfig) (Provider, error) {
	if config == nil {
		config = &ProviderConfig{}
	}
	config.ApplyDefaults()

	switch name {
	case "cloudflare":
		return NewCloudflareProvider(credentials, config)
	case "route53":
		return NewRoute53Provider(credentials, config)
	case "googledns":
		return NewGoogleDNSProvider(credentials, config)
	default:
		return nil, fmt.Errorf("%w: %s (supported: cloudflare, route53, googledns)", errUnsupportedDNSProvider, name)
	}
}

// DNS01ChallengeProvider wraps a DNS Provider to implement the lego challenge.Provider interface.
type DNS01ChallengeProvider struct {
	provider Provider
	logger   *zap.Logger
	ctx      context.Context //nolint:containedctx // required: lego interface has no context param
}

// NewDNS01ChallengeProvider creates a new DNS-01 challenge provider wrapping a DNS Provider.
func NewDNS01ChallengeProvider(provider Provider, logger *zap.Logger) *DNS01ChallengeProvider {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &DNS01ChallengeProvider{
		provider: provider,
		logger:   logger,
		ctx:      context.Background(),
	}
}

// WithContext returns a copy of the provider that uses the given context for
// DNS operations, making Present/CleanUp cancellable.
func (p *DNS01ChallengeProvider) WithContext(ctx context.Context) *DNS01ChallengeProvider {
	cp := *p
	cp.ctx = ctx
	return &cp
}

// Present creates the DNS TXT record for the challenge.
func (p *DNS01ChallengeProvider) Present(domain, _, keyAuth string) error {
	fqdn := fmt.Sprintf("_acme-challenge.%s.", domain)

	p.logger.Info("Creating DNS-01 challenge record",
		zap.String("domain", domain),
		zap.String("fqdn", fqdn))

	if err := p.provider.CreateTXTRecord(p.ctx, fqdn, keyAuth); err != nil {
		return fmt.Errorf("failed to create TXT record for %s: %w", domain, err)
	}

	// Wait for propagation
	if err := p.provider.WaitForPropagation(p.ctx, fqdn, keyAuth); err != nil {
		p.logger.Warn("DNS propagation wait failed, continuing anyway",
			zap.String("domain", domain),
			zap.Error(err))
	}

	return nil
}

// CleanUp removes the DNS TXT record after the challenge.
func (p *DNS01ChallengeProvider) CleanUp(domain, _, keyAuth string) error {
	fqdn := fmt.Sprintf("_acme-challenge.%s.", domain)

	p.logger.Info("Cleaning up DNS-01 challenge record",
		zap.String("domain", domain),
		zap.String("fqdn", fqdn))

	return p.provider.DeleteTXTRecord(p.ctx, fqdn, keyAuth)
}

// waitForDNSPropagation polls DNS until the expected TXT record is visible or the timeout expires.
// This shared implementation is used by all DNS provider WaitForPropagation methods.
func waitForDNSPropagation(ctx context.Context, fqdn, value string, propagationTimeout, pollingInterval time.Duration, logger *zap.Logger) error {
	timeout := time.After(propagationTimeout)
	ticker := time.NewTicker(pollingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("%w: %s after %s", errDNSPropagationTimeoutFor, fqdn, propagationTimeout)
		case <-ticker.C:
			resolver := &net.Resolver{}
			records, err := resolver.LookupTXT(ctx, strings.TrimSuffix(fqdn, "."))
			if err != nil {
				logger.Debug("DNS lookup not yet propagated",
					zap.String("fqdn", fqdn),
					zap.Error(err))
				continue
			}
			for _, record := range records {
				if record == value {
					logger.Info("DNS propagation confirmed", zap.String("fqdn", fqdn))
					return nil
				}
			}
		}
	}
}
