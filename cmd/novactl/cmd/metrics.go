package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/piwi3910/novaedge/cmd/novactl/pkg/client"
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
	cmd.AddCommand(newMetricsVIPsCommand())

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

func runMetricsAgent(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("exactly one argument required: node-name")
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

func runMetricsBackends(cmd *cobra.Command, args []string) error {
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
	fmt.Fprintln(w, "NAME\tTOTAL\tHEALTHY\tUNHEALTHY\tHEALTH %")

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

		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d%%\n", name, total, healthy, unhealthy, healthPct)
	}

	w.Flush()
	return nil
}

func newMetricsVIPsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vips",
		Short: "Show VIP status across nodes",
		Long:  `Display VIP assignments and status across all nodes.`,
		Example: `  # Show VIP metrics
  novactl metrics vips`,
		RunE: runMetricsVIPs,
	}

	return cmd
}

func runMetricsVIPs(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Create client
	c, err := client.NewClient(config)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// List all VIPs
	vips, err := c.ListResources(ctx, client.ResourceVIP, namespace)
	if err != nil {
		return fmt.Errorf("failed to list VIPs: %w", err)
	}

	if len(vips.Items) == 0 {
		fmt.Println("No VIPs found.")
		return nil
	}

	fmt.Printf("VIP Status:\n\n")

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tIP\tMODE\tASSIGNED NODE\tSTATUS\tLAST TRANSITION")

	for _, vip := range vips.Items {
		name := vip.GetName()

		spec, _, _ := unstructured.NestedMap(vip.Object, "spec")
		ip, _, _ := unstructured.NestedString(spec, "ip")
		mode, _, _ := unstructured.NestedString(spec, "mode")

		status, _, _ := unstructured.NestedMap(vip.Object, "status")
		assignedNode, _, _ := unstructured.NestedString(status, "assignedNode")
		vipStatus, _, _ := unstructured.NestedString(status, "status")
		lastTransition, _, _ := unstructured.NestedString(status, "lastTransitionTime")

		if assignedNode == "" {
			assignedNode = "-"
		}
		if vipStatus == "" {
			vipStatus = "Unknown"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			name, ip, mode, assignedNode, vipStatus, lastTransition)
	}

	w.Flush()
	return nil
}
