package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/azrtydxb/novaedge/cmd/novactl/pkg/client"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var (
	errExactlyOneArgumentRequiredHostname    = errors.New("exactly one argument required: hostname")
	errExactlyOneArgumentRequiredBackendName = errors.New("exactly one argument required: backend-name")
)

func newDebugCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug",
		Short: "Debug NovaEdge routing and backends",
		Long:  `Troubleshoot routing issues and inspect backend health.`,
	}

	cmd.AddCommand(newDebugRoutesCommand())
	cmd.AddCommand(newDebugBackendsCommand())

	return cmd
}

func newDebugRoutesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "routes [hostname]",
		Short: "Show routing information for a hostname",
		Long:  `Display all routes that match a given hostname and their configuration.`,
		Example: `  # Show routes for a hostname
  novactl debug routes api.example.com`,
		RunE: runDebugRoutes,
	}

	return cmd
}

func runDebugRoutes(_ *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errExactlyOneArgumentRequiredHostname
	}

	hostname := args[0]
	ctx := context.Background()

	// Create client
	c, err := client.NewClient(config)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// List all routes
	routes, err := c.ListResources(ctx, client.ResourceRoute, namespace)
	if err != nil {
		return fmt.Errorf("failed to list routes: %w", err)
	}

	// Find matching routes
	var matchingRoutes []unstructured.Unstructured
	for _, route := range routes.Items {
		spec, _, _ := unstructured.NestedMap(route.Object, "spec")
		hostnames, _, _ := unstructured.NestedStringSlice(spec, "hostnames")

		for _, h := range hostnames {
			if h == hostname || h == "*" {
				matchingRoutes = append(matchingRoutes, route)
				break
			}
		}
	}

	if len(matchingRoutes) == 0 {
		fmt.Printf("No routes found matching hostname: %s\n", hostname)
		return nil
	}

	fmt.Printf("Routes matching hostname '%s':\n\n", hostname)

	for i, route := range matchingRoutes {
		if i > 0 {
			fmt.Println("---")
		}

		name := route.GetName()
		spec, _, _ := unstructured.NestedMap(route.Object, "spec")

		fmt.Printf("Name: %s\n", name)
		fmt.Printf("Namespace: %s\n", route.GetNamespace())

		hostnames, _, _ := unstructured.NestedStringSlice(spec, "hostnames")
		fmt.Printf("Hostnames: %v\n", hostnames)

		// Print match rules
		matches, _, _ := unstructured.NestedSlice(spec, "matches")
		if len(matches) > 0 {
			fmt.Printf("Matches:\n")
			for _, match := range matches {
				m, ok := match.(map[string]any)
				if !ok {
					continue
				}
				if path, found, _ := unstructured.NestedString(m, "path", "value"); found {
					pathType, _, _ := unstructured.NestedString(m, "path", "type")
					fmt.Printf("  - Path: %s (%s)\n", path, pathType)
				}
				if method, found, _ := unstructured.NestedString(m, "method"); found {
					fmt.Printf("  - Method: %s\n", method)
				}
			}
		}

		// Print backends
		backends, _, _ := unstructured.NestedSlice(spec, "backends")
		if len(backends) > 0 {
			fmt.Printf("Backends:\n")
			for _, backend := range backends {
				b, ok := backend.(map[string]any)
				if !ok {
					continue
				}
				backendName, _, _ := unstructured.NestedString(b, "name")
				weight, _, _ := unstructured.NestedInt64(b, "weight")
				fmt.Printf("  - %s (weight: %d)\n", backendName, weight)
			}
		}

		fmt.Println()
	}

	return nil
}

func newDebugBackendsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backends [name]",
		Short: "Show backend endpoints and health status",
		Long:  `Display detailed information about a backend including all endpoints and their health.`,
		Example: `  # Show backend details
  novactl debug backends api-backend`,
		RunE: runDebugBackends,
	}

	return cmd
}

func runDebugBackends(_ *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errExactlyOneArgumentRequiredBackendName
	}

	backendName := args[0]
	ctx := context.Background()

	// Create client
	c, err := client.NewClient(config)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Get backend
	backend, err := c.GetResource(ctx, client.ResourceBackend, namespace, backendName)
	if err != nil {
		return fmt.Errorf("failed to get backend: %w", err)
	}

	// Print backend details
	fmt.Printf("Backend: %s\n", backendName)
	fmt.Printf("Namespace: %s\n", backend.GetNamespace())

	spec, _, _ := unstructured.NestedMap(backend.Object, "spec")
	serviceRef, _, _ := unstructured.NestedMap(spec, "service")
	if len(serviceRef) > 0 {
		serviceName, _, _ := unstructured.NestedString(serviceRef, "name")
		servicePort, _, _ := unstructured.NestedInt64(serviceRef, "port")
		fmt.Printf("Service: %s:%d\n", serviceName, servicePort)
	}

	// Print health check config
	healthCheck, found, _ := unstructured.NestedMap(spec, "healthCheck")
	if found {
		fmt.Printf("\nHealth Check:\n")
		path, _, _ := unstructured.NestedString(healthCheck, "path")
		interval, _, _ := unstructured.NestedString(healthCheck, "interval")
		timeout, _, _ := unstructured.NestedString(healthCheck, "timeout")
		fmt.Printf("  Path: %s\n", path)
		fmt.Printf("  Interval: %s\n", interval)
		fmt.Printf("  Timeout: %s\n", timeout)
	}

	// Print endpoints
	status, _, _ := unstructured.NestedMap(backend.Object, "status")
	endpoints, _, _ := unstructured.NestedSlice(status, "endpoints")

	if len(endpoints) > 0 {
		fmt.Printf("\nEndpoints (%d):\n", len(endpoints))
		w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "ADDRESS\tPORT\tHEALTHY\tLAST CHECK")

		for _, ep := range endpoints {
			epMap, ok := ep.(map[string]any)
			if !ok {
				continue
			}

			address, _, _ := unstructured.NestedString(epMap, "address")
			port, _, _ := unstructured.NestedInt64(epMap, "port")
			healthy, _, _ := unstructured.NestedBool(epMap, "healthy")
			lastCheck, _, _ := unstructured.NestedString(epMap, "lastHealthCheck")

			healthStatus := statusNo
			if healthy {
				healthStatus = statusYes
			}

			_, _ = fmt.Fprintf(w, "%s\t%d\t%s\t%s\n", address, port, healthStatus, lastCheck)
		}
		_ = w.Flush()
	} else {
		fmt.Println("\nNo endpoints available")
	}

	return nil
}
