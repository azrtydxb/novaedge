package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/piwi3910/novaedge/cmd/novactl/pkg/client"
	"github.com/piwi3910/novaedge/cmd/novactl/pkg/printer"
	"github.com/spf13/cobra"
)

var outputFormat string

func newGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get [resource-type]",
		Short: "Get NovaEdge resources",
		Long:  `Get and display NovaEdge resources like gateways, routes, backends, policies, and vips.`,
		Example: `  # List all gateways
  novactl get gateways

  # List all routes in a specific namespace
  novactl get routes -n production

  # Get backends with JSON output
  novactl get backends -o json`,
		RunE: runGet,
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "Output format (table|json|yaml|wide)")

	return cmd
}

func runGet(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("exactly one resource type required")
	}

	resourceType := args[0]

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
		return fmt.Errorf("unknown resource type: %s (valid types: gateways, routes, backends, policies, vips)", resourceType)
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
