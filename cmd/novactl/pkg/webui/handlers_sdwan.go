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
	"net/http"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

// SD-WAN data types for API responses.

type sdwanLinkResponse struct {
	Name       string  `json:"name"`
	Namespace  string  `json:"namespace"`
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
	Namespace  string   `json:"namespace"`
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

	links, err := s.listWANLinks(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list WAN links")
		return
	}

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

	links, err := s.listWANLinks(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list WAN links for topology")
		return
	}

	// Build topology from links
	siteMap := make(map[string]bool)
	edges := make([]sdwanEdge, 0, len(links))
	for _, link := range links {
		siteMap[link.Site] = true
		edges = append(edges, sdwanEdge{
			From:      link.Site,
			To:        link.Site,
			LinkName:  link.Name,
			LatencyMs: link.LatencyMs,
			Healthy:   link.Healthy,
		})
	}

	sites := make([]sdwanSite, 0, len(siteMap))
	for site := range siteMap {
		sites = append(sites, sdwanSite{Name: site})
	}

	topology := sdwanTopologyResponse{
		Sites: sites,
		Links: edges,
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

	policies, err := s.listWANPolicies(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list WAN policies")
		return
	}

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

	// Events are not persisted in CRDs; return empty array until an event store is added.
	events := []sdwanEventResponse{}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(events); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode response")
	}
}

// listWANLinks lists ProxyWANLink CRDs and maps them to response objects.
func (s *Server) listWANLinks(ctx context.Context) ([]sdwanLinkResponse, error) {
	if s.crdClient == nil {
		return []sdwanLinkResponse{}, nil
	}

	linkList := &novaedgev1alpha1.ProxyWANLinkList{}
	if err := s.crdClient.List(ctx, linkList); err != nil {
		return nil, err
	}

	links := make([]sdwanLinkResponse, 0, len(linkList.Items))
	for i := range linkList.Items {
		link := &linkList.Items[i]
		resp := sdwanLinkResponse{
			Name:      link.Name,
			Namespace: link.Namespace,
			Site:      link.Spec.Site,
			Provider:  link.Spec.Provider,
			Role:      string(link.Spec.Role),
			Bandwidth: link.Spec.Bandwidth,
			Healthy:   link.Status.Healthy,
		}
		if link.Status.CurrentLatency != nil {
			resp.LatencyMs = *link.Status.CurrentLatency
		}
		if link.Status.CurrentPacketLoss != nil {
			resp.PacketLoss = *link.Status.CurrentPacketLoss
		}
		links = append(links, resp)
	}

	return links, nil
}

// listWANPolicies lists ProxyWANPolicy CRDs and maps them to response objects.
func (s *Server) listWANPolicies(ctx context.Context) ([]sdwanPolicyResponse, error) {
	if s.crdClient == nil {
		return []sdwanPolicyResponse{}, nil
	}

	policyList := &novaedgev1alpha1.ProxyWANPolicyList{}
	if err := s.crdClient.List(ctx, policyList); err != nil {
		return nil, err
	}

	policies := make([]sdwanPolicyResponse, 0, len(policyList.Items))
	for i := range policyList.Items {
		p := &policyList.Items[i]
		policies = append(policies, sdwanPolicyResponse{
			Name:       p.Name,
			Namespace:  p.Namespace,
			Strategy:   string(p.Spec.PathSelection.Strategy),
			MatchHosts: p.Spec.Match.Hosts,
			DSCPClass:  p.Spec.PathSelection.DSCPClass,
			Selections: p.Status.SelectionCount,
		})
	}

	return policies, nil
}
