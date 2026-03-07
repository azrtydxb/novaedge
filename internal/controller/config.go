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

// Package controller implements the Kubernetes control-plane logic for NovaEdge,
// watching CRDs, Ingress, and Gateway API resources to build and distribute
// routing configuration to node agents.
package controller

import (
	"context"
	"sync"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/azrtydxb/novaedge/internal/controller/snapshot"
)

// Condition reason constants used across controllers
const (
	// ConditionReasonValid indicates the resource configuration is valid
	ConditionReasonValid = "Valid"
	// ConditionReasonValidationFailed indicates the resource configuration failed validation
	ConditionReasonValidationFailed = "ValidationFailed"
)

// kindGateway is the Gateway API Kind string for Gateway resources.
const kindGateway = "Gateway"

var (
	configServer   *snapshot.Server
	configServerMu sync.RWMutex
)

// SetConfigServer sets the config server instance for reconcilers to use
func SetConfigServer(server *snapshot.Server) {
	configServerMu.Lock()
	defer configServerMu.Unlock()
	configServer = server
}

// GetConfigServer returns the config server instance
func GetConfigServer() *snapshot.Server {
	configServerMu.RLock()
	defer configServerMu.RUnlock()
	return configServer
}

// TriggerConfigUpdate triggers a config update for all nodes
func TriggerConfigUpdate() {
	server := GetConfigServer()
	if server != nil {
		server.TriggerUpdate("")
	}
}

// TriggerNodeConfigUpdate triggers a config update for a specific node
func TriggerNodeConfigUpdate(nodeName string) {
	server := GetConfigServer()
	if server != nil {
		server.TriggerUpdate(nodeName)
	}
}

// reconcileWithGenerationCheck implements the common CRD reconciliation pattern:
// fetch the resource, skip if not found, skip if generation already reconciled,
// run validation, then trigger a config update.
func reconcileWithGenerationCheck(
	ctx context.Context,
	cli client.Client,
	req ctrl.Request,
	obj client.Object,
	kind string,
	getObservedGeneration func() int64,
	logFields func() []interface{},
	validate func() error,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if err := cli.Get(ctx, req.NamespacedName, obj); err != nil {
		if errors.IsNotFound(err) {
			logger.Info(kind + " resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get "+kind)
		return ctrl.Result{}, err
	}

	// Skip if already reconciled this generation (ObservedGeneration > 0
	// ensures first-ever reconciliation always proceeds)
	observed := getObservedGeneration()
	if observed != 0 && observed == obj.GetGeneration() {
		return ctrl.Result{}, nil
	}

	logger.Info("Reconciling "+kind, logFields()...)

	if err := validate(); err != nil {
		logger.Error(err, "Failed to validate "+kind)
		return ctrl.Result{Requeue: true}, err
	}

	TriggerConfigUpdate()
	return ctrl.Result{}, nil
}

// handleResourceDeletion is a shared helper that deletes an associated proxy resource and removes
// a finalizer from the source Gateway API resource (Gateway or HTTPRoute).
func handleResourceDeletion(ctx context.Context, cli client.Client, source client.Object, proxyObj client.Object, kind, finalizerName string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Handling "+kind+" deletion", "name", source.GetName())

	// Delete associated proxy resource if it exists
	err := cli.Get(ctx, types.NamespacedName{Name: source.GetName(), Namespace: source.GetNamespace()}, proxyObj)
	if err == nil {
		logger.Info("Deleting associated proxy resource", "kind", kind, "name", proxyObj.GetName())
		if err := cli.Delete(ctx, proxyObj); err != nil && !errors.IsNotFound(err) {
			logger.Error(err, "Failed to delete proxy resource")
			return ctrl.Result{}, err
		}
	} else if !errors.IsNotFound(err) {
		logger.Error(err, "Failed to get proxy resource for deletion")
		return ctrl.Result{}, err
	}

	// Remove finalizer if it exists
	if controllerutil.ContainsFinalizer(source, finalizerName) {
		controllerutil.RemoveFinalizer(source, finalizerName)
		if err := cli.Update(ctx, source); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}
