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

package mesh

import (
	"testing"

	"go.uber.org/zap"

	pb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

const testLocalAgentSPIFFE = "spiffe://cluster.local/agent/node-1"

func TestParseSPIFFEID(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantTrust     string
		wantNamespace string
		wantSA        string
		wantSpiffeID  string
	}{
		{
			name:          "standard workload identity",
			input:         "spiffe://cluster.local/ns/default/sa/my-service",
			wantTrust:     "cluster.local",
			wantNamespace: "default",
			wantSA:        "my-service",
			wantSpiffeID:  "spiffe://cluster.local/ns/default/sa/my-service",
		},
		{
			name:          "custom trust domain",
			input:         "spiffe://example.org/ns/production/sa/frontend",
			wantTrust:     "example.org",
			wantNamespace: "production",
			wantSA:        "frontend",
			wantSpiffeID:  "spiffe://example.org/ns/production/sa/frontend",
		},
		{
			name:          "hyphenated names",
			input:         "spiffe://my-domain.io/ns/kube-system/sa/coredns-sa",
			wantTrust:     "my-domain.io",
			wantNamespace: "kube-system",
			wantSA:        "coredns-sa",
			wantSpiffeID:  "spiffe://my-domain.io/ns/kube-system/sa/coredns-sa",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSPIFFEID(tt.input)
			if got.TrustDomain != tt.wantTrust {
				t.Errorf("TrustDomain = %q, want %q", got.TrustDomain, tt.wantTrust)
			}
			if got.Namespace != tt.wantNamespace {
				t.Errorf("Namespace = %q, want %q", got.Namespace, tt.wantNamespace)
			}
			if got.ServiceAccount != tt.wantSA {
				t.Errorf("ServiceAccount = %q, want %q", got.ServiceAccount, tt.wantSA)
			}
			if got.SpiffeID != tt.wantSpiffeID {
				t.Errorf("SpiffeID = %q, want %q", got.SpiffeID, tt.wantSpiffeID)
			}
		})
	}
}

func TestParseSPIFFEIDAgentFormat(t *testing.T) {
	id := ParseSPIFFEID(testLocalAgentSPIFFE)
	if id.TrustDomain != "cluster.local" {
		t.Errorf("TrustDomain = %q, want %q", id.TrustDomain, "cluster.local")
	}
	if id.Namespace != "" {
		t.Errorf("Namespace = %q, want empty", id.Namespace)
	}
	if id.ServiceAccount != "" {
		t.Errorf("ServiceAccount = %q, want empty", id.ServiceAccount)
	}
	if id.SpiffeID != testLocalAgentSPIFFE {
		t.Errorf("SpiffeID = %q, want %q", id.SpiffeID, testLocalAgentSPIFFE)
	}
}

func TestParseSPIFFEIDInvalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"no scheme", "cluster.local/ns/default/sa/svc"},
		{"wrong scheme", "https://cluster.local/ns/default/sa/svc"},
		{"no host", "spiffe:///ns/default/sa/svc"},
		{"incomplete path", "spiffe://cluster.local/ns/default"},
		{"wrong prefix", "spiffe://cluster.local/svc/default/sa/svc"},
		{"missing sa segment", "spiffe://cluster.local/ns/default/notsa/svc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSPIFFEID(tt.input)
			if got.Namespace != "" || got.ServiceAccount != "" {
				t.Errorf("expected empty Namespace and ServiceAccount for invalid input %q, got ns=%q sa=%q",
					tt.input, got.Namespace, got.ServiceAccount)
			}
		})
	}
}

func TestAuthorizeNoPolicy(t *testing.T) {
	authz := NewAuthorizer(zap.NewNop())

	source := ParseSPIFFEID("spiffe://cluster.local/ns/default/sa/client")
	allowed := authz.Authorize(source, "backend.default", "GET", "/api/v1/data")

	if !allowed {
		t.Error("expected allow when no policies exist, got deny")
	}
}

func TestAuthorizeAllowPolicy(t *testing.T) {
	authz := NewAuthorizer(zap.NewNop())

	authz.UpdatePolicies([]*pb.MeshAuthorizationPolicy{
		{
			Name:            "allow-frontend",
			TargetService:   "backend",
			TargetNamespace: "default",
			Action:          "ALLOW",
			Rules: []*pb.MeshAuthorizationRule{
				{
					From: []*pb.MeshSource{
						{ServiceAccounts: []string{"frontend"}},
					},
				},
			},
		},
	})

	// Matching source should be allowed
	source := ParseSPIFFEID("spiffe://cluster.local/ns/default/sa/frontend")
	if !authz.Authorize(source, "backend.default", "GET", "/") {
		t.Error("expected allow for matching service account, got deny")
	}

	// Non-matching source should be denied (ALLOW policy exists but doesn't match)
	other := ParseSPIFFEID("spiffe://cluster.local/ns/default/sa/other-service")
	if authz.Authorize(other, "backend.default", "GET", "/") {
		t.Error("expected deny for non-matching service account, got allow")
	}
}

func TestAuthorizeDenyPolicy(t *testing.T) {
	authz := NewAuthorizer(zap.NewNop())

	authz.UpdatePolicies([]*pb.MeshAuthorizationPolicy{
		{
			Name:            "deny-attacker",
			TargetService:   "backend",
			TargetNamespace: "default",
			Action:          "DENY",
			Rules: []*pb.MeshAuthorizationRule{
				{
					From: []*pb.MeshSource{
						{Namespaces: []string{"untrusted"}},
					},
				},
			},
		},
	})

	// Source from untrusted namespace should be denied
	blocked := ParseSPIFFEID("spiffe://cluster.local/ns/untrusted/sa/attacker")
	if authz.Authorize(blocked, "backend.default", "GET", "/") {
		t.Error("expected deny for untrusted namespace, got allow")
	}

	// Source from trusted namespace should be allowed (DENY didn't match, no ALLOW policies)
	trusted := ParseSPIFFEID("spiffe://cluster.local/ns/production/sa/legit-service")
	if !authz.Authorize(trusted, "backend.default", "GET", "/") {
		t.Error("expected allow for trusted namespace, got deny")
	}
}

func TestAuthorizeNamespaceMatch(t *testing.T) {
	authz := NewAuthorizer(zap.NewNop())

	authz.UpdatePolicies([]*pb.MeshAuthorizationPolicy{
		{
			Name:            "allow-prod-namespaces",
			TargetService:   "api",
			TargetNamespace: "production",
			Action:          "ALLOW",
			Rules: []*pb.MeshAuthorizationRule{
				{
					From: []*pb.MeshSource{
						{Namespaces: []string{"production", "staging"}},
					},
				},
			},
		},
	})

	// Production namespace should match
	prod := ParseSPIFFEID("spiffe://cluster.local/ns/production/sa/web")
	if !authz.Authorize(prod, "api.production", "GET", "/") {
		t.Error("expected allow for production namespace")
	}

	// Staging namespace should match
	staging := ParseSPIFFEID("spiffe://cluster.local/ns/staging/sa/web")
	if !authz.Authorize(staging, "api.production", "GET", "/") {
		t.Error("expected allow for staging namespace")
	}

	// Development namespace should not match
	dev := ParseSPIFFEID("spiffe://cluster.local/ns/development/sa/web")
	if authz.Authorize(dev, "api.production", "GET", "/") {
		t.Error("expected deny for development namespace")
	}
}

func TestAuthorizeMethodPathMatch(t *testing.T) {
	authz := NewAuthorizer(zap.NewNop())

	authz.UpdatePolicies([]*pb.MeshAuthorizationPolicy{
		{
			Name:            "allow-read-api",
			TargetService:   "backend",
			TargetNamespace: "default",
			Action:          "ALLOW",
			Rules: []*pb.MeshAuthorizationRule{
				{
					From: []*pb.MeshSource{
						{Namespaces: []string{"default"}},
					},
					To: []*pb.MeshDestination{
						{
							Methods: []string{"GET", "HEAD"},
							Paths:   []string{"/api/*"},
						},
					},
				},
			},
		},
	})

	source := ParseSPIFFEID("spiffe://cluster.local/ns/default/sa/client")

	// GET /api/v1 should match
	if !authz.Authorize(source, "backend.default", "GET", "/api/v1") {
		t.Error("expected allow for GET /api/v1")
	}

	// HEAD /api/data should match
	if !authz.Authorize(source, "backend.default", "HEAD", "/api/data") {
		t.Error("expected allow for HEAD /api/data")
	}

	// POST /api/v1 should not match (wrong method)
	if authz.Authorize(source, "backend.default", "POST", "/api/v1") {
		t.Error("expected deny for POST /api/v1")
	}

	// GET /admin should not match (wrong path)
	if authz.Authorize(source, "backend.default", "GET", "/admin") {
		t.Error("expected deny for GET /admin")
	}

	// Case-insensitive method matching
	if !authz.Authorize(source, "backend.default", "get", "/api/v1") {
		t.Error("expected allow for lowercase 'get' method")
	}
}

func TestAuthorizeOpaqueTCP(t *testing.T) {
	authz := NewAuthorizer(zap.NewNop())

	authz.UpdatePolicies([]*pb.MeshAuthorizationPolicy{
		{
			Name:            "allow-with-l7-rules",
			TargetService:   "database",
			TargetNamespace: "default",
			Action:          "ALLOW",
			Rules: []*pb.MeshAuthorizationRule{
				{
					From: []*pb.MeshSource{
						{Namespaces: []string{"default"}},
					},
					To: []*pb.MeshDestination{
						{
							Methods: []string{"GET"},
							Paths:   []string{"/api/*"},
						},
					},
				},
			},
		},
	})

	source := ParseSPIFFEID("spiffe://cluster.local/ns/default/sa/app")

	// Opaque TCP (empty method/path) should NOT match a rule with methods/paths set
	if authz.Authorize(source, "database.default", "", "") {
		t.Error("expected deny for opaque TCP when only L7 rules exist")
	}

	// Add a policy with empty to (matches all including TCP)
	authz.UpdatePolicies([]*pb.MeshAuthorizationPolicy{
		{
			Name:            "allow-all-traffic",
			TargetService:   "database",
			TargetNamespace: "default",
			Action:          "ALLOW",
			Rules: []*pb.MeshAuthorizationRule{
				{
					From: []*pb.MeshSource{
						{Namespaces: []string{"default"}},
					},
					// Empty To — matches all destinations including opaque TCP
				},
			},
		},
	})

	// Opaque TCP should now match since To is empty
	if !authz.Authorize(source, "database.default", "", "") {
		t.Error("expected allow for opaque TCP when To is empty")
	}
}

func TestParseSPIFFEIDFederated(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		wantFederationID string
		wantClusterName  string
		wantNodeName     string
		wantTrustDomain  string
	}{
		{
			name:             "federated agent identity",
			input:            "spiffe://my-federation/cluster/cluster-east/agent/node-1",
			wantFederationID: "my-federation",
			wantClusterName:  "cluster-east",
			wantNodeName:     "node-1",
			wantTrustDomain:  "my-federation",
		},
		{
			name:             "federated agent with dots in names",
			input:            "spiffe://prod.federation.io/cluster/us-west-2.prod/agent/worker-03",
			wantFederationID: "prod.federation.io",
			wantClusterName:  "us-west-2.prod",
			wantNodeName:     "worker-03",
			wantTrustDomain:  "prod.federation.io",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSPIFFEID(tt.input)
			if got.FederationID != tt.wantFederationID {
				t.Errorf("FederationID = %q, want %q", got.FederationID, tt.wantFederationID)
			}
			if got.ClusterName != tt.wantClusterName {
				t.Errorf("ClusterName = %q, want %q", got.ClusterName, tt.wantClusterName)
			}
			if got.NodeName != tt.wantNodeName {
				t.Errorf("NodeName = %q, want %q", got.NodeName, tt.wantNodeName)
			}
			if got.TrustDomain != tt.wantTrustDomain {
				t.Errorf("TrustDomain = %q, want %q", got.TrustDomain, tt.wantTrustDomain)
			}
			if !got.IsFederated() {
				t.Error("expected IsFederated() to return true")
			}
		})
	}
}

func TestParseSPIFFEIDLocalAgentNodeName(t *testing.T) {
	id := ParseSPIFFEID(testLocalAgentSPIFFE)
	if id.NodeName != "node-1" {
		t.Errorf("NodeName = %q, want %q", id.NodeName, "node-1")
	}
	if id.IsFederated() {
		t.Error("expected IsFederated() to return false for local agent identity")
	}
}

func TestSourceIdentityIsFederated(t *testing.T) {
	tests := []struct {
		name     string
		identity SourceIdentity
		want     bool
	}{
		{
			name:     "federated",
			identity: SourceIdentity{FederationID: "fed-1", ClusterName: "cluster-a"},
			want:     true,
		},
		{
			name:     "missing cluster",
			identity: SourceIdentity{FederationID: "fed-1"},
			want:     false,
		},
		{
			name:     "missing federation",
			identity: SourceIdentity{ClusterName: "cluster-a"},
			want:     false,
		},
		{
			name:     "empty",
			identity: SourceIdentity{},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.identity.IsFederated(); got != tt.want {
				t.Errorf("IsFederated() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFederationConfig(t *testing.T) {
	t.Run("inactive when empty", func(t *testing.T) {
		cfg := FederationConfig{}
		if cfg.IsActive() {
			t.Error("expected IsActive() to return false for empty config")
		}
		if cfg.IsClusterAllowed("any") {
			t.Error("expected IsClusterAllowed() to return false when inactive")
		}
	})

	t.Run("active with allowed clusters", func(t *testing.T) {
		cfg := FederationConfig{
			FederationID:    "my-fed",
			ClusterName:     "cluster-a",
			AllowedClusters: []string{"cluster-b", "cluster-c"},
		}
		if !cfg.IsActive() {
			t.Error("expected IsActive() to return true")
		}
		if !cfg.IsClusterAllowed("cluster-b") {
			t.Error("expected cluster-b to be allowed")
		}
		if !cfg.IsClusterAllowed("cluster-c") {
			t.Error("expected cluster-c to be allowed")
		}
		if cfg.IsClusterAllowed("cluster-d") {
			t.Error("expected cluster-d to be rejected")
		}
	})
}

func TestBuildSPIFFEID(t *testing.T) {
	t.Run("local (no federation)", func(t *testing.T) {
		got := BuildSPIFFEID("cluster.local", "node-1", nil)
		want := testLocalAgentSPIFFE
		if got != want {
			t.Errorf("BuildSPIFFEID() = %q, want %q", got, want)
		}
	})

	t.Run("local (inactive federation)", func(t *testing.T) {
		cfg := &FederationConfig{}
		got := BuildSPIFFEID("cluster.local", "node-1", cfg)
		want := testLocalAgentSPIFFE
		if got != want {
			t.Errorf("BuildSPIFFEID() = %q, want %q", got, want)
		}
	})

	t.Run("federated", func(t *testing.T) {
		cfg := &FederationConfig{
			FederationID: "prod-federation",
			ClusterName:  "us-east-1",
		}
		got := BuildSPIFFEID("cluster.local", "node-1", cfg)
		want := "spiffe://prod-federation/cluster/us-east-1/agent/node-1"
		if got != want {
			t.Errorf("BuildSPIFFEID() = %q, want %q", got, want)
		}
	})

	t.Run("roundtrip parse", func(t *testing.T) {
		cfg := &FederationConfig{
			FederationID: "my-fed",
			ClusterName:  "cluster-west",
		}
		spiffeID := BuildSPIFFEID("cluster.local", "worker-5", cfg)
		parsed := ParseSPIFFEID(spiffeID)
		if parsed.FederationID != "my-fed" {
			t.Errorf("FederationID = %q, want %q", parsed.FederationID, "my-fed")
		}
		if parsed.ClusterName != "cluster-west" {
			t.Errorf("ClusterName = %q, want %q", parsed.ClusterName, "cluster-west")
		}
		if parsed.NodeName != "worker-5" {
			t.Errorf("NodeName = %q, want %q", parsed.NodeName, "worker-5")
		}
	})
}
