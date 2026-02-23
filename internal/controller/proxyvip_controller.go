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
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
	"github.com/piwi3910/novaedge/internal/controller/ipam"
)

// ProxyVIPReconciler reconciles a ProxyVIP object
type ProxyVIPReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Allocator *ipam.Allocator
}

// +kubebuilder:rbac:groups=novaedge.io,resources=proxyvips,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=novaedge.io,resources=proxyvips/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=novaedge.io,resources=proxyvips/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop
func (r *ProxyVIPReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the ProxyVIP instance
	vip := &novaedgev1alpha1.ProxyVIP{}
	err := r.Get(ctx, req.NamespacedName, vip)
	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, could have been deleted
			logger.Info("ProxyVIP resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ProxyVIP")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling ProxyVIP", "name", vip.Name, "mode", vip.Spec.Mode, "address", vip.Spec.Address)

	// Handle IPAM allocation if poolRef is set and no address allocated yet
	if vip.Spec.PoolRef != nil && vip.Status.AllocatedAddress == "" && r.Allocator != nil {
		// First, ensure the pool is loaded
		pool := &novaedgev1alpha1.ProxyIPPool{}
		if err := r.Get(ctx, client.ObjectKey{Name: vip.Spec.PoolRef.Name}, pool); err != nil {
			if !errors.IsNotFound(err) {
				logger.Error(err, "Failed to get ProxyIPPool", "pool", vip.Spec.PoolRef.Name)
				return ctrl.Result{}, err
			}
			logger.Info("Referenced ProxyIPPool not found", "pool", vip.Spec.PoolRef.Name)
		} else {
			// Register pool with allocator (idempotent - updates if exists)
			cidrs := append([]string{}, pool.Spec.CIDRs...)
			addresses := append([]string{}, pool.Spec.Addresses...)
			if err := r.Allocator.AddPool(pool.Name, cidrs, addresses); err != nil {
				logger.Error(err, "Failed to register IP pool", "pool", pool.Name)
				return ctrl.Result{}, err
			}

			// Allocate address from pool
			allocated, err := r.Allocator.Allocate(pool.Name, vip.Name)
			if err != nil {
				logger.Error(err, "Failed to allocate IP from pool", "pool", pool.Name)
				return ctrl.Result{}, err
			}

			// Update VIP status with allocated address
			vip.Status.AllocatedAddress = allocated
			if err := r.Status().Update(ctx, vip); err != nil {
				logger.Error(err, "Failed to update VIP status with allocated address")
				// Release the allocation since we couldn't persist it
				r.Allocator.Release(pool.Name, vip.Name)
				return ctrl.Result{}, err
			}

			logger.Info("Allocated IP from pool",
				"pool", pool.Name,
				"address", allocated,
				"vip", vip.Name,
			)
		}
	}

	// Handle IPAM release if VIP had allocation but poolRef was removed
	if vip.Spec.PoolRef == nil && vip.Status.AllocatedAddress != "" && r.Allocator != nil {
		// Clear the allocated address
		vip.Status.AllocatedAddress = ""
		vip.Status.AllocatedIPv6Address = ""
		if err := r.Status().Update(ctx, vip); err != nil {
			logger.Error(err, "Failed to clear allocated address from VIP status")
			return ctrl.Result{}, err
		}
		logger.Info("Cleared allocated address (poolRef removed)", "vip", vip.Name)
	}

	// Use allocated address if spec.address is empty
	if vip.Spec.Address == "" && vip.Status.AllocatedAddress != "" {
		logger.Info("Using allocated address from pool",
			"vip", vip.Name,
			"address", vip.Status.AllocatedAddress,
		)
	}

	// Get candidate nodes based on NodeSelector and cluster-wide exclusions
	exclusions := r.getVIPNodeExclusions(ctx)
	candidateNodes, err := r.getCandidateNodes(ctx, vip, exclusions)
	if err != nil {
		logger.Error(err, "Failed to get candidate nodes")
		return ctrl.Result{}, err
	}

	if len(candidateNodes) == 0 {
		logger.Info("No candidate nodes found for VIP")
		// Update status to clear active node
		if err := r.updateVIPStatus(ctx, vip, "", nil); err != nil {
			logger.Error(err, "Failed to update VIP status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Handle VIP based on mode
	switch vip.Spec.Mode {
	case novaedgev1alpha1.VIPModeL2ARP:
		// For L2ARP mode: elect single active node (alphabetically first ready node)
		activeNode := r.electActiveNode(candidateNodes)
		logger.Info("Elected active node for L2ARP VIP", "activeNode", activeNode)

		// Update status with active node
		if err := r.updateVIPStatus(ctx, vip, activeNode, nil); err != nil {
			logger.Error(err, "Failed to update VIP status")
			return ctrl.Result{}, err
		}

	case novaedgev1alpha1.VIPModeBGP, novaedgev1alpha1.VIPModeOSPF:
		// For BGP/OSPF mode: all candidate nodes can announce
		announcingNodes := make([]string, 0, len(candidateNodes))
		for _, node := range candidateNodes {
			announcingNodes = append(announcingNodes, node.Name)
		}
		logger.Info("Announcing nodes for BGP/OSPF VIP", "count", len(announcingNodes))

		// Update status with announcing nodes
		if err := r.updateVIPStatus(ctx, vip, "", announcingNodes); err != nil {
			logger.Error(err, "Failed to update VIP status")
			return ctrl.Result{}, err
		}
	}

	// Trigger config update for all nodes
	TriggerConfigUpdate()

	return ctrl.Result{}, nil
}

// getCandidateNodes returns nodes that match the VIP's NodeSelector and are ready,
// minus any nodes excluded by the cluster-wide VipNodeExclusions that are not
// tolerated by the VIP's Tolerations list.
func (r *ProxyVIPReconciler) getCandidateNodes(ctx context.Context, vip *novaedgev1alpha1.ProxyVIP, exclusions []string) ([]corev1.Node, error) {
	// Build effective node selector starting from the VIP's own selector
	selector := &metav1.LabelSelector{}
	if vip.Spec.NodeSelector != nil {
		selector = vip.Spec.NodeSelector.DeepCopy()
	}

	// Compute effective exclusions = cluster exclusions - VIP tolerations
	tolerated := make(map[string]bool, len(vip.Spec.Tolerations))
	for _, t := range vip.Spec.Tolerations {
		tolerated[t] = true
	}
	for _, key := range exclusions {
		if !tolerated[key] {
			selector.MatchExpressions = append(selector.MatchExpressions, metav1.LabelSelectorRequirement{
				Key:      key,
				Operator: metav1.LabelSelectorOpDoesNotExist,
			})
		}
	}

	nodeList := &corev1.NodeList{}
	var listOpts []client.ListOption
	if len(selector.MatchLabels) > 0 || len(selector.MatchExpressions) > 0 {
		labelSelector, err := metav1.LabelSelectorAsSelector(selector)
		if err != nil {
			return nil, err
		}
		listOpts = append(listOpts, client.MatchingLabelsSelector{Selector: labelSelector})
	}

	if err := r.List(ctx, nodeList, listOpts...); err != nil {
		return nil, err
	}

	var readyNodes []corev1.Node
	for _, node := range nodeList.Items {
		if r.isNodeReady(&node) {
			readyNodes = append(readyNodes, node)
		}
	}

	return readyNodes, nil
}

// isNodeReady checks if a node is in Ready condition
func (r *ProxyVIPReconciler) isNodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// getVIPNodeExclusions returns the cluster-wide VIP node exclusion label keys.
// Returns nil if no NovaEdgeCluster resource exists or none have exclusions configured.
func (r *ProxyVIPReconciler) getVIPNodeExclusions(ctx context.Context) []string {
	clusterList := &novaedgev1alpha1.NovaEdgeClusterList{}
	if err := r.List(ctx, clusterList); err != nil || len(clusterList.Items) == 0 {
		return nil
	}
	return clusterList.Items[0].Spec.VipNodeExclusions
}

// electActiveNode selects the active node for L2ARP mode
// Uses alphabetical ordering for deterministic selection
func (r *ProxyVIPReconciler) electActiveNode(nodes []corev1.Node) string {
	if len(nodes) == 0 {
		return ""
	}

	// Sort nodes alphabetically by name for deterministic selection
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Name < nodes[j].Name
	})

	// Return first node (alphabetically)
	return nodes[0].Name
}

// updateVIPStatus updates the VIP status with active/announcing nodes
func (r *ProxyVIPReconciler) updateVIPStatus(ctx context.Context, vip *novaedgev1alpha1.ProxyVIP, activeNode string, announcingNodes []string) error {
	// Update status fields
	vip.Status.ActiveNode = activeNode
	vip.Status.AnnouncingNodes = announcingNodes
	vip.Status.ObservedGeneration = vip.Generation

	// Set condition
	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: vip.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             "VIPAssigned",
		Message:            "VIP has been assigned to node(s)",
	}

	if activeNode == "" && len(announcingNodes) == 0 {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "NoNodesAvailable"
		condition.Message = "No candidate nodes available for VIP"
	}

	// Update or add condition
	setCondition(&vip.Status.Conditions, condition)

	// Validate LB policies for ECMP compatibility
	r.validateLBPoliciesForECMP(ctx, vip)

	// Update status
	return r.Status().Update(ctx, vip)
}

// setCondition sets or updates a condition in the condition list
func setCondition(conditions *[]metav1.Condition, newCondition metav1.Condition) {
	if conditions == nil {
		*conditions = []metav1.Condition{}
	}

	// Find existing condition
	for i, condition := range *conditions {
		if condition.Type == newCondition.Type {
			// Update existing condition
			(*conditions)[i] = newCondition
			return
		}
	}

	// Add new condition
	*conditions = append(*conditions, newCondition)
}

// validateLBPoliciesForECMP checks if backends use hash-based LB when VIP is in ECMP mode (BGP/OSPF).
// Sets a LBPolicyValid condition on the VIP status.
func (r *ProxyVIPReconciler) validateLBPoliciesForECMP(ctx context.Context, vip *novaedgev1alpha1.ProxyVIP) {
	// Only validate for ECMP modes
	if vip.Spec.Mode != novaedgev1alpha1.VIPModeBGP && vip.Spec.Mode != novaedgev1alpha1.VIPModeOSPF {
		removeCondition(&vip.Status.Conditions, "LBPolicyValid")
		return
	}

	backendList := &novaedgev1alpha1.ProxyBackendList{}
	if err := r.List(ctx, backendList); err != nil {
		return
	}

	var invalidBackends []string
	for _, backend := range backendList.Items {
		switch backend.Spec.LBPolicy {
		case novaedgev1alpha1.LBPolicyMaglev, novaedgev1alpha1.LBPolicyRingHash, "":
			// Valid for ECMP (empty will be auto-promoted to Maglev at snapshot time)
		default:
			invalidBackends = append(invalidBackends, backend.Namespace+"/"+backend.Name)
		}
	}

	condition := metav1.Condition{
		Type:               "LBPolicyValid",
		ObservedGeneration: vip.Generation,
		LastTransitionTime: metav1.Now(),
	}

	if len(invalidBackends) == 0 {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "AllPoliciesCompatible"
		condition.Message = "All backends use hash-based LB compatible with ECMP"
	} else {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "IncompatibleLBPolicy"
		condition.Message = fmt.Sprintf("Backends with non-hash LB will be excluded from ECMP routing: %s. Use Maglev or RingHash.", strings.Join(invalidBackends, ", "))
	}

	setCondition(&vip.Status.Conditions, condition)
}

// removeCondition removes a condition by type from the condition list
func removeCondition(conditions *[]metav1.Condition, conditionType string) {
	if conditions == nil {
		return
	}
	for i, c := range *conditions {
		if c.Type == conditionType {
			*conditions = append((*conditions)[:i], (*conditions)[i+1:]...)
			return
		}
	}
}

// SetupWithManager sets up the controller with the Manager
func (r *ProxyVIPReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&novaedgev1alpha1.ProxyVIP{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&novaedgev1alpha1.NovaEdgeCluster{},
			handler.EnqueueRequestsFromMapFunc(r.allVIPsMapper),
		).
		Complete(r)
}

// allVIPsMapper enqueues all ProxyVIP resources for reconciliation when
// NovaEdgeCluster changes (e.g., vipNodeExclusions updated).
func (r *ProxyVIPReconciler) allVIPsMapper(ctx context.Context, _ client.Object) []reconcile.Request {
	vipList := &novaedgev1alpha1.ProxyVIPList{}
	if err := r.List(ctx, vipList); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, len(vipList.Items))
	for i, vip := range vipList.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      vip.Name,
				Namespace: vip.Namespace,
			},
		}
	}
	return requests
}
