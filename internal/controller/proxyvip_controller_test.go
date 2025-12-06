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
	"k8s.io/apimachinery/pkg/types"

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
