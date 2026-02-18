package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"

	"github.com/piwi3910/novaedge/cmd/novactl/pkg/client"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	accessLogNodeName string
	accessLogFollow   bool
	accessLogTail     int64
)

func newLogsAccessCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "access",
		Short: "Stream access logs from an agent",
		Long: `View access logs from NovaEdge agents. Access logs include HTTP request/response
details such as client IP, method, URI, status code, and response time.

Access logs are emitted by agents when access logging is enabled in the gateway
or route configuration.`,
		Example: `  # View access logs from a specific agent
  novactl logs access --node worker-1

  # Follow access logs
  novactl logs access --node worker-1 -f

  # Show last 200 lines
  novactl logs access --node worker-1 --tail 200`,
		RunE: runLogsAccess,
	}

	cmd.Flags().StringVar(&accessLogNodeName, "node", "", "Node name to stream access logs from (required)")
	cmd.Flags().BoolVarP(&accessLogFollow, "follow", "f", false, "Follow log output")
	cmd.Flags().Int64Var(&accessLogTail, "tail", 100, "Number of lines to show from the end of the logs")

	return cmd
}

func runLogsAccess(_ *cobra.Command, _ []string) error {
	if accessLogNodeName == "" {
		return fmt.Errorf("--node flag is required")
	}

	ctx := context.Background()

	// Create client
	c, err := client.NewClient(config)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Find agent pod on the specified node
	pods, err := c.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		LabelSelector: "app=novaedge-agent",
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", accessLogNodeName),
	})
	if err != nil {
		return fmt.Errorf("failed to list agent pods: %w", err)
	}

	if len(pods.Items) == 0 {
		return fmt.Errorf("no agent found on node %s", accessLogNodeName)
	}

	pod := pods.Items[0]

	// Stream logs and filter for access log entries
	return streamAccessLogs(ctx, c, pod.Namespace, pod.Name)
}

func streamAccessLogs(ctx context.Context, c *client.Client, namespace, podName string) error {
	opts := &corev1.PodLogOptions{
		Container:  "novaedge-agent",
		Follow:     accessLogFollow,
		Timestamps: false,
	}

	if accessLogTail > 0 {
		opts.TailLines = &accessLogTail
	}

	req := c.Clientset.CoreV1().Pods(namespace).GetLogs(podName, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("failed to stream logs: %w", err)
	}
	defer func() { _ = stream.Close() }()

	reader := bufio.NewReader(stream)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("error reading logs: %w", err)
		}
		// Print all lines - access logs are written to stdout by the agent
		// and captured in pod logs
		fmt.Print(string(line))
	}

	return nil
}
