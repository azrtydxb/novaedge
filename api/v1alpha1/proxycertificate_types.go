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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CertificateIssuerType defines the type of certificate issuer
// +kubebuilder:validation:Enum=acme;manual;self-signed;cert-manager;vault-pki
type CertificateIssuerType string

const (
	// CertificateIssuerTypeACME uses ACME protocol (e.g., Let's Encrypt)
	CertificateIssuerTypeACME CertificateIssuerType = "acme"
	// CertificateIssuerTypeManual uses manually provided certificates
	CertificateIssuerTypeManual CertificateIssuerType = "manual"
	// CertificateIssuerTypeSelfSigned generates self-signed certificates
	CertificateIssuerTypeSelfSigned CertificateIssuerType = "self-signed"
	// CertificateIssuerTypeCertManager integrates with cert-manager
	CertificateIssuerTypeCertManager CertificateIssuerType = "cert-manager"
	// CertificateIssuerTypeVaultPKI integrates with HashiCorp Vault PKI
	CertificateIssuerTypeVaultPKI CertificateIssuerType = "vault-pki"
)

// ACMEChallengeType defines the ACME challenge type
// +kubebuilder:validation:Enum=http-01;dns-01;tls-alpn-01
type ACMEChallengeType string

const (
	// ACMEChallengeHTTP01 uses HTTP-01 challenge
	ACMEChallengeHTTP01 ACMEChallengeType = "http-01"
	// ACMEChallengeDNS01 uses DNS-01 challenge
	ACMEChallengeDNS01 ACMEChallengeType = "dns-01"
	// ACMEChallengeTLSALPN01 uses TLS-ALPN-01 challenge
	ACMEChallengeTLSALPN01 ACMEChallengeType = "tls-alpn-01"
)

// CertificateKeyType defines the key type for certificate generation
// +kubebuilder:validation:Enum=RSA2048;RSA4096;EC256;EC384
type CertificateKeyType string

// Certificate key type constants.
const (
	CertificateKeyTypeRSA2048 CertificateKeyType = "RSA2048"
	CertificateKeyTypeRSA4096 CertificateKeyType = "RSA4096"
	CertificateKeyTypeEC256   CertificateKeyType = "EC256"
	CertificateKeyTypeEC384   CertificateKeyType = "EC384"
)

// CertificateState defines the current state of a certificate
// +kubebuilder:validation:Enum=Pending;Requesting;Ready;Renewing;Failed
type CertificateState string

// Certificate state constants.
const (
	CertificateStatePending    CertificateState = "Pending"
	CertificateStateRequesting CertificateState = "Requesting"
	CertificateStateReady      CertificateState = "Ready"
	CertificateStateRenewing   CertificateState = "Renewing"
	CertificateStateFailed     CertificateState = "Failed"
)

// ACMEIssuerConfig configures ACME-based certificate issuance
type ACMEIssuerConfig struct {
	// Server is the ACME server URL (default: Let's Encrypt production)
	// +optional
	// +kubebuilder:default="https://acme-v02.api.letsencrypt.org/directory"
	Server string `json:"server,omitempty"`

	// Email is the email address for ACME registration
	// +kubebuilder:validation:Required
	Email string `json:"email"`

	// ChallengeType specifies the ACME challenge type
	// +optional
	// +kubebuilder:default="http-01"
	ChallengeType ACMEChallengeType `json:"challengeType,omitempty"`

	// DNSProvider for DNS-01 challenges (e.g., cloudflare, route53)
	// +optional
	DNSProvider string `json:"dnsProvider,omitempty"`

	// DNSCredentialsRef references a Secret containing DNS provider credentials
	// +optional
	DNSCredentialsRef *corev1.SecretReference `json:"dnsCredentialsRef,omitempty"`

	// AccountKeyRef references a Secret containing the ACME account private key
	// If not specified, a new account key will be generated and stored
	// +optional
	AccountKeyRef *corev1.SecretReference `json:"accountKeyRef,omitempty"`

	// PreferredChain specifies the preferred issuer CN (e.g., "ISRG Root X1")
	// +optional
	PreferredChain string `json:"preferredChain,omitempty"`

	// AcceptTOS indicates acceptance of the ACME Terms of Service
	// +kubebuilder:default=true
	AcceptTOS bool `json:"acceptTOS,omitempty"`

	// DNS01 configures DNS-01 specific options
	// +optional
	DNS01 *DNS01Config `json:"dns01,omitempty"`

	// TLSALPN01 configures TLS-ALPN-01 specific options
	// +optional
	TLSALPN01 *TLSALPN01Config `json:"tlsAlpn01,omitempty"`
}

// DNS01Config configures DNS-01 ACME challenge
type DNS01Config struct {
	// Provider specifies the DNS provider (cloudflare, route53, googledns)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=cloudflare;route53;googledns
	Provider string `json:"provider"`

	// CredentialsRef references a Secret containing DNS provider credentials
	// +kubebuilder:validation:Required
	CredentialsRef LocalObjectReference `json:"credentialsRef"`

	// PropagationTimeout is the maximum time to wait for DNS propagation
	// +optional
	// +kubebuilder:default="120s"
	PropagationTimeout string `json:"propagationTimeout,omitempty"`

	// PollingInterval is the time between DNS propagation checks
	// +optional
	// +kubebuilder:default="5s"
	PollingInterval string `json:"pollingInterval,omitempty"`
}

// TLSALPN01Config configures TLS-ALPN-01 ACME challenge
type TLSALPN01Config struct {
	// Port is the port to listen on for TLS-ALPN-01 challenges (default: 443)
	// +optional
	// +kubebuilder:default=443
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`
}

// ManualIssuerConfig configures manual certificate provision
type ManualIssuerConfig struct {
	// SecretRef references a Secret containing tls.crt and tls.key
	// +kubebuilder:validation:Required
	SecretRef corev1.SecretReference `json:"secretRef"`
}

// SelfSignedIssuerConfig configures self-signed certificate generation
type SelfSignedIssuerConfig struct {
	// Validity is the certificate validity duration (default: 8760h = 1 year)
	// +optional
	// +kubebuilder:default="8760h"
	Validity metav1.Duration `json:"validity,omitempty"`

	// Organization is the organization name in the certificate
	// +optional
	// +kubebuilder:default="NovaEdge Self-Signed"
	Organization string `json:"organization,omitempty"`
}

// CertManagerIssuerConfig configures cert-manager integration
type CertManagerIssuerConfig struct {
	// IssuerRef references a cert-manager Issuer or ClusterIssuer
	// +kubebuilder:validation:Required
	IssuerRef ObjectReference `json:"issuerRef"`
}

// CertificateIssuer configures how the certificate is obtained
type CertificateIssuer struct {
	// Type specifies the issuer type
	// +kubebuilder:validation:Required
	Type CertificateIssuerType `json:"type"`

	// ACME configures ACME-based issuance
	// +optional
	ACME *ACMEIssuerConfig `json:"acme,omitempty"`

	// Manual configures manual certificate provision
	// +optional
	Manual *ManualIssuerConfig `json:"manual,omitempty"`

	// SelfSigned configures self-signed certificate generation
	// +optional
	SelfSigned *SelfSignedIssuerConfig `json:"selfSigned,omitempty"`

	// CertManager configures cert-manager integration
	// +optional
	CertManager *CertManagerIssuerConfig `json:"certManager,omitempty"`

	// VaultPKI configures HashiCorp Vault PKI integration
	// +optional
	VaultPKI *VaultPKIIssuerConfig `json:"vaultPKI,omitempty"`
}

// VaultPKIIssuerConfig configures Vault PKI secrets engine for certificate issuance
type VaultPKIIssuerConfig struct {
	// Path is the Vault PKI mount path (e.g., "pki" or "pki-int")
	// +kubebuilder:validation:Required
	Path string `json:"path"`

	// Role is the Vault PKI role name
	// +kubebuilder:validation:Required
	Role string `json:"role"`

	// TTL is the requested certificate TTL (e.g., "720h")
	// +optional
	TTL string `json:"ttl,omitempty"`
}

// ProxyCertificateSpec defines the desired state of ProxyCertificate
type ProxyCertificateSpec struct {
	// Domains is the list of domains to include in the certificate
	// The first domain will be used as the Common Name (CN)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Domains []string `json:"domains"`

	// Issuer configures how the certificate is obtained
	// +kubebuilder:validation:Required
	Issuer CertificateIssuer `json:"issuer"`

	// SecretName is the name of the Secret to store the certificate
	// If not specified, a name will be generated from the ProxyCertificate name
	// +optional
	SecretName string `json:"secretName,omitempty"`

	// KeyType specifies the key type for certificate generation
	// +optional
	// +kubebuilder:default="EC256"
	KeyType CertificateKeyType `json:"keyType,omitempty"`

	// RenewBefore specifies how long before expiry to renew the certificate
	// +optional
	// +kubebuilder:default="720h"
	RenewBefore metav1.Duration `json:"renewBefore,omitempty"`

	// MustStaple enables OCSP Must-Staple extension
	// +optional
	MustStaple bool `json:"mustStaple,omitempty"`
}

// ProxyCertificateStatus defines the observed state of ProxyCertificate
type ProxyCertificateStatus struct {
	// State is the current state of the certificate
	// +optional
	State CertificateState `json:"state,omitempty"`

	// SecretName is the name of the Secret containing the certificate
	// +optional
	SecretName string `json:"secretName,omitempty"`

	// NotBefore is when the certificate becomes valid
	// +optional
	NotBefore *metav1.Time `json:"notBefore,omitempty"`

	// NotAfter is when the certificate expires
	// +optional
	NotAfter *metav1.Time `json:"notAfter,omitempty"`

	// LastRenewalTime is when the certificate was last renewed
	// +optional
	LastRenewalTime *metav1.Time `json:"lastRenewalTime,omitempty"`

	// NextRenewalTime is when the next renewal should occur
	// +optional
	NextRenewalTime *metav1.Time `json:"nextRenewalTime,omitempty"`

	// SerialNumber is the certificate serial number
	// +optional
	SerialNumber string `json:"serialNumber,omitempty"`

	// Issuer is the certificate issuer's Common Name
	// +optional
	Issuer string `json:"issuer,omitempty"`

	// Message provides additional status information
	// +optional
	Message string `json:"message,omitempty"`

	// Conditions represent the latest available observations
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// ObservedGeneration is the most recent generation observed
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Domains",type=string,JSONPath=`.spec.domains[0]`
// +kubebuilder:printcolumn:name="Issuer",type=string,JSONPath=`.spec.issuer.type`
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="Expires",type=date,JSONPath=`.status.notAfter`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ProxyCertificate manages TLS certificates for NovaEdge
type ProxyCertificate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProxyCertificateSpec   `json:"spec,omitempty"`
	Status ProxyCertificateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProxyCertificateList contains a list of ProxyCertificate
type ProxyCertificateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProxyCertificate `json:"items"`
}

