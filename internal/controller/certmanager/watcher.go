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

package certmanager

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	errWatchChannelClosed = errors.New("watch channel closed")
)

// CertificateWatcher watches cert-manager Certificate resources for changes
// and triggers callbacks when certificates become Ready.
type CertificateWatcher struct {
	dynamicClient dynamic.Interface
	onReady       func(namespace, name, secretName string)
	onFailed      func(namespace, name, message string)

	mu      sync.Mutex
	stopCh  chan struct{}
	running bool
}

// NewCertificateWatcher creates a new CertificateWatcher.
func NewCertificateWatcher(dynamicClient dynamic.Interface) *CertificateWatcher {
	return &CertificateWatcher{
		dynamicClient: dynamicClient,
		stopCh:        make(chan struct{}),
	}
}

// OnReady sets the callback for when a Certificate becomes Ready.
func (w *CertificateWatcher) OnReady(fn func(namespace, name, secretName string)) {
	w.onReady = fn
}

// OnFailed sets the callback for when a Certificate fails.
func (w *CertificateWatcher) OnFailed(fn func(namespace, name, message string)) {
	w.onFailed = fn
}

// Start begins watching cert-manager Certificate resources.
func (w *CertificateWatcher) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.stopCh = make(chan struct{})
	w.mu.Unlock()

	go w.run(ctx)
	return nil
}

// Stop stops the watcher.
func (w *CertificateWatcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.running {
		return
	}
	close(w.stopCh)
	w.running = false
}

// run is the main watch loop with reconnection.
func (w *CertificateWatcher) run(ctx context.Context) {
	logger := log.FromContext(ctx).WithName("cert-manager-watcher")

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		default:
		}

		if err := w.watch(ctx); err != nil {
			logger.Error(err, "Watch error, reconnecting")
			select {
			case <-ctx.Done():
				return
			case <-w.stopCh:
				return
			case <-time.After(5 * time.Second):
				// Reconnect after backoff
			}
		}
	}
}

// watch creates and processes a single watch session.
func (w *CertificateWatcher) watch(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("cert-manager-watcher")

	gvr := schema.GroupVersionResource{
		Group:    "cert-manager.io",
		Version:  "v1",
		Resource: "certificates",
	}

	// Watch only certificates managed by NovaEdge
	watcher, err := w.dynamicClient.Resource(gvr).Namespace("").Watch(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/managed-by=novaedge",
	})
	if err != nil {
		return fmt.Errorf("failed to start watch: %w", err)
	}
	defer watcher.Stop()

	logger.Info("Watching cert-manager Certificate resources")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.stopCh:
			return nil
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return errWatchChannelClosed
			}
			if event.Type == watch.Modified || event.Type == watch.Added {
				w.handleCertificateEvent(ctx, event.Object)
			}
		}
	}
}

// handleCertificateEvent processes a Certificate watch event.
func (w *CertificateWatcher) handleCertificateEvent(ctx context.Context, obj interface{}) {
	logger := log.FromContext(ctx).WithName("cert-manager-watcher")

	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return
	}

	name := u.GetName()
	namespace := u.GetNamespace()

	ready, message, err := extractReadyCondition(u)
	if err != nil {
		logger.V(1).Info("Could not extract ready condition", "name", name, "error", err)
		return
	}

	if ready {
		secretName, _, _ := unstructured.NestedString(u.Object, "spec", "secretName")
		logger.Info("Certificate is Ready",
			"name", name,
			"namespace", namespace,
			"secretName", secretName)
		if w.onReady != nil {
			w.onReady(namespace, name, secretName)
		}
	} else if message != "" && isCertFailed(u) {
		logger.Info("Certificate failed",
			"name", name,
			"namespace", namespace,
			"message", message)
		if w.onFailed != nil {
			w.onFailed(namespace, name, message)
		}
	}
}

// isCertFailed checks if the certificate has an Issuing=False condition indicating failure.
func isCertFailed(cert *unstructured.Unstructured) bool {
	conditions, found, err := unstructured.NestedSlice(cert.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}

	for _, c := range conditions {
		condMap, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _, _ := unstructured.NestedString(condMap, "type")
		status, _, _ := unstructured.NestedString(condMap, "status")
		if condType == "Issuing" && status == "False" {
			return true
		}
	}
	return false
}
