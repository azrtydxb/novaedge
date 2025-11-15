package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/piwi3910/novaedge/cmd/novactl/pkg/client"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newAgentsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Manage and inspect NovaEdge agents",
		Long:  `View information about NovaEdge agents running on cluster nodes.`,
	}

	cmd.AddCommand(newAgentsListCommand())
	cmd.AddCommand(newAgentsDescribeCommand())
	cmd.AddCommand(newAgentsConfigCommand())

	return cmd
}

func newAgentsListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all NovaEdge agents",
		Long:  `Display a list of all NovaEdge agents and their status.`,
		Example: `  # List all agents
  novactl agents list`,
		RunE: runAgentsList,
	}

	return cmd
}

func runAgentsList(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Create client
	c, err := client.NewClient(config)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// List pods with label selector for agents
	pods, err := c.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		LabelSelector: "app=novaedge-agent",
	})
	if err != nil {
		return fmt.Errorf("failed to list agent pods: %w", err)
	}

	if len(pods.Items) == 0 {
		fmt.Println("No agents found.")
		return nil
	}

	// Print table
	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "NODE\tSTATUS\tRESTARTS\tAGE")

	for _, pod := range pods.Items {
		node := pod.Spec.NodeName
		status := string(pod.Status.Phase)
		restarts := int32(0)
		if len(pod.Status.ContainerStatuses) > 0 {
			restarts = pod.Status.ContainerStatuses[0].RestartCount
		}
		age := formatAgentAge(pod.CreationTimestamp)

		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", node, status, restarts, age)
	}

	w.Flush()
	return nil
}

func newAgentsDescribeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "describe [node-name]",
		Short: "Describe a specific agent",
		Long:  `Show detailed information about a NovaEdge agent on a specific node.`,
		Example: `  # Describe agent on a node
  novactl agents describe worker-1`,
		RunE: runAgentsDescribe,
	}

	return cmd
}

func runAgentsDescribe(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("exactly one argument required: node-name")
	}

	nodeName := args[0]
	ctx := context.Background()

	// Create client
	c, err := client.NewClient(config)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Find agent pod on the specified node
	pods, err := c.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		LabelSelector: "app=novaedge-agent",
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	})
	if err != nil {
		return fmt.Errorf("failed to list agent pods: %w", err)
	}

	if len(pods.Items) == 0 {
		return fmt.Errorf("no agent found on node %s", nodeName)
	}

	pod := pods.Items[0]

	// Print detailed information
	fmt.Printf("Agent on Node: %s\n", nodeName)
	fmt.Printf("Pod Name: %s\n", pod.Name)
	fmt.Printf("Namespace: %s\n", pod.Namespace)
	fmt.Printf("Status: %s\n", pod.Status.Phase)
	fmt.Printf("Pod IP: %s\n", pod.Status.PodIP)
	fmt.Printf("Host IP: %s\n", pod.Status.HostIP)
	fmt.Printf("Start Time: %s\n", pod.Status.StartTime)

	if len(pod.Status.ContainerStatuses) > 0 {
		cs := pod.Status.ContainerStatuses[0]
		fmt.Printf("\nContainer Status:\n")
		fmt.Printf("  Ready: %v\n", cs.Ready)
		fmt.Printf("  Restart Count: %d\n", cs.RestartCount)
		fmt.Printf("  Image: %s\n", cs.Image)
		if cs.State.Running != nil {
			fmt.Printf("  State: Running (started %s)\n", cs.State.Running.StartedAt)
		} else if cs.State.Waiting != nil {
			fmt.Printf("  State: Waiting (%s)\n", cs.State.Waiting.Reason)
		} else if cs.State.Terminated != nil {
			fmt.Printf("  State: Terminated (%s)\n", cs.State.Terminated.Reason)
		}
	}

	// Print conditions
	if len(pod.Status.Conditions) > 0 {
		fmt.Printf("\nConditions:\n")
		for _, cond := range pod.Status.Conditions {
			fmt.Printf("  %s: %s\n", cond.Type, cond.Status)
		}
	}

	return nil
}

func newAgentsConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config [node-name]",
		Short: "Show agent's current configuration",
		Long:  `Display the current configuration snapshot for an agent on a specific node.`,
		Example: `  # Show agent config
  novactl agents config worker-1`,
		RunE: runAgentsConfig,
	}

	return cmd
}

func runAgentsConfig(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("exactly one argument required: node-name")
	}

	nodeName := args[0]
	fmt.Printf("Config for agent on node %s:\n", nodeName)
	fmt.Println("(Config snapshot retrieval requires connection to agent's gRPC API)")
	fmt.Println("This feature requires implementing a client for the agent's gRPC service.")

	return nil
}

func formatAgentAge(t metav1.Time) string {
	duration := time.Since(t.Time)

	if duration.Seconds() < 60 {
		return fmt.Sprintf("%ds", int(duration.Seconds()))
	}
	if duration.Minutes() < 60 {
		return fmt.Sprintf("%dm", int(duration.Minutes()))
	}
	if duration.Hours() < 24 {
		return fmt.Sprintf("%dh", int(duration.Hours()))
	}
	return fmt.Sprintf("%dd", int(duration.Hours()/24))
}
