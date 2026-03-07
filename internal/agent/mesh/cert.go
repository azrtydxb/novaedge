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
	"fmt"
	"net/url"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"

	pb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

const (
	// renewalThreshold is the fraction of cert lifetime at which renewal starts.
	renewalThreshold = 0.8

	// minRenewalInterval prevents tight renewal loops on very short-lived certs.
	minRenewalInterval = 30 * time.Second

	// certRequestTimeout is the timeout for a single cert request RPC.
	certRequestTimeout = 30 * time.Second

	// certRetryDelay is the delay between retries on cert request failure.
	certRetryDelay = 5 * time.Second
)

// CertRequester handles requesting and renewing mesh workload certificates
// from the controller's RequestMeshCertificate RPC.
type CertRequester struct {
	logger      *zap.Logger
	nodeName    string
	trustDomain string
	fedCfg      *FederationConfig
	onUpdate    func(certPEM, keyPEM, caCertPEM []byte, spiffeID string) error
}

// NewCertRequester creates a new certificate requester.
// The onUpdate callback is called whenever a new certificate is obtained.
// The fedCfg parameter may be nil when federation is not active.
func NewCertRequester(logger *zap.Logger, nodeName, trustDomain string,
	fedCfg *FederationConfig,
	onUpdate func(certPEM, keyPEM, caCertPEM []byte, spiffeID string) error,
) *CertRequester {
	return &CertRequester{
		logger:      logger.Named("cert-requester"),
		nodeName:    nodeName,
		trustDomain: trustDomain,
		fedCfg:      fedCfg,
		onUpdate:    onUpdate,
	}
}

// Run starts the certificate request and renewal loop. It blocks until the
// context is cancelled. On first call it immediately requests a certificate,
// then renews at 80% of the certificate's lifetime.
func (cr *CertRequester) Run(ctx context.Context, conn *grpc.ClientConn) {
	client := pb.NewConfigServiceClient(conn)

	for {
		expiry, err := cr.requestAndApply(ctx, client)
		if err != nil {
			cr.logger.Error("Failed to request mesh certificate, retrying",
				zap.Error(err),
				zap.Duration("retry_delay", certRetryDelay))

			select {
			case <-ctx.Done():
				return
			case <-time.After(certRetryDelay):
				continue
			}
		}

		// Schedule renewal at 80% of lifetime
		lifetime := time.Until(expiry)
		renewIn := time.Duration(float64(lifetime) * renewalThreshold)
		if renewIn < minRenewalInterval {
			renewIn = minRenewalInterval
		}

		cr.logger.Info("Mesh certificate obtained, scheduling renewal",
			zap.Time("expiry", expiry),
			zap.Duration("lifetime", lifetime),
			zap.Duration("renew_in", renewIn))

		select {
		case <-ctx.Done():
			return
		case <-time.After(renewIn):
			cr.logger.Info("Mesh certificate renewal triggered")
		}
	}
}

// requestAndApply generates a CSR, calls the controller RPC, and applies
// the returned certificate material. Returns the certificate expiry time.
func (cr *CertRequester) requestAndApply(ctx context.Context, client pb.ConfigServiceClient) (time.Time, error) {
	// Generate ECDSA P-256 private key
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to generate key: %w", err)
	}

	// Build CSR with SPIFFE URI SAN. When federation is active the SPIFFE ID
	// includes the federation trust domain and cluster name so that peer
	// clusters can identify the origin.
	spiffeIDStr := BuildSPIFFEID(cr.trustDomain, cr.nodeName, cr.fedCfg)
	spiffeURI, parseErr := url.Parse(spiffeIDStr)
	if parseErr != nil {
		return time.Time{}, fmt.Errorf("failed to parse SPIFFE URI: %w", parseErr)
	}

	csrTemplate := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   cr.nodeName,
			Organization: []string{"novaedge"},
		},
		URIs: []*url.URL{spiffeURI},
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, key)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to create CSR: %w", err)
	}

	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	// Call controller RPC
	reqCtx, cancel := context.WithTimeout(ctx, certRequestTimeout)
	defer cancel()

	resp, err := client.RequestMeshCertificate(reqCtx, &pb.MeshCertificateRequest{
		Csr:      csrPEM,
		NodeName: cr.nodeName,
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("RequestMeshCertificate RPC failed: %w", err)
	}

	// Encode private key to PEM
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to marshal private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	// Apply the certificate
	if err := cr.onUpdate(resp.Certificate, keyPEM, resp.CaCertificate, resp.SpiffeId); err != nil {
		return time.Time{}, fmt.Errorf("failed to apply certificate: %w", err)
	}

	expiry := time.Unix(resp.ExpiryUnix, 0)

	cr.logger.Info("Mesh certificate applied",
		zap.String("spiffe_id", resp.SpiffeId),
		zap.Time("expiry", expiry))

	return expiry, nil
}
