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
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

// ProxyVIPReconciler reconciles a ProxyVIP object
type ProxyVIPReconciler struct {
	client.Client
	Scheme *runtime.Scheme
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

	// Get candidate nodes based on NodeSelector
	candidateNodes, err := r.getCandidateNodes(ctx, vip.Spec.NodeSelector)
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

// getCandidateNodes returns nodes that match the NodeSelector and are ready
func (r *ProxyVIPReconciler) getCandidateNodes(ctx context.Context, nodeSelector *metav1.LabelSelector) ([]corev1.Node, error) {
	nodeList := &corev1.NodeList{}

	// Build label selector
	var listOpts []client.ListOption
	if nodeSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(nodeSelector)
		if err != nil {
			return nil, err
		}
		listOpts = append(listOpts, client.MatchingLabelsSelector{Selector: selector})
	}

	if err := r.List(ctx, nodeList, listOpts...); err != nil {
		return nil, err
	}

	// Filter for ready nodes only
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

// SetupWithManager sets up the controller with the Manager
func (r *ProxyVIPReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&novaedgev1alpha1.ProxyVIP{}).
		Complete(r)
}
