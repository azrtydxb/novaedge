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

// Package meshca provides an embedded certificate authority for issuing
// short-lived mTLS workload certificates to NovaEdge mesh agents.
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
	"fmt"
	"math/big"
	"net/url"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	errMeshCANotInitialized           = errors.New("mesh CA not initialized")
	errFailedToDecodeCSRPEM           = errors.New("failed to decode CSR PEM")
	errSecret                         = errors.New("secret")
	errFailedToDecodeCACertificatePEM = errors.New("failed to decode CA certificate PEM")
	errFailedToDecodeCAPrivateKeyPEM  = errors.New("failed to decode CA private key PEM")
	errCSRURISANMismatch              = errors.New("CSR URI SANs do not contain expected SPIFFE ID")
	errCSRCNMismatch                  = errors.New("CSR subject CN does not match requested node")
)

const (
	// caSecretName is the Kubernetes Secret storing the root CA.
	caSecretName = "novaedge-mesh-ca" //nolint:gosec // not a credential, just a secret name

	// caSecretKeyCA is the key for the PEM-encoded CA certificate in the Secret.
	caSecretKeyCA = "ca.crt"

	// caSecretKeyKey is the key for the PEM-encoded CA private key in the Secret.
	caSecretKeyKey = "ca.key"

	// rootCAValidityDuration is the validity period for the root CA certificate.
	rootCAValidityDuration = 10 * 365 * 24 * time.Hour // ~10 years

	// workloadCertValidity is the validity period for issued workload certificates.
	workloadCertValidity = 24 * time.Hour

	// serialBitLen is the number of bits for certificate serial numbers.
	serialBitLen = 128
)

// MeshCA is a lightweight embedded certificate authority for issuing
// short-lived mTLS workload certificates to NovaEdge agents.
type MeshCA struct {
	mu          sync.RWMutex
	logger      *zap.Logger
	trustDomain string
	namespace   string

	caCert    *x509.Certificate
	caKey     *ecdsa.PrivateKey
	caCertPEM []byte
}

// NewMeshCA creates a new MeshCA instance. Call Initialize before using
// any other methods. The namespace parameter determines where the CA secret
// is stored; if empty, it is auto-detected from the pod's service account.
func NewMeshCA(logger *zap.Logger, trustDomain, namespace string) *MeshCA {
	if namespace == "" {
		// Auto-detect from the mounted service account namespace.
		if data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
			namespace = string(data)
		} else {
			namespace = "novaedge-system" // fallback
		}
	}
	return &MeshCA{
		logger:      logger.Named("mesh-ca"),
		trustDomain: trustDomain,
		namespace:   namespace,
	}
}

// Initialize loads the root CA from the Kubernetes Secret or generates
// a new root CA keypair and persists it.
func (ca *MeshCA) Initialize(ctx context.Context, cl client.Client) error {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	// Attempt to load existing CA from Secret.
	secret := &corev1.Secret{}
	err := cl.Get(ctx, types.NamespacedName{
		Name:      caSecretName,
		Namespace: ca.namespace,
	}, secret)

	if err == nil {
		ca.logger.Info("loading existing mesh CA from secret",
			zap.String("secret", caSecretName),
			zap.String("namespace", ca.namespace))
		return ca.loadFromSecret(secret)
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get mesh CA secret: %w", err)
	}

	// Secret does not exist — generate a new root CA.
	ca.logger.Info("no existing mesh CA found, generating new root CA",
		zap.String("trustDomain", ca.trustDomain))

	if err := ca.generateRootCA(); err != nil {
		return fmt.Errorf("failed to generate root CA: %w", err)
	}

	// Persist the CA to a Kubernetes Secret.
	keyPEM, err := encodeECPrivateKey(ca.caKey)
	if err != nil {
		return fmt.Errorf("failed to encode CA private key: %w", err)
	}

	secret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      caSecretName,
			Namespace: ca.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "novaedge",
				"app.kubernetes.io/component":  "mesh-ca",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			caSecretKeyCA:  ca.caCertPEM,
			caSecretKeyKey: keyPEM,
		},
	}

	if err := cl.Create(ctx, secret); err != nil {
		return fmt.Errorf("failed to create mesh CA secret: %w", err)
	}

	ca.logger.Info("mesh CA initialized and persisted to secret",
		zap.String("secret", caSecretName),
		zap.String("namespace", ca.namespace))

	return nil
}

// SignCSR parses a PEM-encoded CSR and issues a short-lived workload
// certificate with a SPIFFE URI SAN identifying the agent node.
func (ca *MeshCA) SignCSR(csrPEM []byte, nodeName string) ([]byte, error) {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	if ca.caCert == nil || ca.caKey == nil {
		return nil, errMeshCANotInitialized
	}

	block, _ := pem.Decode(csrPEM)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, errFailedToDecodeCSRPEM
	}

	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CSR: %w", err)
	}

	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("CSR signature verification failed: %w", err)
	}

	// Validate that the nodeName matches the identity in the CSR.
	// The CSR must contain a SPIFFE URI SAN matching the expected agent identity,
	// or the Subject CN must match the nodeName.
	if err := validateCSRIdentity(csr, nodeName, ca.trustDomain); err != nil {
		return nil, err
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	spiffeURI, err := url.Parse(fmt.Sprintf("spiffe://%s/agent/%s", ca.trustDomain, nodeName))
	if err != nil {
		return nil, fmt.Errorf("failed to parse SPIFFE URI: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      csr.Subject,
		NotBefore:    now,
		NotAfter:     now.Add(workloadCertValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageClientAuth,
			x509.ExtKeyUsageServerAuth,
		},
		URIs:                  []*url.URL{spiffeURI},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, ca.caCert, csr.PublicKey, ca.caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	ca.logger.Info("issued workload certificate",
		zap.String("node", nodeName),
		zap.String("spiffeID", spiffeURI.String()),
		zap.Time("notAfter", template.NotAfter))

	return certPEM, nil
}

// CACertPEM returns the PEM-encoded root CA certificate (trust bundle).
func (ca *MeshCA) CACertPEM() []byte {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	dst := make([]byte, len(ca.caCertPEM))
	copy(dst, ca.caCertPEM)
	return dst
}

// TrustDomain returns the SPIFFE trust domain configured for this CA.
func (ca *MeshCA) TrustDomain() string {
	return ca.trustDomain
}

// generateRootCA creates a new ECDSA P-384 root CA keypair and self-signed certificate.
func (ca *MeshCA) generateRootCA() error {
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate ECDSA P-384 key: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return fmt.Errorf("failed to generate serial number: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: "NovaEdge Mesh CA",
		},
		NotBefore:             now,
		NotAfter:              now.Add(rootCAValidityDuration),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		MaxPathLen:            1,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("failed to create root CA certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return fmt.Errorf("failed to parse root CA certificate: %w", err)
	}

	ca.caCert = cert
	ca.caKey = key
	ca.caCertPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	return nil
}

// loadFromSecret loads the root CA certificate and private key from a Kubernetes Secret.
func (ca *MeshCA) loadFromSecret(secret *corev1.Secret) error {
	certPEM, ok := secret.Data[caSecretKeyCA]
	if !ok {
		return fmt.Errorf("%w: %s/%s missing key %q", errSecret, ca.namespace, caSecretName, caSecretKeyCA)
	}

	keyPEM, ok := secret.Data[caSecretKeyKey]
	if !ok {
		return fmt.Errorf("%w: %s/%s missing key %q", errSecret, ca.namespace, caSecretName, caSecretKeyKey)
	}

	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return errFailedToDecodeCACertificatePEM
	}

	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil || keyBlock.Type != "EC PRIVATE KEY" {
		return errFailedToDecodeCAPrivateKeyPEM
	}

	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse CA private key: %w", err)
	}

	ca.caCert = cert
	ca.caKey = key
	ca.caCertPEM = certPEM

	ca.logger.Info("loaded mesh CA from secret",
		zap.String("subject", cert.Subject.CommonName),
		zap.Time("notAfter", cert.NotAfter))

	return nil
}

// validateCSRIdentity checks that the CSR's identity matches the requested
// nodeName. It checks URI SANs for a matching SPIFFE ID first, then falls
// back to the Subject CN. If the CSR contains neither, the request is
// rejected.
func validateCSRIdentity(csr *x509.CertificateRequest, nodeName, trustDomain string) error {
	expectedSPIFFE := fmt.Sprintf("spiffe://%s/agent/%s", trustDomain, nodeName)

	// Check URI SANs for a matching SPIFFE ID.
	for _, uri := range csr.URIs {
		if uri.String() == expectedSPIFFE {
			return nil
		}
	}

	// If no URI SANs present, fall back to CN match.
	if len(csr.URIs) == 0 && csr.Subject.CommonName == nodeName {
		return nil
	}

	// If URI SANs are present but none match, reject.
	if len(csr.URIs) > 0 {
		uris := make([]string, 0, len(csr.URIs))
		for _, u := range csr.URIs {
			uris = append(uris, u.String())
		}
		return fmt.Errorf("%w: SANs=%v, expected=%q", errCSRURISANMismatch, uris, expectedSPIFFE)
	}

	return fmt.Errorf("%w: CN=%q, node=%q", errCSRCNMismatch, csr.Subject.CommonName, nodeName)
}

// randomSerial generates a cryptographically random 128-bit serial number.
func randomSerial() (*big.Int, error) {
	serialMax := new(big.Int).Lsh(big.NewInt(1), serialBitLen)
	serial, err := rand.Int(rand.Reader, serialMax)
	if err != nil {
		return nil, fmt.Errorf("failed to generate random serial: %w", err)
	}
	return serial, nil
}

// encodeECPrivateKey marshals an ECDSA private key to PEM format.
func encodeECPrivateKey(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal EC private key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: der,
	}), nil
}
