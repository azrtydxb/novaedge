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

// NovaEdgeClusterSpec defines the desired state of NovaEdgeCluster
type NovaEdgeClusterSpec struct {
	// Version is the version of NovaEdge components to deploy
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^v?[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9]+)?$`
	Version string `json:"version"`

	// ImageRepository is the container image repository for NovaEdge images
	// +kubebuilder:default="ghcr.io/azrtydxb/novaedge"
	// +optional
	ImageRepository string `json:"imageRepository,omitempty"`

	// ImagePullPolicy is the pull policy for NovaEdge images
	// +kubebuilder:default="IfNotPresent"
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// ImagePullSecrets is a list of secrets for pulling images
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// Controller defines the configuration for the NovaEdge controller
	// +kubebuilder:validation:Required
	Controller ControllerSpec `json:"controller"`

	// Agent defines the configuration for the NovaEdge agent DaemonSet
	// +kubebuilder:validation:Required
	Agent AgentSpec `json:"agent"`

	// WebUI defines the configuration for the NovaEdge web UI (optional)
	// +optional
	WebUI *WebUISpec `json:"webUI,omitempty"`

	// TLS defines the TLS configuration for internal communication
	// +optional
	TLS *ClusterTLSSpec `json:"tls,omitempty"`

	// Observability defines the observability configuration
	// +optional
	Observability *ObservabilitySpec `json:"observability,omitempty"`
}

// ControllerSpec defines the configuration for the NovaEdge controller
type ControllerSpec struct {
	// Replicas is the number of controller replicas
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=5
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Resources defines the resource requirements for the controller
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// NodeSelector defines the node selector for controller pods
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations defines the tolerations for controller pods
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Affinity defines the affinity rules for controller pods
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// LeaderElection enables leader election for high availability
	// +kubebuilder:default=true
	// +optional
	LeaderElection *bool `json:"leaderElection,omitempty"`

	// GRPCPort is the port for the gRPC config distribution server
	// +kubebuilder:default=9090
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	GRPCPort *int32 `json:"grpcPort,omitempty"`

	// MetricsPort is the port for Prometheus metrics
	// +kubebuilder:default=8080
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	MetricsPort *int32 `json:"metricsPort,omitempty"`

	// HealthPort is the port for health probes
	// +kubebuilder:default=8081
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	HealthPort *int32 `json:"healthPort,omitempty"`

	// ServiceAccount defines the service account configuration
	// +optional
	ServiceAccount *ServiceAccountSpec `json:"serviceAccount,omitempty"`

	// PodSecurityContext defines the security context for controller pods
	// +optional
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`

	// SecurityContext defines the security context for the controller container
	// +optional
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`

	// ExtraArgs defines additional command-line arguments for the controller
	// +optional
	ExtraArgs []string `json:"extraArgs,omitempty"`

	// ExtraEnv defines additional environment variables for the controller
	// +optional
	ExtraEnv []corev1.EnvVar `json:"extraEnv,omitempty"`
}

// AgentSpec defines the configuration for the NovaEdge agent DaemonSet
type AgentSpec struct {
	// Resources defines the resource requirements for agent pods
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// NodeSelector defines the node selector for agent pods
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations defines the tolerations for agent pods
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// HostNetwork enables host networking for the agent
	// +kubebuilder:default=true
	// +optional
	HostNetwork *bool `json:"hostNetwork,omitempty"`

	// DNSPolicy defines the DNS policy for agent pods
	// +kubebuilder:default="ClusterFirstWithHostNet"
	// +optional
	DNSPolicy corev1.DNSPolicy `json:"dnsPolicy,omitempty"`

	// HTTPPort is the port for HTTP traffic
	// +kubebuilder:default=80
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	HTTPPort *int32 `json:"httpPort,omitempty"`

	// HTTPSPort is the port for HTTPS traffic
	// +kubebuilder:default=443
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	HTTPSPort *int32 `json:"httpsPort,omitempty"`

	// MetricsPort is the port for Prometheus metrics
	// +kubebuilder:default=9090
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	MetricsPort *int32 `json:"metricsPort,omitempty"`

	// HealthPort is the port for health probes
	// +kubebuilder:default=8080
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	HealthPort *int32 `json:"healthPort,omitempty"`

	// ServiceAccount defines the service account configuration
	// +optional
	ServiceAccount *ServiceAccountSpec `json:"serviceAccount,omitempty"`

	// PodSecurityContext defines the security context for agent pods
	// +optional
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`

	// SecurityContext defines the security context for the agent container
	// +optional
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`

	// ExtraArgs defines additional command-line arguments for the agent
	// +optional
	ExtraArgs []string `json:"extraArgs,omitempty"`

	// ExtraEnv defines additional environment variables for the agent
	// +optional
	ExtraEnv []corev1.EnvVar `json:"extraEnv,omitempty"`

	// ExtraVolumes defines additional volumes to mount
	// +optional
	ExtraVolumes []corev1.Volume `json:"extraVolumes,omitempty"`

	// ExtraVolumeMounts defines additional volume mounts
	// +optional
	ExtraVolumeMounts []corev1.VolumeMount `json:"extraVolumeMounts,omitempty"`

	// UpdateStrategy defines the update strategy for the DaemonSet
	// +optional
	UpdateStrategy *AgentUpdateStrategy `json:"updateStrategy,omitempty"`

	// Controllers defines the controller connection configuration for federated deployments
	// If not specified, agents connect to the local controller in the same cluster
	// +optional
	Controllers *AgentControllerConfig `json:"controllers,omitempty"`
}

// AgentControllerConfig defines the controller connection configuration for agents
type AgentControllerConfig struct {
	// Primary is the primary controller endpoint (highest priority)
	// If not specified, agents use the local controller in the same cluster
	// +optional
	Primary *ControllerEndpoint `json:"primary,omitempty"`

	// Secondary is an ordered list of secondary/failover controllers
	// +optional
	Secondary []ControllerEndpoint `json:"secondary,omitempty"`

	// Failover defines the failover behavior
	// +optional
	Failover *AgentFailoverConfig `json:"failover,omitempty"`

	// AutonomousMode defines behavior when all controllers are unavailable
	// +optional
	AutonomousMode *AutonomousModeConfig `json:"autonomousMode,omitempty"`
}

// ControllerEndpoint defines a controller endpoint for agent connection
type ControllerEndpoint struct {
	// Endpoint is the gRPC endpoint of the controller
	// Format: "host:port"
	// +kubebuilder:validation:Required
	Endpoint string `json:"endpoint"`

	// Priority determines failover order (lower = higher priority)
	// +kubebuilder:default=100
	// +optional
	Priority int32 `json:"priority,omitempty"`

	// TLS defines the TLS configuration for this controller
	// +optional
	TLS *ControllerTLS `json:"tls,omitempty"`

	// Region is the region of this controller (for latency-aware routing)
	// +optional
	Region string `json:"region,omitempty"`
}

// ControllerTLS defines TLS configuration for controller connection
type ControllerTLS struct {
	// Enabled enables TLS for controller communication
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// SecretRef references a secret containing TLS credentials
	// The secret should contain: ca.crt, tls.crt, tls.key
	// +optional
	SecretRef *corev1.SecretReference `json:"secretRef,omitempty"`

	// ServerName is the expected server name for TLS verification
	// +optional
	ServerName string `json:"serverName,omitempty"`

	// InsecureSkipVerify skips TLS certificate verification (NOT recommended)
	// +kubebuilder:default=false
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
}

// AgentFailoverConfig defines the failover behavior for agents
type AgentFailoverConfig struct {
	// Timeout is how long to wait before failing over to secondary
	// +kubebuilder:default="30s"
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// HealthCheckInterval is how often to check controller health
	// +kubebuilder:default="10s"
	// +optional
	HealthCheckInterval *metav1.Duration `json:"healthCheckInterval,omitempty"`

	// FailureThreshold is the number of failures before failover
	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=1
	// +optional
	FailureThreshold int32 `json:"failureThreshold,omitempty"`

	// RecoveryDelay is how long to wait before returning to primary
	// +kubebuilder:default="60s"
	// +optional
	RecoveryDelay *metav1.Duration `json:"recoveryDelay,omitempty"`

	// LatencyAware prefers controllers with lower latency during failover
	// +kubebuilder:default=false
	// +optional
	LatencyAware bool `json:"latencyAware,omitempty"`
}

// AutonomousModeConfig defines behavior when all controllers are unavailable
type AutonomousModeConfig struct {
	// Enabled enables autonomous mode when disconnected from all controllers
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// ConfigPersistPath is the path to persist configuration for restart resilience
	// +kubebuilder:default="/var/lib/novaedge/config.json"
	// +optional
	ConfigPersistPath string `json:"configPersistPath,omitempty"`

	// LocalVIPCoordination enables agent-to-agent VIP coordination when disconnected
	// +kubebuilder:default=true
	// +optional
	LocalVIPCoordination *bool `json:"localVIPCoordination,omitempty"`

	// QueueRetention is how long to keep queued updates for later sync
	// +kubebuilder:default="24h"
	// +optional
	QueueRetention *metav1.Duration `json:"queueRetention,omitempty"`
}

// AgentUpdateStrategy defines the update strategy for the agent DaemonSet
type AgentUpdateStrategy struct {
	// Type is the update strategy type
	// +kubebuilder:default="RollingUpdate"
	// +kubebuilder:validation:Enum=RollingUpdate;OnDelete
	// +optional
	Type string `json:"type,omitempty"`

	// MaxUnavailable is the maximum number of unavailable pods during update
	// +optional
	MaxUnavailable *int32 `json:"maxUnavailable,omitempty"`
}

// WebUISpec defines the configuration for the NovaEdge web UI
type WebUISpec struct {
	// Enabled enables the web UI deployment
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Replicas is the number of web UI replicas
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Resources defines the resource requirements for web UI pods
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// NodeSelector defines the node selector for web UI pods
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations defines the tolerations for web UI pods
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Port is the port for the web UI
	// +kubebuilder:default=9080
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port *int32 `json:"port,omitempty"`

	// ReadOnly enables read-only mode
	// +kubebuilder:default=false
	// +optional
	ReadOnly *bool `json:"readOnly,omitempty"`

	// Service defines the service configuration for the web UI
	// +optional
	Service *WebUIServiceSpec `json:"service,omitempty"`

	// Ingress defines the ingress configuration for the web UI
	// +optional
	Ingress *WebUIIngressSpec `json:"ingress,omitempty"`

	// TLS defines the TLS configuration for the web UI
	// +optional
	TLS *WebUITLSSpec `json:"tls,omitempty"`

	// PrometheusEndpoint is the Prometheus endpoint for metrics
	// +optional
	PrometheusEndpoint string `json:"prometheusEndpoint,omitempty"`

	// ServiceAccount defines the service account configuration
	// +optional
	ServiceAccount *ServiceAccountSpec `json:"serviceAccount,omitempty"`

	// ExtraArgs defines additional command-line arguments
	// +optional
	ExtraArgs []string `json:"extraArgs,omitempty"`

	// ExtraEnv defines additional environment variables
	// +optional
	ExtraEnv []corev1.EnvVar `json:"extraEnv,omitempty"`
}

// WebUIServiceSpec defines the service configuration for the web UI
type WebUIServiceSpec struct {
	// Type is the service type
	// +kubebuilder:default="ClusterIP"
	// +kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer
	// +optional
	Type corev1.ServiceType `json:"type,omitempty"`

	// NodePort is the node port (only for NodePort/LoadBalancer types)
	// +optional
	NodePort *int32 `json:"nodePort,omitempty"`

	// Annotations defines additional annotations for the service
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// WebUIIngressSpec defines the ingress configuration for the web UI
type WebUIIngressSpec struct {
	// Enabled enables ingress for the web UI
	// +kubebuilder:default=false
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// ClassName is the ingress class name
	// +optional
	ClassName *string `json:"className,omitempty"`

	// Host is the hostname for the ingress
	// +optional
	Host string `json:"host,omitempty"`

	// Annotations defines additional annotations for the ingress
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// TLS enables TLS for the ingress
	// +optional
	TLS *IngressTLSSpec `json:"tls,omitempty"`
}

// IngressTLSSpec defines the TLS configuration for ingress
type IngressTLSSpec struct {
	// Enabled enables TLS
	// +kubebuilder:default=false
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// SecretName is the name of the TLS secret
	// +optional
	SecretName string `json:"secretName,omitempty"`
}

// WebUITLSSpec defines the TLS configuration for the web UI server
type WebUITLSSpec struct {
	// Enabled enables TLS for the web UI server
	// +kubebuilder:default=false
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Auto enables auto-generated self-signed certificates
	// +kubebuilder:default=false
	// +optional
	Auto *bool `json:"auto,omitempty"`

	// CertSecretRef references a secret containing the TLS certificate
	// +optional
	CertSecretRef *corev1.SecretKeySelector `json:"certSecretRef,omitempty"`

	// KeySecretRef references a secret containing the TLS private key
	// +optional
	KeySecretRef *corev1.SecretKeySelector `json:"keySecretRef,omitempty"`
}

// ClusterTLSSpec defines the TLS configuration for internal communication
type ClusterTLSSpec struct {
	// Enabled enables mTLS for internal communication
	// +kubebuilder:default=false
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// CertManager enables automatic certificate management with cert-manager
	// +optional
	CertManager *CertManagerSpec `json:"certManager,omitempty"`

	// CA references a secret containing the CA certificate
	// +optional
	CASecretRef *corev1.SecretKeySelector `json:"caSecretRef,omitempty"`
}

// CertManagerSpec defines the cert-manager configuration
type CertManagerSpec struct {
	// Enabled enables cert-manager integration
	// +kubebuilder:default=false
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// IssuerRef references the cert-manager issuer to use
	// +optional
	IssuerRef *CertManagerIssuerRef `json:"issuerRef,omitempty"`
}

// CertManagerIssuerRef references a cert-manager issuer
type CertManagerIssuerRef struct {
	// Name is the name of the issuer
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Kind is the kind of the issuer (Issuer or ClusterIssuer)
	// +kubebuilder:default="ClusterIssuer"
	// +kubebuilder:validation:Enum=Issuer;ClusterIssuer
	// +optional
	Kind string `json:"kind,omitempty"`

	// Group is the API group of the issuer
	// +kubebuilder:default="cert-manager.io"
	// +optional
	Group string `json:"group,omitempty"`
}

// ObservabilitySpec defines the observability configuration
type ObservabilitySpec struct {
	// Metrics defines the metrics configuration
	// +optional
	Metrics *MetricsSpec `json:"metrics,omitempty"`

	// Tracing defines the tracing configuration
	// +optional
	Tracing *TracingSpec `json:"tracing,omitempty"`

	// Logging defines the logging configuration
	// +optional
	Logging *LoggingSpec `json:"logging,omitempty"`
}

// MetricsSpec defines the metrics configuration
type MetricsSpec struct {
	// Enabled enables Prometheus metrics
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// ServiceMonitor enables creation of a ServiceMonitor for Prometheus Operator
	// +optional
	ServiceMonitor *ServiceMonitorSpec `json:"serviceMonitor,omitempty"`
}

// ServiceMonitorSpec defines the ServiceMonitor configuration
type ServiceMonitorSpec struct {
	// Enabled enables creation of a ServiceMonitor
	// +kubebuilder:default=false
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Interval is the scrape interval
	// +kubebuilder:default="30s"
	// +optional
	Interval string `json:"interval,omitempty"`

	// Labels defines additional labels for the ServiceMonitor
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

// TracingSpec defines the tracing configuration
type TracingSpec struct {
	// Enabled enables distributed tracing
	// +kubebuilder:default=false
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Endpoint is the OTLP endpoint for trace export
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// SamplingRate is the trace sampling rate (0-100)
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +optional
	SamplingRate *int32 `json:"samplingRate,omitempty"`
}

// LoggingSpec defines the logging configuration
type LoggingSpec struct {
	// Level is the log level
	// +kubebuilder:default="info"
	// +kubebuilder:validation:Enum=debug;info;warn;error
	// +optional
	Level string `json:"level,omitempty"`

	// Format is the log format
	// +kubebuilder:default="json"
	// +kubebuilder:validation:Enum=json;text
	// +optional
	Format string `json:"format,omitempty"`
}

// ServiceAccountSpec defines the service account configuration
type ServiceAccountSpec struct {
	// Create enables creation of a service account
	// +kubebuilder:default=true
	// +optional
	Create *bool `json:"create,omitempty"`

	// Name is the name of the service account
	// +optional
	Name string `json:"name,omitempty"`

	// Annotations defines additional annotations for the service account
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// NovaEdgeClusterStatus defines the observed state of NovaEdgeCluster
type NovaEdgeClusterStatus struct {
	// Phase is the current phase of the cluster
	// +optional
	Phase ClusterPhase `json:"phase,omitempty"`

	// ObservedGeneration is the generation last observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the cluster's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// Controller is the status of the controller deployment
	// +optional
	Controller *ComponentStatus `json:"controller,omitempty"`

	// Agent is the status of the agent DaemonSet
	// +optional
	Agent *ComponentStatus `json:"agent,omitempty"`

	// WebUI is the status of the web UI deployment
	// +optional
	WebUI *ComponentStatus `json:"webUI,omitempty"`

	// Version is the currently deployed version
	// +optional
	Version string `json:"version,omitempty"`

	// LastUpgradeTime is the timestamp of the last upgrade
	// +optional
	LastUpgradeTime *metav1.Time `json:"lastUpgradeTime,omitempty"`
}

// ClusterPhase represents the phase of the NovaEdgeCluster
// +kubebuilder:validation:Enum=Pending;Initializing;Running;Upgrading;Degraded;Failed
type ClusterPhase string

const (
	// ClusterPhasePending indicates the cluster is pending
	ClusterPhasePending ClusterPhase = "Pending"
	// ClusterPhaseInitializing indicates the cluster is initializing
	ClusterPhaseInitializing ClusterPhase = "Initializing"
	// ClusterPhaseRunning indicates the cluster is running
	ClusterPhaseRunning ClusterPhase = "Running"
	// ClusterPhaseUpgrading indicates the cluster is upgrading
	ClusterPhaseUpgrading ClusterPhase = "Upgrading"
	// ClusterPhaseDegraded indicates the cluster is degraded
	ClusterPhaseDegraded ClusterPhase = "Degraded"
	// ClusterPhaseFailed indicates the cluster has failed
	ClusterPhaseFailed ClusterPhase = "Failed"
)

// ComponentStatus represents the status of a cluster component
type ComponentStatus struct {
	// Ready indicates if the component is ready
	// +optional
	Ready bool `json:"ready,omitempty"`

	// Replicas is the desired number of replicas
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// ReadyReplicas is the number of ready replicas
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// UpdatedReplicas is the number of updated replicas
	// +optional
	UpdatedReplicas int32 `json:"updatedReplicas,omitempty"`

	// Version is the component version
	// +optional
	Version string `json:"version,omitempty"`

	// Message provides additional status information
	// +optional
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=nec
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Controller",type=string,JSONPath=`.status.controller.readyReplicas`
// +kubebuilder:printcolumn:name="Agents",type=string,JSONPath=`.status.agent.readyReplicas`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// NovaEdgeCluster is the Schema for the novaedgeclusters API
type NovaEdgeCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NovaEdgeClusterSpec   `json:"spec,omitempty"`
	Status NovaEdgeClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NovaEdgeClusterList contains a list of NovaEdgeCluster
type NovaEdgeClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NovaEdgeCluster `json:"items"`
}
