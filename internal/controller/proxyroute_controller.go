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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

// ProxyRouteReconciler reconciles a ProxyRoute object
type ProxyRouteReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=novaedge.io,resources=proxyroutes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=novaedge.io,resources=proxyroutes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=novaedge.io,resources=proxyroutes/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop
func (r *ProxyRouteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the ProxyRoute instance
	route := &novaedgev1alpha1.ProxyRoute{}
	err := r.Get(ctx, req.NamespacedName, route)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ProxyRoute resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ProxyRoute")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling ProxyRoute", "name", route.Name, "hostnames", route.Spec.Hostnames)

	// TODO: Implement route reconciliation logic
	// - Validate backend references exist
	// - Build routing rules for snapshot
	// - Validate match conditions

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *ProxyRouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&novaedgev1alpha1.ProxyRoute{}).
		Complete(r)
}
