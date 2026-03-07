package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/azrtydxb/novaedge/cmd/novactl/pkg/client"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	errNoControllerPodsFound        = errors.New("no controller pods found")
	errNoRunningControllerPodsFound = errors.New("no running controller pods found")
)

var (
	follow     bool
	tailLines  int64
	timestamps bool
)

func newLogsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Stream logs from NovaEdge components",
		Long:  `View logs from controller and agent pods.`,
	}

	cmd.AddCommand(newLogsAgentCommand())
	cmd.AddCommand(newLogsControllerCommand())
	cmd.AddCommand(newLogsAccessCommand())

	return cmd
}

func newLogsAgentCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent [node-name]",
		Short: "Stream logs from an agent",
		Long:  `View logs from the NovaEdge agent running on a specific node.`,
		Example: `  # View agent logs
  novactl logs agent worker-1

  # Follow agent logs
  novactl logs agent worker-1 -f

  # Show last 100 lines
  novactl logs agent worker-1 --tail 100`,
		RunE: runLogsAgent,
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	cmd.Flags().Int64Var(&tailLines, "tail", 50, "Number of lines to show from the end of the logs")
	cmd.Flags().BoolVar(&timestamps, "timestamps", false, "Include timestamps in output")

	return cmd
}

func runLogsAgent(_ *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errExactlyOneArgumentRequiredNodeName
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
		return fmt.Errorf("%w: %s", errNoAgentFoundOnNode, nodeName)
	}

	pod := pods.Items[0]
	return streamPodLogs(ctx, c, pod.Namespace, pod.Name, "novaedge-agent")
}

func newLogsControllerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "controller",
		Short: "Stream logs from the controller",
		Long:  `View logs from the NovaEdge controller.`,
		Example: `  # View controller logs
  novactl logs controller

  # Follow controller logs
  novactl logs controller -f`,
		RunE: runLogsController,
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	cmd.Flags().Int64Var(&tailLines, "tail", 50, "Number of lines to show from the end of the logs")
	cmd.Flags().BoolVar(&timestamps, "timestamps", false, "Include timestamps in output")

	return cmd
}

func runLogsController(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	// Create client
	c, err := client.NewClient(config)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Find controller pod
	pods, err := c.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		LabelSelector: "app=novaedge-controller",
	})
	if err != nil {
		return fmt.Errorf("failed to list controller pods: %w", err)
	}

	if len(pods.Items) == 0 {
		return errNoControllerPodsFound
	}

	// Use the first running pod
	var pod *corev1.Pod
	for i := range pods.Items {
		if pods.Items[i].Status.Phase == corev1.PodRunning {
			pod = &pods.Items[i]
			break
		}
	}

	if pod == nil {
		return errNoRunningControllerPodsFound
	}

	return streamPodLogs(ctx, c, pod.Namespace, pod.Name, "novaedge-controller")
}

func streamPodLogs(ctx context.Context, c *client.Client, namespace, podName, containerName string) error {
	opts := &corev1.PodLogOptions{
		Container:  containerName,
		Follow:     follow,
		Timestamps: timestamps,
	}

	if tailLines > 0 {
		opts.TailLines = &tailLines
	}

	req := c.Clientset.CoreV1().Pods(namespace).GetLogs(podName, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("failed to stream logs: %w", err)
	}
	defer func() { _ = stream.Close() }()

	// Copy stream to stdout
	reader := bufio.NewReader(stream)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("error reading logs: %w", err)
		}
		fmt.Print(string(line))
	}

	return nil
}
