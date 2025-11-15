// Package client provides API clients for interacting with NovaEdge resources.
package client

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Client wraps Kubernetes clients for NovaEdge operations
type Client struct {
	Dynamic   dynamic.Interface
	Clientset kubernetes.Interface
	Config    *rest.Config
}

// NewClient creates a new NovaEdge client
func NewClient(config *rest.Config) (*Client, error) {
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	return &Client{
		Dynamic:   dynamicClient,
		Clientset: clientset,
		Config:    config,
	}, nil
}

// ResourceType represents a NovaEdge resource type
type ResourceType string

const (
	ResourceGateway ResourceType = "gateways"
	ResourceRoute   ResourceType = "routes"
	ResourceBackend ResourceType = "backends"
	ResourcePolicy  ResourceType = "policies"
	ResourceVIP     ResourceType = "vips"
)

// GetGVR returns the GroupVersionResource for a given resource type
func GetGVR(resourceType ResourceType) schema.GroupVersionResource {
	group := "novaedge.piwi3910.com"
	version := "v1alpha1"

	var resource string
	switch resourceType {
	case ResourceGateway:
		resource = "proxygateways"
	case ResourceRoute:
		resource = "proxyroutes"
	case ResourceBackend:
		resource = "proxybackends"
	case ResourcePolicy:
		resource = "proxypolicies"
	case ResourceVIP:
		resource = "proxyvips"
	default:
		resource = string(resourceType)
	}

	return schema.GroupVersionResource{
		Group:    group,
		Version:  version,
		Resource: resource,
	}
}

// ListResources lists resources of a given type
func (c *Client) ListResources(ctx context.Context, resourceType ResourceType, namespace string) (*unstructured.UnstructuredList, error) {
	gvr := GetGVR(resourceType)
	return c.Dynamic.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
}

// GetResource gets a specific resource
func (c *Client) GetResource(ctx context.Context, resourceType ResourceType, namespace, name string) (*unstructured.Unstructured, error) {
	gvr := GetGVR(resourceType)
	return c.Dynamic.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
}

// DeleteResource deletes a specific resource
func (c *Client) DeleteResource(ctx context.Context, resourceType ResourceType, namespace, name string) error {
	gvr := GetGVR(resourceType)
	return c.Dynamic.Resource(gvr).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// ApplyResource applies a resource from unstructured data
func (c *Client) ApplyResource(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	gvk := obj.GroupVersionKind()
	gvr := schema.GroupVersionResource{
		Group:    gvk.Group,
		Version:  gvk.Version,
		Resource: gvk.Kind + "s", // Simple pluralization
	}

	namespace := obj.GetNamespace()
	if namespace == "" {
		namespace = "default"
	}

	// Try to get the resource first to determine if we need to create or update
	existing, err := c.Dynamic.Resource(gvr).Namespace(namespace).Get(ctx, obj.GetName(), metav1.GetOptions{})
	if err == nil {
		// Resource exists, update it
		obj.SetResourceVersion(existing.GetResourceVersion())
		return c.Dynamic.Resource(gvr).Namespace(namespace).Update(ctx, obj, metav1.UpdateOptions{})
	}

	// Resource doesn't exist, create it
	return c.Dynamic.Resource(gvr).Namespace(namespace).Create(ctx, obj, metav1.CreateOptions{})
}
