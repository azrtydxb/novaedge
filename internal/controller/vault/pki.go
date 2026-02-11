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

package vault

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

// PKICertificate represents a certificate issued by Vault PKI.
type PKICertificate struct {
	Certificate    string    `json:"certificate"`
	IssuingCA      string    `json:"issuing_ca"`
	CAChain        []string  `json:"ca_chain"`
	PrivateKey     string    `json:"private_key"`
	PrivateKeyType string    `json:"private_key_type"`
	SerialNumber   string    `json:"serial_number"`
	Expiration     int64     `json:"expiration"`
	ExpiresAt      time.Time `json:"-"`
}

// PKIRequest represents a request to issue a certificate from Vault PKI.
type PKIRequest struct {
	// MountPath is the PKI secrets engine mount path (e.g., "pki", "pki-int")
	MountPath string

	// Role is the PKI role to use for issuance
	Role string

	// CommonName is the CN for the certificate
	CommonName string

	// AltNames are Subject Alternative Names (comma-separated)
	AltNames []string

	// IPSANs are IP Subject Alternative Names
	IPSANs []string

	// TTL is the requested certificate TTL
	TTL string

	// Format specifies the output format (pem, der, pem_bundle)
	Format string
}

// PKIManager handles operations with the Vault PKI secrets engine.
type PKIManager struct {
	client *Client
	logger *zap.Logger
}

// NewPKIManager creates a new PKI manager.
func NewPKIManager(client *Client, logger *zap.Logger) *PKIManager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &PKIManager{
		client: client,
		logger: logger,
	}
}

// IssueCertificate requests a certificate from Vault PKI.
func (p *PKIManager) IssueCertificate(ctx context.Context, req *PKIRequest) (*PKICertificate, error) {
	if req.MountPath == "" {
		return nil, fmt.Errorf("PKI mount path is required")
	}
	if req.Role == "" {
		return nil, fmt.Errorf("PKI role is required")
	}
	if req.CommonName == "" {
		return nil, fmt.Errorf("common name is required")
	}

	path := fmt.Sprintf("%s/issue/%s", req.MountPath, req.Role)

	data := map[string]interface{}{
		"common_name": req.CommonName,
	}

	if len(req.AltNames) > 0 {
		data["alt_names"] = strings.Join(req.AltNames, ",")
	}
	if len(req.IPSANs) > 0 {
		data["ip_sans"] = strings.Join(req.IPSANs, ",")
	}
	if req.TTL != "" {
		data["ttl"] = req.TTL
	}
	if req.Format != "" {
		data["format"] = req.Format
	} else {
		data["format"] = "pem"
	}

	resp, err := p.client.Write(ctx, path, data)
	if err != nil {
		return nil, fmt.Errorf("failed to issue certificate from Vault PKI: %w", err)
	}

	cert := &PKICertificate{}

	if certStr, ok := resp.Data["certificate"].(string); ok {
		cert.Certificate = certStr
	}
	if caStr, ok := resp.Data["issuing_ca"].(string); ok {
		cert.IssuingCA = caStr
	}
	if keyStr, ok := resp.Data["private_key"].(string); ok {
		cert.PrivateKey = keyStr
	}
	if keyType, ok := resp.Data["private_key_type"].(string); ok {
		cert.PrivateKeyType = keyType
	}
	if serial, ok := resp.Data["serial_number"].(string); ok {
		cert.SerialNumber = serial
	}
	if exp, ok := resp.Data["expiration"].(float64); ok {
		cert.Expiration = int64(exp)
		cert.ExpiresAt = time.Unix(int64(exp), 0)
	}

	// Parse CA chain
	if chain, ok := resp.Data["ca_chain"].([]interface{}); ok {
		for _, c := range chain {
			if chainStr, ok := c.(string); ok {
				cert.CAChain = append(cert.CAChain, chainStr)
			}
		}
	}

	// Validate required fields in Vault PKI response
	if cert.Certificate == "" {
		return nil, fmt.Errorf("vault PKI response missing certificate field")
	}
	if cert.PrivateKey == "" {
		return nil, fmt.Errorf("vault PKI response missing private_key field")
	}

	p.logger.Info("Issued certificate from Vault PKI",
		zap.String("mountPath", req.MountPath),
		zap.String("role", req.Role),
		zap.String("commonName", req.CommonName),
		zap.String("serialNumber", cert.SerialNumber),
		zap.Time("expiresAt", cert.ExpiresAt))

	return cert, nil
}

// RevokeCertificate revokes a certificate by serial number.
func (p *PKIManager) RevokeCertificate(ctx context.Context, mountPath, serialNumber string) error {
	path := fmt.Sprintf("%s/revoke", mountPath)

	_, err := p.client.Write(ctx, path, map[string]interface{}{
		"serial_number": serialNumber,
	})
	if err != nil {
		return fmt.Errorf("failed to revoke certificate: %w", err)
	}

	p.logger.Info("Revoked certificate",
		zap.String("mountPath", mountPath),
		zap.String("serialNumber", serialNumber))
	return nil
}

// ShouldRenew checks if a PKI certificate should be renewed.
// It returns true if the certificate will expire within the given duration.
func (p *PKIManager) ShouldRenew(cert *PKICertificate, renewBefore time.Duration) bool {
	if cert.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().Add(renewBefore).After(cert.ExpiresAt)
}

// CertToPEM returns the certificate and key in PEM format suitable for K8s Secrets.
func (cert *PKICertificate) CertToPEM() (certPEM, keyPEM []byte) {
	// Build full chain: cert + CA
	fullChain := cert.Certificate
	if cert.IssuingCA != "" {
		fullChain += "\n" + cert.IssuingCA
	}
	return []byte(fullChain), []byte(cert.PrivateKey)
}
