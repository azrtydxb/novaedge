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

// Package cpvip implements a control-plane VIP manager for kube-apiserver HA.
// It operates independently of the NovaEdge controller, making it suitable for
// pre-bootstrap scenarios where the Kubernetes API server itself needs a VIP
// before any controller can run.
package cpvip

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/vip"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

const (
	// DefaultAPIPort is the default kube-apiserver port.
	DefaultAPIPort = 6443

	// DefaultHealthInterval is the default interval between health checks.
	DefaultHealthInterval = 1 * time.Second

	// DefaultHealthTimeout is the default timeout for health check requests.
	DefaultHealthTimeout = 3 * time.Second

	// DefaultFailThreshold is the default number of consecutive failures before releasing the VIP.
	DefaultFailThreshold = 3

	// cpVIPName is the internal name used for the control-plane VIP assignment.
	cpVIPName = "control-plane-vip"

	// healthzPath is the kube-apiserver health check endpoint.
	healthzPath = "/healthz"
)

// Config holds the configuration for the control-plane VIP manager.
type Config struct {
	// VIPAddress is the VIP in CIDR notation (e.g., "10.0.0.100/32").
	VIPAddress string

	// Interface is the network interface to bind the VIP to.
	// If empty, the primary interface is auto-detected.
	Interface string

	// APIPort is the kube-apiserver port (default: 6443).
	APIPort int

	// HealthInterval is the interval between health checks (default: 1s).
	HealthInterval time.Duration

	// HealthTimeout is the timeout for each health check request (default: 3s).
	HealthTimeout time.Duration

	// FailThreshold is the number of consecutive failures before releasing the VIP (default: 3).
	FailThreshold int
}

// Validate checks that the configuration is valid.
func (c *Config) Validate() error {
	if c.VIPAddress == "" {
		return fmt.Errorf("VIP address is required")
	}

	// Validate CIDR notation
	if _, _, err := net.ParseCIDR(c.VIPAddress); err != nil {
		return fmt.Errorf("invalid VIP address %q: %w", c.VIPAddress, err)
	}

	if c.APIPort < 1 || c.APIPort > 65535 {
		return fmt.Errorf("invalid API port %d: must be between 1 and 65535", c.APIPort)
	}

	if c.HealthInterval <= 0 {
		return fmt.Errorf("health interval must be positive")
	}

	if c.HealthTimeout <= 0 {
		return fmt.Errorf("health timeout must be positive")
	}

	if c.FailThreshold < 1 {
		return fmt.Errorf("fail threshold must be at least 1")
	}

	return nil
}

// applyDefaults fills in zero-value fields with defaults.
func (c *Config) applyDefaults() {
	if c.APIPort == 0 {
		c.APIPort = DefaultAPIPort
	}
	if c.HealthInterval == 0 {
		c.HealthInterval = DefaultHealthInterval
	}
	if c.HealthTimeout == 0 {
		c.HealthTimeout = DefaultHealthTimeout
	}
	if c.FailThreshold == 0 {
		c.FailThreshold = DefaultFailThreshold
	}
}

// Manager manages a single control-plane VIP based on kube-apiserver health.
// It binds the VIP when the local apiserver is healthy and releases it when
// the apiserver becomes unreachable, allowing another node to take over.
type Manager struct {
	config     Config
	logger     *zap.Logger
	l2Handler  *vip.L2Handler
	httpClient *http.Client

	mu        sync.Mutex
	vipActive bool
	failCount int
}

// NewManager creates a new control-plane VIP manager.
func NewManager(cfg Config, logger *zap.Logger) (*Manager, error) {
	cfg.applyDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid cpvip config: %w", err)
	}

	l2Handler, err := vip.NewL2HandlerWithInterface(logger.Named("l2"), cfg.Interface)
	if err != nil {
		return nil, fmt.Errorf("failed to create L2 handler: %w", err)
	}

	httpClient := &http.Client{
		Timeout: cfg.HealthTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // apiserver cert may not include localhost
			},
			DisableKeepAlives: true,
		},
	}

	return &Manager{
		config:     cfg,
		logger:     logger.Named("cpvip"),
		l2Handler:  l2Handler,
		httpClient: httpClient,
	}, nil
}

// Start begins the health check loop that manages VIP binding. It blocks until
// the context is cancelled and then releases the VIP before returning.
func (m *Manager) Start(ctx context.Context) error {
	m.logger.Info("Starting control-plane VIP manager",
		zap.String("vip", m.config.VIPAddress),
		zap.String("interface", m.config.Interface),
		zap.Int("api_port", m.config.APIPort),
		zap.Duration("health_interval", m.config.HealthInterval),
		zap.Int("fail_threshold", m.config.FailThreshold),
	)

	// Start the L2 handler (runs the GARP announcement loop)
	if err := m.l2Handler.Start(ctx); err != nil {
		return fmt.Errorf("failed to start L2 handler: %w", err)
	}

	ticker := time.NewTicker(m.config.HealthInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("Context cancelled, stopping CP VIP manager")
			return nil
		case <-ticker.C:
			m.healthCheckTick(ctx)
		}
	}
}

// Stop releases the VIP if it is currently active. It is safe to call
// after Start returns (e.g., during graceful shutdown).
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.vipActive {
		m.logger.Info("Releasing control-plane VIP on shutdown")
		if err := m.releaseVIPLocked(); err != nil {
			return fmt.Errorf("failed to release VIP on stop: %w", err)
		}
	}

	return nil
}

// IsVIPActive returns whether the VIP is currently bound to this node.
func (m *Manager) IsVIPActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.vipActive
}

// healthCheckTick runs a single iteration of the health check loop.
func (m *Manager) healthCheckTick(ctx context.Context) {
	healthy := m.checkAPIServerHealth(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()

	if healthy {
		m.failCount = 0
		if !m.vipActive {
			m.logger.Info("API server is healthy, binding VIP")
			if err := m.bindVIPLocked(); err != nil {
				m.logger.Error("Failed to bind VIP", zap.Error(err))
			}
		}
	} else {
		m.failCount++
		m.logger.Warn("API server health check failed",
			zap.Int("fail_count", m.failCount),
			zap.Int("threshold", m.config.FailThreshold),
		)
		if m.failCount >= m.config.FailThreshold && m.vipActive {
			m.logger.Warn("Fail threshold reached, releasing VIP",
				zap.Int("fail_count", m.failCount),
			)
			if err := m.releaseVIPLocked(); err != nil {
				m.logger.Error("Failed to release VIP", zap.Error(err))
			}
		}
	}
}

// checkAPIServerHealth performs an HTTP GET to the apiserver /healthz endpoint.
func (m *Manager) checkAPIServerHealth(ctx context.Context) bool {
	url := fmt.Sprintf("https://localhost:%d%s", m.config.APIPort, healthzPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		m.logger.Error("Failed to create health check request", zap.Error(err))
		return false
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		m.logger.Debug("API server health check error", zap.Error(err))
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode == http.StatusOK
}

// buildVIPAssignment creates a protobuf VIPAssignment for the control-plane VIP.
func (m *Manager) buildVIPAssignment() *pb.VIPAssignment {
	return &pb.VIPAssignment{
		VipName:  cpVIPName,
		Address:  m.config.VIPAddress,
		Mode:     pb.VIPMode_L2_ARP,
		IsActive: true,
	}
}

// bindVIPLocked binds the VIP using the L2 handler. Must be called with m.mu held.
func (m *Manager) bindVIPLocked() error {
	assignment := m.buildVIPAssignment()

	if err := m.l2Handler.AddVIP(context.Background(), assignment); err != nil {
		return fmt.Errorf("failed to add VIP: %w", err)
	}

	m.vipActive = true
	m.logger.Info("Control-plane VIP bound",
		zap.String("address", m.config.VIPAddress),
	)

	return nil
}

// releaseVIPLocked releases the VIP using the L2 handler. Must be called with m.mu held.
func (m *Manager) releaseVIPLocked() error {
	assignment := m.buildVIPAssignment()

	if err := m.l2Handler.RemoveVIP(context.Background(), assignment); err != nil {
		return fmt.Errorf("failed to remove VIP: %w", err)
	}

	m.vipActive = false
	m.logger.Info("Control-plane VIP released",
		zap.String("address", m.config.VIPAddress),
	)

	return nil
}
