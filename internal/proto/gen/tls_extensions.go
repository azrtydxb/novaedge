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

// Package proto contains generated protobuf types and hand-written extensions
// for features that have not yet been regenerated via protoc (mTLS, PROXY protocol, OCSP).
package proto

// ClientAuthConfig defines mTLS client authentication configuration.
// This type extends the Listener proto with mTLS settings.
type ClientAuthConfig struct {
	// Mode: "none", "optional", "require"
	Mode string `protobuf:"bytes,1,opt,name=mode,proto3" json:"mode,omitempty"`
	// CACert is the CA certificate bundle for client certificate verification
	CACert []byte `protobuf:"bytes,2,opt,name=ca_cert,json=caCert,proto3" json:"ca_cert,omitempty"`
	// RequiredCNPatterns are regex patterns that the client certificate CN must match
	RequiredCNPatterns []string `protobuf:"bytes,3,rep,name=required_cn_patterns,json=requiredCnPatterns,proto3" json:"required_cn_patterns,omitempty"`
	// RequiredSANs are Subject Alternative Names required on the client certificate
	RequiredSANs []string `protobuf:"bytes,4,rep,name=required_sans,json=requiredSans,proto3" json:"required_sans,omitempty"`
}

// ProxyProtocolConfig defines PROXY protocol listener configuration.
type ProxyProtocolConfig struct {
	// Enabled indicates whether PROXY protocol parsing is enabled
	Enabled bool `protobuf:"varint,1,opt,name=enabled,proto3" json:"enabled,omitempty"`
	// Version is the PROXY protocol version to accept (1, 2, or 0 for both)
	Version int32 `protobuf:"varint,2,opt,name=version,proto3" json:"version,omitempty"`
	// TrustedCIDRs are trusted source CIDRs (only accept PROXY protocol from these)
	TrustedCIDRs []string `protobuf:"bytes,3,rep,name=trusted_cidrs,json=trustedCidrs,proto3" json:"trusted_cidrs,omitempty"`
}

// UpstreamProxyProtocol defines PROXY protocol config for backend connections.
type UpstreamProxyProtocol struct {
	// Enabled indicates whether to send PROXY protocol header to backends
	Enabled bool `protobuf:"varint,1,opt,name=enabled,proto3" json:"enabled,omitempty"`
	// Version is the PROXY protocol version to send (1 or 2)
	Version int32 `protobuf:"varint,2,opt,name=version,proto3" json:"version,omitempty"`
}

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
