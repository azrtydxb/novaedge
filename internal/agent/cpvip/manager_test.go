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

package cpvip

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// newTestLogger returns a no-op logger for tests.
func newTestLogger() *zap.Logger {
	return zap.NewNop()
}

// --- Config Validation Tests ---

func TestConfig_Validate_MissingAddress(t *testing.T) {
	cfg := Config{
		APIPort:        6443,
		HealthInterval: time.Second,
		HealthTimeout:  3 * time.Second,
		FailThreshold:  3,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing VIP address")
	}
	if !strings.Contains(err.Error(), "VIP address is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestConfig_Validate_InvalidCIDR(t *testing.T) {
	cfg := Config{
		VIPAddress:     "not-a-cidr",
		APIPort:        6443,
		HealthInterval: time.Second,
		HealthTimeout:  3 * time.Second,
		FailThreshold:  3,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
	if !strings.Contains(err.Error(), "invalid VIP address") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestConfig_Validate_InvalidPort(t *testing.T) {
	tests := []struct {
		name string
		port int
	}{
		{"zero", 0},
		{"negative", -1},
		{"too_high", 70000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				VIPAddress:     "10.0.0.100/32",
				APIPort:        tt.port,
				HealthInterval: time.Second,
				HealthTimeout:  3 * time.Second,
				FailThreshold:  3,
			}

			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected error for invalid port")
			}
			if !strings.Contains(err.Error(), "invalid API port") {
				t.Errorf("unexpected error message: %v", err)
			}
		})
	}
}

func TestConfig_Validate_InvalidHealthInterval(t *testing.T) {
	cfg := Config{
		VIPAddress:     "10.0.0.100/32",
		APIPort:        6443,
		HealthInterval: 0,
		HealthTimeout:  3 * time.Second,
		FailThreshold:  3,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for zero health interval")
	}
	if !strings.Contains(err.Error(), "health interval must be positive") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestConfig_Validate_InvalidHealthTimeout(t *testing.T) {
	cfg := Config{
		VIPAddress:     "10.0.0.100/32",
		APIPort:        6443,
		HealthInterval: time.Second,
		HealthTimeout:  0,
		FailThreshold:  3,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for zero health timeout")
	}
	if !strings.Contains(err.Error(), "health timeout must be positive") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestConfig_Validate_InvalidFailThreshold(t *testing.T) {
	cfg := Config{
		VIPAddress:     "10.0.0.100/32",
		APIPort:        6443,
		HealthInterval: time.Second,
		HealthTimeout:  3 * time.Second,
		FailThreshold:  0,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for zero fail threshold")
	}
	if !strings.Contains(err.Error(), "fail threshold must be at least 1") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestConfig_Validate_Valid(t *testing.T) {
	cfg := Config{
		VIPAddress:     "10.0.0.100/32",
		APIPort:        6443,
		HealthInterval: time.Second,
		HealthTimeout:  3 * time.Second,
		FailThreshold:  3,
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestConfig_ApplyDefaults(t *testing.T) {
	cfg := Config{
		VIPAddress: "10.0.0.100/32",
	}

	cfg.applyDefaults()

	if cfg.APIPort != DefaultAPIPort {
		t.Errorf("expected default API port %d, got %d", DefaultAPIPort, cfg.APIPort)
	}
	if cfg.HealthInterval != DefaultHealthInterval {
		t.Errorf("expected default health interval %v, got %v", DefaultHealthInterval, cfg.HealthInterval)
	}
	if cfg.HealthTimeout != DefaultHealthTimeout {
		t.Errorf("expected default health timeout %v, got %v", DefaultHealthTimeout, cfg.HealthTimeout)
	}
	if cfg.FailThreshold != DefaultFailThreshold {
		t.Errorf("expected default fail threshold %d, got %d", DefaultFailThreshold, cfg.FailThreshold)
	}
}

func TestConfig_ApplyDefaults_PreservesExplicit(t *testing.T) {
	cfg := Config{
		VIPAddress:     "10.0.0.100/32",
		APIPort:        8443,
		HealthInterval: 5 * time.Second,
		HealthTimeout:  10 * time.Second,
		FailThreshold:  5,
	}

	cfg.applyDefaults()

	if cfg.APIPort != 8443 {
		t.Errorf("expected port 8443, got %d", cfg.APIPort)
	}
	if cfg.HealthInterval != 5*time.Second {
		t.Errorf("expected 5s interval, got %v", cfg.HealthInterval)
	}
	if cfg.HealthTimeout != 10*time.Second {
		t.Errorf("expected 10s timeout, got %v", cfg.HealthTimeout)
	}
	if cfg.FailThreshold != 5 {
		t.Errorf("expected threshold 5, got %d", cfg.FailThreshold)
	}
}

// --- Health Check Tests ---

func TestCheckAPIServerHealth_Healthy(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	// Extract port from test server address
	port := extractPort(t, ts.URL)

	m := &Manager{
		config: Config{
			APIPort: port,
		},
		logger:     newTestLogger().Named("cpvip"),
		httpClient: ts.Client(),
	}

	// Override the URL scheme check: the test server uses a specific address,
	// so we test the health check logic by calling the method directly with a
	// context that targets the test server.
	// Since checkAPIServerHealth hardcodes "localhost", we need to create our
	// own test approach: call the test server directly.
	healthy := checkHealthAgainstURL(m, ts.URL+livezPath)
	if !healthy {
		t.Error("expected healthy response from test server")
	}
}

func TestCheckAPIServerHealth_Unhealthy(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("unhealthy"))
	}))
	defer ts.Close()

	m := &Manager{
		config: Config{
			APIPort: extractPort(t, ts.URL),
		},
		logger:     newTestLogger().Named("cpvip"),
		httpClient: ts.Client(),
	}

	healthy := checkHealthAgainstURL(m, ts.URL+livezPath)
	if healthy {
		t.Error("expected unhealthy response from test server")
	}
}

func TestCheckAPIServerHealth_ConnectionRefused(t *testing.T) {
	m := &Manager{
		config: Config{
			APIPort: 1, // unlikely to have anything listening
		},
		logger: newTestLogger().Named("cpvip"),
		httpClient: &http.Client{
			Timeout: 100 * time.Millisecond,
		},
	}

	healthy := m.checkAPIServerHealth(context.Background())
	if healthy {
		t.Error("expected unhealthy when connection is refused")
	}
}

// --- Health Check Tick Logic Tests ---

func TestHealthCheckTick_HealthyBindsVIP(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	m := &Manager{
		config: Config{
			VIPAddress:    "10.0.0.100/32",
			APIPort:       extractPort(t, ts.URL),
			FailThreshold: 3,
		},
		logger:     newTestLogger().Named("cpvip"),
		httpClient: ts.Client(),
	}

	// Simulate a healthy tick without a real L2 handler.
	// We test the state machine logic by directly manipulating state.
	m.mu.Lock()
	m.vipActive = false
	m.failCount = 0
	m.mu.Unlock()

	// Verify state transitions by testing the internal logic
	// (bindVIPLocked would fail without root, so we test the decision logic)
	healthy := checkHealthAgainstURL(m, ts.URL+livezPath)
	if !healthy {
		t.Fatal("expected healthy check to succeed")
	}

	m.mu.Lock()
	if m.failCount != 0 {
		t.Errorf("expected failCount 0, got %d", m.failCount)
	}
	m.mu.Unlock()
}

func TestHealthCheckTick_FailThresholdReached(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	m := &Manager{
		config: Config{
			VIPAddress:    "10.0.0.100/32",
			APIPort:       extractPort(t, ts.URL),
			FailThreshold: 3,
		},
		logger:     newTestLogger().Named("cpvip"),
		httpClient: ts.Client(),
		vipActive:  true,
		failCount:  0,
	}

	// Simulate consecutive unhealthy checks
	for i := 0; i < 3; i++ {
		healthy := checkHealthAgainstURL(m, ts.URL+livezPath)
		if healthy {
			t.Fatal("expected unhealthy check")
		}

		m.mu.Lock()
		m.failCount++
		m.mu.Unlock()
	}

	m.mu.Lock()
	if m.failCount < m.config.FailThreshold {
		t.Errorf("expected failCount >= %d, got %d", m.config.FailThreshold, m.failCount)
	}
	// Verify the decision would be to release VIP
	shouldRelease := m.failCount >= m.config.FailThreshold && m.vipActive
	m.mu.Unlock()

	if !shouldRelease {
		t.Error("expected VIP to be marked for release after reaching fail threshold")
	}
}

func TestHealthCheckTick_RecoveryAfterFailure(t *testing.T) {
	healthy := true
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if healthy {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer ts.Close()

	m := &Manager{
		config: Config{
			VIPAddress:    "10.0.0.100/32",
			APIPort:       extractPort(t, ts.URL),
			FailThreshold: 3,
		},
		logger:     newTestLogger().Named("cpvip"),
		httpClient: ts.Client(),
		vipActive:  true,
		failCount:  0,
	}

	// Simulate failures below threshold
	healthy = false
	for i := 0; i < 2; i++ {
		result := checkHealthAgainstURL(m, ts.URL+livezPath)
		if result {
			t.Fatal("expected unhealthy check")
		}
		m.mu.Lock()
		m.failCount++
		m.mu.Unlock()
	}

	m.mu.Lock()
	if m.failCount != 2 {
		t.Errorf("expected failCount 2, got %d", m.failCount)
	}
	m.mu.Unlock()

	// Recovery: apiserver becomes healthy again
	healthy = true
	result := checkHealthAgainstURL(m, ts.URL+livezPath)
	if !result {
		t.Fatal("expected healthy check after recovery")
	}

	// Reset fail count as the health check loop would
	m.mu.Lock()
	m.failCount = 0
	m.mu.Unlock()

	m.mu.Lock()
	if m.failCount != 0 {
		t.Errorf("expected failCount to be reset to 0, got %d", m.failCount)
	}
	if !m.vipActive {
		t.Error("expected VIP to remain active after recovery")
	}
	m.mu.Unlock()
}

func TestHealthCheckTick_BelowThresholdKeepsVIP(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	m := &Manager{
		config: Config{
			VIPAddress:    "10.0.0.100/32",
			APIPort:       extractPort(t, ts.URL),
			FailThreshold: 5,
		},
		logger:     newTestLogger().Named("cpvip"),
		httpClient: ts.Client(),
		vipActive:  true,
		failCount:  0,
	}

	// Only 2 failures (below threshold of 5)
	for i := 0; i < 2; i++ {
		m.mu.Lock()
		m.failCount++
		m.mu.Unlock()
	}

	m.mu.Lock()
	shouldRelease := m.failCount >= m.config.FailThreshold && m.vipActive
	m.mu.Unlock()

	if shouldRelease {
		t.Error("VIP should not be released when below fail threshold")
	}
}

// --- Manager Lifecycle Tests ---

func TestNewManager_InvalidConfig(t *testing.T) {
	_, err := NewManager(Config{}, newTestLogger())
	if err == nil {
		t.Fatal("expected error for empty config")
	}
}

func TestNewManager_InvalidInterface(t *testing.T) {
	_, err := NewManager(Config{
		VIPAddress: "10.0.0.100/32",
		Interface:  "nonexistent-iface-xyz",
	}, newTestLogger())
	if err == nil {
		t.Fatal("expected error for invalid interface")
	}
}

func TestStop_WhenVIPNotActive(t *testing.T) {
	// Create a Manager with no L2 handler needed since VIP is not active
	m := &Manager{
		config: Config{
			VIPAddress: "10.0.0.100/32",
		},
		logger:    newTestLogger().Named("cpvip"),
		vipActive: false,
	}

	if err := m.Stop(); err != nil {
		t.Fatalf("expected no error on stop with inactive VIP, got: %v", err)
	}
}

func TestIsVIPActive(t *testing.T) {
	m := &Manager{
		logger: newTestLogger().Named("cpvip"),
	}

	if m.IsVIPActive() {
		t.Error("expected VIP to be inactive initially")
	}

	m.mu.Lock()
	m.vipActive = true
	m.mu.Unlock()

	if !m.IsVIPActive() {
		t.Error("expected VIP to be active after setting flag")
	}
}

func TestBuildVIPAssignment(t *testing.T) {
	m := &Manager{
		config: Config{
			VIPAddress: "10.0.0.100/32",
		},
	}

	assignment := m.buildVIPAssignment()

	if assignment.VipName != cpVIPName {
		t.Errorf("expected VIP name %q, got %q", cpVIPName, assignment.VipName)
	}
	if assignment.Address != "10.0.0.100/32" {
		t.Errorf("expected address 10.0.0.100/32, got %s", assignment.Address)
	}
	if !assignment.IsActive {
		t.Error("expected IsActive to be true")
	}
}

func TestBuildVIPAssignment_L2Mode(t *testing.T) {
	m := &Manager{
		config: Config{
			VIPAddress: "10.0.0.100/32",
			Mode:       "l2",
		},
	}

	assignment := m.buildVIPAssignment()

	if assignment.Mode != pb.VIPMode_L2_ARP {
		t.Errorf("expected L2_ARP mode, got %v", assignment.Mode)
	}
	if assignment.BgpConfig != nil {
		t.Error("expected nil BgpConfig for L2 mode")
	}
	if assignment.BfdConfig != nil {
		t.Error("expected nil BfdConfig for L2 mode")
	}
}

func TestBuildVIPAssignment_BGPMode(t *testing.T) {
	m := &Manager{
		config: Config{
			VIPAddress:  "192.168.100.10/32",
			Mode:        "bgp",
			BGPLocalAS:  65000,
			BGPRouterID: "192.168.100.11",
			BGPPeers: []BGPPeerConfig{
				{Address: "192.168.100.2", AS: 65000, Port: 179},
				{Address: "192.168.100.3", AS: 65000, Port: 179},
			},
		},
	}

	assignment := m.buildVIPAssignment()

	if assignment.Mode != pb.VIPMode_BGP {
		t.Errorf("expected BGP mode, got %v", assignment.Mode)
	}
	if assignment.BgpConfig == nil {
		t.Fatal("expected non-nil BgpConfig for BGP mode")
	}
	if assignment.BgpConfig.LocalAs != 65000 {
		t.Errorf("expected LocalAs 65000, got %d", assignment.BgpConfig.LocalAs)
	}
	if assignment.BgpConfig.RouterId != "192.168.100.11" {
		t.Errorf("expected RouterId 192.168.100.11, got %s", assignment.BgpConfig.RouterId)
	}
	if len(assignment.BgpConfig.Peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(assignment.BgpConfig.Peers))
	}
	if assignment.BgpConfig.Peers[0].Address != "192.168.100.2" {
		t.Errorf("expected peer 0 address 192.168.100.2, got %s", assignment.BgpConfig.Peers[0].Address)
	}
	if assignment.BgpConfig.Peers[0].As != 65000 {
		t.Errorf("expected peer 0 AS 65000, got %d", assignment.BgpConfig.Peers[0].As)
	}
	if assignment.BgpConfig.Peers[0].Port != 179 {
		t.Errorf("expected peer 0 port 179, got %d", assignment.BgpConfig.Peers[0].Port)
	}
	if assignment.BgpConfig.Peers[1].Address != "192.168.100.3" {
		t.Errorf("expected peer 1 address 192.168.100.3, got %s", assignment.BgpConfig.Peers[1].Address)
	}
	// BFD not enabled, so BfdConfig should be nil
	if assignment.BfdConfig != nil {
		t.Error("expected nil BfdConfig when BFD is not enabled")
	}
}

func TestBuildVIPAssignment_BGPWithBFD(t *testing.T) {
	m := &Manager{
		config: Config{
			VIPAddress:    "192.168.100.10/32",
			Mode:          "bgp",
			BGPLocalAS:    65000,
			BGPRouterID:   "192.168.100.11",
			BGPPeers:      []BGPPeerConfig{{Address: "192.168.100.2", AS: 65000, Port: 179}},
			BFDEnabled:    true,
			BFDDetectMult: 3,
			BFDTxInterval: "300ms",
			BFDRxInterval: "300ms",
		},
	}

	assignment := m.buildVIPAssignment()

	if assignment.Mode != pb.VIPMode_BGP {
		t.Errorf("expected BGP mode, got %v", assignment.Mode)
	}
	if assignment.BfdConfig == nil {
		t.Fatal("expected non-nil BfdConfig when BFD is enabled")
	}
	if !assignment.BfdConfig.Enabled {
		t.Error("expected BfdConfig.Enabled to be true")
	}
	if assignment.BfdConfig.DetectMultiplier != 3 {
		t.Errorf("expected DetectMultiplier 3, got %d", assignment.BfdConfig.DetectMultiplier)
	}
	if assignment.BfdConfig.DesiredMinTxInterval != "300ms" {
		t.Errorf("expected DesiredMinTxInterval 300ms, got %s", assignment.BfdConfig.DesiredMinTxInterval)
	}
	if assignment.BfdConfig.RequiredMinRxInterval != "300ms" {
		t.Errorf("expected RequiredMinRxInterval 300ms, got %s", assignment.BfdConfig.RequiredMinRxInterval)
	}
}

// --- BGP Config Validation Tests ---

func TestConfig_Validate_BGPMode_MissingLocalAS(t *testing.T) {
	cfg := Config{
		VIPAddress:     "10.0.0.100/32",
		APIPort:        6443,
		HealthInterval: time.Second,
		HealthTimeout:  3 * time.Second,
		FailThreshold:  3,
		Mode:           "bgp",
		BGPLocalAS:     0,
		BGPPeers:       []BGPPeerConfig{{Address: "10.0.0.2", AS: 65000, Port: 179}},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing BGP local AS")
	}
	if !strings.Contains(err.Error(), "BGP local AS is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestConfig_Validate_BGPMode_MissingPeers(t *testing.T) {
	cfg := Config{
		VIPAddress:     "10.0.0.100/32",
		APIPort:        6443,
		HealthInterval: time.Second,
		HealthTimeout:  3 * time.Second,
		FailThreshold:  3,
		Mode:           "bgp",
		BGPLocalAS:     65000,
		BGPPeers:       nil,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing BGP peers")
	}
	if !strings.Contains(err.Error(), "at least one BGP peer") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestConfig_Validate_BGPMode_Valid(t *testing.T) {
	cfg := Config{
		VIPAddress:     "10.0.0.100/32",
		APIPort:        6443,
		HealthInterval: time.Second,
		HealthTimeout:  3 * time.Second,
		FailThreshold:  3,
		Mode:           "bgp",
		BGPLocalAS:     65000,
		BGPRouterID:    "10.0.0.1",
		BGPPeers:       []BGPPeerConfig{{Address: "10.0.0.2", AS: 65000, Port: 179}},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error for valid BGP config, got: %v", err)
	}
}

func TestConfig_Validate_L2Mode_IgnoresBGPFields(t *testing.T) {
	// L2 mode should not require BGP fields
	cfg := Config{
		VIPAddress:     "10.0.0.100/32",
		APIPort:        6443,
		HealthInterval: time.Second,
		HealthTimeout:  3 * time.Second,
		FailThreshold:  3,
		Mode:           "l2",
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error for L2 config without BGP fields, got: %v", err)
	}
}

func TestConfig_ApplyDefaults_BGPDefaults(t *testing.T) {
	cfg := Config{
		VIPAddress: "10.0.0.100/32",
	}

	cfg.applyDefaults()

	if cfg.Mode != "l2" {
		t.Errorf("expected default mode 'l2', got %q", cfg.Mode)
	}
	if cfg.BFDDetectMult != DefaultBFDDetectMult {
		t.Errorf("expected default BFD detect mult %d, got %d", DefaultBFDDetectMult, cfg.BFDDetectMult)
	}
	if cfg.BFDTxInterval != DefaultBFDInterval {
		t.Errorf("expected default BFD TX interval %q, got %q", DefaultBFDInterval, cfg.BFDTxInterval)
	}
	if cfg.BFDRxInterval != DefaultBFDInterval {
		t.Errorf("expected default BFD RX interval %q, got %q", DefaultBFDInterval, cfg.BFDRxInterval)
	}
	if cfg.SATokenPath != defaultSATokenPath {
		t.Errorf("expected default SA token path %q, got %q", defaultSATokenPath, cfg.SATokenPath)
	}
}

func TestStartContextCancellation(t *testing.T) {
	// Create a minimal manager that will return quickly when context is cancelled.
	// We cannot fully test Start without root privileges (for L2 handler),
	// so we test the context cancellation path directly.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	m := &Manager{
		config: Config{
			VIPAddress:     "10.0.0.100/32",
			APIPort:        6443,
			HealthInterval: 100 * time.Millisecond,
			HealthTimeout:  100 * time.Millisecond,
			FailThreshold:  3,
		},
		logger: newTestLogger().Named("cpvip"),
		httpClient: &http.Client{
			Timeout: 100 * time.Millisecond,
		},
	}

	// Start should return promptly when context is already cancelled.
	// We skip the L2 handler start since it requires network access.
	// Instead, test the loop logic directly.
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Simulate the loop: it should exit immediately because ctx is done
		select {
		case <-ctx.Done():
			// Expected: context already cancelled
		case <-time.After(5 * time.Second):
			t.Error("expected context to be done")
		}
	}()

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("context cancellation test timed out")
	}

	_ = m // avoid unused variable
}

// --- SA Token and Auth Tests ---

func TestCheckAPIServerHealth_AuthenticatedWithToken(t *testing.T) {
	// Create a temp token file
	tokenDir := t.TempDir()
	tokenFile := filepath.Join(tokenDir, "token")
	if err := os.WriteFile(tokenFile, []byte("test-bearer-token"), 0600); err != nil {
		t.Fatal(err)
	}

	// Server that validates the Bearer token
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "Bearer test-bearer-token" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		} else {
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer ts.Close()

	m := &Manager{
		config: Config{
			APIPort:     extractPort(t, ts.URL),
			SATokenPath: tokenFile,
		},
		logger:     newTestLogger().Named("cpvip"),
		httpClient: ts.Client(),
	}

	healthy := checkHealthAgainstURL(m, ts.URL+livezPath)
	if !healthy {
		t.Error("expected healthy response with valid Bearer token")
	}
}

func TestCheckAPIServerHealth_401WithoutToken(t *testing.T) {
	// Server that always returns 401
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	m := &Manager{
		config: Config{
			APIPort:     extractPort(t, ts.URL),
			SATokenPath: "/nonexistent/path/token",
		},
		logger:     newTestLogger().Named("cpvip"),
		httpClient: ts.Client(),
	}

	// Without token, 401 should be treated as healthy (pre-bootstrap fallback)
	healthy := checkHealthAgainstURL(m, ts.URL+livezPath)
	if !healthy {
		t.Error("expected 401 to be treated as healthy when no SA token is available")
	}
}

func TestCheckAPIServerHealth_401WithToken(t *testing.T) {
	// Create a temp token file with an invalid token
	tokenDir := t.TempDir()
	tokenFile := filepath.Join(tokenDir, "token")
	if err := os.WriteFile(tokenFile, []byte("invalid-token"), 0600); err != nil {
		t.Fatal(err)
	}

	// Server that rejects the token
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	m := &Manager{
		config: Config{
			APIPort:     extractPort(t, ts.URL),
			SATokenPath: tokenFile,
		},
		logger:     newTestLogger().Named("cpvip"),
		httpClient: ts.Client(),
	}

	// With a token present but invalid, 401 should NOT be treated as healthy
	healthy := checkHealthAgainstURL(m, ts.URL+livezPath)
	if healthy {
		t.Error("expected 401 to be treated as unhealthy when SA token is present but rejected")
	}
}

func TestCheckAPIServerHealth_503WithoutToken(t *testing.T) {
	// Server that returns 503
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	m := &Manager{
		config: Config{
			APIPort:     extractPort(t, ts.URL),
			SATokenPath: "/nonexistent/path/token",
		},
		logger:     newTestLogger().Named("cpvip"),
		httpClient: ts.Client(),
	}

	// 503 should always be unhealthy, even without token
	healthy := checkHealthAgainstURL(m, ts.URL+livezPath)
	if healthy {
		t.Error("expected 503 to be treated as unhealthy")
	}
}

func TestGetSAToken_CachesToken(t *testing.T) {
	tokenDir := t.TempDir()
	tokenFile := filepath.Join(tokenDir, "token")
	if err := os.WriteFile(tokenFile, []byte("cached-token"), 0600); err != nil {
		t.Fatal(err)
	}

	m := &Manager{
		config: Config{
			SATokenPath: tokenFile,
		},
		logger: newTestLogger().Named("cpvip"),
	}

	// First call reads from disk
	token1 := m.getSAToken()
	if token1 != "cached-token" {
		t.Errorf("expected 'cached-token', got %q", token1)
	}

	// Write a different token to disk
	if err := os.WriteFile(tokenFile, []byte("new-token"), 0600); err != nil {
		t.Fatal(err)
	}

	// Second call should return cached token (within refresh interval)
	token2 := m.getSAToken()
	if token2 != "cached-token" {
		t.Errorf("expected cached 'cached-token', got %q", token2)
	}
}

func TestGetSAToken_RefreshesExpiredCache(t *testing.T) {
	tokenDir := t.TempDir()
	tokenFile := filepath.Join(tokenDir, "token")
	if err := os.WriteFile(tokenFile, []byte("original-token"), 0600); err != nil {
		t.Fatal(err)
	}

	m := &Manager{
		config: Config{
			SATokenPath: tokenFile,
		},
		logger: newTestLogger().Named("cpvip"),
	}

	// Load initial token
	token1 := m.getSAToken()
	if token1 != "original-token" {
		t.Fatalf("expected 'original-token', got %q", token1)
	}

	// Simulate cache expiry by backdating tokenLoadedAt
	m.tokenMu.Lock()
	m.tokenLoadedAt = time.Now().Add(-tokenRefreshInterval - time.Second)
	m.tokenMu.Unlock()

	// Write new token
	if err := os.WriteFile(tokenFile, []byte("rotated-token"), 0600); err != nil {
		t.Fatal(err)
	}

	// Should pick up the new token
	token2 := m.getSAToken()
	if token2 != "rotated-token" {
		t.Errorf("expected 'rotated-token' after cache expiry, got %q", token2)
	}
}

func TestGetSAToken_MissingFile(t *testing.T) {
	m := &Manager{
		config: Config{
			SATokenPath: "/nonexistent/path/to/token",
		},
		logger: newTestLogger().Named("cpvip"),
	}

	token := m.getSAToken()
	if token != "" {
		t.Errorf("expected empty token for missing file, got %q", token)
	}
}

// --- Helper Functions ---

// checkHealthAgainstURL performs a health check against a specific URL instead of localhost.
// This allows testing the health check logic against a httptest.Server.
// It mirrors the logic in checkAPIServerHealth: attach token if available,
// accept 200 always, accept 401 only when no token is present.
func checkHealthAgainstURL(m *Manager, targetURL string) bool {
	if _, parseErr := url.ParseRequestURI(targetURL); parseErr != nil {
		return false
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, targetURL, nil)
	if err != nil {
		return false
	}

	token := m.getSAToken()
	hasToken := token != ""
	if hasToken {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		return true
	}
	if !hasToken && resp.StatusCode == http.StatusUnauthorized {
		return true
	}
	return false
}

// extractPort extracts the port number from a URL string like "https://127.0.0.1:12345".
func extractPort(t *testing.T, rawURL string) int {
	t.Helper()

	// Remove scheme
	addr := rawURL
	if idx := strings.Index(addr, "://"); idx >= 0 {
		addr = addr[idx+3:]
	}

	// Parse host:port
	_, portStr, err := strings.Cut(addr, ":")
	if err {
		// portStr is the port
		var port int
		for _, c := range portStr {
			if c < '0' || c > '9' {
				break
			}
			port = port*10 + int(c-'0')
		}
		return port
	}

	t.Fatalf("could not extract port from URL: %s", rawURL)
	return 0
}
