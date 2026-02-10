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

package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/ocsp"
)

const (
	// ocspRefreshInterval is how often to check if OCSP responses need refreshing
	ocspRefreshInterval = 1 * time.Hour

	// ocspRefreshBeforeExpiry refreshes OCSP response this long before expiry
	ocspRefreshBeforeExpiry = 24 * time.Hour

	// ocspHTTPTimeout is the timeout for OCSP HTTP requests
	ocspHTTPTimeout = 10 * time.Second

	// ocspMaxResponseSize is the maximum size of an OCSP response (1 MB)
	ocspMaxResponseSize = 1 << 20
)

// OCSPStapler manages OCSP stapling for TLS certificates.
// It fetches OCSP responses from the certificate's OCSP responder and
// attaches them to TLS handshakes. A background goroutine refreshes
// responses before they expire.
type OCSPStapler struct {
	logger *zap.Logger

	mu           sync.RWMutex
	certificates map[string]*ocspCertEntry // hostname -> entry

	cancel context.CancelFunc
	done   chan struct{}

	// Status tracking
	lastRefresh atomic.Int64 // Unix timestamp of last successful refresh
	lastError   atomic.Value // last error message (string)
}

type ocspCertEntry struct {
	cert       *tls.Certificate
	issuer     *x509.Certificate
	response   []byte
	nextUpdate time.Time
}

// NewOCSPStapler creates a new OCSP stapler
func NewOCSPStapler(logger *zap.Logger) *OCSPStapler {
	return &OCSPStapler{
		logger:       logger,
		certificates: make(map[string]*ocspCertEntry),
		done:         make(chan struct{}),
	}
}

// Start begins the background OCSP response refresh goroutine
func (s *OCSPStapler) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	go s.refreshLoop(ctx)
}

// Stop stops the background refresh goroutine
func (s *OCSPStapler) Stop() {
	if s.cancel != nil {
		s.cancel()
		<-s.done
	}
}

// AddCertificate registers a certificate for OCSP stapling.
// The hostname is used as a key for lookup. The issuer certificate is required
// for building the OCSP request.
func (s *OCSPStapler) AddCertificate(hostname string, cert *tls.Certificate, issuerPEM []byte) error {
	if cert == nil || len(cert.Certificate) == 0 {
		return fmt.Errorf("certificate is nil or empty")
	}

	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return fmt.Errorf("failed to parse leaf certificate: %w", err)
	}

	if len(leaf.OCSPServer) == 0 {
		s.logger.Info("Certificate has no OCSP responder, skipping stapling",
			zap.String("hostname", hostname),
		)
		return nil
	}

	// Parse issuer certificate
	var issuer *x509.Certificate
	if len(issuerPEM) > 0 {
		block, _ := pem.Decode(issuerPEM)
		if block != nil {
			issuer, err = x509.ParseCertificate(block.Bytes)
			if err != nil {
				s.logger.Warn("Failed to parse issuer certificate for OCSP",
					zap.String("hostname", hostname),
					zap.Error(err),
				)
			}
		}
	}

	// Try to find issuer in the certificate chain
	if issuer == nil && len(cert.Certificate) > 1 {
		issuer, err = x509.ParseCertificate(cert.Certificate[1])
		if err != nil {
			s.logger.Warn("Failed to parse issuer from certificate chain",
				zap.String("hostname", hostname),
				zap.Error(err),
			)
		}
	}

	if issuer == nil {
		s.logger.Warn("No issuer certificate available for OCSP stapling",
			zap.String("hostname", hostname),
		)
		return nil
	}

	entry := &ocspCertEntry{
		cert:   cert,
		issuer: issuer,
	}

	// Fetch initial OCSP response
	if err := s.fetchOCSPResponse(entry, leaf); err != nil {
		s.logger.Warn("Failed to fetch initial OCSP response, will retry later",
			zap.String("hostname", hostname),
			zap.Error(err),
		)
	}

	s.mu.Lock()
	s.certificates[hostname] = entry
	s.mu.Unlock()

	return nil
}

// StapleToConfig configures a tls.Config to use OCSP stapling
func (s *OCSPStapler) StapleToConfig(tlsConfig *tls.Config) {
	origGetCert := tlsConfig.GetCertificate

	tlsConfig.GetCertificate = func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		// Get the certificate from the original handler
		var cert *tls.Certificate
		var err error

		if origGetCert != nil {
			cert, err = origGetCert(hello)
		}
		if err != nil {
			return nil, err
		}

		// Look up OCSP staple
		s.mu.RLock()
		entry, ok := s.certificates[hello.ServerName]
		s.mu.RUnlock()

		if ok && len(entry.response) > 0 {
			// Return a copy of the certificate with the OCSP staple
			if cert != nil {
				stapled := *cert
				stapled.OCSPStaple = entry.response
				return &stapled, nil
			}
		}

		return cert, nil
	}
}

// Status returns the current OCSP stapling status
func (s *OCSPStapler) Status() OCSPStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := OCSPStatus{
		LastRefresh:  time.Unix(s.lastRefresh.Load(), 0),
		Certificates: make(map[string]OCSPCertStatus),
	}

	if errVal := s.lastError.Load(); errVal != nil {
		errStr, ok := errVal.(string)
		if ok {
			status.LastError = errStr
		}
	}

	for hostname, entry := range s.certificates {
		certStatus := OCSPCertStatus{
			HasStaple:  len(entry.response) > 0,
			NextUpdate: entry.nextUpdate,
		}
		status.Certificates[hostname] = certStatus
	}

	return status
}

// OCSPStatus represents the current OCSP stapling status
type OCSPStatus struct {
	LastRefresh  time.Time
	LastError    string
	Certificates map[string]OCSPCertStatus
}

// OCSPCertStatus represents OCSP status for a single certificate
type OCSPCertStatus struct {
	HasStaple  bool
	NextUpdate time.Time
}

func (s *OCSPStapler) refreshLoop(ctx context.Context) {
	defer close(s.done)

	ticker := time.NewTicker(ocspRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshAll()
		}
	}
}

func (s *OCSPStapler) refreshAll() {
	s.mu.RLock()
	entries := make(map[string]*ocspCertEntry, len(s.certificates))
	for k, v := range s.certificates {
		entries[k] = v
	}
	s.mu.RUnlock()

	for hostname, entry := range entries {
		// Check if refresh is needed
		if len(entry.response) > 0 && time.Until(entry.nextUpdate) > ocspRefreshBeforeExpiry {
			continue
		}

		if len(entry.cert.Certificate) == 0 {
			continue
		}

		leaf, err := x509.ParseCertificate(entry.cert.Certificate[0])
		if err != nil {
			s.logger.Warn("Failed to parse certificate for OCSP refresh",
				zap.String("hostname", hostname),
				zap.Error(err),
			)
			continue
		}

		if err := s.fetchOCSPResponse(entry, leaf); err != nil {
			s.logger.Warn("Failed to refresh OCSP response",
				zap.String("hostname", hostname),
				zap.Error(err),
			)
			s.lastError.Store(err.Error())
		} else {
			s.lastRefresh.Store(time.Now().Unix())
			s.logger.Debug("OCSP response refreshed",
				zap.String("hostname", hostname),
				zap.Time("next_update", entry.nextUpdate),
			)
		}
	}
}

func (s *OCSPStapler) fetchOCSPResponse(entry *ocspCertEntry, leaf *x509.Certificate) error {
	if len(leaf.OCSPServer) == 0 {
		return fmt.Errorf("certificate has no OCSP responder URL")
	}

	ocspReq, err := ocsp.CreateRequest(leaf, entry.issuer, nil)
	if err != nil {
		return fmt.Errorf("failed to create OCSP request: %w", err)
	}

	responderURL := leaf.OCSPServer[0]
	httpClient := &http.Client{Timeout: ocspHTTPTimeout}

	httpResp, err := httpClient.Post(responderURL, "application/ocsp-request", bytes.NewReader(ocspReq))
	if err != nil {
		return fmt.Errorf("OCSP request failed: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	if httpResp.StatusCode != http.StatusOK {
		return fmt.Errorf("OCSP responder returned status %d", httpResp.StatusCode)
	}

	respBytes, err := io.ReadAll(io.LimitReader(httpResp.Body, ocspMaxResponseSize))
	if err != nil {
		return fmt.Errorf("failed to read OCSP response: %w", err)
	}

	ocspResp, err := ocsp.ParseResponse(respBytes, entry.issuer)
	if err != nil {
		return fmt.Errorf("failed to parse OCSP response: %w", err)
	}

	if ocspResp.Status != ocsp.Good {
		return fmt.Errorf("OCSP response status is not good: %d", ocspResp.Status)
	}

	entry.response = respBytes
	entry.nextUpdate = ocspResp.NextUpdate

	return nil
}
