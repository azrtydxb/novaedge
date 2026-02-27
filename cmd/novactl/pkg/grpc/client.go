// Package grpc provides gRPC client functionality for querying NovaEdge agents.
package grpc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/piwi3910/novaedge/internal/pkg/grpclimits"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var (
	errPod                   = errors.New("pod")
	errNoAgentPodFoundOnNode = errors.New("no agent pod found on node")
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
		return nil, fmt.Errorf("%w: %s has no IP address", errPod, podName)
	}

	// Agent introspection gRPC port
	agentAddress := fmt.Sprintf("%s:9092", pod.Status.PodIP)

	// Create gRPC connection using lazy connect (grpc.NewClient)
	opts := grpclimits.ClientOptions()
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.NewClient(agentAddress, opts...)
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
	Name     string
	Address  string
	Mode     string
	IsActive bool
	Ports    []int32
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

// GetConfig retrieves the current configuration from the agent.
func (c *AgentClient) GetConfig(ctx context.Context) (*AgentConfig, error) {
	resp, err := c.client.GetAgentConfig(ctx, &pb.GetConfigRequest{})
	if err != nil {
		return nil, fmt.Errorf("get agent config: %w", err)
	}
	return &AgentConfig{
		Version:       resp.Version,
		GatewayCount:  int(resp.GatewayCount),
		RouteCount:    int(resp.RouteCount),
		ClusterCount:  int(resp.ClusterCount),
		EndpointCount: int(resp.EndpointCount),
		VIPCount:      int(resp.VipCount),
		PolicyCount:   int(resp.PolicyCount),
	}, nil
}

// GetBackendHealth retrieves backend health information from the agent.
func (c *AgentClient) GetBackendHealth(ctx context.Context) ([]BackendHealth, error) {
	resp, err := c.client.GetBackendHealth(ctx, &pb.GetBackendHealthRequest{})
	if err != nil {
		return nil, fmt.Errorf("get backend health: %w", err)
	}
	backends := make([]BackendHealth, 0, len(resp.Backends))
	for _, b := range resp.Backends {
		bh := BackendHealth{
			ClusterName: b.ClusterName,
			Namespace:   b.Namespace,
			LBPolicy:    b.LbPolicy,
		}
		for _, ep := range b.Endpoints {
			bh.Endpoints = append(bh.Endpoints, EndpointHealth{
				Address: ep.Address,
				Port:    ep.Port,
				Ready:   ep.Healthy,
			})
		}
		backends = append(backends, bh)
	}
	return backends, nil
}

// GetVIPs retrieves VIP information from the agent.
func (c *AgentClient) GetVIPs(ctx context.Context) ([]VIPInfo, error) {
	resp, err := c.client.GetVIPs(ctx, &pb.GetVIPsRequest{})
	if err != nil {
		return nil, fmt.Errorf("get VIPs: %w", err)
	}
	vips := make([]VIPInfo, 0, len(resp.Vips))
	for _, v := range resp.Vips {
		vips = append(vips, VIPInfo{
			Name:     v.Name,
			Address:  v.Address,
			Mode:     v.Mode,
			IsActive: v.IsActive,
			Ports:    v.Ports,
		})
	}
	return vips, nil
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

	return "", fmt.Errorf("%w: %s", errNoAgentPodFoundOnNode, nodeName)
}
