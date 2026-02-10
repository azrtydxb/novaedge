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

package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

const (
	remoteClusterFinalizer = "novaedge.io/remote-cluster-finalizer"

	// Remote cluster condition types
	ConditionTypeRemoteReady      = "Ready"
	ConditionTypeRemoteConnected  = "Connected"
	ConditionTypeRemoteHealthy    = "Healthy"
	ConditionTypeRemoteConfigured = "Configured"
)

// RemoteClusterInfo holds runtime information about a remote cluster
type RemoteClusterInfo struct {
	Name              string
	Region            string
	Zone              string
	ControllerAddress string
	TLSEnabled        bool
	Connected         bool
	LastHeartbeat     time.Time
	AgentCount        int32
	ReadyAgents       int32
	Labels            map[string]string
}

// RemoteClusterRegistry maintains a thread-safe registry of remote clusters
type RemoteClusterRegistry struct {
	mu       sync.RWMutex
	clusters map[string]*RemoteClusterInfo
}

// NewRemoteClusterRegistry creates a new registry
func NewRemoteClusterRegistry() *RemoteClusterRegistry {
	return &RemoteClusterRegistry{
		clusters: make(map[string]*RemoteClusterInfo),
	}
}

// Register adds or updates a remote cluster in the registry
func (r *RemoteClusterRegistry) Register(info *RemoteClusterInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clusters[info.Name] = info
}

// Unregister removes a remote cluster from the registry
func (r *RemoteClusterRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clusters, name)
}

// Get retrieves a remote cluster by name
func (r *RemoteClusterRegistry) Get(name string) (*RemoteClusterInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, ok := r.clusters[name]
	return info, ok
}

// List returns all registered remote clusters
func (r *RemoteClusterRegistry) List() []*RemoteClusterInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*RemoteClusterInfo, 0, len(r.clusters))
	for _, info := range r.clusters {
		result = append(result, info)
	}
	return result
}

// UpdateConnection updates the connection status of a remote cluster
func (r *RemoteClusterRegistry) UpdateConnection(name string, connected bool, agentCount, readyAgents int32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if info, ok := r.clusters[name]; ok {
		info.Connected = connected
		info.AgentCount = agentCount
		info.ReadyAgents = readyAgents
		if connected {
			info.LastHeartbeat = time.Now()
		}
	}
}

// NovaEdgeRemoteClusterReconciler reconciles a NovaEdgeRemoteCluster object
type NovaEdgeRemoteClusterReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Registry *RemoteClusterRegistry
}

// +kubebuilder:rbac:groups=novaedge.io,resources=novaedgeremoteclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=novaedge.io,resources=novaedgeremoteclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=novaedge.io,resources=novaedgeremoteclusters/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop
func (r *NovaEdgeRemoteClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling NovaEdgeRemoteCluster", "name", req.Name, "namespace", req.Namespace)

	// Fetch the NovaEdgeRemoteCluster instance
	remoteCluster := &novaedgev1alpha1.NovaEdgeRemoteCluster{}
	if err := r.Get(ctx, req.NamespacedName, remoteCluster); err != nil {
		if errors.IsNotFound(err) {
			// Resource deleted, unregister from registry
			r.Registry.Unregister(req.Name)
			logger.Info("NovaEdgeRemoteCluster resource not found, unregistered from registry")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get NovaEdgeRemoteCluster")
		return ctrl.Result{}, err
	}

	// Handle finalizer
	if remoteCluster.DeletionTimestamp.IsZero() {
		// Add finalizer if not present
		if !controllerutil.ContainsFinalizer(remoteCluster, remoteClusterFinalizer) {
			controllerutil.AddFinalizer(remoteCluster, remoteClusterFinalizer)
			if err := r.Update(ctx, remoteCluster); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// Handle deletion
		if controllerutil.ContainsFinalizer(remoteCluster, remoteClusterFinalizer) {
			if err := r.cleanupRemoteCluster(ctx, remoteCluster); err != nil {
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(remoteCluster, remoteClusterFinalizer)
			if err := r.Update(ctx, remoteCluster); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Check if reconciliation is paused
	if remoteCluster.Spec.Paused {
		logger.Info("Reconciliation is paused for this remote cluster")
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	// Update phase to initializing if pending
	if remoteCluster.Status.Phase == "" || remoteCluster.Status.Phase == novaedgev1alpha1.RemoteClusterPhasePending {
		remoteCluster.Status.Phase = novaedgev1alpha1.RemoteClusterPhaseConnecting
		if err := r.Status().Update(ctx, remoteCluster); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Register/update the remote cluster in the registry
	if err := r.registerRemoteCluster(ctx, remoteCluster); err != nil {
		logger.Error(err, "Failed to register remote cluster")
		r.setCondition(remoteCluster, ConditionTypeRemoteConfigured, metav1.ConditionFalse,
			"ConfigurationFailed", err.Error())
		if err := r.Status().Update(ctx, remoteCluster); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Set configured condition
	r.setCondition(remoteCluster, ConditionTypeRemoteConfigured, metav1.ConditionTrue,
		"Configured", "Remote cluster configuration is valid")

	// Check connection status from registry
	if err := r.updateConnectionStatus(ctx, remoteCluster); err != nil {
		logger.Error(err, "Failed to update connection status")
	}

	// Update overall status
	if err := r.updateOverallStatus(ctx, remoteCluster); err != nil {
		return ctrl.Result{}, err
	}

	// Requeue for periodic health checks
	requeueAfter := 30 * time.Second
	if remoteCluster.Spec.HealthCheck != nil && remoteCluster.Spec.HealthCheck.Interval != nil {
		requeueAfter = remoteCluster.Spec.HealthCheck.Interval.Duration
	}

	logger.Info("Reconciliation complete", "requeueAfter", requeueAfter)
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// registerRemoteCluster registers the remote cluster in the registry
func (r *NovaEdgeRemoteClusterReconciler) registerRemoteCluster(ctx context.Context, rc *novaedgev1alpha1.NovaEdgeRemoteCluster) error {
	logger := log.FromContext(ctx)

	// Validate required fields
	if rc.Spec.ClusterName == "" {
		return fmt.Errorf("clusterName is required")
	}
	if rc.Spec.Connection.ControllerEndpoint == "" {
		return fmt.Errorf("connection.controllerEndpoint is required")
	}

	// Determine TLS settings
	tlsEnabled := true
	if rc.Spec.Connection.TLS != nil && rc.Spec.Connection.TLS.Enabled != nil {
		tlsEnabled = *rc.Spec.Connection.TLS.Enabled
	}

	// Build cluster info
	info := &RemoteClusterInfo{
		Name:              rc.Spec.ClusterName,
		Region:            rc.Spec.Region,
		Zone:              rc.Spec.Zone,
		ControllerAddress: rc.Spec.Connection.ControllerEndpoint,
		TLSEnabled:        tlsEnabled,
		Labels:            rc.Spec.Labels,
	}

	// Check if already registered and preserve connection state
	if existing, ok := r.Registry.Get(rc.Spec.ClusterName); ok {
		info.Connected = existing.Connected
		info.LastHeartbeat = existing.LastHeartbeat
		info.AgentCount = existing.AgentCount
		info.ReadyAgents = existing.ReadyAgents
	}

	r.Registry.Register(info)
	logger.Info("Registered remote cluster", "clusterName", rc.Spec.ClusterName, "region", rc.Spec.Region)

	return nil
}

// updateConnectionStatus updates the connection status from the registry
func (r *NovaEdgeRemoteClusterReconciler) updateConnectionStatus(ctx context.Context, rc *novaedgev1alpha1.NovaEdgeRemoteCluster) error {
	info, ok := r.Registry.Get(rc.Spec.ClusterName)
	if !ok {
		return fmt.Errorf("remote cluster not found in registry")
	}

	// Initialize connection status if nil
	if rc.Status.Connection == nil {
		rc.Status.Connection = &novaedgev1alpha1.ConnectionStatus{}
	}

	// Update connection status
	rc.Status.Connection.Connected = info.Connected
	rc.Status.Connection.ActiveConnections = info.ReadyAgents

	if info.Connected {
		now := metav1.Now()
		rc.Status.Connection.LastConnected = &now
		rc.Status.LastHeartbeat = &now
		r.setCondition(rc, ConditionTypeRemoteConnected, metav1.ConditionTrue,
			"Connected", fmt.Sprintf("Connected with %d agents", info.ReadyAgents))
	} else {
		r.setCondition(rc, ConditionTypeRemoteConnected, metav1.ConditionFalse,
			"Disconnected", "No active connections from remote cluster")
	}

	// Update agent status
	if rc.Status.Agents == nil {
		rc.Status.Agents = &novaedgev1alpha1.RemoteAgentStatus{}
	}
	rc.Status.Agents.Total = info.AgentCount
	rc.Status.Agents.Ready = info.ReadyAgents
	rc.Status.Agents.Healthy = info.ReadyAgents // For now, ready = healthy

	return nil
}

// updateOverallStatus determines and updates the overall phase
func (r *NovaEdgeRemoteClusterReconciler) updateOverallStatus(ctx context.Context, rc *novaedgev1alpha1.NovaEdgeRemoteCluster) error {
	// Determine phase based on conditions
	connected := meta.IsStatusConditionTrue(rc.Status.Conditions, ConditionTypeRemoteConnected)
	configured := meta.IsStatusConditionTrue(rc.Status.Conditions, ConditionTypeRemoteConfigured)

	oldPhase := rc.Status.Phase

	if !configured {
		rc.Status.Phase = novaedgev1alpha1.RemoteClusterPhaseFailed
	} else if connected {
		// Check if all agents are healthy
		if rc.Status.Agents != nil && rc.Status.Agents.Ready == rc.Status.Agents.Total && rc.Status.Agents.Total > 0 {
			rc.Status.Phase = novaedgev1alpha1.RemoteClusterPhaseConnected
			r.setCondition(rc, ConditionTypeRemoteHealthy, metav1.ConditionTrue,
				"AllAgentsHealthy", "All agents are ready")
		} else if rc.Status.Agents != nil && rc.Status.Agents.Ready > 0 {
			rc.Status.Phase = novaedgev1alpha1.RemoteClusterPhaseDegraded
			r.setCondition(rc, ConditionTypeRemoteHealthy, metav1.ConditionFalse,
				"SomeAgentsUnhealthy", fmt.Sprintf("%d/%d agents ready", rc.Status.Agents.Ready, rc.Status.Agents.Total))
		} else {
			rc.Status.Phase = novaedgev1alpha1.RemoteClusterPhaseConnecting
		}
	} else {
		// Check if we've ever been connected
		if rc.Status.Connection != nil && rc.Status.Connection.LastConnected != nil {
			rc.Status.Phase = novaedgev1alpha1.RemoteClusterPhaseDisconnected
		} else {
			rc.Status.Phase = novaedgev1alpha1.RemoteClusterPhaseConnecting
		}
	}

	// Set overall Ready condition
	switch rc.Status.Phase {
	case novaedgev1alpha1.RemoteClusterPhaseConnected:
		r.setCondition(rc, ConditionTypeRemoteReady, metav1.ConditionTrue,
			"Ready", "Remote cluster is fully connected and healthy")
	case novaedgev1alpha1.RemoteClusterPhaseDegraded:
		r.setCondition(rc, ConditionTypeRemoteReady, metav1.ConditionFalse,
			"Degraded", "Remote cluster is connected but some agents are unhealthy")
	default:
		r.setCondition(rc, ConditionTypeRemoteReady, metav1.ConditionFalse,
			string(rc.Status.Phase), fmt.Sprintf("Remote cluster is in %s phase", rc.Status.Phase))
	}

	rc.Status.ObservedGeneration = rc.Generation

	if oldPhase != rc.Status.Phase {
		log.FromContext(ctx).Info("Remote cluster phase changed", "from", oldPhase, "to", rc.Status.Phase)
	}

	return r.Status().Update(ctx, rc)
}

// cleanupRemoteCluster handles cleanup when a remote cluster is deleted
func (r *NovaEdgeRemoteClusterReconciler) cleanupRemoteCluster(ctx context.Context, rc *novaedgev1alpha1.NovaEdgeRemoteCluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Cleaning up remote cluster", "clusterName", rc.Spec.ClusterName)

	// Unregister from registry
	r.Registry.Unregister(rc.Spec.ClusterName)

	// TODO: Notify controller to stop accepting connections from this cluster
	// TODO: Clean up any cluster-specific resources (secrets, configmaps, etc.)

	return nil
}

// setCondition sets a condition on the remote cluster status
func (r *NovaEdgeRemoteClusterReconciler) setCondition(rc *novaedgev1alpha1.NovaEdgeRemoteCluster,
	conditionType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&rc.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: rc.Generation,
	})
}

// SetupWithManager sets up the controller with the Manager
func (r *NovaEdgeRemoteClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&novaedgev1alpha1.NovaEdgeRemoteCluster{}).
		Complete(r)
}
