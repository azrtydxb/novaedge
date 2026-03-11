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

// Package acme provides ACME (Automatic Certificate Management Environment) support
// for automated TLS certificate provisioning using Let's Encrypt and other ACME providers.
package acme

import (
	"crypto/tls"
	"time"
)

// Config holds ACME client configuration.
type Config struct {
	// Email for ACME account registration
	Email string `json:"email" yaml:"email"`

	// Server is the ACME server URL
	// Defaults to Let's Encrypt production
	Server string `json:"server,omitempty" yaml:"server,omitempty"`

	// KeyType specifies the type of key to use for certificates
	// Options: RSA2048, RSA4096, EC256, EC384
	KeyType string `json:"keyType,omitempty" yaml:"keyType,omitempty"`

	// ChallengeType specifies the ACME challenge type
	// Options: http-01, dns-01, tls-alpn-01
	ChallengeType string `json:"challengeType,omitempty" yaml:"challengeType,omitempty"`

	// DNSProvider for DNS-01 challenges (e.g., cloudflare, route53, godaddy)
	DNSProvider string `json:"dnsProvider,omitempty" yaml:"dnsProvider,omitempty"`

	// DNSCredentials for DNS-01 challenges
	DNSCredentials map[string]string `json:"dnsCredentials,omitempty" yaml:"dnsCredentials,omitempty"`

	// RenewalDays specifies how many days before expiry to renew
	// Defaults to 30
	RenewalDays int `json:"renewalDays,omitempty" yaml:"renewalDays,omitempty"`

	// Storage configuration for certificates
	Storage StorageConfig `json:"storage,omitempty" yaml:"storage,omitempty"`

	// AcceptTOS indicates acceptance of ACME Terms of Service
	AcceptTOS bool `json:"acceptTOS,omitempty" yaml:"acceptTOS,omitempty"`
}

// StorageConfig configures certificate storage.
type StorageConfig struct {
	// Type of storage: file, kubernetes-secret
	Type string `json:"type,omitempty" yaml:"type,omitempty"`

	// Path for file storage (directory path)
	Path string `json:"path,omitempty" yaml:"path,omitempty"`

	// SecretNamespace for Kubernetes secret storage
	SecretNamespace string `json:"secretNamespace,omitempty" yaml:"secretNamespace,omitempty"`
}

// CertificateRequest represents a request for a certificate.
type CertificateRequest struct {
	// Domains to include in the certificate (first is primary/CN)
	Domains []string `json:"domains"`

	// MustStaple enables OCSP Must-Staple extension
	MustStaple bool `json:"mustStaple,omitempty"`

	// PreferredChain specifies preferred issuer CN (e.g., "ISRG Root X1")
	PreferredChain string `json:"preferredChain,omitempty"`
}

// Certificate represents a managed TLS certificate.
type Certificate struct {
	// Domains covered by this certificate
	Domains []string `json:"domains"`

	// Certificate PEM bytes
	CertificatePEM []byte `json:"certificatePEM"`

	// PrivateKey PEM bytes
	PrivateKeyPEM []byte `json:"privateKeyPEM"`

	// IssuerCertificatePEM is the issuer certificate chain
	IssuerCertificatePEM []byte `json:"issuerCertificatePEM,omitempty"`

	// NotBefore is when the certificate becomes valid
	NotBefore time.Time `json:"notBefore"`

	// NotAfter is when the certificate expires
	NotAfter time.Time `json:"notAfter"`

	// SerialNumber of the certificate
	SerialNumber string `json:"serialNumber,omitempty"`

	// Issuer CN
	Issuer string `json:"issuer,omitempty"`
}

// TLSCertificate returns a tls.Certificate from the stored PEM data.
func (c *Certificate) TLSCertificate() (*tls.Certificate, error) {
	cert, err := tls.X509KeyPair(c.CertificatePEM, c.PrivateKeyPEM)
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// IsExpired returns true if the certificate is expired.
func (c *Certificate) IsExpired() bool {
	return time.Now().After(c.NotAfter)
}

// ShouldRenew returns true if the certificate should be renewed
// based on the given renewal threshold.
func (c *Certificate) ShouldRenew(renewBefore time.Duration) bool {
	return time.Now().Add(renewBefore).After(c.NotAfter)
}

// ExpiresIn returns the duration until the certificate expires.
func (c *Certificate) ExpiresIn() time.Duration {
	return time.Until(c.NotAfter)
}

// ChallengeInfo contains information about a pending ACME challenge.
type ChallengeInfo struct {
	// Type of challenge (http-01, dns-01, tls-alpn-01)
	Type string `json:"type"`

	// Domain being validated
	Domain string `json:"domain"`

	// Token for the challenge
	Token string `json:"token"`

	// KeyAuth is the key authorization for the challenge
	KeyAuth string `json:"keyAuth"`

	// URL for challenge response (HTTP-01)
	URL string `json:"url,omitempty"`

	// DNSRecord to create (DNS-01)
	DNSRecord string `json:"dnsRecord,omitempty"`

	// DNSValue to set (DNS-01)
	DNSValue string `json:"dnsValue,omitempty"`
}

// AccountInfo contains ACME account information.
type AccountInfo struct {
	// Email address
	Email string `json:"email"`

	// URI of the account on the ACME server
	URI string `json:"uri"`

	// Registration timestamp
	Registration time.Time `json:"registration"`

	// PrivateKey in PEM format
	PrivateKeyPEM []byte `json:"privateKeyPEM"`
}

// Well-known ACME server URLs.
const (
	LetsEncryptProduction = "https://acme-v02.api.letsencrypt.org/directory"
	LetsEncryptStaging    = "https://acme-staging-v02.api.letsencrypt.org/directory"
	ZeroSSLProduction     = "https://acme.zerossl.com/v2/DV90"
)

// Challenge types.
const (
	ChallengeHTTP01    = "http-01"
	ChallengeDNS01     = "dns-01"
	ChallengeTLSALPN01 = "tls-alpn-01"
)

// Key types.
const (
	KeyTypeRSA2048 = "RSA2048"
	KeyTypeRSA4096 = "RSA4096"
	KeyTypeEC256   = "EC256"
	KeyTypeEC384   = "EC384"
)

// Default configuration values.
const (
	DefaultServer      = LetsEncryptProduction
	DefaultKeyType     = KeyTypeEC256
	DefaultChallenge   = ChallengeHTTP01
	DefaultRenewalDays = 30
	DefaultStorageType = "file"
	DefaultStoragePath = "/var/lib/novaedge/certs"
)

// ApplyDefaults fills in default values for empty fields.
func (c *Config) ApplyDefaults() {
	if c.Server == "" {
		c.Server = DefaultServer
	}
	if c.KeyType == "" {
		c.KeyType = DefaultKeyType
	}
	if c.ChallengeType == "" {
		c.ChallengeType = DefaultChallenge
	}
	if c.RenewalDays <= 0 {
		c.RenewalDays = DefaultRenewalDays
	}
	if c.Storage.Type == "" {
		c.Storage.Type = DefaultStorageType
	}
	if c.Storage.Path == "" && c.Storage.Type == DefaultStorageType {
		c.Storage.Path = DefaultStoragePath
	}
}
