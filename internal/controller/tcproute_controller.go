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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

// TCPRouteReconciler reconciles Gateway API TCPRoute objects
type TCPRouteReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=tcproutes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=tcproutes/status,verbs=get;update;patch

// Reconcile translates a TCPRoute into a NovaEdge ProxyRoute with L4 TCP annotations
func (r *TCPRouteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	route := &gatewayv1alpha2.TCPRoute{}
	return reconcileL4Controller(ctx, r.Client, r.Status(), req, route,
		func() l4RouteObject { return &tcpRouteWrapper{route} },
		"TCPRoute",
		func() (*novaedgev1alpha1.ProxyRoute, error) { return TranslateTCPRouteToProxyRoute(route) },
	)
}

// SetupWithManager sets up the TCPRoute controller with the Manager
func (r *TCPRouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1alpha2.TCPRoute{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&novaedgev1alpha1.ProxyRoute{}).
		Complete(r)
}

// TLSRouteReconciler reconciles Gateway API TLSRoute objects
type TLSRouteReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=tlsroutes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=tlsroutes/status,verbs=get;update;patch

// Reconcile translates a TLSRoute into a NovaEdge ProxyRoute with TLS passthrough annotations
func (r *TLSRouteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	route := &gatewayv1alpha2.TLSRoute{}
	return reconcileL4Controller(ctx, r.Client, r.Status(), req, route,
		func() l4RouteObject { return &tlsRouteWrapper{route} },
		"TLSRoute",
		func() (*novaedgev1alpha1.ProxyRoute, error) { return TranslateTLSRouteToProxyRoute(route) },
	)
}

// SetupWithManager sets up the TLSRoute controller with the Manager
func (r *TLSRouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1alpha2.TLSRoute{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&novaedgev1alpha1.ProxyRoute{}).
		Complete(r)
}
