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

// Package proto contains generated protobuf types and hand-written extensions.
package proto

// ListenerExtensions holds mTLS, PROXY protocol, and OCSP settings for a Listener.
// These are distributed alongside the Listener proto in the ConfigSnapshot.
type ListenerExtensions struct {
	// ClientAuth configures mTLS client authentication
	ClientAuth *ClientAuthConfig `json:"client_auth,omitempty"`
	// OCSPStapling enables OCSP stapling for TLS certificates
	OCSPStapling bool `json:"ocsp_stapling,omitempty"`
	// ProxyProtocol configures PROXY protocol parsing on this listener
	ProxyProtocol *ProxyProtocolConfig `json:"proxy_protocol,omitempty"`
}

// ClusterExtensions holds additional configuration for a Cluster (backend).
type ClusterExtensions struct {
	// UpstreamProxyProtocol configures sending PROXY protocol to backends
	UpstreamProxyProtocol *UpstreamProxyProtocol `json:"upstream_proxy_protocol,omitempty"`
}
