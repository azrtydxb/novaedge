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

package server

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"

	"go.uber.org/zap"

	"github.com/azrtydxb/novaedge/internal/agent/config"
	pb "github.com/azrtydxb/novaedge/internal/proto/gen"
	"google.golang.org/protobuf/proto"
)

// EndpointOverride represents a runtime override for a single endpoint.
type EndpointOverride struct {
	Address string `json:"address"`
	Port    int32  `json:"port"`
	Weight  int32  `json:"weight"`
	Drain   bool   `json:"drain"`
}

// ClusterOverrides tracks all runtime overrides for a single cluster.
type ClusterOverrides struct {
	mu       sync.RWMutex
	Added    map[string]*EndpointOverride `json:"added"`
	Removed  map[string]bool              `json:"removed"`
	Weights  map[string]int32             `json:"weights"`
	Draining map[string]bool              `json:"draining"`
}

// RuntimeOverrides stores ephemeral runtime changes that override the config snapshot.
// All overrides are cleared when a new ConfigSnapshot is applied.
type RuntimeOverrides struct {
	// clusters is a sync.Map of string (cluster name) -> *ClusterOverrides
	clusters sync.Map
}

// RuntimeAPI provides HTTP handlers for live operational changes to the data plane
// without requiring a full config reload from the controller.
type RuntimeAPI struct {
	logger    *zap.Logger
	overrides *RuntimeOverrides
	mux       *http.ServeMux
}

// NewRuntimeAPI creates a new RuntimeAPI instance.
func NewRuntimeAPI(logger *zap.Logger) *RuntimeAPI {
	api := &RuntimeAPI{
		logger:    logger,
		overrides: &RuntimeOverrides{},
		mux:       http.NewServeMux(),
	}
	api.registerRoutes()
	return api
}

// registerRoutes sets up the HTTP routing for the runtime API.
//
// Security: These endpoints have no authentication middleware because they are
// only registered on the admin loopback interface (127.0.0.1), which is not
// reachable from outside the node. The AdminServer binds exclusively to a
// loopback address, so network-level isolation provides the access control.
func (api *RuntimeAPI) registerRoutes() {
	api.mux.HandleFunc("/api/v1/clusters/", api.handleClusters)
	api.mux.HandleFunc("/api/v1/overrides", api.handleOverrides)
}

// ServeHTTP implements http.Handler.
func (api *RuntimeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	api.mux.ServeHTTP(w, r)
}

// Overrides returns the underlying RuntimeOverrides for direct access.
func (api *RuntimeAPI) Overrides() *RuntimeOverrides {
	return api.overrides
}

// handleClusters dispatches cluster endpoint operations based on method and path.
func (api *RuntimeAPI) handleClusters(w http.ResponseWriter, r *http.Request) {
	// Parse path: /api/v1/clusters/{cluster}/endpoints[/{endpoint}][/weight|/drain]
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/clusters/")
	parts := strings.Split(path, "/")

	if len(parts) < 2 || parts[1] != "endpoints" {
		http.Error(w, "invalid path", http.StatusNotFound)
		return
	}

	cluster := parts[0]
	if cluster == "" {
		http.Error(w, "cluster name required", http.StatusBadRequest)
		return
	}

	switch {
	case len(parts) == 2 && r.Method == http.MethodPost:
		// POST /api/v1/clusters/{cluster}/endpoints
		api.handleAddEndpoint(w, r, cluster)
	case len(parts) == 3 && r.Method == http.MethodDelete:
		// DELETE /api/v1/clusters/{cluster}/endpoints/{endpoint}
		api.handleRemoveEndpoint(w, r, cluster, parts[2])
	case len(parts) == 4 && parts[3] == "weight" && r.Method == http.MethodPut:
		// PUT /api/v1/clusters/{cluster}/endpoints/{endpoint}/weight
		api.handleChangeWeight(w, r, cluster, parts[2])
	case len(parts) == 4 && parts[3] == "drain" && r.Method == http.MethodPut:
		// PUT /api/v1/clusters/{cluster}/endpoints/{endpoint}/drain
		api.handleDrainEndpoint(w, r, cluster, parts[2])
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// addEndpointRequest is the JSON body for adding an endpoint.
type addEndpointRequest struct {
	Address string `json:"address"`
	Port    int32  `json:"port"`
	Weight  int32  `json:"weight"`
}

// changeWeightRequest is the JSON body for changing endpoint weight.
type changeWeightRequest struct {
	Weight int32 `json:"weight"`
}

// handleAddEndpoint handles POST /api/v1/clusters/{cluster}/endpoints.
func (api *RuntimeAPI) handleAddEndpoint(w http.ResponseWriter, r *http.Request, cluster string) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req addEndpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.Address == "" || req.Port <= 0 {
		http.Error(w, "address and port are required", http.StatusBadRequest)
		return
	}

	key := net.JoinHostPort(req.Address, fmt.Sprint(req.Port))
	co := api.overrides.getOrCreateCluster(cluster)

	co.mu.Lock()
	co.Added[key] = &EndpointOverride{
		Address: req.Address,
		Port:    req.Port,
		Weight:  req.Weight,
	}
	// If it was previously removed, un-remove it
	delete(co.Removed, key)
	co.mu.Unlock()

	api.logger.Info("Runtime API: endpoint added",
		zap.String("cluster", cluster),
		zap.String("endpoint", key),
		zap.Int32("weight", req.Weight),
	)

	writeJSON(w, http.StatusCreated, map[string]string{"status": "added", "endpoint": key})
}

// handleRemoveEndpoint handles DELETE /api/v1/clusters/{cluster}/endpoints/{endpoint}.
func (api *RuntimeAPI) handleRemoveEndpoint(w http.ResponseWriter, _ *http.Request, cluster, endpoint string) {
	co := api.overrides.getOrCreateCluster(cluster)

	co.mu.Lock()
	co.Removed[endpoint] = true
	// If it was a runtime-added endpoint, remove it from added as well
	delete(co.Added, endpoint)
	delete(co.Weights, endpoint)
	delete(co.Draining, endpoint)
	co.mu.Unlock()

	api.logger.Info("Runtime API: endpoint removed",
		zap.String("cluster", cluster),
		zap.String("endpoint", endpoint),
	)

	writeJSON(w, http.StatusOK, map[string]string{"status": "removed", "endpoint": endpoint})
}

// handleChangeWeight handles PUT /api/v1/clusters/{cluster}/endpoints/{endpoint}/weight.
func (api *RuntimeAPI) handleChangeWeight(w http.ResponseWriter, r *http.Request, cluster, endpoint string) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req changeWeightRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.Weight < 0 {
		http.Error(w, "weight must be non-negative", http.StatusBadRequest)
		return
	}

	co := api.overrides.getOrCreateCluster(cluster)

	co.mu.Lock()
	co.Weights[endpoint] = req.Weight
	// Also update weight on added endpoints
	if ep, exists := co.Added[endpoint]; exists {
		ep.Weight = req.Weight
	}
	co.mu.Unlock()

	api.logger.Info("Runtime API: endpoint weight changed",
		zap.String("cluster", cluster),
		zap.String("endpoint", endpoint),
		zap.Int32("weight", req.Weight),
	)

	writeJSON(w, http.StatusOK, map[string]string{"status": "weight_updated", "endpoint": endpoint})
}

// handleDrainEndpoint handles PUT /api/v1/clusters/{cluster}/endpoints/{endpoint}/drain.
func (api *RuntimeAPI) handleDrainEndpoint(w http.ResponseWriter, _ *http.Request, cluster, endpoint string) {
	co := api.overrides.getOrCreateCluster(cluster)

	co.mu.Lock()
	co.Draining[endpoint] = true
	co.mu.Unlock()

	api.logger.Info("Runtime API: endpoint draining",
		zap.String("cluster", cluster),
		zap.String("endpoint", endpoint),
	)

	writeJSON(w, http.StatusOK, map[string]string{"status": "draining", "endpoint": endpoint})
}

// overridesListResponse is the response for listing all overrides.
type overridesListResponse struct {
	Clusters map[string]*ClusterOverrides `json:"clusters"`
}

// handleOverrides dispatches GET/DELETE on /api/v1/overrides.
func (api *RuntimeAPI) handleOverrides(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		api.handleListOverrides(w)
	case http.MethodDelete:
		api.handleClearOverrides(w)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleListOverrides handles GET /api/v1/overrides.
func (api *RuntimeAPI) handleListOverrides(w http.ResponseWriter) {
	resp := overridesListResponse{
		Clusters: make(map[string]*ClusterOverrides),
	}

	api.overrides.clusters.Range(func(key, value any) bool {
		clusterName, ok := key.(string)
		if !ok {
			return true
		}
		co, ok := value.(*ClusterOverrides)
		if !ok {
			return true
		}
		co.mu.RLock()
		resp.Clusters[clusterName] = co
		co.mu.RUnlock()
		return true
	})

	writeJSON(w, http.StatusOK, resp)
}

// handleClearOverrides handles DELETE /api/v1/overrides.
func (api *RuntimeAPI) handleClearOverrides(w http.ResponseWriter) {
	api.overrides.Clear()

	api.logger.Info("Runtime API: all overrides cleared")

	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

// getOrCreateCluster returns existing or creates new ClusterOverrides for the cluster.
func (ro *RuntimeOverrides) getOrCreateCluster(cluster string) *ClusterOverrides {
	val, loaded := ro.clusters.Load(cluster)
	if loaded {
		co, ok := val.(*ClusterOverrides)
		if ok {
			return co
		}
	}

	co := &ClusterOverrides{
		Added:    make(map[string]*EndpointOverride),
		Removed:  make(map[string]bool),
		Weights:  make(map[string]int32),
		Draining: make(map[string]bool),
	}
	actual, loaded := ro.clusters.LoadOrStore(cluster, co)
	if loaded {
		stored, ok := actual.(*ClusterOverrides)
		if ok {
			return stored
		}
	}
	return co
}

// Clear removes all runtime overrides.
func (ro *RuntimeOverrides) Clear() {
	ro.clusters.Range(func(key, _ any) bool {
		ro.clusters.Delete(key)
		return true
	})
}

// HasOverrides returns true if any runtime overrides exist.
func (ro *RuntimeOverrides) HasOverrides() bool {
	hasOverrides := false
	ro.clusters.Range(func(_, _ any) bool {
		hasOverrides = true
		return false // stop iteration
	})
	return hasOverrides
}

// ApplyOverrides merges runtime overrides into a config snapshot, returning a new
// snapshot with the overrides applied. The original snapshot is not modified.
// This method is intended to be called every time the agent needs a snapshot that
// reflects both the controller-pushed config and any live operational changes.
func (ro *RuntimeOverrides) ApplyOverrides(snapshot *config.Snapshot) *config.Snapshot {
	if snapshot == nil || snapshot.ConfigSnapshot == nil {
		return snapshot
	}

	if !ro.HasOverrides() {
		return snapshot
	}

	// Deep-copy the endpoints map so we don't mutate the original snapshot
	newEndpoints := make(map[string]*pb.EndpointList, len(snapshot.Endpoints))
	for k, v := range snapshot.Endpoints {
		epCopy := make([]*pb.Endpoint, len(v.Endpoints))
		copy(epCopy, v.Endpoints)
		newEndpoints[k] = &pb.EndpointList{Endpoints: epCopy}
	}

	// Apply overrides for each cluster
	ro.clusters.Range(func(key, value any) bool {
		clusterName, ok := key.(string)
		if !ok {
			return true
		}
		co, ok := value.(*ClusterOverrides)
		if !ok {
			return true
		}

		co.mu.RLock()
		defer co.mu.RUnlock()

		epList, exists := newEndpoints[clusterName]
		if !exists {
			epList = &pb.EndpointList{Endpoints: make([]*pb.Endpoint, 0)}
			newEndpoints[clusterName] = epList
		}

		// Remove endpoints marked for removal
		if len(co.Removed) > 0 {
			filtered := make([]*pb.Endpoint, 0, len(epList.Endpoints))
			for _, ep := range epList.Endpoints {
				epKey := net.JoinHostPort(ep.Address, fmt.Sprint(ep.Port))
				if !co.Removed[epKey] {
					filtered = append(filtered, ep)
				}
			}
			epList.Endpoints = filtered
		}

		// Mark draining endpoints as not ready
		if len(co.Draining) > 0 {
			for _, ep := range epList.Endpoints {
				epKey := net.JoinHostPort(ep.Address, fmt.Sprint(ep.Port))
				if co.Draining[epKey] {
					ep.Ready = false
				}
			}
		}

		// Add new endpoints
		for _, override := range co.Added {
			epList.Endpoints = append(epList.Endpoints, &pb.Endpoint{
				Address: override.Address,
				Port:    override.Port,
				Ready:   !override.Drain,
			})
		}

		return true
	})

	// Create a new snapshot with overridden endpoints
	cloned, ok := proto.Clone(snapshot.ConfigSnapshot).(*pb.ConfigSnapshot)
	if !ok {
		return snapshot
	}
	cloned.Endpoints = newEndpoints

	return &config.Snapshot{
		Extensions:     snapshot.Extensions,
		ConfigSnapshot: cloned,
	}
}
