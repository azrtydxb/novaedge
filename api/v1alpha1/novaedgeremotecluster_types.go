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

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NovaEdgeRemoteClusterSpec defines the desired state for remote cluster configuration.
// It represents a remote/edge cluster that runs agents
// connecting back to the hub cluster's controller
type NovaEdgeRemoteClusterSpec struct {
	// ClusterName is a unique identifier for this remote cluster
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	ClusterName string `json:"clusterName"`

	// Region is the geographic region of the remote cluster
	// +optional
	Region string `json:"region,omitempty"`

	// Zone is the availability zone of the remote cluster
	// +optional
	Zone string `json:"zone,omitempty"`

	// Labels are additional labels to apply to resources from this cluster
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Connection defines how agents in this cluster connect to the hub controller
	// +kubebuilder:validation:Required
	Connection RemoteClusterConnection `json:"connection"`

	// Agent defines the configuration for agents in this remote cluster
	// +optional
	Agent *RemoteAgentSpec `json:"agent,omitempty"`

	// Routing defines how traffic should be routed to/from this cluster
	// +optional
	Routing *RemoteClusterRouting `json:"routing,omitempty"`

	// HealthCheck defines how the hub monitors this remote cluster
	// +optional
	HealthCheck *RemoteClusterHealthCheck `json:"healthCheck,omitempty"`

	// Paused suspends reconciliation for this remote cluster
	// +kubebuilder:default=false
	// +optional
	Paused bool `json:"paused,omitempty"`

	// OverlayCIDR is the overlay network CIDR assigned to this remote cluster
	// for site-to-site routing (e.g., "10.200.1.0/24").
	// +optional
	OverlayCIDR string `json:"overlayCIDR,omitempty"`
}

// RemoteClusterConnection defines how agents connect to the hub controller
type RemoteClusterConnection struct {
	// Mode is the connection mode
	// - Direct: Agents connect directly to the hub controller (requires network connectivity)
	// - Tunnel: Agents connect through a tunnel/relay (for NAT/firewall traversal)
	// +kubebuilder:default="Direct"
	// +kubebuilder:validation:Enum=Direct;Tunnel
	// +optional
	Mode ConnectionMode `json:"mode,omitempty"`

	// ControllerEndpoint is the address of the hub controller's gRPC endpoint
	// For Direct mode: "controller.novaedge-system.svc.cluster.local:9090" or external address
	// For Tunnel mode: tunnel relay address
	// +kubebuilder:validation:Required
	ControllerEndpoint string `json:"controllerEndpoint"`

	// TLS defines the mTLS configuration for secure communication
	// +optional
	TLS *RemoteClusterTLS `json:"tls,omitempty"`

	// Tunnel defines tunnel-specific configuration (when Mode=Tunnel)
	// +optional
	Tunnel *TunnelConfig `json:"tunnel,omitempty"`

	// ReconnectInterval is how often to attempt reconnection if disconnected
	// +kubebuilder:default="30s"
	// +optional
	ReconnectInterval *metav1.Duration `json:"reconnectInterval,omitempty"`

	// Timeout is the connection timeout
	// +kubebuilder:default="10s"
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`
}

// ConnectionMode defines the agent connection mode
type ConnectionMode string

const (
	// ConnectionModeDirect means agents connect directly to the controller
	ConnectionModeDirect ConnectionMode = "Direct"
	// ConnectionModeTunnel means agents connect through a tunnel/relay
	ConnectionModeTunnel ConnectionMode = "Tunnel"
)

// RemoteClusterTLS defines mTLS configuration for remote cluster communication
type RemoteClusterTLS struct {
	// Enabled enables mTLS for agent-controller communication
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// CASecretRef references a secret containing the CA certificate for validating the controller
	// The secret must be created in the remote cluster
	// +optional
	CASecretRef *corev1.SecretReference `json:"caSecretRef,omitempty"`

	// ClientCertSecretRef references a secret containing the client certificate and key
	// This secret is used by agents to authenticate to the controller
	// +optional
	ClientCertSecretRef *corev1.SecretReference `json:"clientCertSecretRef,omitempty"`

	// ServerName is the expected server name for TLS verification
	// +optional
	ServerName string `json:"serverName,omitempty"`

	// InsecureSkipVerify skips TLS certificate verification (NOT recommended for production)
	// +kubebuilder:default=false
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
}

// TunnelConfig defines tunnel-specific configuration
type TunnelConfig struct {
	// Type is the tunnel type
	// +kubebuilder:default="WireGuard"
	// +kubebuilder:validation:Enum=WireGuard;SSH;WebSocket
	// +optional
	Type TunnelType `json:"type,omitempty"`

	// RelayEndpoint is the address of the tunnel relay server
	// +optional
	RelayEndpoint string `json:"relayEndpoint,omitempty"`

	// WireGuard defines WireGuard-specific configuration
	// +optional
	WireGuard *WireGuardConfig `json:"wireGuard,omitempty"`
}

// TunnelType defines the type of tunnel
type TunnelType string

const (
	// TunnelTypeWireGuard uses WireGuard for tunneling
	TunnelTypeWireGuard TunnelType = "WireGuard"
	// TunnelTypeSSH uses SSH for tunneling
	TunnelTypeSSH TunnelType = "SSH"
	// TunnelTypeWebSocket uses WebSocket for tunneling
	TunnelTypeWebSocket TunnelType = "WebSocket"
)

// WireGuardConfig defines WireGuard tunnel configuration
type WireGuardConfig struct {
	// PrivateKeySecretRef references a secret containing the WireGuard private key
	// +optional
	PrivateKeySecretRef *corev1.SecretKeySelector `json:"privateKeySecretRef,omitempty"`

	// PublicKey is the hub's WireGuard public key
	// +optional
	PublicKey string `json:"publicKey,omitempty"`

	// Endpoint is the hub's WireGuard endpoint
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// AllowedIPs are the allowed IP ranges for the tunnel
	// +optional
	AllowedIPs []string `json:"allowedIPs,omitempty"`

	// PersistentKeepalive is the keepalive interval in seconds
	// +kubebuilder:default=25
	// +optional
	PersistentKeepalive *int32 `json:"persistentKeepalive,omitempty"`

	// ListenPort is the local UDP port for the WireGuard interface.
	// +optional
	ListenPort *int32 `json:"listenPort,omitempty"`
}

// RemoteAgentSpec defines agent configuration specific to this remote cluster
type RemoteAgentSpec struct {
	// Version overrides the agent version for this cluster
	// If not set, uses the hub cluster's default version
	// +optional
	Version string `json:"version,omitempty"`

	// NodeSelector defines which nodes should run agents in the remote cluster
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations for agent pods in the remote cluster
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Resources defines resource requirements for agents in this cluster
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// VIP defines VIP configuration for this remote cluster
	// +optional
	VIP *VIPConfig `json:"vip,omitempty"`

	// ExtraArgs are additional arguments passed to agents
	// +optional
	ExtraArgs []string `json:"extraArgs,omitempty"`

	// ExtraEnv are additional environment variables for agents
	// +optional
	ExtraEnv []corev1.EnvVar `json:"extraEnv,omitempty"`

	// ExtraLabels are additional labels for agent pods
	// +optional
	ExtraLabels map[string]string `json:"extraLabels,omitempty"`

	// ExtraAnnotations are additional annotations for agent pods
	// +optional
	ExtraAnnotations map[string]string `json:"extraAnnotations,omitempty"`
}

// RemoteClusterRouting defines routing configuration for the remote cluster
type RemoteClusterRouting struct {
	// Enabled enables routing of traffic to/from this cluster
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Priority is the routing priority (lower = higher priority)
	// Used for failover between clusters
	// +kubebuilder:default=100
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	// +optional
	Priority *int32 `json:"priority,omitempty"`

	// Weight is the traffic weight for weighted routing
	// +kubebuilder:default=100
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +optional
	Weight *int32 `json:"weight,omitempty"`

	// LocalPreference enables preferring local backends within the cluster
	// +kubebuilder:default=true
	// +optional
	LocalPreference *bool `json:"localPreference,omitempty"`

	// AllowCrossClusterTraffic allows traffic to be routed across clusters
	// +kubebuilder:default=true
	// +optional
	AllowCrossClusterTraffic *bool `json:"allowCrossClusterTraffic,omitempty"`

	// Endpoints restricts which endpoints from this cluster should be included
	// +optional
	Endpoints *EndpointSelector `json:"endpoints,omitempty"`
}

// EndpointSelector defines which endpoints to include/exclude
type EndpointSelector struct {
	// MatchLabels selects endpoints with matching labels
	// +optional
	MatchLabels map[string]string `json:"matchLabels,omitempty"`

	// MatchExpressions selects endpoints matching expressions
	// +optional
	MatchExpressions []metav1.LabelSelectorRequirement `json:"matchExpressions,omitempty"`

	// Namespaces restricts endpoints to specific namespaces
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`

	// ExcludeNamespaces excludes endpoints from specific namespaces
	// +optional
	ExcludeNamespaces []string `json:"excludeNamespaces,omitempty"`
}

// RemoteClusterHealthCheck defines health check configuration
type RemoteClusterHealthCheck struct {
	// Enabled enables health checking of the remote cluster
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Interval is the health check interval
	// +kubebuilder:default="30s"
	// +optional
	Interval *metav1.Duration `json:"interval,omitempty"`

	// Timeout is the health check timeout
	// +kubebuilder:default="10s"
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// HealthyThreshold is the number of consecutive successes required
	// +kubebuilder:default=2
	// +kubebuilder:validation:Minimum=1
	// +optional
	HealthyThreshold *int32 `json:"healthyThreshold,omitempty"`

	// UnhealthyThreshold is the number of consecutive failures required
	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=1
	// +optional
	UnhealthyThreshold *int32 `json:"unhealthyThreshold,omitempty"`

	// FailoverEnabled enables automatic failover when cluster becomes unhealthy
	// +kubebuilder:default=true
	// +optional
	FailoverEnabled *bool `json:"failoverEnabled,omitempty"`
}

// NovaEdgeRemoteClusterStatus defines the observed state of NovaEdgeRemoteCluster
type NovaEdgeRemoteClusterStatus struct {
	// Phase is the current phase of the remote cluster
	// +optional
	Phase RemoteClusterPhase `json:"phase,omitempty"`

	// ObservedGeneration is the generation last observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Connection is the current connection status
	// +optional
	Connection *ConnectionStatus `json:"connection,omitempty"`

	// Agents is the status of agents in this remote cluster
	// +optional
	Agents *RemoteAgentStatus `json:"agents,omitempty"`

	// LastHeartbeat is the timestamp of the last heartbeat from agents
	// +optional
	LastHeartbeat *metav1.Time `json:"lastHeartbeat,omitempty"`

	// LastConfigSync is the timestamp of the last successful configuration sync
	// +optional
	LastConfigSync *metav1.Time `json:"lastConfigSync,omitempty"`

	// Version is the agent version running in the remote cluster
	// +optional
	Version string `json:"version,omitempty"`
}

// RemoteClusterPhase represents the phase of the remote cluster
// +kubebuilder:validation:Enum=Pending;Connecting;Connected;Degraded;Disconnected;Failed
type RemoteClusterPhase string

const (
	// RemoteClusterPhasePending indicates the remote cluster is pending
	RemoteClusterPhasePending RemoteClusterPhase = "Pending"
	// RemoteClusterPhaseConnecting indicates connection is being established
	RemoteClusterPhaseConnecting RemoteClusterPhase = "Connecting"
	// RemoteClusterPhaseConnected indicates the remote cluster is connected
	RemoteClusterPhaseConnected RemoteClusterPhase = "Connected"
	// RemoteClusterPhaseDegraded indicates the remote cluster is degraded
	RemoteClusterPhaseDegraded RemoteClusterPhase = "Degraded"
	// RemoteClusterPhaseDisconnected indicates the remote cluster is disconnected
	RemoteClusterPhaseDisconnected RemoteClusterPhase = "Disconnected"
	// RemoteClusterPhaseFailed indicates the remote cluster has failed
	RemoteClusterPhaseFailed RemoteClusterPhase = "Failed"
)

// ConnectionStatus represents the current connection status
type ConnectionStatus struct {
	// Connected indicates if there are active connections from the remote cluster
	// +optional
	Connected bool `json:"connected,omitempty"`

	// ActiveConnections is the number of active agent connections
	// +optional
	ActiveConnections int32 `json:"activeConnections,omitempty"`

	// LastConnected is when the last connection was established
	// +optional
	LastConnected *metav1.Time `json:"lastConnected,omitempty"`

	// LastDisconnected is when the last disconnection occurred
	// +optional
	LastDisconnected *metav1.Time `json:"lastDisconnected,omitempty"`

	// Error is the last connection error message
	// +optional
	Error string `json:"error,omitempty"`

	// Latency is the average round-trip latency to the remote cluster
	// +optional
	Latency string `json:"latency,omitempty"`
}

// RemoteAgentStatus represents the status of agents in the remote cluster
type RemoteAgentStatus struct {
	// Total is the total number of expected agents
	// +optional
	Total int32 `json:"total,omitempty"`

	// Ready is the number of ready agents
	// +optional
	Ready int32 `json:"ready,omitempty"`

	// Healthy is the number of healthy agents
	// +optional
	Healthy int32 `json:"healthy,omitempty"`

	// Unhealthy is the number of unhealthy agents
	// +optional
	Unhealthy int32 `json:"unhealthy,omitempty"`

	// Nodes lists the nodes with agent status
	// +optional
	Nodes []RemoteAgentNodeStatus `json:"nodes,omitempty"`
}

// RemoteAgentNodeStatus represents agent status on a specific node
type RemoteAgentNodeStatus struct {
	// Name is the node name
	// +optional
	Name string `json:"name,omitempty"`

	// Ready indicates if the agent is ready
	// +optional
	Ready bool `json:"ready,omitempty"`

	// IP is the node's IP address
	// +optional
	IP string `json:"ip,omitempty"`

	// Version is the agent version on this node
	// +optional
	Version string `json:"version,omitempty"`

	// VIPs are the VIPs assigned to this node
	// +optional
	VIPs []string `json:"vips,omitempty"`

	// LastSeen is when the agent was last seen
	// +optional
	LastSeen *metav1.Time `json:"lastSeen,omitempty"`

	// ActiveConnections is the number of active connections on this node
	// +optional
	ActiveConnections int32 `json:"activeConnections,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=nerc
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterName`
// +kubebuilder:printcolumn:name="Region",type=string,JSONPath=`.spec.region`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Connected",type=boolean,JSONPath=`.status.connection.connected`
// +kubebuilder:printcolumn:name="Agents",type=string,JSONPath=`.status.agents.ready`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// NovaEdgeRemoteCluster is the Schema for the novaedgeremoteclusters API
// It represents a remote/edge cluster in a hub-spoke multi-cluster deployment
type NovaEdgeRemoteCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NovaEdgeRemoteClusterSpec   `json:"spec,omitempty"`
	Status NovaEdgeRemoteClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NovaEdgeRemoteClusterList contains a list of NovaEdgeRemoteCluster
type NovaEdgeRemoteClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NovaEdgeRemoteCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NovaEdgeRemoteCluster{}, &NovaEdgeRemoteClusterList{})
}
