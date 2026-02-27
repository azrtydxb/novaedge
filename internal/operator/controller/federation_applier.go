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
	"sync"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
	"github.com/piwi3910/novaedge/internal/controller/federation"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)
var (
	errFailedToDeepCopyDesiredObjectFor = errors.New("failed to deep copy desired object for")
	errFailedToDeepCopyStubObjectFor = errors.New("failed to deep copy stub object for")
)


const (
	// FederationOriginLabel marks a resource as originating from federation sync.
	// Its value is the name of the originating federation member.
	FederationOriginLabel = "novaedge.io/federation-origin"
)

// FederationResourceApplier applies incoming federated resource changes
// to the local Kubernetes API server. It labels all written resources
// with FederationOriginLabel to prevent sync loops and to protect
// non-federated resources from accidental deletion.
type FederationResourceApplier struct {
	client client.Client
	scheme *runtime.Scheme
	logger *zap.Logger

	// serviceEndpoints caches ServiceEndpoints received from federated
	// clusters, keyed by "namespace/serviceName".
	serviceEndpoints sync.Map // map[string]*pb.ServiceEndpoints
}

// NewFederationResourceApplier creates a new FederationResourceApplier.
func NewFederationResourceApplier(c client.Client, scheme *runtime.Scheme, logger *zap.Logger) *FederationResourceApplier {
	return &FederationResourceApplier{
		client: c,
		scheme: scheme,
		logger: logger.Named("federation-applier"),
	}
}

// Apply processes a single incoming federated resource change. It routes
// the change to the appropriate handler based on the resource kind.
func (a *FederationResourceApplier) Apply(ctx context.Context, key federation.ResourceKey, changeType federation.ChangeType, data []byte) {
	log := a.logger.With(
		zap.String("resource", key.String()),
		zap.String("change_type", string(changeType)),
	)

	var err error
	switch key.Kind {
	case "ProxyGateway":
		err = a.applyCRD(ctx, key, changeType, data, &novaedgev1alpha1.ProxyGateway{})
	case "ProxyRoute":
		err = a.applyCRD(ctx, key, changeType, data, &novaedgev1alpha1.ProxyRoute{})
	case "ProxyBackend":
		err = a.applyCRD(ctx, key, changeType, data, &novaedgev1alpha1.ProxyBackend{})
	case "ProxyPolicy":
		err = a.applyCRD(ctx, key, changeType, data, &novaedgev1alpha1.ProxyPolicy{})
	case "ProxyVIP":
		err = a.applyCRD(ctx, key, changeType, data, &novaedgev1alpha1.ProxyVIP{})
	case "ConfigMap":
		err = a.applyConfigMap(ctx, key, changeType, data)
	case "Secret":
		err = a.applySecret(ctx, key, changeType, data)
	case "ServiceEndpoints":
		err = a.applyServiceEndpoints(key, changeType, data)
	default:
		log.Warn("Unknown resource kind received from federation, skipping")
		return
	}

	if err != nil {
		log.Error("Failed to apply federated resource change", zap.Error(err))
	}
}

// applyServiceEndpoints caches or removes ServiceEndpoints received from
// federated clusters. The data is stored in an in-memory sync.Map keyed by
// "namespace/serviceName" for thread-safe concurrent access.
func (a *FederationResourceApplier) applyServiceEndpoints(key federation.ResourceKey, changeType federation.ChangeType, data []byte) error {
	cacheKey := key.Namespace + "/" + key.Name

	if changeType == federation.ChangeTypeDeleted {
		a.serviceEndpoints.Delete(cacheKey)
		a.logger.Info("Deleted cached ServiceEndpoints",
			zap.String("key", cacheKey),
		)
		return nil
	}

	var endpoints pb.ServiceEndpoints
	if err := json.Unmarshal(data, &endpoints); err != nil {
		return fmt.Errorf("failed to unmarshal ServiceEndpoints %s: %w", cacheKey, err)
	}

	a.serviceEndpoints.Store(cacheKey, &endpoints)
	a.logger.Info("Cached ServiceEndpoints",
		zap.String("key", cacheKey),
		zap.Int("endpoint_count", len(endpoints.GetEndpoints())),
	)
	return nil
}

// GetCachedServiceEndpoints returns the cached ServiceEndpoints for a given
// namespace and service name, or nil if not cached.
func (a *FederationResourceApplier) GetCachedServiceEndpoints(namespace, serviceName string) *pb.ServiceEndpoints {
	cacheKey := namespace + "/" + serviceName
	val, ok := a.serviceEndpoints.Load(cacheKey)
	if !ok {
		return nil
	}
	endpoints, ok := val.(*pb.ServiceEndpoints)
	if !ok {
		return nil
	}
	return endpoints
}

// federatedObject is a minimal interface for objects that carry ObjectMeta,
// allowing us to write generic create/update/delete helpers.
type federatedObject interface {
	client.Object
}

// applyCRD handles create, update, and delete for NovaEdge CRD resources.
func (a *FederationResourceApplier) applyCRD(ctx context.Context, key federation.ResourceKey, changeType federation.ChangeType, data []byte, obj federatedObject) error {
	if changeType == federation.ChangeTypeDeleted {
		return a.deleteResource(ctx, key, obj)
	}

	// Unmarshal the incoming data into the target object
	if err := json.Unmarshal(data, obj); err != nil {
		return fmt.Errorf("failed to unmarshal %s %s/%s: %w", key.Kind, key.Namespace, key.Name, err)
	}

	return a.createOrUpdate(ctx, key, obj)
}

// applyConfigMap handles create, update, and delete for ConfigMap resources.
func (a *FederationResourceApplier) applyConfigMap(ctx context.Context, key federation.ResourceKey, changeType federation.ChangeType, data []byte) error {
	obj := &corev1.ConfigMap{}
	if changeType == federation.ChangeTypeDeleted {
		return a.deleteResource(ctx, key, obj)
	}

	if err := json.Unmarshal(data, obj); err != nil {
		return fmt.Errorf("failed to unmarshal ConfigMap %s/%s: %w", key.Namespace, key.Name, err)
	}

	return a.createOrUpdate(ctx, key, obj)
}

// applySecret handles create, update, and delete for Secret resources.
func (a *FederationResourceApplier) applySecret(ctx context.Context, key federation.ResourceKey, changeType federation.ChangeType, data []byte) error {
	obj := &corev1.Secret{}
	if changeType == federation.ChangeTypeDeleted {
		return a.deleteResource(ctx, key, obj)
	}

	if err := json.Unmarshal(data, obj); err != nil {
		return fmt.Errorf("failed to unmarshal Secret %s/%s: %w", key.Namespace, key.Name, err)
	}

	return a.createOrUpdate(ctx, key, obj)
}

// createOrUpdate creates the resource if it does not exist, or updates it if
// it already exists. The FederationOriginLabel is always set to mark the
// resource as federation-managed.
func (a *FederationResourceApplier) createOrUpdate(ctx context.Context, key federation.ResourceKey, obj client.Object) error {
	// Ensure the object has the correct namespace and name for the lookup
	desired, ok := obj.DeepCopyObject().(client.Object)
	if !ok {
		return fmt.Errorf("%w: %s %s/%s", errFailedToDeepCopyDesiredObjectFor, key.Kind, key.Namespace, key.Name)
	}

	// Build a fresh stub for the CreateOrUpdate lookup key
	stub, ok := obj.DeepCopyObject().(client.Object)
	if !ok {
		return fmt.Errorf("%w: %s %s/%s", errFailedToDeepCopyStubObjectFor, key.Kind, key.Namespace, key.Name)
	}
	stub.SetName(key.Name)
	stub.SetNamespace(key.Namespace)
	// Clear resource version so the stub works for both create and get
	stub.SetResourceVersion("")

	result, err := controllerutil.CreateOrUpdate(ctx, a.client, stub, func() error {
		// Copy spec and labels from desired into stub.
		// We use JSON round-trip to transfer the spec fields generically
		// while preserving the server-set metadata on stub.
		specData, marshalErr := json.Marshal(desired)
		if marshalErr != nil {
			return fmt.Errorf("failed to marshal desired object: %w", marshalErr)
		}
		if unmarshalErr := json.Unmarshal(specData, stub); unmarshalErr != nil {
			return fmt.Errorf("failed to unmarshal into stub: %w", unmarshalErr)
		}

		// Restore server-managed fields that must not be overwritten
		stub.SetName(key.Name)
		stub.SetNamespace(key.Namespace)

		// Ensure the federation origin label is present
		labels := stub.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels[FederationOriginLabel] = "true"
		stub.SetLabels(labels)

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create or update %s %s/%s: %w", key.Kind, key.Namespace, key.Name, err)
	}

	a.logger.Info("Applied federated resource",
		zap.String("resource", key.String()),
		zap.String("result", string(result)),
	)
	return nil
}

// deleteResource deletes a resource only if it carries the FederationOriginLabel.
// This prevents accidental deletion of non-federated (locally managed) resources.
func (a *FederationResourceApplier) deleteResource(ctx context.Context, key federation.ResourceKey, obj client.Object) error {
	objKey := types.NamespacedName{
		Namespace: key.Namespace,
		Name:      key.Name,
	}

	if err := a.client.Get(ctx, objKey, obj); err != nil {
		if apierrors.IsNotFound(err) {
			// Already gone, nothing to do
			a.logger.Debug("Resource already deleted",
				zap.String("resource", key.String()),
			)
			return nil
		}
		return fmt.Errorf("failed to get %s %s/%s for deletion: %w", key.Kind, key.Namespace, key.Name, err)
	}

	// Only delete resources that were created by federation
	labels := obj.GetLabels()
	if labels[FederationOriginLabel] == "" {
		a.logger.Warn("Refusing to delete non-federated resource",
			zap.String("resource", key.String()),
		)
		return nil
	}

	if err := a.client.Delete(ctx, obj, &client.DeleteOptions{
		Preconditions: &metav1.Preconditions{
			UID: uidPtr(obj.GetUID()),
		},
	}); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to delete %s %s/%s: %w", key.Kind, key.Namespace, key.Name, err)
	}

	a.logger.Info("Deleted federated resource",
		zap.String("resource", key.String()),
	)
	return nil
}

// uidPtr returns a pointer to a UID value
func uidPtr(uid types.UID) *types.UID {
	return &uid
}
