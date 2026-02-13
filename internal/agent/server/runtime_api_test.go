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
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/config"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func newTestRuntimeAPI(t *testing.T) *RuntimeAPI {
	t.Helper()
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	return NewRuntimeAPI(logger)
}

func doRequest(t *testing.T, api *RuntimeAPI, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("failed to encode request body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	api.ServeHTTP(rr, req)
	return rr
}

func TestAddEndpoint(t *testing.T) {
	api := newTestRuntimeAPI(t)

	body := addEndpointRequest{
		Address: "10.0.0.5",
		Port:    8080,
		Weight:  100,
	}
	rr := doRequest(t, api, http.MethodPost, "/api/v1/clusters/my-cluster/endpoints", body)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rr.Code, rr.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "added" {
		t.Errorf("expected status 'added', got %q", resp["status"])
	}
	if resp["endpoint"] != "10.0.0.5:8080" {
		t.Errorf("expected endpoint '10.0.0.5:8080', got %q", resp["endpoint"])
	}

	// Verify the override was stored
	co := api.Overrides().getOrCreateCluster("my-cluster")
	co.mu.RLock()
	defer co.mu.RUnlock()
	if _, exists := co.Added["10.0.0.5:8080"]; !exists {
		t.Error("endpoint override was not stored")
	}
}

func TestAddEndpointInvalidBody(t *testing.T) {
	api := newTestRuntimeAPI(t)

	// Missing address
	body := addEndpointRequest{Port: 8080}
	rr := doRequest(t, api, http.MethodPost, "/api/v1/clusters/my-cluster/endpoints", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestRemoveEndpoint(t *testing.T) {
	api := newTestRuntimeAPI(t)

	// First add an endpoint
	body := addEndpointRequest{Address: "10.0.0.5", Port: 8080, Weight: 100}
	doRequest(t, api, http.MethodPost, "/api/v1/clusters/my-cluster/endpoints", body)

	// Now remove it
	rr := doRequest(t, api, http.MethodDelete, "/api/v1/clusters/my-cluster/endpoints/10.0.0.5:8080", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "removed" {
		t.Errorf("expected status 'removed', got %q", resp["status"])
	}

	// Verify the endpoint was removed from added and marked as removed
	co := api.Overrides().getOrCreateCluster("my-cluster")
	co.mu.RLock()
	defer co.mu.RUnlock()
	if _, exists := co.Added["10.0.0.5:8080"]; exists {
		t.Error("endpoint should have been removed from added map")
	}
	if !co.Removed["10.0.0.5:8080"] {
		t.Error("endpoint should be marked as removed")
	}
}

func TestChangeWeight(t *testing.T) {
	api := newTestRuntimeAPI(t)

	body := changeWeightRequest{Weight: 50}
	rr := doRequest(t, api, http.MethodPut, "/api/v1/clusters/my-cluster/endpoints/10.0.0.1:8080/weight", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "weight_updated" {
		t.Errorf("expected status 'weight_updated', got %q", resp["status"])
	}

	co := api.Overrides().getOrCreateCluster("my-cluster")
	co.mu.RLock()
	defer co.mu.RUnlock()
	if co.Weights["10.0.0.1:8080"] != 50 {
		t.Errorf("expected weight 50, got %d", co.Weights["10.0.0.1:8080"])
	}
}

func TestChangeWeightNegative(t *testing.T) {
	api := newTestRuntimeAPI(t)

	body := changeWeightRequest{Weight: -1}
	rr := doRequest(t, api, http.MethodPut, "/api/v1/clusters/my-cluster/endpoints/10.0.0.1:8080/weight", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestDrainEndpoint(t *testing.T) {
	api := newTestRuntimeAPI(t)

	rr := doRequest(t, api, http.MethodPut, "/api/v1/clusters/my-cluster/endpoints/10.0.0.1:8080/drain", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "draining" {
		t.Errorf("expected status 'draining', got %q", resp["status"])
	}

	co := api.Overrides().getOrCreateCluster("my-cluster")
	co.mu.RLock()
	defer co.mu.RUnlock()
	if !co.Draining["10.0.0.1:8080"] {
		t.Error("endpoint should be marked as draining")
	}
}

func TestListOverrides(t *testing.T) {
	api := newTestRuntimeAPI(t)

	// Add some overrides
	doRequest(t, api, http.MethodPost, "/api/v1/clusters/cluster-a/endpoints", addEndpointRequest{
		Address: "10.0.0.5", Port: 8080, Weight: 100,
	})
	doRequest(t, api, http.MethodPut, "/api/v1/clusters/cluster-b/endpoints/10.0.0.1:9090/drain", nil)

	rr := doRequest(t, api, http.MethodGet, "/api/v1/overrides", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var resp overridesListResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Clusters) != 2 {
		t.Fatalf("expected 2 clusters in overrides, got %d", len(resp.Clusters))
	}
	if _, exists := resp.Clusters["cluster-a"]; !exists {
		t.Error("expected cluster-a in overrides")
	}
	if _, exists := resp.Clusters["cluster-b"]; !exists {
		t.Error("expected cluster-b in overrides")
	}
}

func TestClearOverrides(t *testing.T) {
	api := newTestRuntimeAPI(t)

	// Add some overrides
	doRequest(t, api, http.MethodPost, "/api/v1/clusters/cluster-a/endpoints", addEndpointRequest{
		Address: "10.0.0.5", Port: 8080, Weight: 100,
	})
	doRequest(t, api, http.MethodPut, "/api/v1/clusters/cluster-b/endpoints/10.0.0.1:9090/drain", nil)

	// Clear all
	rr := doRequest(t, api, http.MethodDelete, "/api/v1/overrides", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	// Verify cleared
	if api.Overrides().HasOverrides() {
		t.Error("expected no overrides after clear")
	}
}

func TestApplyOverridesToSnapshot(t *testing.T) {
	api := newTestRuntimeAPI(t)

	// Build a base snapshot with existing endpoints
	snapshot := &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version: "v1",
			Clusters: []*pb.Cluster{
				{Name: "web-backend", Namespace: "default"},
			},
			Endpoints: map[string]*pb.EndpointList{
				"web-backend": {
					Endpoints: []*pb.Endpoint{
						{Address: "10.0.0.1", Port: 8080, Ready: true},
						{Address: "10.0.0.2", Port: 8080, Ready: true},
						{Address: "10.0.0.3", Port: 8080, Ready: true},
					},
				},
			},
		},
	}

	// Add an endpoint
	doRequest(t, api, http.MethodPost, "/api/v1/clusters/web-backend/endpoints", addEndpointRequest{
		Address: "10.0.0.4", Port: 8080, Weight: 100,
	})

	// Remove an endpoint
	doRequest(t, api, http.MethodDelete, "/api/v1/clusters/web-backend/endpoints/10.0.0.2:8080", nil)

	// Drain an endpoint
	doRequest(t, api, http.MethodPut, "/api/v1/clusters/web-backend/endpoints/10.0.0.3:8080/drain", nil)

	// Apply overrides
	result := api.Overrides().ApplyOverrides(snapshot)

	// Verify the original snapshot was not modified
	if len(snapshot.Endpoints["web-backend"].Endpoints) != 3 {
		t.Fatal("original snapshot was modified")
	}

	// Verify the result
	resultEps := result.Endpoints["web-backend"].Endpoints

	// Should have: 10.0.0.1 (original), 10.0.0.3 (drained, not ready), 10.0.0.4 (added)
	// 10.0.0.2 was removed
	if len(resultEps) != 3 {
		t.Fatalf("expected 3 endpoints after overrides, got %d", len(resultEps))
	}

	epMap := make(map[string]*pb.Endpoint)
	for _, ep := range resultEps {
		key := ep.Address + ":" + formatPort(ep.Port)
		epMap[key] = ep
	}

	// 10.0.0.1 should still be ready
	if ep, exists := epMap["10.0.0.1:8080"]; !exists {
		t.Error("expected 10.0.0.1:8080 to exist")
	} else if !ep.Ready {
		t.Error("expected 10.0.0.1:8080 to be ready")
	}

	// 10.0.0.2 should be gone
	if _, exists := epMap["10.0.0.2:8080"]; exists {
		t.Error("expected 10.0.0.2:8080 to be removed")
	}

	// 10.0.0.3 should be draining (not ready)
	if ep, exists := epMap["10.0.0.3:8080"]; !exists {
		t.Error("expected 10.0.0.3:8080 to exist")
	} else if ep.Ready {
		t.Error("expected 10.0.0.3:8080 to be not ready (draining)")
	}

	// 10.0.0.4 should be added and ready
	if ep, exists := epMap["10.0.0.4:8080"]; !exists {
		t.Error("expected 10.0.0.4:8080 to exist")
	} else if !ep.Ready {
		t.Error("expected 10.0.0.4:8080 to be ready")
	}
}

func TestApplyOverridesNilSnapshot(t *testing.T) {
	api := newTestRuntimeAPI(t)

	result := api.Overrides().ApplyOverrides(nil)
	if result != nil {
		t.Error("expected nil result for nil snapshot")
	}
}

func TestApplyOverridesNoOverrides(t *testing.T) {
	api := newTestRuntimeAPI(t)

	snapshot := &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version: "v1",
			Endpoints: map[string]*pb.EndpointList{
				"web-backend": {
					Endpoints: []*pb.Endpoint{
						{Address: "10.0.0.1", Port: 8080, Ready: true},
					},
				},
			},
		},
	}

	result := api.Overrides().ApplyOverrides(snapshot)
	// Should return the same snapshot when no overrides exist
	if result != snapshot {
		t.Error("expected same snapshot when no overrides exist")
	}
}

func TestInvalidPaths(t *testing.T) {
	api := newTestRuntimeAPI(t)

	tests := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{"invalid cluster path", http.MethodGet, "/api/v1/clusters/foo/bar", http.StatusNotFound},
		{"wrong method on endpoints", http.MethodGet, "/api/v1/clusters/foo/endpoints", http.StatusNotFound},
		{"wrong method on overrides", http.MethodPost, "/api/v1/overrides", http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := doRequest(t, api, tt.method, tt.path, nil)
			if rr.Code != tt.want {
				t.Errorf("expected status %d, got %d", tt.want, rr.Code)
			}
		})
	}
}

// formatPort converts a port number to string for test assertions.
func formatPort(port int32) string {
	return fmt.Sprintf("%d", port)
}
