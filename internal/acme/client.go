package acme

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
	"go.uber.org/zap"
)

// Client provides ACME certificate management functionality.
type Client struct {
	config   *Config
	storage  Storage
	provider ChallengeProvider
	logger   *zap.Logger

	mu      sync.RWMutex
	account *AccountInfo
	client  *lego.Client
}

// User implements registration.User for lego.
type User struct {
	email      string
	key        crypto.PrivateKey
	accountURI string
}

// GetEmail returns the user's email.
func (u *User) GetEmail() string {
	return u.email
}

// GetRegistration returns the account registration.
func (u *User) GetRegistration() *registration.Resource {
	if u.accountURI == "" {
		return nil
	}
	return &registration.Resource{
		URI: u.accountURI,
	}
}

// GetPrivateKey returns the user's private key.
func (u *User) GetPrivateKey() crypto.PrivateKey {
	return u.key
}

// NewClient creates a new ACME client.
func NewClient(config *Config, storage Storage, provider ChallengeProvider, logger *zap.Logger) (*Client, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}
	if storage == nil {
		return nil, fmt.Errorf("storage is required")
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	config.ApplyDefaults()

	return &Client{
		config:   config,
		storage:  storage,
		provider: provider,
		logger:   logger,
	}, nil
}

// Initialize sets up the ACME client, loading or creating an account.
func (c *Client) Initialize(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Try to load existing account
	account, err := c.storage.LoadAccount(ctx)
	if err != nil {
		c.logger.Debug("No existing account found, will create new", zap.Error(err))
	}

	var privateKey crypto.PrivateKey

	if account != nil {
		// Decode existing private key
		block, _ := pem.Decode(account.PrivateKeyPEM)
		if block == nil {
			return fmt.Errorf("failed to decode account private key")
		}

		privateKey, err = x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			// Try RSA
			privateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
			if err != nil {
				return fmt.Errorf("failed to parse account private key: %w", err)
			}
		}
		c.account = account
	} else {
		// Generate new private key
		privateKey, err = c.generatePrivateKey()
		if err != nil {
			return fmt.Errorf("failed to generate private key: %w", err)
		}
	}

	// Create lego user
	user := &User{
		email:      c.config.Email,
		key:        privateKey,
		accountURI: "",
	}
	if c.account != nil {
		user.accountURI = c.account.URI
	}

	// Configure lego client
	legoConfig := lego.NewConfig(user)
	legoConfig.CADirURL = c.config.Server
	legoConfig.Certificate.KeyType = c.getKeyType()

	// Create lego client
	client, err := lego.NewClient(legoConfig)
	if err != nil {
		return fmt.Errorf("failed to create ACME client: %w", err)
	}

	// Set up challenge provider
	if err := c.setupChallengeProvider(client); err != nil {
		return fmt.Errorf("failed to setup challenge provider: %w", err)
	}

	c.client = client

	// Register account if not already registered
	if c.account == nil {
		if err := c.registerAccount(ctx, user, privateKey); err != nil {
			return fmt.Errorf("failed to register account: %w", err)
		}
	}

	c.logger.Info("ACME client initialized",
		zap.String("email", c.config.Email),
		zap.String("server", c.config.Server),
	)

	return nil
}

// registerAccount registers a new ACME account.
func (c *Client) registerAccount(ctx context.Context, _ *User, privateKey crypto.PrivateKey) error {
	reg, err := c.client.Registration.Register(registration.RegisterOptions{
		TermsOfServiceAgreed: c.config.AcceptTOS,
	})
	if err != nil {
		return err
	}

	// Encode private key
	var keyPEM []byte
	switch k := privateKey.(type) {
	case *ecdsa.PrivateKey:
		keyBytes, err := x509.MarshalECPrivateKey(k)
		if err != nil {
			return err
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
	default:
		return fmt.Errorf("unsupported private key type")
	}

	c.account = &AccountInfo{
		Email:         c.config.Email,
		URI:           reg.URI,
		Registration:  time.Now(),
		PrivateKeyPEM: keyPEM,
	}

	// Save account
	if err := c.storage.SaveAccount(ctx, c.account); err != nil {
		c.logger.Warn("Failed to save account", zap.Error(err))
	}

	c.logger.Info("Registered new ACME account",
		zap.String("email", c.config.Email),
		zap.String("uri", reg.URI),
	)

	return nil
}

// ObtainCertificate requests a new certificate for the given domains.
func (c *Client) ObtainCertificate(ctx context.Context, req *CertificateRequest) (*Certificate, error) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	if len(req.Domains) == 0 {
		return nil, fmt.Errorf("at least one domain is required")
	}

	c.logger.Info("Requesting certificate",
		zap.Strings("domains", req.Domains),
	)

	request := certificate.ObtainRequest{
		Domains:    req.Domains,
		Bundle:     true,
		MustStaple: req.MustStaple,
	}

	if req.PreferredChain != "" {
		request.PreferredChain = req.PreferredChain
	}

	// Request certificate
	cert, err := client.Certificate.Obtain(request)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain certificate: %w", err)
	}

	// Parse certificate to get expiry info
	certData, err := parseCertificate(cert.Certificate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	result := &Certificate{
		Domains:              req.Domains,
		CertificatePEM:       cert.Certificate,
		PrivateKeyPEM:        cert.PrivateKey,
		IssuerCertificatePEM: cert.IssuerCertificate,
		NotBefore:            certData.NotBefore,
		NotAfter:             certData.NotAfter,
		SerialNumber:         certData.SerialNumber.String(),
		Issuer:               certData.Issuer.CommonName,
	}

	// Save certificate
	if err := c.storage.SaveCertificate(ctx, result); err != nil {
		c.logger.Warn("Failed to save certificate", zap.Error(err))
	}

	c.logger.Info("Certificate obtained",
		zap.Strings("domains", req.Domains),
		zap.Time("expires", result.NotAfter),
	)

	return result, nil
}

// RenewCertificate renews an existing certificate.
func (c *Client) RenewCertificate(ctx context.Context, domain string) (*Certificate, error) {
	// Load existing certificate
	existing, err := c.storage.LoadCertificate(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("failed to load existing certificate: %w", err)
	}

	c.logger.Info("Renewing certificate",
		zap.String("domain", domain),
		zap.Time("currentExpiry", existing.NotAfter),
	)

	// Request new certificate with same domains
	return c.ObtainCertificate(ctx, &CertificateRequest{
		Domains: existing.Domains,
	})
}

// GetCertificate retrieves a certificate from storage.
func (c *Client) GetCertificate(ctx context.Context, domain string) (*Certificate, error) {
	return c.storage.LoadCertificate(ctx, domain)
}

// GetCertificatesNeedingRenewal returns certificates that need renewal.
func (c *Client) GetCertificatesNeedingRenewal(ctx context.Context) ([]*Certificate, error) {
	certs, err := c.storage.ListCertificates(ctx)
	if err != nil {
		return nil, err
	}

	renewBefore := time.Duration(c.config.RenewalDays) * 24 * time.Hour
	var needRenewal []*Certificate

	for _, cert := range certs {
		if cert.ShouldRenew(renewBefore) {
			needRenewal = append(needRenewal, cert)
		}
	}

	return needRenewal, nil
}

// setupChallengeProvider configures the challenge provider for the client.
func (c *Client) setupChallengeProvider(client *lego.Client) error {
	if c.provider == nil {
		return nil // No custom provider, will use default
	}

	switch c.config.ChallengeType {
	case ChallengeHTTP01:
		return client.Challenge.SetHTTP01Provider(c.provider)
	case ChallengeDNS01:
		return client.Challenge.SetDNS01Provider(c.provider)
	case ChallengeTLSALPN01:
		return client.Challenge.SetTLSALPN01Provider(c.provider)
	default:
		return fmt.Errorf("unsupported challenge type: %s", c.config.ChallengeType)
	}
}

// generatePrivateKey generates a new private key based on configuration.
func (c *Client) generatePrivateKey() (crypto.PrivateKey, error) {
	switch c.config.KeyType {
	case KeyTypeEC256:
		return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case KeyTypeEC384:
		return ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	case KeyTypeRSA2048:
		return rsa.GenerateKey(rand.Reader, 2048)
	case KeyTypeRSA4096:
		return rsa.GenerateKey(rand.Reader, 4096)
	default:
		return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	}
}

// getKeyType converts our key type to lego's KeyType.
func (c *Client) getKeyType() certcrypto.KeyType {
	switch c.config.KeyType {
	case KeyTypeEC256:
		return certcrypto.EC256
	case KeyTypeEC384:
		return certcrypto.EC384
	case KeyTypeRSA2048:
		return certcrypto.RSA2048
	case KeyTypeRSA4096:
		return certcrypto.RSA4096
	default:
		return certcrypto.EC256
	}
}

// parseCertificate parses PEM-encoded certificate bytes.
func parseCertificate(pemBytes []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}
	return x509.ParseCertificate(block.Bytes)
}

// ParseCertificatePEM parses a PEM-encoded certificate or raw DER bytes.
func ParseCertificatePEM(data []byte) (*x509.Certificate, error) {
	// First try to decode as PEM
	block, _ := pem.Decode(data)
	if block != nil {
		return x509.ParseCertificate(block.Bytes)
	}
	// If PEM decode fails, try parsing as raw DER
	return x509.ParseCertificate(data)
}

// Close releases any resources held by the client.
func (c *Client) Close() error {
	return nil
}
