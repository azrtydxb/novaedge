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

package webhook

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

func TestFederationValidator_ValidateCreate(t *testing.T) {
	validator := &FederationValidator{}

	t.Run("valid federation", func(t *testing.T) {
		fed := &novaedgev1alpha1.NovaEdgeFederation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-federation",
				Namespace: "default",
			},
			Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
				FederationID: "test-federation-1",
				LocalMember: novaedgev1alpha1.FederationMember{
					Name:     "local",
					Endpoint: "local.example.com:8443",
				},
				Members: []novaedgev1alpha1.FederationPeer{
					{
						Name:     "remote1",
						Endpoint: "remote1.example.com:8443",
					},
				},
			},
		}

		warnings, err := validator.ValidateCreate(context.Background(), fed)
		assert.NoError(t, err)
		assert.Empty(t, warnings)
	})

	t.Run("invalid federation ID", func(t *testing.T) {
		fed := &novaedgev1alpha1.NovaEdgeFederation{
			Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
				FederationID: "INVALID-ID",
				LocalMember: novaedgev1alpha1.FederationMember{
					Name:     "local",
					Endpoint: "local.example.com:8443",
				},
			},
		}

		_, err := validator.ValidateCreate(context.Background(), fed)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid federation ID")
	})

	t.Run("federation ID too long", func(t *testing.T) {
		fed := &novaedgev1alpha1.NovaEdgeFederation{
			Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
				FederationID: "this-is-a-very-long-federation-id-that-exceeds-the-maximum-allowed-length-of-63-characters",
				LocalMember: novaedgev1alpha1.FederationMember{
					Name:     "local",
					Endpoint: "local.example.com:8443",
				},
			},
		}

		_, err := validator.ValidateCreate(context.Background(), fed)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "too long")
	})

	t.Run("invalid member name", func(t *testing.T) {
		fed := &novaedgev1alpha1.NovaEdgeFederation{
			Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
				FederationID: "test-federation",
				LocalMember: novaedgev1alpha1.FederationMember{
					Name:     "INVALID-NAME",
					Endpoint: "local.example.com:8443",
				},
			},
		}

		_, err := validator.ValidateCreate(context.Background(), fed)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid name")
	})

	t.Run("invalid endpoint format", func(t *testing.T) {
		fed := &novaedgev1alpha1.NovaEdgeFederation{
			Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
				FederationID: "test-federation",
				LocalMember: novaedgev1alpha1.FederationMember{
					Name:     "local",
					Endpoint: "invalid-endpoint",
				},
			},
		}

		_, err := validator.ValidateCreate(context.Background(), fed)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid endpoint")
	})

	t.Run("duplicate member name", func(t *testing.T) {
		fed := &novaedgev1alpha1.NovaEdgeFederation{
			Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
				FederationID: "test-federation",
				LocalMember: novaedgev1alpha1.FederationMember{
					Name:     "local",
					Endpoint: "local.example.com:8443",
				},
				Members: []novaedgev1alpha1.FederationPeer{
					{
						Name:     "local", // Same as local member
						Endpoint: "remote.example.com:8443",
					},
				},
			},
		}

		_, err := validator.ValidateCreate(context.Background(), fed)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot have same name as local member")
	})

	t.Run("duplicate endpoint", func(t *testing.T) {
		fed := &novaedgev1alpha1.NovaEdgeFederation{
			Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
				FederationID: "test-federation",
				LocalMember: novaedgev1alpha1.FederationMember{
					Name:     "local",
					Endpoint: "shared.example.com:8443",
				},
				Members: []novaedgev1alpha1.FederationPeer{
					{
						Name:     "remote",
						Endpoint: "shared.example.com:8443", // Same as local member
					},
				},
			},
		}

		_, err := validator.ValidateCreate(context.Background(), fed)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate endpoint")
	})

	t.Run("invalid resource type", func(t *testing.T) {
		fed := &novaedgev1alpha1.NovaEdgeFederation{
			Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
				FederationID: "test-federation",
				LocalMember: novaedgev1alpha1.FederationMember{
					Name:     "local",
					Endpoint: "local.example.com:8443",
				},
				Sync: &novaedgev1alpha1.FederationSyncConfig{
					ResourceTypes: []string{"InvalidResource"},
				},
			},
		}

		_, err := validator.ValidateCreate(context.Background(), fed)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid resource type")
	})

	t.Run("invalid conflict resolution strategy", func(t *testing.T) {
		fed := &novaedgev1alpha1.NovaEdgeFederation{
			Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
				FederationID: "test-federation",
				LocalMember: novaedgev1alpha1.FederationMember{
					Name:     "local",
					Endpoint: "local.example.com:8443",
				},
				ConflictResolution: &novaedgev1alpha1.ConflictResolutionConfig{
					Strategy: "InvalidStrategy",
				},
			},
		}

		_, err := validator.ValidateCreate(context.Background(), fed)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid conflict resolution strategy")
	})

	t.Run("health check timeout greater than interval", func(t *testing.T) {
		fed := &novaedgev1alpha1.NovaEdgeFederation{
			Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
				FederationID: "test-federation",
				LocalMember: novaedgev1alpha1.FederationMember{
					Name:     "local",
					Endpoint: "local.example.com:8443",
				},
				HealthCheck: &novaedgev1alpha1.FederationHealthCheck{
					Interval: &metav1.Duration{Duration: 10 * time.Second},
					Timeout:  &metav1.Duration{Duration: 30 * time.Second},
				},
			},
		}

		_, err := validator.ValidateCreate(context.Background(), fed)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "timeout must be less than interval")
	})

	t.Run("wrong object type", func(t *testing.T) {
		_, err := validator.ValidateCreate(context.Background(), &novaedgev1alpha1.ProxyGateway{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected NovaEdgeFederation")
	})
}

func TestFederationValidator_ValidateUpdate(t *testing.T) {
	validator := &FederationValidator{}

	t.Run("valid update", func(t *testing.T) {
		oldFed := &novaedgev1alpha1.NovaEdgeFederation{
			Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
				FederationID: "test-federation",
				LocalMember: novaedgev1alpha1.FederationMember{
					Name:     "local",
					Endpoint: "local.example.com:8443",
				},
			},
		}

		newFed := &novaedgev1alpha1.NovaEdgeFederation{
			Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
				FederationID: "test-federation",
				LocalMember: novaedgev1alpha1.FederationMember{
					Name:     "local",
					Endpoint: "local.example.com:8443",
				},
				Members: []novaedgev1alpha1.FederationPeer{
					{
						Name:     "remote1",
						Endpoint: "remote1.example.com:8443",
					},
				},
			},
		}

		warnings, err := validator.ValidateUpdate(context.Background(), oldFed, newFed)
		assert.NoError(t, err)
		assert.Empty(t, warnings)
	})

	t.Run("federation ID is immutable", func(t *testing.T) {
		oldFed := &novaedgev1alpha1.NovaEdgeFederation{
			Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
				FederationID: "old-id",
				LocalMember: novaedgev1alpha1.FederationMember{
					Name:     "local",
					Endpoint: "local.example.com:8443",
				},
			},
		}

		newFed := &novaedgev1alpha1.NovaEdgeFederation{
			Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
				FederationID: "new-id",
				LocalMember: novaedgev1alpha1.FederationMember{
					Name:     "local",
					Endpoint: "local.example.com:8443",
				},
			},
		}

		_, err := validator.ValidateUpdate(context.Background(), oldFed, newFed)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "federation ID is immutable")
	})

	t.Run("local member name is immutable", func(t *testing.T) {
		oldFed := &novaedgev1alpha1.NovaEdgeFederation{
			Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
				FederationID: "test-federation",
				LocalMember: novaedgev1alpha1.FederationMember{
					Name:     "old-name",
					Endpoint: "local.example.com:8443",
				},
			},
		}

		newFed := &novaedgev1alpha1.NovaEdgeFederation{
			Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
				FederationID: "test-federation",
				LocalMember: novaedgev1alpha1.FederationMember{
					Name:     "new-name",
					Endpoint: "local.example.com:8443",
				},
			},
		}

		_, err := validator.ValidateUpdate(context.Background(), oldFed, newFed)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "local member name is immutable")
	})

	t.Run("wrong old object type", func(t *testing.T) {
		newFed := &novaedgev1alpha1.NovaEdgeFederation{
			Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
				FederationID: "test-federation",
			},
		}
		_, err := validator.ValidateUpdate(context.Background(), &novaedgev1alpha1.ProxyGateway{}, newFed)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected NovaEdgeFederation")
	})

	t.Run("wrong new object type", func(t *testing.T) {
		oldFed := &novaedgev1alpha1.NovaEdgeFederation{
			Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
				FederationID: "test-federation",
			},
		}
		_, err := validator.ValidateUpdate(context.Background(), oldFed, &novaedgev1alpha1.ProxyGateway{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected NovaEdgeFederation")
	})
}

func TestFederationValidator_ValidateDelete(t *testing.T) {
	validator := &FederationValidator{}

	t.Run("delete with no issues", func(t *testing.T) {
		fed := &novaedgev1alpha1.NovaEdgeFederation{
			Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
				FederationID: "test-federation",
			},
		}

		warnings, err := validator.ValidateDelete(context.Background(), fed)
		assert.NoError(t, err)
		assert.Empty(t, warnings)
	})

	t.Run("delete with healthy peers warning", func(t *testing.T) {
		fed := &novaedgev1alpha1.NovaEdgeFederation{
			Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
				FederationID: "test-federation",
			},
			Status: novaedgev1alpha1.NovaEdgeFederationStatus{
				Members: []novaedgev1alpha1.FederationMemberStatus{
					{
						Name:    "remote1",
						Healthy: true,
					},
				},
			},
		}

		warnings, err := validator.ValidateDelete(context.Background(), fed)
		assert.NoError(t, err)
		assert.NotEmpty(t, warnings)
		assert.Contains(t, warnings[0], "healthy peers")
	})

	t.Run("delete with pending conflicts warning", func(t *testing.T) {
		fed := &novaedgev1alpha1.NovaEdgeFederation{
			Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
				FederationID: "test-federation",
			},
			Status: novaedgev1alpha1.NovaEdgeFederationStatus{
				ConflictsPending: 5,
			},
		}

		warnings, err := validator.ValidateDelete(context.Background(), fed)
		assert.NoError(t, err)
		assert.NotEmpty(t, warnings)
		assert.Contains(t, warnings[0], "pending conflicts")
	})

	t.Run("wrong object type", func(t *testing.T) {
		_, err := validator.ValidateDelete(context.Background(), &novaedgev1alpha1.ProxyGateway{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected NovaEdgeFederation")
	})
}

func TestFederationIDRegex(t *testing.T) {
	tests := []struct {
		id       string
		expected bool
	}{
		{"valid-id", true},
		{"valid123", true},
		{"123valid", true},
		{"a", true},
		{"ab", true},
		{"a-b", true},
		{"INVALID", false},
		{"invalid_ID", false},
		{"-invalid", false},
		{"invalid-", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			result := federationIDRegex.MatchString(tt.id)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMemberNameRegex(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"valid-name", true},
		{"valid123", true},
		{"123valid", true},
		{"a", true},
		{"ab", true},
		{"a-b", true},
		{"INVALID", false},
		{"invalid_name", false},
		{"-invalid", false},
		{"invalid-", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := memberNameRegex.MatchString(tt.name)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFederationValidator_ManualConflictResolutionWarning(t *testing.T) {
	validator := &FederationValidator{}

	fed := &novaedgev1alpha1.NovaEdgeFederation{
		Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
			FederationID: "test-federation",
			LocalMember: novaedgev1alpha1.FederationMember{
				Name:     "local",
				Endpoint: "local.example.com:8443",
			},
			ConflictResolution: &novaedgev1alpha1.ConflictResolutionConfig{
				Strategy: novaedgev1alpha1.ConflictResolutionManual,
			},
		},
	}

	warnings, err := validator.ValidateCreate(context.Background(), fed)
	require.NoError(t, err)
	assert.NotEmpty(t, warnings)
	assert.Contains(t, warnings[0], "Manual conflict resolution")
}

func TestFederationValidator_BatchSizeWarning(t *testing.T) {
	validator := &FederationValidator{}

	fed := &novaedgev1alpha1.NovaEdgeFederation{
		Spec: novaedgev1alpha1.NovaEdgeFederationSpec{
			FederationID: "test-federation",
			LocalMember: novaedgev1alpha1.FederationMember{
				Name:     "local",
				Endpoint: "local.example.com:8443",
			},
			Sync: &novaedgev1alpha1.FederationSyncConfig{
				BatchSize: 2000, // Large batch size
			},
		},
	}

	warnings, err := validator.ValidateCreate(context.Background(), fed)
	require.NoError(t, err)
	assert.NotEmpty(t, warnings)
	assert.Contains(t, warnings[0], "batch size")
}
