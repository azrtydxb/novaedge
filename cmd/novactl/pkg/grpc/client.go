// Package grpc provides gRPC client functionality for querying NovaEdge agents.
package grpc

import (
	"context"
	"fmt"
	"time"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// AgentClient wraps a gRPC connection to a NovaEdge agent
type AgentClient struct {
	conn   *grpc.ClientConn
	client pb.ConfigServiceClient
}

// NewAgentClient creates a new agent client for the specified agent pod
func NewAgentClient(ctx context.Context, clientset kubernetes.Interface, namespace, podName string) (*AgentClient, error) {
	// Get pod details to find the agent's IP
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod %s: %w", podName, err)
	}

	if pod.Status.PodIP == "" {
		return nil, fmt.Errorf("pod %s has no IP address", podName)
	}

	// Agent gRPC port (typically 9090)
	agentAddress := fmt.Sprintf("%s:9090", pod.Status.PodIP)

	// Create gRPC connection with timeout
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, agentAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to agent at %s: %w", agentAddress, err)
	}

	client := pb.NewConfigServiceClient(conn)

	return &AgentClient{
		conn:   conn,
		client: client,
	}, nil
}

// Close closes the gRPC connection
func (c *AgentClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// GetAgentStatus queries the agent's current status
func (c *AgentClient) GetAgentStatus(ctx context.Context, nodeName string) (*pb.StatusResponse, error) {
	status := &pb.AgentStatus{
		NodeName:  nodeName,
		Timestamp: time.Now().Unix(),
	}

	resp, err := c.client.ReportStatus(ctx, status)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent status: %w", err)
	}

	return resp, nil
}

// AgentConfig represents simplified agent configuration for display
type AgentConfig struct {
	Version        string
	GenerationTime time.Time
	GatewayCount   int
	RouteCount     int
	ClusterCount   int
	EndpointCount  int
	VIPCount       int
	PolicyCount    int
	Gateways       []GatewayInfo
	VIPs           []VIPInfo
}

// GatewayInfo contains gateway information
type GatewayInfo struct {
	Name      string
	Namespace string
	VIPRef    string
	Listeners []ListenerInfo
}

// ListenerInfo contains listener information
type ListenerInfo struct {
	Name      string
	Port      int32
	Protocol  string
	Hostnames []string
}

// VIPInfo contains VIP information
type VIPInfo struct {
	Name      string
	Address   string
	Mode      string
	IsActive  bool
	Ports     []int32
}

// BackendHealth represents backend health information
type BackendHealth struct {
	ClusterName string
	Namespace   string
	LBPolicy    string
	Endpoints   []EndpointHealth
}

// EndpointHealth represents individual endpoint health
type EndpointHealth struct {
	Address string
	Port    int32
	Ready   bool
}

// GetConfig retrieves the current configuration from the agent
// Note: This requires implementing a new RPC method in the proto definition
// For now, this is a placeholder that shows the structure
func (c *AgentClient) GetConfig(ctx context.Context) (*AgentConfig, error) {
	// This would require a new gRPC method like GetCurrentConfig()
	// Since the current proto only has StreamConfig and ReportStatus,
	// we return an informative error
	return nil, fmt.Errorf("GetConfig requires implementing a new gRPC method in the agent service")
}

// GetBackendHealth retrieves backend health information from the agent
// Note: This requires implementing a new RPC method in the proto definition
func (c *AgentClient) GetBackendHealth(ctx context.Context) ([]BackendHealth, error) {
	// This would require a new gRPC method like GetBackendHealth()
	return nil, fmt.Errorf("GetBackendHealth requires implementing a new gRPC method in the agent service")
}

// GetVIPs retrieves VIP information from the agent
// Note: This requires implementing a new RPC method in the proto definition
func (c *AgentClient) GetVIPs(ctx context.Context) ([]VIPInfo, error) {
	// This would require a new gRPC method like GetVIPs()
	return nil, fmt.Errorf("GetVIPs requires implementing a new gRPC method in the agent service")
}

// FindAgentPod finds the NovaEdge agent pod running on a specific node
func FindAgentPod(ctx context.Context, clientset kubernetes.Interface, namespace, nodeName string) (string, error) {
	// List all pods in the namespace with the agent label
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=novaedge-agent",
	})
	if err != nil {
		return "", fmt.Errorf("failed to list agent pods: %w", err)
	}

	// Find the pod running on the specified node
	for _, pod := range pods.Items {
		if pod.Spec.NodeName == nodeName {
			return pod.Name, nil
		}
	}

	return "", fmt.Errorf("no agent pod found on node %s", nodeName)
}
