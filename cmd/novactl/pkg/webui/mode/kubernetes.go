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
	"fmt"

	"github.com/piwi3910/novaedge/cmd/novactl/pkg/webui/models"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// KubernetesBackend implements Backend for Kubernetes CRD operations
type KubernetesBackend struct {
	dynamic   dynamic.Interface
	clientset kubernetes.Interface
	readOnly  bool
}

// GVRs for NovaEdge CRDs
var (
	gvrGateway = schema.GroupVersionResource{
		Group:    "novaedge.piwi3910.com",
		Version:  "v1alpha1",
		Resource: "proxygateways",
	}
	gvrRoute = schema.GroupVersionResource{
		Group:    "novaedge.piwi3910.com",
		Version:  "v1alpha1",
		Resource: "proxyroutes",
	}
	gvrBackend = schema.GroupVersionResource{
		Group:    "novaedge.piwi3910.com",
		Version:  "v1alpha1",
		Resource: "proxybackends",
	}
	gvrVIP = schema.GroupVersionResource{
		Group:    "novaedge.piwi3910.com",
		Version:  "v1alpha1",
		Resource: "proxyvips",
	}
	gvrPolicy = schema.GroupVersionResource{
		Group:    "novaedge.piwi3910.com",
		Version:  "v1alpha1",
		Resource: "proxypolicies",
	}
)

// NewKubernetesBackend creates a new Kubernetes backend
func NewKubernetesBackend(config *rest.Config, readOnly bool) (*KubernetesBackend, error) {
	if config == nil {
		return nil, fmt.Errorf("kubernetes config is required")
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

	if namespace == "" || namespace == "all" {
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
		return nil, fmt.Errorf("backend is read-only")
	}

	obj := k.gatewayToUnstructured(gateway)
	namespace := gateway.Namespace
	if namespace == "" {
		namespace = "default"
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
		return nil, fmt.Errorf("backend is read-only")
	}

	obj := k.gatewayToUnstructured(gateway)
	namespace := gateway.Namespace
	if namespace == "" {
		namespace = "default"
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
		return fmt.Errorf("backend is read-only")
	}

	return k.dynamic.Resource(gvrGateway).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// ListRoutes returns all routes
func (k *KubernetesBackend) ListRoutes(ctx context.Context, namespace string) ([]models.Route, error) {
	var list *unstructured.UnstructuredList
	var err error

	if namespace == "" || namespace == "all" {
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
		return nil, fmt.Errorf("backend is read-only")
	}

	obj := k.routeToUnstructured(route)
	namespace := route.Namespace
	if namespace == "" {
		namespace = "default"
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
		return nil, fmt.Errorf("backend is read-only")
	}

	obj := k.routeToUnstructured(route)
	namespace := route.Namespace
	if namespace == "" {
		namespace = "default"
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
		return fmt.Errorf("backend is read-only")
	}

	return k.dynamic.Resource(gvrRoute).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// ListBackends returns all backends
func (k *KubernetesBackend) ListBackends(ctx context.Context, namespace string) ([]models.Backend, error) {
	var list *unstructured.UnstructuredList
	var err error

	if namespace == "" || namespace == "all" {
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
		return nil, fmt.Errorf("backend is read-only")
	}

	obj := k.backendToUnstructured(backend)
	namespace := backend.Namespace
	if namespace == "" {
		namespace = "default"
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
		return nil, fmt.Errorf("backend is read-only")
	}

	obj := k.backendToUnstructured(backend)
	namespace := backend.Namespace
	if namespace == "" {
		namespace = "default"
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
		return fmt.Errorf("backend is read-only")
	}

	return k.dynamic.Resource(gvrBackend).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// ListVIPs returns all VIPs
func (k *KubernetesBackend) ListVIPs(ctx context.Context, namespace string) ([]models.VIP, error) {
	var list *unstructured.UnstructuredList
	var err error

	if namespace == "" || namespace == "all" {
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
		return nil, fmt.Errorf("backend is read-only")
	}

	obj := k.vipToUnstructured(vip)
	namespace := vip.Namespace
	if namespace == "" {
		namespace = "default"
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
		return nil, fmt.Errorf("backend is read-only")
	}

	obj := k.vipToUnstructured(vip)
	namespace := vip.Namespace
	if namespace == "" {
		namespace = "default"
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
		return fmt.Errorf("backend is read-only")
	}

	return k.dynamic.Resource(gvrVIP).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// ListPolicies returns all policies
func (k *KubernetesBackend) ListPolicies(ctx context.Context, namespace string) ([]models.Policy, error) {
	var list *unstructured.UnstructuredList
	var err error

	if namespace == "" || namespace == "all" {
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
		return nil, fmt.Errorf("backend is read-only")
	}

	obj := k.policyToUnstructured(policy)
	namespace := policy.Namespace
	if namespace == "" {
		namespace = "default"
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
		return nil, fmt.Errorf("backend is read-only")
	}

	obj := k.policyToUnstructured(policy)
	namespace := policy.Namespace
	if namespace == "" {
		namespace = "default"
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
		return fmt.Errorf("backend is read-only")
	}

	return k.dynamic.Resource(gvrPolicy).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
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
func (k *KubernetesBackend) ValidateConfig(ctx context.Context, config *models.Config) error {
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
		return nil, fmt.Errorf("backend is read-only")
	}

	var config models.Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	result := &models.ImportResult{DryRun: dryRun}

	// Import gateways
	for _, gw := range config.Gateways {
		ref := models.ResourceRef{Kind: "Gateway", Name: gw.Name, Namespace: gw.Namespace}
		if dryRun {
			result.Created = append(result.Created, ref)
			continue
		}

		_, err := k.GetGateway(ctx, gw.Namespace, gw.Name)
		if err != nil {
			// Create
			if _, err := k.CreateGateway(ctx, &gw); err != nil {
				result.Errors = append(result.Errors, models.ImportError{Resource: ref, Error: err.Error()})
			} else {
				result.Created = append(result.Created, ref)
			}
		} else {
			// Update
			if _, err := k.UpdateGateway(ctx, &gw); err != nil {
				result.Errors = append(result.Errors, models.ImportError{Resource: ref, Error: err.Error()})
			} else {
				result.Updated = append(result.Updated, ref)
			}
		}
	}

	// Import routes
	for _, rt := range config.Routes {
		ref := models.ResourceRef{Kind: "Route", Name: rt.Name, Namespace: rt.Namespace}
		if dryRun {
			result.Created = append(result.Created, ref)
			continue
		}

		_, err := k.GetRoute(ctx, rt.Namespace, rt.Name)
		if err != nil {
			if _, err := k.CreateRoute(ctx, &rt); err != nil {
				result.Errors = append(result.Errors, models.ImportError{Resource: ref, Error: err.Error()})
			} else {
				result.Created = append(result.Created, ref)
			}
		} else {
			if _, err := k.UpdateRoute(ctx, &rt); err != nil {
				result.Errors = append(result.Errors, models.ImportError{Resource: ref, Error: err.Error()})
			} else {
				result.Updated = append(result.Updated, ref)
			}
		}
	}

	// Import backends
	for _, be := range config.Backends {
		ref := models.ResourceRef{Kind: "Backend", Name: be.Name, Namespace: be.Namespace}
		if dryRun {
			result.Created = append(result.Created, ref)
			continue
		}

		_, err := k.GetBackend(ctx, be.Namespace, be.Name)
		if err != nil {
			if _, err := k.CreateBackend(ctx, &be); err != nil {
				result.Errors = append(result.Errors, models.ImportError{Resource: ref, Error: err.Error()})
			} else {
				result.Created = append(result.Created, ref)
			}
		} else {
			if _, err := k.UpdateBackend(ctx, &be); err != nil {
				result.Errors = append(result.Errors, models.ImportError{Resource: ref, Error: err.Error()})
			} else {
				result.Updated = append(result.Updated, ref)
			}
		}
	}

	// Import VIPs
	for _, vip := range config.VIPs {
		ref := models.ResourceRef{Kind: "VIP", Name: vip.Name, Namespace: vip.Namespace}
		if dryRun {
			result.Created = append(result.Created, ref)
			continue
		}

		_, err := k.GetVIP(ctx, vip.Namespace, vip.Name)
		if err != nil {
			if _, err := k.CreateVIP(ctx, &vip); err != nil {
				result.Errors = append(result.Errors, models.ImportError{Resource: ref, Error: err.Error()})
			} else {
				result.Created = append(result.Created, ref)
			}
		} else {
			if _, err := k.UpdateVIP(ctx, &vip); err != nil {
				result.Errors = append(result.Errors, models.ImportError{Resource: ref, Error: err.Error()})
			} else {
				result.Updated = append(result.Updated, ref)
			}
		}
	}

	// Import policies
	for _, pol := range config.Policies {
		ref := models.ResourceRef{Kind: "Policy", Name: pol.Name, Namespace: pol.Namespace}
		if dryRun {
			result.Created = append(result.Created, ref)
			continue
		}

		_, err := k.GetPolicy(ctx, pol.Namespace, pol.Name)
		if err != nil {
			if _, err := k.CreatePolicy(ctx, &pol); err != nil {
				result.Errors = append(result.Errors, models.ImportError{Resource: ref, Error: err.Error()})
			} else {
				result.Created = append(result.Created, ref)
			}
		} else {
			if _, err := k.UpdatePolicy(ctx, &pol); err != nil {
				result.Errors = append(result.Errors, models.ImportError{Resource: ref, Error: err.Error()})
			} else {
				result.Updated = append(result.Updated, ref)
			}
		}
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
	if err != nil || !found {
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
			"apiVersion": "novaedge.piwi3910.com/v1alpha1",
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

	spec := obj.Object["spec"].(map[string]interface{})

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
	if err != nil || !found {
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
			"apiVersion": "novaedge.piwi3910.com/v1alpha1",
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

	spec := obj.Object["spec"].(map[string]interface{})

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
	if err != nil || !found {
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
			"apiVersion": "novaedge.piwi3910.com/v1alpha1",
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

	spec := obj.Object["spec"].(map[string]interface{})

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
	if err != nil || !found {
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
			"apiVersion": "novaedge.piwi3910.com/v1alpha1",
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

	spec := obj.Object["spec"].(map[string]interface{})

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
	if err != nil || !found {
		return pol, nil
	}

	pol.Type = getStringField(spec, "type")

	return pol, nil
}

func (k *KubernetesBackend) policyToUnstructured(pol *models.Policy) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "novaedge.piwi3910.com/v1alpha1",
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

// validateConfig performs basic configuration validation
func validateConfig(config *models.Config) error {
	if config == nil {
		return fmt.Errorf("config is nil")
	}

	// Validate backends have endpoints
	for i, be := range config.Backends {
		if be.Name == "" {
			return fmt.Errorf("backend[%d]: name is required", i)
		}
		if len(be.Endpoints) == 0 {
			return fmt.Errorf("backend[%d] '%s': at least one endpoint is required", i, be.Name)
		}
	}

	// Validate routes reference valid backends
	backendNames := make(map[string]bool)
	for _, be := range config.Backends {
		backendNames[be.Name] = true
	}

	for i, rt := range config.Routes {
		if rt.Name == "" {
			return fmt.Errorf("route[%d]: name is required", i)
		}
		for j, ref := range rt.BackendRefs {
			if !backendNames[ref.Name] {
				return fmt.Errorf("route[%d] '%s' backendRef[%d]: unknown backend '%s'", i, rt.Name, j, ref.Name)
			}
		}
	}

	return nil
}
