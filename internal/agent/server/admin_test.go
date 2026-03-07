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
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/azrtydxb/novaedge/internal/agent/config"
	pb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

// newTestAdminServer creates an AdminServer wired up for unit tests.
func newTestAdminServer(t *testing.T) *AdminServer {
	t.Helper()
	logger := zaptest.NewLogger(t)
	srv := NewAdminServer("", logger)
	srv.SetAtomicLevel(zap.NewAtomicLevelAt(zap.InfoLevel))
	return srv
}

// testAdminMux builds the http.ServeMux used by AdminServer so we can call
// handlers directly via httptest without starting a real listener.
func testAdminMux(a *AdminServer) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ready", a.handleReady)
	mux.HandleFunc("/health", a.handleHealth)
	mux.HandleFunc("/stats", a.handleStats)
	mux.HandleFunc("/clusters", a.handleClusters)
	mux.HandleFunc("/config", a.handleConfig)
	mux.HandleFunc("/routes", a.handleRoutes)
	mux.HandleFunc("/logging", a.handleLogging)
	return mux
}

func TestAdminReady(t *testing.T) {
	srv := newTestAdminServer(t)
	mux := testAdminMux(srv)

	// Default: ready is false
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["ready"] {
		t.Fatal("expected ready=false initially")
	}

	// Mark ready
	srv.SetReady(true)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !body["ready"] {
		t.Fatal("expected ready=true after SetReady(true)")
	}
}

func TestAdminHealthReturnsJSON(t *testing.T) {
	srv := newTestAdminServer(t)
	mux := testAdminMux(srv)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	for _, key := range []string{"uptime_seconds", "config_version", "goroutine_count", "memory"} {
		if _, ok := body[key]; !ok {
			t.Errorf("missing key %q in health response", key)
		}
	}
}

func TestAdminStatsReturnsJSON(t *testing.T) {
	srv := newTestAdminServer(t)
	mux := testAdminMux(srv)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stats", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	for _, key := range []string{"total_clusters", "total_routes", "total_endpoints", "goroutine_count"} {
		if _, ok := body[key]; !ok {
			t.Errorf("missing key %q in stats response", key)
		}
	}
}

func TestAdminClustersReturnsJSON(t *testing.T) {
	srv := newTestAdminServer(t)
	mux := testAdminMux(srv)

	// Set a snapshot with clusters
	srv.SetSnapshot(&config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version: "v1",
			Clusters: []*pb.Cluster{
				{Name: "web", Namespace: "default", LbPolicy: pb.LoadBalancingPolicy_ROUND_ROBIN},
			},
			Endpoints: map[string]*pb.EndpointList{
				"default/web": {
					Endpoints: []*pb.Endpoint{
						{Address: "10.0.0.1", Port: 8080, Ready: true},
						{Address: "10.0.0.2", Port: 8080, Ready: false},
					},
				},
			},
		},
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/clusters", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	clusters, ok := body["clusters"].([]interface{})
	if !ok {
		t.Fatal("expected clusters array")
	}
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(clusters))
	}
}

func TestAdminConfigReturnsJSON(t *testing.T) {
	srv := newTestAdminServer(t)
	mux := testAdminMux(srv)

	srv.SetSnapshot(&config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version:  "v42",
			Routes:   []*pb.Route{{Name: "r1", Namespace: "ns1"}},
			Clusters: []*pb.Cluster{{Name: "c1", Namespace: "ns1"}},
		},
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["version"] != "v42" {
		t.Fatalf("expected version v42, got %v", body["version"])
	}
}

func TestAdminRoutesReturnsJSON(t *testing.T) {
	srv := newTestAdminServer(t)
	mux := testAdminMux(srv)

	srv.SetSnapshot(&config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version: "v1",
			Routes: []*pb.Route{
				{
					Name:      "my-route",
					Namespace: "default",
					Hostnames: []string{"example.com"},
					Rules: []*pb.RouteRule{
						{
							Matches: []*pb.RouteMatch{
								{
									Path: &pb.PathMatch{
										Type:  pb.PathMatchType_PATH_PREFIX,
										Value: "/api",
									},
								},
							},
							BackendRefs: []*pb.BackendRef{
								{Name: "api-svc", Namespace: "default", Weight: 100},
							},
						},
					},
				},
			},
		},
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/routes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	routes, ok := body["routes"].(map[string]interface{})
	if !ok {
		t.Fatal("expected routes map")
	}
	if _, ok := routes["example.com"]; !ok {
		t.Fatal("expected example.com in routes")
	}
}

func TestAdminLoggingChangesLevel(t *testing.T) {
	srv := newTestAdminServer(t)
	mux := testAdminMux(srv)

	// Change to debug
	req := httptest.NewRequest(http.MethodPut, "/logging?level=debug", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["level"] != "debug" {
		t.Fatalf("expected level=debug, got %s", body["level"])
	}

	// Change to warn
	req = httptest.NewRequest(http.MethodPut, "/logging?level=warn", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["level"] != "warn" {
		t.Fatalf("expected level=warn, got %s", body["level"])
	}
}

func TestAdminLoggingRejectsGET(t *testing.T) {
	srv := newTestAdminServer(t)
	mux := testAdminMux(srv)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/logging", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestAdminLoggingInvalidLevel(t *testing.T) {
	srv := newTestAdminServer(t)
	mux := testAdminMux(srv)

	req := httptest.NewRequest(http.MethodPut, "/logging?level=bogus", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAdminLoggingMissingLevel(t *testing.T) {
	srv := newTestAdminServer(t)
	mux := testAdminMux(srv)

	req := httptest.NewRequest(http.MethodPut, "/logging", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAdminUnknownEndpoint404(t *testing.T) {
	srv := newTestAdminServer(t)
	mux := testAdminMux(srv)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nonexistent", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown endpoint, got %d", rec.Code)
	}
}

func TestAdminDefaultAddr(t *testing.T) {
	srv := NewAdminServer("", zaptest.NewLogger(t))
	if srv.addr != DefaultAdminAddr {
		t.Fatalf("expected default addr %s, got %s", DefaultAdminAddr, srv.addr)
	}
}

func TestAdminCustomAddr(t *testing.T) {
	srv := NewAdminServer("0.0.0.0:8888", zaptest.NewLogger(t))
	if srv.addr != "0.0.0.0:8888" {
		t.Fatalf("expected 0.0.0.0:8888, got %s", srv.addr)
	}
}

func TestAdminNoSnapshotGraceful(t *testing.T) {
	srv := newTestAdminServer(t)
	mux := testAdminMux(srv)

	// All endpoints should return 200 with empty/default data when no snapshot is set
	for _, endpoint := range []string{"/health", "/stats", "/clusters", "/config", "/routes"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, endpoint, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: expected 200 with no snapshot, got %d", endpoint, rec.Code)
		}
		var body map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Errorf("%s: invalid JSON with no snapshot: %v", endpoint, err)
		}
	}
}
