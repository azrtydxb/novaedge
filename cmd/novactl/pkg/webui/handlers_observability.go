/*
Copyright 2024 NovaEdge Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package webui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/piwi3910/novaedge/cmd/novactl/pkg/prometheus"
	"github.com/piwi3910/novaedge/cmd/novactl/pkg/trace"
	"github.com/piwi3910/novaedge/cmd/novactl/pkg/webui/models"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// handleTraces handles GET /api/v1/traces - search traces
func (s *Server) handleTraces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.traceClient == nil {
		writeError(w, http.StatusServiceUnavailable, "tracing not configured")
		return
	}

	ctx := r.Context()
	q := r.URL.Query()

	// Parse search parameters
	params := trace.SearchParams{
		ServiceName:   q.Get("service"),
		OperationName: q.Get("operation"),
	}

	// Jaeger requires a service parameter; return empty list if not provided
	if params.ServiceName == "" {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}

	// Parse limit (default 20)
	limit := 20
	if limitStr := q.Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	params.Limit = limit

	// Parse lookback (default 1h)
	lookback := time.Hour
	if lookbackStr := q.Get("lookback"); lookbackStr != "" {
		if parsed, err := time.ParseDuration(lookbackStr); err == nil {
			lookback = parsed
		}
	}
	params.EndTime = time.Now()
	params.StartTime = time.Now().Add(-lookback)

	// Parse minDuration
	if minDur := q.Get("minDuration"); minDur != "" {
		if parsed, err := time.ParseDuration(minDur); err == nil {
			params.MinDuration = parsed
		}
	}

	// Parse maxDuration
	if maxDur := q.Get("maxDuration"); maxDur != "" {
		if parsed, err := time.ParseDuration(maxDur); err == nil {
			params.MaxDuration = parsed
		}
	}

	traces, err := s.traceClient.SearchTraces(ctx, params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to search traces: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, traces)
}

// handleTrace handles GET /api/v1/traces/{traceID} - get single trace
func (s *Server) handleTrace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.traceClient == nil {
		writeError(w, http.StatusServiceUnavailable, "tracing not configured")
		return
	}

	// Extract traceID from path: /api/v1/traces/{traceID}
	traceID := strings.TrimPrefix(r.URL.Path, "/api/v1/traces/")
	if traceID == "" || strings.Contains(traceID, "/") {
		writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/traces/{traceID}")
		return
	}

	ctx := r.Context()
	t, err := s.traceClient.GetTrace(ctx, traceID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("trace not found: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, t)
}

// handleTraceServices handles GET /api/v1/traces/services - list services
func (s *Server) handleTraceServices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.traceClient == nil {
		writeError(w, http.StatusServiceUnavailable, "tracing not configured")
		return
	}

	ctx := r.Context()
	services, err := s.traceClient.GetServices(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get services: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, services)
}

// handleTraceOperations handles GET /api/v1/traces/operations?service= - list operations for service
func (s *Server) handleTraceOperations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.traceClient == nil {
		writeError(w, http.StatusServiceUnavailable, "tracing not configured")
		return
	}

	service := r.URL.Query().Get("service")
	if service == "" {
		writeError(w, http.StatusBadRequest, "service parameter is required")
		return
	}

	ctx := r.Context()
	operations, err := s.traceClient.GetOperations(ctx, service)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get operations: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, operations)
}

// handleLogs handles GET /api/v1/logs?pod=&namespace=&tailLines=&follow=
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.clientset == nil {
		writeError(w, http.StatusServiceUnavailable, "kubernetes client not initialized")
		return
	}

	q := r.URL.Query()
	pod := q.Get("pod")
	if pod == "" {
		writeError(w, http.StatusBadRequest, "pod parameter is required")
		return
	}

	namespace := q.Get("namespace")
	if namespace == "" {
		namespace = "novaedge-system"
	}

	// Parse tailLines (default 100)
	var tailLines int64 = 100
	if tailStr := q.Get("tailLines"); tailStr != "" {
		if parsed, err := strconv.ParseInt(tailStr, 10, 64); err == nil && parsed > 0 {
			tailLines = parsed
		}
	}

	follow := q.Get("follow") == "true"

	logOptions := &corev1.PodLogOptions{
		TailLines: &tailLines,
		Follow:    follow,
	}

	if container := q.Get("container"); container != "" {
		logOptions.Container = container
	}

	ctx := r.Context()
	logStream, err := s.clientset.CoreV1().Pods(namespace).GetLogs(pod, logOptions).Stream(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get pod logs: %v", err))
		return
	}
	defer func() { _ = logStream.Close() }()

	if follow {
		// For follow mode, use Server-Sent Events for streaming
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming not supported")
			return
		}

		buf := make([]byte, 4096)
		for {
			n, readErr := logStream.Read(buf)
			if n > 0 {
				_, writeErr := fmt.Fprintf(w, "data: %s\n\n", strings.TrimRight(string(buf[:n]), "\n"))
				if writeErr != nil {
					return
				}
				flusher.Flush()
			}
			if readErr != nil {
				return
			}
		}
	} else {
		// Non-follow: return plain text
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, logStream)
	}
}

// EventInfo represents a Kubernetes event for the API response
type EventInfo struct {
	Timestamp          time.Time `json:"timestamp"`
	Type               string    `json:"type"`
	Reason             string    `json:"reason"`
	Message            string    `json:"message"`
	InvolvedObjectName string    `json:"involvedObjectName"`
	InvolvedObjectKind string    `json:"involvedObjectKind"`
	Namespace          string    `json:"namespace"`
}

// handleEvents handles GET /api/v1/events?namespace=&involved=
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.clientset == nil {
		writeError(w, http.StatusServiceUnavailable, "kubernetes client not initialized")
		return
	}

	q := r.URL.Query()
	namespace := q.Get("namespace")
	if namespace == "" {
		namespace = "novaedge-system"
	}
	involved := q.Get("involved")

	ctx := r.Context()
	listOpts := metav1.ListOptions{}

	// If involved is set, use field selector for efficient server-side filtering
	if involved != "" {
		listOpts.FieldSelector = fmt.Sprintf("involvedObject.name=%s", involved)
	}

	events, err := s.clientset.CoreV1().Events(namespace).List(ctx, listOpts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list events: %v", err))
		return
	}

	result := make([]EventInfo, 0, len(events.Items))
	for i := range events.Items {
		ev := &events.Items[i]
		ts := ev.LastTimestamp.Time
		if ts.IsZero() {
			ts = ev.CreationTimestamp.Time
		}

		result = append(result, EventInfo{
			Timestamp:          ts,
			Type:               ev.Type,
			Reason:             ev.Reason,
			Message:            ev.Message,
			InvolvedObjectName: ev.InvolvedObject.Name,
			InvolvedObjectKind: ev.InvolvedObject.Kind,
			Namespace:          ev.Namespace,
		})
	}

	// Sort by timestamp descending
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.After(result[j].Timestamp)
	})

	writeJSON(w, http.StatusOK, result)
}

// WAFEventsSummary represents WAF events data for the API response
type WAFEventsSummary struct {
	TotalRequests float64       `json:"totalRequests"`
	BlockedTotal  float64       `json:"blockedTotal"`
	TopRules      []WAFRuleInfo `json:"topRules"`
}

// WAFRuleInfo represents a WAF rule with its block count
type WAFRuleInfo struct {
	RuleID  string  `json:"ruleId"`
	RuleMsg string  `json:"ruleMsg"`
	Count   float64 `json:"count"`
}

// handleWAFEvents handles GET /api/v1/waf/events?limit=&action=
func (s *Server) handleWAFEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.prometheusClient == nil {
		writeError(w, http.StatusServiceUnavailable, "prometheus not configured")
		return
	}

	ctx := r.Context()
	summary := WAFEventsSummary{
		TopRules: []WAFRuleInfo{},
	}

	// Query total WAF requests
	if result, err := s.prometheusClient.Query(ctx, `sum(novaedge_waf_requests_total) or vector(0)`); err == nil && len(result.Data.Result) > 0 {
		if value, err := prometheus.ValueAsFloat(result.Data.Result[0]); err == nil {
			summary.TotalRequests = value
		}
	}

	// Query total WAF blocked
	if result, err := s.prometheusClient.Query(ctx, `sum(novaedge_waf_blocked_total) or vector(0)`); err == nil && len(result.Data.Result) > 0 {
		if value, err := prometheus.ValueAsFloat(result.Data.Result[0]); err == nil {
			summary.BlockedTotal = value
		}
	}

	// Query top rules by block count
	if result, err := s.prometheusClient.Query(ctx, `topk(10, sum by (rule_id, rule_msg) (novaedge_waf_blocked_total))`); err == nil {
		for _, r := range result.Data.Result {
			value, valErr := prometheus.ValueAsFloat(r)
			if valErr != nil {
				continue
			}
			summary.TopRules = append(summary.TopRules, WAFRuleInfo{
				RuleID:  r.Metric["rule_id"],
				RuleMsg: r.Metric["rule_msg"],
				Count:   value,
			})
		}
	}

	writeJSON(w, http.StatusOK, summary)
}

// MeshServiceInfo represents a mesh-enabled service
type MeshServiceInfo struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	SpiffeID    string `json:"spiffeId"`
	MeshEnabled bool   `json:"meshEnabled"`
	MtlsStatus  string `json:"mtlsStatus"`
}

// MeshStatusResponse represents the mesh status response
type MeshStatusResponse struct {
	Services      []MeshServiceInfo `json:"services"`
	TotalServices int               `json:"totalServices"`
	MtlsEnabled   int               `json:"mtlsEnabled"`
}

// handleMeshStatus handles GET /api/v1/mesh/status
func (s *Server) handleMeshStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.clientset == nil {
		writeError(w, http.StatusServiceUnavailable, "kubernetes client not initialized")
		return
	}

	ctx := r.Context()

	// Query pods with mesh annotation
	pods, err := s.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list pods: %v", err))
		return
	}

	serviceMap := make(map[string]*MeshServiceInfo)
	for i := range pods.Items {
		pod := &pods.Items[i]
		meshEnabled := pod.Annotations["novaedge.io/mesh"] == "enabled" ||
			pod.Labels["novaedge.io/mesh"] == "enabled"

		if !meshEnabled {
			continue
		}

		// Use service name from label, or pod name as fallback
		svcName := pod.Labels["app"]
		if svcName == "" {
			svcName = pod.Labels["app.kubernetes.io/name"]
		}
		if svcName == "" {
			svcName = pod.Name
		}

		key := fmt.Sprintf("%s/%s", pod.Namespace, svcName)
		if _, exists := serviceMap[key]; exists {
			continue
		}

		spiffeID := pod.Annotations["novaedge.io/spiffe-id"]
		mtlsStatus := "disabled"
		if spiffeID != "" {
			mtlsStatus = "active"
		}

		serviceMap[key] = &MeshServiceInfo{
			Name:        svcName,
			Namespace:   pod.Namespace,
			SpiffeID:    spiffeID,
			MeshEnabled: true,
			MtlsStatus:  mtlsStatus,
		}
	}

	services := make([]MeshServiceInfo, 0, len(serviceMap))
	mtlsCount := 0
	for _, svc := range serviceMap {
		services = append(services, *svc)
		if svc.MtlsStatus == "active" {
			mtlsCount++
		}
	}

	writeJSON(w, http.StatusOK, MeshStatusResponse{
		Services:      services,
		TotalServices: len(services),
		MtlsEnabled:   mtlsCount,
	})
}

// MeshTopologyResponse represents the mesh topology for D3 visualization
type MeshTopologyResponse struct {
	Nodes []MeshNode `json:"nodes"`
	Edges []MeshEdge `json:"edges"`
}

// MeshNode represents a node in the mesh topology
type MeshNode struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	SpiffeID  string `json:"spiffeId"`
}

// MeshEdge represents an edge (connection) in the mesh topology
type MeshEdge struct {
	Source  string  `json:"source"`
	Target  string  `json:"target"`
	Mtls    bool    `json:"mtls"`
	Traffic float64 `json:"traffic"`
}

// handleMeshTopology handles GET /api/v1/mesh/topology
func (s *Server) handleMeshTopology(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.clientset == nil {
		writeError(w, http.StatusServiceUnavailable, "kubernetes client not initialized")
		return
	}

	ctx := r.Context()

	// Get mesh-enabled pods to build nodes
	pods, err := s.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list pods: %v", err))
		return
	}

	nodeMap := make(map[string]*MeshNode)
	for i := range pods.Items {
		pod := &pods.Items[i]
		meshEnabled := pod.Annotations["novaedge.io/mesh"] == "enabled" ||
			pod.Labels["novaedge.io/mesh"] == "enabled"

		if !meshEnabled {
			continue
		}

		svcName := pod.Labels["app"]
		if svcName == "" {
			svcName = pod.Labels["app.kubernetes.io/name"]
		}
		if svcName == "" {
			svcName = pod.Name
		}

		nodeID := fmt.Sprintf("%s/%s", pod.Namespace, svcName)
		if _, exists := nodeMap[nodeID]; exists {
			continue
		}

		nodeMap[nodeID] = &MeshNode{
			ID:        nodeID,
			Name:      svcName,
			Namespace: pod.Namespace,
			SpiffeID:  pod.Annotations["novaedge.io/spiffe-id"],
		}
	}

	nodes := make([]MeshNode, 0, len(nodeMap))
	for _, node := range nodeMap {
		nodes = append(nodes, *node)
	}

	// Build edges from Prometheus metrics if available
	edges := []MeshEdge{}
	if s.prometheusClient != nil {
		if result, err := s.prometheusClient.Query(ctx,
			`sum by (source_service, source_namespace, target_service, target_namespace) (rate(novaedge_mesh_requests_total[5m]))`); err == nil {
			for _, r := range result.Data.Result {
				sourceID := fmt.Sprintf("%s/%s", r.Metric["source_namespace"], r.Metric["source_service"])
				targetID := fmt.Sprintf("%s/%s", r.Metric["target_namespace"], r.Metric["target_service"])

				traffic, _ := prometheus.ValueAsFloat(r)

				// Only include edges where both source and target are known mesh services
				if _, srcOK := nodeMap[sourceID]; srcOK {
					if _, tgtOK := nodeMap[targetID]; tgtOK {
						edges = append(edges, MeshEdge{
							Source:  sourceID,
							Target:  targetID,
							Mtls:    true,
							Traffic: traffic,
						})
					}
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, MeshTopologyResponse{
		Nodes: nodes,
		Edges: edges,
	})
}

// handleMeshPolicies handles GET /api/v1/mesh/policies and POST /api/v1/mesh/policies
func (s *Server) handleMeshPolicies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleMeshPoliciesList(w, r)
	case http.MethodPost:
		s.handleMeshPolicyWrite(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleMeshPolicy handles GET/PUT/DELETE /api/v1/mesh/policies/{namespace}/{name}
func (s *Server) handleMeshPolicy(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleMeshPolicyGet(w, r)
	case http.MethodPut, http.MethodDelete:
		s.handleMeshPolicyWrite(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleMeshPoliciesList handles GET /api/v1/mesh/policies
func (s *Server) handleMeshPoliciesList(w http.ResponseWriter, r *http.Request) {
	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	ctx := r.Context()
	namespace := parseNamespaceFromQuery(r)

	// List ProxyPolicy resources and filter for mesh authorization type
	policies, err := s.backend.ListPolicies(ctx, namespace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list mesh policies: %v", err))
		return
	}

	// Filter for policies that are of mesh authorization type
	meshPolicies := make([]map[string]interface{}, 0)
	for _, p := range policies {
		if p.Type == "MeshAuthorization" {
			meshPolicies = append(meshPolicies, map[string]interface{}{
				"apiVersion": "novaedge.io/v1alpha1",
				"kind":       "MeshAuthorizationPolicy",
				"metadata": map[string]interface{}{
					"name":      p.Name,
					"namespace": p.Namespace,
				},
				"spec": map[string]interface{}{
					"targetRef": p.TargetRef,
				},
			})
		}
	}

	writeJSON(w, http.StatusOK, meshPolicies)
}

// handleMeshPolicyGet handles GET /api/v1/mesh/policies/{namespace}/{name}
func (s *Server) handleMeshPolicyGet(w http.ResponseWriter, r *http.Request) {
	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/mesh/policies/"), "/")
	if len(parts) != 2 {
		writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/mesh/policies/{namespace}/{name}")
		return
	}

	namespace, name := parts[0], parts[1]
	ctx := r.Context()

	policy, err := s.backend.GetPolicy(ctx, namespace, name)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("mesh policy not found: %v", err))
		return
	}

	result := map[string]interface{}{
		"apiVersion": "novaedge.io/v1alpha1",
		"kind":       "MeshAuthorizationPolicy",
		"metadata": map[string]interface{}{
			"name":      policy.Name,
			"namespace": policy.Namespace,
		},
		"spec": map[string]interface{}{
			"type":      policy.Type,
			"targetRef": policy.TargetRef,
			"rateLimit": policy.RateLimit,
			"cors":      policy.CORS,
			"ipFilter":  policy.IPFilter,
			"jwt":       policy.JWT,
		},
	}

	writeJSON(w, http.StatusOK, result)
}

// handleMeshPolicyWrite handles POST/PUT/DELETE for mesh policies
func (s *Server) handleMeshPolicyWrite(w http.ResponseWriter, r *http.Request) {
	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	if s.backend.ReadOnly() {
		writeError(w, http.StatusForbidden, "backend is read-only")
		return
	}

	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/mesh/policies")

	switch r.Method {
	case http.MethodPost:
		var resource map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&resource); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		policy := meshResourceToPolicy(resource)
		result, err := s.backend.CreatePolicy(ctx, policy)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, result)

	case http.MethodPut:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid path")
			return
		}

		var resource map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&resource); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		policy := meshResourceToPolicy(resource)
		policy.Namespace = parts[0]
		policy.Name = parts[1]

		result, err := s.backend.UpdatePolicy(ctx, policy)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)

	case http.MethodDelete:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid path")
			return
		}

		if err := s.backend.DeletePolicy(ctx, parts[0], parts[1]); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// meshResourceToPolicy converts a generic mesh policy resource to a Policy model
func meshResourceToPolicy(resource map[string]interface{}) *models.Policy {
	policy := &models.Policy{
		Type: "MeshAuthorization",
	}

	if metadata, ok := resource["metadata"].(map[string]interface{}); ok {
		if name, ok := metadata["name"].(string); ok {
			policy.Name = name
		}
		if ns, ok := metadata["namespace"].(string); ok {
			policy.Namespace = ns
		}
	}

	return policy
}
