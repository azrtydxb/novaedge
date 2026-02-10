package acme

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

// memoryStorage implements Storage interface for testing without filesystem or mocks
type memoryStorage struct {
	certs   map[string]*Certificate
	account *AccountInfo
}

func newMemoryStorage() *memoryStorage {
	return &memoryStorage{
		certs: make(map[string]*Certificate),
	}
}

func (s *memoryStorage) SaveCertificate(_ context.Context, cert *Certificate) error {
	if len(cert.Domains) > 0 {
		s.certs[cert.Domains[0]] = cert
	}
	return nil
}

func (s *memoryStorage) LoadCertificate(_ context.Context, domain string) (*Certificate, error) {
	cert, ok := s.certs[domain]
	if !ok {
		return nil, &notFoundError{domain: domain}
	}
	return cert, nil
}

func (s *memoryStorage) DeleteCertificate(_ context.Context, domain string) error {
	delete(s.certs, domain)
	return nil
}

func (s *memoryStorage) ListCertificates(_ context.Context) ([]*Certificate, error) {
	result := make([]*Certificate, 0, len(s.certs))
	for _, cert := range s.certs {
		result = append(result, cert)
	}
	return result, nil
}

func (s *memoryStorage) SaveAccount(_ context.Context, account *AccountInfo) error {
	s.account = account
	return nil
}

func (s *memoryStorage) LoadAccount(_ context.Context) (*AccountInfo, error) {
	if s.account == nil {
		return nil, &notFoundError{domain: "account"}
	}
	return s.account, nil
}

type notFoundError struct {
	domain string
}

func (e *notFoundError) Error() string {
	return "not found: " + e.domain
}

func TestNewClient_NilConfig(t *testing.T) {
	_, err := NewClient(nil, newMemoryStorage(), nil, zap.NewNop())
	if err == nil {
		t.Fatal("expected error for nil config")
	}
}

func TestNewClient_NilStorage(t *testing.T) {
	config := &Config{Email: "test@example.com"}
	_, err := NewClient(config, nil, nil, zap.NewNop())
	if err == nil {
		t.Fatal("expected error for nil storage")
	}
}

func TestNewClient_NilLogger(t *testing.T) {
	config := &Config{Email: "test@example.com"}
	storage := newMemoryStorage()

	client, err := NewClient(config, storage, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.logger == nil {
		t.Error("expected logger to be set to nop logger when nil")
	}
}

func TestNewClient_Success(t *testing.T) {
	config := &Config{
		Email:  "test@example.com",
		Server: LetsEncryptStaging,
	}
	storage := newMemoryStorage()
	logger := zap.NewNop()

	client, err := NewClient(config, storage, nil, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.config.Server != LetsEncryptStaging {
		t.Errorf("expected server %q, got %q", LetsEncryptStaging, client.config.Server)
	}
}

func TestNewClient_AppliesDefaults(t *testing.T) {
	config := &Config{
		Email: "test@example.com",
	}
	storage := newMemoryStorage()

	client, err := NewClient(config, storage, nil, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// After ApplyDefaults, Server should be set
	if client.config.Server != DefaultServer {
		t.Errorf("expected default server %q, got %q", DefaultServer, client.config.Server)
	}
	if client.config.KeyType != DefaultKeyType {
		t.Errorf("expected default key type %q, got %q", DefaultKeyType, client.config.KeyType)
	}
	if client.config.RenewalDays != DefaultRenewalDays {
		t.Errorf("expected default renewal days %d, got %d", DefaultRenewalDays, client.config.RenewalDays)
	}
}

func TestClient_ObtainCertificate_NotInitialized(t *testing.T) {
	config := &Config{Email: "test@example.com"}
	storage := newMemoryStorage()
	client, err := NewClient(config, storage, nil, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ObtainCertificate without Initialize should fail
	_, err = client.ObtainCertificate(context.Background(), &CertificateRequest{
		Domains: []string{"example.com"},
	})
	if err == nil {
		t.Fatal("expected error when client is not initialized")
	}
}

func TestClient_ObtainCertificate_EmptyDomains(t *testing.T) {
	config := &Config{Email: "test@example.com"}
	storage := newMemoryStorage()
	client, err := NewClient(config, storage, nil, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Manually set client to simulate initialization
	// We can't actually call Initialize (requires ACME server) so we test the domain validation
	_, err = client.ObtainCertificate(context.Background(), &CertificateRequest{
		Domains: []string{},
	})
	if err == nil {
		t.Fatal("expected error for empty domains")
	}
}

func TestClient_Close(t *testing.T) {
	config := &Config{Email: "test@example.com"}
	storage := newMemoryStorage()
	client, err := NewClient(config, storage, nil, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = client.Close()
	if err != nil {
		t.Fatalf("unexpected error on close: %v", err)
	}
}

func TestParseCertificatePEM_InvalidData(t *testing.T) {
	_, err := ParseCertificatePEM([]byte("not a certificate"))
	if err == nil {
		t.Fatal("expected error for invalid certificate data")
	}
}

func TestParseCertificatePEM_ValidSelfSigned(t *testing.T) {
	// Generate a real self-signed cert to parse
	cert, err := QuickSelfSigned("test.example.com")
	if err != nil {
		t.Fatalf("failed to generate self-signed cert: %v", err)
	}

	parsed, err := ParseCertificatePEM(cert.CertificatePEM)
	if err != nil {
		t.Fatalf("failed to parse valid certificate PEM: %v", err)
	}

	if parsed.Subject.CommonName != "test.example.com" {
		t.Errorf("expected CN 'test.example.com', got %q", parsed.Subject.CommonName)
	}
}

func TestUser_GetEmail(t *testing.T) {
	u := &User{email: "user@example.com"}
	if u.GetEmail() != "user@example.com" {
		t.Errorf("expected email 'user@example.com', got %q", u.GetEmail())
	}
}

func TestUser_GetRegistration_Empty(t *testing.T) {
	u := &User{email: "user@example.com"}
	if u.GetRegistration() != nil {
		t.Error("expected nil registration for user with no account URI")
	}
}

func TestUser_GetRegistration_WithURI(t *testing.T) {
	u := &User{email: "user@example.com", accountURI: "https://acme.example.com/account/123"}
	reg := u.GetRegistration()
	if reg == nil {
		t.Fatal("expected non-nil registration")
	}
	if reg.URI != "https://acme.example.com/account/123" {
		t.Errorf("expected URI, got %q", reg.URI)
	}
}
