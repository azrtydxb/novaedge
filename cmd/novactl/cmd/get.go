package cmd

import (
	"errors"
	"context"
	"fmt"
	"os"

	"github.com/piwi3910/novaedge/cmd/novactl/pkg/client"
	"github.com/piwi3910/novaedge/cmd/novactl/pkg/printer"
	"github.com/spf13/cobra"
)
var (
	errExactlyOneResourceTypeRequired = errors.New("exactly one resource type required")
)


var outputFormat string

func newGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get [resource-type]",
		Short: "Get NovaEdge resources",
		Long:  `Get and display NovaEdge resources like gateways, routes, backends, policies, vips, and grpcroutes.`,
		Example: `  # List all gateways
  novactl get gateways

  # List all routes in a specific namespace
  novactl get routes -n production

  # Get backends with JSON output
  novactl get backends -o json

  # List all gRPC routes
  novactl get grpcroutes`,
		RunE: runGet,
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "Output format (table|json|yaml|wide)")

	return cmd
}

func runGet(_ *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errExactlyOneResourceTypeRequired
	}

	resourceType := args[0]

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
		// WASM plugins are shown via policies with type WASMPlugin
		rt = client.ResourcePolicy
	case resourceAliasCertificates, resourceAliasCertificate, resourceAliasCert, resourceAliasProxyCertificates:
		rt = client.ResourceCertificate
	default:
		return fmt.Errorf("%w: %s (valid types: gateways, routes, backends, policies, vips, tcproutes, tlsroutes, grpcroutes, ippools, wasmplugins, certificates)", errUnknownResourceType, resourceType)
	}

	ctx := context.Background()

	// Create client
	c, err := client.NewClient(config)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// List resources
	list, err := c.ListResources(ctx, rt, namespace)
	if err != nil {
		return fmt.Errorf("failed to list %s: %w", resourceType, err)
	}

	// Print resources
	format := printer.OutputFormat(outputFormat)
	p := printer.NewPrinter(format, os.Stdout)
	return p.PrintResourceList(resourceType, list.Items)
}
