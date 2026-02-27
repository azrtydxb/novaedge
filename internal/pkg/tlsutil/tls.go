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

// Package tlsutil provides centralized TLS configuration utilities for NovaEdge.
//
// This package offers hardened TLS configurations that enforce:
//   - TLS 1.3 minimum version (TLS 1.2 optional for compatibility)
//   - Secure cipher suites (AEAD ciphers only)
//   - Proper certificate validation
//   - SNI support for multi-certificate scenarios
//
// Usage:
//
//	// Create server TLS config with SNI
//	config, err := CreateServerTLSConfig(
//	    WithCertificatesFromFiles(certFile, keyFile),
//	    WithMinTLSVersion(tls.VersionTLS13),
//	    WithSecureCipherSuites(),
//	)
//
//	// Create client TLS config
//	config, err := CreateClientTLSConfig(
//	    WithCertificatesFromFiles(certFile, keyFile),
//	    WithRootCAs(caPool),
//	    WithServerName("backend.example.com"),
//	)
package tlsutil

import (
	"errors"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/grpc/credentials"

	pkgerrors "github.com/piwi3910/novaedge/internal/pkg/errors"
)
var (
	errFailedToAddCACertificateToPool = errors.New("failed to add CA certificate to pool")
)


// LoadServerTLSCredentials loads TLS credentials for gRPC server with mTLS.
// It uses GetCertificate/GetConfigForClient callbacks so that certificate files
// are re-read from disk on each new TLS handshake, enabling rotation without restart.
func LoadServerTLSCredentials(certFile, keyFile, caFile string) (credentials.TransportCredentials, error) {
	// Verify the certificate files are readable at startup
	if _, err := tls.LoadX509KeyPair(certFile, keyFile); err != nil {
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}

	// Load CA certificate for client verification
	caCert, err := os.ReadFile(filepath.Clean(caFile))
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caCert) {
		return nil, errFailedToAddCACertificateToPool
	}

	cleanCertFile := filepath.Clean(certFile)
	cleanKeyFile := filepath.Clean(keyFile)

	// Create TLS configuration with dynamic certificate loading
	config := &tls.Config{
		GetCertificate: func(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
			cert, err := tls.LoadX509KeyPair(cleanCertFile, cleanKeyFile)
			if err != nil {
				return nil, fmt.Errorf("failed to reload server certificate: %w", err)
			}
			return &cert, nil
		},
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  certPool,
		MinVersion: tls.VersionTLS13,
	}

	return credentials.NewTLS(config), nil
}

// LoadClientTLSCredentials loads TLS credentials for gRPC client with mTLS.
// It uses GetClientCertificate callback so that certificate files are re-read
// from disk on each new TLS handshake, enabling rotation without restart.
func LoadClientTLSCredentials(certFile, keyFile, caFile, serverName string) (credentials.TransportCredentials, error) {
	// Verify the certificate files are readable at startup
	if _, err := tls.LoadX509KeyPair(certFile, keyFile); err != nil {
		return nil, fmt.Errorf("failed to load client certificate: %w", err)
	}

	// Load CA certificate for server verification
	caCert, err := os.ReadFile(filepath.Clean(caFile))
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caCert) {
		return nil, errFailedToAddCACertificateToPool
	}

	cleanCertFile := filepath.Clean(certFile)
	cleanKeyFile := filepath.Clean(keyFile)

	// Create TLS configuration with dynamic certificate loading
	config := &tls.Config{
		GetClientCertificate: func(_ *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			cert, err := tls.LoadX509KeyPair(cleanCertFile, cleanKeyFile)
			if err != nil {
				return nil, fmt.Errorf("failed to reload client certificate: %w", err)
			}
			return &cert, nil
		},
		RootCAs:    certPool,
		ServerName: serverName,
		MinVersion: tls.VersionTLS13,
	}

	return credentials.NewTLS(config), nil
}

// LoadServerTLSCredentialsFromMemory loads TLS credentials from byte slices (for Kubernetes secrets)
func LoadServerTLSCredentialsFromMemory(certPEM, keyPEM, caPEM []byte) (credentials.TransportCredentials, error) {
	// Load server certificate and key from memory
	serverCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}

	// Load CA certificate for client verification
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caPEM) {
		return nil, errFailedToAddCACertificateToPool
	}

	// Create TLS configuration with mutual TLS
	config := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    certPool,
		MinVersion:   tls.VersionTLS13,
	}

	return credentials.NewTLS(config), nil
}

// LoadClientTLSCredentialsFromMemory loads TLS credentials from byte slices (for Kubernetes secrets)
func LoadClientTLSCredentialsFromMemory(certPEM, keyPEM, caPEM []byte, serverName string) (credentials.TransportCredentials, error) {
	// Load client certificate and key from memory
	clientCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificate: %w", err)
	}

	// Load CA certificate for server verification
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caPEM) {
		return nil, errFailedToAddCACertificateToPool
	}

	// Create TLS configuration with mutual TLS
	config := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      certPool,
		ServerName:   serverName,
		MinVersion:   tls.VersionTLS13,
	}

	return credentials.NewTLS(config), nil
}

// SecureCipherSuites returns a list of secure cipher suites
// Only AEAD ciphers (Authenticated Encryption with Associated Data) are included
func SecureCipherSuites() []uint16 {
	return []uint16{
		// TLS 1.3 cipher suites (implicit, always available in TLS 1.3)
		tls.TLS_AES_128_GCM_SHA256,
		tls.TLS_AES_256_GCM_SHA384,
		tls.TLS_CHACHA20_POLY1305_SHA256,

		// TLS 1.2 ECDHE cipher suites with AEAD
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
		tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
	}
}

// CreateServerTLSConfig creates a hardened server TLS configuration
// Default settings:
//   - TLS 1.3 minimum
//   - Secure cipher suites only
//   - No client certificate verification (use WithClientAuth to enable mTLS)
func CreateServerTLSConfig(certPEM, keyPEM []byte) (*tls.Config, error) {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, pkgerrors.NewTLSError("failed to parse certificate").
			WithField("error", err.Error())
	}

	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
		CipherSuites: SecureCipherSuites(),
		ClientAuth:   tls.NoClientCert,
		// Prefer server cipher suites
		PreferServerCipherSuites: true,
	}

	return config, nil
}

// CreateServerTLSConfigWithMTLS creates a server TLS configuration with mutual TLS
func CreateServerTLSConfigWithMTLS(certPEM, keyPEM, caPEM []byte) (*tls.Config, error) {
	config, err := CreateServerTLSConfig(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}

	// Load CA certificate for client verification
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caPEM) {
		return nil, pkgerrors.NewTLSError("failed to parse CA certificate")
	}

	config.ClientAuth = tls.RequireAndVerifyClientCert
	config.ClientCAs = certPool

	return config, nil
}

// CreateClientTLSConfig creates a hardened client TLS configuration
// Default settings:
//   - TLS 1.3 minimum
//   - Secure cipher suites only
//   - System root CAs for server verification
func CreateClientTLSConfig(serverName string) (*tls.Config, error) {
	config := &tls.Config{
		ServerName:   serverName,
		MinVersion:   tls.VersionTLS13,
		CipherSuites: SecureCipherSuites(),
	}

	return config, nil
}

// CreateClientTLSConfigWithMTLS creates a client TLS configuration with mutual TLS
func CreateClientTLSConfigWithMTLS(certPEM, keyPEM, caPEM []byte, serverName string) (*tls.Config, error) {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, pkgerrors.NewTLSError("failed to parse client certificate").
			WithField("error", err.Error())
	}

	// Load CA certificate for server verification
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caPEM) {
		return nil, pkgerrors.NewTLSError("failed to parse CA certificate")
	}

	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      certPool,
		ServerName:   serverName,
		MinVersion:   tls.VersionTLS13,
		CipherSuites: SecureCipherSuites(),
	}

	return config, nil
}

// CreateBackendTLSConfig creates a TLS config for backend connections
// This is used by the upstream pool to connect to backend services
func CreateBackendTLSConfig(caCertPEM []byte, serverName string, insecureSkipVerify bool) (*tls.Config, error) {
	config := &tls.Config{
		ServerName:   serverName,
		MinVersion:   tls.VersionTLS13,
		CipherSuites: SecureCipherSuites(),
	}

	// Security: InsecureSkipVerify is intentionally configurable for backend connections
	// where backends may use self-signed certificates in development/internal environments.
	// Callers are responsible for ensuring this is only enabled when appropriate.
	if insecureSkipVerify {
		config.InsecureSkipVerify = true
	}

	// Load CA certificate if provided
	if len(caCertPEM) > 0 {
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caCertPEM) {
			return nil, pkgerrors.NewTLSError("failed to parse CA certificate for backend")
		}
		config.RootCAs = certPool
	}

	return config, nil
}

// SNIConfig represents a Server Name Indication configuration for multi-certificate support
type SNIConfig struct {
	// Default certificate (required)
	DefaultCert tls.Certificate

	// Certificate map: hostname -> certificate
	// Supports wildcard patterns (*.example.com)
	Certificates map[string]tls.Certificate

	// TLS configuration settings
	MinVersion   uint16
	CipherSuites []uint16
}

// CreateServerTLSConfigWithSNI creates a server TLS configuration with SNI support
func CreateServerTLSConfigWithSNI(sniConfig *SNIConfig) (*tls.Config, error) {
	if sniConfig == nil {
		return nil, pkgerrors.NewConfigError("SNI configuration cannot be nil")
	}

	config := &tls.Config{
		Certificates:             []tls.Certificate{sniConfig.DefaultCert},
		MinVersion:               tls.VersionTLS13,
		CipherSuites:             sniConfig.CipherSuites,
		PreferServerCipherSuites: true,
	}

	// Allow TLS 1.2 if explicitly requested; enforce TLS 1.2 as the floor
	if sniConfig.MinVersion == tls.VersionTLS12 {
		config.MinVersion = tls.VersionTLS12
	}
	if len(config.CipherSuites) == 0 {
		config.CipherSuites = SecureCipherSuites()
	}

	// Enable SNI certificate selection if multiple certificates provided
	if len(sniConfig.Certificates) > 0 {
		config.GetCertificate = func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			serverName := clientHello.ServerName

			// Look for exact match
			if cert, ok := sniConfig.Certificates[serverName]; ok {
				return &cert, nil
			}

			// Check for wildcard match (*.example.com)
			if serverName != "" {
				labels := strings.Split(serverName, ".")
				if len(labels) > 1 {
					wildcardName := "*." + strings.Join(labels[1:], ".")
					if cert, ok := sniConfig.Certificates[wildcardName]; ok {
						return &cert, nil
					}
				}
			}

			// Return default certificate if no match
			return &sniConfig.DefaultCert, nil
		}
	}

	return config, nil
}

// ParseTLSVersion converts a string TLS version to tls constant
func ParseTLSVersion(version string) uint16 {
	switch strings.ToUpper(version) {
	case "TLS1.2", "TLSV1.2", "1.2":
		return tls.VersionTLS12
	case "TLS1.3", "TLSV1.3", "1.3":
		return tls.VersionTLS13
	default:
		// Default to TLS 1.3 for security
		return tls.VersionTLS13
	}
}

// ParseCipherSuites converts cipher suite names to constants
func ParseCipherSuites(suites []string) []uint16 {
	if len(suites) == 0 {
		// Return secure defaults
		return SecureCipherSuites()
	}

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
		if id, ok := cipherMap[strings.ToUpper(name)]; ok {
			result = append(result, id)
		}
	}

	// If no valid suites parsed, return secure defaults
	if len(result) == 0 {
		return SecureCipherSuites()
	}

	return result
}
