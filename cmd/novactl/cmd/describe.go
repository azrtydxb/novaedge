package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/piwi3910/novaedge/cmd/novactl/pkg/client"
	"github.com/piwi3910/novaedge/cmd/novactl/pkg/printer"
	"github.com/spf13/cobra"
)

func newDescribeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "describe [resource-type] [name]",
		Short: "Describe a NovaEdge resource",
		Long: `Show detailed information about a specific NovaEdge resource.

For ProxyGateway resources, shows HTTP/3 status, SSE config, mTLS client
authentication mode, PROXY protocol configuration, and OCSP stapling status
per listener.

For ProxyBackend resources, shows upstream PROXY protocol configuration.`,
		Example: `  # Describe a gateway (includes HTTP/3 status, SSE config, mTLS, PROXY protocol)
  novactl describe gateway external-gateway

  # Describe a route in a specific namespace
  novactl describe route api-route -n production

  # Describe a gRPC route
  novactl describe grpcroute my-grpc-service`,
		RunE: runDescribe,
	}

	return cmd
}

func runDescribe(_ *cobra.Command, args []string) error {
	if len(args) != 2 {
		return errExactlyTwoArgumentsRequiredResourceTypeAndName
	}

	resourceType := args[0]
	name := args[1]

	// Map user-friendly names to resource types
	var rt client.ResourceType
	switch resourceType {
	case resourceAliasGateways, resourceAliasGateway, "gw":
		rt = client.ResourceGateway
	case resourceAliasRoutes, resourceAliasRoute, "rt":
		rt = client.ResourceRoute
	case resourceAliasBackends, resourceAliasBackend, "be":
		rt = client.ResourceBackend
	case resourceAliasPolicies, resourceAliasPolicy, resourceAliasPol:
		rt = client.ResourcePolicy
	case resourceAliasVIPs, resourceAliasVIP:
		rt = client.ResourceVIP
	case resourceAliasTCPRoutes, resourceAliasTCPRoute:
		rt = client.ResourceTCPRoute
	case resourceAliasTLSRoutes, resourceAliasTLSRoute:
		rt = client.ResourceTLSRoute
	case resourceAliasGRPCRoutes, resourceAliasGRPCRoute:
		rt = client.ResourceGRPCRoute
	case resourceAliasIPPools, resourceAliasIPPool:
		rt = client.ResourceIPPool
	case resourceAliasWASMPlugins, resourceAliasWASMPlugin, resourceAliasWASM:
		// WASM plugins are shown via policies
		rt = client.ResourcePolicy
	case resourceAliasCertificates, resourceAliasCertificate, resourceAliasCert, resourceAliasProxyCertificates:
		rt = client.ResourceCertificate
	default:
		return fmt.Errorf("%w: %s", errUnknownResourceType, resourceType)
	}

	ctx := context.Background()

	// Create client
	c, err := client.NewClient(config)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Get resource
	resource, err := c.GetResource(ctx, rt, namespace, name)
	if err != nil {
		return fmt.Errorf("failed to get %s/%s: %w", resourceType, name, err)
	}

	// Print resource as YAML
	p := printer.NewPrinter(printer.OutputYAML, os.Stdout)
	return p.PrintResource(*resource)
}
