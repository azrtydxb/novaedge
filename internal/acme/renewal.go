package acme

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// RenewalManager handles automatic certificate renewal.
type RenewalManager struct {
	client      *Client
	logger      *zap.Logger
	interval    time.Duration
	renewBefore time.Duration

	mu       sync.Mutex
	stopChan chan struct{}
	running  bool

	// Callback function when a certificate is renewed
	OnRenewal func(domain string, cert *Certificate)
}

// NewRenewalManager creates a new renewal manager.
func NewRenewalManager(client *Client, logger *zap.Logger) *RenewalManager {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &RenewalManager{
		client:      client,
		logger:      logger,
		interval:    24 * time.Hour, // Check once per day by default
		renewBefore: time.Duration(client.config.RenewalDays) * 24 * time.Hour,
	}
}

// SetInterval sets the check interval.
func (m *RenewalManager) SetInterval(interval time.Duration) {
	m.interval = interval
}

// SetRenewBefore sets how long before expiry to renew.
func (m *RenewalManager) SetRenewBefore(d time.Duration) {
	m.renewBefore = d
}

// Start begins the automatic renewal process.
func (m *RenewalManager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return nil
	}
	m.running = true
	m.stopChan = make(chan struct{})
	m.mu.Unlock()

	m.logger.Info("Starting certificate renewal manager",
		zap.Duration("interval", m.interval),
		zap.Duration("renew_before", m.renewBefore),
	)

	// Run initial check
	if err := m.checkAndRenew(ctx); err != nil {
		m.logger.Warn("Initial renewal check failed", zap.Error(err))
	}

	// Start periodic check
	go m.run(ctx)

	return nil
}

// Stop stops the renewal manager.
func (m *RenewalManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return
	}

	close(m.stopChan)
	m.running = false
	m.logger.Info("Certificate renewal manager stopped")
}

// run is the main renewal loop.
func (m *RenewalManager) run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.Stop()
			return
		case <-m.stopChan:
			return
		case <-ticker.C:
			if err := m.checkAndRenew(ctx); err != nil {
				m.logger.Error("Renewal check failed", zap.Error(err))
			}
		}
	}
}

// checkAndRenew checks all certificates and renews those that need it.
func (m *RenewalManager) checkAndRenew(ctx context.Context) error {
	certs, err := m.client.GetCertificatesNeedingRenewal(ctx)
	if err != nil {
		return err
	}

	if len(certs) == 0 {
		m.logger.Debug("No certificates need renewal")
		return nil
	}

	m.logger.Info("Certificates need renewal",
		zap.Int("count", len(certs)),
	)

	for _, cert := range certs {
		if len(cert.Domains) == 0 {
			continue
		}

		domain := cert.Domains[0]
		m.logger.Info("Renewing certificate",
			zap.String("domain", domain),
			zap.Time("expires", cert.NotAfter),
		)

		newCert, err := m.client.RenewCertificate(ctx, domain)
		if err != nil {
			m.logger.Error("Failed to renew certificate",
				zap.String("domain", domain),
				zap.Error(err),
			)
			// Increment renewal failure metric
			renewalsTotal.WithLabelValues(domain, "failure").Inc()
			continue
		}

		// Increment renewal success metric
		renewalsTotal.WithLabelValues(domain, "success").Inc()

		m.logger.Info("Certificate renewed",
			zap.String("domain", domain),
			zap.Time("newExpiry", newCert.NotAfter),
		)

		// Notify callback if set
		if m.OnRenewal != nil {
			m.OnRenewal(domain, newCert)
		}
	}

	return nil
}

// RenewNow forces immediate renewal of a specific certificate.
func (m *RenewalManager) RenewNow(ctx context.Context, domain string) (*Certificate, error) {
	m.logger.Info("Forcing certificate renewal",
		zap.String("domain", domain),
	)

	cert, err := m.client.RenewCertificate(ctx, domain)
	if err != nil {
		renewalsTotal.WithLabelValues(domain, "failure").Inc()
		return nil, err
	}

	renewalsTotal.WithLabelValues(domain, "success").Inc()

	if m.OnRenewal != nil {
		m.OnRenewal(domain, cert)
	}

	return cert, nil
}

// GetNextRenewalTime returns when the next renewal should occur.
func (m *RenewalManager) GetNextRenewalTime(ctx context.Context) (time.Time, error) {
	certs, err := m.client.storage.ListCertificates(ctx)
	if err != nil {
		return time.Time{}, err
	}

	if len(certs) == 0 {
		return time.Time{}, nil
	}

	var earliest time.Time
	for _, cert := range certs {
		renewTime := cert.NotAfter.Add(-m.renewBefore)
		if earliest.IsZero() || renewTime.Before(earliest) {
			earliest = renewTime
		}
	}

	return earliest, nil
}
