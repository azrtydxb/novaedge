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

package certmanager

import (
	"context"
	"errors"
	"testing"

	openapi_v2 "github.com/google/gnostic-models/openapiv2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/openapi"
	restclient "k8s.io/client-go/rest"
)

// mockDiscoveryClient implements discovery.DiscoveryInterface for testing
type mockDiscoveryClient struct {
	groupsAndResourcesFunc func() ([]*metav1.APIGroup, []*metav1.APIResourceList, error)
}

func (m *mockDiscoveryClient) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	if m.groupsAndResourcesFunc != nil {
		return m.groupsAndResourcesFunc()
	}
	return nil, nil, nil
}

// Stub implementations for other DiscoveryInterface methods
func (m *mockDiscoveryClient) ServerGroups() (*metav1.APIGroupList, error) {
	return nil, nil
}

func (m *mockDiscoveryClient) ServerResourcesForGroupVersion(_ string) (*metav1.APIResourceList, error) {
	return nil, nil
}

func (m *mockDiscoveryClient) ServerResources() ([]*metav1.APIResourceList, error) {
	return nil, nil
}

func (m *mockDiscoveryClient) ServerPreferredResources() ([]*metav1.APIResourceList, error) {
	return nil, nil
}

func (m *mockDiscoveryClient) ServerPreferredNamespacedResources() ([]*metav1.APIResourceList, error) {
	return nil, nil
}

func (m *mockDiscoveryClient) ServerVersion() (*version.Info, error) {
	return nil, nil
}

func (m *mockDiscoveryClient) OpenAPISchema() (*openapi_v2.Document, error) {
	return nil, nil
}

func (m *mockDiscoveryClient) OpenAPIV3() openapi.Client {
	return nil
}

func (m *mockDiscoveryClient) RESTClient() restclient.Interface {
	return nil
}

func (m *mockDiscoveryClient) WithLegacy() discovery.DiscoveryInterface {
	return m
}

func TestEnableModeConstants(t *testing.T) {
	assert.Equal(t, EnableMode("auto"), EnableModeAuto)
	assert.Equal(t, EnableMode("true"), EnableModeTrue)
	assert.Equal(t, EnableMode("false"), EnableModeFalse)
}

func TestGVRVariables(t *testing.T) {
	assert.Equal(t, "cert-manager.io", CertificateGVR.Group)
	assert.Equal(t, "v1", CertificateGVR.Version)
	assert.Equal(t, "certificates", CertificateGVR.Resource)

	assert.Equal(t, "cert-manager.io", IssuerGVR.Group)
	assert.Equal(t, "v1", IssuerGVR.Version)
	assert.Equal(t, "issuers", IssuerGVR.Resource)

	assert.Equal(t, "cert-manager.io", ClusterIssuerGVR.Group)
	assert.Equal(t, "v1", ClusterIssuerGVR.Version)
	assert.Equal(t, "clusterissuers", ClusterIssuerGVR.Resource)
}

func TestNewDetectorFromClient(t *testing.T) {
	mockClient := &mockDiscoveryClient{}
	detector := NewDetectorFromClient(mockClient)
	assert.NotNil(t, detector)
	assert.Equal(t, mockClient, detector.discoveryClient)
}

func TestDetector_IsCertManagerInstalled(t *testing.T) {
	ctx := context.Background()

	t.Run("cert-manager installed", func(t *testing.T) {
		mockClient := &mockDiscoveryClient{
			groupsAndResourcesFunc: func() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
				return []*metav1.APIGroup{},
					[]*metav1.APIResourceList{
						{
							GroupVersion: "cert-manager.io/v1",
							APIResources: []metav1.APIResource{
								{Name: "certificates"},
								{Name: "issuers"},
								{Name: "clusterissuers"},
							},
						},
						{
							GroupVersion: "apps/v1",
							APIResources: []metav1.APIResource{
								{Name: "deployments"},
							},
						},
					}, nil
			},
		}
		detector := NewDetectorFromClient(mockClient)
		installed, err := detector.IsCertManagerInstalled(ctx)
		require.NoError(t, err)
		assert.True(t, installed)
	})

	t.Run("cert-manager not installed", func(t *testing.T) {
		mockClient := &mockDiscoveryClient{
			groupsAndResourcesFunc: func() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
				return []*metav1.APIGroup{},
					[]*metav1.APIResourceList{
						{
							GroupVersion: "apps/v1",
							APIResources: []metav1.APIResource{
								{Name: "deployments"},
							},
						},
					}, nil
			},
		}
		detector := NewDetectorFromClient(mockClient)
		installed, err := detector.IsCertManagerInstalled(ctx)
		require.NoError(t, err)
		assert.False(t, installed)
	})

	t.Run("empty resources", func(t *testing.T) {
		mockClient := &mockDiscoveryClient{
			groupsAndResourcesFunc: func() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
				return nil, nil, nil
			},
		}
		detector := NewDetectorFromClient(mockClient)
		installed, err := detector.IsCertManagerInstalled(ctx)
		require.NoError(t, err)
		assert.False(t, installed)
	})

	t.Run("nil resource list entries", func(t *testing.T) {
		mockClient := &mockDiscoveryClient{
			groupsAndResourcesFunc: func() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
				return []*metav1.APIGroup{},
					[]*metav1.APIResourceList{nil, nil}, nil
			},
		}
		detector := NewDetectorFromClient(mockClient)
		installed, err := detector.IsCertManagerInstalled(ctx)
		require.NoError(t, err)
		assert.False(t, installed)
	})

	t.Run("invalid group version is skipped", func(t *testing.T) {
		mockClient := &mockDiscoveryClient{
			groupsAndResourcesFunc: func() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
				return []*metav1.APIGroup{},
					[]*metav1.APIResourceList{
						{
							GroupVersion: "invalid-format",
							APIResources: []metav1.APIResource{
								{Name: "certificates"},
							},
						},
					}, nil
			},
		}
		detector := NewDetectorFromClient(mockClient)
		installed, err := detector.IsCertManagerInstalled(ctx)
		require.NoError(t, err)
		assert.False(t, installed)
	})

	t.Run("discovery error - non-group-failed", func(t *testing.T) {
		mockClient := &mockDiscoveryClient{
			groupsAndResourcesFunc: func() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
				return nil, nil, errors.New("discovery failed")
			},
		}
		detector := NewDetectorFromClient(mockClient)
		installed, err := detector.IsCertManagerInstalled(ctx)
		require.Error(t, err)
		assert.False(t, installed)
		assert.Contains(t, err.Error(), "failed to discover API resources")
	})

	t.Run("discovery error - group failed with partial results", func(t *testing.T) {
		mockClient := &mockDiscoveryClient{
			groupsAndResourcesFunc: func() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
				return []*metav1.APIGroup{},
					[]*metav1.APIResourceList{
						{
							GroupVersion: "cert-manager.io/v1",
							APIResources: []metav1.APIResource{
								{Name: "certificates"},
							},
						},
					}, &discovery.ErrGroupDiscoveryFailed{
						Groups: map[schema.GroupVersion]error{
							{Group: "some-group", Version: "v1"}: errors.New("failed"),
						},
					}
			},
		}
		detector := NewDetectorFromClient(mockClient)
		installed, err := detector.IsCertManagerInstalled(ctx)
		require.NoError(t, err) // Partial results should not cause error
		assert.True(t, installed)
	})

	t.Run("cert-manager group without certificates resource", func(t *testing.T) {
		mockClient := &mockDiscoveryClient{
			groupsAndResourcesFunc: func() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
				return []*metav1.APIGroup{},
					[]*metav1.APIResourceList{
						{
							GroupVersion: "cert-manager.io/v1",
							APIResources: []metav1.APIResource{
								{Name: "issuers"},
								{Name: "clusterissuers"},
							},
						},
					}, nil
			},
		}
		detector := NewDetectorFromClient(mockClient)
		installed, err := detector.IsCertManagerInstalled(ctx)
		require.NoError(t, err)
		assert.False(t, installed)
	})
}

func TestDetector_ShouldEnable(t *testing.T) {
	ctx := context.Background()

	t.Run("mode false always returns false", func(t *testing.T) {
		mockClient := &mockDiscoveryClient{}
		detector := NewDetectorFromClient(mockClient)
		enabled, err := detector.ShouldEnable(ctx, EnableModeFalse)
		require.NoError(t, err)
		assert.False(t, enabled)
	})

	t.Run("mode true with cert-manager installed", func(t *testing.T) {
		mockClient := &mockDiscoveryClient{
			groupsAndResourcesFunc: func() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
				return nil,
					[]*metav1.APIResourceList{
						{
							GroupVersion: "cert-manager.io/v1",
							APIResources: []metav1.APIResource{
								{Name: "certificates"},
							},
						},
					}, nil
			},
		}
		detector := NewDetectorFromClient(mockClient)
		enabled, err := detector.ShouldEnable(ctx, EnableModeTrue)
		require.NoError(t, err)
		assert.True(t, enabled)
	})

	t.Run("mode true with cert-manager not installed", func(t *testing.T) {
		mockClient := &mockDiscoveryClient{
			groupsAndResourcesFunc: func() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
				return nil, []*metav1.APIResourceList{}, nil
			},
		}
		detector := NewDetectorFromClient(mockClient)
		enabled, err := detector.ShouldEnable(ctx, EnableModeTrue)
		require.Error(t, err)
		assert.False(t, enabled)
		assert.Contains(t, err.Error(), "cert-manager is required")
	})

	t.Run("mode true with discovery error", func(t *testing.T) {
		mockClient := &mockDiscoveryClient{
			groupsAndResourcesFunc: func() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
				return nil, nil, errors.New("discovery failed")
			},
		}
		detector := NewDetectorFromClient(mockClient)
		enabled, err := detector.ShouldEnable(ctx, EnableModeTrue)
		require.Error(t, err)
		assert.False(t, enabled)
		assert.Contains(t, err.Error(), "failed to detect cert-manager")
	})

	t.Run("mode auto with cert-manager installed", func(t *testing.T) {
		mockClient := &mockDiscoveryClient{
			groupsAndResourcesFunc: func() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
				return nil,
					[]*metav1.APIResourceList{
						{
							GroupVersion: "cert-manager.io/v1",
							APIResources: []metav1.APIResource{
								{Name: "certificates"},
							},
						},
					}, nil
			},
		}
		detector := NewDetectorFromClient(mockClient)
		enabled, err := detector.ShouldEnable(ctx, EnableModeAuto)
		require.NoError(t, err)
		assert.True(t, enabled)
	})

	t.Run("mode auto with cert-manager not installed", func(t *testing.T) {
		mockClient := &mockDiscoveryClient{
			groupsAndResourcesFunc: func() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
				return nil, []*metav1.APIResourceList{}, nil
			},
		}
		detector := NewDetectorFromClient(mockClient)
		enabled, err := detector.ShouldEnable(ctx, EnableModeAuto)
		require.NoError(t, err)
		assert.False(t, enabled)
	})

	t.Run("mode auto with discovery error returns false", func(t *testing.T) {
		mockClient := &mockDiscoveryClient{
			groupsAndResourcesFunc: func() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
				return nil, nil, errors.New("discovery failed")
			},
		}
		detector := NewDetectorFromClient(mockClient)
		enabled, err := detector.ShouldEnable(ctx, EnableModeAuto)
		require.NoError(t, err) // Auto mode doesn't return error on discovery failure
		assert.False(t, enabled)
	})

	t.Run("invalid mode returns error", func(t *testing.T) {
		mockClient := &mockDiscoveryClient{}
		detector := NewDetectorFromClient(mockClient)
		enabled, err := detector.ShouldEnable(ctx, EnableMode("invalid"))
		require.Error(t, err)
		assert.False(t, enabled)
		assert.Contains(t, err.Error(), "invalid cert-manager enable mode")
	})
}
