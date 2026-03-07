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

package config

import (
	"testing"

	pb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

func TestSnapshot_GetListenerExtensions_NilExtensions(t *testing.T) {
	s := &Snapshot{
		Extensions: nil,
	}

	ext := s.GetListenerExtensions("gateway-key", "listener-0")
	if ext != nil {
		t.Error("GetListenerExtensions() should return nil when Extensions is nil")
	}
}

func TestSnapshot_GetListenerExtensions_NilMap(t *testing.T) {
	s := &Snapshot{
		Extensions: &SnapshotExtensions{
			ListenerExtensions: nil,
		},
	}

	ext := s.GetListenerExtensions("gateway-key", "listener-0")
	if ext != nil {
		t.Error("GetListenerExtensions() should return nil when ListenerExtensions map is nil")
	}
}

func TestSnapshot_GetListenerExtensions_NotFound(t *testing.T) {
	s := &Snapshot{
		Extensions: &SnapshotExtensions{
			ListenerExtensions: map[string]*pb.ListenerExtensions{
				"other-gateway/listener-0": {},
			},
		},
	}

	ext := s.GetListenerExtensions("gateway-key", "listener-0")
	if ext != nil {
		t.Error("GetListenerExtensions() should return nil when key not found")
	}
}

func TestSnapshot_GetListenerExtensions_Found(t *testing.T) {
	expected := &pb.ListenerExtensions{
		ClientAuth: &pb.ClientAuthConfig{
			Mode: "require",
		},
		OCSPStapling: true,
	}

	s := &Snapshot{
		Extensions: &SnapshotExtensions{
			ListenerExtensions: map[string]*pb.ListenerExtensions{
				"gateway-key/listener-0": expected,
			},
		},
	}

	ext := s.GetListenerExtensions("gateway-key", "listener-0")
	if ext == nil {
		t.Fatal("GetListenerExtensions() should return extension when found")
	}
	if ext != expected {
		t.Error("GetListenerExtensions() should return the correct extension")
	}
}

func TestSnapshot_GetListenerExtensions_KeyFormat(t *testing.T) {
	// Test that the key is correctly formatted as "gatewayKey/listenerName"
	s := &Snapshot{
		Extensions: &SnapshotExtensions{
			ListenerExtensions: map[string]*pb.ListenerExtensions{
				"ns/my-gateway/http": {},
			},
		},
	}

	tests := []struct {
		gatewayKey    string
		listenerName  string
		shouldBeFound bool
	}{
		{"ns/my-gateway", "http", true},
		{"ns/my-gateway", "https", false},
		{"ns/other", "http", false},
		{"", "http", false},
		{"ns/my-gateway", "", false},
	}

	for _, tt := range tests {
		ext := s.GetListenerExtensions(tt.gatewayKey, tt.listenerName)
		found := ext != nil
		if found != tt.shouldBeFound {
			t.Errorf("GetListenerExtensions(%q, %q) found = %v, want %v",
				tt.gatewayKey, tt.listenerName, found, tt.shouldBeFound)
		}
	}
}

func TestSnapshot_GetClusterExtensions_NilExtensions(t *testing.T) {
	s := &Snapshot{
		Extensions: nil,
	}

	ext := s.GetClusterExtensions("cluster-key")
	if ext != nil {
		t.Error("GetClusterExtensions() should return nil when Extensions is nil")
	}
}

func TestSnapshot_GetClusterExtensions_NilMap(t *testing.T) {
	s := &Snapshot{
		Extensions: &SnapshotExtensions{
			ClusterExtensions: nil,
		},
	}

	ext := s.GetClusterExtensions("cluster-key")
	if ext != nil {
		t.Error("GetClusterExtensions() should return nil when ClusterExtensions map is nil")
	}
}

func TestSnapshot_GetClusterExtensions_NotFound(t *testing.T) {
	s := &Snapshot{
		Extensions: &SnapshotExtensions{
			ClusterExtensions: map[string]*pb.ClusterExtensions{
				"other-cluster": {},
			},
		},
	}

	ext := s.GetClusterExtensions("cluster-key")
	if ext != nil {
		t.Error("GetClusterExtensions() should return nil when key not found")
	}
}

func TestSnapshot_GetClusterExtensions_Found(t *testing.T) {
	expected := &pb.ClusterExtensions{
		UpstreamProxyProtocol: &pb.UpstreamProxyProtocol{
			Enabled: true,
			Version: 2,
		},
	}

	s := &Snapshot{
		Extensions: &SnapshotExtensions{
			ClusterExtensions: map[string]*pb.ClusterExtensions{
				"ns/backend-api": expected,
			},
		},
	}

	ext := s.GetClusterExtensions("ns/backend-api")
	if ext == nil {
		t.Fatal("GetClusterExtensions() should return extension when found")
	}
	if ext != expected {
		t.Error("GetClusterExtensions() should return the correct extension")
	}
}

func TestSnapshotExtensions_EmptyMaps(t *testing.T) {
	ext := &SnapshotExtensions{
		ListenerExtensions: map[string]*pb.ListenerExtensions{},
		ClusterExtensions:  map[string]*pb.ClusterExtensions{},
	}

	s := &Snapshot{Extensions: ext}

	if s.GetListenerExtensions("any", "listener") != nil {
		t.Error("GetListenerExtensions() should return nil for empty map")
	}
	if s.GetClusterExtensions("any") != nil {
		t.Error("GetClusterExtensions() should return nil for empty map")
	}
}
