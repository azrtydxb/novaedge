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

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
	"github.com/azrtydxb/novaedge/internal/controller/ipam"
)

const (
	// testNamespace is the default namespace used for test resources.
	testNamespace = "default"
	// testServiceKind is the expected Kubernetes Kind for Service resources.
	testServiceKind = "Service"
)

// serviceTestEnv holds the test environment for ServiceReconciler tests.
type serviceTestEnv struct {
	*testEnv
	serviceReconciler *ServiceReconciler
	allocator         *ipam.Allocator
	recorder          *record.FakeRecorder
}

// setupServiceTestEnv creates a test environment with the ServiceReconciler
// and a pre-configured IPAM pool.
func setupServiceTestEnv(t *testing.T) *serviceTestEnv {
	t.Helper()

	env := setupTestEnv(t)

	zapLogger, _ := zap.NewDevelopment()
	allocator := ipam.NewAllocator(zapLogger)

	// Add a default pool with test addresses
	if err := allocator.AddPool(defaultPoolName, []string{"10.200.0.0/24"}, nil); err != nil {
		t.Fatalf("failed to add default pool: %v", err)
	}

	fakeRecorder := record.NewFakeRecorder(32)

	// Rebuild the fake client with Service status subresource support
	k8sClient := env.client

	reconciler := &ServiceReconciler{
		Client:          k8sClient,
		Scheme:          env.scheme,
		Allocator:       allocator,
		Recorder:        fakeRecorder,
		EnableServiceLB: true,
	}

	return &serviceTestEnv{
		testEnv:           env,
		serviceReconciler: reconciler,
		allocator:         allocator,
		recorder:          fakeRecorder,
	}
}

// reconcileService triggers a reconciliation for the given Service.
func (e *serviceTestEnv) reconcileService(ctx context.Context, name, namespace string) error {
	_, err := e.serviceReconciler.Reconcile(ctx, newRequest(name, namespace))
	return err
}

// newRequest creates a ctrl.Request for the given namespaced name.
func newRequest(name, namespace string) ctrl.Request {
	return ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
	}
}

// createLoadBalancerService creates a Service of type LoadBalancer with the given
// annotations and ports in the testNamespace.
func createLoadBalancerService(name string, annotations map[string]string, ports []corev1.ServicePort) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   testNamespace,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: ports,
		},
	}
}

func TestServiceWithoutAnnotationIsIgnored(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupServiceTestEnv(t)

	svc := createLoadBalancerService("no-annotation-svc", nil,
		[]corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}})

	if err := env.client.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := env.reconcileService(ctx, svc.Name, svc.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	// No ProxyVIP should be created
	vip := &novaedgev1alpha1.ProxyVIP{}
	err := env.client.Get(ctx, types.NamespacedName{
		Name:      vipNamePrefix + svc.Name,
		Namespace: svc.Namespace,
	}, vip)
	if err == nil {
		t.Error("expected no ProxyVIP to be created for Service without annotation")
	}
}

func TestServiceClusterIPTypeIsIgnored(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupServiceTestEnv(t)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clusterip-svc",
			Namespace: testNamespace,
			Annotations: map[string]string{
				annotationVIPMode: "L2ARP",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
		},
	}

	if err := env.client.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := env.reconcileService(ctx, svc.Name, svc.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	// No ProxyVIP should be created for ClusterIP services
	vip := &novaedgev1alpha1.ProxyVIP{}
	err := env.client.Get(ctx, types.NamespacedName{
		Name:      vipNamePrefix + svc.Name,
		Namespace: svc.Namespace,
	}, vip)
	if err == nil {
		t.Error("expected no ProxyVIP to be created for ClusterIP Service")
	}
}

func TestServiceWithValidAnnotationCreatesProxyVIP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupServiceTestEnv(t)

	svc := createLoadBalancerService("web-svc",
		map[string]string{
			annotationVIPMode: "L2ARP",
		},
		[]corev1.ServicePort{
			{Port: 80, Protocol: corev1.ProtocolTCP},
			{Port: 443, Protocol: corev1.ProtocolTCP},
		},
	)

	if err := env.client.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := env.reconcileService(ctx, svc.Name, svc.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	// Verify ProxyVIP was created
	vip := &novaedgev1alpha1.ProxyVIP{}
	vipName := vipNamePrefix + svc.Name
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      vipName,
		Namespace: svc.Namespace,
	}, vip); err != nil {
		t.Fatalf("failed to get ProxyVIP: %v", err)
	}

	// Verify mode
	if vip.Spec.Mode != novaedgev1alpha1.VIPModeL2ARP {
		t.Errorf("expected VIP mode L2ARP, got %s", vip.Spec.Mode)
	}

	// Verify address is allocated (should be a CIDR)
	if vip.Spec.Address == "" {
		t.Error("expected VIP address to be allocated")
	}

	// Verify ports match Service ports
	if len(vip.Spec.Ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(vip.Spec.Ports))
	}
	if vip.Spec.Ports[0] != 80 {
		t.Errorf("expected port 80, got %d", vip.Spec.Ports[0])
	}
	if vip.Spec.Ports[1] != 443 {
		t.Errorf("expected port 443, got %d", vip.Spec.Ports[1])
	}

	// Verify pool reference
	if vip.Spec.PoolRef == nil || vip.Spec.PoolRef.Name != defaultPoolName {
		t.Errorf("expected pool ref to 'default', got %v", vip.Spec.PoolRef)
	}

	// Verify labels
	if vip.Labels["novaedge.io/managed-by"] != "service-lb" {
		t.Errorf("expected managed-by label, got %v", vip.Labels)
	}
	if vip.Labels["novaedge.io/service-name"] != svc.Name {
		t.Errorf("expected service-name label %s, got %s", svc.Name, vip.Labels["novaedge.io/service-name"])
	}
}

func TestProxyVIPHasCorrectOwnerReference(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupServiceTestEnv(t)

	svc := createLoadBalancerService("owner-test-svc",
		map[string]string{
			annotationVIPMode: "BGP",
		},
		[]corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
	)

	if err := env.client.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := env.reconcileService(ctx, svc.Name, svc.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	vip := &novaedgev1alpha1.ProxyVIP{}
	vipName := vipNamePrefix + svc.Name
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      vipName,
		Namespace: svc.Namespace,
	}, vip); err != nil {
		t.Fatalf("failed to get ProxyVIP: %v", err)
	}

	// Verify owner reference
	if len(vip.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(vip.OwnerReferences))
	}

	ownerRef := vip.OwnerReferences[0]
	if ownerRef.Kind != testServiceKind {
		t.Errorf("expected owner kind %s, got %s", testServiceKind, ownerRef.Kind)
	}
	if ownerRef.Name != svc.Name {
		t.Errorf("expected owner name %s, got %s", svc.Name, ownerRef.Name)
	}
	if ownerRef.Controller == nil || !*ownerRef.Controller {
		t.Error("expected controller owner reference to be true")
	}
}

func TestServiceStatusUpdatedWithVIPAddress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupServiceTestEnv(t)

	svc := createLoadBalancerService("status-test-svc",
		map[string]string{
			annotationVIPMode: "L2ARP",
		},
		[]corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
	)

	if err := env.client.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := env.reconcileService(ctx, svc.Name, svc.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	// Re-fetch the Service to check status
	updatedSvc := &corev1.Service{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      svc.Name,
		Namespace: svc.Namespace,
	}, updatedSvc); err != nil {
		t.Fatalf("failed to get updated Service: %v", err)
	}

	if len(updatedSvc.Status.LoadBalancer.Ingress) != 1 {
		t.Fatalf("expected 1 load balancer ingress entry, got %d", len(updatedSvc.Status.LoadBalancer.Ingress))
	}

	ingressIP := updatedSvc.Status.LoadBalancer.Ingress[0].IP
	if ingressIP == "" {
		t.Error("expected Service status to have an allocated IP")
	}

	// IP should not contain CIDR notation
	for _, c := range ingressIP {
		if c == '/' {
			t.Errorf("Service status IP should not contain CIDR notation, got %s", ingressIP)
			break
		}
	}
}

func TestInvalidVIPModeAnnotationRecordsEvent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupServiceTestEnv(t)

	svc := createLoadBalancerService("invalid-mode-svc",
		map[string]string{
			annotationVIPMode: "INVALID_MODE",
		},
		[]corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
	)

	if err := env.client.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	// Should not return error (invalid config is not retryable)
	if err := env.reconcileService(ctx, svc.Name, svc.Namespace); err != nil {
		t.Fatalf("expected no error for invalid mode, got: %v", err)
	}

	// No ProxyVIP should be created
	vip := &novaedgev1alpha1.ProxyVIP{}
	err := env.client.Get(ctx, types.NamespacedName{
		Name:      vipNamePrefix + svc.Name,
		Namespace: svc.Namespace,
	}, vip)
	if err == nil {
		t.Error("expected no ProxyVIP for invalid mode annotation")
	}

	// Check that a warning event was recorded
	select {
	case event := <-env.recorder.Events:
		if !contains(event, "InvalidVIPMode") {
			t.Errorf("expected InvalidVIPMode event, got: %s", event)
		}
	default:
		t.Error("expected a warning event for invalid VIP mode")
	}
}

func TestServiceDeletionTriggersIPAMRelease(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupServiceTestEnv(t)

	svc := createLoadBalancerService("delete-test-svc",
		map[string]string{
			annotationVIPMode: "L2ARP",
		},
		[]corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
	)

	if err := env.client.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	// Reconcile to allocate IP
	if err := env.reconcileService(ctx, svc.Name, svc.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	// Verify IP was allocated
	allocated1, _, err := env.allocator.GetPoolStats(defaultPoolName)
	if err != nil {
		t.Fatalf("failed to get pool stats: %v", err)
	}
	if allocated1 == 0 {
		t.Fatal("expected at least one IP allocation after reconciliation")
	}

	// Delete the Service
	if err := env.client.Delete(ctx, svc); err != nil {
		t.Fatalf("failed to delete Service: %v", err)
	}

	// Reconcile again (Service not found triggers cleanup)
	if err := env.reconcileService(ctx, svc.Name, svc.Namespace); err != nil {
		t.Fatalf("reconciliation after deletion failed: %v", err)
	}

	// Verify IP was released
	allocated2, _, err := env.allocator.GetPoolStats(defaultPoolName)
	if err != nil {
		t.Fatalf("failed to get pool stats after release: %v", err)
	}
	if allocated2 >= allocated1 {
		t.Errorf("expected IP allocation count to decrease after deletion, before=%d after=%d", allocated1, allocated2)
	}
}

func TestPoolNotFoundReturnsError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupServiceTestEnv(t)

	svc := createLoadBalancerService("pool-missing-svc",
		map[string]string{
			annotationVIPMode:     "L2ARP",
			annotationAddressPool: "nonexistent-pool",
		},
		[]corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
	)

	if err := env.client.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	// Should return an error because the pool doesn't exist
	err := env.reconcileService(ctx, svc.Name, svc.Namespace)
	if err == nil {
		t.Error("expected error when IP pool does not exist")
	}

	// Check that a warning event was recorded
	select {
	case event := <-env.recorder.Events:
		if !contains(event, "IPAllocationFailed") {
			t.Errorf("expected IPAllocationFailed event, got: %s", event)
		}
	default:
		t.Error("expected a warning event for pool not found")
	}
}

func TestDualStackAddressFamilyAnnotation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupServiceTestEnv(t)

	svc := createLoadBalancerService("dual-stack-svc",
		map[string]string{
			annotationVIPMode:       "L2ARP",
			annotationAddressFamily: "dual",
		},
		[]corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
	)

	if err := env.client.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := env.reconcileService(ctx, svc.Name, svc.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	vip := &novaedgev1alpha1.ProxyVIP{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      vipNamePrefix + svc.Name,
		Namespace: svc.Namespace,
	}, vip); err != nil {
		t.Fatalf("failed to get ProxyVIP: %v", err)
	}

	if vip.Spec.AddressFamily != novaedgev1alpha1.AddressFamilyDual {
		t.Errorf("expected address family 'dual', got %q", vip.Spec.AddressFamily)
	}
}

func TestBGPConfigAnnotationParsing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupServiceTestEnv(t)

	bgpJSON := `{"localAS":65001,"routerID":"10.0.0.1","peers":[{"address":"10.0.0.254","as":65000}]}`

	svc := createLoadBalancerService("bgp-svc",
		map[string]string{
			annotationVIPMode:   "BGP",
			annotationBGPConfig: bgpJSON,
		},
		[]corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
	)

	if err := env.client.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := env.reconcileService(ctx, svc.Name, svc.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	vip := &novaedgev1alpha1.ProxyVIP{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      vipNamePrefix + svc.Name,
		Namespace: svc.Namespace,
	}, vip); err != nil {
		t.Fatalf("failed to get ProxyVIP: %v", err)
	}

	if vip.Spec.Mode != novaedgev1alpha1.VIPModeBGP {
		t.Errorf("expected BGP mode, got %s", vip.Spec.Mode)
	}

	if vip.Spec.BGPConfig == nil {
		t.Fatal("expected BGP config to be set")
	}
	if vip.Spec.BGPConfig.LocalAS != 65001 {
		t.Errorf("expected LocalAS 65001, got %d", vip.Spec.BGPConfig.LocalAS)
	}
	if vip.Spec.BGPConfig.RouterID != "10.0.0.1" {
		t.Errorf("expected RouterID 10.0.0.1, got %s", vip.Spec.BGPConfig.RouterID)
	}
	if len(vip.Spec.BGPConfig.Peers) != 1 {
		t.Fatalf("expected 1 BGP peer, got %d", len(vip.Spec.BGPConfig.Peers))
	}
	if vip.Spec.BGPConfig.Peers[0].Address != "10.0.0.254" {
		t.Errorf("expected peer address 10.0.0.254, got %s", vip.Spec.BGPConfig.Peers[0].Address)
	}
	if vip.Spec.BGPConfig.Peers[0].AS != 65000 {
		t.Errorf("expected peer AS 65000, got %d", vip.Spec.BGPConfig.Peers[0].AS)
	}
}

func TestOSPFConfigAnnotationParsing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupServiceTestEnv(t)

	ospfJSON := `{"routerID":"10.0.0.1","areaID":0,"cost":20}`

	svc := createLoadBalancerService("ospf-svc",
		map[string]string{
			annotationVIPMode:    "OSPF",
			annotationOSPFConfig: ospfJSON,
		},
		[]corev1.ServicePort{{Port: 443, Protocol: corev1.ProtocolTCP}},
	)

	if err := env.client.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := env.reconcileService(ctx, svc.Name, svc.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	vip := &novaedgev1alpha1.ProxyVIP{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      vipNamePrefix + svc.Name,
		Namespace: svc.Namespace,
	}, vip); err != nil {
		t.Fatalf("failed to get ProxyVIP: %v", err)
	}

	if vip.Spec.Mode != novaedgev1alpha1.VIPModeOSPF {
		t.Errorf("expected OSPF mode, got %s", vip.Spec.Mode)
	}

	if vip.Spec.OSPFConfig == nil {
		t.Fatal("expected OSPF config to be set")
	}
	if vip.Spec.OSPFConfig.RouterID != "10.0.0.1" {
		t.Errorf("expected RouterID 10.0.0.1, got %s", vip.Spec.OSPFConfig.RouterID)
	}
	if vip.Spec.OSPFConfig.AreaID != 0 {
		t.Errorf("expected AreaID 0, got %d", vip.Spec.OSPFConfig.AreaID)
	}
	if vip.Spec.OSPFConfig.Cost != 20 {
		t.Errorf("expected cost 20, got %d", vip.Spec.OSPFConfig.Cost)
	}
}

func TestBFDEnabledAnnotation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupServiceTestEnv(t)

	svc := createLoadBalancerService("bfd-svc",
		map[string]string{
			annotationVIPMode:    "BGP",
			annotationBFDEnabled: "true",
		},
		[]corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
	)

	if err := env.client.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := env.reconcileService(ctx, svc.Name, svc.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	vip := &novaedgev1alpha1.ProxyVIP{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      vipNamePrefix + svc.Name,
		Namespace: svc.Namespace,
	}, vip); err != nil {
		t.Fatalf("failed to get ProxyVIP: %v", err)
	}

	if vip.Spec.BFD == nil {
		t.Fatal("expected BFD config to be set")
	}
	if !vip.Spec.BFD.Enabled {
		t.Error("expected BFD to be enabled")
	}
}

func TestNodeSelectorAnnotationParsing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupServiceTestEnv(t)

	nodeSelJSON := `{"matchLabels":{"node-role.kubernetes.io/lb":"true"}}`

	svc := createLoadBalancerService("nodeselector-svc",
		map[string]string{
			annotationVIPMode:      "L2ARP",
			annotationNodeSelector: nodeSelJSON,
		},
		[]corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
	)

	if err := env.client.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := env.reconcileService(ctx, svc.Name, svc.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	vip := &novaedgev1alpha1.ProxyVIP{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      vipNamePrefix + svc.Name,
		Namespace: svc.Namespace,
	}, vip); err != nil {
		t.Fatalf("failed to get ProxyVIP: %v", err)
	}

	if vip.Spec.NodeSelector == nil {
		t.Fatal("expected node selector to be set")
	}
	if vip.Spec.NodeSelector.MatchLabels["node-role.kubernetes.io/lb"] != "true" {
		t.Errorf("expected node selector label, got %v", vip.Spec.NodeSelector.MatchLabels)
	}
}

func TestCustomAddressPoolAnnotation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupServiceTestEnv(t)

	// Add a custom pool
	if err := env.allocator.AddPool("production-pool", []string{"192.168.1.0/24"}, nil); err != nil {
		t.Fatalf("failed to add production pool: %v", err)
	}

	svc := createLoadBalancerService("custom-pool-svc",
		map[string]string{
			annotationVIPMode:     "L2ARP",
			annotationAddressPool: "production-pool",
		},
		[]corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
	)

	if err := env.client.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := env.reconcileService(ctx, svc.Name, svc.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	vip := &novaedgev1alpha1.ProxyVIP{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      vipNamePrefix + svc.Name,
		Namespace: svc.Namespace,
	}, vip); err != nil {
		t.Fatalf("failed to get ProxyVIP: %v", err)
	}

	// Address should come from the production pool range
	if vip.Spec.Address == "" {
		t.Error("expected allocated address from production pool")
	}
	if !contains(vip.Spec.Address, "192.168.1.") {
		t.Errorf("expected address from 192.168.1.0/24, got %s", vip.Spec.Address)
	}
	if vip.Spec.PoolRef == nil || vip.Spec.PoolRef.Name != "production-pool" {
		t.Errorf("expected pool ref to 'production-pool', got %v", vip.Spec.PoolRef)
	}
}

func TestInvalidBGPConfigAnnotation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupServiceTestEnv(t)

	svc := createLoadBalancerService("bad-bgp-svc",
		map[string]string{
			annotationVIPMode:   "BGP",
			annotationBGPConfig: "not-valid-json{",
		},
		[]corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
	)

	if err := env.client.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	// Should not return error (invalid annotation is not retryable)
	if err := env.reconcileService(ctx, svc.Name, svc.Namespace); err != nil {
		t.Fatalf("expected no error for invalid BGP JSON, got: %v", err)
	}

	// Check that a warning event was recorded
	select {
	case event := <-env.recorder.Events:
		if !contains(event, "InvalidAnnotation") {
			t.Errorf("expected InvalidAnnotation event, got: %s", event)
		}
	default:
		t.Error("expected a warning event for invalid BGP config")
	}
}

func TestIdempotentReconciliation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupServiceTestEnv(t)

	svc := createLoadBalancerService("idempotent-svc",
		map[string]string{
			annotationVIPMode: "L2ARP",
		},
		[]corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
	)

	if err := env.client.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	// Reconcile twice
	if err := env.reconcileService(ctx, svc.Name, svc.Namespace); err != nil {
		t.Fatalf("first reconciliation failed: %v", err)
	}

	// Get the VIP address from first reconciliation
	vip1 := &novaedgev1alpha1.ProxyVIP{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      vipNamePrefix + svc.Name,
		Namespace: svc.Namespace,
	}, vip1); err != nil {
		t.Fatalf("failed to get ProxyVIP: %v", err)
	}
	addr1 := vip1.Spec.Address

	if err := env.reconcileService(ctx, svc.Name, svc.Namespace); err != nil {
		t.Fatalf("second reconciliation failed: %v", err)
	}

	// Verify same address is used (IPAM returns existing allocation)
	vip2 := &novaedgev1alpha1.ProxyVIP{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      vipNamePrefix + svc.Name,
		Namespace: svc.Namespace,
	}, vip2); err != nil {
		t.Fatalf("failed to get ProxyVIP after second reconciliation: %v", err)
	}

	if vip2.Spec.Address != addr1 {
		t.Errorf("expected same address after second reconciliation, got %s then %s", addr1, vip2.Spec.Address)
	}

	// Verify only one allocation exists
	allocated, _, err := env.allocator.GetPoolStats(defaultPoolName)
	if err != nil {
		t.Fatalf("failed to get pool stats: %v", err)
	}
	if allocated != 1 {
		t.Errorf("expected exactly 1 allocation after idempotent reconciliation, got %d", allocated)
	}
}

func TestDisabledServiceLBSkipsReconciliation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupServiceTestEnv(t)
	env.serviceReconciler.EnableServiceLB = false

	svc := createLoadBalancerService("disabled-svc",
		map[string]string{
			annotationVIPMode: "L2ARP",
		},
		[]corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
	)

	if err := env.client.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := env.reconcileService(ctx, svc.Name, svc.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	// No ProxyVIP should be created when ServiceLB is disabled
	vip := &novaedgev1alpha1.ProxyVIP{}
	err := env.client.Get(ctx, types.NamespacedName{
		Name:      vipNamePrefix + svc.Name,
		Namespace: svc.Namespace,
	}, vip)
	if err == nil {
		t.Error("expected no ProxyVIP when ServiceLB is disabled")
	}
}

func TestStripCIDR(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"10.0.0.1/32", "10.0.0.1"},
		{"192.168.1.100/24", "192.168.1.100"},
		{"2001:db8::1/128", "2001:db8::1"},
		{"10.0.0.1", "10.0.0.1"},
		{"", ""},
	}

	for _, tt := range tests {
		result := stripCIDR(tt.input)
		if result != tt.expected {
			t.Errorf("stripCIDR(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestParseVIPMode(t *testing.T) {
	tests := []struct {
		input       string
		expected    novaedgev1alpha1.VIPMode
		expectError bool
	}{
		{"L2ARP", novaedgev1alpha1.VIPModeL2ARP, false},
		{"BGP", novaedgev1alpha1.VIPModeBGP, false},
		{"OSPF", novaedgev1alpha1.VIPModeOSPF, false},
		{"invalid", "", true},
		{"l2arp", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		result, err := parseVIPMode(tt.input)
		if tt.expectError {
			if err == nil {
				t.Errorf("parseVIPMode(%q): expected error, got nil", tt.input)
			}
		} else {
			if err != nil {
				t.Errorf("parseVIPMode(%q): unexpected error: %v", tt.input, err)
			}
			if result != tt.expected {
				t.Errorf("parseVIPMode(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		}
	}
}

func TestParseAddressFamily(t *testing.T) {
	tests := []struct {
		input       string
		expected    novaedgev1alpha1.AddressFamily
		expectError bool
	}{
		{"ipv4", novaedgev1alpha1.AddressFamilyIPv4, false},
		{"ipv6", novaedgev1alpha1.AddressFamilyIPv6, false},
		{"dual", novaedgev1alpha1.AddressFamilyDual, false},
		{"invalid", "", true},
		{"IPv4", "", true},
	}

	for _, tt := range tests {
		result, err := parseAddressFamily(tt.input)
		if tt.expectError {
			if err == nil {
				t.Errorf("parseAddressFamily(%q): expected error, got nil", tt.input)
			}
		} else {
			if err != nil {
				t.Errorf("parseAddressFamily(%q): unexpected error: %v", tt.input, err)
			}
			if result != tt.expected {
				t.Errorf("parseAddressFamily(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		}
	}
}

func TestAllVIPModes(t *testing.T) {
	modes := []struct {
		mode     string
		expected novaedgev1alpha1.VIPMode
	}{
		{"L2ARP", novaedgev1alpha1.VIPModeL2ARP},
		{"BGP", novaedgev1alpha1.VIPModeBGP},
		{"OSPF", novaedgev1alpha1.VIPModeOSPF},
	}

	for _, m := range modes {
		t.Run(m.mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			env := setupServiceTestEnv(t)

			svc := createLoadBalancerService("mode-"+m.mode+"-svc",
				map[string]string{
					annotationVIPMode: m.mode,
				},
				[]corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
			)

			if err := env.client.Create(ctx, svc); err != nil {
				t.Fatalf("failed to create Service: %v", err)
			}

			if err := env.reconcileService(ctx, svc.Name, svc.Namespace); err != nil {
				t.Fatalf("reconciliation failed: %v", err)
			}

			vip := &novaedgev1alpha1.ProxyVIP{}
			if err := env.client.Get(ctx, types.NamespacedName{
				Name:      vipNamePrefix + svc.Name,
				Namespace: svc.Namespace,
			}, vip); err != nil {
				t.Fatalf("failed to get ProxyVIP: %v", err)
			}

			if vip.Spec.Mode != m.expected {
				t.Errorf("expected mode %s, got %s", m.expected, vip.Spec.Mode)
			}
		})
	}
}
