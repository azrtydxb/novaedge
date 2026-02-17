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

package tunnel

import (
	"testing"

	"go.uber.org/zap"
)

func TestNewOverlayRouteManager(t *testing.T) {
	logger := zap.NewNop()
	mgr := NewOverlayRouteManager(logger)

	if mgr == nil {
		t.Fatal("expected non-nil overlay route manager")
	}

	if mgr.routes == nil {
		t.Fatal("expected non-nil routes map")
	}

	if mgr.logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestOverlayRouteManagerGetRoutesEmpty(t *testing.T) {
	logger := zap.NewNop()
	mgr := NewOverlayRouteManager(logger)

	routes := mgr.GetRoutes()
	if len(routes) != 0 {
		t.Fatalf("expected empty routes map, got %d entries", len(routes))
	}
}

func TestOverlayRouteManagerRemoveNonexistent(t *testing.T) {
	logger := zap.NewNop()
	mgr := NewOverlayRouteManager(logger)

	err := mgr.RemoveRoute("10.200.0.0/24")
	if err == nil {
		t.Fatal("expected error when removing nonexistent route")
	}
}

func TestOverlayRouteManagerRemoveAllEmpty(t *testing.T) {
	logger := zap.NewNop()
	mgr := NewOverlayRouteManager(logger)

	// Should not panic when called on empty routes
	mgr.RemoveAllRoutes()

	routes := mgr.GetRoutes()
	if len(routes) != 0 {
		t.Fatalf("expected empty routes after RemoveAllRoutes, got %d", len(routes))
	}
}

func TestOverlayRouteManagerInstallInvalidCIDR(t *testing.T) {
	logger := zap.NewNop()
	mgr := NewOverlayRouteManager(logger)

	err := mgr.InstallRoute("not-a-cidr", "wg0")
	if err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
}

func TestOverlayRouteManagerInstallInvalidInterface(t *testing.T) {
	logger := zap.NewNop()
	mgr := NewOverlayRouteManager(logger)

	// This will fail because the interface doesn't exist,
	// which is expected behavior
	err := mgr.InstallRoute("10.200.0.0/24", "nonexistent-iface-xyz")
	if err == nil {
		t.Fatal("expected error for nonexistent interface")
	}
}
