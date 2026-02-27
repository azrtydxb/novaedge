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
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/piwi3910/novaedge/cmd/novactl/pkg/webui/models"
)

// namespacedResource bundles the backend operations for a namespaced resource
// so they can be served by a single generic handler (handleNamespacedCRUD).
type namespacedResource[T any] struct {
	pathPrefix string
	create     func(ctx context.Context, obj *T) (*T, error)
	update     func(ctx context.Context, obj *T) (*T, error)
	delete     func(ctx context.Context, ns, name string) error
	setKey     func(obj *T, ns, name string)
}

// handleNamespacedCRUD is a generic POST/PUT/DELETE handler for namespaced resources.
func handleNamespacedCRUD[T any](s *Server, w http.ResponseWriter, r *http.Request, res namespacedResource[T]) {
	if s.backend == nil {
		writeError(w, http.StatusServiceUnavailable, "backend not initialized")
		return
	}

	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, res.pathPrefix)

	switch r.Method {
	case http.MethodPost:
		var obj T
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		result, err := res.create(ctx, &obj)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, result)

	case http.MethodPut:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid path: expected "+res.pathPrefix+"/{namespace}/{name}")
			return
		}

		var obj T
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		res.setKey(&obj, parts[0], parts[1])

		result, err := res.update(ctx, &obj)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)

	case http.MethodDelete:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) != 2 {
			writeError(w, http.StatusBadRequest, "invalid path: expected "+res.pathPrefix+"/{namespace}/{name}")
			return
		}

		if err := res.delete(ctx, parts[0], parts[1]); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

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
	handleNamespacedCRUD(s, w, r, namespacedResource[models.Gateway]{
		pathPrefix: "/api/v1/gateways",
		create:     s.backend.CreateGateway,
		update:     s.backend.UpdateGateway,
		delete:     s.backend.DeleteGateway,
		setKey:     func(g *models.Gateway, ns, name string) { g.Namespace = ns; g.Name = name },
	})
}

// handleRouteWrite handles POST/PUT/DELETE for routes
func (s *Server) handleRouteWrite(w http.ResponseWriter, r *http.Request) {
	handleNamespacedCRUD(s, w, r, namespacedResource[models.Route]{
		pathPrefix: "/api/v1/routes",
		create:     s.backend.CreateRoute,
		update:     s.backend.UpdateRoute,
		delete:     s.backend.DeleteRoute,
		setKey:     func(rt *models.Route, ns, name string) { rt.Namespace = ns; rt.Name = name },
	})
}

// handleBackendWrite handles POST/PUT/DELETE for backends
func (s *Server) handleBackendWrite(w http.ResponseWriter, r *http.Request) {
	handleNamespacedCRUD(s, w, r, namespacedResource[models.Backend]{
		pathPrefix: "/api/v1/backends",
		create:     s.backend.CreateBackend,
		update:     s.backend.UpdateBackend,
		delete:     s.backend.DeleteBackend,
		setKey:     func(b *models.Backend, ns, name string) { b.Namespace = ns; b.Name = name },
	})
}

// handleVIPWrite handles POST/PUT/DELETE for VIPs
func (s *Server) handleVIPWrite(w http.ResponseWriter, r *http.Request) {
	handleNamespacedCRUD(s, w, r, namespacedResource[models.VIP]{
		pathPrefix: "/api/v1/vips",
		create:     s.backend.CreateVIP,
		update:     s.backend.UpdateVIP,
		delete:     s.backend.DeleteVIP,
		setKey:     func(v *models.VIP, ns, name string) { v.Namespace = ns; v.Name = name },
	})
}

// handlePolicyWrite handles POST/PUT/DELETE for policies
func (s *Server) handlePolicyWrite(w http.ResponseWriter, r *http.Request) {
	handleNamespacedCRUD(s, w, r, namespacedResource[models.Policy]{
		pathPrefix: "/api/v1/policies",
		create:     s.backend.CreatePolicy,
		update:     s.backend.UpdatePolicy,
		delete:     s.backend.DeletePolicy,
		setKey:     func(p *models.Policy, ns, name string) { p.Namespace = ns; p.Name = name },
	})
}

// handleCertificateWrite handles POST/PUT/DELETE for certificates
func (s *Server) handleCertificateWrite(w http.ResponseWriter, r *http.Request) {
	handleNamespacedCRUD(s, w, r, namespacedResource[models.Certificate]{
		pathPrefix: "/api/v1/certificates",
		create:     s.backend.CreateCertificate,
		update:     s.backend.UpdateCertificate,
		delete:     s.backend.DeleteCertificate,
		setKey:     func(c *models.Certificate, ns, name string) { c.Namespace = ns; c.Name = name },
	})
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
	handleNamespacedCRUD(s, w, r, namespacedResource[models.NovaEdgeClusterModel]{
		pathPrefix: "/api/v1/clusters",
		create:     s.backend.CreateNovaEdgeCluster,
		update:     s.backend.UpdateNovaEdgeCluster,
		delete:     s.backend.DeleteNovaEdgeCluster,
		setKey:     func(c *models.NovaEdgeClusterModel, ns, name string) { c.Namespace = ns; c.Name = name },
	})
}

// handleFederationWrite handles POST/PUT/DELETE for federations
func (s *Server) handleFederationWrite(w http.ResponseWriter, r *http.Request) {
	handleNamespacedCRUD(s, w, r, namespacedResource[models.FederationModel]{
		pathPrefix: "/api/v1/federations",
		create:     s.backend.CreateFederation,
		update:     s.backend.UpdateFederation,
		delete:     s.backend.DeleteFederation,
		setKey:     func(f *models.FederationModel, ns, name string) { f.Namespace = ns; f.Name = name },
	})
}

// handleRemoteClusterWrite handles POST/PUT/DELETE for remote clusters
func (s *Server) handleRemoteClusterWrite(w http.ResponseWriter, r *http.Request) {
	handleNamespacedCRUD(s, w, r, namespacedResource[models.RemoteClusterModel]{
		pathPrefix: "/api/v1/remoteclusters",
		create:     s.backend.CreateRemoteCluster,
		update:     s.backend.UpdateRemoteCluster,
		delete:     s.backend.DeleteRemoteCluster,
		setKey:     func(rc *models.RemoteClusterModel, ns, name string) { rc.Namespace = ns; rc.Name = name },
	})
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
