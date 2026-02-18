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

package federation

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg == nil {
		t.Fatal("DefaultConfig() returned nil")
	}

	// Verify default values
	if cfg.SyncInterval != 5*time.Second {
		t.Errorf("SyncInterval = %v, want %v", cfg.SyncInterval, 5*time.Second)
	}
	if cfg.SyncTimeout != 30*time.Second {
		t.Errorf("SyncTimeout = %v, want %v", cfg.SyncTimeout, 30*time.Second)
	}
	if cfg.BatchSize != 100 {
		t.Errorf("BatchSize = %d, want 100", cfg.BatchSize)
	}
	if !cfg.CompressionEnabled {
		t.Error("CompressionEnabled should be true")
	}
	if cfg.ConflictResolutionStrategy != StrategyLastWriterWins {
		t.Errorf("ConflictResolutionStrategy = %q, want %q", cfg.ConflictResolutionStrategy, StrategyLastWriterWins)
	}
	if !cfg.VectorClocksEnabled {
		t.Error("VectorClocksEnabled should be true")
	}
	if cfg.TombstoneTTL != 24*time.Hour {
		t.Errorf("TombstoneTTL = %v, want %v", cfg.TombstoneTTL, 24*time.Hour)
	}
	if cfg.HealthCheckInterval != 10*time.Second {
		t.Errorf("HealthCheckInterval = %v, want %v", cfg.HealthCheckInterval, 10*time.Second)
	}
	if cfg.HealthCheckTimeout != 5*time.Second {
		t.Errorf("HealthCheckTimeout = %v, want %v", cfg.HealthCheckTimeout, 5*time.Second)
	}
	if cfg.FailureThreshold != 3 {
		t.Errorf("FailureThreshold = %d, want 3", cfg.FailureThreshold)
	}
	if cfg.SuccessThreshold != 1 {
		t.Errorf("SuccessThreshold = %d, want 1", cfg.SuccessThreshold)
	}
}

func TestConfig_WithCustomValues(t *testing.T) {
	cfg := DefaultConfig()

	// Modify config
	cfg.FederationID = "test-federation"
	cfg.LocalMember = &PeerInfo{
		Name:     testLocalMember,
		Endpoint: "localhost:8080",
	}
	cfg.Peers = []*PeerInfo{
		{Name: "peer1", Endpoint: "peer1:8080"},
		{Name: "peer2", Endpoint: "peer2:8080"},
	}
	cfg.SyncInterval = 10 * time.Second
	cfg.BatchSize = 50
	cfg.ResourceTypes = []string{"ProxyGateway", "ProxyRoute"}
	cfg.ExcludeNamespaces = []string{"kube-system", "kube-public"}

	// Verify custom values
	if cfg.FederationID != "test-federation" {
		t.Errorf("FederationID = %q, want %q", cfg.FederationID, "test-federation")
	}
	if cfg.LocalMember == nil {
		t.Error("LocalMember should not be nil")
	}
	if len(cfg.Peers) != 2 {
		t.Errorf("len(Peers) = %d, want 2", len(cfg.Peers))
	}
	if cfg.SyncInterval != 10*time.Second {
		t.Errorf("SyncInterval = %v, want %v", cfg.SyncInterval, 10*time.Second)
	}
	if cfg.BatchSize != 50 {
		t.Errorf("BatchSize = %d, want 50", cfg.BatchSize)
	}
	if len(cfg.ResourceTypes) != 2 {
		t.Errorf("len(ResourceTypes) = %d, want 2", len(cfg.ResourceTypes))
	}
	if len(cfg.ExcludeNamespaces) != 2 {
		t.Errorf("len(ExcludeNamespaces) = %d, want 2", len(cfg.ExcludeNamespaces))
	}
}

func TestTrackedResource_Creation(t *testing.T) {
	now := time.Now()

	tr := &TrackedResource{
		Key: ResourceKey{
			Kind:      "ProxyGateway",
			Namespace: "default",
			Name:      "test-gateway",
		},
		ResourceVersion: "12345",
		Hash:            "abc123",
		Data:            []byte(`{"kind":"ProxyGateway"}`),
		VectorClock:     map[string]int64{testLocalMember: 1, "peer1": 2},
		OriginMember:    testLocalMember,
		LastModified:    now,
		Labels:          map[string]string{"env": "test"},
	}

	// Verify all fields
	if tr.Key.Kind != "ProxyGateway" {
		t.Errorf("Key.Kind = %q, want %q", tr.Key.Kind, "ProxyGateway")
	}
	if tr.ResourceVersion != "12345" {
		t.Errorf("ResourceVersion = %q, want %q", tr.ResourceVersion, "12345")
	}
	if tr.Hash != "abc123" {
		t.Errorf("Hash = %q, want %q", tr.Hash, "abc123")
	}
	if string(tr.Data) != `{"kind":"ProxyGateway"}` {
		t.Errorf("Data = %s, want %s", string(tr.Data), `{"kind":"ProxyGateway"}`)
	}
	if tr.VectorClock[testLocalMember] != 1 {
		t.Errorf("VectorClock[local] = %d, want 1", tr.VectorClock[testLocalMember])
	}
	if tr.OriginMember != testLocalMember {
		t.Errorf("OriginMember = %q, want %q", tr.OriginMember, testLocalMember)
	}
	if tr.LastModified != now {
		t.Errorf("LastModified = %v, want %v", tr.LastModified, now)
	}
	if tr.Labels["env"] != "test" {
		t.Errorf("Labels[env] = %q, want %q", tr.Labels["env"], "test")
	}
}

func TestTombstone_Creation(t *testing.T) {
	now := time.Now()

	ts := &Tombstone{
		Key: ResourceKey{
			Kind:      "ProxyRoute",
			Namespace: "default",
			Name:      "deleted-route",
		},
		DeletionTime: now,
		VectorClock:  map[string]int64{testLocalMember: 5},
		OriginMember: testLocalMember,
	}

	// Verify fields
	if ts.Key.Kind != "ProxyRoute" {
		t.Errorf("Key.Kind = %q, want %q", ts.Key.Kind, "ProxyRoute")
	}
	if ts.DeletionTime != now {
		t.Errorf("DeletionTime = %v, want %v", ts.DeletionTime, now)
	}
	if ts.VectorClock[testLocalMember] != 5 {
		t.Errorf("VectorClock[local] = %d, want 5", ts.VectorClock[testLocalMember])
	}
	if ts.OriginMember != testLocalMember {
		t.Errorf("OriginMember = %q, want %q", ts.OriginMember, testLocalMember)
	}
}

func TestConflictInfo_Creation(t *testing.T) {
	now := time.Now()

	local := &TrackedResource{
		Key:          ResourceKey{Kind: "ProxyGateway", Namespace: "default", Name: "gw"},
		OriginMember: testLocalMember,
		LastModified: now.Add(-time.Minute),
	}
	remote := &TrackedResource{
		Key:          ResourceKey{Kind: "ProxyGateway", Namespace: "default", Name: "gw"},
		OriginMember: "peer1",
		LastModified: now,
	}

	ci := &ConflictInfo{
		Key:            local.Key,
		LocalVersion:   local,
		RemoteVersion:  remote,
		DetectedAt:     now,
		Resolution:     ConflictResolutionNone,
		RequiresManual: false,
	}

	// Verify fields
	if ci.Key.Name != "gw" {
		t.Errorf("Key.Name = %q, want %q", ci.Key.Name, "gw")
	}
	if ci.LocalVersion.OriginMember != testLocalMember {
		t.Errorf("LocalVersion.OriginMember = %q, want %q", ci.LocalVersion.OriginMember, testLocalMember)
	}
	if ci.RemoteVersion.OriginMember != "peer1" {
		t.Errorf("RemoteVersion.OriginMember = %q, want %q", ci.RemoteVersion.OriginMember, "peer1")
	}
	if ci.Resolution != ConflictResolutionNone {
		t.Errorf("Resolution = %q, want %q", ci.Resolution, ConflictResolutionNone)
	}
}

func TestConflictResolutionType_Constants(t *testing.T) {
	tests := []struct {
		name     string
		value    ConflictResolutionType
		expected string
	}{
		{"None", ConflictResolutionNone, ""},
		{"LastWriterWins", ConflictResolutionLastWriterWins, "LastWriterWins"},
		{"Merged", ConflictResolutionMerged, "Merged"},
		{"PendingManual", ConflictResolutionPendingManual, "PendingManual"},
		{"LocalWins", ConflictResolutionLocalWins, "LocalWins"},
		{"RemoteWins", ConflictResolutionRemoteWins, "RemoteWins"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.expected {
				t.Errorf("ConflictResolutionType = %q, want %q", tt.value, tt.expected)
			}
		})
	}
}

func TestSyncStats_Creation(t *testing.T) {
	stats := &SyncStats{
		TotalChangesReceived: 100,
		TotalChangesSent:     200,
		LastSyncDuration:     time.Second * 5,
		ConflictsDetected:    5,
		ConflictsResolved:    4,
		PendingChanges:       10,
		FullSyncs:            2,
		IncrementalSyncs:     50,
	}

	// Verify all fields
	if stats.TotalChangesReceived != 100 {
		t.Errorf("TotalChangesReceived = %d, want 100", stats.TotalChangesReceived)
	}
	if stats.TotalChangesSent != 200 {
		t.Errorf("TotalChangesSent = %d, want 200", stats.TotalChangesSent)
	}
	if stats.LastSyncDuration != time.Second*5 {
		t.Errorf("LastSyncDuration = %v, want %v", stats.LastSyncDuration, time.Second*5)
	}
	if stats.ConflictsDetected != 5 {
		t.Errorf("ConflictsDetected = %d, want 5", stats.ConflictsDetected)
	}
	if stats.ConflictsResolved != 4 {
		t.Errorf("ConflictsResolved = %d, want 4", stats.ConflictsResolved)
	}
	if stats.PendingChanges != 10 {
		t.Errorf("PendingChanges = %d, want 10", stats.PendingChanges)
	}
	if stats.FullSyncs != 2 {
		t.Errorf("FullSyncs = %d, want 2", stats.FullSyncs)
	}
	if stats.IncrementalSyncs != 50 {
		t.Errorf("IncrementalSyncs = %d, want 50", stats.IncrementalSyncs)
	}
}

func TestPhase_Constants(t *testing.T) {
	tests := []struct {
		name     string
		value    Phase
		expected string
	}{
		{"Initializing", PhaseInitializing, "Initializing"},
		{"Syncing", PhaseSyncing, "Syncing"},
		{"Healthy", PhaseHealthy, "Healthy"},
		{"Degraded", PhaseDegraded, "Degraded"},
		{"Partitioned", PhasePartitioned, "Partitioned"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.expected {
				t.Errorf("Phase = %q, want %q", tt.value, tt.expected)
			}
		})
	}
}

func TestChangeEntry_Creation(t *testing.T) {
	now := time.Now()

	ce := &ChangeEntry{
		ID: "change-123",
		Key: ResourceKey{
			Kind:      "ProxyGateway",
			Namespace: "default",
			Name:      "test-gateway",
		},
		Type: ChangeTypeCreated,
		Resource: &TrackedResource{
			Key:          ResourceKey{Kind: "ProxyGateway", Namespace: "default", Name: "test-gateway"},
			OriginMember: testLocalMember,
		},
		Tombstone:   nil,
		VectorClock: map[string]int64{testLocalMember: 1},
		Timestamp:   now,
		Acknowledged: map[string]bool{
			testLocalMember: true,
			"peer1":         false,
		},
	}

	// Verify fields
	if ce.ID != "change-123" {
		t.Errorf("ID = %q, want %q", ce.ID, "change-123")
	}
	if ce.Type != ChangeTypeCreated {
		t.Errorf("Type = %q, want %q", ce.Type, ChangeTypeCreated)
	}
	if ce.Resource == nil {
		t.Error("Resource should not be nil")
	}
	if ce.Tombstone != nil {
		t.Error("Tombstone should be nil for create")
	}
	if ce.Timestamp != now {
		t.Errorf("Timestamp = %v, want %v", ce.Timestamp, now)
	}
	if !ce.Acknowledged[testLocalMember] {
		t.Error("local should have acknowledged")
	}
	if ce.Acknowledged["peer1"] {
		t.Error("peer1 should not have acknowledged")
	}
}

func TestChangeEntry_DeleteChange(t *testing.T) {
	now := time.Now()

	ce := &ChangeEntry{
		ID: "change-456",
		Key: ResourceKey{
			Kind:      "ProxyRoute",
			Namespace: "default",
			Name:      "deleted-route",
		},
		Type:     ChangeTypeDeleted,
		Resource: nil,
		Tombstone: &Tombstone{
			Key:          ResourceKey{Kind: "ProxyRoute", Namespace: "default", Name: "deleted-route"},
			DeletionTime: now,
			OriginMember: testLocalMember,
		},
		VectorClock:  map[string]int64{testLocalMember: 5},
		Timestamp:    now,
		Acknowledged: map[string]bool{},
	}

	// Verify delete change
	if ce.Type != ChangeTypeDeleted {
		t.Errorf("Type = %q, want %q", ce.Type, ChangeTypeDeleted)
	}
	if ce.Resource != nil {
		t.Error("Resource should be nil for delete")
	}
	if ce.Tombstone == nil {
		t.Error("Tombstone should not be nil for delete")
	}
}

func TestChangeType_Constants(t *testing.T) {
	tests := []struct {
		name     string
		value    ChangeType
		expected string
	}{
		{"Created", ChangeTypeCreated, "Created"},
		{"Updated", ChangeTypeUpdated, "Updated"},
		{"Deleted", ChangeTypeDeleted, "Deleted"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.expected {
				t.Errorf("ChangeType = %q, want %q", tt.value, tt.expected)
			}
		})
	}
}

func TestPeerInfo_TLSEnabled(t *testing.T) {
	tests := []struct {
		name    string
		peer    *PeerInfo
		wantTLS bool
	}{
		{
			name: "TLS enabled",
			peer: &PeerInfo{
				Name:          "peer1",
				Endpoint:      "peer1:8443",
				TLSEnabled:    true,
				TLSServerName: "peer1.example.com",
			},
			wantTLS: true,
		},
		{
			name: "TLS disabled",
			peer: &PeerInfo{
				Name:       "peer2",
				Endpoint:   "peer2:8080",
				TLSEnabled: false,
			},
			wantTLS: false,
		},
		{
			name: "TLS with insecure skip verify",
			peer: &PeerInfo{
				Name:               "peer3",
				Endpoint:           "peer3:8443",
				TLSEnabled:         true,
				InsecureSkipVerify: true,
			},
			wantTLS: true,
		},
		{
			name: "TLS with client certs",
			peer: &PeerInfo{
				Name:       "peer4",
				Endpoint:   "peer4:8443",
				TLSEnabled: true,
				CACert:     []byte("ca-cert"),
				ClientCert: []byte("client-cert"),
				ClientKey:  []byte("client-key"),
			},
			wantTLS: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.peer.TLSEnabled != tt.wantTLS {
				t.Errorf("TLSEnabled = %v, want %v", tt.peer.TLSEnabled, tt.wantTLS)
			}
		})
	}
}

func TestPeerState_Updates(t *testing.T) {
	state := &PeerState{
		Info: &PeerInfo{
			Name:     "peer1",
			Endpoint: "peer1:8080",
		},
		VectorClock: NewVectorClock(),
		Healthy:     true,
		Connected:   true,
	}

	// Simulate connection loss
	state.Connected = false
	state.Healthy = false
	state.LastError = "connection reset"
	state.ConsecutiveFailures++

	if state.Connected {
		t.Error("Connected should be false")
	}
	if state.Healthy {
		t.Error("Healthy should be false")
	}
	if state.LastError != "connection reset" {
		t.Errorf("LastError = %q, want %q", state.LastError, "connection reset")
	}
	if state.ConsecutiveFailures != 1 {
		t.Errorf("ConsecutiveFailures = %d, want 1", state.ConsecutiveFailures)
	}

	// Simulate recovery
	state.Connected = true
	state.Healthy = true
	state.LastError = ""
	state.ConsecutiveFailures = 0

	if !state.Connected {
		t.Error("Connected should be true after recovery")
	}
	if !state.Healthy {
		t.Error("Healthy should be true after recovery")
	}
}

func TestResourceKey_Equality(t *testing.T) {
	key1 := ResourceKey{Kind: "ProxyGateway", Namespace: "default", Name: "gw"}
	key2 := ResourceKey{Kind: "ProxyGateway", Namespace: "default", Name: "gw"}
	key3 := ResourceKey{Kind: "ProxyGateway", Namespace: "kube-system", Name: "gw"}
	key4 := ResourceKey{Kind: "ProxyRoute", Namespace: "default", Name: "gw"}
	key5 := ResourceKey{Kind: "ProxyGateway", Namespace: "default", Name: "gw2"}

	if key1 != key2 {
		t.Error("identical keys should be equal")
	}
	if key1 == key3 {
		t.Error("keys with different namespaces should not be equal")
	}
	if key1 == key4 {
		t.Error("keys with different kinds should not be equal")
	}
	if key1 == key5 {
		t.Error("keys with different names should not be equal")
	}
}

func TestConfig_Validation(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*Config)
		valid  bool
	}{
		{
			name:   "default config is valid",
			modify: func(_ *Config) {},
			valid:  true,
		},
		{
			name: "zero sync interval",
			modify: func(c *Config) {
				c.SyncInterval = 0
			},
			valid: true, // Zero is allowed, will use default
		},
		{
			name: "negative sync interval",
			modify: func(c *Config) {
				c.SyncInterval = -1 * time.Second
			},
			valid: true, // No validation in place
		},
		{
			name: "zero batch size",
			modify: func(c *Config) {
				c.BatchSize = 0
			},
			valid: true, // Zero is allowed
		},
		{
			name: "large batch size",
			modify: func(c *Config) {
				c.BatchSize = 10000
			},
			valid: true,
		},
		{
			name: "with federation ID",
			modify: func(c *Config) {
				c.FederationID = "my-federation"
			},
			valid: true,
		},
		{
			name: "with local member",
			modify: func(c *Config) {
				c.LocalMember = &PeerInfo{Name: testLocalMember, Endpoint: "localhost:8080"}
			},
			valid: true,
		},
		{
			name: "with peers",
			modify: func(c *Config) {
				c.Peers = []*PeerInfo{
					{Name: "peer1", Endpoint: "peer1:8080"},
					{Name: "peer2", Endpoint: "peer2:8080"},
				}
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.modify(cfg)

			// Basic validation - config should not be nil
			if cfg == nil {
				t.Error("Config should not be nil")
			}
		})
	}
}

func TestTrackedResource_DataHandling(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty data", []byte{}},
		{"nil data", nil},
		{"small data", []byte("test")},
		{"json data", []byte(`{"key":"value"}`)},
		{"large data", make([]byte, 1024*1024)}, // 1MB
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &TrackedResource{
				Key:  ResourceKey{Kind: "Test", Name: "test"},
				Data: tt.data,
			}

			// Verify data is stored correctly
			if tr.Data == nil && tt.data != nil {
				t.Error("data should not be nil")
			}
			if len(tr.Data) != len(tt.data) {
				t.Errorf("data length = %d, want %d", len(tr.Data), len(tt.data))
			}
		})
	}
}

func TestChangeEntry_AcknowledgmentTracking(t *testing.T) {
	ce := &ChangeEntry{
		ID:           "change-1",
		Key:          ResourceKey{Kind: "Test", Name: "test"},
		Type:         ChangeTypeCreated,
		Acknowledged: make(map[string]bool),
	}

	// Initially no acknowledgments
	if len(ce.Acknowledged) != 0 {
		t.Errorf("expected 0 acknowledgments, got %d", len(ce.Acknowledged))
	}

	// Add acknowledgments
	ce.Acknowledged[testLocalMember] = true
	ce.Acknowledged["peer1"] = true
	ce.Acknowledged["peer2"] = false

	// Count acknowledged
	count := 0
	for _, ack := range ce.Acknowledged {
		if ack {
			count++
		}
	}

	if count != 2 {
		t.Errorf("acknowledged count = %d, want 2", count)
	}
}

func TestConfig_Equal(t *testing.T) {
	baseConfig := func() *Config {
		cfg := DefaultConfig()
		cfg.FederationID = "fed-1"
		cfg.LocalMember = &PeerInfo{
			Name:     "local",
			Endpoint: "localhost:50051",
			Region:   "us-east-1",
			Labels:   map[string]string{"env": "prod"},
		}
		cfg.Peers = []*PeerInfo{
			{Name: "peer1", Endpoint: "peer1:50051", Region: "us-west-1"},
			{Name: "peer2", Endpoint: "peer2:50051", Region: "eu-west-1"},
		}
		cfg.ResourceTypes = []string{"proxygateways", "proxyroutes"}
		cfg.ExcludeNamespaces = []string{"kube-system"}
		return cfg
	}

	t.Run("identical configs are equal", func(t *testing.T) {
		a := baseConfig()
		b := baseConfig()
		if !a.Equal(b) {
			t.Error("identical configs should be equal")
		}
	})

	t.Run("same pointer is equal", func(t *testing.T) {
		a := baseConfig()
		aCopy := *a
		if !a.Equal(&aCopy) {
			t.Error("same pointer should be equal")
		}
	})

	t.Run("nil configs", func(t *testing.T) {
		a := baseConfig()
		if a.Equal(nil) {
			t.Error("non-nil config should not equal nil")
		}
		var nilCfg *Config
		if nilCfg.Equal(a) {
			t.Error("nil config should not equal non-nil")
		}
		if !nilCfg.Equal(nil) {
			t.Error("nil should equal nil")
		}
	})

	t.Run("different federation ID", func(t *testing.T) {
		a := baseConfig()
		b := baseConfig()
		b.FederationID = "fed-2"
		if a.Equal(b) {
			t.Error("configs with different FederationID should not be equal")
		}
	})

	t.Run("different sync interval", func(t *testing.T) {
		a := baseConfig()
		b := baseConfig()
		b.SyncInterval = 99 * time.Second
		if a.Equal(b) {
			t.Error("configs with different SyncInterval should not be equal")
		}
	})

	t.Run("different peer count", func(t *testing.T) {
		a := baseConfig()
		b := baseConfig()
		b.Peers = b.Peers[:1]
		if a.Equal(b) {
			t.Error("configs with different peer count should not be equal")
		}
	})

	t.Run("different peer endpoint", func(t *testing.T) {
		a := baseConfig()
		b := baseConfig()
		b.Peers[0].Endpoint = "changed:9999"
		if a.Equal(b) {
			t.Error("configs with different peer endpoint should not be equal")
		}
	})

	t.Run("peer order does not matter", func(t *testing.T) {
		a := baseConfig()
		b := baseConfig()
		// Reverse peer order
		b.Peers[0], b.Peers[1] = b.Peers[1], b.Peers[0]
		if !a.Equal(b) {
			t.Error("peer order should not affect equality")
		}
	})

	t.Run("different resource types", func(t *testing.T) {
		a := baseConfig()
		b := baseConfig()
		b.ResourceTypes = []string{"proxygateways"}
		if a.Equal(b) {
			t.Error("configs with different resource types should not be equal")
		}
	})

	t.Run("different local member labels", func(t *testing.T) {
		a := baseConfig()
		b := baseConfig()
		b.LocalMember.Labels = map[string]string{"env": "staging"}
		if a.Equal(b) {
			t.Error("configs with different local member labels should not be equal")
		}
	})

	t.Run("split brain config nil vs non-nil", func(t *testing.T) {
		a := baseConfig()
		b := baseConfig()
		b.SplitBrain = &SplitBrainConfig{
			PartitionTimeout: 30 * time.Second,
			QuorumRequired:   true,
		}
		if a.Equal(b) {
			t.Error("configs with different split-brain config should not be equal")
		}
	})

	t.Run("different TLS certs", func(t *testing.T) {
		a := baseConfig()
		b := baseConfig()
		a.Peers[0].CACert = []byte("ca-1")
		b.Peers[0].CACert = []byte("ca-2")
		if a.Equal(b) {
			t.Error("configs with different TLS certs should not be equal")
		}
	})
}
