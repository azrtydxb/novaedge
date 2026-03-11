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

package meshca

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	return scheme
}

const pemTypeCertificate = "CERTIFICATE"

func generateTestCSRWithName(t *testing.T, nodeName string) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate test key: %v", err)
	}

	template := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: nodeName,
		},
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	if err != nil {
		t.Fatalf("failed to create CSR: %v", err)
	}

	return pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE REQUEST",
		Bytes: csrDER,
	})
}

func TestMeshCAInitializeCreatesNewCA(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cl := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		Build()

	ca := NewMeshCA(logger, "cluster.local", "test-ns")

	ctx := context.Background()
	if err := ca.Initialize(ctx, cl); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Verify CA certificate was generated.
	caPEM := ca.CACertPEM()
	if len(caPEM) == 0 {
		t.Fatal("CACertPEM returned empty bytes")
	}

	block, _ := pem.Decode(caPEM)
	if block == nil || block.Type != pemTypeCertificate {
		t.Fatal("CACertPEM did not return a valid PEM certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse CA certificate: %v", err)
	}

	if cert.Subject.CommonName != "NovaEdge Mesh CA" {
		t.Errorf("unexpected CA CN: got %q, want %q", cert.Subject.CommonName, "NovaEdge Mesh CA")
	}

	if !cert.IsCA {
		t.Error("CA certificate is not marked as CA")
	}

	if cert.MaxPathLen != 1 {
		t.Errorf("unexpected MaxPathLen: got %d, want 1", cert.MaxPathLen)
	}

	if cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Error("CA certificate missing CertSign key usage")
	}

	if cert.KeyUsage&x509.KeyUsageCRLSign == 0 {
		t.Error("CA certificate missing CRLSign key usage")
	}

	// Verify validity period is approximately 10 years.
	expectedDuration := rootCAValidityDuration
	actualDuration := cert.NotAfter.Sub(cert.NotBefore)
	if actualDuration < expectedDuration-time.Hour || actualDuration > expectedDuration+time.Hour {
		t.Errorf("unexpected CA validity duration: got %v, want ~%v", actualDuration, expectedDuration)
	}

	// Verify trust domain.
	if ca.TrustDomain() != "cluster.local" {
		t.Errorf("unexpected trust domain: got %q, want %q", ca.TrustDomain(), "cluster.local")
	}

	// Verify the secret was persisted by loading a second CA from the same client.
	ca2 := NewMeshCA(logger, "cluster.local", "test-ns")
	if err := ca2.Initialize(ctx, cl); err != nil {
		t.Fatalf("second Initialize failed: %v", err)
	}

	if string(ca2.CACertPEM()) != string(caPEM) {
		t.Error("second CA did not load the same certificate from the secret")
	}
}

func TestMeshCASignCSR(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cl := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		Build()

	ca := NewMeshCA(logger, "cluster.local", "test-ns")
	ctx := context.Background()

	if err := ca.Initialize(ctx, cl); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	csrPEM := generateTestCSRWithName(t, "worker-1")

	certPEM, err := ca.SignCSR(csrPEM, "worker-1")
	if err != nil {
		t.Fatalf("SignCSR failed: %v", err)
	}

	if len(certPEM) == 0 {
		t.Fatal("SignCSR returned empty certificate")
	}

	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != pemTypeCertificate {
		t.Fatal("SignCSR did not return a valid PEM certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse signed certificate: %v", err)
	}

	// Verify it is not a CA.
	if cert.IsCA {
		t.Error("workload certificate should not be a CA")
	}

	// Verify key usage.
	if cert.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		t.Error("workload certificate missing DigitalSignature key usage")
	}

	// Verify extended key usage.
	hasClientAuth := false
	hasServerAuth := false
	for _, eku := range cert.ExtKeyUsage {
		if eku == x509.ExtKeyUsageClientAuth {
			hasClientAuth = true
		}
		if eku == x509.ExtKeyUsageServerAuth {
			hasServerAuth = true
		}
	}
	if !hasClientAuth {
		t.Error("workload certificate missing ClientAuth extended key usage")
	}
	if !hasServerAuth {
		t.Error("workload certificate missing ServerAuth extended key usage")
	}

	// Verify validity is 24 hours.
	expectedDuration := workloadCertValidity
	actualDuration := cert.NotAfter.Sub(cert.NotBefore)
	if actualDuration < expectedDuration-time.Second || actualDuration > expectedDuration+time.Second {
		t.Errorf("unexpected workload cert validity: got %v, want %v", actualDuration, expectedDuration)
	}

	// Verify the certificate chains to the CA.
	roots := x509.NewCertPool()
	roots.AddCert(ca.caCert)
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Errorf("workload certificate failed verification against CA: %v", err)
	}
}

func TestMeshCASPIFFEID(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cl := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		Build()

	ca := NewMeshCA(logger, "example.org", "test-ns")
	ctx := context.Background()

	if err := ca.Initialize(ctx, cl); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	csrPEM := generateTestCSRWithName(t, "node-alpha")

	certPEM, err := ca.SignCSR(csrPEM, "node-alpha")
	if err != nil {
		t.Fatalf("SignCSR failed: %v", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("failed to decode signed certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse signed certificate: %v", err)
	}

	if len(cert.URIs) == 0 {
		t.Fatal("workload certificate has no URI SANs")
	}

	expectedSPIFFE := "spiffe://example.org/agent/node-alpha"
	found := false
	for _, uri := range cert.URIs {
		if uri.String() == expectedSPIFFE {
			found = true
			break
		}
	}

	if !found {
		uris := make([]string, 0, len(cert.URIs))
		for _, uri := range cert.URIs {
			uris = append(uris, uri.String())
		}
		t.Errorf("expected SPIFFE ID %q not found in URI SANs: %v", expectedSPIFFE, uris)
	}
}

func TestMeshCASignCSRIdentityMismatch(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cl := fake.NewClientBuilder().
		WithScheme(newTestScheme()).
		Build()

	ca := NewMeshCA(logger, "cluster.local", "test-ns")
	ctx := context.Background()

	if err := ca.Initialize(ctx, cl); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// CSR has CN "attacker-node" but request is for "victim-node"
	csrPEM := generateTestCSRWithName(t, "attacker-node")

	_, err := ca.SignCSR(csrPEM, "victim-node")
	if err == nil {
		t.Fatal("expected SignCSR to reject mismatched node name, but it succeeded")
	}

	if !errors.Is(err, errCSRCNMismatch) {
		t.Errorf("expected errCSRCNMismatch, got: %v", err)
	}
}
