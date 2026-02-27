package cmd

import (
	"context"
	"fmt"

	"github.com/piwi3910/novaedge/cmd/novactl/pkg/client"
	"github.com/spf13/cobra"
)

func newDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete [resource-type] [name]",
		Short: "Delete a NovaEdge resource",
		Long:  `Delete a specific NovaEdge resource.`,
		Example: `  # Delete a gateway
  novactl delete gateway external-gateway

  # Delete a route in a specific namespace
  novactl delete route api-route -n production`,
		RunE: runDelete,
	}

	return cmd
}

func runDelete(_ *cobra.Command, args []string) error {
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
	case resourceAliasGRPCRoutes, resourceAliasGRPCRoute:
		rt = client.ResourceGRPCRoute
	default:
		return fmt.Errorf("%w: %s", errUnknownResourceType, resourceType)
	}

	ctx := context.Background()

	// Create client
	c, err := client.NewClient(config)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Delete resource
	if err := c.DeleteResource(ctx, rt, namespace, name); err != nil {
		return fmt.Errorf("failed to delete %s/%s: %w", resourceType, name, err)
	}

	fmt.Printf("%s \"%s\" deleted\n", resourceType, name)
	return nil
}
