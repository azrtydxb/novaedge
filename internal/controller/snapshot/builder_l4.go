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
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/log"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// buildL4Listeners builds L4 listener configurations from gateways and backends
func (b *Builder) buildL4Listeners(ctx context.Context, gateways []*pb.Gateway, endpoints map[string]*pb.EndpointList, bc *buildContext) []*pb.L4Listener {
	logger := log.FromContext(ctx)
	var l4Listeners []*pb.L4Listener

	for _, gw := range gateways {
		for _, listener := range gw.Listeners {
			switch listener.Protocol {
			case pb.Protocol_TCP:
				l4Listener := b.buildTCPListener(ctx, gw, listener, endpoints, bc)
				if l4Listener != nil {
					l4Listeners = append(l4Listeners, l4Listener)
				}
			case pb.Protocol_TLS:
				l4Listener := b.buildTLSPassthroughListener(ctx, gw, listener, endpoints, bc)
				if l4Listener != nil {
					l4Listeners = append(l4Listeners, l4Listener)
				}
			case pb.Protocol_UDP:
				l4Listener := b.buildUDPListener(ctx, gw, listener, endpoints, bc)
				if l4Listener != nil {
					l4Listeners = append(l4Listeners, l4Listener)
				}
			default:
				// HTTP/HTTPS/HTTP3 listeners are handled by the HTTP server
				continue
			}
		}
	}

	logger.Info("Built L4 listeners", "count", len(l4Listeners))
	return l4Listeners
}

// buildTCPListener builds a TCP L4 listener from gateway and route configuration
func (b *Builder) buildTCPListener(ctx context.Context, gw *pb.Gateway, listener *pb.Listener, endpoints map[string]*pb.EndpointList, bc *buildContext) *pb.L4Listener {
	logger := log.FromContext(ctx)

	// Find matching routes for this TCP listener
	// For TCP, routes are matched by gateway reference (routes with no hostnames binding to TCP listeners)
	l4Listener := &pb.L4Listener{
		Name:     fmt.Sprintf("%s-%s-%s", gw.Namespace, gw.Name, listener.Name),
		Port:     listener.Port,
		Protocol: pb.Protocol_TCP,
	}

	// Find TCPRoute-style backends for this listener
	// We look at the routes that match this gateway's TCP listeners
	backendName, backendEndpoints := b.findL4BackendForListener(ctx, gw, listener, endpoints, bc)
	if backendName == "" {
		logger.Info("No backends found for TCP listener",
			"gateway", gw.Name,
			"listener", listener.Name)
		return nil
	}

	l4Listener.BackendName = backendName
	l4Listener.Backends = backendEndpoints

	// TCP configuration with defaults
	l4Listener.TcpConfig = &pb.L4TCPConfig{
		ConnectTimeoutMs: 5000,
		IdleTimeoutMs:    300000,
		BufferSize:       32768,
		DrainTimeoutMs:   30000,
	}

	return l4Listener
}

// buildTLSPassthroughListener builds a TLS passthrough L4 listener
func (b *Builder) buildTLSPassthroughListener(ctx context.Context, gw *pb.Gateway, listener *pb.Listener, endpoints map[string]*pb.EndpointList, bc *buildContext) *pb.L4Listener {
	logger := log.FromContext(ctx)

	l4Listener := &pb.L4Listener{
		Name:     fmt.Sprintf("%s-%s-%s", gw.Namespace, gw.Name, listener.Name),
		Port:     listener.Port,
		Protocol: pb.Protocol_TLS,
	}

	// For TLS passthrough, routes are mapped by SNI hostname
	// Each hostname maps to a backend
	var tlsRoutes []*pb.L4TLSRoute

	// Use listener hostnames to build TLS routes
	if len(listener.Hostnames) > 0 {
		backendName, backendEndpoints := b.findL4BackendForListener(ctx, gw, listener, endpoints, bc)
		if backendName != "" {
			for _, hostname := range listener.Hostnames {
				tlsRoutes = append(tlsRoutes, &pb.L4TLSRoute{
					Hostname:    hostname,
					BackendName: backendName,
					Backends:    backendEndpoints,
				})
			}
		}
	}

	if len(tlsRoutes) == 0 {
		logger.Info("No TLS routes found for TLS passthrough listener",
			"gateway", gw.Name,
			"listener", listener.Name)
		return nil
	}

	l4Listener.TlsRoutes = tlsRoutes

	return l4Listener
}

// buildUDPListener builds a UDP L4 listener from gateway and route configuration
func (b *Builder) buildUDPListener(ctx context.Context, gw *pb.Gateway, listener *pb.Listener, endpoints map[string]*pb.EndpointList, bc *buildContext) *pb.L4Listener {
	logger := log.FromContext(ctx)

	l4Listener := &pb.L4Listener{
		Name:     fmt.Sprintf("%s-%s-%s", gw.Namespace, gw.Name, listener.Name),
		Port:     listener.Port,
		Protocol: pb.Protocol_UDP,
	}

	backendName, backendEndpoints := b.findL4BackendForListener(ctx, gw, listener, endpoints, bc)
	if backendName == "" {
		logger.Info("No backends found for UDP listener",
			"gateway", gw.Name,
			"listener", listener.Name)
		return nil
	}

	l4Listener.BackendName = backendName
	l4Listener.Backends = backendEndpoints

	// UDP configuration with defaults
	l4Listener.UdpConfig = &pb.L4UDPConfig{
		SessionTimeoutMs: 30000,
		BufferSize:       65535,
	}

	return l4Listener
}

// findL4BackendForListener finds the backend and endpoints for an L4 listener
// It searches pre-fetched routes that reference backends bound to this gateway
func (b *Builder) findL4BackendForListener(_ context.Context, gw *pb.Gateway, _ *pb.Listener, endpoints map[string]*pb.EndpointList, bc *buildContext) (string, []*pb.Endpoint) {
	// Find routes that belong to this gateway's namespace from pre-fetched cache
	for _, route := range bc.routes {
		if route.Namespace != gw.Namespace {
			continue
		}

		for _, rule := range route.Spec.Rules {
			for _, backendRef := range rule.BackendRefs {
				backendNS := gw.Namespace
				if backendRef.Namespace != nil {
					backendNS = *backendRef.Namespace
				}
				clusterKey := fmt.Sprintf("%s/%s", backendNS, backendRef.Name)
				if epList, ok := endpoints[clusterKey]; ok {
					return backendRef.Name, epList.Endpoints
				}
			}
		}
	}

	return "", nil
}
