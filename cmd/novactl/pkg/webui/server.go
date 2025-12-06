// Package webui provides a web-based dashboard for NovaEdge.
package webui

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/piwi3910/novaedge/cmd/novactl/pkg/client"
	"github.com/piwi3910/novaedge/cmd/novactl/pkg/prometheus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

//go:embed static/*
var staticFiles embed.FS

// Server represents the web UI server
type Server struct {
	addr             string
	k8sClient        *client.Client
	clientset        kubernetes.Interface
	prometheusClient *prometheus.Client
	server           *http.Server
	mu               sync.RWMutex
}

// Config holds server configuration
type Config struct {
	Address            string
	KubeConfig         *rest.Config
	PrometheusEndpoint string
}

// NewServer creates a new web UI server
func NewServer(cfg Config) (*Server, error) {
	k8sClient, err := client.NewClient(cfg.KubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(cfg.KubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	var promClient *prometheus.Client
	if cfg.PrometheusEndpoint != "" {
		promClient = prometheus.NewClient(cfg.PrometheusEndpoint)
	}

	return &Server{
		addr:             cfg.Address,
		k8sClient:        k8sClient,
		clientset:        clientset,
		prometheusClient: promClient,
	}, nil
}

// Start starts the web UI server
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/v1/gateways", s.handleGateways)
	mux.HandleFunc("/api/v1/gateways/", s.handleGateway)
	mux.HandleFunc("/api/v1/routes", s.handleRoutes)
	mux.HandleFunc("/api/v1/routes/", s.handleRoute)
	mux.HandleFunc("/api/v1/backends", s.handleBackends)
	mux.HandleFunc("/api/v1/backends/", s.handleBackend)
	mux.HandleFunc("/api/v1/vips", s.handleVIPs)
	mux.HandleFunc("/api/v1/vips/", s.handleVIP)
	mux.HandleFunc("/api/v1/policies", s.handlePolicies)
	mux.HandleFunc("/api/v1/policies/", s.handlePolicy)
	mux.HandleFunc("/api/v1/agents", s.handleAgents)
	mux.HandleFunc("/api/v1/metrics/dashboard", s.handleMetricsDashboard)
	mux.HandleFunc("/api/v1/metrics/query", s.handleMetricsQuery)
	mux.HandleFunc("/api/v1/namespaces", s.handleNamespaces)
	mux.HandleFunc("/api/v1/health", s.handleHealth)

	// Static files
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("failed to get static files: %w", err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	s.server = &http.Server{
		Addr:              s.addr,
		Handler:           corsMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	fmt.Printf("Starting NovaEdge Web UI at http://%s\n", s.addr)
	return s.server.ListenAndServe()
}

// Stop gracefully stops the server
func (s *Server) Stop(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

// corsMiddleware adds CORS headers for development
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// writeJSON writes a JSON response
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// writeError writes an error response
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// parseNamespaceFromQuery extracts namespace from query params
func parseNamespaceFromQuery(r *http.Request) string {
	ns := r.URL.Query().Get("namespace")
	if ns == "" {
		return "default"
	}
	return ns
}

// handleGateways handles GET /api/v1/gateways
func (s *Server) handleGateways(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := r.Context()
	namespace := parseNamespaceFromQuery(r)

	var list *unstructured.UnstructuredList
	var err error

	if namespace == "" || namespace == "all" {
		// List across all namespaces
		list, err = s.k8sClient.Dynamic.Resource(client.GetGVR(client.ResourceGateway)).List(ctx, metav1.ListOptions{})
	} else {
		list, err = s.k8sClient.ListResources(ctx, client.ResourceGateway, namespace)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list gateways: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, list.Items)
}

// handleGateway handles GET /api/v1/gateways/{namespace}/{name}
func (s *Server) handleGateway(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/gateways/"), "/")
	if len(parts) != 2 {
		writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/gateways/{namespace}/{name}")
		return
	}

	namespace, name := parts[0], parts[1]
	ctx := r.Context()

	gateway, err := s.k8sClient.GetResource(ctx, client.ResourceGateway, namespace, name)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("gateway not found: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, gateway)
}

// handleRoutes handles GET /api/v1/routes
func (s *Server) handleRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := r.Context()
	namespace := parseNamespaceFromQuery(r)

	var list *unstructured.UnstructuredList
	var err error

	if namespace == "" || namespace == "all" {
		list, err = s.k8sClient.Dynamic.Resource(client.GetGVR(client.ResourceRoute)).List(ctx, metav1.ListOptions{})
	} else {
		list, err = s.k8sClient.ListResources(ctx, client.ResourceRoute, namespace)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list routes: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, list.Items)
}

// handleRoute handles GET /api/v1/routes/{namespace}/{name}
func (s *Server) handleRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/routes/"), "/")
	if len(parts) != 2 {
		writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/routes/{namespace}/{name}")
		return
	}

	namespace, name := parts[0], parts[1]
	ctx := r.Context()

	route, err := s.k8sClient.GetResource(ctx, client.ResourceRoute, namespace, name)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("route not found: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, route)
}

// handleBackends handles GET /api/v1/backends
func (s *Server) handleBackends(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := r.Context()
	namespace := parseNamespaceFromQuery(r)

	var list *unstructured.UnstructuredList
	var err error

	if namespace == "" || namespace == "all" {
		list, err = s.k8sClient.Dynamic.Resource(client.GetGVR(client.ResourceBackend)).List(ctx, metav1.ListOptions{})
	} else {
		list, err = s.k8sClient.ListResources(ctx, client.ResourceBackend, namespace)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list backends: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, list.Items)
}

// handleBackend handles GET /api/v1/backends/{namespace}/{name}
func (s *Server) handleBackend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/backends/"), "/")
	if len(parts) != 2 {
		writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/backends/{namespace}/{name}")
		return
	}

	namespace, name := parts[0], parts[1]
	ctx := r.Context()

	backend, err := s.k8sClient.GetResource(ctx, client.ResourceBackend, namespace, name)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("backend not found: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, backend)
}

// handleVIPs handles GET /api/v1/vips
func (s *Server) handleVIPs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := r.Context()
	namespace := parseNamespaceFromQuery(r)

	var list *unstructured.UnstructuredList
	var err error

	if namespace == "" || namespace == "all" {
		list, err = s.k8sClient.Dynamic.Resource(client.GetGVR(client.ResourceVIP)).List(ctx, metav1.ListOptions{})
	} else {
		list, err = s.k8sClient.ListResources(ctx, client.ResourceVIP, namespace)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list vips: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, list.Items)
}

// handleVIP handles GET /api/v1/vips/{namespace}/{name}
func (s *Server) handleVIP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/vips/"), "/")
	if len(parts) != 2 {
		writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/vips/{namespace}/{name}")
		return
	}

	namespace, name := parts[0], parts[1]
	ctx := r.Context()

	vip, err := s.k8sClient.GetResource(ctx, client.ResourceVIP, namespace, name)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("vip not found: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, vip)
}

// handlePolicies handles GET /api/v1/policies
func (s *Server) handlePolicies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := r.Context()
	namespace := parseNamespaceFromQuery(r)

	var list *unstructured.UnstructuredList
	var err error

	if namespace == "" || namespace == "all" {
		list, err = s.k8sClient.Dynamic.Resource(client.GetGVR(client.ResourcePolicy)).List(ctx, metav1.ListOptions{})
	} else {
		list, err = s.k8sClient.ListResources(ctx, client.ResourcePolicy, namespace)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list policies: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, list.Items)
}

// handlePolicy handles GET /api/v1/policies/{namespace}/{name}
func (s *Server) handlePolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/policies/"), "/")
	if len(parts) != 2 {
		writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/policies/{namespace}/{name}")
		return
	}

	namespace, name := parts[0], parts[1]
	ctx := r.Context()

	policy, err := s.k8sClient.GetResource(ctx, client.ResourcePolicy, namespace, name)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("policy not found: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, policy)
}

// handleAgents handles GET /api/v1/agents
func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := r.Context()
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "novaedge-system"
	}

	// List agent pods
	pods, err := s.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=novaedge-agent",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list agent pods: %v", err))
		return
	}

	agents := make([]AgentInfo, 0, len(pods.Items))
	for _, pod := range pods.Items {
		agent := AgentInfo{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			NodeName:  pod.Spec.NodeName,
			PodIP:     pod.Status.PodIP,
			Phase:     string(pod.Status.Phase),
			Ready:     isPodReady(&pod),
			StartTime: pod.Status.StartTime,
		}
		agents = append(agents, agent)
	}

	writeJSON(w, http.StatusOK, agents)
}

// AgentInfo represents agent pod information
type AgentInfo struct {
	Name      string       `json:"name"`
	Namespace string       `json:"namespace"`
	NodeName  string       `json:"nodeName"`
	PodIP     string       `json:"podIP"`
	Phase     string       `json:"phase"`
	Ready     bool         `json:"ready"`
	StartTime *metav1.Time `json:"startTime,omitempty"`
}

// isPodReady checks if a pod is ready
func isPodReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// handleNamespaces handles GET /api/v1/namespaces
func (s *Server) handleNamespaces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := r.Context()

	namespaces, err := s.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list namespaces: %v", err))
		return
	}

	names := make([]string, 0, len(namespaces.Items))
	for _, ns := range namespaces.Items {
		names = append(names, ns.Name)
	}

	writeJSON(w, http.StatusOK, names)
}

// handleHealth handles GET /api/v1/health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
}

// DashboardMetrics represents aggregated metrics for the dashboard
type DashboardMetrics struct {
	RequestRate       *float64 `json:"requestRate,omitempty"`
	ActiveConnections *float64 `json:"activeConnections,omitempty"`
	ErrorRate         *float64 `json:"errorRate,omitempty"`
	AvgLatency        *float64 `json:"avgLatency,omitempty"`
	VIPFailovers      *float64 `json:"vipFailovers,omitempty"`
	HealthyAgents     *float64 `json:"healthyAgents,omitempty"`
	TotalAgents       *float64 `json:"totalAgents,omitempty"`
	Timestamp         int64    `json:"timestamp"`
}

// handleMetricsDashboard handles GET /api/v1/metrics/dashboard
func (s *Server) handleMetricsDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.prometheusClient == nil {
		writeError(w, http.StatusServiceUnavailable, "prometheus not configured")
		return
	}

	ctx := r.Context()
	metrics := DashboardMetrics{
		Timestamp: time.Now().Unix(),
	}

	// Query request rate
	if result, err := s.prometheusClient.Query(ctx, `sum(rate(novaedge_agent_requests_total[5m]))`); err == nil && len(result.Data.Result) > 0 {
		if value, err := prometheus.ValueAsFloat(result.Data.Result[0]); err == nil {
			metrics.RequestRate = &value
		}
	}

	// Query active connections
	if result, err := s.prometheusClient.Query(ctx, `sum(novaedge_agent_active_connections)`); err == nil && len(result.Data.Result) > 0 {
		if value, err := prometheus.ValueAsFloat(result.Data.Result[0]); err == nil {
			metrics.ActiveConnections = &value
		}
	}

	// Query error rate
	if result, err := s.prometheusClient.Query(ctx, `sum(rate(novaedge_agent_requests_total{status=~"5.."}[5m]))`); err == nil && len(result.Data.Result) > 0 {
		if value, err := prometheus.ValueAsFloat(result.Data.Result[0]); err == nil {
			metrics.ErrorRate = &value
		}
	}

	// Query average latency
	if result, err := s.prometheusClient.Query(ctx, `avg(novaedge_agent_request_duration_seconds)`); err == nil && len(result.Data.Result) > 0 {
		if value, err := prometheus.ValueAsFloat(result.Data.Result[0]); err == nil {
			metrics.AvgLatency = &value
		}
	}

	// Query VIP failovers
	if result, err := s.prometheusClient.Query(ctx, `sum(increase(novaedge_vip_failovers_total[24h]))`); err == nil && len(result.Data.Result) > 0 {
		if value, err := prometheus.ValueAsFloat(result.Data.Result[0]); err == nil {
			metrics.VIPFailovers = &value
		}
	}

	// Query healthy agents
	if result, err := s.prometheusClient.Query(ctx, `count(up{job="novaedge-agent"} == 1)`); err == nil && len(result.Data.Result) > 0 {
		if value, err := prometheus.ValueAsFloat(result.Data.Result[0]); err == nil {
			metrics.HealthyAgents = &value
		}
	}

	// Query total agents
	if result, err := s.prometheusClient.Query(ctx, `count(up{job="novaedge-agent"})`); err == nil && len(result.Data.Result) > 0 {
		if value, err := prometheus.ValueAsFloat(result.Data.Result[0]); err == nil {
			metrics.TotalAgents = &value
		}
	}

	writeJSON(w, http.StatusOK, metrics)
}

// handleMetricsQuery handles GET /api/v1/metrics/query
func (s *Server) handleMetricsQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.prometheusClient == nil {
		writeError(w, http.StatusServiceUnavailable, "prometheus not configured")
		return
	}

	query := r.URL.Query().Get("query")
	if query == "" {
		writeError(w, http.StatusBadRequest, "query parameter is required")
		return
	}

	ctx := r.Context()

	// Check if it's a range query
	start := r.URL.Query().Get("start")
	end := r.URL.Query().Get("end")

	if start != "" && end != "" {
		// Range query
		startTime, err := time.Parse(time.RFC3339, start)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid start time: %v", err))
			return
		}

		endTime, err := time.Parse(time.RFC3339, end)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid end time: %v", err))
			return
		}

		step := 15 * time.Second
		if stepStr := r.URL.Query().Get("step"); stepStr != "" {
			if parsed, err := time.ParseDuration(stepStr); err == nil {
				step = parsed
			}
		}

		result, err := s.prometheusClient.QueryRange(ctx, prometheus.RangeQueryParams{
			Query: query,
			Start: startTime,
			End:   endTime,
			Step:  step,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
			return
		}

		writeJSON(w, http.StatusOK, result)
	} else {
		// Instant query
		result, err := s.prometheusClient.Query(ctx, query)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}
