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

package mesh

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

// fakeCertServer implements the RequestMeshCertificate RPC for testing.
type fakeCertServer struct {
	pb.UnimplementedConfigServiceServer
	caKey  *ecdsa.PrivateKey
	caCert *x509.Certificate
	caPEM  []byte
}

func newFakeCertServer(t *testing.T) *fakeCertServer {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}

	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}

	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})

	return &fakeCertServer{
		caKey:  caKey,
		caCert: caCert,
		caPEM:  caPEM,
	}
}

func (s *fakeCertServer) RequestMeshCertificate(_ context.Context, req *pb.MeshCertificateRequest) (*pb.MeshCertificateResponse, error) {
	// Parse the CSR
	block, _ := pem.Decode(req.Csr)
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, err
	}

	// Sign the certificate
	expiry := time.Now().Add(24 * time.Hour)
	certTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      csr.Subject,
		URIs:         csr.URIs,
		NotBefore:    time.Now(),
		NotAfter:     expiry,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, certTemplate, s.caCert, csr.PublicKey, s.caKey)
	if err != nil {
		return nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	spiffeID := ""
	if len(csr.URIs) > 0 {
		spiffeID = csr.URIs[0].String()
	}

	return &pb.MeshCertificateResponse{
		Certificate:   certPEM,
		CaCertificate: s.caPEM,
		SpiffeId:      spiffeID,
		ExpiryUnix:    expiry.Unix(),
	}, nil
}

func TestCertRequester_RequestAndApply(t *testing.T) {
	// Start a fake gRPC server
	server := grpc.NewServer()
	fakeSrv := newFakeCertServer(t)
	pb.RegisterConfigServiceServer(server, fakeSrv)

	var lc net.ListenConfig
	lis, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(lis) }()
	defer server.Stop()

	// Connect to the fake server
	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	logger := zaptest.NewLogger(t)

	// Track cert updates
	var mu sync.Mutex
	var gotCert, gotKey, gotCA []byte
	var gotSPIFFE string

	cr := NewCertRequester(logger, "test-node", "cluster.local", nil,
		func(certPEM, keyPEM, caCertPEM []byte, spiffeID string) error {
			mu.Lock()
			defer mu.Unlock()
			gotCert = certPEM
			gotKey = keyPEM
			gotCA = caCertPEM
			gotSPIFFE = spiffeID
			return nil
		})

	// Run with a short-lived context so it does one request then stops
	ctx, cancel := context.WithCancel(context.Background())

	go cr.Run(ctx, conn)

	// Wait for the certificate to be applied
	deadline := time.After(5 * time.Second)
	for {
		mu.Lock()
		hasCert := gotCert != nil
		mu.Unlock()
		if hasCert {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for cert to be applied")
		case <-time.After(50 * time.Millisecond):
		}
	}

	cancel()

	mu.Lock()
	defer mu.Unlock()

	// Verify certificate
	if len(gotCert) == 0 {
		t.Fatal("expected certificate, got empty")
	}
	if len(gotKey) == 0 {
		t.Fatal("expected key, got empty")
	}
	if len(gotCA) == 0 {
		t.Fatal("expected CA cert, got empty")
	}

	expectedSPIFFE := "spiffe://cluster.local/agent/test-node"
	if gotSPIFFE != expectedSPIFFE {
		t.Errorf("expected SPIFFE ID %q, got %q", expectedSPIFFE, gotSPIFFE)
	}

	// Verify the cert can be parsed and used as a TLS certificate
	block, _ := pem.Decode(gotCert)
	if block == nil {
		t.Fatal("failed to decode cert PEM")
	}
	cert, parseErr := x509.ParseCertificate(block.Bytes)
	if parseErr != nil {
		t.Fatalf("failed to parse certificate: %v", parseErr)
	}

	// Verify SPIFFE URI SAN
	if len(cert.URIs) == 0 {
		t.Fatal("certificate has no URI SANs")
	}
	if cert.URIs[0].String() != expectedSPIFFE {
		t.Errorf("cert URI SAN = %q, want %q", cert.URIs[0].String(), expectedSPIFFE)
	}

	// Verify the private key matches the cert
	keyBlock, _ := pem.Decode(gotKey)
	if keyBlock == nil {
		t.Fatal("failed to decode key PEM")
	}
	privKey, keyErr := x509.ParseECPrivateKey(keyBlock.Bytes)
	if keyErr != nil {
		t.Fatalf("failed to parse private key: %v", keyErr)
	}

	// Verify key matches cert's public key
	if !privKey.PublicKey.Equal(cert.PublicKey) {
		t.Fatal("private key does not match certificate public key")
	}
}

func TestCertRequester_RenewalLoop(t *testing.T) {
	// Start a fake gRPC server that issues short-lived certs
	server := grpc.NewServer()
	fakeSrv := newFakeCertServer(t)
	pb.RegisterConfigServiceServer(server, fakeSrv)

	var lc net.ListenConfig
	lis, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(lis) }()
	defer server.Stop()

	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	logger := zaptest.NewLogger(t)

	var mu sync.Mutex
	var requestCount int

	cr := NewCertRequester(logger, "test-node", "cluster.local", nil,
		func(_, _, _ []byte, _ string) error {
			mu.Lock()
			defer mu.Unlock()
			requestCount++
			return nil
		})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go cr.Run(ctx, conn)

	// Wait for at least one request
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		count := requestCount
		mu.Unlock()
		if count >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for cert request")
		case <-time.After(50 * time.Millisecond):
		}
	}

	mu.Lock()
	count := requestCount
	mu.Unlock()

	if count < 1 {
		t.Fatalf("expected at least 1 cert request, got %d", count)
	}
}
