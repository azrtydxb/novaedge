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

package mode

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/azrtydxb/novaedge/cmd/novactl/pkg/webui/models"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	errKubernetesConfigIsRequired = errors.New("kubernetes config is required")
	errBackendIsReadOnly          = errors.New("backend is read-only")
	errConfigIsNil                = errors.New("config is nil")
	errBackend                    = errors.New("backend[")
	errRoute                      = errors.New("route[")
)

// KubernetesBackend implements Backend for Kubernetes CRD operations
type KubernetesBackend struct {
	dynamic   dynamic.Interface
	clientset kubernetes.Interface
	readOnly  bool
}

// namespaceAll is the special namespace value meaning all namespaces.
const (
	namespaceAll     = "all"
	defaultNamespace = "default"
)

// GVRs for NovaEdge CRDs
var (
	gvrGateway = schema.GroupVersionResource{
		Group:    "novaedge.io",
		Version:  "v1alpha1",
		Resource: "proxygateways",
	}
	gvrRoute = schema.GroupVersionResource{
		Group:    "novaedge.io",
		Version:  "v1alpha1",
		Resource: "proxyroutes",
	}
	gvrBackend = schema.GroupVersionResource{
		Group:    "novaedge.io",
		Version:  "v1alpha1",
		Resource: "proxybackends",
	}
	gvrVIP = schema.GroupVersionResource{
		Group:    "novaedge.io",
		Version:  "v1alpha1",
		Resource: "proxyvips",
	}
	gvrPolicy = schema.GroupVersionResource{
		Group:    "novaedge.io",
		Version:  "v1alpha1",
		Resource: "proxypolicies",
	}
	gvrCertificate = schema.GroupVersionResource{
		Group:    "novaedge.io",
		Version:  "v1alpha1",
		Resource: "proxycertificates",
	}
	gvrIPPool = schema.GroupVersionResource{
		Group:    "novaedge.io",
		Version:  "v1alpha1",
		Resource: "proxyippools",
	}
	gvrCluster = schema.GroupVersionResource{
		Group:    "novaedge.io",
		Version:  "v1alpha1",
		Resource: "novaedgeclusters",
	}
	gvrFederation = schema.GroupVersionResource{
		Group:    "novaedge.io",
		Version:  "v1alpha1",
		Resource: "novaedgefederations",
	}
	gvrRemoteCluster = schema.GroupVersionResource{
		Group:    "novaedge.io",
		Version:  "v1alpha1",
		Resource: "novaedgeremoteclusters",
	}
)

// NewKubernetesBackend creates a new Kubernetes backend
func NewKubernetesBackend(config *rest.Config, readOnly bool) (*KubernetesBackend, error) {
	if config == nil {
		return nil, errKubernetesConfigIsRequired
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	return &KubernetesBackend{
		dynamic:   dynamicClient,
		clientset: clientset,
		readOnly:  readOnly,
	}, nil
}

// Mode returns the backend mode
func (k *KubernetesBackend) Mode() Mode {
	return ModeKubernetes
}

// ReadOnly returns whether the backend is read-only
func (k *KubernetesBackend) ReadOnly() bool {
	return k.readOnly
}

// ListGateways returns all gateways
func (k *KubernetesBackend) ListGateways(ctx context.Context, namespace string) ([]models.Gateway, error) {
	var list *unstructured.UnstructuredList
	var err error

	if namespace == "" || namespace == namespaceAll {
		list, err = k.dynamic.Resource(gvrGateway).List(ctx, metav1.ListOptions{})
	} else {
		list, err = k.dynamic.Resource(gvrGateway).Namespace(namespace).List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list gateways: %w", err)
	}

	gateways := make([]models.Gateway, 0, len(list.Items))
	for _, item := range list.Items {
		gw, err := k.unstructuredToGateway(&item)
		if err != nil {
			continue // Skip invalid items
		}
		gateways = append(gateways, *gw)
	}

	return gateways, nil
}

// GetGateway returns a specific gateway
func (k *KubernetesBackend) GetGateway(ctx context.Context, namespace, name string) (*models.Gateway, error) {
	obj, err := k.dynamic.Resource(gvrGateway).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("gateway not found: %w", err)
	}
	return k.unstructuredToGateway(obj)
}

// CreateGateway creates a new gateway
func (k *KubernetesBackend) CreateGateway(ctx context.Context, gateway *models.Gateway) (*models.Gateway, error) {
	if k.readOnly {
		return nil, errBackendIsReadOnly
	}

	obj := k.gatewayToUnstructured(gateway)
	namespace := gateway.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	result, err := k.dynamic.Resource(gvrGateway).Namespace(namespace).Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create gateway: %w", err)
	}

	return k.unstructuredToGateway(result)
}

// UpdateGateway updates an existing gateway
func (k *KubernetesBackend) UpdateGateway(ctx context.Context, gateway *models.Gateway) (*models.Gateway, error) {
	if k.readOnly {
		return nil, errBackendIsReadOnly
	}

	obj := k.gatewayToUnstructured(gateway)
	namespace := gateway.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	result, err := k.dynamic.Resource(gvrGateway).Namespace(namespace).Update(ctx, obj, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to update gateway: %w", err)
	}

	return k.unstructuredToGateway(result)
}

// DeleteGateway deletes a gateway
func (k *KubernetesBackend) DeleteGateway(ctx context.Context, namespace, name string) error {
	if k.readOnly {
		return errBackendIsReadOnly
	}

	return k.dynamic.Resource(gvrGateway).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// ListRoutes returns all routes
func (k *KubernetesBackend) ListRoutes(ctx context.Context, namespace string) ([]models.Route, error) {
	var list *unstructured.UnstructuredList
	var err error

	if namespace == "" || namespace == namespaceAll {
		list, err = k.dynamic.Resource(gvrRoute).List(ctx, metav1.ListOptions{})
	} else {
		list, err = k.dynamic.Resource(gvrRoute).Namespace(namespace).List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list routes: %w", err)
	}

	routes := make([]models.Route, 0, len(list.Items))
	for _, item := range list.Items {
		rt, err := k.unstructuredToRoute(&item)
		if err != nil {
			continue
		}
		routes = append(routes, *rt)
	}

	return routes, nil
}

// GetRoute returns a specific route
func (k *KubernetesBackend) GetRoute(ctx context.Context, namespace, name string) (*models.Route, error) {
	obj, err := k.dynamic.Resource(gvrRoute).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("route not found: %w", err)
	}
	return k.unstructuredToRoute(obj)
}

// CreateRoute creates a new route
func (k *KubernetesBackend) CreateRoute(ctx context.Context, route *models.Route) (*models.Route, error) {
	if k.readOnly {
		return nil, errBackendIsReadOnly
	}

	obj := k.routeToUnstructured(route)
	namespace := route.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	result, err := k.dynamic.Resource(gvrRoute).Namespace(namespace).Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create route: %w", err)
	}

	return k.unstructuredToRoute(result)
}

// UpdateRoute updates an existing route
func (k *KubernetesBackend) UpdateRoute(ctx context.Context, route *models.Route) (*models.Route, error) {
	if k.readOnly {
		return nil, errBackendIsReadOnly
	}

	obj := k.routeToUnstructured(route)
	namespace := route.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	result, err := k.dynamic.Resource(gvrRoute).Namespace(namespace).Update(ctx, obj, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to update route: %w", err)
	}

	return k.unstructuredToRoute(result)
}

// DeleteRoute deletes a route
func (k *KubernetesBackend) DeleteRoute(ctx context.Context, namespace, name string) error {
	if k.readOnly {
		return errBackendIsReadOnly
	}

	return k.dynamic.Resource(gvrRoute).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// ListBackends returns all backends
func (k *KubernetesBackend) ListBackends(ctx context.Context, namespace string) ([]models.Backend, error) {
	var list *unstructured.UnstructuredList
	var err error

	if namespace == "" || namespace == namespaceAll {
		list, err = k.dynamic.Resource(gvrBackend).List(ctx, metav1.ListOptions{})
	} else {
		list, err = k.dynamic.Resource(gvrBackend).Namespace(namespace).List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list backends: %w", err)
	}

	backends := make([]models.Backend, 0, len(list.Items))
	for _, item := range list.Items {
		be, err := k.unstructuredToBackend(&item)
		if err != nil {
			continue
		}
		backends = append(backends, *be)
	}

	return backends, nil
}

// GetBackend returns a specific backend
func (k *KubernetesBackend) GetBackend(ctx context.Context, namespace, name string) (*models.Backend, error) {
	obj, err := k.dynamic.Resource(gvrBackend).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("backend not found: %w", err)
	}
	return k.unstructuredToBackend(obj)
}

// CreateBackend creates a new backend
func (k *KubernetesBackend) CreateBackend(ctx context.Context, backend *models.Backend) (*models.Backend, error) {
	if k.readOnly {
		return nil, errBackendIsReadOnly
	}

	obj := k.backendToUnstructured(backend)
	namespace := backend.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	result, err := k.dynamic.Resource(gvrBackend).Namespace(namespace).Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create backend: %w", err)
	}

	return k.unstructuredToBackend(result)
}

// UpdateBackend updates an existing backend
func (k *KubernetesBackend) UpdateBackend(ctx context.Context, backend *models.Backend) (*models.Backend, error) {
	if k.readOnly {
		return nil, errBackendIsReadOnly
	}

	obj := k.backendToUnstructured(backend)
	namespace := backend.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	result, err := k.dynamic.Resource(gvrBackend).Namespace(namespace).Update(ctx, obj, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to update backend: %w", err)
	}

	return k.unstructuredToBackend(result)
}

// DeleteBackend deletes a backend
func (k *KubernetesBackend) DeleteBackend(ctx context.Context, namespace, name string) error {
	if k.readOnly {
		return errBackendIsReadOnly
	}

	return k.dynamic.Resource(gvrBackend).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// ListVIPs returns all VIPs
func (k *KubernetesBackend) ListVIPs(ctx context.Context, namespace string) ([]models.VIP, error) {
	var list *unstructured.UnstructuredList
	var err error

	if namespace == "" || namespace == namespaceAll {
		list, err = k.dynamic.Resource(gvrVIP).List(ctx, metav1.ListOptions{})
	} else {
		list, err = k.dynamic.Resource(gvrVIP).Namespace(namespace).List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list VIPs: %w", err)
	}

	vips := make([]models.VIP, 0, len(list.Items))
	for _, item := range list.Items {
		vip, err := k.unstructuredToVIP(&item)
		if err != nil {
			continue
		}
		vips = append(vips, *vip)
	}

	return vips, nil
}

// GetVIP returns a specific VIP
func (k *KubernetesBackend) GetVIP(ctx context.Context, namespace, name string) (*models.VIP, error) {
	obj, err := k.dynamic.Resource(gvrVIP).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("VIP not found: %w", err)
	}
	return k.unstructuredToVIP(obj)
}

// CreateVIP creates a new VIP
func (k *KubernetesBackend) CreateVIP(ctx context.Context, vip *models.VIP) (*models.VIP, error) {
	if k.readOnly {
		return nil, errBackendIsReadOnly
	}

	obj := k.vipToUnstructured(vip)
	namespace := vip.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	result, err := k.dynamic.Resource(gvrVIP).Namespace(namespace).Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create VIP: %w", err)
	}

	return k.unstructuredToVIP(result)
}

// UpdateVIP updates an existing VIP
func (k *KubernetesBackend) UpdateVIP(ctx context.Context, vip *models.VIP) (*models.VIP, error) {
	if k.readOnly {
		return nil, errBackendIsReadOnly
	}

	obj := k.vipToUnstructured(vip)
	namespace := vip.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	result, err := k.dynamic.Resource(gvrVIP).Namespace(namespace).Update(ctx, obj, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to update VIP: %w", err)
	}

	return k.unstructuredToVIP(result)
}

// DeleteVIP deletes a VIP
func (k *KubernetesBackend) DeleteVIP(ctx context.Context, namespace, name string) error {
	if k.readOnly {
		return errBackendIsReadOnly
	}

	return k.dynamic.Resource(gvrVIP).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// ListPolicies returns all policies
func (k *KubernetesBackend) ListPolicies(ctx context.Context, namespace string) ([]models.Policy, error) {
	var list *unstructured.UnstructuredList
	var err error

	if namespace == "" || namespace == namespaceAll {
		list, err = k.dynamic.Resource(gvrPolicy).List(ctx, metav1.ListOptions{})
	} else {
		list, err = k.dynamic.Resource(gvrPolicy).Namespace(namespace).List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list policies: %w", err)
	}

	policies := make([]models.Policy, 0, len(list.Items))
	for _, item := range list.Items {
		pol, err := k.unstructuredToPolicy(&item)
		if err != nil {
			continue
		}
		policies = append(policies, *pol)
	}

	return policies, nil
}

// GetPolicy returns a specific policy
func (k *KubernetesBackend) GetPolicy(ctx context.Context, namespace, name string) (*models.Policy, error) {
	obj, err := k.dynamic.Resource(gvrPolicy).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("policy not found: %w", err)
	}
	return k.unstructuredToPolicy(obj)
}

// CreatePolicy creates a new policy
func (k *KubernetesBackend) CreatePolicy(ctx context.Context, policy *models.Policy) (*models.Policy, error) {
	if k.readOnly {
		return nil, errBackendIsReadOnly
	}

	obj := k.policyToUnstructured(policy)
	namespace := policy.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	result, err := k.dynamic.Resource(gvrPolicy).Namespace(namespace).Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create policy: %w", err)
	}

	return k.unstructuredToPolicy(result)
}

// UpdatePolicy updates an existing policy
func (k *KubernetesBackend) UpdatePolicy(ctx context.Context, policy *models.Policy) (*models.Policy, error) {
	if k.readOnly {
		return nil, errBackendIsReadOnly
	}

	obj := k.policyToUnstructured(policy)
	namespace := policy.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	result, err := k.dynamic.Resource(gvrPolicy).Namespace(namespace).Update(ctx, obj, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to update policy: %w", err)
	}

	return k.unstructuredToPolicy(result)
}

// DeletePolicy deletes a policy
func (k *KubernetesBackend) DeletePolicy(ctx context.Context, namespace, name string) error {
	if k.readOnly {
		return errBackendIsReadOnly
	}

	return k.dynamic.Resource(gvrPolicy).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// ListCertificates returns all certificates
func (k *KubernetesBackend) ListCertificates(ctx context.Context, namespace string) ([]models.Certificate, error) {
	var list *unstructured.UnstructuredList
	var err error

	if namespace == "" || namespace == namespaceAll {
		list, err = k.dynamic.Resource(gvrCertificate).List(ctx, metav1.ListOptions{})
	} else {
		list, err = k.dynamic.Resource(gvrCertificate).Namespace(namespace).List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list certificates: %w", err)
	}

	certs := make([]models.Certificate, 0, len(list.Items))
	for _, item := range list.Items {
		cert, err := k.unstructuredToCertificate(&item)
		if err != nil {
			continue
		}
		certs = append(certs, *cert)
	}

	return certs, nil
}

// GetCertificate returns a specific certificate
func (k *KubernetesBackend) GetCertificate(ctx context.Context, namespace, name string) (*models.Certificate, error) {
	obj, err := k.dynamic.Resource(gvrCertificate).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("certificate not found: %w", err)
	}
	return k.unstructuredToCertificate(obj)
}

// CreateCertificate creates a new certificate
func (k *KubernetesBackend) CreateCertificate(ctx context.Context, cert *models.Certificate) (*models.Certificate, error) {
	if k.readOnly {
		return nil, errBackendIsReadOnly
	}

	obj := k.certificateToUnstructured(cert)
	namespace := cert.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	result, err := k.dynamic.Resource(gvrCertificate).Namespace(namespace).Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	return k.unstructuredToCertificate(result)
}

// UpdateCertificate updates an existing certificate
func (k *KubernetesBackend) UpdateCertificate(ctx context.Context, cert *models.Certificate) (*models.Certificate, error) {
	if k.readOnly {
		return nil, errBackendIsReadOnly
	}

	obj := k.certificateToUnstructured(cert)
	namespace := cert.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	result, err := k.dynamic.Resource(gvrCertificate).Namespace(namespace).Update(ctx, obj, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to update certificate: %w", err)
	}

	return k.unstructuredToCertificate(result)
}

// DeleteCertificate deletes a certificate
func (k *KubernetesBackend) DeleteCertificate(ctx context.Context, namespace, name string) error {
	if k.readOnly {
		return errBackendIsReadOnly
	}

	return k.dynamic.Resource(gvrCertificate).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// ListIPPools returns all IP pools (cluster-scoped)
func (k *KubernetesBackend) ListIPPools(ctx context.Context) ([]models.IPPool, error) {
	list, err := k.dynamic.Resource(gvrIPPool).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list IP pools: %w", err)
	}

	pools := make([]models.IPPool, 0, len(list.Items))
	for _, item := range list.Items {
		pool, err := k.unstructuredToIPPool(&item)
		if err != nil {
			continue
		}
		pools = append(pools, *pool)
	}

	return pools, nil
}

// GetIPPool returns a specific IP pool (cluster-scoped)
func (k *KubernetesBackend) GetIPPool(ctx context.Context, name string) (*models.IPPool, error) {
	obj, err := k.dynamic.Resource(gvrIPPool).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("IP pool not found: %w", err)
	}
	return k.unstructuredToIPPool(obj)
}

// CreateIPPool creates a new IP pool (cluster-scoped)
func (k *KubernetesBackend) CreateIPPool(ctx context.Context, pool *models.IPPool) (*models.IPPool, error) {
	if k.readOnly {
		return nil, errBackendIsReadOnly
	}

	obj := k.ipPoolToUnstructured(pool)

	result, err := k.dynamic.Resource(gvrIPPool).Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create IP pool: %w", err)
	}

	return k.unstructuredToIPPool(result)
}

// UpdateIPPool updates an existing IP pool (cluster-scoped)
func (k *KubernetesBackend) UpdateIPPool(ctx context.Context, pool *models.IPPool) (*models.IPPool, error) {
	if k.readOnly {
		return nil, errBackendIsReadOnly
	}

	obj := k.ipPoolToUnstructured(pool)

	result, err := k.dynamic.Resource(gvrIPPool).Update(ctx, obj, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to update IP pool: %w", err)
	}

	return k.unstructuredToIPPool(result)
}

// DeleteIPPool deletes an IP pool (cluster-scoped)
func (k *KubernetesBackend) DeleteIPPool(ctx context.Context, name string) error {
	if k.readOnly {
		return errBackendIsReadOnly
	}

	return k.dynamic.Resource(gvrIPPool).Delete(ctx, name, metav1.DeleteOptions{})
}

// ListNovaEdgeClusters returns all NovaEdge clusters
func (k *KubernetesBackend) ListNovaEdgeClusters(ctx context.Context, namespace string) ([]models.NovaEdgeClusterModel, error) {
	items, err := k.genericList(ctx, genericResourceConfig{"NovaEdgeCluster", gvrCluster}, namespace)
	if err != nil {
		return nil, err
	}
	clusters := make([]models.NovaEdgeClusterModel, 0, len(items))
	for _, g := range items {
		clusters = append(clusters, *genericToClusterModel(g))
	}
	return clusters, nil
}

// GetNovaEdgeCluster returns a specific NovaEdge cluster
func (k *KubernetesBackend) GetNovaEdgeCluster(ctx context.Context, namespace, name string) (*models.NovaEdgeClusterModel, error) {
	obj, err := k.dynamic.Resource(gvrCluster).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("NovaEdge cluster not found: %w", err)
	}
	g := unstructuredToGenericModel(obj)
	return &models.NovaEdgeClusterModel{
		Name:            g.name,
		Namespace:       g.namespace,
		Labels:          g.labels,
		Annotations:     g.annotations,
		Spec:            g.spec,
		Status:          g.status,
		ResourceVersion: g.resourceVersion,
	}, nil
}

// CreateNovaEdgeCluster creates a new NovaEdge cluster
func (k *KubernetesBackend) CreateNovaEdgeCluster(ctx context.Context, cluster *models.NovaEdgeClusterModel) (*models.NovaEdgeClusterModel, error) {
	g, err := k.genericCreate(ctx, genericResourceConfig{"NovaEdgeCluster", gvrCluster}, clusterModelToGeneric(cluster))
	if err != nil {
		return nil, err
	}
	return genericToClusterModel(g), nil
}

// UpdateNovaEdgeCluster updates an existing NovaEdge cluster
func (k *KubernetesBackend) UpdateNovaEdgeCluster(ctx context.Context, cluster *models.NovaEdgeClusterModel) (*models.NovaEdgeClusterModel, error) {
	g, err := k.genericUpdate(ctx, genericResourceConfig{"NovaEdgeCluster", gvrCluster}, clusterModelToGeneric(cluster))
	if err != nil {
		return nil, err
	}
	return genericToClusterModel(g), nil
}

// DeleteNovaEdgeCluster deletes a NovaEdge cluster
func (k *KubernetesBackend) DeleteNovaEdgeCluster(ctx context.Context, namespace, name string) error {
	if k.readOnly {
		return errBackendIsReadOnly
	}

	return k.dynamic.Resource(gvrCluster).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// ListFederations returns all federations
func (k *KubernetesBackend) ListFederations(ctx context.Context, namespace string) ([]models.FederationModel, error) {
	items, err := k.genericList(ctx, genericResourceConfig{"NovaEdgeFederation", gvrFederation}, namespace)
	if err != nil {
		return nil, err
	}
	federations := make([]models.FederationModel, 0, len(items))
	for _, g := range items {
		federations = append(federations, *genericToFederationModel(g))
	}
	return federations, nil
}

// GetFederation returns a specific federation
func (k *KubernetesBackend) GetFederation(ctx context.Context, namespace, name string) (*models.FederationModel, error) {
	obj, err := k.dynamic.Resource(gvrFederation).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("federation not found: %w", err)
	}
	g := unstructuredToGenericModel(obj)
	return &models.FederationModel{
		Name:            g.name,
		Namespace:       g.namespace,
		Labels:          g.labels,
		Annotations:     g.annotations,
		Spec:            g.spec,
		Status:          g.status,
		ResourceVersion: g.resourceVersion,
	}, nil
}

// CreateFederation creates a new federation
func (k *KubernetesBackend) CreateFederation(ctx context.Context, federation *models.FederationModel) (*models.FederationModel, error) {
	g, err := k.genericCreate(ctx, genericResourceConfig{"NovaEdgeFederation", gvrFederation}, federationModelToGeneric(federation))
	if err != nil {
		return nil, err
	}
	return genericToFederationModel(g), nil
}

// UpdateFederation updates an existing federation
func (k *KubernetesBackend) UpdateFederation(ctx context.Context, federation *models.FederationModel) (*models.FederationModel, error) {
	g, err := k.genericUpdate(ctx, genericResourceConfig{"NovaEdgeFederation", gvrFederation}, federationModelToGeneric(federation))
	if err != nil {
		return nil, err
	}
	return genericToFederationModel(g), nil
}

// DeleteFederation deletes a federation
func (k *KubernetesBackend) DeleteFederation(ctx context.Context, namespace, name string) error {
	if k.readOnly {
		return errBackendIsReadOnly
	}

	return k.dynamic.Resource(gvrFederation).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// ListRemoteClusters returns all remote clusters
func (k *KubernetesBackend) ListRemoteClusters(ctx context.Context, namespace string) ([]models.RemoteClusterModel, error) {
	items, err := k.genericList(ctx, genericResourceConfig{"NovaEdgeRemoteCluster", gvrRemoteCluster}, namespace)
	if err != nil {
		return nil, err
	}
	remoteClusters := make([]models.RemoteClusterModel, 0, len(items))
	for _, g := range items {
		remoteClusters = append(remoteClusters, *genericToRemoteClusterModel(g))
	}
	return remoteClusters, nil
}

// GetRemoteCluster returns a specific remote cluster
func (k *KubernetesBackend) GetRemoteCluster(ctx context.Context, namespace, name string) (*models.RemoteClusterModel, error) {
	obj, err := k.dynamic.Resource(gvrRemoteCluster).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("remote cluster not found: %w", err)
	}
	g := unstructuredToGenericModel(obj)
	return &models.RemoteClusterModel{
		Name:            g.name,
		Namespace:       g.namespace,
		Labels:          g.labels,
		Annotations:     g.annotations,
		Spec:            g.spec,
		Status:          g.status,
		ResourceVersion: g.resourceVersion,
	}, nil
}

// CreateRemoteCluster creates a new remote cluster
func (k *KubernetesBackend) CreateRemoteCluster(ctx context.Context, rc *models.RemoteClusterModel) (*models.RemoteClusterModel, error) {
	g, err := k.genericCreate(ctx, genericResourceConfig{"NovaEdgeRemoteCluster", gvrRemoteCluster}, remoteClusterModelToGeneric(rc))
	if err != nil {
		return nil, err
	}
	return genericToRemoteClusterModel(g), nil
}

// UpdateRemoteCluster updates an existing remote cluster
func (k *KubernetesBackend) UpdateRemoteCluster(ctx context.Context, rc *models.RemoteClusterModel) (*models.RemoteClusterModel, error) {
	g, err := k.genericUpdate(ctx, genericResourceConfig{"NovaEdgeRemoteCluster", gvrRemoteCluster}, remoteClusterModelToGeneric(rc))
	if err != nil {
		return nil, err
	}
	return genericToRemoteClusterModel(g), nil
}

// DeleteRemoteCluster deletes a remote cluster
func (k *KubernetesBackend) DeleteRemoteCluster(ctx context.Context, namespace, name string) error {
	if k.readOnly {
		return errBackendIsReadOnly
	}

	return k.dynamic.Resource(gvrRemoteCluster).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// ListNamespaces returns available namespaces
func (k *KubernetesBackend) ListNamespaces(ctx context.Context) ([]string, error) {
	list, err := k.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	namespaces := make([]string, 0, len(list.Items))
	for _, ns := range list.Items {
		namespaces = append(namespaces, ns.Name)
	}

	return namespaces, nil
}

// ValidateConfig validates the configuration
func (k *KubernetesBackend) ValidateConfig(_ context.Context, config *models.Config) error {
	// Kubernetes uses CRD validation, so we just do basic validation here
	return validateConfig(config)
}

// ExportConfig exports the full configuration as YAML
func (k *KubernetesBackend) ExportConfig(ctx context.Context, namespace string) ([]byte, error) {
	config := &models.Config{}

	gateways, err := k.ListGateways(ctx, namespace)
	if err == nil {
		config.Gateways = gateways
	}

	routes, err := k.ListRoutes(ctx, namespace)
	if err == nil {
		config.Routes = routes
	}

	backends, err := k.ListBackends(ctx, namespace)
	if err == nil {
		config.Backends = backends
	}

	vips, err := k.ListVIPs(ctx, namespace)
	if err == nil {
		config.VIPs = vips
	}

	policies, err := k.ListPolicies(ctx, namespace)
	if err == nil {
		config.Policies = policies
	}

	return yaml.Marshal(config)
}

// ImportConfig imports configuration from YAML
func (k *KubernetesBackend) ImportConfig(ctx context.Context, data []byte, dryRun bool) (*models.ImportResult, error) {
	if k.readOnly {
		return nil, errBackendIsReadOnly
	}

	var config models.Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	result := &models.ImportResult{DryRun: dryRun}

	// importResource is a helper that performs create-or-update for a single resource.
	importResource := func(kind, name, ns string, get func() error, create, update func() error) {
		ref := models.ResourceRef{Kind: kind, Name: name, Namespace: ns}
		if dryRun {
			result.Created = append(result.Created, ref)
			return
		}
		if err := get(); err != nil {
			if createErr := create(); createErr != nil {
				result.Errors = append(result.Errors, models.ImportError{Resource: ref, Error: createErr.Error()})
			} else {
				result.Created = append(result.Created, ref)
			}
		} else {
			if updateErr := update(); updateErr != nil {
				result.Errors = append(result.Errors, models.ImportError{Resource: ref, Error: updateErr.Error()})
			} else {
				result.Updated = append(result.Updated, ref)
			}
		}
	}

	for _, gw := range config.Gateways {
		gw := gw
		importResource("Gateway", gw.Name, gw.Namespace,
			func() error { _, e := k.GetGateway(ctx, gw.Namespace, gw.Name); return e },
			func() error { _, e := k.CreateGateway(ctx, &gw); return e },
			func() error { _, e := k.UpdateGateway(ctx, &gw); return e })
	}
	for _, rt := range config.Routes {
		rt := rt
		importResource("Route", rt.Name, rt.Namespace,
			func() error { _, e := k.GetRoute(ctx, rt.Namespace, rt.Name); return e },
			func() error { _, e := k.CreateRoute(ctx, &rt); return e },
			func() error { _, e := k.UpdateRoute(ctx, &rt); return e })
	}
	for _, be := range config.Backends {
		be := be
		importResource("Backend", be.Name, be.Namespace,
			func() error { _, e := k.GetBackend(ctx, be.Namespace, be.Name); return e },
			func() error { _, e := k.CreateBackend(ctx, &be); return e },
			func() error { _, e := k.UpdateBackend(ctx, &be); return e })
	}
	for _, vip := range config.VIPs {
		vip := vip
		importResource("VIP", vip.Name, vip.Namespace,
			func() error { _, e := k.GetVIP(ctx, vip.Namespace, vip.Name); return e },
			func() error { _, e := k.CreateVIP(ctx, &vip); return e },
			func() error { _, e := k.UpdateVIP(ctx, &vip); return e })
	}
	for _, pol := range config.Policies {
		pol := pol
		importResource("Policy", pol.Name, pol.Namespace,
			func() error { _, e := k.GetPolicy(ctx, pol.Namespace, pol.Name); return e },
			func() error { _, e := k.CreatePolicy(ctx, &pol); return e },
			func() error { _, e := k.UpdatePolicy(ctx, &pol); return e })
	}

	return result, nil
}

// Conversion helpers

func (k *KubernetesBackend) unstructuredToGateway(obj *unstructured.Unstructured) (*models.Gateway, error) {
	gw := &models.Gateway{
		Name:            obj.GetName(),
		Namespace:       obj.GetNamespace(),
		ResourceVersion: obj.GetResourceVersion(),
	}

	spec, found, err := unstructured.NestedMap(obj.Object, "spec")
	if err != nil {
		return nil, fmt.Errorf("failed to read spec: %w", err)
	}
	if !found {
		return gw, nil
	}

	// Parse listeners
	if listeners, found, _ := unstructured.NestedSlice(spec, "listeners"); found {
		for _, l := range listeners {
			if lm, ok := l.(map[string]interface{}); ok {
				listener := models.Listener{
					Name:     getStringField(lm, "name"),
					Port:     int(getInt64Field(lm, "port")),
					Protocol: getStringField(lm, "protocol"),
				}
				if hostnames, found, _ := unstructured.NestedStringSlice(lm, "hostnames"); found {
					listener.Hostnames = hostnames
				}
				gw.Listeners = append(gw.Listeners, listener)
			}
		}
	}

	return gw, nil
}

func (k *KubernetesBackend) gatewayToUnstructured(gw *models.Gateway) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "novaedge.io/v1alpha1",
			"kind":       "ProxyGateway",
			"metadata": map[string]interface{}{
				"name":      gw.Name,
				"namespace": gw.Namespace,
			},
			"spec": map[string]interface{}{},
		},
	}

	if gw.ResourceVersion != "" {
		obj.SetResourceVersion(gw.ResourceVersion)
	}

	spec, ok := obj.Object["spec"].(map[string]interface{})
	if !ok {
		return obj
	}

	// Convert listeners
	if len(gw.Listeners) > 0 {
		listeners := make([]interface{}, 0, len(gw.Listeners))
		for _, l := range gw.Listeners {
			listener := map[string]interface{}{
				"name":     l.Name,
				"port":     l.Port,
				"protocol": l.Protocol,
			}
			if len(l.Hostnames) > 0 {
				listener["hostnames"] = l.Hostnames
			}
			listeners = append(listeners, listener)
		}
		spec["listeners"] = listeners
	}

	return obj
}

func (k *KubernetesBackend) unstructuredToRoute(obj *unstructured.Unstructured) (*models.Route, error) {
	rt := &models.Route{
		Name:            obj.GetName(),
		Namespace:       obj.GetNamespace(),
		ResourceVersion: obj.GetResourceVersion(),
	}

	spec, found, err := unstructured.NestedMap(obj.Object, "spec")
	if err != nil {
		return nil, fmt.Errorf("failed to read spec: %w", err)
	}
	if !found {
		return rt, nil
	}

	// Parse hostnames
	if hostnames, found, _ := unstructured.NestedStringSlice(spec, "hostnames"); found {
		rt.Hostnames = hostnames
	}

	// Parse backend refs
	if backends, found, _ := unstructured.NestedSlice(spec, "backendRefs"); found {
		for _, b := range backends {
			if bm, ok := b.(map[string]interface{}); ok {
				ref := models.BackendRef{
					Name:   getStringField(bm, "name"),
					Weight: int(getInt64Field(bm, "weight")),
				}
				rt.BackendRefs = append(rt.BackendRefs, ref)
			}
		}
	}

	return rt, nil
}

func (k *KubernetesBackend) routeToUnstructured(rt *models.Route) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "novaedge.io/v1alpha1",
			"kind":       "ProxyRoute",
			"metadata": map[string]interface{}{
				"name":      rt.Name,
				"namespace": rt.Namespace,
			},
			"spec": map[string]interface{}{},
		},
	}

	if rt.ResourceVersion != "" {
		obj.SetResourceVersion(rt.ResourceVersion)
	}

	spec, ok := obj.Object["spec"].(map[string]interface{})
	if !ok {
		return obj
	}

	if len(rt.Hostnames) > 0 {
		spec["hostnames"] = rt.Hostnames
	}

	if len(rt.BackendRefs) > 0 {
		backends := make([]interface{}, 0, len(rt.BackendRefs))
		for _, b := range rt.BackendRefs {
			backend := map[string]interface{}{
				"name": b.Name,
			}
			if b.Weight > 0 {
				backend["weight"] = b.Weight
			}
			backends = append(backends, backend)
		}
		spec["backendRefs"] = backends
	}

	return obj
}

func (k *KubernetesBackend) unstructuredToBackend(obj *unstructured.Unstructured) (*models.Backend, error) {
	be := &models.Backend{
		Name:            obj.GetName(),
		Namespace:       obj.GetNamespace(),
		ResourceVersion: obj.GetResourceVersion(),
	}

	spec, found, err := unstructured.NestedMap(obj.Object, "spec")
	if err != nil {
		return nil, fmt.Errorf("failed to read spec: %w", err)
	}
	if !found {
		return be, nil
	}

	be.LBPolicy = getStringField(spec, "lbPolicy")

	// Parse endpoints
	if endpoints, found, _ := unstructured.NestedSlice(spec, "endpoints"); found {
		for _, e := range endpoints {
			if em, ok := e.(map[string]interface{}); ok {
				endpoint := models.Endpoint{
					Address: getStringField(em, "address"),
					Weight:  int(getInt64Field(em, "weight")),
				}
				be.Endpoints = append(be.Endpoints, endpoint)
			}
		}
	}

	return be, nil
}

func (k *KubernetesBackend) backendToUnstructured(be *models.Backend) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "novaedge.io/v1alpha1",
			"kind":       "ProxyBackend",
			"metadata": map[string]interface{}{
				"name":      be.Name,
				"namespace": be.Namespace,
			},
			"spec": map[string]interface{}{
				"lbPolicy": be.LBPolicy,
			},
		},
	}

	if be.ResourceVersion != "" {
		obj.SetResourceVersion(be.ResourceVersion)
	}

	spec, ok := obj.Object["spec"].(map[string]interface{})
	if !ok {
		return obj
	}

	if len(be.Endpoints) > 0 {
		endpoints := make([]interface{}, 0, len(be.Endpoints))
		for _, e := range be.Endpoints {
			endpoint := map[string]interface{}{
				"address": e.Address,
			}
			if e.Weight > 0 {
				endpoint["weight"] = e.Weight
			}
			endpoints = append(endpoints, endpoint)
		}
		spec["endpoints"] = endpoints
	}

	return obj
}

func (k *KubernetesBackend) unstructuredToVIP(obj *unstructured.Unstructured) (*models.VIP, error) {
	vip := &models.VIP{
		Name:            obj.GetName(),
		Namespace:       obj.GetNamespace(),
		ResourceVersion: obj.GetResourceVersion(),
	}

	spec, found, err := unstructured.NestedMap(obj.Object, "spec")
	if err != nil {
		return nil, fmt.Errorf("failed to read spec: %w", err)
	}
	if !found {
		return vip, nil
	}

	vip.Address = getStringField(spec, "address")
	vip.Mode = getStringField(spec, "mode")
	vip.Interface = getStringField(spec, "interface")

	return vip, nil
}

func (k *KubernetesBackend) vipToUnstructured(vip *models.VIP) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "novaedge.io/v1alpha1",
			"kind":       "ProxyVIP",
			"metadata": map[string]interface{}{
				"name":      vip.Name,
				"namespace": vip.Namespace,
			},
			"spec": map[string]interface{}{
				"address": vip.Address,
				"mode":    vip.Mode,
			},
		},
	}

	if vip.ResourceVersion != "" {
		obj.SetResourceVersion(vip.ResourceVersion)
	}

	spec, ok := obj.Object["spec"].(map[string]interface{})
	if !ok {
		return obj
	}

	if vip.Interface != "" {
		spec["interface"] = vip.Interface
	}

	return obj
}

func (k *KubernetesBackend) unstructuredToPolicy(obj *unstructured.Unstructured) (*models.Policy, error) {
	pol := &models.Policy{
		Name:            obj.GetName(),
		Namespace:       obj.GetNamespace(),
		ResourceVersion: obj.GetResourceVersion(),
	}

	spec, found, err := unstructured.NestedMap(obj.Object, "spec")
	if err != nil {
		return nil, fmt.Errorf("failed to read spec: %w", err)
	}
	if !found {
		return pol, nil
	}

	pol.Type = getStringField(spec, "type")

	return pol, nil
}

func (k *KubernetesBackend) policyToUnstructured(pol *models.Policy) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "novaedge.io/v1alpha1",
			"kind":       "ProxyPolicy",
			"metadata": map[string]interface{}{
				"name":      pol.Name,
				"namespace": pol.Namespace,
			},
			"spec": map[string]interface{}{
				"type": pol.Type,
			},
		},
	}

	if pol.ResourceVersion != "" {
		obj.SetResourceVersion(pol.ResourceVersion)
	}

	return obj
}

// Helper functions

func getStringField(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt64Field(m map[string]interface{}, key string) int64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case int64:
			return n
		case int:
			return int64(n)
		case float64:
			return int64(n)
		}
	}
	return 0
}

// Certificate conversion helpers

func (k *KubernetesBackend) unstructuredToCertificate(obj *unstructured.Unstructured) (*models.Certificate, error) {
	cert := &models.Certificate{
		Name:            obj.GetName(),
		Namespace:       obj.GetNamespace(),
		Labels:          obj.GetLabels(),
		Annotations:     obj.GetAnnotations(),
		ResourceVersion: obj.GetResourceVersion(),
	}

	spec, found, err := unstructured.NestedMap(obj.Object, "spec")
	if err != nil {
		return nil, fmt.Errorf("failed to read spec: %w", err)
	}
	if !found {
		return cert, nil
	}

	if domains, found, _ := unstructured.NestedStringSlice(spec, "domains"); found {
		cert.Spec.Domains = domains
	}
	cert.Spec.SecretName = getStringField(spec, "secretName")
	cert.Spec.KeyType = getStringField(spec, "keyType")
	cert.Spec.MustStaple = getBoolField(spec, "mustStaple")

	if issuer, found, _ := unstructured.NestedMap(spec, "issuer"); found {
		cert.Spec.Issuer.Type = getStringField(issuer, "type")
		if acme, found, _ := unstructured.NestedMap(issuer, "acme"); found {
			cert.Spec.ACME = &models.ACMEConfig{
				Server:        getStringField(acme, "server"),
				Email:         getStringField(acme, "email"),
				ChallengeType: getStringField(acme, "challengeType"),
				DNSProvider:   getStringField(acme, "dnsProvider"),
			}
		}
	}

	// Parse status
	status, found, _ := unstructured.NestedMap(obj.Object, "status")
	if found {
		cert.Status.State = getStringField(status, "state")
		cert.Status.SecretName = getStringField(status, "secretName")
		cert.Status.SerialNumber = getStringField(status, "serialNumber")
		cert.Status.Issuer = getStringField(status, "issuer")
		cert.Status.Message = getStringField(status, "message")
	}

	return cert, nil
}

func (k *KubernetesBackend) certificateToUnstructured(cert *models.Certificate) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "novaedge.io/v1alpha1",
			"kind":       "ProxyCertificate",
			"metadata": map[string]interface{}{
				"name":      cert.Name,
				"namespace": cert.Namespace,
			},
			"spec": map[string]interface{}{},
		},
	}

	if cert.ResourceVersion != "" {
		obj.SetResourceVersion(cert.ResourceVersion)
	}
	if len(cert.Labels) > 0 {
		obj.SetLabels(cert.Labels)
	}
	if len(cert.Annotations) > 0 {
		obj.SetAnnotations(cert.Annotations)
	}

	spec, ok := obj.Object["spec"].(map[string]interface{})
	if !ok {
		return obj
	}

	if len(cert.Spec.Domains) > 0 {
		domains := make([]interface{}, len(cert.Spec.Domains))
		for i, d := range cert.Spec.Domains {
			domains[i] = d
		}
		spec["domains"] = domains
	}

	issuer := map[string]interface{}{
		"type": cert.Spec.Issuer.Type,
	}
	if cert.Spec.ACME != nil {
		acme := map[string]interface{}{}
		if cert.Spec.ACME.Server != "" {
			acme["server"] = cert.Spec.ACME.Server
		}
		if cert.Spec.ACME.Email != "" {
			acme["email"] = cert.Spec.ACME.Email
		}
		if cert.Spec.ACME.ChallengeType != "" {
			acme["challengeType"] = cert.Spec.ACME.ChallengeType
		}
		if cert.Spec.ACME.DNSProvider != "" {
			acme["dnsProvider"] = cert.Spec.ACME.DNSProvider
		}
		issuer["acme"] = acme
	}
	spec["issuer"] = issuer

	if cert.Spec.SecretName != "" {
		spec["secretName"] = cert.Spec.SecretName
	}
	if cert.Spec.KeyType != "" {
		spec["keyType"] = cert.Spec.KeyType
	}
	if cert.Spec.MustStaple {
		spec["mustStaple"] = cert.Spec.MustStaple
	}

	return obj
}

// IPPool conversion helpers

func (k *KubernetesBackend) unstructuredToIPPool(obj *unstructured.Unstructured) (*models.IPPool, error) {
	pool := &models.IPPool{
		Name:            obj.GetName(),
		Labels:          obj.GetLabels(),
		Annotations:     obj.GetAnnotations(),
		ResourceVersion: obj.GetResourceVersion(),
	}

	spec, found, err := unstructured.NestedMap(obj.Object, "spec")
	if err != nil {
		return nil, fmt.Errorf("failed to read spec: %w", err)
	}
	if !found {
		return pool, nil
	}

	if cidrs, found, _ := unstructured.NestedStringSlice(spec, "cidrs"); found {
		pool.Spec.CIDRs = cidrs
	}
	if addresses, found, _ := unstructured.NestedStringSlice(spec, "addresses"); found {
		pool.Spec.Addresses = addresses
	}
	pool.Spec.AutoAssign = getBoolField(spec, "autoAssign")

	// Parse status
	status, found, _ := unstructured.NestedMap(obj.Object, "status")
	if found {
		pool.Status.Allocated = safeInt64ToInt32(getInt64Field(status, "allocated"))
		pool.Status.Available = safeInt64ToInt32(getInt64Field(status, "available"))
	}

	return pool, nil
}

func (k *KubernetesBackend) ipPoolToUnstructured(pool *models.IPPool) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "novaedge.io/v1alpha1",
			"kind":       "ProxyIPPool",
			"metadata": map[string]interface{}{
				"name": pool.Name,
			},
			"spec": map[string]interface{}{},
		},
	}

	if pool.ResourceVersion != "" {
		obj.SetResourceVersion(pool.ResourceVersion)
	}
	if len(pool.Labels) > 0 {
		obj.SetLabels(pool.Labels)
	}
	if len(pool.Annotations) > 0 {
		obj.SetAnnotations(pool.Annotations)
	}

	spec, ok := obj.Object["spec"].(map[string]interface{})
	if !ok {
		return obj
	}

	if len(pool.Spec.CIDRs) > 0 {
		cidrs := make([]interface{}, len(pool.Spec.CIDRs))
		for i, c := range pool.Spec.CIDRs {
			cidrs[i] = c
		}
		spec["cidrs"] = cidrs
	}
	if len(pool.Spec.Addresses) > 0 {
		addresses := make([]interface{}, len(pool.Spec.Addresses))
		for i, a := range pool.Spec.Addresses {
			addresses[i] = a
		}
		spec["addresses"] = addresses
	}
	spec["autoAssign"] = pool.Spec.AutoAssign

	return obj
}

// Generic model conversion helpers for Cluster, Federation, RemoteCluster

type genericModel struct {
	name            string
	namespace       string
	labels          map[string]string
	annotations     map[string]string
	spec            map[string]interface{}
	status          map[string]interface{}
	resourceVersion string
}

func unstructuredToGenericModel(obj *unstructured.Unstructured) genericModel {
	g := genericModel{
		name:            obj.GetName(),
		namespace:       obj.GetNamespace(),
		labels:          obj.GetLabels(),
		annotations:     obj.GetAnnotations(),
		resourceVersion: obj.GetResourceVersion(),
	}

	if spec, found, err := unstructured.NestedMap(obj.Object, "spec"); err == nil && found {
		g.spec = spec
	}
	if status, found, err := unstructured.NestedMap(obj.Object, "status"); err == nil && found {
		g.status = status
	}

	return g
}

// genericResourceConfig describes a generic namespaced resource for CRUD operations.
type genericResourceConfig struct {
	kind string
	gvr  schema.GroupVersionResource
}

// genericCreate creates a namespaced resource via the dynamic client and returns a genericModel.
func (k *KubernetesBackend) genericCreate(ctx context.Context, cfg genericResourceConfig, g genericModel) (genericModel, error) {
	if k.readOnly {
		return genericModel{}, errBackendIsReadOnly
	}
	ns := g.namespace
	if ns == "" {
		ns = defaultNamespace
	}
	obj := genericModelToUnstructured(cfg.kind, g.name, ns, g.labels, g.annotations, g.spec, g.resourceVersion)
	result, err := k.dynamic.Resource(cfg.gvr).Namespace(ns).Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		return genericModel{}, fmt.Errorf("failed to create %s: %w", cfg.kind, err)
	}
	return unstructuredToGenericModel(result), nil
}

// genericUpdate updates a namespaced resource via the dynamic client and returns a genericModel.
func (k *KubernetesBackend) genericUpdate(ctx context.Context, cfg genericResourceConfig, g genericModel) (genericModel, error) {
	if k.readOnly {
		return genericModel{}, errBackendIsReadOnly
	}
	ns := g.namespace
	if ns == "" {
		ns = defaultNamespace
	}
	obj := genericModelToUnstructured(cfg.kind, g.name, ns, g.labels, g.annotations, g.spec, g.resourceVersion)
	result, err := k.dynamic.Resource(cfg.gvr).Namespace(ns).Update(ctx, obj, metav1.UpdateOptions{})
	if err != nil {
		return genericModel{}, fmt.Errorf("failed to update %s: %w", cfg.kind, err)
	}
	return unstructuredToGenericModel(result), nil
}

// genericList lists namespaced resources via the dynamic client and returns a slice of genericModels.
func (k *KubernetesBackend) genericList(ctx context.Context, cfg genericResourceConfig, namespace string) ([]genericModel, error) {
	var list *unstructured.UnstructuredList
	var err error

	if namespace == "" || namespace == namespaceAll {
		list, err = k.dynamic.Resource(cfg.gvr).List(ctx, metav1.ListOptions{})
	} else {
		list, err = k.dynamic.Resource(cfg.gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list %s resources: %w", cfg.kind, err)
	}

	result := make([]genericModel, 0, len(list.Items))
	for _, item := range list.Items {
		result = append(result, unstructuredToGenericModel(&item))
	}
	return result, nil
}

// safeInt64ToInt32 safely converts int64 to int32 with bounds checking
func safeInt64ToInt32(val int64) int32 {
	if val > math.MaxInt32 {
		return math.MaxInt32
	}
	if val < math.MinInt32 {
		return math.MinInt32
	}
	return int32(val)
}

func clusterModelToGeneric(c *models.NovaEdgeClusterModel) genericModel {
	return genericModel{name: c.Name, namespace: c.Namespace, labels: c.Labels, annotations: c.Annotations, spec: c.Spec, status: c.Status, resourceVersion: c.ResourceVersion}
}

func genericToClusterModel(g genericModel) *models.NovaEdgeClusterModel {
	return &models.NovaEdgeClusterModel{Name: g.name, Namespace: g.namespace, Labels: g.labels, Annotations: g.annotations, Spec: g.spec, Status: g.status, ResourceVersion: g.resourceVersion}
}

func federationModelToGeneric(f *models.FederationModel) genericModel {
	return genericModel{name: f.Name, namespace: f.Namespace, labels: f.Labels, annotations: f.Annotations, spec: f.Spec, status: f.Status, resourceVersion: f.ResourceVersion}
}

func genericToFederationModel(g genericModel) *models.FederationModel {
	return &models.FederationModel{Name: g.name, Namespace: g.namespace, Labels: g.labels, Annotations: g.annotations, Spec: g.spec, Status: g.status, ResourceVersion: g.resourceVersion}
}

func remoteClusterModelToGeneric(rc *models.RemoteClusterModel) genericModel {
	return genericModel{name: rc.Name, namespace: rc.Namespace, labels: rc.Labels, annotations: rc.Annotations, spec: rc.Spec, status: rc.Status, resourceVersion: rc.ResourceVersion}
}

func genericToRemoteClusterModel(g genericModel) *models.RemoteClusterModel {
	return &models.RemoteClusterModel{Name: g.name, Namespace: g.namespace, Labels: g.labels, Annotations: g.annotations, Spec: g.spec, Status: g.status, ResourceVersion: g.resourceVersion}
}

func genericModelToUnstructured(kind, name, namespace string,
	labels, annotations map[string]string, spec map[string]interface{}, resourceVersion string) *unstructured.Unstructured {

	const apiVersion = "novaedge.io/v1alpha1"

	metadata := map[string]interface{}{
		"name": name,
	}
	if namespace != "" {
		metadata["namespace"] = namespace
	}

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata":   metadata,
		},
	}

	if resourceVersion != "" {
		obj.SetResourceVersion(resourceVersion)
	}
	if len(labels) > 0 {
		obj.SetLabels(labels)
	}
	if len(annotations) > 0 {
		obj.SetAnnotations(annotations)
	}

	if spec != nil {
		obj.Object["spec"] = spec
	} else {
		obj.Object["spec"] = map[string]interface{}{}
	}

	return obj
}

func getBoolField(m map[string]interface{}, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// validateConfig performs basic configuration validation
func validateConfig(config *models.Config) error {
	if config == nil {
		return errConfigIsNil
	}

	// Validate backends have endpoints
	for i, be := range config.Backends {
		if be.Name == "" {
			return fmt.Errorf("%w: %d]: name is required", errBackend, i)
		}
		if len(be.Endpoints) == 0 {
			return fmt.Errorf("%w: %d] '%s': at least one endpoint is required", errBackend, i, be.Name)
		}
	}

	// Validate routes reference valid backends
	backendNames := make(map[string]bool)
	for _, be := range config.Backends {
		backendNames[be.Name] = true
	}

	for i, rt := range config.Routes {
		if rt.Name == "" {
			return fmt.Errorf("%w: %d]: name is required", errRoute, i)
		}
		for j, ref := range rt.BackendRefs {
			if !backendNames[ref.Name] {
				return fmt.Errorf("%w: %d] '%s' backendRef[%d]: unknown backend '%s'", errRoute, i, rt.Name, j, ref.Name)
			}
		}
	}

	return nil
}
