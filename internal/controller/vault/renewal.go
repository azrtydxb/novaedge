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
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	// defaultTokenRenewalThreshold is how long before expiry to renew the token.
	defaultTokenRenewalThreshold = 5 * time.Minute

	// defaultCertRenewalThreshold is how long before expiry to renew certificates.
	defaultCertRenewalThreshold = 24 * time.Hour

	// defaultCheckInterval is how often to check for renewals.
	defaultCheckInterval = 1 * time.Minute
)

// RenewalManager handles automatic renewal of Vault tokens and PKI certificates.
type RenewalManager struct {
	client     *Client
	pkiManager *PKIManager
	logger     *zap.Logger

	tokenRenewalThreshold time.Duration
	certRenewalThreshold  time.Duration
	checkInterval         time.Duration

	mu           sync.Mutex
	trackedCerts map[string]*trackedCertificate
	stopCh       chan struct{}
	running      bool

	// Callbacks
	onTokenRenewed func()
	onCertRenewed  func(name string, cert *PKICertificate)
	onRenewalError func(name string, err error)
}

// trackedCertificate is a certificate being tracked for renewal.
type trackedCertificate struct {
	name    string
	cert    *PKICertificate
	request *PKIRequest
}

// NewRenewalManager creates a new renewal manager.
func NewRenewalManager(client *Client, pkiManager *PKIManager, logger *zap.Logger) *RenewalManager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &RenewalManager{
		client:                client,
		pkiManager:            pkiManager,
		logger:                logger,
		tokenRenewalThreshold: defaultTokenRenewalThreshold,
		certRenewalThreshold:  defaultCertRenewalThreshold,
		checkInterval:         defaultCheckInterval,
		trackedCerts:          make(map[string]*trackedCertificate),
		stopCh:                make(chan struct{}),
	}
}

// SetTokenRenewalThreshold sets how long before expiry to renew the token.
func (r *RenewalManager) SetTokenRenewalThreshold(d time.Duration) {
	r.tokenRenewalThreshold = d
}

// SetCertRenewalThreshold sets how long before expiry to renew certificates.
func (r *RenewalManager) SetCertRenewalThreshold(d time.Duration) {
	r.certRenewalThreshold = d
}

// SetCheckInterval sets how often to check for renewals.
func (r *RenewalManager) SetCheckInterval(d time.Duration) {
	r.checkInterval = d
}

// OnTokenRenewed sets the callback for successful token renewal.
func (r *RenewalManager) OnTokenRenewed(fn func()) {
	r.onTokenRenewed = fn
}

// OnCertRenewed sets the callback for successful certificate renewal.
func (r *RenewalManager) OnCertRenewed(fn func(name string, cert *PKICertificate)) {
	r.onCertRenewed = fn
}

// OnRenewalError sets the callback for renewal errors.
func (r *RenewalManager) OnRenewalError(fn func(name string, err error)) {
	r.onRenewalError = fn
}

// TrackCertificate adds a certificate to the renewal tracker.
func (r *RenewalManager) TrackCertificate(name string, cert *PKICertificate, request *PKIRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.trackedCerts[name] = &trackedCertificate{
		name:    name,
		cert:    cert,
		request: request,
	}

	r.logger.Info("Tracking certificate for renewal",
		zap.String("name", name),
		zap.Time("expiresAt", cert.ExpiresAt))
}

// UntrackCertificate removes a certificate from the renewal tracker.
func (r *RenewalManager) UntrackCertificate(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.trackedCerts, name)
}

// Start begins the renewal loop.
func (r *RenewalManager) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return nil
	}
	r.running = true
	r.stopCh = make(chan struct{})
	r.mu.Unlock()

	r.logger.Info("Starting Vault renewal manager",
		zap.Duration("checkInterval", r.checkInterval),
		zap.Duration("tokenThreshold", r.tokenRenewalThreshold),
		zap.Duration("certThreshold", r.certRenewalThreshold))

	go r.run(ctx)
	return nil
}

// Stop stops the renewal manager.
func (r *RenewalManager) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.running {
		return
	}
	close(r.stopCh)
	r.running = false
	r.logger.Info("Vault renewal manager stopped")
}

// run is the main renewal loop.
func (r *RenewalManager) run(ctx context.Context) {
	ticker := time.NewTicker(r.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.Stop()
			return
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.checkTokenRenewal(ctx)
			r.checkCertRenewals(ctx)
		}
	}
}

// checkTokenRenewal checks if the Vault token needs renewal.
func (r *RenewalManager) checkTokenRenewal(ctx context.Context) {
	if !r.client.IsTokenExpiring(r.tokenRenewalThreshold) {
		return
	}

	r.logger.Info("Vault token expiring soon, renewing")

	// Renew token by re-authenticating
	if err := r.client.Authenticate(ctx); err != nil {
		r.logger.Error("Failed to renew Vault token", zap.Error(err))
		if r.onRenewalError != nil {
			r.onRenewalError("vault-token", err)
		}
		return
	}

	r.logger.Info("Vault token renewed")
	if r.onTokenRenewed != nil {
		r.onTokenRenewed()
	}
}

// checkCertRenewals checks tracked certificates for renewal.
func (r *RenewalManager) checkCertRenewals(ctx context.Context) {
	r.mu.Lock()
	// Make a copy to avoid holding the lock during renewal
	certs := make(map[string]*trackedCertificate, len(r.trackedCerts))
	for k, v := range r.trackedCerts {
		certs[k] = v
	}
	r.mu.Unlock()

	for name, tracked := range certs {
		if !r.pkiManager.ShouldRenew(tracked.cert, r.certRenewalThreshold) {
			continue
		}

		r.logger.Info("Certificate expiring soon, renewing",
			zap.String("name", name),
			zap.Time("expiresAt", tracked.cert.ExpiresAt))

		newCert, err := r.pkiManager.IssueCertificate(ctx, tracked.request)
		if err != nil {
			r.logger.Error("Failed to renew certificate",
				zap.String("name", name),
				zap.Error(err))
			if r.onRenewalError != nil {
				r.onRenewalError(name, err)
			}
			continue
		}

		// Update tracked certificate
		r.mu.Lock()
		if t, ok := r.trackedCerts[name]; ok {
			t.cert = newCert
		}
		r.mu.Unlock()

		r.logger.Info("Certificate renewed",
			zap.String("name", name),
			zap.Time("newExpiry", newCert.ExpiresAt))

		if r.onCertRenewed != nil {
			r.onCertRenewed(name, newCert)
		}
	}
}
