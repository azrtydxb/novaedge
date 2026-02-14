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

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

const (
	testFederationID = "test-federation"
	testLocalMember  = "local"
)

func TestNewManager(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()
	config.FederationID = testFederationID

	manager := NewManager(config, logger)
	if manager == nil {
		t.Fatal("expected manager, got nil")
	}

	if manager.config.FederationID != testFederationID {
		t.Errorf("FederationID = %v, want test-federation", manager.config.FederationID)
	}

	if manager.clients == nil {
		t.Error("clients map should be initialized")
	}
}

func TestNewManagerFromCRD(t *testing.T) {
	logger := zap.NewNop()

	federation := &novaedgev1alpha1.NovaEdgeFederation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testFederationID,
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
			FederationID: "fed-123",
			LocalMember: novaedgev1alpha1.FederationMember{
				Name:     testLocalMember,
				Endpoint: "localhost:50051",
				Region:   "us-east-1",
				Zone:     "us-east-1a",
				Labels:   map[string]string{"env": "prod"},
			},
			Members: []novaedgev1alpha1.FederationPeer{
				{
					Name:     "peer1",
					Endpoint: "peer1.example.com:50051",
					Region:   "us-west-1",
					Zone:     "us-west-1a",
					Priority: 1,
					Labels:   map[string]string{"env": "prod"},
					TLS: &novaedgev1alpha1.FederationTLS{
						Enabled:            ptr.To(true),
						ServerName:         "peer1.example.com",
						InsecureSkipVerify: false,
					},
				},
			},
			Sync: &novaedgev1alpha1.FederationSyncConfig{
				Interval:          &metav1.Duration{Duration: 30 * time.Second},
				Timeout:           &metav1.Duration{Duration: 10 * time.Second},
				BatchSize:         100,
				Compression:       ptr.To(true),
				ResourceTypes:     []string{"proxygateways", "proxyroutes"},
				ExcludeNamespaces: []string{"kube-system"},
			},
			ConflictResolution: &novaedgev1alpha1.ConflictResolutionConfig{
				Strategy:     novaedgev1alpha1.ConflictResolutionLastWriterWins,
				VectorClocks: ptr.To(true),
				TombstoneTTL: &metav1.Duration{Duration: 24 * time.Hour},
			},
			HealthCheck: &novaedgev1alpha1.FederationHealthCheck{
				Interval:         &metav1.Duration{Duration: 10 * time.Second},
				Timeout:          &metav1.Duration{Duration: 5 * time.Second},
				FailureThreshold: 3,
				SuccessThreshold: 2,
			},
		},
	}

	manager, err := NewManagerFromCRD(federation, logger)
	if err != nil {
		t.Fatalf("NewManagerFromCRD() error = %v", err)
	}

	if manager == nil {
		t.Fatal("expected manager, got nil")
	}

	// Verify config was populated correctly
	if manager.config.FederationID != "fed-123" {
		t.Errorf("FederationID = %v, want fed-123", manager.config.FederationID)
	}

	if manager.config.LocalMember.Name != testLocalMember {
		t.Errorf("LocalMember.Name = %v, want local", manager.config.LocalMember.Name)
	}

	if manager.config.LocalMember.Endpoint != "localhost:50051" {
		t.Errorf("LocalMember.Endpoint = %v, want localhost:50051", manager.config.LocalMember.Endpoint)
	}

	if len(manager.config.Peers) != 1 {
		t.Errorf("Peers count = %v, want 1", len(manager.config.Peers))
	} else {
		peer := manager.config.Peers[0]
		if peer.Name != "peer1" {
			t.Errorf("Peer name = %v, want peer1", peer.Name)
		}
		if !peer.TLSEnabled {
			t.Error("Peer TLSEnabled should be true")
		}
		if peer.TLSServerName != "peer1.example.com" {
			t.Errorf("TLSServerName = %v, want peer1.example.com", peer.TLSServerName)
		}
	}

	if manager.config.SyncInterval != 30*time.Second {
		t.Errorf("SyncInterval = %v, want 30s", manager.config.SyncInterval)
	}

	if manager.config.BatchSize != 100 {
		t.Errorf("BatchSize = %v, want 100", manager.config.BatchSize)
	}

	if !manager.config.CompressionEnabled {
		t.Error("CompressionEnabled should be true")
	}

	if manager.config.VectorClocksEnabled != true {
		t.Error("VectorClocksEnabled should be true")
	}

	if manager.config.FailureThreshold != 3 {
		t.Errorf("FailureThreshold = %v, want 3", manager.config.FailureThreshold)
	}
}

func TestNewManagerFromCRDWithCreds(t *testing.T) {
	logger := zap.NewNop()

	federation := &novaedgev1alpha1.NovaEdgeFederation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testFederationID,
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
			FederationID: "fed-123",
			LocalMember: novaedgev1alpha1.FederationMember{
				Name:     testLocalMember,
				Endpoint: "localhost:50051",
			},
			Members: []novaedgev1alpha1.FederationPeer{
				{
					Name:     "peer1",
					Endpoint: "peer1.example.com:50051",
				},
			},
		},
	}

	tlsCreds := map[string]*TLSCredentials{
		"peer1": {
			CACert:     []byte("ca-cert-data"),
			ClientCert: []byte("client-cert-data"),
			ClientKey:  []byte("client-key-data"),
		},
	}

	manager, err := NewManagerFromCRDWithCreds(federation, tlsCreds, logger)
	if err != nil {
		t.Fatalf("NewManagerFromCRDWithCreds() error = %v", err)
	}

	if manager == nil {
		t.Fatal("expected manager, got nil")
	}

	// Verify TLS credentials were applied
	if len(manager.config.Peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(manager.config.Peers))
	}

	peer := manager.config.Peers[0]
	if string(peer.CACert) != "ca-cert-data" {
		t.Errorf("CACert = %v, want ca-cert-data", string(peer.CACert))
	}
	if string(peer.ClientCert) != "client-cert-data" {
		t.Errorf("ClientCert = %v, want client-cert-data", string(peer.ClientCert))
	}
	if string(peer.ClientKey) != "client-key-data" {
		t.Errorf("ClientKey = %v, want client-key-data", string(peer.ClientKey))
	}
}

func TestNewManagerFromCRDWithCreds_NilCreds(t *testing.T) {
	logger := zap.NewNop()

	federation := &novaedgev1alpha1.NovaEdgeFederation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testFederationID,
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
			FederationID: "fed-123",
			LocalMember: novaedgev1alpha1.FederationMember{
				Name:     testLocalMember,
				Endpoint: "localhost:50051",
			},
			Members: []novaedgev1alpha1.FederationPeer{
				{
					Name:     "peer1",
					Endpoint: "peer1.example.com:50051",
				},
			},
		},
	}

	// Pass nil in the creds map - should not panic
	tlsCreds := map[string]*TLSCredentials{
		"peer1": nil,
	}

	manager, err := NewManagerFromCRDWithCreds(federation, tlsCreds, logger)
	if err != nil {
		t.Fatalf("NewManagerFromCRDWithCreds() error = %v", err)
	}

	if manager == nil {
		t.Fatal("expected manager, got nil")
	}
}

func TestManagerStop_WhenNotStarted(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()
	manager := NewManager(config, logger)

	// Should not panic when stopping without starting
	manager.Stop()
}

func TestManagerGetPeerStates_WhenNotStarted(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()
	manager := NewManager(config, logger)

	states := manager.GetPeerStates()
	if states != nil {
		t.Errorf("GetPeerStates() = %v, want nil", states)
	}
}

func TestManagerGetStats_WhenNotStarted(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()
	manager := NewManager(config, logger)

	stats := manager.GetStats()
	// Should return empty stats
	if stats.TotalChangesReceived != 0 || stats.TotalChangesSent != 0 {
		t.Errorf("GetStats() = %+v, want empty stats", stats)
	}
}

func TestManagerGetConflicts_WhenNotStarted(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()
	manager := NewManager(config, logger)

	conflicts := manager.GetConflicts()
	if conflicts != nil {
		t.Errorf("GetConflicts() = %v, want nil", conflicts)
	}
}

func TestManagerGetPhase_WhenNotStarted(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()
	manager := NewManager(config, logger)

	phase := manager.GetPhase()
	if phase != PhaseInitializing {
		t.Errorf("GetPhase() = %v, want %v", phase, PhaseInitializing)
	}
}

func TestManagerResolveConflict_WhenNotStarted(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()
	manager := NewManager(config, logger)

	err := manager.ResolveConflict("test-key", true)
	if err == nil {
		t.Error("ResolveConflict() should return error when not started")
	}
}

func TestManagerGetVectorClock_WhenNotStarted(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()
	manager := NewManager(config, logger)

	vc := manager.GetVectorClock()
	if vc != nil {
		t.Errorf("GetVectorClock() = %v, want nil", vc)
	}
}

func TestManagerOnResourceChange(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultConfig()
	manager := NewManager(config, logger)

	callback := func(key ResourceKey, changeType ChangeType, data []byte) {
		// callback function
	}

	manager.OnResourceChange(callback)

	if manager.onResourceChange == nil {
		t.Error("OnResourceChange callback should be set")
	}
}

func TestCrdToConfig_EmptyMembers(t *testing.T) {
	federation := &novaedgev1alpha1.NovaEdgeFederation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testFederationID,
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
			FederationID: "fed-123",
			LocalMember: novaedgev1alpha1.FederationMember{
				Name:     testLocalMember,
				Endpoint: "localhost:50051",
			},
			Members: []novaedgev1alpha1.FederationPeer{},
		},
	}

	config := crdToConfig(federation)

	if config.FederationID != "fed-123" {
		t.Errorf("FederationID = %v, want fed-123", config.FederationID)
	}

	if len(config.Peers) != 0 {
		t.Errorf("Peers count = %v, want 0", len(config.Peers))
	}
}

func TestCrdToConfig_NilSync(t *testing.T) {
	federation := &novaedgev1alpha1.NovaEdgeFederation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testFederationID,
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
			FederationID: "fed-123",
			LocalMember: novaedgev1alpha1.FederationMember{
				Name:     testLocalMember,
				Endpoint: "localhost:50051",
			},
			Sync: nil, // No sync config
		},
	}

	config := crdToConfig(federation)

	// Should use defaults
	if config.SyncInterval == 0 {
		t.Error("SyncInterval should have a default value")
	}
}

func TestCrdToConfig_TLSDisabled(t *testing.T) {
	federation := &novaedgev1alpha1.NovaEdgeFederation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testFederationID,
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
			FederationID: "fed-123",
			LocalMember: novaedgev1alpha1.FederationMember{
				Name:     testLocalMember,
				Endpoint: "localhost:50051",
			},
			Members: []novaedgev1alpha1.FederationPeer{
				{
					Name:     "peer1",
					Endpoint: "peer1.example.com:50051",
					TLS: &novaedgev1alpha1.FederationTLS{
						Enabled: ptr.To(false),
					},
				},
			},
		},
	}

	config := crdToConfig(federation)

	if len(config.Peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(config.Peers))
	}

	if config.Peers[0].TLSEnabled {
		t.Error("TLSEnabled should be false")
	}
}

func TestCrdToConfig_TLSNil(t *testing.T) {
	federation := &novaedgev1alpha1.NovaEdgeFederation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testFederationID,
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
			FederationID: "fed-123",
			LocalMember: novaedgev1alpha1.FederationMember{
				Name:     testLocalMember,
				Endpoint: "localhost:50051",
			},
			Members: []novaedgev1alpha1.FederationPeer{
				{
					Name:     "peer1",
					Endpoint: "peer1.example.com:50051",
					TLS:      nil, // No TLS config
				},
			},
		},
	}

	config := crdToConfig(federation)

	if len(config.Peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(config.Peers))
	}

	// TLSEnabled should be false when TLS is nil
	if config.Peers[0].TLSEnabled {
		t.Error("TLSEnabled should be false when TLS config is nil")
	}
}

func TestCrdToConfig_Labels(t *testing.T) {
	federation := &novaedgev1alpha1.NovaEdgeFederation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testFederationID,
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
			FederationID: "fed-123",
			LocalMember: novaedgev1alpha1.FederationMember{
				Name:     testLocalMember,
				Endpoint: "localhost:50051",
				Labels:   map[string]string{"env": "prod", "team": "platform"},
			},
			Members: []novaedgev1alpha1.FederationPeer{
				{
					Name:     "peer1",
					Endpoint: "peer1.example.com:50051",
					Labels:   map[string]string{"env": "staging"},
				},
			},
		},
	}

	config := crdToConfig(federation)

	if config.LocalMember.Labels["env"] != "prod" {
		t.Errorf("LocalMember env label = %v, want prod", config.LocalMember.Labels["env"])
	}
	if config.LocalMember.Labels["team"] != "platform" {
		t.Errorf("LocalMember team label = %v, want platform", config.LocalMember.Labels["team"])
	}

	if config.Peers[0].Labels["env"] != "staging" {
		t.Errorf("Peer env label = %v, want staging", config.Peers[0].Labels["env"])
	}
}
