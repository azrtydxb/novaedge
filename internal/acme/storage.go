package acme

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// FileStorage implements Storage using the filesystem.
type FileStorage struct {
	basePath string
	logger   *zap.Logger
	mu       sync.RWMutex
}

// NewFileStorage creates a new file-based storage.
func NewFileStorage(basePath string, logger *zap.Logger) (*FileStorage, error) {
	if basePath == "" {
		basePath = DefaultStoragePath
	}

	// Ensure directory exists
	if err := os.MkdirAll(basePath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	return &FileStorage{
		basePath: basePath,
		logger:   logger,
	}, nil
}

// SaveCertificate stores a certificate.
func (s *FileStorage) SaveCertificate(ctx context.Context, cert *Certificate) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(cert.Domains) == 0 {
		return fmt.Errorf("certificate has no domains")
	}

	domain := cert.Domains[0]
	dir := s.certPath(domain)

	// Create directory
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create certificate directory: %w", err)
	}

	// Write certificate file
	certPath := filepath.Join(dir, "cert.pem")
	if err := os.WriteFile(certPath, cert.CertificatePEM, 0600); err != nil {
		return fmt.Errorf("failed to write certificate: %w", err)
	}

	// Write private key file
	keyPath := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(keyPath, cert.PrivateKeyPEM, 0600); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	// Write issuer certificate if present
	if len(cert.IssuerCertificatePEM) > 0 {
		issuerPath := filepath.Join(dir, "issuer.pem")
		if err := os.WriteFile(issuerPath, cert.IssuerCertificatePEM, 0600); err != nil {
			return fmt.Errorf("failed to write issuer certificate: %w", err)
		}
	}

	// Write metadata
	meta := &certMetadata{
		Domains:      cert.Domains,
		NotBefore:    cert.NotBefore.Format(time.RFC3339),
		NotAfter:     cert.NotAfter.Format(time.RFC3339),
		SerialNumber: cert.SerialNumber,
		Issuer:       cert.Issuer,
	}

	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	metaPath := filepath.Join(dir, "meta.json")
	if err := os.WriteFile(metaPath, metaBytes, 0600); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	s.logger.Debug("Certificate saved",
		zap.String("domain", domain),
		zap.String("path", dir),
	)

	return nil
}

// certMetadata is the structure stored in meta.json.
type certMetadata struct {
	Domains      []string `json:"domains"`
	NotBefore    string   `json:"notBefore"`
	NotAfter     string   `json:"notAfter"`
	SerialNumber string   `json:"serialNumber"`
	Issuer       string   `json:"issuer"`
}

// LoadCertificate retrieves a certificate by domain.
func (s *FileStorage) LoadCertificate(ctx context.Context, domain string) (*Certificate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := s.certPath(domain)

	// Read certificate
	certPath := filepath.Join(dir, "cert.pem")
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate: %w", err)
	}

	// Read private key
	keyPath := filepath.Join(dir, "key.pem")
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %w", err)
	}

	// Read issuer certificate (optional)
	issuerPath := filepath.Join(dir, "issuer.pem")
	issuerPEM, _ := os.ReadFile(issuerPath) // Ignore error, issuer is optional

	// Read metadata
	metaPath := filepath.Join(dir, "meta.json")
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	var meta certMetadata
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	cert := &Certificate{
		Domains:              meta.Domains,
		CertificatePEM:       certPEM,
		PrivateKeyPEM:        keyPEM,
		IssuerCertificatePEM: issuerPEM,
		SerialNumber:         meta.SerialNumber,
		Issuer:               meta.Issuer,
	}

	// Parse times
	if meta.NotBefore != "" {
		if t, err := time.Parse(time.RFC3339, meta.NotBefore); err == nil {
			cert.NotBefore = t
		}
	}
	if meta.NotAfter != "" {
		if t, err := time.Parse(time.RFC3339, meta.NotAfter); err == nil {
			cert.NotAfter = t
		}
	}

	return cert, nil
}

// DeleteCertificate removes a certificate.
func (s *FileStorage) DeleteCertificate(ctx context.Context, domain string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.certPath(domain)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to delete certificate: %w", err)
	}

	s.logger.Debug("Certificate deleted",
		zap.String("domain", domain),
	)

	return nil
}

// ListCertificates returns all stored certificates.
func (s *FileStorage) ListCertificates(ctx context.Context) ([]*Certificate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	certsDir := filepath.Join(s.basePath, "certs")
	entries, err := os.ReadDir(certsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list certificates: %w", err)
	}

	var certs []*Certificate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Reconstruct domain from directory name
		domain := strings.ReplaceAll(entry.Name(), "_", ".")
		domain = strings.ReplaceAll(domain, "wildcard", "*")

		// Need to release lock to call LoadCertificate
		s.mu.RUnlock()
		cert, err := s.LoadCertificate(ctx, domain)
		s.mu.RLock()

		if err != nil {
			s.logger.Warn("Failed to load certificate",
				zap.String("domain", domain),
				zap.Error(err),
			)
			continue
		}
		certs = append(certs, cert)
	}

	return certs, nil
}

// accountMetadata is the structure stored in account.json.
type accountMetadata struct {
	Email        string `json:"email"`
	URI          string `json:"uri"`
	Registration string `json:"registration"`
}

// SaveAccount stores ACME account information.
func (s *FileStorage) SaveAccount(ctx context.Context, account *AccountInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Join(s.basePath, "account")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create account directory: %w", err)
	}

	// Write private key
	keyPath := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(keyPath, account.PrivateKeyPEM, 0600); err != nil {
		return fmt.Errorf("failed to write account key: %w", err)
	}

	// Write account metadata
	meta := &accountMetadata{
		Email:        account.Email,
		URI:          account.URI,
		Registration: account.Registration.Format(time.RFC3339),
	}

	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal account metadata: %w", err)
	}

	metaPath := filepath.Join(dir, "account.json")
	if err := os.WriteFile(metaPath, metaBytes, 0600); err != nil {
		return fmt.Errorf("failed to write account metadata: %w", err)
	}

	s.logger.Debug("Account saved",
		zap.String("email", account.Email),
	)

	return nil
}

// LoadAccount retrieves ACME account information.
func (s *FileStorage) LoadAccount(ctx context.Context) (*AccountInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := filepath.Join(s.basePath, "account")

	// Read private key
	keyPath := filepath.Join(dir, "key.pem")
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read account key: %w", err)
	}

	// Read account metadata
	metaPath := filepath.Join(dir, "account.json")
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read account metadata: %w", err)
	}

	var meta accountMetadata
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, fmt.Errorf("failed to parse account metadata: %w", err)
	}

	account := &AccountInfo{
		Email:         meta.Email,
		URI:           meta.URI,
		PrivateKeyPEM: keyPEM,
	}

	if meta.Registration != "" {
		if t, err := time.Parse(time.RFC3339, meta.Registration); err == nil {
			account.Registration = t
		}
	}

	return account, nil
}

// certPath returns the storage path for a certificate.
func (s *FileStorage) certPath(domain string) string {
	// Replace dots with underscores for filesystem safety
	safeName := strings.ReplaceAll(domain, ".", "_")
	// Replace wildcards
	safeName = strings.ReplaceAll(safeName, "*", "wildcard")
	return filepath.Join(s.basePath, "certs", safeName)
}
