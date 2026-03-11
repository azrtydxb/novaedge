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

package snapshot

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/url"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// newTestCert creates a self-signed certificate with the given URIs and CN.
func newTestCert(t *testing.T, cn string, uris []*url.URL) *x509.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: cn,
		},
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter:  time.Now().Add(time.Hour),
		URIs:      uris,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}

	return cert
}

// ctxWithPeerCert creates a context with the given certificate as peer TLS info.
func ctxWithPeerCert(t *testing.T, cert *x509.Certificate) context.Context {
	t.Helper()

	p := &peer.Peer{
		Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345},
		AuthInfo: credentials.TLSInfo{
			State: tls.ConnectionState{
				PeerCertificates: []*x509.Certificate{cert},
			},
		},
	}

	return peer.NewContext(context.Background(), p)
}

func TestValidatePeerNodeName_MatchingSPIFFE(t *testing.T) {
	spiffeURI, _ := url.Parse("spiffe://cluster.local/agent/worker-1")
	cert := newTestCert(t, "", []*url.URL{spiffeURI})
	ctx := ctxWithPeerCert(t, cert)

	err := validatePeerNodeName(ctx, "worker-1")
	if err != nil {
		t.Errorf("expected no error for matching SPIFFE, got: %v", err)
	}
}

func TestValidatePeerNodeName_MismatchedSPIFFE(t *testing.T) {
	spiffeURI, _ := url.Parse("spiffe://cluster.local/agent/worker-1")
	cert := newTestCert(t, "", []*url.URL{spiffeURI})
	ctx := ctxWithPeerCert(t, cert)

	err := validatePeerNodeName(ctx, "worker-2")
	if err == nil {
		t.Fatal("expected PermissionDenied for mismatched SPIFFE, got nil")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.PermissionDenied {
		t.Errorf("expected PermissionDenied, got: %v", err)
	}
}

func TestValidatePeerNodeName_MultipleSPIFFEURIs_SecondMatches(t *testing.T) {
	uri1, _ := url.Parse("spiffe://cluster.local/agent/worker-1")
	uri2, _ := url.Parse("spiffe://cluster.local/agent/worker-2")
	cert := newTestCert(t, "", []*url.URL{uri1, uri2})
	ctx := ctxWithPeerCert(t, cert)

	// worker-2 is the second URI SAN — should still match.
	err := validatePeerNodeName(ctx, "worker-2")
	if err != nil {
		t.Errorf("expected no error when second SPIFFE URI matches, got: %v", err)
	}
}

func TestValidatePeerNodeName_CNFallback(t *testing.T) {
	cert := newTestCert(t, "worker-1", nil)
	ctx := ctxWithPeerCert(t, cert)

	err := validatePeerNodeName(ctx, "worker-1")
	if err != nil {
		t.Errorf("expected no error for matching CN fallback, got: %v", err)
	}
}

func TestValidatePeerNodeName_CNMismatch(t *testing.T) {
	cert := newTestCert(t, "worker-1", nil)
	ctx := ctxWithPeerCert(t, cert)

	err := validatePeerNodeName(ctx, "worker-2")
	if err == nil {
		t.Fatal("expected PermissionDenied for mismatched CN, got nil")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.PermissionDenied {
		t.Errorf("expected PermissionDenied, got: %v", err)
	}
}

func TestValidatePeerNodeName_CertPresentNoIdentity(t *testing.T) {
	// Certificate with empty CN and no URI SANs — should fail closed.
	cert := newTestCert(t, "", nil)
	ctx := ctxWithPeerCert(t, cert)

	err := validatePeerNodeName(ctx, "worker-1")
	if err == nil {
		t.Fatal("expected PermissionDenied when cert has no recognizable identity, got nil")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.PermissionDenied {
		t.Errorf("expected PermissionDenied, got: %v", err)
	}
}

func TestValidatePeerNodeName_NoPeerInfo(t *testing.T) {
	// No peer info — non-TLS, should allow.
	err := validatePeerNodeName(context.Background(), "worker-1")
	if err != nil {
		t.Errorf("expected no error for non-TLS connection, got: %v", err)
	}
}
