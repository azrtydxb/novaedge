// Package webui provides a web-based dashboard for NovaEdge.
package webui

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/azrtydxb/novaedge/cmd/novactl/pkg/client"
	"github.com/azrtydxb/novaedge/cmd/novactl/pkg/prometheus"
	"github.com/azrtydxb/novaedge/cmd/novactl/pkg/trace"
	"github.com/azrtydxb/novaedge/cmd/novactl/pkg/webui/auth"
	"github.com/azrtydxb/novaedge/cmd/novactl/pkg/webui/mode"
	"github.com/azrtydxb/novaedge/internal/acme"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
)

const (
	defaultNamespace = "nova-system"
)

// serverStartTime records when the process started, used for standalone agent info.
var serverStartTime = metav1.Now()

// Server represents the web UI server
type Server struct {
	addr             string
	k8sClient        *client.Client
	clientset        kubernetes.Interface
	prometheusClient *prometheus.Client
	backend          mode.Backend
	server           *http.Server
	tlsConfig        *tls.Config
	tlsCert          string
	tlsKey           string
	tlsAuto          bool
	authManager      *auth.Manager
	jaegerEndpoint   string
	traceClient      *trace.Client
	snapshotStore    *SnapshotStore
	crdClient        ctrlclient.Client
}

// Config holds server configuration
type Config struct {
	Address              string
	KubeConfig           *rest.Config
	PrometheusEndpoint   string
	Mode                 string // auto, kubernetes, standalone
	StandaloneConfigPath string
	ReadOnly             bool

	// TLS configuration
	TLSCert    string // Path to TLS certificate file
	TLSKey     string // Path to TLS key file
	TLSAuto    bool   // Auto-generate self-signed certificate
	ACMEEmail  string // Email for ACME/Let's Encrypt
	ACMEDomain string // Domain for ACME certificate

	// Authentication configuration
	AuthConfig auth.Config

	// Jaeger endpoint for trace viewing
	JaegerEndpoint string
}

// NewServer creates a new web UI server
func NewServer(cfg Config) (*Server, error) {
	s := &Server{
		addr:    cfg.Address,
		tlsCert: cfg.TLSCert,
		tlsKey:  cfg.TLSKey,
		tlsAuto: cfg.TLSAuto,
	}

	// Set up TLS if configured
	if err := s.setupTLS(cfg); err != nil {
		return nil, fmt.Errorf("failed to setup TLS: %w", err)
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

		// Create controller-runtime client for CRD access (SD-WAN etc.)
		crdScheme := runtime.NewScheme()
		if schemeErr := novaedgev1alpha1.AddToScheme(crdScheme); schemeErr == nil {
			crdCl, crdErr := ctrlclient.New(cfg.KubeConfig, ctrlclient.Options{Scheme: crdScheme})
			if crdErr == nil {
				s.crdClient = crdCl
			}
		}
	}

	// Create Prometheus client if configured
	if cfg.PrometheusEndpoint != "" {
		s.prometheusClient = prometheus.NewClient(cfg.PrometheusEndpoint)
	}

	// Initialize auth manager
	authMgr, err := auth.NewManager(cfg.AuthConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth manager: %w", err)
	}
	s.authManager = authMgr

	// Store Jaeger endpoint and create trace client if configured
	s.jaegerEndpoint = cfg.JaegerEndpoint
	if cfg.JaegerEndpoint != "" {
		s.traceClient = trace.NewClient(cfg.JaegerEndpoint)
	}

	// Initialize snapshot store
	s.snapshotStore = NewSnapshotStore(50)

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
	mux.HandleFunc("/api/v1/certificates", s.handleCertificatesWithWrite)
	mux.HandleFunc("/api/v1/certificates/", s.handleCertificateWithWrite)
	mux.HandleFunc("/api/v1/ippools", s.handleIPPoolsWithWrite)
	mux.HandleFunc("/api/v1/ippools/", s.handleIPPoolWithWrite)
	mux.HandleFunc("/api/v1/clusters", s.handleClustersWithWrite)
	mux.HandleFunc("/api/v1/clusters/", s.handleClusterWithWrite)
	mux.HandleFunc("/api/v1/federations", s.handleFederationsWithWrite)
	mux.HandleFunc("/api/v1/federations/", s.handleFederationWithWrite)
	mux.HandleFunc("/api/v1/remoteclusters", s.handleRemoteClustersWithWrite)
	mux.HandleFunc("/api/v1/remoteclusters/", s.handleRemoteClusterWithWrite)
	mux.HandleFunc("/api/v1/agents", s.handleAgents)
	mux.HandleFunc("/api/v1/metrics/dashboard", s.handleMetricsDashboard)
	mux.HandleFunc("/api/v1/metrics/query", s.handleMetricsQuery)
	mux.HandleFunc("/api/v1/namespaces", s.handleNamespaces)
	mux.HandleFunc("/api/v1/health", s.handleHealth)

	// Auth endpoints
	mux.HandleFunc("/api/v1/auth/login", s.handleAuthLogin)
	mux.HandleFunc("/api/v1/auth/logout", s.handleAuthLogout)
	mux.HandleFunc("/api/v1/auth/session", s.handleAuthSession)

	// Config management endpoints
	mux.HandleFunc("/api/v1/config/validate", s.handleConfigValidate)
	mux.HandleFunc("/api/v1/config/export", s.handleConfigExport)
	mux.HandleFunc("/api/v1/config/import", s.handleConfigImport)
	mux.HandleFunc("/api/v1/config/history", s.handleConfigHistory)
	mux.HandleFunc("/api/v1/config/history/", s.handleConfigHistoryRestore)

	// Observability endpoints (more specific paths must be registered first)
	mux.HandleFunc("/api/v1/traces/services", s.handleTraceServices)
	mux.HandleFunc("/api/v1/traces/operations", s.handleTraceOperations)
	mux.HandleFunc("/api/v1/traces/", s.handleTrace)
	mux.HandleFunc("/api/v1/traces", s.handleTraces)
	mux.HandleFunc("/api/v1/logs", s.handleLogs)
	mux.HandleFunc("/api/v1/events", s.handleEvents)
	mux.HandleFunc("/api/v1/waf/events", s.handleWAFEvents)
	mux.HandleFunc("/api/v1/mesh/status", s.handleMeshStatus)
	mux.HandleFunc("/api/v1/mesh/topology", s.handleMeshTopology)
	mux.HandleFunc("/api/v1/mesh/policies/", s.handleMeshPolicy)
	mux.HandleFunc("/api/v1/mesh/policies", s.handleMeshPolicies)

	// Overload / load-shedding endpoints
	mux.HandleFunc("/api/v1/overload/status", s.handleOverloadStatus)
	mux.HandleFunc("/api/v1/overload/config", s.handleOverloadConfig)

	// SD-WAN endpoints
	mux.HandleFunc("/api/v1/sdwan/links", s.handleSDWANLinks)
	mux.HandleFunc("/api/v1/sdwan/topology", s.handleSDWANTopology)
	mux.HandleFunc("/api/v1/sdwan/policies", s.handleSDWANPolicies)
	mux.HandleFunc("/api/v1/sdwan/events", s.handleSDWANEvents)

	// Config snapshot endpoints
	mux.HandleFunc("/api/v1/config/snapshots/", s.handleConfigSnapshot)
	mux.HandleFunc("/api/v1/config/snapshots", s.handleConfigSnapshots)
	mux.HandleFunc("/api/v1/config/diff", s.handleConfigDiff)
	mux.HandleFunc("/api/v1/config/rollback/", s.handleConfigRollback)

	// Root handler returns API info (static files served by nginx sidecar)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"service": "novaedge-webui-api",
			"version": "v1",
			"docs":    "/api/v1/",
		})
	})

	// Wrap with auth middleware, then CORS
	var handler http.Handler = mux
	if s.authManager != nil {
		handler = s.authManager.Middleware(handler)
	}

	s.server = &http.Server{
		Addr:              s.addr,
		Handler:           corsMiddleware(handler),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Use TLS if configured
	if s.tlsConfig != nil {
		s.server.TLSConfig = s.tlsConfig
		fmt.Printf("Starting NovaEdge Web UI at https://%s\n", s.addr)
		// When TLS config has certificates, we pass empty strings to ListenAndServeTLS
		// because the certificates are already configured in tlsConfig
		return s.server.ListenAndServeTLS("", "")
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

// setupTLS configures TLS for the server
func (s *Server) setupTLS(cfg Config) error {
	// No TLS if no configuration provided
	if cfg.TLSCert == "" && cfg.TLSKey == "" && !cfg.TLSAuto {
		return nil
	}

	// Manual certificate
	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			return fmt.Errorf("failed to load certificate: %w", err)
		}
		s.tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
		return nil
	}

	// Auto-generate self-signed certificate
	if cfg.TLSAuto {
		domains := []string{"localhost"}
		if cfg.ACMEDomain != "" {
			domains = append(domains, cfg.ACMEDomain)
		}

		cert, err := acme.GenerateSelfSigned(&acme.SelfSignedConfig{
			Domains:      domains,
			Organization: "NovaEdge Web UI",
			Validity:     365 * 24 * time.Hour,
		})
		if err != nil {
			return fmt.Errorf("failed to generate self-signed certificate: %w", err)
		}

		tlsCert, err := cert.TLSCertificate()
		if err != nil {
			return fmt.Errorf("failed to load self-signed certificate: %w", err)
		}

		s.tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{*tlsCert},
			MinVersion:   tls.VersionTLS12,
		}
		return nil
	}

	return nil
}

// IsTLSEnabled returns true if TLS is configured
func (s *Server) IsTLSEnabled() bool {
	return s.tlsConfig != nil
}

// corsMiddleware adds CORS headers for development
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
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

// handleCertificatesWithWrite dispatches to GET or POST handler
func (s *Server) handleCertificatesWithWrite(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleCertificates(w, r)
	case http.MethodPost:
		s.handleCertificateWrite(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleCertificateWithWrite dispatches to GET, PUT, or DELETE handler
func (s *Server) handleCertificateWithWrite(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleCertificate(w, r)
	case http.MethodPut, http.MethodDelete:
		s.handleCertificateWrite(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleIPPoolsWithWrite dispatches to GET or POST handler
func (s *Server) handleIPPoolsWithWrite(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleIPPools(w, r)
	case http.MethodPost:
		s.handleIPPoolWrite(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleIPPoolWithWrite dispatches to GET, PUT, or DELETE handler
func (s *Server) handleIPPoolWithWrite(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleIPPool(w, r)
	case http.MethodPut, http.MethodDelete:
		s.handleIPPoolWrite(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleClustersWithWrite dispatches to GET or POST handler
func (s *Server) handleClustersWithWrite(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleClusters(w, r)
	case http.MethodPost:
		s.handleClusterWrite(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleClusterWithWrite dispatches to GET, PUT, or DELETE handler
func (s *Server) handleClusterWithWrite(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleCluster(w, r)
	case http.MethodPut, http.MethodDelete:
		s.handleClusterWrite(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleFederationsWithWrite dispatches to GET or POST handler
func (s *Server) handleFederationsWithWrite(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleFederations(w, r)
	case http.MethodPost:
		s.handleFederationWrite(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleFederationWithWrite dispatches to GET, PUT, or DELETE handler
func (s *Server) handleFederationWithWrite(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleFederation(w, r)
	case http.MethodPut, http.MethodDelete:
		s.handleFederationWrite(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleRemoteClustersWithWrite dispatches to GET or POST handler
func (s *Server) handleRemoteClustersWithWrite(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleRemoteClusters(w, r)
	case http.MethodPost:
		s.handleRemoteClusterWrite(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleRemoteClusterWithWrite dispatches to GET, PUT, or DELETE handler
func (s *Server) handleRemoteClusterWithWrite(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleRemoteCluster(w, r)
	case http.MethodPut, http.MethodDelete:
		s.handleRemoteClusterWrite(w, r)
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

// handleCertificates handles GET /api/v1/certificates
func (s *Server) handleCertificates(w http.ResponseWriter, r *http.Request) {
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

	certificates, err := s.backend.ListCertificates(ctx, namespace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list certificates: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, certificates)
}

// handleCertificate handles GET /api/v1/certificates/{namespace}/{name}
func (s *Server) handleCertificate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/certificates/"), "/")
	if len(parts) != 2 {
		writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/certificates/{namespace}/{name}")
		return
	}

	namespace, name := parts[0], parts[1]
	ctx := r.Context()

	certificate, err := s.backend.GetCertificate(ctx, namespace, name)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("certificate not found: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, certificate)
}

// handleIPPools handles GET /api/v1/ippools
func (s *Server) handleIPPools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	ctx := r.Context()

	pools, err := s.backend.ListIPPools(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list IP pools: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, pools)
}

// handleIPPool handles GET /api/v1/ippools/{name}
func (s *Server) handleIPPool(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/v1/ippools/")
	if name == "" || strings.Contains(name, "/") {
		writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/ippools/{name}")
		return
	}

	ctx := r.Context()

	pool, err := s.backend.GetIPPool(ctx, name)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("IP pool not found: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, pool)
}

// handleClusters handles GET /api/v1/clusters
func (s *Server) handleClusters(w http.ResponseWriter, r *http.Request) {
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

	clusters, err := s.backend.ListNovaEdgeClusters(ctx, namespace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list clusters: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, clusters)
}

// handleCluster handles GET /api/v1/clusters/{namespace}/{name}
func (s *Server) handleCluster(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/clusters/"), "/")
	if len(parts) != 2 {
		writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/clusters/{namespace}/{name}")
		return
	}

	namespace, name := parts[0], parts[1]
	ctx := r.Context()

	cluster, err := s.backend.GetNovaEdgeCluster(ctx, namespace, name)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("cluster not found: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, cluster)
}

// handleFederations handles GET /api/v1/federations
func (s *Server) handleFederations(w http.ResponseWriter, r *http.Request) {
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

	federations, err := s.backend.ListFederations(ctx, namespace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list federations: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, federations)
}

// handleFederation handles GET /api/v1/federations/{namespace}/{name}
func (s *Server) handleFederation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/federations/"), "/")
	if len(parts) != 2 {
		writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/federations/{namespace}/{name}")
		return
	}

	namespace, name := parts[0], parts[1]
	ctx := r.Context()

	federation, err := s.backend.GetFederation(ctx, namespace, name)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("federation not found: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, federation)
}

// handleRemoteClusters handles GET /api/v1/remoteclusters
func (s *Server) handleRemoteClusters(w http.ResponseWriter, r *http.Request) {
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

	remoteClusters, err := s.backend.ListRemoteClusters(ctx, namespace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list remote clusters: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, remoteClusters)
}

// handleRemoteCluster handles GET /api/v1/remoteclusters/{namespace}/{name}
func (s *Server) handleRemoteCluster(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/remoteclusters/"), "/")
	if len(parts) != 2 {
		writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/remoteclusters/{namespace}/{name}")
		return
	}

	namespace, name := parts[0], parts[1]
	ctx := r.Context()

	remoteCluster, err := s.backend.GetRemoteCluster(ctx, namespace, name)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("remote cluster not found: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, remoteCluster)
}

// handleAgents handles GET /api/v1/agents
func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// In standalone mode, return agent info derived from the running process.
	if s.backend != nil && s.backend.Mode() == mode.ModeStandalone {
		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "localhost"
		}
		listenAddr := s.addr
		if listenAddr == "" {
			listenAddr = "127.0.0.1"
		}
		startTime := serverStartTime
		agents := []AgentInfo{{
			Name:      "standalone-agent",
			Namespace: "standalone",
			NodeName:  hostname,
			PodIP:     listenAddr,
			Phase:     "Running",
			Ready:     true,
			StartTime: &startTime,
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
		namespace = defaultNamespace
	}

	// List agent pods (support both common label conventions)
	pods, err := s.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=novaedge-agent",
	})
	if err == nil && len(pods.Items) == 0 {
		// Fallback to legacy label selector
		pods, err = s.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app=novaedge-agent",
		})
	}
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
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
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

// queryScalarMetric executes a Prometheus query and returns the first scalar result.
func (s *Server) queryScalarMetric(ctx context.Context, query string) *float64 {
	result, err := s.prometheusClient.Query(ctx, query)
	if err != nil || len(result.Data.Result) == 0 {
		return nil
	}
	value, err := prometheus.ValueAsFloat(result.Data.Result[0])
	if err != nil {
		return nil
	}
	return &value
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

	metrics.RequestRate = s.queryScalarMetric(ctx, `sum(rate(novaedge_http_requests_total[5m]))`)
	metrics.ActiveConnections = s.queryScalarMetric(ctx, `sum(novaedge_backend_active_connections) or vector(0)`)
	metrics.ErrorRate = s.queryScalarMetric(ctx, `sum(rate(novaedge_http_requests_total{status=~"5.."}[5m])) or vector(0)`)
	metrics.AvgLatency = s.queryScalarMetric(ctx, `histogram_quantile(0.5, sum(rate(novaedge_http_request_duration_seconds_bucket[5m])) by (le)) or vector(0)`)
	metrics.VIPFailovers = s.queryScalarMetric(ctx, `sum(increase(novaedge_vip_failovers_total[24h])) or vector(0)`)
	metrics.HealthyAgents = s.queryScalarMetric(ctx, `count(up{job="novaedge-agent-metrics"} == 1) or vector(0)`)
	metrics.TotalAgents = s.queryScalarMetric(ctx, `count(up{job="novaedge-agent-metrics"}) or vector(0)`)
	metrics.TotalCPUUsage = s.queryScalarMetric(ctx, `sum(rate(process_cpu_seconds_total{job=~"novaedge-agent-metrics|novaedge-controller"}[1m])) * 100`)
	metrics.TotalMemoryUsage = s.queryScalarMetric(ctx, `sum(process_resident_memory_bytes{job=~"novaedge-agent-metrics|novaedge-controller"})`)
	metrics.TotalGoroutines = s.queryScalarMetric(ctx, `sum(go_goroutines{job=~"novaedge-agent-metrics|novaedge-controller"})`)

	// Query per-worker metrics
	metrics.Workers = s.queryWorkerMetrics(ctx)

	writeJSON(w, http.StatusOK, metrics)
}

// populateWorkerMetric queries a Prometheus metric and populates the given field for each worker.
func (s *Server) populateWorkerMetric(ctx context.Context, workerMap map[string]*WorkerMetrics, query string, setter func(*WorkerMetrics, float64)) {
	result, err := s.prometheusClient.Query(ctx, query)
	if err != nil {
		return
	}
	for _, r := range result.Data.Result {
		if instance, ok := r.Metric["instance"]; ok {
			if w, exists := workerMap[instance]; exists {
				if val, valErr := prometheus.ValueAsFloat(r); valErr == nil {
					setter(w, val)
				}
			}
		}
	}
}

// queryWorkerMetrics queries per-worker metrics from Prometheus
func (s *Server) queryWorkerMetrics(ctx context.Context) []WorkerMetrics {
	workers := []WorkerMetrics{}

	if s.prometheusClient == nil {
		return workers
	}

	// Get list of instances from up metric (match actual Prometheus job names)
	instancesResult, err := s.prometheusClient.Query(ctx, `up{job=~"novaedge-agent-metrics|novaedge-controller"}`)
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

	const jobFilter = `{job=~"novaedge-agent-metrics|novaedge-controller"}`
	s.populateWorkerMetric(ctx, workerMap, `rate(process_cpu_seconds_total`+jobFilter+`[1m]) * 100`,
		func(w *WorkerMetrics, v float64) { w.CPUUsage = v })
	s.populateWorkerMetric(ctx, workerMap, `process_resident_memory_bytes`+jobFilter,
		func(w *WorkerMetrics, v float64) { w.MemoryUsage = v })
	s.populateWorkerMetric(ctx, workerMap, `go_goroutines`+jobFilter,
		func(w *WorkerMetrics, v float64) { w.Goroutines = v })
	s.populateWorkerMetric(ctx, workerMap, `time() - process_start_time_seconds`+jobFilter,
		func(w *WorkerMetrics, v float64) { w.Uptime = v })
	s.populateWorkerMetric(ctx, workerMap, `sum by (instance) (rate(novaedge_http_requests_total`+jobFilter+`[5m]))`,
		func(w *WorkerMetrics, v float64) { w.RequestsRate = v })

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

// loginRequest represents the JSON body for login.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"` //nolint:gosec // G117: struct field name for login request, not a hardcoded credential
}

// handleAuthLogin handles POST /api/v1/auth/login
func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	token, err := s.authManager.Login(req.Username, req.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "novaedge_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleAuthLogout handles POST /api/v1/auth/logout
func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	cookie, err := r.Cookie("novaedge_session")
	if err == nil && cookie.Value != "" {
		s.authManager.Logout(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "novaedge_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleAuthSession handles GET /api/v1/auth/session
func (s *Server) handleAuthSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	authEnabled := s.authManager.Enabled()
	authenticated := false

	if authEnabled {
		cookie, err := r.Cookie("novaedge_session")
		if err == nil && cookie.Value != "" {
			if validateErr := s.authManager.ValidateToken(cookie.Value); validateErr == nil {
				authenticated = true
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"authenticated": authenticated,
		"authEnabled":   authEnabled,
		"oidcEnabled":   s.authManager.OIDCEnabled(),
	})
}
