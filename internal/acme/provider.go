package acme

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/go-acme/lego/v4/challenge"
	"go.uber.org/zap"
)

// ChallengeProvider is the interface for ACME challenge providers.
// It embeds lego's challenge.Provider interface.
type ChallengeProvider interface {
	challenge.Provider
}

// Storage defines the interface for certificate and account storage.
type Storage interface {
	// SaveCertificate stores a certificate.
	SaveCertificate(ctx context.Context, cert *Certificate) error

	// LoadCertificate retrieves a certificate by domain.
	LoadCertificate(ctx context.Context, domain string) (*Certificate, error)

	// DeleteCertificate removes a certificate.
	DeleteCertificate(ctx context.Context, domain string) error

	// ListCertificates returns all stored certificates.
	ListCertificates(ctx context.Context) ([]*Certificate, error)

	// SaveAccount stores ACME account information.
	SaveAccount(ctx context.Context, account *AccountInfo) error

	// LoadAccount retrieves ACME account information.
	LoadAccount(ctx context.Context) (*AccountInfo, error)
}

// HTTP01Provider implements HTTP-01 challenge handling.
type HTTP01Provider struct {
	mu         sync.RWMutex
	challenges map[string]string // token -> keyAuth
	logger     *zap.Logger
}

// NewHTTP01Provider creates a new HTTP-01 challenge provider.
func NewHTTP01Provider(logger *zap.Logger) *HTTP01Provider {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &HTTP01Provider{
		challenges: make(map[string]string),
		logger:     logger,
	}
}

// Present is called when a challenge should be set up.
func (p *HTTP01Provider) Present(domain, token, keyAuth string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.challenges[token] = keyAuth
	p.logger.Debug("HTTP-01 challenge presented",
		zap.String("domain", domain),
		zap.String("token", token),
	)
	return nil
}

// CleanUp is called when a challenge should be removed.
func (p *HTTP01Provider) CleanUp(domain, token, _ string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.challenges, token)
	p.logger.Debug("HTTP-01 challenge cleaned up",
		zap.String("domain", domain),
		zap.String("token", token),
	)
	return nil
}

// GetKeyAuth returns the key authorization for a given token.
func (p *HTTP01Provider) GetKeyAuth(token string) (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	keyAuth, ok := p.challenges[token]
	return keyAuth, ok
}

// Handler returns an HTTP handler for serving HTTP-01 challenges.
// Mount this at /.well-known/acme-challenge/
func (p *HTTP01Provider) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Path
		// Remove leading slash if present
		if len(token) > 0 && token[0] == '/' {
			token = token[1:]
		}

		keyAuth, ok := p.GetKeyAuth(token)
		if !ok {
			p.logger.Debug("Challenge token not found",
				zap.String("token", token),
			)
			http.NotFound(w, r)
			return
		}

		p.logger.Debug("Serving HTTP-01 challenge",
			zap.String("token", token),
		)

		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, keyAuth)
	})
}

// TLSALPN01Provider implements TLS-ALPN-01 challenge handling.
type TLSALPN01Provider struct {
	mu         sync.RWMutex
	challenges map[string]*TLSALPNChallenge
	logger     *zap.Logger
}

// TLSALPNChallenge holds TLS-ALPN-01 challenge data.
type TLSALPNChallenge struct {
	Domain  string
	KeyAuth string
}

// NewTLSALPN01Provider creates a new TLS-ALPN-01 challenge provider.
func NewTLSALPN01Provider(logger *zap.Logger) *TLSALPN01Provider {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &TLSALPN01Provider{
		challenges: make(map[string]*TLSALPNChallenge),
		logger:     logger,
	}
}

// Present is called when a challenge should be set up.
func (p *TLSALPN01Provider) Present(domain, _, keyAuth string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.challenges[domain] = &TLSALPNChallenge{
		Domain:  domain,
		KeyAuth: keyAuth,
	}
	p.logger.Debug("TLS-ALPN-01 challenge presented",
		zap.String("domain", domain),
	)
	return nil
}

// CleanUp is called when a challenge should be removed.
func (p *TLSALPN01Provider) CleanUp(domain, _, _ string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.challenges, domain)
	p.logger.Debug("TLS-ALPN-01 challenge cleaned up",
		zap.String("domain", domain),
	)
	return nil
}

// GetChallenge returns the challenge for a given domain.
func (p *TLSALPN01Provider) GetChallenge(domain string) (*TLSALPNChallenge, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	ch, ok := p.challenges[domain]
	return ch, ok
}

// DNS01Provider is a base implementation for DNS-01 challenges.
// Specific DNS providers should embed this and implement Present/CleanUp.
type DNS01Provider struct {
	mu         sync.RWMutex
	challenges map[string]*DNSChallenge
	logger     *zap.Logger
}

// DNSChallenge holds DNS-01 challenge data.
type DNSChallenge struct {
	Domain      string
	Token       string
	KeyAuth     string
	RecordName  string
	RecordValue string
}

// NewDNS01Provider creates a new base DNS-01 challenge provider.
func NewDNS01Provider(logger *zap.Logger) *DNS01Provider {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &DNS01Provider{
		challenges: make(map[string]*DNSChallenge),
		logger:     logger,
	}
}

// GetInfo returns the challenge info for a domain.
func (p *DNS01Provider) GetInfo(domain string) (*DNSChallenge, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	ch, ok := p.challenges[domain]
	return ch, ok
}

// SetChallenge stores a DNS challenge for later processing.
func (p *DNS01Provider) SetChallenge(domain string, challenge *DNSChallenge) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.challenges[domain] = challenge
}

// RemoveChallenge removes a DNS challenge.
func (p *DNS01Provider) RemoveChallenge(domain string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.challenges, domain)
}
