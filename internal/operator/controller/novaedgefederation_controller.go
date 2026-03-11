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
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
	"github.com/azrtydxb/novaedge/internal/controller/federation"
)

const (
	// FederationFinalizer is the finalizer for NovaEdgeFederation resources
	FederationFinalizer = "novaedge.io/federation-finalizer"

	// FederationConditionReady indicates the federation is ready
	FederationConditionReady = "Ready"

	// FederationConditionSyncing indicates sync is in progress
	FederationConditionSyncing = "Syncing"

	// FederationConditionDegraded indicates some peers are unhealthy
	FederationConditionDegraded = "Degraded"
)

// NovaEdgeFederationReconciler reconciles a NovaEdgeFederation object
type NovaEdgeFederationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger *zap.Logger

	// gRPC server to register federation service
	GRPCServer *grpc.Server

	// Active federation managers
	managers   map[string]*federation.Manager
	managersMu sync.RWMutex

	// Configuration
	ControllerName string
}

// +kubebuilder:rbac:groups=novaedge.io,resources=novaedgefederations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=novaedge.io,resources=novaedgefederations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=novaedge.io,resources=novaedgefederations/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile handles NovaEdgeFederation resources
func (r *NovaEdgeFederationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Logger.With(
		zap.String("federation", req.Name),
		zap.String("namespace", req.Namespace),
	)

	// Fetch the NovaEdgeFederation
	fed := &novaedgev1alpha1.NovaEdgeFederation{}
	if err := r.Get(ctx, req.NamespacedName, fed); err != nil {
		if apierrors.IsNotFound(err) {
			// Federation was deleted, stop the manager
			r.stopManager(req.String())
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !fed.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, fed, log)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(fed, FederationFinalizer) {
		controllerutil.AddFinalizer(fed, FederationFinalizer)
		if err := r.Update(ctx, fed); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Check if paused
	if fed.Spec.Paused {
		log.Info("Federation is paused")
		r.stopManager(req.String())
		return r.updateStatus(ctx, fed, novaedgev1alpha1.FederationPhaseInitializing, "Federation is paused", log)
	}

	// Start or update federation manager
	if err := r.ensureManager(ctx, fed, log); err != nil {
		log.Error("Failed to ensure federation manager", zap.Error(err))
		return r.updateStatus(ctx, fed, novaedgev1alpha1.FederationPhaseDegraded, err.Error(), log)
	}

	// Update status from manager
	return r.syncStatus(ctx, fed, log)
}

// handleDeletion handles the deletion of a federation
func (r *NovaEdgeFederationReconciler) handleDeletion(ctx context.Context, fed *novaedgev1alpha1.NovaEdgeFederation, log *zap.Logger) (ctrl.Result, error) {
	log.Info("Handling federation deletion")

	// Stop the manager
	key := fmt.Sprintf("%s/%s", fed.Namespace, fed.Name)
	r.stopManager(key)

	// Remove finalizer
	controllerutil.RemoveFinalizer(fed, FederationFinalizer)
	if err := r.Update(ctx, fed); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Federation deleted")
	return ctrl.Result{}, nil
}

// ensureManager ensures a federation manager is running for this federation
func (r *NovaEdgeFederationReconciler) ensureManager(ctx context.Context, fed *novaedgev1alpha1.NovaEdgeFederation, log *zap.Logger) error {
	key := fmt.Sprintf("%s/%s", fed.Namespace, fed.Name)

	r.managersMu.Lock()
	defer r.managersMu.Unlock()

	// Check if manager already exists and apply config updates if spec changed
	if existing, ok := r.managers[key]; ok {
		if fed.Status.ObservedGeneration == fed.Generation {
			log.Debug("Federation spec unchanged, skipping update",
				zap.String("key", key),
			)
			return nil
		}

		log.Info("Federation spec changed, updating manager in-place",
			zap.String("key", key),
			zap.Int64("old_generation", fed.Status.ObservedGeneration),
			zap.Int64("new_generation", fed.Generation),
		)

		// Load TLS credentials for the updated spec
		updatedTLSCreds, tlsErr := r.loadTLSCredentials(ctx, fed, log)
		if tlsErr != nil {
			return fmt.Errorf("failed to load TLS credentials for update: %w", tlsErr)
		}

		// Build new config from CRD
		newConfig := federation.CRDToConfig(fed)

		// Apply TLS credentials to peers
		for _, peer := range newConfig.Peers {
			if creds, credOK := updatedTLSCreds[peer.Name]; credOK {
				peer.CACert = creds.CACert
				peer.ClientCert = creds.ClientCert
				peer.ClientKey = creds.ClientKey
			}
		}

		if updateErr := existing.UpdateConfig(newConfig); updateErr != nil {
			return fmt.Errorf("failed to update federation manager config: %w", updateErr)
		}

		return nil
	}

	// Load TLS credentials from secrets if configured
	tlsCreds, err := r.loadTLSCredentials(ctx, fed, log)
	if err != nil {
		return fmt.Errorf("failed to load TLS credentials: %w", err)
	}

	// Convert credentials to federation.TLSCredentials
	fedCreds := make(map[string]*federation.TLSCredentials)
	for name, creds := range tlsCreds {
		fedCreds[name] = &federation.TLSCredentials{
			CACert:     creds.CACert,
			ClientCert: creds.ClientCert,
			ClientKey:  creds.ClientKey,
		}
	}

	// Create new manager with TLS credentials
	manager, err := federation.NewManagerFromCRDWithCreds(fed, fedCreds, r.Logger)
	if err != nil {
		return fmt.Errorf("failed to create federation manager: %w", err)
	}

	// Register with gRPC server if available
	if r.GRPCServer != nil {
		manager.RegisterServer(r.GRPCServer)
	}

	// Set up resource application callback
	applier := NewFederationResourceApplier(r.Client, r.Scheme, log)
	manager.OnResourceChange(func(key federation.ResourceKey, changeType federation.ChangeType, data []byte) {
		applier.Apply(ctx, key, changeType, data)
	})

	// Start the manager
	if err := manager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start federation manager: %w", err)
	}

	r.managers[key] = manager
	log.Info("Started federation manager",
		zap.String("federation_id", fed.Spec.FederationID),
		zap.Int("peers", len(fed.Spec.Members)),
	)

	return nil
}

// stopManager stops a federation manager
func (r *NovaEdgeFederationReconciler) stopManager(key string) {
	r.managersMu.Lock()
	defer r.managersMu.Unlock()

	if manager, ok := r.managers[key]; ok {
		manager.Stop()
		delete(r.managers, key)
		r.Logger.Info("Stopped federation manager", zap.String("key", key))
	}
}

// PeerTLSCredentials holds TLS credentials for a peer
type PeerTLSCredentials struct {
	CACert     []byte
	ClientCert []byte
	ClientKey  []byte //nolint:gosec // G117: struct field name for TLS credential holder, not a hardcoded credential
}

// loadTLSCredentials loads TLS credentials from Kubernetes secrets
func (r *NovaEdgeFederationReconciler) loadTLSCredentials(ctx context.Context, fed *novaedgev1alpha1.NovaEdgeFederation, log *zap.Logger) (map[string]*PeerTLSCredentials, error) {
	credentials := make(map[string]*PeerTLSCredentials)

	for _, peer := range fed.Spec.Members {
		if peer.TLS == nil {
			continue
		}

		creds := &PeerTLSCredentials{}

		// Load CA certificate
		if peer.TLS.CASecretRef != nil {
			secret := &corev1.Secret{}
			secretKey := client.ObjectKey{
				Name:      peer.TLS.CASecretRef.Name,
				Namespace: peer.TLS.CASecretRef.Namespace,
			}
			if secretKey.Namespace == "" {
				secretKey.Namespace = fed.Namespace
			}

			if err := r.Get(ctx, secretKey, secret); err != nil {
				if apierrors.IsNotFound(err) {
					log.Warn("CA secret not found for peer",
						zap.String("peer", peer.Name),
						zap.String("secret", secretKey.String()),
					)
				} else {
					return nil, fmt.Errorf("failed to get CA secret for peer %s: %w", peer.Name, err)
				}
			} else {
				// CA data is in secret.Data["ca.crt"]
				if caCert, ok := secret.Data["ca.crt"]; ok {
					creds.CACert = caCert
					log.Debug("Loaded CA certificate for peer",
						zap.String("peer", peer.Name),
						zap.Int("bytes", len(caCert)),
					)
				} else {
					log.Warn("CA secret missing ca.crt key",
						zap.String("peer", peer.Name),
						zap.String("secret", secretKey.String()),
					)
				}
			}
		}

		// Load client certificate
		if peer.TLS.ClientCertSecretRef != nil {
			secret := &corev1.Secret{}
			secretKey := client.ObjectKey{
				Name:      peer.TLS.ClientCertSecretRef.Name,
				Namespace: peer.TLS.ClientCertSecretRef.Namespace,
			}
			if secretKey.Namespace == "" {
				secretKey.Namespace = fed.Namespace
			}

			if err := r.Get(ctx, secretKey, secret); err != nil {
				if apierrors.IsNotFound(err) {
					log.Warn("Client cert secret not found for peer",
						zap.String("peer", peer.Name),
						zap.String("secret", secretKey.String()),
					)
				} else {
					return nil, fmt.Errorf("failed to get client cert secret for peer %s: %w", peer.Name, err)
				}
			} else {
				// Client cert and key are in secret.Data["tls.crt"] and secret.Data["tls.key"]
				if clientCert, ok := secret.Data["tls.crt"]; ok {
					creds.ClientCert = clientCert
				} else {
					log.Warn("Client cert secret missing tls.crt key",
						zap.String("peer", peer.Name),
						zap.String("secret", secretKey.String()),
					)
				}
				if clientKey, ok := secret.Data["tls.key"]; ok {
					creds.ClientKey = clientKey
				} else {
					log.Warn("Client cert secret missing tls.key key",
						zap.String("peer", peer.Name),
						zap.String("secret", secretKey.String()),
					)
				}

				if len(creds.ClientCert) > 0 && len(creds.ClientKey) > 0 {
					log.Debug("Loaded client certificate for peer",
						zap.String("peer", peer.Name),
					)
				}
			}
		}

		if len(creds.CACert) > 0 || len(creds.ClientCert) > 0 {
			credentials[peer.Name] = creds
		}
	}

	return credentials, nil
}

// syncStatus synchronizes the federation status from the manager
func (r *NovaEdgeFederationReconciler) syncStatus(ctx context.Context, fed *novaedgev1alpha1.NovaEdgeFederation, log *zap.Logger) (ctrl.Result, error) {
	key := fmt.Sprintf("%s/%s", fed.Namespace, fed.Name)

	r.managersMu.RLock()
	manager, ok := r.managers[key]
	r.managersMu.RUnlock()

	if !ok {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Get phase from manager
	phase := manager.GetPhase()

	// Map internal phase to CRD phase
	var crdPhase novaedgev1alpha1.FederationPhase
	switch phase {
	case federation.PhaseInitializing:
		crdPhase = novaedgev1alpha1.FederationPhaseInitializing
	case federation.PhaseSyncing:
		crdPhase = novaedgev1alpha1.FederationPhaseSyncing
	case federation.PhaseHealthy:
		crdPhase = novaedgev1alpha1.FederationPhaseHealthy
	case federation.PhaseDegraded:
		crdPhase = novaedgev1alpha1.FederationPhaseDegraded
	case federation.PhasePartitioned:
		crdPhase = novaedgev1alpha1.FederationPhasePartitioned
	default:
		crdPhase = novaedgev1alpha1.FederationPhaseInitializing
	}

	// Get peer states
	peerStates := manager.GetPeerStates()
	memberStatuses := make([]novaedgev1alpha1.FederationMemberStatus, 0, len(peerStates))
	for name, state := range peerStates {
		// Convert VectorClock to map if available
		var vectorClockMap map[string]int64
		if state.VectorClock != nil {
			vectorClockMap = state.VectorClock.ToMap()
		}

		status := novaedgev1alpha1.FederationMemberStatus{
			Name:        name,
			Healthy:     state.Healthy,
			VectorClock: vectorClockMap,
			AgentCount:  state.AgentCount,
		}

		if !state.LastSeen.IsZero() {
			lastSeen := metav1.NewTime(state.LastSeen)
			status.LastSeen = &lastSeen
		}

		if !state.LastSyncTime.IsZero() {
			lastSync := metav1.NewTime(state.LastSyncTime)
			status.LastSyncTime = &lastSync
		}

		if state.SyncLag > 0 {
			syncLag := metav1.Duration{Duration: state.SyncLag}
			status.SyncLag = &syncLag
		}

		if state.LastError != "" {
			status.Error = state.LastError
		}

		memberStatuses = append(memberStatuses, status)
	}

	// Get conflicts
	conflicts := manager.GetConflicts()

	// Get vector clock
	vectorClock := manager.GetVectorClock()

	// Update status
	fed.Status.Phase = crdPhase
	fed.Status.Members = memberStatuses
	fed.Status.ConflictsPending = safeIntToInt32(len(conflicts))
	fed.Status.LocalVectorClock = vectorClock
	fed.Status.ObservedGeneration = fed.Generation

	now := metav1.Now()
	fed.Status.LastSyncTime = &now

	// Update conditions
	fed.Status.Conditions = r.buildConditions(crdPhase, fed.Status.Conditions)

	if err := r.Status().Update(ctx, fed); err != nil {
		log.Error("Failed to update federation status", zap.Error(err))
		return ctrl.Result{}, err
	}

	// Requeue based on phase
	switch crdPhase {
	case novaedgev1alpha1.FederationPhaseHealthy:
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	case novaedgev1alpha1.FederationPhaseSyncing:
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	case novaedgev1alpha1.FederationPhaseInitializing,
		novaedgev1alpha1.FederationPhaseDegraded,
		novaedgev1alpha1.FederationPhasePartitioned:
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	default:
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
}

// updateStatus updates the status with a simple phase and message
func (r *NovaEdgeFederationReconciler) updateStatus(ctx context.Context, fed *novaedgev1alpha1.NovaEdgeFederation, phase novaedgev1alpha1.FederationPhase, _ string, log *zap.Logger) (ctrl.Result, error) {
	fed.Status.Phase = phase
	fed.Status.ObservedGeneration = fed.Generation
	fed.Status.Conditions = r.buildConditions(phase, fed.Status.Conditions)

	if err := r.Status().Update(ctx, fed); err != nil {
		log.Error("Failed to update federation status", zap.Error(err))
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

// buildConditions builds conditions based on the current phase
func (r *NovaEdgeFederationReconciler) buildConditions(phase novaedgev1alpha1.FederationPhase, existing []metav1.Condition) []metav1.Condition {
	now := metav1.Now()

	// Helper to find existing condition
	findCondition := func(condType string) *metav1.Condition {
		for i := range existing {
			if existing[i].Type == condType {
				return &existing[i]
			}
		}
		return nil
	}

	// Build ready condition
	readyCondition := metav1.Condition{
		Type:               FederationConditionReady,
		LastTransitionTime: now,
		ObservedGeneration: 0,
	}

	switch phase {
	case novaedgev1alpha1.FederationPhaseHealthy:
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = "FederationHealthy"
		readyCondition.Message = "All federation peers are healthy and in sync"
	case novaedgev1alpha1.FederationPhaseSyncing:
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = "SyncInProgress"
		readyCondition.Message = "Federation sync is in progress"
	case novaedgev1alpha1.FederationPhaseDegraded:
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = "FederationDegraded"
		readyCondition.Message = "Some federation peers are unhealthy"
	case novaedgev1alpha1.FederationPhasePartitioned:
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = "NetworkPartition"
		readyCondition.Message = "Federation is experiencing network partition"
	case novaedgev1alpha1.FederationPhaseInitializing:
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = "Initializing"
		readyCondition.Message = "Federation is initializing"
	default:
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = "Initializing"
		readyCondition.Message = "Federation is initializing"
	}

	// Preserve transition time if status hasn't changed
	if existingReady := findCondition(FederationConditionReady); existingReady != nil {
		if existingReady.Status == readyCondition.Status {
			readyCondition.LastTransitionTime = existingReady.LastTransitionTime
		}
	}

	return []metav1.Condition{readyCondition}
}

// GetManager returns the federation manager for a given federation
func (r *NovaEdgeFederationReconciler) GetManager(namespace, name string) *federation.Manager {
	key := fmt.Sprintf("%s/%s", namespace, name)
	r.managersMu.RLock()
	defer r.managersMu.RUnlock()
	return r.managers[key]
}

// SetupWithManager sets up the controller with the Manager
func (r *NovaEdgeFederationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.managers == nil {
		r.managers = make(map[string]*federation.Manager)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&novaedgev1alpha1.NovaEdgeFederation{}).
		Complete(r)
}

// safeIntToInt32 safely converts an int to int32, clamping to max int32 value if needed
func safeIntToInt32(v int) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v) //nolint:gosec // bounds checked above
}
