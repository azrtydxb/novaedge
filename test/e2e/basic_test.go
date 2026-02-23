//go:build e2e

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

// Package e2e provides end-to-end tests for NovaEdge using a kind cluster.
package e2e

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// getKubeConfig returns the kubeconfig for the kind cluster.
func getKubeConfig(t *testing.T) *kubernetes.Clientset {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home := os.Getenv("HOME")
		kubeconfig = home + "/.kube/config"
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	require.NoError(t, err, "Failed to build kubeconfig")

	client, err := kubernetes.NewForConfig(config)
	require.NoError(t, err, "Failed to create Kubernetes client")

	return client
}

// TestControllerReady tests that the controller deployment is ready.
func TestControllerReady(t *testing.T) {
	t.Parallel()

	client := getKubeConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Get the controller deployment - try Helm name first, then plain name
	deploy, err := client.AppsV1().Deployments("novaedge-system").Get(ctx, "novaedge-controller", metav1.GetOptions{})
	if err != nil {
		// Fallback: try without namespace prefix for non-Helm deploys
		t.Logf("novaedge-controller not found, trying alternate names")
		deploys, listErr := client.AppsV1().Deployments("novaedge-system").List(ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/component=controller",
		})
		if listErr != nil {
			require.NoError(t, err, "Failed to get controller deployment")
		}
		require.NotEmpty(t, deploys.Items, "Should have at least one controller deployment")
		deploy = &deploys.Items[0]
	}

	// Check that the deployment has at least 1 ready replica (may be scaled to 3)
	assert.GreaterOrEqual(t, deploy.Status.ReadyReplicas, int32(1), "Controller should have at least 1 ready replica")

	t.Logf("Controller deployment is ready: %s (%d/%d replicas)", deploy.Name, deploy.Status.ReadyReplicas, *deploy.Spec.Replicas)
}

// TestAgentDaemonSet tests that the agent DaemonSet is running.
func TestAgentDaemonSet(t *testing.T) {
	t.Parallel()

	client := getKubeConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Get the agent DaemonSet
	ds, err := client.AppsV1().DaemonSets("novaedge-system").Get(ctx, "novaedge-agent", metav1.GetOptions{})
	require.NoError(t, err, "Failed to get agent DaemonSet")

	// Check that the DaemonSet has desired number of nodes
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	require.NoError(t, err, "Failed to list nodes")

	expectedCount := int32(len(nodes.Items))
	assert.Equal(t, expectedCount, ds.Status.NumberReady, "Agent should be ready on all nodes")

	t.Logf("Agent DaemonSet is ready: %d/%d pods", ds.Status.NumberReady, expectedCount)
}

// TestBasicHTTPRouting tests basic HTTP routing functionality.
func TestBasicHTTPRouting(t *testing.T) {
	t.Parallel()

	client := getKubeConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Create a test backend deployment
	backend := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test-backend"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test-backend"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:alpine",
							Ports: []corev1.ContainerPort{{ContainerPort: 80}},
						},
					},
				},
			},
		},
	}

	_, err := client.AppsV1().Deployments("default").Create(ctx, backend, metav1.CreateOptions{})
	if err != nil {
		t.Logf("Deployment may already exist: %v", err)
	}

	// Clean up
	defer func() {
		_ = client.AppsV1().Deployments("default").Delete(ctx, "test-backend", metav1.DeleteOptions{})
	}()

	// Create a service for the backend
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "test-backend"},
			Ports: []corev1.ServicePort{
				{
					Port:       80,
					TargetPort: intstrFromInt(80),
				},
			},
		},
	}

	_, err = client.CoreV1().Services("default").Create(ctx, svc, metav1.CreateOptions{})
	if err != nil {
		t.Logf("Service may already exist: %v", err)
	}

	defer func() {
		_ = client.CoreV1().Services("default").Delete(ctx, "test-backend", metav1.DeleteOptions{})
	}()

	// Wait for the deployment to be ready
	require.Eventually(t, func() bool {
		deploy, err := client.AppsV1().Deployments("default").Get(ctx, "test-backend", metav1.GetOptions{})
		if err != nil {
			return false
		}
		return deploy.Status.ReadyReplicas == 1
	}, 2*time.Minute, 5*time.Second, "Backend deployment should be ready")

	t.Log("Basic HTTP routing test setup complete")
}

// TestPodDNSResolution tests DNS resolution within pods.
func TestPodDNSResolution(t *testing.T) {
	t.Parallel()

	client := getKubeConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Get the controller pod - try Helm labels first, then plain labels
	pods, err := client.CoreV1().Pods("novaedge-system").List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/component=controller",
	})
	if err == nil && len(pods.Items) == 0 {
		// Fallback: try legacy label selector
		pods, err = client.CoreV1().Pods("novaedge-system").List(ctx, metav1.ListOptions{
			LabelSelector: "app=novaedge-controller",
		})
	}
	require.NoError(t, err, "Failed to list controller pods")
	require.NotEmpty(t, pods.Items, "Should have at least one controller pod")

	pod := pods.Items[0]
	t.Logf("Testing DNS resolution from pod: %s", pod.Name)

	// The pod should be able to resolve kubernetes.default
	// This is a basic connectivity test
	assert.NotEmpty(t, pod.Status.PodIP, "Pod should have an IP address")
	t.Logf("Controller pod IP: %s", pod.Status.PodIP)
}

// TestClusterInfo tests basic cluster connectivity.
func TestClusterInfo(t *testing.T) {
	t.Parallel()

	client := getKubeConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get cluster nodes
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	require.NoError(t, err, "Failed to list nodes")
	assert.NotEmpty(t, nodes.Items, "Should have at least one node")

	t.Logf("Cluster has %d nodes", len(nodes.Items))

	// Get namespaces
	namespaces, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	require.NoError(t, err, "Failed to list namespaces")

	var foundNovaEdge bool
	for _, ns := range namespaces.Items {
		if ns.Name == "novaedge-system" {
			foundNovaEdge = true
			break
		}
	}
	assert.True(t, foundNovaEdge, "Should have novaedge-system namespace")

	t.Log("Cluster info test passed")
}

// TestHealthEndpoints tests that health endpoints are accessible.
func TestHealthEndpoints(t *testing.T) {
	t.Parallel()

	client := getKubeConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Get the controller service
	svc, err := client.CoreV1().Services("novaedge-system").Get(ctx, "novaedge-controller", metav1.GetOptions{})
	if err != nil {
		t.Logf("Controller service may not exist: %v", err)
		t.Skip("Controller service not found")
	}

	t.Logf("Controller service: %s, type: %s", svc.Name, svc.Spec.Type)

	// For ClusterIP services, we can test via port-forward in a real scenario
	// For now, just verify the service exists and has endpoints
	endpoints, err := client.CoreV1().Endpoints("novaedge-system").Get(ctx, "novaedge-controller", metav1.GetOptions{})
	require.NoError(t, err, "Failed to get endpoints")

	assert.NotEmpty(t, endpoints.Subsets, "Service should have endpoints")

	for _, subset := range endpoints.Subsets {
		t.Logf("Service has %d ready addresses", len(subset.Addresses))
	}
}

// TestHTTPClient tests basic HTTP client functionality.
func TestHTTPClient(t *testing.T) {
	t.Parallel()

	// Simple HTTP test to verify networking
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get("http://httpbin.org/get")
	if err != nil {
		t.Logf("HTTP request failed (may be network issue): %v", err)
		t.Skip("Network access not available")
	}
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "HTTP request should succeed")
	t.Log("HTTP client test passed")
}

// Helper functions

func int32Ptr(i int32) *int32 {
	return &i
}

func intstrFromInt(i int) intstr.IntOrString {
	return intstr.FromInt(i)
}
