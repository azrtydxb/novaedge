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
	"net/http"
)

// SD-WAN data types for API responses.

type sdwanLinkResponse struct {
	Name       string  `json:"name"`
	Site       string  `json:"site"`
	Provider   string  `json:"provider"`
	Role       string  `json:"role"`
	Bandwidth  string  `json:"bandwidth"`
	LatencyMs  float64 `json:"latencyMs"`
	JitterMs   float64 `json:"jitterMs"`
	PacketLoss float64 `json:"packetLossPercent"`
	Score      float64 `json:"score"`
	Healthy    bool    `json:"healthy"`
}

type sdwanTopologyResponse struct {
	Sites []sdwanSite `json:"sites"`
	Links []sdwanEdge `json:"links"`
}

type sdwanSite struct {
	Name        string `json:"name"`
	Region      string `json:"region"`
	OverlayAddr string `json:"overlayAddr"`
}

type sdwanEdge struct {
	From      string  `json:"from"`
	To        string  `json:"to"`
	LinkName  string  `json:"linkName"`
	LatencyMs float64 `json:"latencyMs"`
	Healthy   bool    `json:"healthy"`
}

type sdwanPolicyResponse struct {
	Name       string   `json:"name"`
	Strategy   string   `json:"strategy"`
	MatchHosts []string `json:"matchHosts"`
	DSCPClass  string   `json:"dscpClass"`
	Selections int64    `json:"selections"`
}

type sdwanEventResponse struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	FromLink  string `json:"fromLink"`
	ToLink    string `json:"toLink"`
	Reason    string `json:"reason"`
	Policy    string `json:"policy"`
}

// handleSDWANLinks handles GET /api/v1/sdwan/links.
// Returns SD-WAN link status from ProxyWANLink CRDs.
func (s *Server) handleSDWANLinks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Return SD-WAN link data from Kubernetes (list ProxyWANLink CRDs).
	// Populated when controller integration is added.
	links := []sdwanLinkResponse{}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(links); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode response")
	}
}

// handleSDWANTopology handles GET /api/v1/sdwan/topology.
// Returns the SD-WAN site and link topology graph.
func (s *Server) handleSDWANTopology(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	topology := sdwanTopologyResponse{
		Sites: []sdwanSite{},
		Links: []sdwanEdge{},
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(topology); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode response")
	}
}

// handleSDWANPolicies handles GET /api/v1/sdwan/policies.
// Returns SD-WAN path-selection policies from ProxyWANPolicy CRDs.
func (s *Server) handleSDWANPolicies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	policies := []sdwanPolicyResponse{}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(policies); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode response")
	}
}

// handleSDWANEvents handles GET /api/v1/sdwan/events.
// Returns recent SD-WAN failover and path-switch events.
func (s *Server) handleSDWANEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	events := []sdwanEventResponse{}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(events); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode response")
	}
}
