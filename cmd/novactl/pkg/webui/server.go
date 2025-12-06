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
	"time"

	"github.com/piwi3910/novaedge/cmd/novactl/pkg/client"
	"github.com/piwi3910/novaedge/cmd/novactl/pkg/prometheus"
	"github.com/piwi3910/novaedge/cmd/novactl/pkg/webui/mode"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	backend          mode.Backend
	server           *http.Server
}

// Config holds server configuration
type Config struct {
	Address              string
	KubeConfig           *rest.Config
	PrometheusEndpoint   string
	Mode                 string // auto, kubernetes, standalone
	StandaloneConfigPath string
	ReadOnly             bool
}

// NewServer creates a new web UI server
func NewServer(cfg Config) (*Server, error) {
	s := &Server{
		addr: cfg.Address,
	}

	// Determine operating mode
	var opMode mode.Mode
	switch cfg.Mode {
	case "kubernetes":
		opMode = mode.ModeKubernetes
	case "standalone":
		opMode = mode.ModeStandalone
	default:
		// Auto-detect mode
		opMode = mode.DetectMode(cfg.KubeConfig, cfg.StandaloneConfigPath)
	}

	// Create the backend
	backend, err := mode.NewBackend(opMode, cfg.KubeConfig, cfg.StandaloneConfigPath, cfg.ReadOnly)
	if err != nil {
		return nil, fmt.Errorf("failed to create backend: %w", err)
	}
	s.backend = backend

	// For Kubernetes mode, also create the legacy clients for agent listing etc.
	if opMode == mode.ModeKubernetes && cfg.KubeConfig != nil {
		k8sClient, err := client.NewClient(cfg.KubeConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
		}
		s.k8sClient = k8sClient

		clientset, err := kubernetes.NewForConfig(cfg.KubeConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create clientset: %w", err)
		}
		s.clientset = clientset
	}

	// Create Prometheus client if configured
	if cfg.PrometheusEndpoint != "" {
		s.prometheusClient = prometheus.NewClient(cfg.PrometheusEndpoint)
	}

	return s, nil
}

// Start starts the web UI server
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Mode endpoint
	mux.HandleFunc("/api/v1/mode", s.handleMode)

	// API routes - use method-aware handlers
	mux.HandleFunc("/api/v1/gateways", s.handleGatewaysWithWrite)
	mux.HandleFunc("/api/v1/gateways/", s.handleGatewayWithWrite)
	mux.HandleFunc("/api/v1/routes", s.handleRoutesWithWrite)
	mux.HandleFunc("/api/v1/routes/", s.handleRouteWithWrite)
	mux.HandleFunc("/api/v1/backends", s.handleBackendsWithWrite)
	mux.HandleFunc("/api/v1/backends/", s.handleBackendWithWrite)
	mux.HandleFunc("/api/v1/vips", s.handleVIPsWithWrite)
	mux.HandleFunc("/api/v1/vips/", s.handleVIPWithWrite)
	mux.HandleFunc("/api/v1/policies", s.handlePoliciesWithWrite)
	mux.HandleFunc("/api/v1/policies/", s.handlePolicyWithWrite)
	mux.HandleFunc("/api/v1/agents", s.handleAgents)
	mux.HandleFunc("/api/v1/metrics/dashboard", s.handleMetricsDashboard)
	mux.HandleFunc("/api/v1/metrics/query", s.handleMetricsQuery)
	mux.HandleFunc("/api/v1/namespaces", s.handleNamespaces)
	mux.HandleFunc("/api/v1/health", s.handleHealth)

	// Config management endpoints
	mux.HandleFunc("/api/v1/config/validate", s.handleConfigValidate)
	mux.HandleFunc("/api/v1/config/export", s.handleConfigExport)
	mux.HandleFunc("/api/v1/config/import", s.handleConfigImport)
	mux.HandleFunc("/api/v1/config/history", s.handleConfigHistory)
	mux.HandleFunc("/api/v1/config/history/", s.handleConfigHistoryRestore)

	// Static files with SPA fallback
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("failed to get static files: %w", err)
	}
	mux.Handle("/", spaHandler(http.FS(staticFS)))

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

// spaHandler serves static files and falls back to index.html for SPA routing
func spaHandler(fsys http.FileSystem) http.Handler {
	fileServer := http.FileServer(fsys)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Try to open the requested file
		f, err := fsys.Open(path)
		if err != nil {
			// File not found, serve index.html for SPA routing
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}

		// Check if it's a directory
		stat, err := f.Stat()
		f.Close()
		if err == nil && stat.IsDir() {
			// Try to serve index.html from the directory
			indexPath := strings.TrimSuffix(path, "/") + "/index.html"
			indexFile, err := fsys.Open(indexPath)
			if err != nil {
				// No index.html in directory, serve root index.html
				r.URL.Path = "/"
				fileServer.ServeHTTP(w, r)
				return
			}
			indexFile.Close()
		}

		// Serve the file
		fileServer.ServeHTTP(w, r)
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

// Method-aware handlers that dispatch to GET or write handlers

// handleGatewaysWithWrite dispatches to GET or POST handler
func (s *Server) handleGatewaysWithWrite(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGateways(w, r)
	case http.MethodPost:
		s.handleGatewayWrite(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleGatewayWithWrite dispatches to GET, PUT, or DELETE handler
func (s *Server) handleGatewayWithWrite(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGateway(w, r)
	case http.MethodPut, http.MethodDelete:
		s.handleGatewayWrite(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleRoutesWithWrite dispatches to GET or POST handler
func (s *Server) handleRoutesWithWrite(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleRoutes(w, r)
	case http.MethodPost:
		s.handleRouteWrite(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleRouteWithWrite dispatches to GET, PUT, or DELETE handler
func (s *Server) handleRouteWithWrite(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleRoute(w, r)
	case http.MethodPut, http.MethodDelete:
		s.handleRouteWrite(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleBackendsWithWrite dispatches to GET or POST handler
func (s *Server) handleBackendsWithWrite(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleBackends(w, r)
	case http.MethodPost:
		s.handleBackendWrite(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleBackendWithWrite dispatches to GET, PUT, or DELETE handler
func (s *Server) handleBackendWithWrite(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleBackend(w, r)
	case http.MethodPut, http.MethodDelete:
		s.handleBackendWrite(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleVIPsWithWrite dispatches to GET or POST handler
func (s *Server) handleVIPsWithWrite(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleVIPs(w, r)
	case http.MethodPost:
		s.handleVIPWrite(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleVIPWithWrite dispatches to GET, PUT, or DELETE handler
func (s *Server) handleVIPWithWrite(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleVIP(w, r)
	case http.MethodPut, http.MethodDelete:
		s.handleVIPWrite(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handlePoliciesWithWrite dispatches to GET or POST handler
func (s *Server) handlePoliciesWithWrite(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handlePolicies(w, r)
	case http.MethodPost:
		s.handlePolicyWrite(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handlePolicyWithWrite dispatches to GET, PUT, or DELETE handler
func (s *Server) handlePolicyWithWrite(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handlePolicy(w, r)
	case http.MethodPut, http.MethodDelete:
		s.handlePolicyWrite(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleGateways handles GET /api/v1/gateways
func (s *Server) handleGateways(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	ctx := r.Context()
	namespace := parseNamespaceFromQuery(r)

	gateways, err := s.backend.ListGateways(ctx, namespace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list gateways: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, gateways)
}

// handleGateway handles GET /api/v1/gateways/{namespace}/{name}
func (s *Server) handleGateway(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/gateways/"), "/")
	if len(parts) != 2 {
		writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/gateways/{namespace}/{name}")
		return
	}

	namespace, name := parts[0], parts[1]
	ctx := r.Context()

	gateway, err := s.backend.GetGateway(ctx, namespace, name)
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

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	ctx := r.Context()
	namespace := parseNamespaceFromQuery(r)

	routes, err := s.backend.ListRoutes(ctx, namespace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list routes: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, routes)
}

// handleRoute handles GET /api/v1/routes/{namespace}/{name}
func (s *Server) handleRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/routes/"), "/")
	if len(parts) != 2 {
		writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/routes/{namespace}/{name}")
		return
	}

	namespace, name := parts[0], parts[1]
	ctx := r.Context()

	route, err := s.backend.GetRoute(ctx, namespace, name)
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

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	ctx := r.Context()
	namespace := parseNamespaceFromQuery(r)

	backends, err := s.backend.ListBackends(ctx, namespace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list backends: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, backends)
}

// handleBackend handles GET /api/v1/backends/{namespace}/{name}
func (s *Server) handleBackend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/backends/"), "/")
	if len(parts) != 2 {
		writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/backends/{namespace}/{name}")
		return
	}

	namespace, name := parts[0], parts[1]
	ctx := r.Context()

	backend, err := s.backend.GetBackend(ctx, namespace, name)
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

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	ctx := r.Context()
	namespace := parseNamespaceFromQuery(r)

	vips, err := s.backend.ListVIPs(ctx, namespace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list vips: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, vips)
}

// handleVIP handles GET /api/v1/vips/{namespace}/{name}
func (s *Server) handleVIP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/vips/"), "/")
	if len(parts) != 2 {
		writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/vips/{namespace}/{name}")
		return
	}

	namespace, name := parts[0], parts[1]
	ctx := r.Context()

	vip, err := s.backend.GetVIP(ctx, namespace, name)
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

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	ctx := r.Context()
	namespace := parseNamespaceFromQuery(r)

	policies, err := s.backend.ListPolicies(ctx, namespace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list policies: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, policies)
}

// handlePolicy handles GET /api/v1/policies/{namespace}/{name}
func (s *Server) handlePolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/policies/"), "/")
	if len(parts) != 2 {
		writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/policies/{namespace}/{name}")
		return
	}

	namespace, name := parts[0], parts[1]
	ctx := r.Context()

	policy, err := s.backend.GetPolicy(ctx, namespace, name)
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

	// In standalone mode, return empty list or mock agent
	if s.backend != nil && s.backend.Mode() == mode.ModeStandalone {
		// Return a single "standalone" agent
		agents := []AgentInfo{{
			Name:      "standalone-agent",
			Namespace: "standalone",
			NodeName:  "localhost",
			PodIP:     "127.0.0.1",
			Phase:     "Running",
			Ready:     true,
		}}
		writeJSON(w, http.StatusOK, agents)
		return
	}

	if s.clientset == nil {
		writeError(w, http.StatusServiceUnavailable, "kubernetes client not initialized")
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

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	ctx := r.Context()

	namespaces, err := s.backend.ListNamespaces(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list namespaces: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, namespaces)
}

// handleHealth handles GET /api/v1/health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
}

// WorkerMetrics represents resource metrics for a single worker/agent
type WorkerMetrics struct {
	Instance     string  `json:"instance"`
	CPUUsage     float64 `json:"cpuUsage"`     // CPU usage percentage (0-100)
	MemoryUsage  float64 `json:"memoryUsage"`  // Memory usage in bytes
	MemoryLimit  float64 `json:"memoryLimit"`  // Memory limit in bytes (if set)
	Goroutines   float64 `json:"goroutines"`   // Number of goroutines
	Uptime       float64 `json:"uptime"`       // Uptime in seconds
	RequestsRate float64 `json:"requestsRate"` // Requests per second for this worker
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
	// Resource metrics (totals across all workers)
	TotalCPUUsage    *float64 `json:"totalCpuUsage,omitempty"`    // Total CPU usage percentage
	TotalMemoryUsage *float64 `json:"totalMemoryUsage,omitempty"` // Total memory usage in bytes
	TotalGoroutines  *float64 `json:"totalGoroutines,omitempty"`  // Total goroutines across all workers
	// Per-worker metrics
	Workers   []WorkerMetrics `json:"workers,omitempty"`
	Timestamp int64           `json:"timestamp"`
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

	// Query request rate (use novaedge_http_requests_total metric)
	if result, err := s.prometheusClient.Query(ctx, `sum(rate(novaedge_http_requests_total[5m]))`); err == nil && len(result.Data.Result) > 0 {
		if value, err := prometheus.ValueAsFloat(result.Data.Result[0]); err == nil {
			metrics.RequestRate = &value
		}
	}

	// Query active connections (use novaedge_backend_active_connections or novaedge_http_requests_in_flight)
	if result, err := s.prometheusClient.Query(ctx, `sum(novaedge_backend_active_connections) or vector(0)`); err == nil && len(result.Data.Result) > 0 {
		if value, err := prometheus.ValueAsFloat(result.Data.Result[0]); err == nil {
			metrics.ActiveConnections = &value
		}
	}

	// Query error rate (use novaedge_http_requests_total with 5xx status)
	if result, err := s.prometheusClient.Query(ctx, `sum(rate(novaedge_http_requests_total{status=~"5.."}[5m])) or vector(0)`); err == nil && len(result.Data.Result) > 0 {
		if value, err := prometheus.ValueAsFloat(result.Data.Result[0]); err == nil {
			metrics.ErrorRate = &value
		}
	}

	// Query average latency (use novaedge_http_request_duration_seconds histogram)
	if result, err := s.prometheusClient.Query(ctx, `histogram_quantile(0.5, sum(rate(novaedge_http_request_duration_seconds_bucket[5m])) by (le)) or vector(0)`); err == nil && len(result.Data.Result) > 0 {
		if value, err := prometheus.ValueAsFloat(result.Data.Result[0]); err == nil {
			metrics.AvgLatency = &value
		}
	}

	// Query VIP failovers (no change, metric name is correct)
	if result, err := s.prometheusClient.Query(ctx, `sum(increase(novaedge_vip_failovers_total[24h])) or vector(0)`); err == nil && len(result.Data.Result) > 0 {
		if value, err := prometheus.ValueAsFloat(result.Data.Result[0]); err == nil {
			metrics.VIPFailovers = &value
		}
	}

	// Query healthy agents (use novaedge job name)
	if result, err := s.prometheusClient.Query(ctx, `count(up{job="novaedge"} == 1) or vector(0)`); err == nil && len(result.Data.Result) > 0 {
		if value, err := prometheus.ValueAsFloat(result.Data.Result[0]); err == nil {
			metrics.HealthyAgents = &value
		}
	}

	// Query total agents (use novaedge job name)
	if result, err := s.prometheusClient.Query(ctx, `count(up{job="novaedge"}) or vector(0)`); err == nil && len(result.Data.Result) > 0 {
		if value, err := prometheus.ValueAsFloat(result.Data.Result[0]); err == nil {
			metrics.TotalAgents = &value
		}
	}

	// Query total CPU usage (rate of process_cpu_seconds_total * 100 for percentage)
	if result, err := s.prometheusClient.Query(ctx, `sum(rate(process_cpu_seconds_total{job="novaedge"}[1m])) * 100`); err == nil && len(result.Data.Result) > 0 {
		if value, err := prometheus.ValueAsFloat(result.Data.Result[0]); err == nil {
			metrics.TotalCPUUsage = &value
		}
	}

	// Query total memory usage (resident memory)
	if result, err := s.prometheusClient.Query(ctx, `sum(process_resident_memory_bytes{job="novaedge"})`); err == nil && len(result.Data.Result) > 0 {
		if value, err := prometheus.ValueAsFloat(result.Data.Result[0]); err == nil {
			metrics.TotalMemoryUsage = &value
		}
	}

	// Query total goroutines
	if result, err := s.prometheusClient.Query(ctx, `sum(go_goroutines{job="novaedge"})`); err == nil && len(result.Data.Result) > 0 {
		if value, err := prometheus.ValueAsFloat(result.Data.Result[0]); err == nil {
			metrics.TotalGoroutines = &value
		}
	}

	// Query per-worker metrics
	metrics.Workers = s.queryWorkerMetrics(ctx)

	writeJSON(w, http.StatusOK, metrics)
}

// queryWorkerMetrics queries per-worker metrics from Prometheus
func (s *Server) queryWorkerMetrics(ctx context.Context) []WorkerMetrics {
	workers := []WorkerMetrics{}

	if s.prometheusClient == nil {
		return workers
	}

	// Get list of instances from up metric
	instancesResult, err := s.prometheusClient.Query(ctx, `up{job="novaedge"}`)
	if err != nil || len(instancesResult.Data.Result) == 0 {
		return workers
	}

	// Build a map of instance -> WorkerMetrics
	workerMap := make(map[string]*WorkerMetrics)
	for _, result := range instancesResult.Data.Result {
		if instance, ok := result.Metric["instance"]; ok {
			workerMap[instance] = &WorkerMetrics{Instance: instance}
		}
	}

	// Query CPU usage per instance
	if result, err := s.prometheusClient.Query(ctx, `rate(process_cpu_seconds_total{job="novaedge"}[1m]) * 100`); err == nil {
		for _, r := range result.Data.Result {
			if instance, ok := r.Metric["instance"]; ok {
				if w, exists := workerMap[instance]; exists {
					if val, err := prometheus.ValueAsFloat(r); err == nil {
						w.CPUUsage = val
					}
				}
			}
		}
	}

	// Query memory usage per instance
	if result, err := s.prometheusClient.Query(ctx, `process_resident_memory_bytes{job="novaedge"}`); err == nil {
		for _, r := range result.Data.Result {
			if instance, ok := r.Metric["instance"]; ok {
				if w, exists := workerMap[instance]; exists {
					if val, err := prometheus.ValueAsFloat(r); err == nil {
						w.MemoryUsage = val
					}
				}
			}
		}
	}

	// Query goroutines per instance
	if result, err := s.prometheusClient.Query(ctx, `go_goroutines{job="novaedge"}`); err == nil {
		for _, r := range result.Data.Result {
			if instance, ok := r.Metric["instance"]; ok {
				if w, exists := workerMap[instance]; exists {
					if val, err := prometheus.ValueAsFloat(r); err == nil {
						w.Goroutines = val
					}
				}
			}
		}
	}

	// Query uptime per instance (current time - start time)
	if result, err := s.prometheusClient.Query(ctx, `time() - process_start_time_seconds{job="novaedge"}`); err == nil {
		for _, r := range result.Data.Result {
			if instance, ok := r.Metric["instance"]; ok {
				if w, exists := workerMap[instance]; exists {
					if val, err := prometheus.ValueAsFloat(r); err == nil {
						w.Uptime = val
					}
				}
			}
		}
	}

	// Query requests rate per instance
	if result, err := s.prometheusClient.Query(ctx, `sum by (instance) (rate(novaedge_http_requests_total{job="novaedge"}[5m]))`); err == nil {
		for _, r := range result.Data.Result {
			if instance, ok := r.Metric["instance"]; ok {
				if w, exists := workerMap[instance]; exists {
					if val, err := prometheus.ValueAsFloat(r); err == nil {
						w.RequestsRate = val
					}
				}
			}
		}
	}

	// Convert map to slice
	for _, w := range workerMap {
		workers = append(workers, *w)
	}

	return workers
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
