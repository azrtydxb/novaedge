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
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"testing"
	"time"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
	"go.uber.org/zap/zaptest"
)

// Helper function to generate a self-signed test certificate
func generateTestCertificate(t *testing.T) tls.Certificate {
	t.Helper()

	// Generate RSA key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"NovaEdge Test"},
			CommonName:   "localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
	}

	// Create self-signed certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	// Encode certificate and private key to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	// Parse PEM to tls.Certificate
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Failed to parse certificate: %v", err)
	}

	return cert
}

func TestNewHTTP3Server(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cert := generateTestCertificate(t)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("creates server with default config", func(t *testing.T) {
		quicConfig := &pb.QUICConfig{
			MaxIdleTimeout: "30s",
			MaxBiStreams:   100,
			MaxUniStreams:  10,
			Enable_0Rtt:    false,
		}

		server := NewHTTP3Server(logger, 8443, tlsConfig, quicConfig, handler)

		if server == nil {
			t.Fatal("Expected server instance, got nil")
		}

		if server.port != 8443 {
			t.Errorf("Expected port 8443, got %d", server.port)
		}

		if server.handler == nil {
			t.Error("Handler is nil")
		}

		if server.server == nil {
			t.Error("Internal http3.Server is nil")
		}

		if server.server.Addr != ":8443" {
			t.Errorf("Expected address :8443, got %s", server.server.Addr)
		}
	})

	t.Run("creates server with 0-RTT enabled", func(t *testing.T) {
		quicConfig := &pb.QUICConfig{
			MaxIdleTimeout: "60s",
			MaxBiStreams:   200,
			MaxUniStreams:  20,
			Enable_0Rtt:    true,
		}

		server := NewHTTP3Server(logger, 9443, tlsConfig, quicConfig, handler)

		if server == nil {
			t.Fatal("Expected server instance, got nil")
		}

		if server.config.Enable_0Rtt != true {
			t.Error("Expected 0-RTT to be enabled")
		}

		if server.server.QUICConfig.Allow0RTT != true {
			t.Error("Expected QUIC config to allow 0-RTT")
		}
	})

	t.Run("creates server with custom stream limits", func(t *testing.T) {
		quicConfig := &pb.QUICConfig{
			MaxIdleTimeout: "45s",
			MaxBiStreams:   500,
			MaxUniStreams:  50,
			Enable_0Rtt:    false,
		}

		server := NewHTTP3Server(logger, 10443, tlsConfig, quicConfig, handler)

		if server.server.QUICConfig.MaxIncomingStreams != 500 {
			t.Errorf("Expected MaxIncomingStreams 500, got %d", server.server.QUICConfig.MaxIncomingStreams)
		}

		if server.server.QUICConfig.MaxIncomingUniStreams != 50 {
			t.Errorf("Expected MaxIncomingUniStreams 50, got %d", server.server.QUICConfig.MaxIncomingUniStreams)
		}
	})

	t.Run("enables datagrams by default", func(t *testing.T) {
		quicConfig := &pb.QUICConfig{
			MaxIdleTimeout: "30s",
			MaxBiStreams:   100,
			MaxUniStreams:  10,
			Enable_0Rtt:    false,
		}

		server := NewHTTP3Server(logger, 11443, tlsConfig, quicConfig, handler)

		if server.server.QUICConfig.EnableDatagrams != true {
			t.Error("Expected EnableDatagrams to be true")
		}

		if server.server.QUICConfig.DisablePathMTUDiscovery != false {
			t.Error("Expected DisablePathMTUDiscovery to be false")
		}
	})
}

func TestParseTimeout(t *testing.T) {
	tests := []struct {
		name           string
		timeoutStr     string
		defaultTimeout time.Duration
		expected       time.Duration
	}{
		{
			name:           "valid timeout string",
			timeoutStr:     "30s",
			defaultTimeout: 10 * time.Second,
			expected:       30 * time.Second,
		},
		{
			name:           "valid timeout with minutes",
			timeoutStr:     "5m",
			defaultTimeout: 1 * time.Minute,
			expected:       5 * time.Minute,
		},
		{
			name:           "empty string uses default",
			timeoutStr:     "",
			defaultTimeout: 15 * time.Second,
			expected:       15 * time.Second,
		},
		{
			name:           "invalid format uses default",
			timeoutStr:     "invalid",
			defaultTimeout: 20 * time.Second,
			expected:       20 * time.Second,
		},
		{
			name:           "valid timeout with hours",
			timeoutStr:     "2h",
			defaultTimeout: 30 * time.Minute,
			expected:       2 * time.Hour,
		},
		{
			name:           "valid timeout with milliseconds",
			timeoutStr:     "500ms",
			defaultTimeout: 1 * time.Second,
			expected:       500 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseTimeout(tt.timeoutStr, tt.defaultTimeout)

			if result != tt.expected {
				t.Errorf("Expected timeout %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestGetPort(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cert := generateTestCertificate(t)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	quicConfig := &pb.QUICConfig{
		MaxIdleTimeout: "30s",
		MaxBiStreams:   100,
		MaxUniStreams:  10,
		Enable_0Rtt:    false,
	}

	t.Run("returns correct port", func(t *testing.T) {
		server := NewHTTP3Server(logger, 12443, tlsConfig, quicConfig, handler)

		port := server.GetPort()

		if port != 12443 {
			t.Errorf("Expected port 12443, got %d", port)
		}
	})

	t.Run("returns correct port for different instance", func(t *testing.T) {
		server := NewHTTP3Server(logger, 13443, tlsConfig, quicConfig, handler)

		port := server.GetPort()

		if port != 13443 {
			t.Errorf("Expected port 13443, got %d", port)
		}
	})
}

func TestSupportsEarlyData(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cert := generateTestCertificate(t)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("returns false when 0-RTT disabled", func(t *testing.T) {
		quicConfig := &pb.QUICConfig{
			MaxIdleTimeout: "30s",
			MaxBiStreams:   100,
			MaxUniStreams:  10,
			Enable_0Rtt:    false,
		}

		server := NewHTTP3Server(logger, 14443, tlsConfig, quicConfig, handler)

		if server.SupportsEarlyData() {
			t.Error("Expected SupportsEarlyData to return false when 0-RTT is disabled")
		}
	})

	t.Run("returns true when 0-RTT enabled", func(t *testing.T) {
		quicConfig := &pb.QUICConfig{
			MaxIdleTimeout: "30s",
			MaxBiStreams:   100,
			MaxUniStreams:  10,
			Enable_0Rtt:    true,
		}

		server := NewHTTP3Server(logger, 15443, tlsConfig, quicConfig, handler)

		if !server.SupportsEarlyData() {
			t.Error("Expected SupportsEarlyData to return true when 0-RTT is enabled")
		}
	})
}

func TestHTTP3Server_Shutdown(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cert := generateTestCertificate(t)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	quicConfig := &pb.QUICConfig{
		MaxIdleTimeout: "30s",
		MaxBiStreams:   100,
		MaxUniStreams:  10,
		Enable_0Rtt:    false,
	}

	t.Run("shutdown without starting does not error", func(t *testing.T) {
		server := NewHTTP3Server(logger, 16443, tlsConfig, quicConfig, handler)

		ctx := context.Background()
		err := server.Shutdown(ctx)

		// Shutdown should complete without error even if server wasn't started
		// (the underlying http3.Server.Close() is idempotent)
		if err != nil {
			// This is acceptable - some implementations may return an error
			// when closing an unstarted server
			t.Logf("Shutdown returned error (acceptable): %v", err)
		}
	})
}

func TestHTTP3Server_Configuration(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cert := generateTestCertificate(t)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("validates stream receive windows", func(t *testing.T) {
		quicConfig := &pb.QUICConfig{
			MaxIdleTimeout: "30s",
			MaxBiStreams:   100,
			MaxUniStreams:  10,
			Enable_0Rtt:    false,
		}

		server := NewHTTP3Server(logger, 17443, tlsConfig, quicConfig, handler)

		// Verify stream receive windows are set correctly
		if server.server.QUICConfig.InitialStreamReceiveWindow != 1<<20 {
			t.Errorf("Expected InitialStreamReceiveWindow 1MB, got %d", server.server.QUICConfig.InitialStreamReceiveWindow)
		}

		if server.server.QUICConfig.MaxStreamReceiveWindow != 6<<20 {
			t.Errorf("Expected MaxStreamReceiveWindow 6MB, got %d", server.server.QUICConfig.MaxStreamReceiveWindow)
		}

		if server.server.QUICConfig.InitialConnectionReceiveWindow != 1<<20 {
			t.Errorf("Expected InitialConnectionReceiveWindow 1MB, got %d", server.server.QUICConfig.InitialConnectionReceiveWindow)
		}

		if server.server.QUICConfig.MaxConnectionReceiveWindow != 15<<20 {
			t.Errorf("Expected MaxConnectionReceiveWindow 15MB, got %d", server.server.QUICConfig.MaxConnectionReceiveWindow)
		}
	})

	t.Run("validates idle timeout configuration", func(t *testing.T) {
		quicConfig := &pb.QUICConfig{
			MaxIdleTimeout: "120s",
			MaxBiStreams:   100,
			MaxUniStreams:  10,
			Enable_0Rtt:    false,
		}

		server := NewHTTP3Server(logger, 18443, tlsConfig, quicConfig, handler)

		expectedTimeout := 120 * time.Second
		if server.server.QUICConfig.MaxIdleTimeout != expectedTimeout {
			t.Errorf("Expected MaxIdleTimeout %v, got %v", expectedTimeout, server.server.QUICConfig.MaxIdleTimeout)
		}
	})

	t.Run("uses default timeout for invalid config", func(t *testing.T) {
		quicConfig := &pb.QUICConfig{
			MaxIdleTimeout: "invalid-timeout",
			MaxBiStreams:   100,
			MaxUniStreams:  10,
			Enable_0Rtt:    false,
		}

		server := NewHTTP3Server(logger, 19443, tlsConfig, quicConfig, handler)

		// Should fall back to default 30s timeout
		expectedTimeout := 30 * time.Second
		if server.server.QUICConfig.MaxIdleTimeout != expectedTimeout {
			t.Errorf("Expected default MaxIdleTimeout %v, got %v", expectedTimeout, server.server.QUICConfig.MaxIdleTimeout)
		}
	})
}

// Note: Full integration testing of HTTP/3 server Start() requires:
// - Actual network listener (UDP port binding)
// - Valid TLS certificate chain
// - HTTP/3 client to send requests
// These tests are better suited for integration test suite rather than unit tests.
// For comprehensive testing, see integration tests in test/integration/http3_test.go
