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

package v1alpha1

// HTTP3Config defines HTTP/3 (QUIC) protocol configuration for a gateway
type HTTP3Config struct {
	// Enabled enables HTTP/3 support on this gateway
	// +optional
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// Port is the UDP port for QUIC connections (defaults to same as HTTPS port)
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`

	// ZeroRTT enables 0-RTT connection resumption for faster handshakes
	// Note: 0-RTT data may be replayed by attackers; only enable for idempotent endpoints
	// +optional
	// +kubebuilder:default=false
	ZeroRTT bool `json:"zeroRTT,omitempty"`

	// MaxIdleTimeout is the maximum idle timeout for QUIC connections
	// +optional
	// +kubebuilder:default="30s"
	MaxIdleTimeout string `json:"maxIdleTimeout,omitempty"`

	// AltSvcMaxAge is the max-age for the Alt-Svc header in seconds
	// Clients cache the HTTP/3 availability for this duration
	// +optional
	// +kubebuilder:default=2592000
	AltSvcMaxAge int64 `json:"altSvcMaxAge,omitempty"`
}

// SSEConfig defines Server-Sent Events configuration for a gateway
type SSEConfig struct {
	// IdleTimeout is the maximum time an SSE connection can be idle before being closed
	// SSE connections typically need longer timeouts than regular HTTP requests
	// +optional
	// +kubebuilder:default="5m"
	IdleTimeout string `json:"idleTimeout,omitempty"`

	// HeartbeatInterval is the interval at which keepalive comments are sent
	// to prevent intermediate proxies from closing the connection
	// +optional
	// +kubebuilder:default="30s"
	HeartbeatInterval string `json:"heartbeatInterval,omitempty"`

	// MaxConnections is the maximum number of concurrent SSE connections per listener
	// 0 means unlimited
	// +optional
	MaxConnections int32 `json:"maxConnections,omitempty"`
}

// GRPCRouteConfig defines gRPC-specific routing configuration
type GRPCRouteConfig struct {
	// Enabled enables gRPC request routing through the Gateway API GRPCRoute resources
	// +optional
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`
}
