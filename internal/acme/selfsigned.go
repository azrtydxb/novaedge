package acme

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"time"
)

var (
	errAtLeastOneDomainOrIPIsRequired = errors.New("at least one domain or IP is required")
	errUnsupportedKeyType             = errors.New("unsupported key type")
	errFailedToDecodeCACertificate    = errors.New("failed to decode CA certificate")
	errFailedToDecodeCAPrivateKey     = errors.New("failed to decode CA private key")
	errUnsupportedCAKeyType           = errors.New("unsupported CA key type")
)

// generateKeyPair generates a private/public key pair based on the configured key type.
func generateKeyPair(keyType string) (privateKey, publicKey interface{}, err error) {
	switch keyType {
	case KeyTypeEC256:
		key, genErr := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if genErr != nil {
			return nil, nil, fmt.Errorf("failed to generate EC key: %w", genErr)
		}
		return key, &key.PublicKey, nil
	case KeyTypeEC384:
		key, genErr := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		if genErr != nil {
			return nil, nil, fmt.Errorf("failed to generate EC key: %w", genErr)
		}
		return key, &key.PublicKey, nil
	case KeyTypeRSA2048:
		key, genErr := rsa.GenerateKey(rand.Reader, 2048)
		if genErr != nil {
			return nil, nil, fmt.Errorf("failed to generate RSA key: %w", genErr)
		}
		return key, &key.PublicKey, nil
	case KeyTypeRSA4096:
		key, genErr := rsa.GenerateKey(rand.Reader, 4096)
		if genErr != nil {
			return nil, nil, fmt.Errorf("failed to generate RSA key: %w", genErr)
		}
		return key, &key.PublicKey, nil
	default:
		return nil, nil, fmt.Errorf("%w: %s", errUnsupportedKeyType, keyType)
	}
}

// SelfSignedConfig configures self-signed certificate generation.
type SelfSignedConfig struct {
	// Domains to include in the certificate (first is CN)
	Domains []string

	// IPs to include in the certificate
	IPs []net.IP

	// Organization name
	Organization string

	// Validity duration (default: 1 year)
	Validity time.Duration

	// KeyType: RSA2048, RSA4096, EC256, EC384 (default: EC256)
	KeyType string

	// IsCA indicates if this is a CA certificate
	IsCA bool
}

// GenerateSelfSigned generates a self-signed certificate.
func GenerateSelfSigned(config *SelfSignedConfig) (*Certificate, error) {
	if len(config.Domains) == 0 && len(config.IPs) == 0 {
		return nil, errAtLeastOneDomainOrIPIsRequired
	}

	// Set defaults
	if config.Validity == 0 {
		config.Validity = 365 * 24 * time.Hour // 1 year
	}
	if config.KeyType == "" {
		config.KeyType = KeyTypeEC256
	}
	if config.Organization == "" {
		config.Organization = "NovaEdge Self-Signed"
	}

	// Generate private key
	privateKey, publicKey, err := generateKeyPair(config.KeyType)
	if err != nil {
		return nil, err
	}

	// Generate serial number
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	// Create certificate template
	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{config.Organization},
		},
		NotBefore:             now,
		NotAfter:              now.Add(config.Validity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  config.IsCA,
	}

	// Set CN from first domain
	if len(config.Domains) > 0 {
		template.Subject.CommonName = config.Domains[0]
		template.DNSNames = config.Domains
	}

	// Add IPs
	if len(config.IPs) > 0 {
		template.IPAddresses = config.IPs
	}

	// If CA, allow cert signing
	if config.IsCA {
		template.KeyUsage |= x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	}

	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, publicKey, privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	// Encode certificate to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	// Encode private key to PEM
	var keyPEM []byte
	switch k := privateKey.(type) {
	case *ecdsa.PrivateKey:
		keyBytes, err := x509.MarshalECPrivateKey(k)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal EC key: %w", err)
		}
		keyPEM = pem.EncodeToMemory(&pem.Block{
			Type:  "EC PRIVATE KEY",
			Bytes: keyBytes,
		})
	case *rsa.PrivateKey:
		keyPEM = pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(k),
		})
	}

	return &Certificate{
		Domains:        config.Domains,
		CertificatePEM: certPEM,
		PrivateKeyPEM:  keyPEM,
		NotBefore:      template.NotBefore,
		NotAfter:       template.NotAfter,
		SerialNumber:   serialNumber.String(),
		Issuer:         "Self-Signed",
	}, nil
}

// GenerateCA generates a self-signed CA certificate.
func GenerateCA(organization string, validity time.Duration) (*Certificate, error) {
	return GenerateSelfSigned(&SelfSignedConfig{
		Domains:      []string{organization + " CA"},
		Organization: organization,
		Validity:     validity,
		KeyType:      KeyTypeEC256,
		IsCA:         true,
	})
}

// SignCertificate signs a certificate with a CA.
func SignCertificate(caCert *Certificate, config *SelfSignedConfig) (*Certificate, error) {
	if len(config.Domains) == 0 && len(config.IPs) == 0 {
		return nil, errAtLeastOneDomainOrIPIsRequired
	}

	// Parse CA certificate
	caBlock, _ := pem.Decode(caCert.CertificatePEM)
	if caBlock == nil {
		return nil, errFailedToDecodeCACertificate
	}
	caX509, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	// Parse CA private key
	caKeyBlock, _ := pem.Decode(caCert.PrivateKeyPEM)
	if caKeyBlock == nil {
		return nil, errFailedToDecodeCAPrivateKey
	}

	var caPrivateKey interface{}
	switch caKeyBlock.Type {
	case "EC PRIVATE KEY":
		caPrivateKey, err = x509.ParseECPrivateKey(caKeyBlock.Bytes)
	case "RSA PRIVATE KEY":
		caPrivateKey, err = x509.ParsePKCS1PrivateKey(caKeyBlock.Bytes)
	default:
		return nil, fmt.Errorf("%w: %s", errUnsupportedCAKeyType, caKeyBlock.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA private key: %w", err)
	}

	// Set defaults
	if config.Validity == 0 {
		config.Validity = 365 * 24 * time.Hour
	}
	if config.KeyType == "" {
		config.KeyType = KeyTypeEC256
	}
	if config.Organization == "" {
		config.Organization = "NovaEdge"
	}

	// Generate private key for new certificate
	privateKey, publicKey, err := generateKeyPair(config.KeyType)
	if err != nil {
		return nil, err
	}

	// Generate serial number
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	// Create certificate template
	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{config.Organization},
		},
		NotBefore:             now,
		NotAfter:              now.Add(config.Validity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	// Set CN from first domain
	if len(config.Domains) > 0 {
		template.Subject.CommonName = config.Domains[0]
		template.DNSNames = config.Domains
	}

	// Add IPs
	if len(config.IPs) > 0 {
		template.IPAddresses = config.IPs
	}

	// Sign with CA
	certDER, err := x509.CreateCertificate(rand.Reader, &template, caX509, publicKey, caPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	// Encode certificate to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	// Encode private key to PEM
	var keyPEM []byte
	switch k := privateKey.(type) {
	case *ecdsa.PrivateKey:
		keyBytes, err := x509.MarshalECPrivateKey(k)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal EC key: %w", err)
		}
		keyPEM = pem.EncodeToMemory(&pem.Block{
			Type:  "EC PRIVATE KEY",
			Bytes: keyBytes,
		})
	case *rsa.PrivateKey:
		keyPEM = pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(k),
		})
	}

	return &Certificate{
		Domains:              config.Domains,
		CertificatePEM:       certPEM,
		PrivateKeyPEM:        keyPEM,
		IssuerCertificatePEM: caCert.CertificatePEM,
		NotBefore:            template.NotBefore,
		NotAfter:             template.NotAfter,
		SerialNumber:         serialNumber.String(),
		Issuer:               caX509.Subject.CommonName,
	}, nil
}

// QuickSelfSigned generates a quick self-signed certificate for development.
func QuickSelfSigned(domains ...string) (*Certificate, error) {
	if len(domains) == 0 {
		domains = []string{"localhost"}
	}

	ips := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}

	return GenerateSelfSigned(&SelfSignedConfig{
		Domains:      domains,
		IPs:          ips,
		Organization: "NovaEdge Development",
		Validity:     365 * 24 * time.Hour,
		KeyType:      KeyTypeEC256,
	})
}
