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
	"encoding/json"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
	"github.com/azrtydxb/novaedge/internal/controller/ipam"
)

var (
	errUnsupportedVIPMode       = errors.New("unsupported VIP mode")
	errUnsupportedAddressFamily = errors.New("unsupported address family")
)

const (
	// annotationVIPMode is the required annotation that triggers VIP creation.
	annotationVIPMode = "novaedge.io/vip-mode"
	// annotationAddressPool specifies the IP pool name for allocation.
	annotationAddressPool = "novaedge.io/address-pool"
	// annotationAddressFamily specifies the address family (ipv4, ipv6, dual).
	annotationAddressFamily = "novaedge.io/address-family"
	// annotationBGPConfig specifies JSON-encoded BGP configuration.
	annotationBGPConfig = "novaedge.io/bgp-config"
	// annotationOSPFConfig specifies JSON-encoded OSPF configuration.
	annotationOSPFConfig = "novaedge.io/ospf-config"
	// annotationBFDEnabled enables BFD for fast failure detection.
	annotationBFDEnabled = "novaedge.io/bfd-enabled"
	// annotationNodeSelector specifies a JSON label selector for node placement.
	annotationNodeSelector = "novaedge.io/node-selector"

	// defaultPoolName is the default IP pool when none is specified.
	defaultPoolName = "default"
	// vipNamePrefix is prepended to Service names to form ProxyVIP names.
	vipNamePrefix = "svc-"
)

// ServiceReconciler reconciles Service objects of type LoadBalancer and creates
// corresponding ProxyVIP resources with IPAM allocation.
type ServiceReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	Allocator       *ipam.Allocator
	Recorder        record.EventRecorder
	EnableServiceLB bool
}

// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=services/status,verbs=update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=novaedge.io,resources=proxyvips,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles Service reconciliation for LoadBalancer type services with
// the novaedge.io/vip-mode annotation.
func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !r.EnableServiceLB {
		return ctrl.Result{}, nil
	}

	// Fetch the Service
	svc := &corev1.Service{}
	err := r.Get(ctx, req.NamespacedName, svc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Service deleted — IPAM release is handled below when ProxyVIP is not found
			logger.Info("Service resource not found, cleaning up IPAM allocation if any")
			r.releaseIPAMForService(req.Name, req.Namespace)
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Service")
		return ctrl.Result{}, err
	}

	// Only handle LoadBalancer services
	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		return ctrl.Result{}, nil
	}

	// Require the vip-mode annotation for opt-in
	vipModeStr, hasAnnotation := svc.Annotations[annotationVIPMode]
	if !hasAnnotation {
		return ctrl.Result{}, nil
	}

	logger.Info("Reconciling LoadBalancer Service",
		"name", svc.Name,
		"namespace", svc.Namespace,
		"vipMode", vipModeStr,
	)

	// Validate VIP mode
	vipMode, err := parseVIPMode(vipModeStr)
	if err != nil {
		r.recordEvent(svc, corev1.EventTypeWarning, "InvalidVIPMode",
			fmt.Sprintf("Invalid vip-mode annotation value %q: %v", vipModeStr, err))
		logger.Error(err, "Invalid vip-mode annotation", "value", vipModeStr)
		return ctrl.Result{}, nil // Do not requeue for invalid config
	}

	// Determine pool name
	poolName := defaultPoolName
	if pool, ok := svc.Annotations[annotationAddressPool]; ok && pool != "" {
		poolName = pool
	}

	// Build VIP name from Service
	vipName := vipNamePrefix + svc.Name

	// Allocate IP from IPAM pool
	address, err := r.Allocator.Allocate(poolName, vipName)
	if err != nil {
		r.recordEvent(svc, corev1.EventTypeWarning, "IPAllocationFailed",
			fmt.Sprintf("Failed to allocate IP from pool %q: %v", poolName, err))
		logger.Error(err, "Failed to allocate IP from pool", "pool", poolName)
		return ctrl.Result{}, err
	}

	// Build ProxyVIP spec
	vipSpec, err := r.buildVIPSpec(svc, vipMode, address, poolName)
	if err != nil {
		r.recordEvent(svc, corev1.EventTypeWarning, "InvalidAnnotation",
			fmt.Sprintf("Failed to parse annotations: %v", err))
		logger.Error(err, "Failed to build VIP spec from annotations")
		// Release the allocated IP since we can't create the VIP
		r.Allocator.Release(poolName, vipName)
		return ctrl.Result{}, nil // Do not requeue for invalid annotations
	}

	// Create or update ProxyVIP
	existingVIP := &novaedgev1alpha1.ProxyVIP{}
	err = r.Get(ctx, types.NamespacedName{Name: vipName, Namespace: svc.Namespace}, existingVIP)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create new ProxyVIP
			vip := &novaedgev1alpha1.ProxyVIP{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vipName,
					Namespace: svc.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						*metav1.NewControllerRef(svc, corev1.SchemeGroupVersion.WithKind("Service")),
					},
					Labels: map[string]string{
						"novaedge.io/managed-by":   "service-lb",
						"novaedge.io/service-name": svc.Name,
					},
				},
				Spec: *vipSpec,
			}

			if createErr := r.Create(ctx, vip); createErr != nil {
				r.recordEvent(svc, corev1.EventTypeWarning, "VIPCreateFailed",
					fmt.Sprintf("Failed to create ProxyVIP: %v", createErr))
				logger.Error(createErr, "Failed to create ProxyVIP", "vipName", vipName)
				// Release the allocated IP since VIP creation failed
				r.Allocator.Release(poolName, vipName)
				return ctrl.Result{}, createErr
			}

			r.recordEvent(svc, corev1.EventTypeNormal, "VIPCreated",
				fmt.Sprintf("Created ProxyVIP %q with address %s", vipName, address))
			logger.Info("Created ProxyVIP for Service",
				"vipName", vipName,
				"address", address,
				"mode", vipMode,
			)
		} else {
			logger.Error(err, "Failed to get existing ProxyVIP", "vipName", vipName)
			return ctrl.Result{}, err
		}
	} else {
		// Update existing ProxyVIP spec if changed
		existingVIP.Spec = *vipSpec
		if updateErr := r.Update(ctx, existingVIP); updateErr != nil {
			logger.Error(updateErr, "Failed to update ProxyVIP", "vipName", vipName)
			return ctrl.Result{}, updateErr
		}
		logger.Info("Updated ProxyVIP for Service", "vipName", vipName)
	}

	// Update Service status with the allocated IP
	if err := r.updateServiceStatus(ctx, svc, address); err != nil {
		logger.Error(err, "Failed to update Service status")
		return ctrl.Result{}, err
	}

	// Trigger a config update so agents pick up the new VIP
	TriggerConfigUpdate()

	return ctrl.Result{}, nil
}

// releaseIPAMForService releases the IPAM allocation for a deleted Service.
func (r *ServiceReconciler) releaseIPAMForService(serviceName, namespace string) {
	if r.Allocator == nil {
		return
	}
	vipName := vipNamePrefix + serviceName
	// Try to release from all known pools since we don't track which pool was used
	for _, poolName := range r.Allocator.GetPoolNames() {
		r.Allocator.Release(poolName, vipName)
	}
	_ = namespace // namespace used for logging context if needed
}

// parseVIPMode validates and converts a string annotation value to a VIPMode.
func parseVIPMode(mode string) (novaedgev1alpha1.VIPMode, error) {
	switch novaedgev1alpha1.VIPMode(mode) {
	case novaedgev1alpha1.VIPModeL2ARP:
		return novaedgev1alpha1.VIPModeL2ARP, nil
	case novaedgev1alpha1.VIPModeBGP:
		return novaedgev1alpha1.VIPModeBGP, nil
	case novaedgev1alpha1.VIPModeOSPF:
		return novaedgev1alpha1.VIPModeOSPF, nil
	default:
		return "", fmt.Errorf("%w: %q: must be one of L2ARP, BGP, OSPF", errUnsupportedVIPMode, mode)
	}
}

// buildVIPSpec constructs a ProxyVIPSpec from Service annotations.
func (r *ServiceReconciler) buildVIPSpec(svc *corev1.Service, mode novaedgev1alpha1.VIPMode, address, poolName string) (*novaedgev1alpha1.ProxyVIPSpec, error) {
	spec := &novaedgev1alpha1.ProxyVIPSpec{
		Address: address,
		Mode:    mode,
		PoolRef: &novaedgev1alpha1.LocalObjectReference{
			Name: poolName,
		},
	}

	// Extract ports from Service spec
	ports := make([]int32, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		ports = append(ports, p.Port)
	}
	spec.Ports = ports

	// Parse address family annotation
	if af, ok := svc.Annotations[annotationAddressFamily]; ok && af != "" {
		family, err := parseAddressFamily(af)
		if err != nil {
			return nil, fmt.Errorf("invalid address-family annotation: %w", err)
		}
		spec.AddressFamily = family
	}

	// Parse BGP config annotation
	if bgpJSON, ok := svc.Annotations[annotationBGPConfig]; ok && bgpJSON != "" {
		bgpConfig := &novaedgev1alpha1.BGPConfig{}
		if err := json.Unmarshal([]byte(bgpJSON), bgpConfig); err != nil {
			return nil, fmt.Errorf("invalid bgp-config annotation JSON: %w", err)
		}
		spec.BGPConfig = bgpConfig
	}

	// Parse OSPF config annotation
	if ospfJSON, ok := svc.Annotations[annotationOSPFConfig]; ok && ospfJSON != "" {
		ospfConfig := &novaedgev1alpha1.OSPFConfig{}
		if err := json.Unmarshal([]byte(ospfJSON), ospfConfig); err != nil {
			return nil, fmt.Errorf("invalid ospf-config annotation JSON: %w", err)
		}
		spec.OSPFConfig = ospfConfig
	}

	// Parse BFD enabled annotation
	if bfdStr, ok := svc.Annotations[annotationBFDEnabled]; ok && bfdStr == "true" {
		spec.BFD = &novaedgev1alpha1.BFDConfig{
			Enabled: true,
		}
	}

	// Parse node selector annotation
	if nsJSON, ok := svc.Annotations[annotationNodeSelector]; ok && nsJSON != "" {
		nodeSelector := &metav1.LabelSelector{}
		if err := json.Unmarshal([]byte(nsJSON), nodeSelector); err != nil {
			return nil, fmt.Errorf("invalid node-selector annotation JSON: %w", err)
		}
		spec.NodeSelector = nodeSelector
	}

	return spec, nil
}

// parseAddressFamily validates and converts an address family string.
func parseAddressFamily(af string) (novaedgev1alpha1.AddressFamily, error) {
	switch novaedgev1alpha1.AddressFamily(af) {
	case novaedgev1alpha1.AddressFamilyIPv4:
		return novaedgev1alpha1.AddressFamilyIPv4, nil
	case novaedgev1alpha1.AddressFamilyIPv6:
		return novaedgev1alpha1.AddressFamilyIPv6, nil
	case novaedgev1alpha1.AddressFamilyDual:
		return novaedgev1alpha1.AddressFamilyDual, nil
	default:
		return "", fmt.Errorf("%w: %q: must be one of ipv4, ipv6, dual", errUnsupportedAddressFamily, af)
	}
}

// updateServiceStatus patches the Service status with the allocated load balancer IP.
func (r *ServiceReconciler) updateServiceStatus(ctx context.Context, svc *corev1.Service, address string) error {
	// Strip CIDR suffix to get a plain IP for the status
	ip := stripCIDR(address)

	svc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{
		{IP: ip},
	}
	return r.Status().Update(ctx, svc)
}

// stripCIDR removes the /prefix from a CIDR string to return a plain IP.
func stripCIDR(cidr string) string {
	for i := range cidr {
		if cidr[i] == '/' {
			return cidr[:i]
		}
	}
	return cidr
}

// recordEvent records a Kubernetes event if a recorder is available.
func (r *ServiceReconciler) recordEvent(obj runtime.Object, eventType, reason, message string) {
	if r.Recorder != nil {
		r.Recorder.Event(obj, eventType, reason, message)
	}
}

// SetupWithManager sets up the controller with the Manager
func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&novaedgev1alpha1.ProxyVIP{}).
		Complete(r)
}
