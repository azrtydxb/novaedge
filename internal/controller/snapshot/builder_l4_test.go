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

package snapshot

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
	corev1 "k8s.io/api/core/v1"
)

func TestBuildL4Listeners(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	builder := NewBuilder(fakeClient)
	ctx := context.Background()

	tests := []struct {
		name             string
		gateways         []*pb.Gateway
		endpoints        map[string]*pb.EndpointList
		expectedCount    int
		expectedProtocol pb.Protocol
	}{
		{
			name: "TCP listener",
			gateways: []*pb.Gateway{
				{
					Name:      "tcp-gateway",
					Namespace: "default",
					Listeners: []*pb.Listener{
						{
							Name:     "tcp",
							Port:     9000,
							Protocol: pb.Protocol_TCP,
						},
					},
				},
			},
			endpoints:        make(map[string]*pb.EndpointList),
			expectedCount:    0, // No backends configured
			expectedProtocol: pb.Protocol_TCP,
		},
		{
			name: "TLS passthrough listener",
			gateways: []*pb.Gateway{
				{
					Name:      "tls-gateway",
					Namespace: "default",
					Listeners: []*pb.Listener{
						{
							Name:      "tls",
							Port:      443,
							Protocol:  pb.Protocol_TLS,
							Hostnames: []string{"example.com"},
						},
					},
				},
			},
			endpoints:        make(map[string]*pb.EndpointList),
			expectedCount:    0, // No backends configured
			expectedProtocol: pb.Protocol_TLS,
		},
		{
			name: "UDP listener",
			gateways: []*pb.Gateway{
				{
					Name:      "udp-gateway",
					Namespace: "default",
					Listeners: []*pb.Listener{
						{
							Name:     "udp",
							Port:     53,
							Protocol: pb.Protocol_UDP,
						},
					},
				},
			},
			endpoints:        make(map[string]*pb.EndpointList),
			expectedCount:    0, // No backends configured
			expectedProtocol: pb.Protocol_UDP,
		},
		{
			name: "HTTP listener ignored",
			gateways: []*pb.Gateway{
				{
					Name:      "http-gateway",
					Namespace: "default",
					Listeners: []*pb.Listener{
						{
							Name:     "http",
							Port:     80,
							Protocol: pb.Protocol_HTTP,
						},
					},
				},
			},
			endpoints:     make(map[string]*pb.EndpointList),
			expectedCount: 0, // HTTP listeners are not L4
		},
		{
			name:          "no listeners",
			gateways:      []*pb.Gateway{},
			endpoints:     make(map[string]*pb.EndpointList),
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listeners := builder.buildL4Listeners(ctx, tt.gateways, tt.endpoints)
			
			if len(listeners) != tt.expectedCount {
				t.Errorf("Expected %d listeners, got %d", tt.expectedCount, len(listeners))
			}
		})
	}
}

func TestBuildTCPListener(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	builder := NewBuilder(fakeClient)
	ctx := context.Background()

	gateway := &pb.Gateway{
		Name:      "test-gateway",
		Namespace: "default",
	}

	listener := &pb.Listener{
		Name:     "tcp-listener",
		Port:     9000,
		Protocol: pb.Protocol_TCP,
	}

	endpoints := make(map[string]*pb.EndpointList)

	t.Run("TCP listener without backends", func(t *testing.T) {
		result := builder.buildTCPListener(ctx, gateway, listener, endpoints)
		if result != nil {
			t.Error("Expected nil listener without backends")
		}
	})

	t.Run("TCP listener with defaults", func(t *testing.T) {
		// Even without backends, the config should have defaults if it were created
		// For this test, we verify the function behavior with no backends
		result := builder.buildTCPListener(ctx, gateway, listener, endpoints)
		if result != nil {
			t.Error("Expected nil listener without backends")
		}
	})
}

func TestBuildTLSPassthroughListener(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	builder := NewBuilder(fakeClient)
	ctx := context.Background()

	gateway := &pb.Gateway{
		Name:      "test-gateway",
		Namespace: "default",
	}

	tests := []struct {
		name         string
		listener     *pb.Listener
		endpoints    map[string]*pb.EndpointList
		expectNil    bool
		expectRoutes int
	}{
		{
			name: "TLS listener with hostname",
			listener: &pb.Listener{
				Name:      "tls-listener",
				Port:      443,
				Protocol:  pb.Protocol_TLS,
				Hostnames: []string{"example.com", "www.example.com"},
			},
			endpoints:    make(map[string]*pb.EndpointList),
			expectNil:    true, // No backends configured
			expectRoutes: 0,
		},
		{
			name: "TLS listener without hostname",
			listener: &pb.Listener{
				Name:      "tls-listener",
				Port:      443,
				Protocol:  pb.Protocol_TLS,
				Hostnames: []string{},
			},
			endpoints:    make(map[string]*pb.EndpointList),
			expectNil:    true,
			expectRoutes: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := builder.buildTLSPassthroughListener(ctx, gateway, tt.listener, tt.endpoints)
			
			if tt.expectNil && result != nil {
				t.Error("Expected nil listener")
			}
			if !tt.expectNil && result == nil {
				t.Error("Expected non-nil listener")
			}
		})
	}
}

func TestBuildUDPListener(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	builder := NewBuilder(fakeClient)
	ctx := context.Background()

	gateway := &pb.Gateway{
		Name:      "test-gateway",
		Namespace: "default",
	}

	listener := &pb.Listener{
		Name:     "udp-listener",
		Port:     53,
		Protocol: pb.Protocol_UDP,
	}

	endpoints := make(map[string]*pb.EndpointList)

	t.Run("UDP listener without backends", func(t *testing.T) {
		result := builder.buildUDPListener(ctx, gateway, listener, endpoints)
		if result != nil {
			t.Error("Expected nil listener without backends")
		}
	})
}

func TestFindL4BackendForListener(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name            string
		routes          []runtime.Object
		gateway         *pb.Gateway
		listener        *pb.Listener
		endpoints       map[string]*pb.EndpointList
		expectedBackend string
		expectedEPs     int
	}{
		{
			name: "route with matching backend",
			routes: []runtime.Object{
				&novaedgev1alpha1.ProxyRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "default",
					},
					Spec: novaedgev1alpha1.ProxyRouteSpec{
						Rules: []novaedgev1alpha1.HTTPRouteRule{
							{
								BackendRefs: []novaedgev1alpha1.BackendRef{
									{
										Name: "backend1",
									},
								},
							},
						},
					},
				},
			},
			gateway: &pb.Gateway{
				Name:      "test-gateway",
				Namespace: "default",
			},
			listener: &pb.Listener{
				Name:     "tcp",
				Port:     9000,
				Protocol: pb.Protocol_TCP,
			},
			endpoints: map[string]*pb.EndpointList{
				"default/backend1": {
					Endpoints: []*pb.Endpoint{
						{Address: "10.0.0.1", Port: 8080},
						{Address: "10.0.0.2", Port: 8080},
					},
				},
			},
			expectedBackend: "backend1",
			expectedEPs:     2,
		},
		{
			name:   "no routes",
			routes: []runtime.Object{},
			gateway: &pb.Gateway{
				Name:      "test-gateway",
				Namespace: "default",
			},
			listener: &pb.Listener{
				Name:     "tcp",
				Port:     9000,
				Protocol: pb.Protocol_TCP,
			},
			endpoints:       make(map[string]*pb.EndpointList),
			expectedBackend: "",
			expectedEPs:     0,
		},
		{
			name: "route in different namespace",
			routes: []runtime.Object{
				&novaedgev1alpha1.ProxyRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "other-ns",
					},
					Spec: novaedgev1alpha1.ProxyRouteSpec{
						Rules: []novaedgev1alpha1.HTTPRouteRule{
							{
								BackendRefs: []novaedgev1alpha1.BackendRef{
									{
										Name: "backend1",
									},
								},
							},
						},
					},
				},
			},
			gateway: &pb.Gateway{
				Name:      "test-gateway",
				Namespace: "default",
			},
			listener: &pb.Listener{
				Name:     "tcp",
				Port:     9000,
				Protocol: pb.Protocol_TCP,
			},
			endpoints:       make(map[string]*pb.EndpointList),
			expectedBackend: "",
			expectedEPs:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tt.routes...).
				Build()

			builder := NewBuilder(fakeClient)
			ctx := context.Background()

			backendName, endpoints := builder.findL4BackendForListener(ctx, tt.gateway, tt.listener, tt.endpoints)

			if backendName != tt.expectedBackend {
				t.Errorf("Expected backend %q, got %q", tt.expectedBackend, backendName)
			}

			if len(endpoints) != tt.expectedEPs {
				t.Errorf("Expected %d endpoints, got %d", tt.expectedEPs, len(endpoints))
			}
		})
	}
}

func TestBuildL4ListenersIntegration(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Create a route with backend
	route := &novaedgev1alpha1.ProxyRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tcp-route",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyRouteSpec{
			Rules: []novaedgev1alpha1.HTTPRouteRule{
				{
					BackendRefs: []novaedgev1alpha1.BackendRef{
						{
							Name: "tcp-backend",
						},
					},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(route).
		Build()

	builder := NewBuilder(fakeClient)
	ctx := context.Background()

	gateways := []*pb.Gateway{
		{
			Name:      "test-gateway",
			Namespace: "default",
			Listeners: []*pb.Listener{
				{
					Name:     "tcp",
					Port:     9000,
					Protocol: pb.Protocol_TCP,
				},
			},
		},
	}

	endpoints := map[string]*pb.EndpointList{
		"default/tcp-backend": {
			Endpoints: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 8080},
			},
		},
	}

	t.Run("build TCP listener with backend", func(t *testing.T) {
		listeners := builder.buildL4Listeners(ctx, gateways, endpoints)
		
		if len(listeners) != 1 {
			t.Fatalf("Expected 1 listener, got %d", len(listeners))
		}

		listener := listeners[0]
		if listener.Protocol != pb.Protocol_TCP {
			t.Errorf("Expected TCP protocol, got %v", listener.Protocol)
		}
		if listener.Port != 9000 {
			t.Errorf("Expected port 9000, got %d", listener.Port)
		}
		if listener.BackendName != "tcp-backend" {
			t.Errorf("Expected backend tcp-backend, got %s", listener.BackendName)
		}
		if len(listener.Backends) != 1 {
			t.Errorf("Expected 1 backend endpoint, got %d", len(listener.Backends))
		}
		if listener.TcpConfig == nil {
			t.Error("Expected TCP config to be set")
		} else {
			if listener.TcpConfig.ConnectTimeoutMs != 5000 {
				t.Errorf("Expected connect timeout 5000ms, got %d", listener.TcpConfig.ConnectTimeoutMs)
			}
			if listener.TcpConfig.IdleTimeoutMs != 300000 {
				t.Errorf("Expected idle timeout 300000ms, got %d", listener.TcpConfig.IdleTimeoutMs)
			}
		}
	})
}
