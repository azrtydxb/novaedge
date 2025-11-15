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
		Long:  `Show detailed information about a specific NovaEdge resource.`,
		Example: `  # Describe a gateway
  novactl describe gateway external-gateway

  # Describe a route in a specific namespace
  novactl describe route api-route -n production`,
		RunE: runDescribe,
	}

	return cmd
}

func runDescribe(cmd *cobra.Command, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("exactly two arguments required: resource-type and name")
	}

	resourceType := args[0]
	name := args[1]

	// Map user-friendly names to resource types
	var rt client.ResourceType
	switch resourceType {
	case "gateways", "gateway", "gw":
		rt = client.ResourceGateway
	case "routes", "route", "rt":
		rt = client.ResourceRoute
	case "backends", "backend", "be":
		rt = client.ResourceBackend
	case "policies", "policy", "pol":
		rt = client.ResourcePolicy
	case "vips", "vip":
		rt = client.ResourceVIP
	default:
		return fmt.Errorf("unknown resource type: %s", resourceType)
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
