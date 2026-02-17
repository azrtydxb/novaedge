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

package sdwan

import (
	"testing"

	"go.uber.org/zap"
)

func newTestLinkManager() (*LinkManager, *Prober, *PathSelector) {
	logger := zap.NewNop()
	prober := NewProber(logger)
	selector := NewPathSelector(logger)
	mgr := NewLinkManager(prober, selector, logger)
	return mgr, prober, selector
}

func TestLinkManager_AddLink(t *testing.T) {
	mgr, _, _ := newTestLinkManager()

	err := mgr.AddLink(LinkConfig{
		Name:      "wan-1",
		Site:      "dc-east",
		Provider:  "ISP-A",
		Role:      RolePrimary,
		Bandwidth: "1Gbps",
		Cost:      100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	link, exists := mgr.GetLink("wan-1")
	if !exists {
		t.Fatal("expected link wan-1 to exist")
	}
	if link.Name != "wan-1" {
		t.Errorf("expected name 'wan-1', got %q", link.Name)
	}
	if link.Site != "dc-east" {
		t.Errorf("expected site 'dc-east', got %q", link.Site)
	}
	if link.State != LinkStateActive {
		t.Errorf("expected initial state 'active', got %q", link.State)
	}
}

func TestLinkManager_AddLink_Empty(t *testing.T) {
	mgr, _, _ := newTestLinkManager()

	err := mgr.AddLink(LinkConfig{})
	if err == nil {
		t.Error("expected error for empty link name")
	}
}

func TestLinkManager_AddLink_Duplicate(t *testing.T) {
	mgr, _, _ := newTestLinkManager()

	err := mgr.AddLink(LinkConfig{Name: "wan-1", Site: "dc-east"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = mgr.AddLink(LinkConfig{Name: "wan-1", Site: "dc-west"})
	if err == nil {
		t.Error("expected error for duplicate link")
	}
}

func TestLinkManager_RemoveLink(t *testing.T) {
	mgr, _, _ := newTestLinkManager()

	err := mgr.AddLink(LinkConfig{Name: "wan-1", Site: "dc-east"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = mgr.RemoveLink("wan-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, exists := mgr.GetLink("wan-1")
	if exists {
		t.Error("expected link to be removed")
	}
}

func TestLinkManager_RemoveLink_NotFound(t *testing.T) {
	mgr, _, _ := newTestLinkManager()

	err := mgr.RemoveLink("nonexistent")
	if err == nil {
		t.Error("expected error for removing nonexistent link")
	}
}

func TestLinkManager_GetAllLinks(t *testing.T) {
	mgr, _, _ := newTestLinkManager()

	_ = mgr.AddLink(LinkConfig{Name: "wan-1", Site: "dc-east"})
	_ = mgr.AddLink(LinkConfig{Name: "wan-2", Site: "dc-west"})

	all := mgr.GetAllLinks()
	if len(all) != 2 {
		t.Errorf("expected 2 links, got %d", len(all))
	}
}

func TestLinkManager_GetLinksForSite(t *testing.T) {
	mgr, _, _ := newTestLinkManager()

	_ = mgr.AddLink(LinkConfig{Name: "wan-1", Site: "dc-east"})
	_ = mgr.AddLink(LinkConfig{Name: "wan-2", Site: "dc-west"})
	_ = mgr.AddLink(LinkConfig{Name: "wan-3", Site: "dc-east"})

	east := mgr.GetLinksForSite("dc-east")
	if len(east) != 2 {
		t.Errorf("expected 2 links for dc-east, got %d", len(east))
	}

	west := mgr.GetLinksForSite("dc-west")
	if len(west) != 1 {
		t.Errorf("expected 1 link for dc-west, got %d", len(west))
	}

	none := mgr.GetLinksForSite("nonexistent")
	if len(none) != 0 {
		t.Errorf("expected 0 links for nonexistent site, got %d", len(none))
	}
}

func TestLinkManager_UpdateLinkStates(t *testing.T) {
	mgr, prober, _ := newTestLinkManager()

	_ = mgr.AddLink(LinkConfig{
		Name:      "wan-1",
		Site:      "dc-east",
		ProbeAddr: "10.0.0.1:5000",
	})

	// Manually set quality to simulate probe results
	prober.mu.Lock()
	prober.qualities["wan-1"] = &LinkQuality{
		LinkName:   "wan-1",
		PacketLoss: 0.0,
		Healthy:    true,
	}
	prober.mu.Unlock()

	mgr.UpdateLinkStates()

	link, _ := mgr.GetLink("wan-1")
	if link.State != LinkStateActive {
		t.Errorf("expected active state, got %q", link.State)
	}

	// Simulate degraded
	prober.mu.Lock()
	prober.qualities["wan-1"].Healthy = false
	prober.qualities["wan-1"].PacketLoss = 0.3
	prober.mu.Unlock()

	mgr.UpdateLinkStates()

	link, _ = mgr.GetLink("wan-1")
	if link.State != LinkStateDegraded {
		t.Errorf("expected degraded state, got %q", link.State)
	}

	// Simulate down
	prober.mu.Lock()
	prober.qualities["wan-1"].PacketLoss = 1.0
	prober.mu.Unlock()

	mgr.UpdateLinkStates()

	link, _ = mgr.GetLink("wan-1")
	if link.State != LinkStateDown {
		t.Errorf("expected down state, got %q", link.State)
	}
}

func TestLinkManager_SelectPathForPolicy_NoData(t *testing.T) {
	mgr, _, _ := newTestLinkManager()

	_, err := mgr.SelectPathForPolicy("policy1", StrategyLowestLatency)
	if err == nil {
		t.Error("expected error when no quality data available")
	}
}
