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
