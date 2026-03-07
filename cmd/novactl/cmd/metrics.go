package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/azrtydxb/novaedge/cmd/novactl/pkg/client"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func newMetricsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "View NovaEdge metrics",
		Long:  `Display metrics for agents, backends, and VIPs.`,
	}

	cmd.AddCommand(newMetricsAgentCommand())
	cmd.AddCommand(newMetricsBackendsCommand())
	cmd.AddCommand(newMetricsQueryCommand())
	cmd.AddCommand(newMetricsTopBackendsCommand())
	cmd.AddCommand(newMetricsTopRoutesCommand())
	cmd.AddCommand(newMetricsDashboardCommand())

	return cmd
}

func newMetricsAgentCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent [node-name]",
		Short: "Show metrics for a specific agent",
		Long:  `Display request counts, latencies, and other metrics for an agent on a node.`,
		Example: `  # Show metrics for agent on a node
  novactl metrics agent worker-1`,
		RunE: runMetricsAgent,
	}

	return cmd
}

func runMetricsAgent(_ *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errExactlyOneArgumentRequiredNodeName
	}

	nodeName := args[0]
	fmt.Printf("Metrics for agent on node: %s\n\n", nodeName)
	fmt.Println("(Metrics retrieval requires connection to Prometheus endpoint)")
	fmt.Println("This feature requires implementing Prometheus query API client.")
	fmt.Println()
	fmt.Println("Example metrics to query:")
	fmt.Println("  - novaedge_agent_requests_total{node=\"" + nodeName + "\"}")
	fmt.Println("  - novaedge_agent_request_duration_seconds{node=\"" + nodeName + "\"}")
	fmt.Println("  - novaedge_agent_active_connections{node=\"" + nodeName + "\"}")

	return nil
}

func newMetricsBackendsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backends",
		Short: "Show health metrics for all backends",
		Long:  `Display health status and endpoint counts for all backends.`,
		Example: `  # Show backend health metrics
  novactl metrics backends`,
		RunE: runMetricsBackends,
	}

	return cmd
}

func runMetricsBackends(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	// Create client
	c, err := client.NewClient(config)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// List all backends
	backends, err := c.ListResources(ctx, client.ResourceBackend, namespace)
	if err != nil {
		return fmt.Errorf("failed to list backends: %w", err)
	}

	if len(backends.Items) == 0 {
		fmt.Println("No backends found.")
		return nil
	}

	fmt.Printf("Backend Health Metrics:\n\n")

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tTOTAL\tHEALTHY\tUNHEALTHY\tHEALTH %")

	for _, backend := range backends.Items {
		name := backend.GetName()

		status, _, _ := unstructured.NestedMap(backend.Object, "status")
		endpoints, _, _ := unstructured.NestedSlice(status, "endpoints")
		total := len(endpoints)

		healthy := 0
		for _, ep := range endpoints {
			epMap, ok := ep.(map[string]interface{})
			if !ok {
				continue
			}
			isHealthy, _, _ := unstructured.NestedBool(epMap, "healthy")
			if isHealthy {
				healthy++
			}
		}

		unhealthy := total - healthy
		healthPct := 0
		if total > 0 {
			healthPct = (healthy * 100) / total
		}

		_, _ = fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d%%\n", name, total, healthy, unhealthy, healthPct)
	}

	_ = w.Flush()
	return nil
}
