package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/piwi3910/novaedge/cmd/novactl/pkg/client"
	grpcClient "github.com/piwi3910/novaedge/cmd/novactl/pkg/grpc"
	"github.com/spf13/cobra"
)

const (
	defaultAgentNamespace = "novaedge-system"
)

var (
	agentNamespace string
)

func newAgentQueryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Query NovaEdge agent directly via gRPC",
		Long: `Connect to a NovaEdge agent via gRPC to query its current configuration,
backend health status, and active VIPs.`,
	}

	cmd.PersistentFlags().StringVar(&agentNamespace, "agent-namespace", defaultAgentNamespace,
		"Namespace where NovaEdge agents are running")

	cmd.AddCommand(newAgentConfigCommand())
	cmd.AddCommand(newAgentBackendsCommand())
	cmd.AddCommand(newAgentVIPsCommand())

	return cmd
}

func newAgentConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config <node-name>",
		Short: "Get agent configuration from a specific node",
		Long: `Query the NovaEdge agent running on a specific node to retrieve
its current configuration snapshot including gateways, routes, clusters, and VIP assignments.`,
		Example: `  # Get configuration from agent on worker-1
  novactl agent config worker-1

  # Get configuration from agent in custom namespace
  novactl agent config worker-1 --agent-namespace custom-namespace`,
		Args: cobra.ExactArgs(1),
		RunE: runAgentConfig,
	}

	return cmd
}

func runAgentConfig(cmd *cobra.Command, args []string) error {
	nodeName := args[0]
	ctx := context.Background()

	// Create Kubernetes client
	c, err := client.NewClient(config)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Find the agent pod on this node
	podName, err := grpcClient.FindAgentPod(ctx, c.Clientset, agentNamespace, nodeName)
	if err != nil {
		return fmt.Errorf("failed to find agent pod: %w", err)
	}

	fmt.Printf("Querying agent pod: %s (node: %s)\n\n", podName, nodeName)

	// Create gRPC client
	agentClient, err := grpcClient.NewAgentClient(ctx, c.Clientset, agentNamespace, podName)
	if err != nil {
		return fmt.Errorf("failed to create agent client: %w", err)
	}
	defer agentClient.Close()

	// Get configuration
	agentConfig, err := agentClient.GetConfig(ctx)
	if err != nil {
		fmt.Println("Note: Direct agent configuration querying requires implementing additional gRPC methods.")
		fmt.Println()
		fmt.Println("Current Implementation Status:")
		fmt.Println("  The agent gRPC service currently provides:")
		fmt.Println("    - StreamConfig: For receiving config updates from controller")
		fmt.Println("    - ReportStatus: For sending status to controller")
		fmt.Println()
		fmt.Println("  To enable 'novactl agent config', the following is needed:")
		fmt.Println("    1. Add GetCurrentConfig RPC to ConfigService in api/proto/config.proto")
		fmt.Println("    2. Implement the handler in internal/agent/config/handler.go")
		fmt.Println("    3. Expose the method in the agent gRPC server")
		fmt.Println()
		fmt.Println("Example proto addition:")
		fmt.Println("  rpc GetCurrentConfig(GetConfigRequest) returns (ConfigSnapshot);")
		return nil
	}

	// Display configuration
	fmt.Printf("Configuration Version: %s\n", agentConfig.Version)
	fmt.Printf("Generation Time: %s\n", agentConfig.GenerationTime)
	fmt.Printf("\nSummary:\n")
	fmt.Printf("  Gateways:  %d\n", agentConfig.GatewayCount)
	fmt.Printf("  Routes:    %d\n", agentConfig.RouteCount)
	fmt.Printf("  Clusters:  %d\n", agentConfig.ClusterCount)
	fmt.Printf("  Endpoints: %d\n", agentConfig.EndpointCount)
	fmt.Printf("  VIPs:      %d\n", agentConfig.VIPCount)
	fmt.Printf("  Policies:  %d\n", agentConfig.PolicyCount)

	if len(agentConfig.Gateways) > 0 {
		fmt.Printf("\nGateways:\n")
		w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tNAMESPACE\tVIP\tLISTENERS")
		for _, gw := range agentConfig.Gateways {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\n",
				gw.Name, gw.Namespace, gw.VIPRef, len(gw.Listeners))
		}
		w.Flush()
	}

	if len(agentConfig.VIPs) > 0 {
		fmt.Printf("\nVIP Assignments:\n")
		w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tADDRESS\tMODE\tACTIVE\tPORTS")
		for _, vip := range agentConfig.VIPs {
			active := "No"
			if vip.IsActive {
				active = "Yes"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%v\n",
				vip.Name, vip.Address, vip.Mode, active, vip.Ports)
		}
		w.Flush()
	}

	return nil
}

func newAgentBackendsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backends <node-name>",
		Short: "Get backend health status from a specific node",
		Long: `Query the NovaEdge agent running on a specific node to retrieve
backend cluster health status and endpoint information.`,
		Example: `  # Get backend health from agent on worker-1
  novactl agent backends worker-1`,
		Args: cobra.ExactArgs(1),
		RunE: runAgentBackends,
	}

	return cmd
}

func runAgentBackends(cmd *cobra.Command, args []string) error {
	nodeName := args[0]
	ctx := context.Background()

	// Create Kubernetes client
	c, err := client.NewClient(config)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Find the agent pod on this node
	podName, err := grpcClient.FindAgentPod(ctx, c.Clientset, agentNamespace, nodeName)
	if err != nil {
		return fmt.Errorf("failed to find agent pod: %w", err)
	}

	fmt.Printf("Querying agent pod: %s (node: %s)\n\n", podName, nodeName)

	// Create gRPC client
	agentClient, err := grpcClient.NewAgentClient(ctx, c.Clientset, agentNamespace, podName)
	if err != nil {
		return fmt.Errorf("failed to create agent client: %w", err)
	}
	defer agentClient.Close()

	// Get backend health
	backends, err := agentClient.GetBackendHealth(ctx)
	if err != nil {
		fmt.Println("Note: Direct backend health querying requires implementing additional gRPC methods.")
		fmt.Println()
		fmt.Println("To enable 'novactl agent backends', add to api/proto/config.proto:")
		fmt.Println("  rpc GetBackendHealth(GetBackendHealthRequest) returns (BackendHealthResponse);")
		return nil
	}

	if len(backends) == 0 {
		fmt.Println("No backend clusters configured on this agent.")
		return nil
	}

	fmt.Printf("Backend Health Status:\n\n")

	for _, backend := range backends {
		fmt.Printf("Cluster: %s/%s\n", backend.Namespace, backend.ClusterName)
		fmt.Printf("Load Balancing: %s\n", backend.LBPolicy)
		fmt.Printf("Endpoints: %d\n", len(backend.Endpoints))

		if len(backend.Endpoints) > 0 {
			w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
			fmt.Fprintln(w, "  ADDRESS\tPORT\tSTATUS")
			for _, ep := range backend.Endpoints {
				status := "Not Ready"
				if ep.Ready {
					status = "Ready"
				}
				fmt.Fprintf(w, "  %s\t%d\t%s\n", ep.Address, ep.Port, status)
			}
			w.Flush()
		}
		fmt.Println()
	}

	return nil
}

func newAgentVIPsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vips <node-name>",
		Short: "Get active VIPs from a specific node",
		Long: `Query the NovaEdge agent running on a specific node to retrieve
information about VIPs that are currently active on that node.`,
		Example: `  # Get active VIPs from agent on worker-1
  novactl agent vips worker-1`,
		Args: cobra.ExactArgs(1),
		RunE: runAgentVIPs,
	}

	return cmd
}

func runAgentVIPs(cmd *cobra.Command, args []string) error {
	nodeName := args[0]
	ctx := context.Background()

	// Create Kubernetes client
	c, err := client.NewClient(config)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Find the agent pod on this node
	podName, err := grpcClient.FindAgentPod(ctx, c.Clientset, agentNamespace, nodeName)
	if err != nil {
		return fmt.Errorf("failed to find agent pod: %w", err)
	}

	fmt.Printf("Querying agent pod: %s (node: %s)\n\n", podName, nodeName)

	// Create gRPC client
	agentClient, err := grpcClient.NewAgentClient(ctx, c.Clientset, agentNamespace, nodeName)
	if err != nil {
		return fmt.Errorf("failed to create agent client: %w", err)
	}
	defer agentClient.Close()

	// Get VIPs
	vips, err := agentClient.GetVIPs(ctx)
	if err != nil {
		fmt.Println("Note: Direct VIP querying requires implementing additional gRPC methods.")
		fmt.Println()
		fmt.Println("To enable 'novactl agent vips', add to api/proto/config.proto:")
		fmt.Println("  rpc GetVIPs(GetVIPsRequest) returns (VIPsResponse);")
		return nil
	}

	if len(vips) == 0 {
		fmt.Println("No VIPs assigned to this agent.")
		return nil
	}

	fmt.Printf("Active VIPs:\n\n")

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tADDRESS\tMODE\tACTIVE\tPORTS")

	for _, vip := range vips {
		active := "No"
		if vip.IsActive {
			active = "Yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%v\n",
			vip.Name, vip.Address, vip.Mode, active, vip.Ports)
	}

	w.Flush()

	return nil
}
