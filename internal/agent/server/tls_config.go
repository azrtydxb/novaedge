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
	"errors"
	"crypto/tls"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)
var (
	errNoTLSCertificatesConfigured = errors.New("no TLS certificates configured")
)


// createTLSConfigWithSNI creates a tls.Config with SNI support for multiple certificates
func (s *HTTPServer) createTLSConfigWithSNI(listener *pb.Listener) (*tls.Config, error) {
	// Parse all certificates for SNI
	certMap := make(map[string]*tls.Certificate)
	var defaultCert *tls.Certificate

	// If listener has tls_certificates map, use it for SNI
	if len(listener.TlsCertificates) > 0 {
		for hostname, tlsConfig := range listener.TlsCertificates {
			cert, err := tls.X509KeyPair(tlsConfig.Cert, tlsConfig.Key)
			if err != nil {
				s.logger.Error("Failed to parse certificate for hostname",
					zap.String("hostname", hostname),
					zap.Error(err))
				continue
			}
			certMap[hostname] = &cert

			// Use first certificate as default
			if defaultCert == nil {
				defaultCert = &cert
			}
		}
	}

	// Fallback to single TLS config if no SNI certificates
	if defaultCert == nil && listener.Tls != nil {
		cert, err := tls.X509KeyPair(listener.Tls.Cert, listener.Tls.Key)
		if err != nil {
			return nil, fmt.Errorf("failed to parse default certificate: %w", err)
		}
		defaultCert = &cert
	}

	if defaultCert == nil {
		return nil, errNoTLSCertificatesConfigured
	}

	// Get TLS settings from first certificate config
	var minVersion string
	var cipherSuites []string
	if listener.Tls != nil {
		minVersion = listener.Tls.MinVersion
		cipherSuites = listener.Tls.CipherSuites
	} else if len(listener.TlsCertificates) > 0 {
		// Get settings from first certificate
		for _, tlsConfig := range listener.TlsCertificates {
			minVersion = tlsConfig.MinVersion
			cipherSuites = tlsConfig.CipherSuites
			break
		}
	}

	config := &tls.Config{
		Certificates: []tls.Certificate{*defaultCert},
		MinVersion:   tls.VersionTLS13,
		CipherSuites: s.parseCipherSuites(cipherSuites),
	}

	// Allow TLS 1.2 only when explicitly configured (legacy compatibility)
	parsed := s.parseTLSVersion(minVersion)
	if parsed == tls.VersionTLS12 && minVersion == "TLS1.2" {
		s.logger.Warn("TLS 1.2 enabled for legacy compatibility, TLS 1.3 recommended")
		config.MinVersion = tls.VersionTLS12
		config.CipherSuites = aeadOnlyCipherSuites()
	}

	// Enable SNI certificate selection
	if len(certMap) > 0 {
		config.GetCertificate = func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			// Track TLS metrics
			metrics.TLSHandshakes.Inc()

			serverName := clientHello.ServerName
			if ce := s.logger.Check(zap.DebugLevel, "SNI certificate selection"); ce != nil {
				ce.Write(
					zap.String("server_name", serverName),
					zap.Int("available_certs", len(certMap)))
			}

			// Look for exact match
			if cert, ok := certMap[serverName]; ok {
				if ce := s.logger.Check(zap.DebugLevel, "SNI certificate found"); ce != nil {
					ce.Write(zap.String("server_name", serverName))
				}
				return cert, nil
			}

			// Check for wildcard match (*.example.com)
			if serverName != "" {
				labels := strings.Split(serverName, ".")
				if len(labels) > 1 {
					wildcardName := "*." + strings.Join(labels[1:], ".")
					if cert, ok := certMap[wildcardName]; ok {
						if ce := s.logger.Check(zap.DebugLevel, "SNI wildcard certificate found"); ce != nil {
							ce.Write(
								zap.String("server_name", serverName),
								zap.String("wildcard", wildcardName))
						}
						return cert, nil
					}
				}
			}

			// Return default certificate if no match
			if ce := s.logger.Check(zap.DebugLevel, "SNI using default certificate"); ce != nil {
				ce.Write(zap.String("server_name", serverName))
			}
			return defaultCert, nil
		}
	}

	return config, nil
}

// parseTLSVersion converts string TLS version to constant
func (s *HTTPServer) parseTLSVersion(version string) uint16 {
	switch version {
	case "TLS1.2":
		return tls.VersionTLS12
	case "TLS1.3":
		return tls.VersionTLS13
	default:
		return tls.VersionTLS13 // Default to TLS 1.3 for security
	}
}

// parseCipherSuites converts cipher suite names to constants
func (s *HTTPServer) parseCipherSuites(suites []string) []uint16 {
	if len(suites) == 0 {
		return nil // Use Go's default secure cipher suites
	}

	// Map of cipher suite names to constants
	cipherMap := map[string]uint16{
		"TLS_AES_128_GCM_SHA256":                        tls.TLS_AES_128_GCM_SHA256,
		"TLS_AES_256_GCM_SHA384":                        tls.TLS_AES_256_GCM_SHA384,
		"TLS_CHACHA20_POLY1305_SHA256":                  tls.TLS_CHACHA20_POLY1305_SHA256,
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256":         tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384":         tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256":       tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384":       tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256":   tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
		"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256": tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	}

	result := make([]uint16, 0, len(suites))
	for _, name := range suites {
		if id, ok := cipherMap[name]; ok {
			result = append(result, id)
		}
	}

	return result
}

// aeadOnlyCipherSuites returns the hardened set of AEAD-only cipher suites
// permitted when TLS 1.2 legacy mode is explicitly enabled.
func aeadOnlyCipherSuites() []uint16 {
	return []uint16{
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
		tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
	}
}

// createTLSConfigWithMTLS creates a TLS config with SNI and optional mTLS support
func (s *HTTPServer) createTLSConfigWithMTLS(listener *pb.Listener, clientAuth *pb.ClientAuthConfig, enableOCSP bool) (*tls.Config, error) {
	tlsConfig, err := s.createTLSConfigWithSNI(listener)
	if err != nil {
		return nil, err
	}

	// Apply mTLS client authentication if configured
	if clientAuth != nil {
		if err := s.applyClientAuthConfig(tlsConfig, clientAuth); err != nil {
			return nil, fmt.Errorf("failed to apply mTLS config: %w", err)
		}
	}

	// Set up OCSP stapling if enabled
	if enableOCSP && s.ocspStapler != nil {
		s.ocspStapler.StapleToConfig(tlsConfig)
		s.logger.Info("OCSP stapling enabled for listener",
			zap.String("listener", listener.Name),
		)
	}

	return tlsConfig, nil
}
