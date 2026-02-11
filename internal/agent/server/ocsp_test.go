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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	"go.uber.org/zap"
)

func generateTestCACert() (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(1 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		panic(err)
	}

	return cert, key, certDER
}

func generateTestLeafCert(ca *x509.Certificate, caKey *ecdsa.PrivateKey, ocspServer []string) (*tls.Certificate, []byte) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "test.example.com"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(1 * time.Hour),
		OCSPServer:   ocspServer,
		DNSNames:     []string{"test.example.com"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		panic(err)
	}

	return &tls.Certificate{
		Certificate: [][]byte{certDER, ca.Raw},
		PrivateKey:  key,
	}, certDER
}

func TestOCSPStapler_NewAndStop(t *testing.T) {
	logger := zap.NewNop()
	stapler := NewOCSPStapler(logger)

	if stapler == nil {
		t.Fatal("Expected non-nil stapler")
	}

	if len(stapler.certificates) != 0 {
		t.Errorf("Expected empty certificates map, got %d", len(stapler.certificates))
	}
}

func TestOCSPStapler_AddCertificateNoOCSPServer(t *testing.T) {
	logger := zap.NewNop()
	stapler := NewOCSPStapler(logger)

	ca, caKey, _ := generateTestCACert()
	cert, _ := generateTestLeafCert(ca, caKey, nil) // No OCSP server

	// Should not error but also not add the cert for stapling
	err := stapler.AddCertificate("test.example.com", cert, nil)
	if err != nil {
		t.Errorf("Expected no error for cert without OCSP server, got: %v", err)
	}
}

func TestOCSPStapler_AddCertificateNilCert(t *testing.T) {
	logger := zap.NewNop()
	stapler := NewOCSPStapler(logger)

	err := stapler.AddCertificate("test.example.com", nil, nil)
	if err == nil {
		t.Error("Expected error for nil certificate")
	}
}

func TestOCSPStapler_Status(t *testing.T) {
	logger := zap.NewNop()
	stapler := NewOCSPStapler(logger)

	status := stapler.Status()
	if len(status.Certificates) != 0 {
		t.Errorf("Expected empty certificates status, got %d", len(status.Certificates))
	}
}

func TestOCSPStapler_StapleToConfig(t *testing.T) {
	logger := zap.NewNop()
	stapler := NewOCSPStapler(logger)

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	// Should not panic
	stapler.StapleToConfig(tlsConfig)

	// GetCertificate should now be set
	if tlsConfig.GetCertificate == nil {
		t.Error("Expected GetCertificate to be set after StapleToConfig")
	}
}

func TestOCSPStapler_GracefulFailure(t *testing.T) {
	// This tests that OCSP stapling fails gracefully when the OCSP responder
	// is unreachable, and the certificate is still served without a staple
	logger := zap.NewNop()
	stapler := NewOCSPStapler(logger)

	ca, caKey, _ := generateTestCACert()
	// Use an unreachable OCSP server
	cert, _ := generateTestLeafCert(ca, caKey, []string{"http://192.0.2.1:9999/ocsp"})

	// Should not error even though OCSP fetch fails
	err := stapler.AddCertificate("test.example.com", cert, nil)
	if err != nil {
		t.Errorf("Expected graceful handling of OCSP fetch failure, got: %v", err)
	}

	// Status should show no staple
	status := stapler.Status()
	if certStatus, ok := status.Certificates["test.example.com"]; ok {
		if certStatus.HasStaple {
			t.Error("Expected no staple for unreachable OCSP responder")
		}
	}
}
