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
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/piwi3910/novaedge/cmd/novactl/pkg/webui/models"
)

// handleMode handles GET /api/v1/mode
func (s *Server) handleMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	response := map[string]interface{}{
		"mode":     string(s.backend.Mode()),
		"readOnly": s.backend.ReadOnly(),
	}

	writeJSON(w, http.StatusOK, response)
}

// handleGatewayWrite handles POST/PUT/DELETE for gateways
func (s *Server) handleGatewayWrite(w http.ResponseWriter, r *http.Request) {
	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/gateways")

	switch r.Method {
	case http.MethodPost:
		// Create gateway
		var gateway models.Gateway
		if err := json.NewDecoder(r.Body).Decode(&gateway); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		result, err := s.backend.CreateGateway(ctx, &gateway)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, result)

	case http.MethodPut:
		// Update gateway
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/gateways/{namespace}/{name}")
			return
		}

		var gateway models.Gateway
		if err := json.NewDecoder(r.Body).Decode(&gateway); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		gateway.Namespace = parts[0]
		gateway.Name = parts[1]

		result, err := s.backend.UpdateGateway(ctx, &gateway)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)

	case http.MethodDelete:
		// Delete gateway
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/gateways/{namespace}/{name}")
			return
		}

		if err := s.backend.DeleteGateway(ctx, parts[0], parts[1]); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleRouteWrite handles POST/PUT/DELETE for routes
func (s *Server) handleRouteWrite(w http.ResponseWriter, r *http.Request) {
	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/routes")

	switch r.Method {
	case http.MethodPost:
		var route models.Route
		if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		result, err := s.backend.CreateRoute(ctx, &route)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, result)

	case http.MethodPut:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/routes/{namespace}/{name}")
			return
		}

		var route models.Route
		if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		route.Namespace = parts[0]
		route.Name = parts[1]

		result, err := s.backend.UpdateRoute(ctx, &route)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)

	case http.MethodDelete:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/routes/{namespace}/{name}")
			return
		}

		if err := s.backend.DeleteRoute(ctx, parts[0], parts[1]); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleBackendWrite handles POST/PUT/DELETE for backends
func (s *Server) handleBackendWrite(w http.ResponseWriter, r *http.Request) {
	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/backends")

	switch r.Method {
	case http.MethodPost:
		var backend models.Backend
		if err := json.NewDecoder(r.Body).Decode(&backend); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		result, err := s.backend.CreateBackend(ctx, &backend)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, result)

	case http.MethodPut:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/backends/{namespace}/{name}")
			return
		}

		var backend models.Backend
		if err := json.NewDecoder(r.Body).Decode(&backend); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		backend.Namespace = parts[0]
		backend.Name = parts[1]

		result, err := s.backend.UpdateBackend(ctx, &backend)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)

	case http.MethodDelete:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/backends/{namespace}/{name}")
			return
		}

		if err := s.backend.DeleteBackend(ctx, parts[0], parts[1]); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleVIPWrite handles POST/PUT/DELETE for VIPs
func (s *Server) handleVIPWrite(w http.ResponseWriter, r *http.Request) {
	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/vips")

	switch r.Method {
	case http.MethodPost:
		var vip models.VIP
		if err := json.NewDecoder(r.Body).Decode(&vip); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		result, err := s.backend.CreateVIP(ctx, &vip)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, result)

	case http.MethodPut:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/vips/{namespace}/{name}")
			return
		}

		var vip models.VIP
		if err := json.NewDecoder(r.Body).Decode(&vip); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		vip.Namespace = parts[0]
		vip.Name = parts[1]

		result, err := s.backend.UpdateVIP(ctx, &vip)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)

	case http.MethodDelete:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/vips/{namespace}/{name}")
			return
		}

		if err := s.backend.DeleteVIP(ctx, parts[0], parts[1]); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handlePolicyWrite handles POST/PUT/DELETE for policies
func (s *Server) handlePolicyWrite(w http.ResponseWriter, r *http.Request) {
	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/policies")

	switch r.Method {
	case http.MethodPost:
		var policy models.Policy
		if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		result, err := s.backend.CreatePolicy(ctx, &policy)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, result)

	case http.MethodPut:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/policies/{namespace}/{name}")
			return
		}

		var policy models.Policy
		if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		policy.Namespace = parts[0]
		policy.Name = parts[1]

		result, err := s.backend.UpdatePolicy(ctx, &policy)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)

	case http.MethodDelete:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/policies/{namespace}/{name}")
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

// handleCertificateWrite handles POST/PUT/DELETE for certificates
func (s *Server) handleCertificateWrite(w http.ResponseWriter, r *http.Request) {
	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/certificates")

	switch r.Method {
	case http.MethodPost:
		var cert models.Certificate
		if err := json.NewDecoder(r.Body).Decode(&cert); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		result, err := s.backend.CreateCertificate(ctx, &cert)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, result)

	case http.MethodPut:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/certificates/{namespace}/{name}")
			return
		}

		var cert models.Certificate
		if err := json.NewDecoder(r.Body).Decode(&cert); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		cert.Namespace = parts[0]
		cert.Name = parts[1]

		result, err := s.backend.UpdateCertificate(ctx, &cert)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)

	case http.MethodDelete:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/certificates/{namespace}/{name}")
			return
		}

		if err := s.backend.DeleteCertificate(ctx, parts[0], parts[1]); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleIPPoolWrite handles POST/PUT/DELETE for IP pools
func (s *Server) handleIPPoolWrite(w http.ResponseWriter, r *http.Request) {
	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/ippools")

	switch r.Method {
	case http.MethodPost:
		var pool models.IPPool
		if err := json.NewDecoder(r.Body).Decode(&pool); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		result, err := s.backend.CreateIPPool(ctx, &pool)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, result)

	case http.MethodPut:
		name := strings.TrimPrefix(path, "/")
		if name == "" || strings.Contains(name, "/") {
			writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/ippools/{name}")
			return
		}

		var pool models.IPPool
		if err := json.NewDecoder(r.Body).Decode(&pool); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		pool.Name = name

		result, err := s.backend.UpdateIPPool(ctx, &pool)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)

	case http.MethodDelete:
		name := strings.TrimPrefix(path, "/")
		if name == "" || strings.Contains(name, "/") {
			writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/ippools/{name}")
			return
		}

		if err := s.backend.DeleteIPPool(ctx, name); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleClusterWrite handles POST/PUT/DELETE for NovaEdge clusters
func (s *Server) handleClusterWrite(w http.ResponseWriter, r *http.Request) {
	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/clusters")

	switch r.Method {
	case http.MethodPost:
		var cluster models.NovaEdgeClusterModel
		if err := json.NewDecoder(r.Body).Decode(&cluster); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		result, err := s.backend.CreateNovaEdgeCluster(ctx, &cluster)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, result)

	case http.MethodPut:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/clusters/{namespace}/{name}")
			return
		}

		var cluster models.NovaEdgeClusterModel
		if err := json.NewDecoder(r.Body).Decode(&cluster); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		cluster.Namespace = parts[0]
		cluster.Name = parts[1]

		result, err := s.backend.UpdateNovaEdgeCluster(ctx, &cluster)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)

	case http.MethodDelete:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/clusters/{namespace}/{name}")
			return
		}

		if err := s.backend.DeleteNovaEdgeCluster(ctx, parts[0], parts[1]); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleFederationWrite handles POST/PUT/DELETE for federations
func (s *Server) handleFederationWrite(w http.ResponseWriter, r *http.Request) {
	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/federations")

	switch r.Method {
	case http.MethodPost:
		var federation models.FederationModel
		if err := json.NewDecoder(r.Body).Decode(&federation); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		result, err := s.backend.CreateFederation(ctx, &federation)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, result)

	case http.MethodPut:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/federations/{namespace}/{name}")
			return
		}

		var federation models.FederationModel
		if err := json.NewDecoder(r.Body).Decode(&federation); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		federation.Namespace = parts[0]
		federation.Name = parts[1]

		result, err := s.backend.UpdateFederation(ctx, &federation)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)

	case http.MethodDelete:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/federations/{namespace}/{name}")
			return
		}

		if err := s.backend.DeleteFederation(ctx, parts[0], parts[1]); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleRemoteClusterWrite handles POST/PUT/DELETE for remote clusters
func (s *Server) handleRemoteClusterWrite(w http.ResponseWriter, r *http.Request) {
	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/remoteclusters")

	switch r.Method {
	case http.MethodPost:
		var rc models.RemoteClusterModel
		if err := json.NewDecoder(r.Body).Decode(&rc); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		result, err := s.backend.CreateRemoteCluster(ctx, &rc)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, result)

	case http.MethodPut:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/remoteclusters/{namespace}/{name}")
			return
		}

		var rc models.RemoteClusterModel
		if err := json.NewDecoder(r.Body).Decode(&rc); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		rc.Namespace = parts[0]
		rc.Name = parts[1]

		result, err := s.backend.UpdateRemoteCluster(ctx, &rc)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)

	case http.MethodDelete:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/remoteclusters/{namespace}/{name}")
			return
		}

		if err := s.backend.DeleteRemoteCluster(ctx, parts[0], parts[1]); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleConfigValidate handles POST /api/v1/config/validate
func (s *Server) handleConfigValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	var config models.Config
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	ctx := r.Context()
	if err := s.backend.ValidateConfig(ctx, &config); err != nil {
		writeJSON(w, http.StatusOK, models.ValidationResult{
			Valid: false,
			Errors: []models.ValidationError{{
				Field:   "config",
				Message: err.Error(),
			}},
		})
		return
	}

	writeJSON(w, http.StatusOK, models.ValidationResult{Valid: true})
}

// handleConfigExport handles POST /api/v1/config/export
func (s *Server) handleConfigExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "all"
	}

	ctx := r.Context()
	data, err := s.backend.ExportConfig(ctx, namespace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/x-yaml")
	w.Header().Set("Content-Disposition", "attachment; filename=novaedge-config.yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// handleConfigImport handles POST /api/v1/config/import
func (s *Server) handleConfigImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	dryRun := r.URL.Query().Get("dryRun") == "true"

	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body: "+err.Error())
		return
	}

	ctx := r.Context()
	result, err := s.backend.ImportConfig(ctx, data, dryRun)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// HistoryEntry represents a configuration change history entry
type HistoryEntry struct {
	ID           string `json:"id"`
	Timestamp    string `json:"timestamp"`
	Type         string `json:"type"`         // create, update, delete
	ResourceType string `json:"resourceType"` // gateway, route, backend, vip, policy
	ResourceName string `json:"resourceName"`
	Namespace    string `json:"namespace"`
	Snapshot     string `json:"snapshot,omitempty"`
}

// handleConfigHistory handles GET /api/v1/config/history
func (s *Server) handleConfigHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	snapshots := s.snapshotStore.List()
	history := make([]HistoryEntry, 0, len(snapshots))
	for _, snap := range snapshots {
		history = append(history, HistoryEntry{
			ID:        snap.ID,
			Timestamp: snap.Timestamp.Format(time.RFC3339),
			Type:      "snapshot",
			Snapshot:  snap.Comment,
		})
	}

	writeJSON(w, http.StatusOK, history)
}

// handleConfigHistoryRestore handles POST /api/v1/config/history/{id}/restore
func (s *Server) handleConfigHistoryRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "no backend configured")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/config/history/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[1] != "restore" {
		writeError(w, http.StatusBadRequest, "invalid path: expected /api/v1/config/history/{id}/restore")
		return
	}
	snapshotID := parts[0]

	snapshot := s.snapshotStore.Get(snapshotID)
	if snapshot == nil {
		writeError(w, http.StatusNotFound, "snapshot not found: "+snapshotID)
		return
	}

	result, err := s.backend.ImportConfig(r.Context(), []byte(snapshot.Config), false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to restore configuration: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":    "configuration restored from snapshot",
		"snapshotId": snapshotID,
		"timestamp":  snapshot.Timestamp.Format(time.RFC3339),
		"result":     result,
	})
}
