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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

// tcpRouteWrapper adapts TCPRoute to the l4RouteObject interface.
type tcpRouteWrapper struct {
	*gatewayv1alpha2.TCPRoute
}

func (w *tcpRouteWrapper) Unwrap() client.Object { return w.TCPRoute }
func (w *tcpRouteWrapper) GetRouteParents() []gatewayv1alpha2.RouteParentStatus {
	return w.Status.Parents
}
func (w *tcpRouteWrapper) SetRouteParents(parents []gatewayv1alpha2.RouteParentStatus) {
	w.Status.Parents = parents
}

// tlsRouteWrapper adapts TLSRoute to the l4RouteObject interface.
type tlsRouteWrapper struct {
	*gatewayv1alpha2.TLSRoute
}

func (w *tlsRouteWrapper) Unwrap() client.Object { return w.TLSRoute }
func (w *tlsRouteWrapper) GetRouteParents() []gatewayv1alpha2.RouteParentStatus {
	return w.Status.Parents
}
func (w *tlsRouteWrapper) SetRouteParents(parents []gatewayv1alpha2.RouteParentStatus) {
	w.Status.Parents = parents
}

// l4RouteObject abstracts TCPRoute and TLSRoute so the reconcile logic can be shared.
type l4RouteObject interface {
	client.Object
	GetGeneration() int64
	GetDeletionTimestamp() *metav1.Time
	GetRouteParents() []gatewayv1alpha2.RouteParentStatus
	SetRouteParents([]gatewayv1alpha2.RouteParentStatus)
	// Unwrap returns the underlying Gateway API route object for status updates,
	// since the wrapper type is not registered in the scheme.
	Unwrap() client.Object
}

// reconcileL4Controller is the top-level helper shared by TCPRouteReconciler and TLSRouteReconciler.
// It fetches the route object, wraps it, creates an adapter, and delegates to reconcileL4Route.
func reconcileL4Controller(
	ctx context.Context,
	cli client.Client,
	statusCli client.SubResourceWriter,
	req ctrl.Request,
	route client.Object,
	wrap func() l4RouteObject,
	routeKind string,
	translate func() (*novaedgev1alpha1.ProxyRoute, error),
) (ctrl.Result, error) {
	if err := cli.Get(ctx, req.NamespacedName, route); err != nil {
		if errors.IsNotFound(err) {
			log.FromContext(ctx).Info(routeKind + " not found, ignoring")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	a := &l4RouteAdapter{client: cli, statusCli: statusCli}
	w := wrap()
	return a.reconcileL4Route(ctx, routeKind, w,
		translate,
		func(ctx context.Context) (ctrl.Result, error) { return a.handleL4RouteDeletion(ctx, routeKind, route) },
		func(ctx context.Context, c metav1.Condition) (ctrl.Result, error) {
			return a.updateL4RouteStatus(ctx, w, c)
		},
	)
}

// l4RouteAdapter wraps common reconcile logic for L4 route types (TCPRoute, TLSRoute).
type l4RouteAdapter struct {
	client    client.Client
	statusCli client.SubResourceWriter
}

// reconcileL4Route performs the shared reconcile logic for TCP/TLS routes.
func (a *l4RouteAdapter) reconcileL4Route(
	ctx context.Context,
	routeKind string,
	route l4RouteObject,
	translate func() (*novaedgev1alpha1.ProxyRoute, error),
	handleDeletion func(context.Context) (ctrl.Result, error),
	updateStatus func(context.Context, metav1.Condition) (ctrl.Result, error),
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling "+routeKind, "name", route.GetName(), "namespace", route.GetNamespace())

	if !route.GetDeletionTimestamp().IsZero() {
		return handleDeletion(ctx)
	}

	proxyRoute, err := translate()
	if err != nil {
		logger.Error(err, "Failed to translate "+routeKind)
		return updateStatus(ctx, metav1.Condition{
			Type:               "Accepted",
			Status:             metav1.ConditionFalse,
			Reason:             "Invalid",
			Message:            fmt.Sprintf("Translation failed: %v", err),
			ObservedGeneration: route.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		})
	}

	existing := &novaedgev1alpha1.ProxyRoute{}
	err = a.client.Get(ctx, types.NamespacedName{Name: route.GetName(), Namespace: route.GetNamespace()}, existing)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating ProxyRoute from "+routeKind, "name", proxyRoute.Name)
			if err := a.client.Create(ctx, proxyRoute); err != nil {
				logger.Error(err, "Failed to create ProxyRoute from "+routeKind)
				return ctrl.Result{}, err
			}
		} else {
			return ctrl.Result{}, err
		}
	} else {
		existing.Spec = proxyRoute.Spec
		existing.Labels = proxyRoute.Labels
		existing.Annotations = proxyRoute.Annotations
		if err := a.client.Update(ctx, existing); err != nil {
			logger.Error(err, "Failed to update ProxyRoute from "+routeKind)
			return ctrl.Result{}, err
		}
	}

	return updateStatus(ctx, metav1.Condition{
		Type:               "Accepted",
		Status:             metav1.ConditionTrue,
		Reason:             "Accepted",
		Message:            routeKind + " accepted and translated to ProxyRoute",
		ObservedGeneration: route.GetGeneration(),
		LastTransitionTime: metav1.Now(),
	})
}

// handleL4RouteDeletion handles cleanup when an L4 route is deleted.
func (a *l4RouteAdapter) handleL4RouteDeletion(ctx context.Context, routeKind string, route client.Object) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	proxyRoute := &novaedgev1alpha1.ProxyRoute{}
	err := a.client.Get(ctx, types.NamespacedName{Name: route.GetName(), Namespace: route.GetNamespace()}, proxyRoute)
	if err == nil {
		logger.Info("Deleting ProxyRoute from "+routeKind+" deletion", "name", proxyRoute.Name)
		if err := a.client.Delete(ctx, proxyRoute); err != nil && !errors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	} else if !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	TriggerConfigUpdate()
	return ctrl.Result{}, nil
}

// updateL4RouteStatus updates the route parent status.
func (a *l4RouteAdapter) updateL4RouteStatus(
	ctx context.Context,
	route l4RouteObject,
	condition metav1.Condition,
) (ctrl.Result, error) {
	parents := route.GetRouteParents()
	if len(parents) == 0 {
		parents = make([]gatewayv1alpha2.RouteParentStatus, 1)
	}
	meta.SetStatusCondition(&parents[0].Conditions, condition)
	route.SetRouteParents(parents)
	if err := a.statusCli.Update(ctx, route.Unwrap()); err != nil {
		return ctrl.Result{}, err
	}
	TriggerConfigUpdate()
	return ctrl.Result{}, nil
}
