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

// Package mode provides mode detection and backend abstraction for the web UI.
package mode

import (
	"context"
	"os"

	"github.com/azrtydxb/novaedge/cmd/novactl/pkg/webui/models"
	"k8s.io/client-go/rest"
)

// Mode represents the operating mode of the web UI
type Mode string

const (
	// ModeKubernetes indicates Kubernetes mode with CRD operations
	ModeKubernetes Mode = "kubernetes"
	// ModeStandalone indicates standalone mode with YAML file operations
	ModeStandalone Mode = "standalone"
)

// Backend defines the interface for configuration backends
type Backend interface {
	// Mode returns the backend mode
	Mode() Mode

	// ReadOnly returns whether the backend is read-only
	ReadOnly() bool

	// ListGateways returns all gateways
	ListGateways(ctx context.Context, namespace string) ([]models.Gateway, error)

	// GetGateway returns a specific gateway
	GetGateway(ctx context.Context, namespace, name string) (*models.Gateway, error)

	// CreateGateway creates a new gateway
	CreateGateway(ctx context.Context, gateway *models.Gateway) (*models.Gateway, error)

	// UpdateGateway updates an existing gateway
	UpdateGateway(ctx context.Context, gateway *models.Gateway) (*models.Gateway, error)

	// DeleteGateway deletes a gateway
	DeleteGateway(ctx context.Context, namespace, name string) error

	// ListRoutes returns all routes
	ListRoutes(ctx context.Context, namespace string) ([]models.Route, error)

	// GetRoute returns a specific route
	GetRoute(ctx context.Context, namespace, name string) (*models.Route, error)

	// CreateRoute creates a new route
	CreateRoute(ctx context.Context, route *models.Route) (*models.Route, error)

	// UpdateRoute updates an existing route
	UpdateRoute(ctx context.Context, route *models.Route) (*models.Route, error)

	// DeleteRoute deletes a route
	DeleteRoute(ctx context.Context, namespace, name string) error

	// ListBackends returns all backends
	ListBackends(ctx context.Context, namespace string) ([]models.Backend, error)

	// GetBackend returns a specific backend
	GetBackend(ctx context.Context, namespace, name string) (*models.Backend, error)

	// CreateBackend creates a new backend
	CreateBackend(ctx context.Context, backend *models.Backend) (*models.Backend, error)

	// UpdateBackend updates an existing backend
	UpdateBackend(ctx context.Context, backend *models.Backend) (*models.Backend, error)

	// DeleteBackend deletes a backend
	DeleteBackend(ctx context.Context, namespace, name string) error

	// ListPolicies returns all policies
	ListPolicies(ctx context.Context, namespace string) ([]models.Policy, error)

	// GetPolicy returns a specific policy
	GetPolicy(ctx context.Context, namespace, name string) (*models.Policy, error)

	// CreatePolicy creates a new policy
	CreatePolicy(ctx context.Context, policy *models.Policy) (*models.Policy, error)

	// UpdatePolicy updates an existing policy
	UpdatePolicy(ctx context.Context, policy *models.Policy) (*models.Policy, error)

	// DeletePolicy deletes a policy
	DeletePolicy(ctx context.Context, namespace, name string) error

	// ListCertificates returns all certificates
	ListCertificates(ctx context.Context, namespace string) ([]models.Certificate, error)

	// GetCertificate returns a specific certificate
	GetCertificate(ctx context.Context, namespace, name string) (*models.Certificate, error)

	// CreateCertificate creates a new certificate
	CreateCertificate(ctx context.Context, cert *models.Certificate) (*models.Certificate, error)

	// UpdateCertificate updates an existing certificate
	UpdateCertificate(ctx context.Context, cert *models.Certificate) (*models.Certificate, error)

	// DeleteCertificate deletes a certificate
	DeleteCertificate(ctx context.Context, namespace, name string) error

	// ListIPPools returns all IP pools
	ListIPPools(ctx context.Context) ([]models.IPPool, error)

	// GetIPPool returns a specific IP pool
	GetIPPool(ctx context.Context, name string) (*models.IPPool, error)

	// CreateIPPool creates a new IP pool
	CreateIPPool(ctx context.Context, pool *models.IPPool) (*models.IPPool, error)

	// UpdateIPPool updates an existing IP pool
	UpdateIPPool(ctx context.Context, pool *models.IPPool) (*models.IPPool, error)

	// DeleteIPPool deletes an IP pool
	DeleteIPPool(ctx context.Context, name string) error

	// ListNovaEdgeClusters returns all NovaEdge clusters
	ListNovaEdgeClusters(ctx context.Context, namespace string) ([]models.NovaEdgeClusterModel, error)

	// GetNovaEdgeCluster returns a specific NovaEdge cluster
	GetNovaEdgeCluster(ctx context.Context, namespace, name string) (*models.NovaEdgeClusterModel, error)

	// CreateNovaEdgeCluster creates a new NovaEdge cluster
	CreateNovaEdgeCluster(ctx context.Context, cluster *models.NovaEdgeClusterModel) (*models.NovaEdgeClusterModel, error)

	// UpdateNovaEdgeCluster updates an existing NovaEdge cluster
	UpdateNovaEdgeCluster(ctx context.Context, cluster *models.NovaEdgeClusterModel) (*models.NovaEdgeClusterModel, error)

	// DeleteNovaEdgeCluster deletes a NovaEdge cluster
	DeleteNovaEdgeCluster(ctx context.Context, namespace, name string) error

	// ListFederations returns all federations
	ListFederations(ctx context.Context, namespace string) ([]models.FederationModel, error)

	// GetFederation returns a specific federation
	GetFederation(ctx context.Context, namespace, name string) (*models.FederationModel, error)

	// CreateFederation creates a new federation
	CreateFederation(ctx context.Context, federation *models.FederationModel) (*models.FederationModel, error)

	// UpdateFederation updates an existing federation
	UpdateFederation(ctx context.Context, federation *models.FederationModel) (*models.FederationModel, error)

	// DeleteFederation deletes a federation
	DeleteFederation(ctx context.Context, namespace, name string) error

	// ListRemoteClusters returns all remote clusters
	ListRemoteClusters(ctx context.Context, namespace string) ([]models.RemoteClusterModel, error)

	// GetRemoteCluster returns a specific remote cluster
	GetRemoteCluster(ctx context.Context, namespace, name string) (*models.RemoteClusterModel, error)

	// CreateRemoteCluster creates a new remote cluster
	CreateRemoteCluster(ctx context.Context, rc *models.RemoteClusterModel) (*models.RemoteClusterModel, error)

	// UpdateRemoteCluster updates an existing remote cluster
	UpdateRemoteCluster(ctx context.Context, rc *models.RemoteClusterModel) (*models.RemoteClusterModel, error)

	// DeleteRemoteCluster deletes a remote cluster
	DeleteRemoteCluster(ctx context.Context, namespace, name string) error

	// ListNamespaces returns available namespaces
	ListNamespaces(ctx context.Context) ([]string, error)

	// ValidateConfig validates the configuration
	ValidateConfig(ctx context.Context, config *models.Config) error

	// ExportConfig exports the full configuration as YAML
	ExportConfig(ctx context.Context, namespace string) ([]byte, error)

	// ImportConfig imports configuration from YAML
	ImportConfig(ctx context.Context, data []byte, dryRun bool) (*models.ImportResult, error)
}

// DetectMode auto-detects the operating mode based on environment
func DetectMode(kubeConfig *rest.Config, standaloneConfigPath string) Mode {
	// If standalone config path is explicitly provided, use standalone mode
	if standaloneConfigPath != "" {
		if _, err := os.Stat(standaloneConfigPath); err == nil {
			return ModeStandalone
		}
	}

	// Check for Kubernetes mode
	if kubeConfig != nil {
		return ModeKubernetes
	}

	// Check if running inside a Kubernetes cluster
	if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/token"); err == nil {
		return ModeKubernetes
	}

	// Default to standalone if a config file exists in common locations
	commonPaths := []string{
		"/etc/novaedge/config.yaml",
		"./config.yaml",
		"./deploy/standalone/config.yaml",
	}
	for _, p := range commonPaths {
		if _, err := os.Stat(p); err == nil {
			return ModeStandalone
		}
	}

	// Default to kubernetes mode (will likely fail without proper kubeconfig)
	return ModeKubernetes
}

// NewBackend creates the appropriate backend based on mode
func NewBackend(mode Mode, kubeConfig *rest.Config, standaloneConfigPath string, readOnly bool) (Backend, error) {
	switch mode {
	case ModeStandalone:
		return NewStandaloneBackend(standaloneConfigPath, readOnly)
	case ModeKubernetes:
		fallthrough
	default:
		return NewKubernetesBackend(kubeConfig, readOnly)
	}
}
