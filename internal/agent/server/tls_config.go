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
	"crypto/tls"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// createTLSConfig creates a tls.Config from protobuf TLS configuration with SNI support
func (s *HTTPServer) createTLSConfig(tlsConfig *pb.TLSConfig, hostnames []string) (*tls.Config, error) {
	// Parse default certificate and key
	cert, err := tls.X509KeyPair(tlsConfig.Cert, tlsConfig.Key)
	if err != nil {
		return nil, fmt.Errorf("failed to parse default certificate: %w", err)
	}

	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   s.parseTLSVersion(tlsConfig.MinVersion),
		CipherSuites: s.parseCipherSuites(tlsConfig.CipherSuites),
	}

	return config, nil
}

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
		return nil, fmt.Errorf("no TLS certificates configured")
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
		MinVersion:   s.parseTLSVersion(minVersion),
		CipherSuites: s.parseCipherSuites(cipherSuites),
	}

	// Enable SNI certificate selection
	if len(certMap) > 0 {
		config.GetCertificate = func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			// Track TLS metrics
			metrics.TLSHandshakes.Inc()

			serverName := clientHello.ServerName
			s.logger.Debug("SNI certificate selection",
				zap.String("server_name", serverName),
				zap.Int("available_certs", len(certMap)))

			// Look for exact match
			if cert, ok := certMap[serverName]; ok {
				s.logger.Debug("SNI certificate found",
					zap.String("server_name", serverName))
				return cert, nil
			}

			// Check for wildcard match (*.example.com)
			if serverName != "" {
				labels := strings.Split(serverName, ".")
				if len(labels) > 1 {
					wildcardName := "*." + strings.Join(labels[1:], ".")
					if cert, ok := certMap[wildcardName]; ok {
						s.logger.Debug("SNI wildcard certificate found",
							zap.String("server_name", serverName),
							zap.String("wildcard", wildcardName))
						return cert, nil
					}
				}
			}

			// Return default certificate if no match
			s.logger.Debug("SNI using default certificate",
				zap.String("server_name", serverName))
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

	var result []uint16
	for _, name := range suites {
		if id, ok := cipherMap[name]; ok {
			result = append(result, id)
		}
	}

	return result
}
