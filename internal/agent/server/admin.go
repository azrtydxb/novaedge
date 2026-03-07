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

// Package server provides HTTP/HTTPS/HTTP3 server implementations, TLS configuration,
// PROXY protocol support, OCSP stapling, and overload management for the NovaEdge agent.
//
// DEPRECATED: This package will be removed once --forwarding-plane=rust is
// validated and the Rust dataplane handles all HTTP serving natively.
// See docs/plans/forwarding-deprecation.md for the removal timeline.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/pprof"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/azrtydxb/novaedge/internal/agent/config"
)

// DefaultAdminAddr is the default listen address for the admin API.
const DefaultAdminAddr = "127.0.0.1:9901"

// AdminServer provides admin/debug HTTP endpoints for the NovaEdge agent.
// It exposes health, stats, config, route, and cluster information as well
// as a dynamic log-level endpoint.
type AdminServer struct {
	logger *zap.Logger
	addr   string
	server *http.Server

	mu        sync.RWMutex
	ready     atomic.Bool
	snapshot  *config.Snapshot
	startedAt time.Time

	// logLevel is the current zap AtomicLevel used by the agent.
	// When non-nil the PUT /logging endpoint can change the level at runtime.
	logLevel zap.AtomicLevel
}

// NewAdminServer creates a new admin/debug HTTP server.
// If addr is empty, DefaultAdminAddr is used.
func NewAdminServer(addr string, logger *zap.Logger) *AdminServer {
	if addr == "" {
		addr = DefaultAdminAddr
	}
	return &AdminServer{
		addr:      addr,
		logger:    logger,
		startedAt: time.Now(),
	}
}

// SetAtomicLevel configures the zap AtomicLevel that the PUT /logging
// endpoint will modify. If not set, the endpoint returns 501 Not Implemented.
func (a *AdminServer) SetAtomicLevel(lvl zap.AtomicLevel) {
	a.logLevel = lvl
}

// SetReady marks the agent as ready or not ready.
func (a *AdminServer) SetReady(ready bool) {
	a.ready.Store(ready)
}

// SetSnapshot atomically stores the active config snapshot for introspection.
func (a *AdminServer) SetSnapshot(snap *config.Snapshot) {
	a.mu.Lock()
	a.snapshot = snap
	a.mu.Unlock()
}

// getSnapshot returns the current snapshot (may be nil).
func (a *AdminServer) getSnapshot() *config.Snapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.snapshot
}

// Start starts the admin HTTP server and blocks until the context is cancelled.
func (a *AdminServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/ready", a.handleReady)
	mux.HandleFunc("/health", a.handleHealth)
	mux.HandleFunc("/stats", a.handleStats)
	mux.HandleFunc("/clusters", a.handleClusters)
	mux.HandleFunc("/config", a.handleConfig)
	mux.HandleFunc("/routes", a.handleRoutes)
	mux.HandleFunc("/logging", a.handleLogging)

	// pprof endpoints for CPU/memory profiling during load tests.
	// AdminServer binds to 127.0.0.1 only, so these are not externally accessible.
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	a.server = &http.Server{
		Addr:              a.addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      65 * time.Second, // allow ?seconds=60 CPU profiles to complete
		IdleTimeout:       60 * time.Second,
	}

	a.logger.Info("Starting admin API server", zap.String("addr", a.addr))

	go func() {
		if err := a.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.logger.Error("Admin server error", zap.Error(err))
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a.logger.Info("Shutting down admin API server")
	if err := a.server.Shutdown(shutdownCtx); err != nil { //nolint:contextcheck // shutdown context intentionally derived from context.Background() after parent cancellation
		return err
	}
	return nil
}

// Shutdown gracefully shuts down the admin server.
func (a *AdminServer) Shutdown(ctx context.Context) error {
	if a.server == nil {
		return nil
	}
	a.logger.Info("Shutting down admin API server")
	return a.server.Shutdown(ctx)
}

// --------------------------------------------------------------------------
// Handlers
// --------------------------------------------------------------------------

func (a *AdminServer) handleReady(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ready": a.ready.Load()})
}

func (a *AdminServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	snap := a.getSnapshot()
	configVersion := ""
	if snap != nil && snap.ConfigSnapshot != nil {
		configVersion = snap.GetVersion()
	}

	resp := map[string]interface{}{
		"uptime_seconds":  time.Since(a.startedAt).Seconds(),
		"config_version":  configVersion,
		"goroutine_count": runtime.NumGoroutine(),
		"memory": map[string]interface{}{
			"alloc_bytes":       memStats.Alloc,
			"total_alloc_bytes": memStats.TotalAlloc,
			"sys_bytes":         memStats.Sys,
			"num_gc":            memStats.NumGC,
		},
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *AdminServer) handleStats(w http.ResponseWriter, _ *http.Request) {
	snap := a.getSnapshot()

	totalEndpoints := 0
	totalClusters := 0
	totalRoutes := 0

	if snap != nil && snap.ConfigSnapshot != nil {
		totalClusters = len(snap.GetClusters())
		totalRoutes = len(snap.GetRoutes())
		for _, epList := range snap.GetEndpoints() {
			totalEndpoints += len(epList.GetEndpoints())
		}
	}

	resp := map[string]interface{}{
		"total_clusters":  totalClusters,
		"total_routes":    totalRoutes,
		"total_endpoints": totalEndpoints,
		"goroutine_count": runtime.NumGoroutine(),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *AdminServer) handleClusters(w http.ResponseWriter, _ *http.Request) {
	snap := a.getSnapshot()

	type endpointInfo struct {
		Address string `json:"address"`
		Port    int32  `json:"port"`
		Ready   bool   `json:"ready"`
	}

	type clusterInfo struct {
		Name      string         `json:"name"`
		Namespace string         `json:"namespace"`
		LBPolicy  string         `json:"lb_policy"`
		Endpoints []endpointInfo `json:"endpoints"`
	}

	clusters := make([]clusterInfo, 0)

	if snap != nil && snap.ConfigSnapshot != nil {
		endpoints := snap.GetEndpoints()

		for _, c := range snap.GetClusters() {
			ci := clusterInfo{
				Name:      c.GetName(),
				Namespace: c.GetNamespace(),
				LBPolicy:  c.GetLbPolicy().String(),
				Endpoints: make([]endpointInfo, 0),
			}

			clusterKey := c.GetNamespace() + "/" + c.GetName()
			if epList, ok := endpoints[clusterKey]; ok {
				for _, ep := range epList.GetEndpoints() {
					ci.Endpoints = append(ci.Endpoints, endpointInfo{
						Address: ep.GetAddress(),
						Port:    ep.GetPort(),
						Ready:   ep.GetReady(),
					})
				}
			}

			clusters = append(clusters, ci)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"clusters": clusters})
}

func (a *AdminServer) handleConfig(w http.ResponseWriter, _ *http.Request) {
	snap := a.getSnapshot()

	resp := map[string]interface{}{
		"version":          "",
		"num_routes":       0,
		"num_clusters":     0,
		"num_policies":     0,
		"num_gateways":     0,
		"num_vips":         0,
		"num_l4_listeners": 0,
	}

	if snap != nil && snap.ConfigSnapshot != nil {
		resp["version"] = snap.GetVersion()
		resp["num_routes"] = len(snap.GetRoutes())
		resp["num_clusters"] = len(snap.GetClusters())
		resp["num_policies"] = len(snap.GetPolicies())
		resp["num_gateways"] = len(snap.GetGateways())
		resp["num_vips"] = len(snap.GetVipAssignments())
		resp["num_l4_listeners"] = len(snap.GetL4Listeners())
	}

	writeJSON(w, http.StatusOK, resp)
}

func (a *AdminServer) handleRoutes(w http.ResponseWriter, _ *http.Request) {
	snap := a.getSnapshot()

	type backendInfo struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		Weight    int32  `json:"weight"`
	}

	type ruleInfo struct {
		PathType string        `json:"path_type,omitempty"`
		Path     string        `json:"path,omitempty"`
		Method   string        `json:"method,omitempty"`
		Backends []backendInfo `json:"backends"`
	}

	type routeInfo struct {
		Name      string     `json:"name"`
		Namespace string     `json:"namespace"`
		Rules     []ruleInfo `json:"rules"`
	}

	// routeTable maps hostname -> list of routes
	routeTable := make(map[string][]routeInfo)

	if snap != nil && snap.ConfigSnapshot != nil {
		for _, r := range snap.GetRoutes() {
			ri := routeInfo{
				Name:      r.GetName(),
				Namespace: r.GetNamespace(),
				Rules:     make([]ruleInfo, 0),
			}

			for _, rule := range r.GetRules() {
				ruleEntry := ruleInfo{
					Backends: make([]backendInfo, 0),
				}

				// Extract first match info
				if matches := rule.GetMatches(); len(matches) > 0 {
					m := matches[0]
					if m.GetPath() != nil {
						ruleEntry.PathType = m.GetPath().GetType().String()
						ruleEntry.Path = m.GetPath().GetValue()
					}
					ruleEntry.Method = m.GetMethod()
				}

				for _, br := range rule.GetBackendRefs() {
					ruleEntry.Backends = append(ruleEntry.Backends, backendInfo{
						Name:      br.GetName(),
						Namespace: br.GetNamespace(),
						Weight:    br.GetWeight(),
					})
				}

				ri.Rules = append(ri.Rules, ruleEntry)
			}

			hostnames := r.GetHostnames()
			if len(hostnames) == 0 {
				hostnames = []string{"*"}
			}
			for _, h := range hostnames {
				routeTable[h] = append(routeTable[h], ri)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"routes": routeTable})
}

func (a *AdminServer) handleLogging(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	level := r.URL.Query().Get("level")
	if level == "" {
		http.Error(w, `Missing "level" query parameter`, http.StatusBadRequest)
		return
	}

	var zapLevel zap.AtomicLevel
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid level: " + level})
		return
	}

	a.logLevel.SetLevel(zapLevel.Level())
	a.logger.Info("Log level changed via admin API", zap.String("new_level", level))

	writeJSON(w, http.StatusOK, map[string]string{
		"level":   a.logLevel.Level().String(),
		"message": "log level updated",
	})
}

// writeJSON serializes v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, statusCode int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		// Best effort: the header has already been sent so we can only log.
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}
