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

package tlsutil

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc/credentials"
)

// LoadServerTLSCredentials loads TLS credentials for gRPC server with mTLS
func LoadServerTLSCredentials(certFile, keyFile, caFile string) (credentials.TransportCredentials, error) {
	// Load server certificate and key
	serverCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}

	// Load CA certificate for client verification
	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to add CA certificate to pool")
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

// LoadClientTLSCredentials loads TLS credentials for gRPC client with mTLS
func LoadClientTLSCredentials(certFile, keyFile, caFile, serverName string) (credentials.TransportCredentials, error) {
	// Load client certificate and key
	clientCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificate: %w", err)
	}

	// Load CA certificate for server verification
	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to add CA certificate to pool")
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
		return nil, fmt.Errorf("failed to add CA certificate to pool")
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
		return nil, fmt.Errorf("failed to add CA certificate to pool")
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
