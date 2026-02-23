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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

func TestProxyVIPL2ARPMode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	// Create nodes with matching labels
	nodes := []*corev1.Node{
		createNode("node-1"),
		createNode("node-2"),
		createNode("node-3"),
	}

	for _, node := range nodes {
		node.Labels = map[string]string{"node-type": "load-balancer"}
		if err := env.client.Create(ctx, node); err != nil {
			t.Fatalf("failed to create Node: %v", err)
		}
	}

	// Create VIP with L2ARP mode
	vip := &novaedgev1alpha1.ProxyVIP{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vip",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyVIPSpec{
			Address: "10.0.0.1/32",
			Mode:    novaedgev1alpha1.VIPModeL2ARP,
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"node-type": "load-balancer",
				},
			},
			Ports: []int32{80, 443},
		},
	}

	if err := env.client.Create(ctx, vip); err != nil {
		t.Fatalf("failed to create VIP: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileProxyVIP(ctx, vip.Name, vip.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	updatedVIP := &novaedgev1alpha1.ProxyVIP{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      vip.Name,
		Namespace: vip.Namespace,
	}, updatedVIP); err != nil {
		t.Fatalf("failed to get VIP: %v", err)
	}

	// In L2ARP mode, exactly one node should be elected as active
	if updatedVIP.Status.ActiveNode == "" {
		t.Error("expected active node to be elected in L2ARP mode")
	}

	// Verify active node is one of our candidate nodes
	activeNodeValid := false
	for _, node := range nodes {
		if updatedVIP.Status.ActiveNode == node.Name {
			activeNodeValid = true
			break
		}
	}

	if !activeNodeValid {
		t.Errorf("active node %s is not one of the candidate nodes", updatedVIP.Status.ActiveNode)
	}

	// In L2ARP mode, the controller selects alphabetically first node
	if updatedVIP.Status.ActiveNode != "node-1" {
		t.Errorf("expected node-1 (alphabetically first), got %s", updatedVIP.Status.ActiveNode)
	}
}

func TestProxyVIPBGPMode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	// Create nodes with matching labels
	nodes := []*corev1.Node{
		createNode("node-1"),
		createNode("node-2"),
		createNode("node-3"),
	}

	for _, node := range nodes {
		node.Labels = map[string]string{"node-type": "load-balancer"}
		if err := env.client.Create(ctx, node); err != nil {
			t.Fatalf("failed to create Node: %v", err)
		}
	}

	// Create VIP with BGP mode
	vip := &novaedgev1alpha1.ProxyVIP{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bgp-vip",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyVIPSpec{
			Address: "10.0.0.2/32",
			Mode:    novaedgev1alpha1.VIPModeBGP,
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"node-type": "load-balancer",
				},
			},
		},
	}

	if err := env.client.Create(ctx, vip); err != nil {
		t.Fatalf("failed to create VIP: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileProxyVIP(ctx, vip.Name, vip.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	updatedVIP := &novaedgev1alpha1.ProxyVIP{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      vip.Name,
		Namespace: vip.Namespace,
	}, updatedVIP); err != nil {
		t.Fatalf("failed to get VIP: %v", err)
	}

	// In BGP mode, all candidate nodes should be announcing
	if len(updatedVIP.Status.AnnouncingNodes) != len(nodes) {
		t.Errorf("expected %d announcing nodes, got %d", len(nodes), len(updatedVIP.Status.AnnouncingNodes))
	}

	// Verify all nodes are in announcing list
	for _, node := range nodes {
		nodeFound := false
		for _, announcingNode := range updatedVIP.Status.AnnouncingNodes {
			if announcingNode == node.Name {
				nodeFound = true
				break
			}
		}

		if !nodeFound {
			t.Errorf("node %s not in announcing nodes list", node.Name)
		}
	}
}

func TestProxyVIPOSPFMode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	// Create nodes with matching labels
	nodes := []*corev1.Node{
		createNode("node-1"),
		createNode("node-2"),
	}

	for _, node := range nodes {
		node.Labels = map[string]string{"node-type": "load-balancer"}
		if err := env.client.Create(ctx, node); err != nil {
			t.Fatalf("failed to create Node: %v", err)
		}
	}

	// Create VIP with OSPF mode
	vip := &novaedgev1alpha1.ProxyVIP{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ospf-vip",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyVIPSpec{
			Address: "10.0.0.3/32",
			Mode:    novaedgev1alpha1.VIPModeOSPF,
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"node-type": "load-balancer",
				},
			},
		},
	}

	if err := env.client.Create(ctx, vip); err != nil {
		t.Fatalf("failed to create VIP: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileProxyVIP(ctx, vip.Name, vip.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	updatedVIP := &novaedgev1alpha1.ProxyVIP{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      vip.Name,
		Namespace: vip.Namespace,
	}, updatedVIP); err != nil {
		t.Fatalf("failed to get VIP: %v", err)
	}

	// In OSPF mode, all candidate nodes should be announcing
	if len(updatedVIP.Status.AnnouncingNodes) != len(nodes) {
		t.Errorf("expected %d announcing nodes, got %d", len(nodes), len(updatedVIP.Status.AnnouncingNodes))
	}
}

func TestProxyVIPNodeSelection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	// Create nodes with different labels
	node1 := createNode("lb-node-1")
	node1.Labels = map[string]string{
		"node-type": "load-balancer",
		"region":    "us-west",
	}

	node2 := createNode("compute-node-1")
	node2.Labels = map[string]string{
		"node-type": "compute",
		"region":    "us-west",
	}

	node3 := createNode("lb-node-2")
	node3.Labels = map[string]string{
		"node-type": "load-balancer",
		"region":    "us-east",
	}

	if err := env.client.Create(ctx, node1); err != nil {
		t.Fatalf("failed to create Node 1: %v", err)
	}

	if err := env.client.Create(ctx, node2); err != nil {
		t.Fatalf("failed to create Node 2: %v", err)
	}

	if err := env.client.Create(ctx, node3); err != nil {
		t.Fatalf("failed to create Node 3: %v", err)
	}

	// Create VIP with label selector
	vip := &novaedgev1alpha1.ProxyVIP{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "selector-vip",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyVIPSpec{
			Address: "10.0.0.4/32",
			Mode:    novaedgev1alpha1.VIPModeL2ARP,
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"node-type": "load-balancer",
				},
			},
		},
	}

	if err := env.client.Create(ctx, vip); err != nil {
		t.Fatalf("failed to create VIP: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileProxyVIP(ctx, vip.Name, vip.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	updatedVIP := &novaedgev1alpha1.ProxyVIP{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      vip.Name,
		Namespace: vip.Namespace,
	}, updatedVIP); err != nil {
		t.Fatalf("failed to get VIP: %v", err)
	}

	// Only load-balancer nodes should be selected (node1 and node3)
	// lb-node-1 is alphabetically first
	if updatedVIP.Status.ActiveNode != "lb-node-1" && updatedVIP.Status.ActiveNode != "lb-node-2" {
		t.Errorf("active node should be one of the load-balancer nodes, got %s", updatedVIP.Status.ActiveNode)
	}
}

func TestProxyVIPNoMatchingNodes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	// Create nodes without matching labels
	node := createNode("compute-node")
	node.Labels = map[string]string{"node-type": "compute"}

	if err := env.client.Create(ctx, node); err != nil {
		t.Fatalf("failed to create Node: %v", err)
	}

	// Create VIP with selector that won't match any nodes
	vip := &novaedgev1alpha1.ProxyVIP{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "no-match-vip",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyVIPSpec{
			Address: "10.0.0.5/32",
			Mode:    novaedgev1alpha1.VIPModeL2ARP,
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"node-type": "load-balancer",
				},
			},
		},
	}

	if err := env.client.Create(ctx, vip); err != nil {
		t.Fatalf("failed to create VIP: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileProxyVIP(ctx, vip.Name, vip.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	updatedVIP := &novaedgev1alpha1.ProxyVIP{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      vip.Name,
		Namespace: vip.Namespace,
	}, updatedVIP); err != nil {
		t.Fatalf("failed to get VIP: %v", err)
	}

	// No active node should be assigned
	if updatedVIP.Status.ActiveNode != "" {
		t.Errorf("expected no active node when no nodes match, got %s", updatedVIP.Status.ActiveNode)
	}
}

func TestProxyVIPMultiplePorts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	node := createNode("lb-node")
	node.Labels = map[string]string{"node-type": "load-balancer"}

	if err := env.client.Create(ctx, node); err != nil {
		t.Fatalf("failed to create Node: %v", err)
	}

	vip := &novaedgev1alpha1.ProxyVIP{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "multi-port-vip",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyVIPSpec{
			Address: "10.0.0.6/32",
			Mode:    novaedgev1alpha1.VIPModeL2ARP,
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"node-type": "load-balancer",
				},
			},
			Ports: []int32{80, 443, 8080, 8443},
		},
	}

	if err := env.client.Create(ctx, vip); err != nil {
		t.Fatalf("failed to create VIP: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileProxyVIP(ctx, vip.Name, vip.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	updatedVIP := &novaedgev1alpha1.ProxyVIP{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      vip.Name,
		Namespace: vip.Namespace,
	}, updatedVIP); err != nil {
		t.Fatalf("failed to get VIP: %v", err)
	}

	if len(updatedVIP.Spec.Ports) != 4 {
		t.Errorf("expected 4 ports, got %d", len(updatedVIP.Spec.Ports))
	}

	if updatedVIP.Status.ActiveNode == "" {
		t.Error("expected active node to be assigned for multi-port VIP")
	}
}

func TestProxyVIPFailover(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	// Create initial nodes
	activeNode := createNode("lb-node-1")
	activeNode.Labels = map[string]string{"node-type": "load-balancer"}

	backupNode := createNode("lb-node-2")
	backupNode.Labels = map[string]string{"node-type": "load-balancer"}

	if err := env.client.Create(ctx, activeNode); err != nil {
		t.Fatalf("failed to create active node: %v", err)
	}

	if err := env.client.Create(ctx, backupNode); err != nil {
		t.Fatalf("failed to create backup node: %v", err)
	}

	vip := &novaedgev1alpha1.ProxyVIP{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "failover-vip",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyVIPSpec{
			Address: "10.0.0.7/32",
			Mode:    novaedgev1alpha1.VIPModeL2ARP,
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"node-type": "load-balancer",
				},
			},
		},
	}

	if err := env.client.Create(ctx, vip); err != nil {
		t.Fatalf("failed to create VIP: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileProxyVIP(ctx, vip.Name, vip.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	// Verify active node is assigned
	updatedVIP1 := &novaedgev1alpha1.ProxyVIP{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      vip.Name,
		Namespace: vip.Namespace,
	}, updatedVIP1); err != nil {
		t.Fatalf("failed to get VIP: %v", err)
	}

	originalActiveNode := updatedVIP1.Status.ActiveNode
	if originalActiveNode == "" {
		t.Fatal("expected active node to be assigned")
	}

	// lb-node-1 should be elected (alphabetically first)
	if originalActiveNode != "lb-node-1" {
		t.Errorf("expected lb-node-1 as initial active node, got %s", originalActiveNode)
	}

	// Simulate failover by marking active node as not ready
	activeNode.Status.Conditions = []corev1.NodeCondition{
		{
			Type:   corev1.NodeReady,
			Status: corev1.ConditionFalse,
		},
	}
	if err := env.client.Status().Update(ctx, activeNode); err != nil {
		t.Fatalf("failed to update active node status: %v", err)
	}

	// Trigger re-reconciliation
	if err := env.reconcileProxyVIP(ctx, vip.Name, vip.Namespace); err != nil {
		t.Fatalf("reconciliation failed after node failure: %v", err)
	}

	// After failover, lb-node-2 should be active
	updatedVIP2 := &novaedgev1alpha1.ProxyVIP{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      vip.Name,
		Namespace: vip.Namespace,
	}, updatedVIP2); err != nil {
		t.Fatalf("failed to get VIP after failover: %v", err)
	}

	if updatedVIP2.Status.ActiveNode != "lb-node-2" {
		t.Errorf("expected lb-node-2 after failover, got %s", updatedVIP2.Status.ActiveNode)
	}
}

func TestProxyVIPDeletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	node := createNode("lb-node")
	node.Labels = map[string]string{"node-type": "load-balancer"}

	if err := env.client.Create(ctx, node); err != nil {
		t.Fatalf("failed to create Node: %v", err)
	}

	vip := &novaedgev1alpha1.ProxyVIP{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "delete-vip",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyVIPSpec{
			Address: "10.0.0.8/32",
			Mode:    novaedgev1alpha1.VIPModeL2ARP,
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"node-type": "load-balancer",
				},
			},
		},
	}

	if err := env.client.Create(ctx, vip); err != nil {
		t.Fatalf("failed to create VIP: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileProxyVIP(ctx, vip.Name, vip.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	if err := env.client.Delete(ctx, vip); err != nil {
		t.Fatalf("failed to delete VIP: %v", err)
	}

	// Verify VIP is deleted (or marked for deletion)
	deletedVIP := &novaedgev1alpha1.ProxyVIP{}
	err := env.client.Get(ctx, types.NamespacedName{
		Name:      vip.Name,
		Namespace: vip.Namespace,
	}, deletedVIP)

	if err == nil && deletedVIP.DeletionTimestamp == nil {
		t.Error("expected VIP to be deleted or marked for deletion")
	}
}

func TestValidateLBPoliciesForECMP(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)

	tests := []struct {
		name              string
		vipMode           novaedgev1alpha1.VIPMode
		backendPolicy     novaedgev1alpha1.LoadBalancingPolicy
		expectValid       bool
		expectNoCondition bool
	}{
		{
			name:          "BGP with Maglev - valid",
			vipMode:       novaedgev1alpha1.VIPModeBGP,
			backendPolicy: novaedgev1alpha1.LBPolicyMaglev,
			expectValid:   true,
		},
		{
			name:          "BGP with RingHash - valid",
			vipMode:       novaedgev1alpha1.VIPModeBGP,
			backendPolicy: novaedgev1alpha1.LBPolicyRingHash,
			expectValid:   true,
		},
		{
			name:          "BGP with unspecified - valid (will be auto-promoted)",
			vipMode:       novaedgev1alpha1.VIPModeBGP,
			backendPolicy: "",
			expectValid:   true,
		},
		{
			name:          "BGP with RoundRobin - invalid",
			vipMode:       novaedgev1alpha1.VIPModeBGP,
			backendPolicy: novaedgev1alpha1.LBPolicyRoundRobin,
			expectValid:   false,
		},
		{
			name:          "OSPF with LeastConn - invalid",
			vipMode:       novaedgev1alpha1.VIPModeOSPF,
			backendPolicy: novaedgev1alpha1.LBPolicyLeastConn,
			expectValid:   false,
		},
		{
			name:              "L2ARP with RoundRobin - no condition",
			vipMode:           novaedgev1alpha1.VIPModeL2ARP,
			backendPolicy:     novaedgev1alpha1.LBPolicyRoundRobin,
			expectNoCondition: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vip := &novaedgev1alpha1.ProxyVIP{
				ObjectMeta: metav1.ObjectMeta{Name: "test-vip", Generation: 1},
				Spec: novaedgev1alpha1.ProxyVIPSpec{
					Address: "10.0.0.1/32",
					Mode:    tt.vipMode,
				},
			}
			backend := &novaedgev1alpha1.ProxyBackend{
				ObjectMeta: metav1.ObjectMeta{Name: "test-backend", Namespace: "default"},
				Spec:       novaedgev1alpha1.ProxyBackendSpec{LBPolicy: tt.backendPolicy},
			}

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(backend).Build()
			reconciler := &ProxyVIPReconciler{Client: fakeClient, Scheme: scheme}
			reconciler.validateLBPoliciesForECMP(context.Background(), vip)

			var found *metav1.Condition
			for i := range vip.Status.Conditions {
				if vip.Status.Conditions[i].Type == "LBPolicyValid" {
					found = &vip.Status.Conditions[i]
					break
				}
			}

			if tt.expectNoCondition {
				if found != nil {
					t.Errorf("expected no LBPolicyValid condition, got %v", found)
				}
				return
			}

			if found == nil {
				t.Fatal("expected LBPolicyValid condition, got none")
			}

			if tt.expectValid && found.Status != metav1.ConditionTrue {
				t.Errorf("expected condition True, got %v", found.Status)
			}
			if !tt.expectValid && found.Status != metav1.ConditionFalse {
				t.Errorf("expected condition False, got %v", found.Status)
			}
		})
	}
}

// TestSetConditionNilPointer verifies that setCondition does not panic when
// called with a nil conditions pointer (regression test for #580).
func TestSetConditionNilPointer(t *testing.T) {
	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Test",
		Message:            "test message",
		LastTransitionTime: metav1.Now(),
	}

	// This must not panic.
	setCondition(nil, condition)
}

// TestSetConditionAddsAndUpdates verifies normal setCondition behaviour: adding
// a new condition and updating an existing one.
func TestSetConditionAddsAndUpdates(t *testing.T) {
	conditions := []metav1.Condition{}

	first := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "Initializing",
		LastTransitionTime: metav1.Now(),
	}
	setCondition(&conditions, first)
	if len(conditions) != 1 {
		t.Fatalf("expected 1 condition after add, got %d", len(conditions))
	}
	if conditions[0].Status != metav1.ConditionFalse {
		t.Errorf("expected ConditionFalse, got %v", conditions[0].Status)
	}

	// Update the same condition type.
	updated := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Running",
		LastTransitionTime: metav1.Now(),
	}
	setCondition(&conditions, updated)
	if len(conditions) != 1 {
		t.Fatalf("expected 1 condition after update, got %d", len(conditions))
	}
	if conditions[0].Status != metav1.ConditionTrue {
		t.Errorf("expected ConditionTrue after update, got %v", conditions[0].Status)
	}
}

// Helper function to create a test node
func createNode(name string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: corev1.NodeSpec{
			Unschedulable: false,
		},
		Status: corev1.NodeStatus{
			Phase: corev1.NodeRunning,
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
}

// TestVIPNodeExclusions_ExcludesControlPlaneByDefault verifies that when
// NovaEdgeCluster sets vipNodeExclusions, a VIP without tolerations is not
// scheduled on excluded nodes.
func TestVIPNodeExclusions_ExcludesControlPlaneByDefault(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	// Create one worker node and one control-plane node
	worker := createNode("worker-1")
	worker.Labels = map[string]string{"node-role.kubernetes.io/worker": "true"}
	if err := env.client.Create(ctx, worker); err != nil {
		t.Fatalf("failed to create worker node: %v", err)
	}

	master := createNode("master-1")
	master.Labels = map[string]string{"node-role.kubernetes.io/control-plane": "true"}
	if err := env.client.Create(ctx, master); err != nil {
		t.Fatalf("failed to create master node: %v", err)
	}

	// Configure cluster-wide exclusion
	cluster := &novaedgev1alpha1.NovaEdgeCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "novaedge"},
		Spec: novaedgev1alpha1.NovaEdgeClusterSpec{
			Version:           "latest",
			VipNodeExclusions: []string{"node-role.kubernetes.io/control-plane"},
		},
	}
	if err := env.client.Create(ctx, cluster); err != nil {
		t.Fatalf("failed to create NovaEdgeCluster: %v", err)
	}

	// VIP with no tolerations — should land only on worker
	vip := &novaedgev1alpha1.ProxyVIP{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vip", Namespace: "default"},
		Spec: novaedgev1alpha1.ProxyVIPSpec{
			Address: "10.0.0.1/32",
			Mode:    novaedgev1alpha1.VIPModeBGP,
			Ports:   []int32{80},
			BGPConfig: &novaedgev1alpha1.BGPConfig{
				LocalAS:  65000,
				RouterID: "0.0.0.0",
			},
		},
	}
	if err := env.client.Create(ctx, vip); err != nil {
		t.Fatalf("failed to create VIP: %v", err)
	}

	if err := env.reconcileProxyVIP(ctx, vip.Name, vip.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	updated := &novaedgev1alpha1.ProxyVIP{}
	if err := env.client.Get(ctx, types.NamespacedName{Name: vip.Name, Namespace: vip.Namespace}, updated); err != nil {
		t.Fatalf("failed to get VIP: %v", err)
	}

	// Must NOT include master-1
	for _, node := range updated.Status.AnnouncingNodes {
		if node == "master-1" {
			t.Errorf("master-1 should be excluded from announcing nodes, got: %v", updated.Status.AnnouncingNodes)
		}
	}
	// Must include worker-1
	found := false
	for _, node := range updated.Status.AnnouncingNodes {
		if node == "worker-1" {
			found = true
		}
	}
	if !found {
		t.Errorf("worker-1 should be in announcing nodes, got: %v", updated.Status.AnnouncingNodes)
	}
}

// TestVIPNodeExclusions_TolerationsAllowMasterNodes verifies that a VIP with
// a toleration for the excluded key is scheduled on the excluded nodes.
func TestVIPNodeExclusions_TolerationsAllowMasterNodes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	master := createNode("master-1")
	master.Labels = map[string]string{"node-role.kubernetes.io/control-plane": "true"}
	if err := env.client.Create(ctx, master); err != nil {
		t.Fatalf("failed to create master node: %v", err)
	}

	cluster := &novaedgev1alpha1.NovaEdgeCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "novaedge"},
		Spec: novaedgev1alpha1.NovaEdgeClusterSpec{
			Version:           "latest",
			VipNodeExclusions: []string{"node-role.kubernetes.io/control-plane"},
		},
	}
	if err := env.client.Create(ctx, cluster); err != nil {
		t.Fatalf("failed to create NovaEdgeCluster: %v", err)
	}

	// VIP with toleration — should land on master
	vip := &novaedgev1alpha1.ProxyVIP{
		ObjectMeta: metav1.ObjectMeta{Name: "cp-vip", Namespace: "default"},
		Spec: novaedgev1alpha1.ProxyVIPSpec{
			Address: "10.0.0.2/32",
			Mode:    novaedgev1alpha1.VIPModeBGP,
			Ports:   []int32{6443},
			BGPConfig: &novaedgev1alpha1.BGPConfig{
				LocalAS:  65000,
				RouterID: "0.0.0.0",
			},
			Tolerations: []string{"node-role.kubernetes.io/control-plane"},
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"node-role.kubernetes.io/control-plane": "true"},
			},
		},
	}
	if err := env.client.Create(ctx, vip); err != nil {
		t.Fatalf("failed to create VIP: %v", err)
	}

	if err := env.reconcileProxyVIP(ctx, vip.Name, vip.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	updated := &novaedgev1alpha1.ProxyVIP{}
	if err := env.client.Get(ctx, types.NamespacedName{Name: vip.Name, Namespace: vip.Namespace}, updated); err != nil {
		t.Fatalf("failed to get VIP: %v", err)
	}

	found := false
	for _, node := range updated.Status.AnnouncingNodes {
		if node == "master-1" {
			found = true
		}
	}
	if !found {
		t.Errorf("master-1 should be in announcing nodes when toleration is set, got: %v", updated.Status.AnnouncingNodes)
	}
}

// TestVIPNodeExclusions_EmptyExclusionsNoChange verifies that without
// vipNodeExclusions configured, all ready nodes are candidates (no regression).
func TestVIPNodeExclusions_EmptyExclusionsNoChange(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	for _, name := range []string{"worker-1", "master-1"} {
		n := createNode(name)
		if name == "master-1" {
			n.Labels = map[string]string{"node-role.kubernetes.io/control-plane": "true"}
		}
		if err := env.client.Create(ctx, n); err != nil {
			t.Fatalf("failed to create node %s: %v", name, err)
		}
	}

	// No NovaEdgeCluster created → no exclusions

	vip := &novaedgev1alpha1.ProxyVIP{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vip", Namespace: "default"},
		Spec: novaedgev1alpha1.ProxyVIPSpec{
			Address: "10.0.0.1/32",
			Mode:    novaedgev1alpha1.VIPModeBGP,
			Ports:   []int32{80},
			BGPConfig: &novaedgev1alpha1.BGPConfig{
				LocalAS:  65000,
				RouterID: "0.0.0.0",
			},
		},
	}
	if err := env.client.Create(ctx, vip); err != nil {
		t.Fatalf("failed to create VIP: %v", err)
	}

	if err := env.reconcileProxyVIP(ctx, vip.Name, vip.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	updated := &novaedgev1alpha1.ProxyVIP{}
	if err := env.client.Get(ctx, types.NamespacedName{Name: vip.Name, Namespace: vip.Namespace}, updated); err != nil {
		t.Fatalf("failed to get VIP: %v", err)
	}

	if len(updated.Status.AnnouncingNodes) != 2 {
		t.Errorf("expected both nodes (worker-1 and master-1) to be candidates without exclusions, got: %v", updated.Status.AnnouncingNodes)
	}
}

// TestVIPNodeExclusions_PartialTolerations verifies that with multiple exclusion
// keys, only tolerated keys are allowed through.
func TestVIPNodeExclusions_PartialTolerations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	master := createNode("master-1")
	master.Labels = map[string]string{"node-role.kubernetes.io/control-plane": "true"}
	if err := env.client.Create(ctx, master); err != nil {
		t.Fatalf("failed to create master node: %v", err)
	}

	gpu := createNode("gpu-1")
	gpu.Labels = map[string]string{"accelerator": "nvidia-gpu"}
	if err := env.client.Create(ctx, gpu); err != nil {
		t.Fatalf("failed to create gpu node: %v", err)
	}

	worker := createNode("worker-1")
	if err := env.client.Create(ctx, worker); err != nil {
		t.Fatalf("failed to create worker node: %v", err)
	}

	cluster := &novaedgev1alpha1.NovaEdgeCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "novaedge"},
		Spec: novaedgev1alpha1.NovaEdgeClusterSpec{
			Version: "latest",
			VipNodeExclusions: []string{
				"node-role.kubernetes.io/control-plane",
				"accelerator",
			},
		},
	}
	if err := env.client.Create(ctx, cluster); err != nil {
		t.Fatalf("failed to create NovaEdgeCluster: %v", err)
	}

	// Tolerate control-plane but NOT accelerator
	vip := &novaedgev1alpha1.ProxyVIP{
		ObjectMeta: metav1.ObjectMeta{Name: "partial-vip", Namespace: "default"},
		Spec: novaedgev1alpha1.ProxyVIPSpec{
			Address: "10.0.0.3/32",
			Mode:    novaedgev1alpha1.VIPModeBGP,
			Ports:   []int32{80},
			BGPConfig: &novaedgev1alpha1.BGPConfig{
				LocalAS:  65000,
				RouterID: "0.0.0.0",
			},
			Tolerations: []string{"node-role.kubernetes.io/control-plane"},
		},
	}
	if err := env.client.Create(ctx, vip); err != nil {
		t.Fatalf("failed to create VIP: %v", err)
	}

	if err := env.reconcileProxyVIP(ctx, vip.Name, vip.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	updated := &novaedgev1alpha1.ProxyVIP{}
	if err := env.client.Get(ctx, types.NamespacedName{Name: vip.Name, Namespace: vip.Namespace}, updated); err != nil {
		t.Fatalf("failed to get VIP: %v", err)
	}

	announcing := make(map[string]bool)
	for _, n := range updated.Status.AnnouncingNodes {
		announcing[n] = true
	}

	if !announcing["worker-1"] {
		t.Errorf("worker-1 should be in announcing nodes, got: %v", updated.Status.AnnouncingNodes)
	}
	if !announcing["master-1"] {
		t.Errorf("master-1 should be in announcing nodes (tolerated), got: %v", updated.Status.AnnouncingNodes)
	}
	if announcing["gpu-1"] {
		t.Errorf("gpu-1 should NOT be in announcing nodes (not tolerated), got: %v", updated.Status.AnnouncingNodes)
	}
}
